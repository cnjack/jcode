package artifact

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg" // Register JPEG decoding for managed-image validation.
	_ "image/png"  // Register PNG decoding for managed-image validation.
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
)

const (
	maxManagedImageSize   int64       = 20 << 20
	maxManagedImageEdge               = 8192
	maxManagedImagePixels int64       = 40_000_000
	managedDirMode        os.FileMode = 0o700
	managedFileMode       os.FileMode = 0o600
)

// ManagedImageRequest is the only P0 managed-content write contract. The
// caller supplies bytes, not a path; the service chooses the opaque ID, key,
// extension, permissions, and final location from validated content.
type ManagedImageRequest struct {
	SessionID        string
	Title            string
	Reader           io.Reader
	ProviderID       string
	ModelID          string
	ParentArtifactID string
	OperationID      string
	ToolCallID       string
	Focus            bool

	// Expected values are optional defense-in-depth assertions against metadata
	// supplied by an adapter. The bytes remain the source of truth.
	ExpectedMediaType string
	ExpectedWidth     int
	ExpectedHeight    int
	ExpectedSHA256    string
}

// CreateManagedImage validates and atomically stores one generated image. If
// metadata persistence fails after the rename, the returned Record identifies
// the preserved orphan for diagnostics; it is intentionally not removed and
// not published into the in-memory registry.
func (s *Service) CreateManagedImage(
	ctx context.Context,
	req ManagedImageRequest,
	recorder Recorder,
) (Record, error) {
	if err := ctx.Err(); err != nil {
		return Record{}, err
	}
	if strings.TrimSpace(req.SessionID) == "" || req.Reader == nil || recorder == nil {
		return Record{}, fmt.Errorf("managed image requires a session, reader, and recorder")
	}
	title := strings.TrimSpace(req.Title)
	if title == "" {
		title = "Generated image"
	}
	if utf8.RuneCountInString(title) > maxTitleRunes {
		return Record{}, fmt.Errorf("artifact title exceeds %d characters", maxTitleRunes)
	}

	root, err := s.openManagedImagesRoot(req.SessionID, true)
	if err != nil {
		return Record{}, err
	}
	defer func() { _ = root.Close() }()

	generationID := uuid.NewString()
	tempName := "." + generationID + ".tmp"
	temp, err := root.OpenFile(tempName, os.O_CREATE|os.O_EXCL|os.O_RDWR, managedFileMode)
	if err != nil {
		return Record{}, fmt.Errorf("create managed artifact temp file: %w", err)
	}
	tempOpen := true
	committed := false
	defer func() {
		if tempOpen {
			_ = temp.Close()
		}
		if !committed {
			_ = root.Remove(tempName)
		}
	}()

	size, digest, err := copyManagedImage(ctx, temp, req.Reader)
	if err != nil {
		return Record{}, err
	}
	mediaType, extension, width, height, err := inspectManagedImage(temp, size)
	if err != nil {
		return Record{}, err
	}
	if err := validateExpectedImage(req, mediaType, width, height, digest); err != nil {
		return Record{}, err
	}
	if err := temp.Sync(); err != nil {
		return Record{}, fmt.Errorf("sync managed artifact: %w", err)
	}
	info, err := temp.Stat()
	if err != nil {
		return Record{}, fmt.Errorf("stat managed artifact temp file: %w", err)
	}
	lstat, err := root.Lstat(tempName)
	if err != nil || lstat.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() ||
		!os.SameFile(lstat, info) || hasMultipleHardLinks(info) || info.Mode().Perm()&0o077 != 0 ||
		info.Size() != size {
		return Record{}, fmt.Errorf("managed artifact temp file changed before commit")
	}
	if err := temp.Close(); err != nil {
		return Record{}, fmt.Errorf("close managed artifact: %w", err)
	}
	tempOpen = false

	finalName := generationID + extension
	key := path.Join("images", finalName)
	if err := root.Rename(tempName, finalName); err != nil {
		return Record{}, fmt.Errorf("commit managed artifact: %w", err)
	}
	committed = true
	if images, openErr := root.Open("."); openErr == nil {
		_ = images.Sync()
		_ = images.Close()
	}

	record := Record{
		ID: managedID(req.SessionID, key), SessionID: req.SessionID,
		StorageKind: StorageManaged, RelativeKey: key, Title: title,
		Kind: KindImage, MediaType: mediaType, Size: size, Width: width,
		Height: height, SHA256: digest, ProviderID: strings.TrimSpace(req.ProviderID),
		ModelID: strings.TrimSpace(req.ModelID), ParentArtifactID: req.ParentArtifactID,
		OperationID: req.OperationID, ToolCallID: req.ToolCallID, Revision: 1,
		UpdatedAt: s.now().UTC(), Status: StatusAvailable, Focus: req.Focus,
		Shareable: false,
	}
	if err := recorder.RecordArtifact(record); err != nil {
		return record, fmt.Errorf("persist managed artifact metadata: %w", err)
	}

	shard := s.shard(req.SessionID)
	shard.mu.Lock()
	defer shard.mu.Unlock()
	if err := s.hydrateLocked(shard, req.SessionID); err != nil {
		// Metadata is already durable. Keep the new revision in the repairable
		// cache but leave loaded=false so a later read retries the historical load.
		// A projection failure must not turn a completed billable generation into
		// an apparent failure that invites a duplicate request.
		shard.records[record.ID] = record
		return record, nil
	}
	shard.records[record.ID] = record
	return record, nil
}

func defaultManagedRoot() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = os.TempDir()
	}
	return filepath.Join(home, ".jcode", "artifacts")
}

func sessionDirectory(sessionID string) string {
	digest := sha256.Sum256([]byte(sessionID))
	return hex.EncodeToString(digest[:])
}

func managedID(sessionID, key string) string {
	digest := sha256.Sum256([]byte(sessionID + "\x00managed\x00" + key))
	return base64.RawURLEncoding.EncodeToString(digest[:])[:22]
}

func (s *Service) openManagedBase(create bool) (*os.Root, error) {
	if s.managedRoot == "" || !filepath.IsAbs(s.managedRoot) {
		return nil, fmt.Errorf("managed artifact root must be absolute")
	}
	if create {
		if err := os.MkdirAll(s.managedRoot, managedDirMode); err != nil {
			return nil, fmt.Errorf("create managed artifact root: %w", err)
		}
	}
	parent, err := os.OpenRoot(filepath.Dir(s.managedRoot))
	if err != nil {
		return nil, fmt.Errorf("open managed artifact parent: %w", err)
	}
	defer func() { _ = parent.Close() }()
	root, err := openRootDirectory(parent, filepath.Base(s.managedRoot))
	if err != nil {
		return nil, fmt.Errorf("open managed artifact root: %w", err)
	}
	return root, nil
}

func (s *Service) openManagedImagesRoot(sessionID string, create bool) (*os.Root, error) {
	base, err := s.openManagedBase(create)
	if err != nil {
		return nil, err
	}
	defer func() { _ = base.Close() }()
	directory := sessionDirectory(sessionID)
	if create {
		created := false
		if err := base.Mkdir(directory, managedDirMode); err == nil {
			created = true
		} else if !errors.Is(err, os.ErrExist) {
			return nil, fmt.Errorf("create managed session directory: %w", err)
		}
		if created {
			if err := base.Chmod(directory, managedDirMode); err != nil {
				return nil, fmt.Errorf("secure managed session directory: %w", err)
			}
		}
	}
	sessionRoot, err := openRootDirectory(base, directory)
	if err != nil {
		return nil, fmt.Errorf("open managed session directory: %w", err)
	}
	if create {
		created := false
		if err := sessionRoot.Mkdir("images", managedDirMode); err == nil {
			created = true
		} else if !errors.Is(err, os.ErrExist) {
			_ = sessionRoot.Close()
			return nil, fmt.Errorf("create managed images directory: %w", err)
		}
		if created {
			if err := sessionRoot.Chmod("images", managedDirMode); err != nil {
				_ = sessionRoot.Close()
				return nil, fmt.Errorf("secure managed images directory: %w", err)
			}
		}
	}
	imagesRoot, err := openRootDirectory(sessionRoot, "images")
	_ = sessionRoot.Close()
	if err != nil {
		return nil, fmt.Errorf("open managed images directory: %w", err)
	}
	return imagesRoot, nil
}

func openRootDirectory(root *os.Root, name string) (*os.Root, error) {
	info, err := root.Lstat(name)
	if err != nil {
		return nil, fmt.Errorf("inspect managed artifact directory: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return nil, fmt.Errorf("managed artifact directory cannot be a symbolic link")
	}
	if info.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("managed artifact directory must be owner-only")
	}
	child, err := root.OpenRoot(name)
	if err != nil {
		return nil, err
	}
	opened, err := child.Stat(".")
	if err != nil || !opened.IsDir() || !os.SameFile(info, opened) {
		_ = child.Close()
		return nil, fmt.Errorf("managed artifact directory changed while opening")
	}
	return child, nil
}

func validateManagedKey(key string) error {
	if key == "" || strings.Contains(key, "\\") || path.IsAbs(key) || path.Clean(key) != key {
		return fmt.Errorf("managed artifact key is invalid")
	}
	parts := strings.Split(key, "/")
	if len(parts) != 2 || parts[0] != "images" || parts[1] == "" || parts[1] == "." || parts[1] == ".." {
		return fmt.Errorf("managed artifact key is invalid")
	}
	return nil
}

func (s *Service) openManaged(
	ctx context.Context,
	sessionID string,
	record Record,
) (Record, *os.File, error) {
	if err := ctx.Err(); err != nil {
		return record, nil, err
	}
	if record.EffectiveStorageKind() != StorageManaged || record.SessionID != sessionID {
		return record, nil, fmt.Errorf("managed artifact session binding is invalid")
	}
	if err := validateManagedKey(record.RelativeKey); err != nil {
		return record, nil, err
	}
	if record.ID != managedID(sessionID, record.RelativeKey) {
		return record, nil, fmt.Errorf("managed artifact ID binding is invalid")
	}
	root, err := s.openManagedImagesRoot(sessionID, false)
	if err != nil {
		return record, nil, err
	}
	defer func() { _ = root.Close() }()
	fileName := path.Base(record.RelativeKey)
	if err := rejectManagedSymlink(root, fileName); err != nil {
		return record, nil, err
	}
	file, err := root.Open(fileName)
	if err != nil {
		return record, nil, fmt.Errorf("open managed artifact: %w", err)
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return record, nil, fmt.Errorf("stat managed artifact: %w", err)
	}
	lstat, err := root.Lstat(fileName)
	if err != nil || lstat.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() ||
		!os.SameFile(lstat, info) || hasMultipleHardLinks(info) || info.Mode().Perm()&0o077 != 0 {
		_ = file.Close()
		return record, nil, fmt.Errorf("managed artifact must be one unchanged regular file")
	}
	if info.Size() <= 0 || info.Size() > maxManagedImageSize || info.Size() != record.Size {
		_ = file.Close()
		return record, nil, fmt.Errorf("managed artifact size binding is invalid")
	}
	mediaType, extension, width, height, digest, err := verifyManagedFile(file, info.Size())
	if err != nil {
		_ = file.Close()
		return record, nil, err
	}
	if mediaType != record.MediaType || width != record.Width || height != record.Height ||
		!strings.EqualFold(digest, record.SHA256) || path.Ext(record.RelativeKey) != extension {
		_ = file.Close()
		return record, nil, fmt.Errorf("managed artifact metadata binding is invalid")
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		_ = file.Close()
		return record, nil, fmt.Errorf("rewind managed artifact: %w", err)
	}
	record.Status = StatusAvailable
	record.Size = info.Size()
	return record, file, nil
}

func rejectManagedSymlink(root *os.Root, name string) error {
	info, err := root.Lstat(name)
	if err != nil {
		return fmt.Errorf("inspect managed artifact path: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("managed artifact path cannot contain symbolic links")
	}
	return nil
}

func (s *Service) managedAbsolutePath(record Record) string {
	return filepath.Join(s.managedRoot, sessionDirectory(record.SessionID), filepath.FromSlash(record.RelativeKey))
}

func (s *Service) refreshRecordStatus(ctx context.Context, workspace string, record *Record) {
	switch record.EffectiveStorageKind() {
	case StorageWorkspace:
		validated, err := validateWorkspaceFile(workspace, record.RelativePath)
		switch {
		case err == nil:
			record.Status = StatusAvailable
			record.Size = validated.info.Size()
		case errors.Is(err, os.ErrNotExist):
			record.Status = StatusMissing
		default:
			record.Status = StatusError
		}
	case StorageManaged:
		_, file, err := s.openManaged(ctx, record.SessionID, *record)
		switch {
		case err == nil:
			_ = file.Close()
			record.Status = StatusAvailable
		case errors.Is(err, os.ErrNotExist):
			record.Status = StatusMissing
		default:
			record.Status = StatusError
		}
	default:
		record.Status = StatusUnsupported
	}
}

func copyManagedImage(ctx context.Context, destination *os.File, source io.Reader) (int64, string, error) {
	hash := sha256.New()
	buffer := make([]byte, 32<<10)
	var total int64
	emptyReads := 0
	for {
		if err := ctx.Err(); err != nil {
			return 0, "", err
		}
		n, readErr := source.Read(buffer)
		if n > 0 {
			emptyReads = 0
			total += int64(n)
			if total > maxManagedImageSize {
				return 0, "", fmt.Errorf("%w: managed image exceeds %d bytes", ErrTooLarge, maxManagedImageSize)
			}
			if _, err := destination.Write(buffer[:n]); err != nil {
				return 0, "", fmt.Errorf("write managed artifact: %w", err)
			}
			_, _ = hash.Write(buffer[:n])
		} else if readErr == nil {
			emptyReads++
			if emptyReads >= 100 {
				return 0, "", io.ErrNoProgress
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				break
			}
			return 0, "", fmt.Errorf("read managed artifact: %w", readErr)
		}
	}
	if total == 0 {
		return 0, "", fmt.Errorf("managed image is empty")
	}
	return total, hex.EncodeToString(hash.Sum(nil)), nil
}

func inspectManagedImage(file *os.File, size int64) (string, string, int, int, error) {
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return "", "", 0, 0, fmt.Errorf("rewind managed artifact: %w", err)
	}
	data, err := io.ReadAll(io.LimitReader(file, size+1))
	if err != nil {
		return "", "", 0, 0, fmt.Errorf("read managed artifact for validation: %w", err)
	}
	if int64(len(data)) != size {
		return "", "", 0, 0, fmt.Errorf("managed artifact changed during validation")
	}
	mediaType, extension, width, height, err := inspectManagedImageBytes(data)
	if err != nil {
		return "", "", 0, 0, err
	}
	if width <= 0 || height <= 0 || width > maxManagedImageEdge || height > maxManagedImageEdge ||
		int64(width)*int64(height) > maxManagedImagePixels {
		return "", "", 0, 0, fmt.Errorf("managed image dimensions %dx%d exceed safety limits", width, height)
	}
	if mediaType != "image/webp" {
		decoded, format, decodeErr := image.Decode(bytes.NewReader(data))
		if decodeErr != nil {
			return "", "", 0, 0, fmt.Errorf("managed image payload is corrupt: %w", decodeErr)
		}
		bounds := decoded.Bounds()
		if bounds.Dx() != width || bounds.Dy() != height ||
			(mediaType == "image/png" && format != "png") ||
			(mediaType == "image/jpeg" && format != "jpeg") {
			return "", "", 0, 0, fmt.Errorf("managed image decode metadata is inconsistent")
		}
	}
	return mediaType, extension, width, height, nil
}

func verifyManagedFile(file *os.File, size int64) (string, string, int, int, string, error) {
	mediaType, extension, width, height, err := inspectManagedImage(file, size)
	if err != nil {
		return "", "", 0, 0, "", err
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return "", "", 0, 0, "", fmt.Errorf("rewind managed artifact for hashing: %w", err)
	}
	hash := sha256.New()
	written, err := io.Copy(hash, io.LimitReader(file, size+1))
	if err != nil {
		return "", "", 0, 0, "", fmt.Errorf("hash managed artifact: %w", err)
	}
	if written != size {
		return "", "", 0, 0, "", fmt.Errorf("managed artifact changed while hashing")
	}
	return mediaType, extension, width, height, hex.EncodeToString(hash.Sum(nil)), nil
}

func inspectManagedImageBytes(data []byte) (string, string, int, int, error) {
	if len(data) >= 8 && bytes.Equal(data[:8], []byte("\x89PNG\r\n\x1a\n")) {
		cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
		if err != nil || format != "png" {
			return "", "", 0, 0, fmt.Errorf("managed image contains invalid PNG data")
		}
		return "image/png", ".png", cfg.Width, cfg.Height, nil
	}
	if len(data) >= 3 && data[0] == 0xff && data[1] == 0xd8 && data[2] == 0xff {
		cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
		if err != nil || format != "jpeg" {
			return "", "", 0, 0, fmt.Errorf("managed image contains invalid JPEG data")
		}
		return "image/jpeg", ".jpg", cfg.Width, cfg.Height, nil
	}
	if width, height, animated, ok := inspectWebP(data); ok {
		if animated {
			return "", "", 0, 0, fmt.Errorf("animated WebP artifacts are not supported")
		}
		return "image/webp", ".webp", width, height, nil
	}
	return "", "", 0, 0, fmt.Errorf("managed artifacts support only valid JPEG, PNG, or WebP images")
}

func inspectWebP(data []byte) (int, int, bool, bool) {
	if len(data) < 20 || string(data[:4]) != "RIFF" || string(data[8:12]) != "WEBP" {
		return 0, 0, false, false
	}
	declared := int64(binary.LittleEndian.Uint32(data[4:8])) + 8
	if declared != int64(len(data)) {
		return 0, 0, false, false
	}
	var canvasWidth, canvasHeight, imageWidth, imageHeight int
	animated := false
	offset := 12
	for offset+8 <= len(data) {
		kind := string(data[offset : offset+4])
		length := int(binary.LittleEndian.Uint32(data[offset+4 : offset+8]))
		start := offset + 8
		end := start + length
		if length < 0 || end < start || end > len(data) {
			return 0, 0, false, false
		}
		chunk := data[start:end]
		switch kind {
		case "VP8X":
			if len(chunk) < 10 {
				return 0, 0, false, false
			}
			canvasWidth = 1 + int(chunk[4]) + int(chunk[5])<<8 + int(chunk[6])<<16
			canvasHeight = 1 + int(chunk[7]) + int(chunk[8])<<8 + int(chunk[9])<<16
			animated = chunk[0]&0x02 != 0
		case "VP8 ":
			if len(chunk) < 10 || !bytes.Equal(chunk[3:6], []byte{0x9d, 0x01, 0x2a}) {
				return 0, 0, false, false
			}
			imageWidth = int(binary.LittleEndian.Uint16(chunk[6:8]) & 0x3fff)
			imageHeight = int(binary.LittleEndian.Uint16(chunk[8:10]) & 0x3fff)
		case "VP8L":
			if len(chunk) < 5 || chunk[0] != 0x2f {
				return 0, 0, false, false
			}
			imageWidth = 1 + int(chunk[1]) + (int(chunk[2]&0x3f) << 8)
			imageHeight = 1 + int(chunk[2]>>6) + (int(chunk[3]) << 2) + (int(chunk[4]&0x0f) << 10)
		}
		offset = end + length%2
	}
	if offset != len(data) {
		return 0, 0, false, false
	}
	if animated && canvasWidth > 0 && canvasHeight > 0 {
		return canvasWidth, canvasHeight, true, true
	}
	if imageWidth <= 0 || imageHeight <= 0 {
		return 0, 0, false, false
	}
	if canvasWidth > 0 || canvasHeight > 0 {
		if canvasWidth <= 0 || canvasHeight <= 0 || imageWidth > canvasWidth || imageHeight > canvasHeight {
			return 0, 0, false, false
		}
		return canvasWidth, canvasHeight, false, true
	}
	return imageWidth, imageHeight, false, true
}

func validateExpectedImage(req ManagedImageRequest, mediaType string, width, height int, digest string) error {
	if req.ExpectedMediaType != "" && req.ExpectedMediaType != mediaType {
		return fmt.Errorf("managed image media type differs from adapter metadata")
	}
	if req.ExpectedWidth != 0 && req.ExpectedWidth != width {
		return fmt.Errorf("managed image width differs from adapter metadata")
	}
	if req.ExpectedHeight != 0 && req.ExpectedHeight != height {
		return fmt.Errorf("managed image height differs from adapter metadata")
	}
	if req.ExpectedSHA256 != "" && !strings.EqualFold(req.ExpectedSHA256, digest) {
		return fmt.Errorf("managed image digest differs from adapter metadata")
	}
	return nil
}
