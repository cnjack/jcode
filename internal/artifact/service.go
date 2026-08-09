// Package artifact owns session-scoped deliverables. Workspace artifacts keep
// their content in the registered workspace; managed artifacts keep validated
// content below JCode's private artifact root. Persisted metadata never contains
// an absolute path or file content.
package artifact

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

type Kind string

const (
	KindAuto     Kind = "auto"
	KindText     Kind = "text"
	KindMarkdown Kind = "markdown"
	KindCode     Kind = "code"
	KindHTML     Kind = "html"
	KindImage    Kind = "image"
	KindPDF      Kind = "pdf"
	KindCSV      Kind = "csv"
	KindBinary   Kind = "binary"
)

var ErrTooLarge = errors.New("artifact is too large")

type Status string

const (
	StatusAvailable   Status = "available"
	StatusMissing     Status = "missing"
	StatusUnsupported Status = "unsupported"
	StatusTooLarge    Status = "too_large"
	StatusError       Status = "error"
)

// StorageKind identifies the trusted backend used for an artifact. The empty
// value is deliberately interpreted as workspace so records written by older
// JCode versions retain their original behavior.
type StorageKind string

const (
	StorageWorkspace StorageKind = "workspace"
	StorageManaged   StorageKind = "managed"
)

const (
	MaxInlineTextSize   int64 = 5 << 20
	MaxInlineBinarySize int64 = 25 << 20
	MaxDownloadSize     int64 = 250 << 20
	MaxShareSize        int64 = 25 << 20
	maxTitleRunes             = 200
)

// Record is the metadata persisted in a session entry and exposed to the Web
// UI. It intentionally contains neither an absolute path nor file content.
type Record struct {
	ID               string      `json:"id"`
	SessionID        string      `json:"session_id"`
	StorageKind      StorageKind `json:"storage_kind,omitempty"`
	RelativePath     string      `json:"relative_path,omitempty"`
	RelativeKey      string      `json:"relative_key,omitempty"`
	Title            string      `json:"title"`
	Kind             Kind        `json:"kind"`
	MediaType        string      `json:"media_type"`
	Size             int64       `json:"size"`
	Width            int         `json:"width,omitempty"`
	Height           int         `json:"height,omitempty"`
	SHA256           string      `json:"sha256,omitempty"`
	ProviderID       string      `json:"provider_id,omitempty"`
	ModelID          string      `json:"model_id,omitempty"`
	ParentArtifactID string      `json:"parent_artifact_id,omitempty"`
	OperationID      string      `json:"operation_id,omitempty"`
	ToolCallID       string      `json:"tool_call_id,omitempty"`
	Revision         int         `json:"revision"`
	UpdatedAt        time.Time   `json:"updated_at"`
	Status           Status      `json:"status"`
	// Focus is never omitted: false is a deliberate instruction to keep the
	// Artifact unseen without stealing the active panel, while older clients
	// may treat an absent field as the legacy focus=true behavior.
	Focus     bool `json:"focus"`
	Shareable bool `json:"shareable,omitempty"`
}

// EffectiveStorageKind preserves the v1 contract: a missing storage_kind is a
// workspace record, never a managed key guessed from other fields.
func (r Record) EffectiveStorageKind() StorageKind {
	if r.StorageKind == "" {
		return StorageWorkspace
	}
	return r.StorageKind
}

// EffectiveShareable preserves the v1 workspace contract. Workspace Artifacts
// predate the persisted shareable bit and remain shareable; managed media is
// fail-closed unless a future storage policy explicitly opts it in.
func (r Record) EffectiveShareable() bool {
	if r.EffectiveStorageKind() == StorageWorkspace {
		return true
	}
	return r.Shareable
}

// Ref is the transport-safe, path-free projection returned by tools and live
// events. Content remains accessible only through Service.Open/Resolve.
type Ref struct {
	ID               string      `json:"id"`
	StorageKind      StorageKind `json:"storage_kind"`
	RelativeKey      string      `json:"relative_key,omitempty"`
	Title            string      `json:"title"`
	Kind             Kind        `json:"kind"`
	MediaType        string      `json:"media_type"`
	Size             int64       `json:"size"`
	Width            int         `json:"width,omitempty"`
	Height           int         `json:"height,omitempty"`
	SHA256           string      `json:"sha256,omitempty"`
	ProviderID       string      `json:"provider_id,omitempty"`
	ModelID          string      `json:"model_id,omitempty"`
	ParentArtifactID string      `json:"parent_artifact_id,omitempty"`
	OperationID      string      `json:"operation_id,omitempty"`
	ToolCallID       string      `json:"tool_call_id,omitempty"`
	Shareable        bool        `json:"shareable,omitempty"`
}

func (r Record) Ref() Ref {
	return Ref{
		ID: r.ID, StorageKind: r.EffectiveStorageKind(), RelativeKey: r.RelativeKey, Title: r.Title,
		Kind: r.Kind, MediaType: r.MediaType, Size: r.Size, Width: r.Width,
		Height: r.Height, SHA256: r.SHA256, ProviderID: r.ProviderID, ModelID: r.ModelID,
		ParentArtifactID: r.ParentArtifactID, OperationID: r.OperationID, ToolCallID: r.ToolCallID,
		Shareable: r.EffectiveShareable(),
	}
}

type RegisterRequest struct {
	SessionID    string
	Workspace    string
	RelativePath string
	Title        string
	Kind         Kind
	Focus        bool
}

// Recorder is the durable boundary. Register never publishes a revision until
// this append succeeds.
type Recorder interface {
	RecordArtifact(Record) error
}

type Loader func(sessionID string) ([]Record, error)

type Service struct {
	loader      Loader
	now         func() time.Time
	managedRoot string
	mu          sync.Mutex
	shards      map[string]*sessionShard
}

type sessionShard struct {
	mu      sync.RWMutex
	loaded  bool
	records map[string]Record
}

func NewService(loader Loader, now func() time.Time) *Service {
	return NewServiceWithManagedRoot(loader, now, defaultManagedRoot())
}

// NewServiceWithManagedRoot is intended for isolated runtimes and tests. The
// supplied path is the trusted root itself; callers must not derive it from a
// model or HTTP request.
func NewServiceWithManagedRoot(loader Loader, now func() time.Time, managedRoot string) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{
		loader: loader, now: now, managedRoot: filepath.Clean(managedRoot),
		shards: make(map[string]*sessionShard),
	}
}

func (s *Service) shard(sessionID string) *sessionShard {
	s.mu.Lock()
	defer s.mu.Unlock()
	shard := s.shards[sessionID]
	if shard == nil {
		shard = &sessionShard{records: make(map[string]Record)}
		s.shards[sessionID] = shard
	}
	return shard
}

func (s *Service) hydrateLocked(shard *sessionShard, sessionID string) error {
	if shard.loaded {
		return nil
	}
	if s.loader != nil {
		records, err := s.loader(sessionID)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		for _, record := range records {
			current, exists := shard.records[record.ID]
			if !exists || record.Revision > current.Revision {
				shard.records[record.ID] = record
			}
		}
	}
	shard.loaded = true
	return nil
}

func (s *Service) Register(ctx context.Context, req RegisterRequest, recorder Recorder) (Record, error) {
	if err := ctx.Err(); err != nil {
		return Record{}, err
	}
	if req.SessionID == "" || recorder == nil {
		return Record{}, fmt.Errorf("artifact registration requires a session recorder")
	}
	validated, err := validateWorkspaceFile(req.Workspace, req.RelativePath)
	if err != nil {
		return Record{}, err
	}
	kind, mediaType, err := classifyFile(validated.absolutePath, req.Kind)
	if err != nil {
		return Record{}, err
	}
	title := strings.TrimSpace(req.Title)
	if title == "" {
		title = filepath.Base(validated.relativePath)
	}
	if utf8.RuneCountInString(title) > maxTitleRunes {
		return Record{}, fmt.Errorf("artifact title exceeds %d characters", maxTitleRunes)
	}

	shard := s.shard(req.SessionID)
	shard.mu.Lock()
	defer shard.mu.Unlock()
	if err := s.hydrateLocked(shard, req.SessionID); err != nil {
		return Record{}, fmt.Errorf("load artifact registry: %w", err)
	}
	id := stableID(req.SessionID, validated.relativePath)
	revision := 1
	if current, exists := shard.records[id]; exists {
		revision = current.Revision + 1
	}
	record := Record{
		ID: id, SessionID: req.SessionID, RelativePath: validated.relativePath,
		StorageKind: StorageWorkspace,
		Title:       title, Kind: kind, MediaType: mediaType, Size: validated.info.Size(),
		Revision: revision, UpdatedAt: s.now().UTC(), Status: StatusAvailable, Focus: req.Focus,
		Shareable: true,
	}
	if err := recorder.RecordArtifact(record); err != nil {
		return Record{}, fmt.Errorf("persist artifact metadata: %w", err)
	}
	shard.records[id] = record
	return record, nil
}

func (s *Service) List(ctx context.Context, sessionID, workspace string) ([]Record, error) {
	records, err := s.listRecords(sessionID, false)
	if err != nil {
		return nil, err
	}
	for i := range records {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		s.refreshRecordStatus(ctx, workspace, &records[i])
	}
	sortArtifactRecords(records)
	return records, nil
}

// ListManaged returns only content rooted under JCode's private managed
// storage. It intentionally accepts no workspace path, making it safe for an
// SSH/Docker task whose workspace path belongs to a different host.
func (s *Service) ListManaged(ctx context.Context, sessionID string) ([]Record, error) {
	records, err := s.listRecords(sessionID, true)
	if err != nil {
		return nil, err
	}
	for i := range records {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		s.refreshRecordStatus(ctx, "", &records[i])
	}
	sortArtifactRecords(records)
	return records, nil
}

func (s *Service) listRecords(sessionID string, managedOnly bool) ([]Record, error) {
	if sessionID == "" {
		return nil, fmt.Errorf("session id is required")
	}
	shard := s.shard(sessionID)
	shard.mu.Lock()
	if err := s.hydrateLocked(shard, sessionID); err != nil {
		shard.mu.Unlock()
		return nil, err
	}
	records := make([]Record, 0, len(shard.records))
	for _, record := range shard.records {
		if managedOnly && record.EffectiveStorageKind() != StorageManaged {
			continue
		}
		records = append(records, record)
	}
	shard.mu.Unlock()
	return records, nil
}

func sortArtifactRecords(records []Record) {
	sort.Slice(records, func(i, j int) bool {
		if records[i].UpdatedAt.Equal(records[j].UpdatedAt) {
			return records[i].ID < records[j].ID
		}
		return records[i].UpdatedAt.After(records[j].UpdatedAt)
	})
}

// Resolve revalidates a registered artifact at the time of use and returns its
// server-only absolute path. Callers must never serialize absolutePath.
func (s *Service) Resolve(ctx context.Context, sessionID, workspace, artifactID string) (Record, string, error) {
	if err := ctx.Err(); err != nil {
		return Record{}, "", err
	}
	shard := s.shard(sessionID)
	shard.mu.Lock()
	if err := s.hydrateLocked(shard, sessionID); err != nil {
		shard.mu.Unlock()
		return Record{}, "", err
	}
	record, ok := shard.records[artifactID]
	shard.mu.Unlock()
	if !ok {
		return Record{}, "", os.ErrNotExist
	}
	switch record.EffectiveStorageKind() {
	case StorageWorkspace:
		validated, err := validateWorkspaceFile(workspace, record.RelativePath)
		if err != nil {
			return record, "", err
		}
		record.Size = validated.info.Size()
		record.Status = StatusAvailable
		return record, validated.absolutePath, nil
	case StorageManaged:
		fileRecord, file, err := s.openManaged(ctx, sessionID, record)
		if err != nil {
			return record, "", err
		}
		_ = file.Close()
		return fileRecord, s.managedAbsolutePath(fileRecord), nil
	default:
		return record, "", fmt.Errorf("unsupported artifact storage kind %q", record.StorageKind)
	}
}

// Open returns a read-only file descriptor constrained by os.Root. Unlike a
// validate-then-os.Open sequence, Root.Open prevents a concurrent symlink swap
// from redirecting the read outside the workspace.
func (s *Service) Open(ctx context.Context, sessionID, workspace, artifactID string) (Record, *os.File, error) {
	if err := ctx.Err(); err != nil {
		return Record{}, nil, err
	}
	shard := s.shard(sessionID)
	shard.mu.Lock()
	if err := s.hydrateLocked(shard, sessionID); err != nil {
		shard.mu.Unlock()
		return Record{}, nil, err
	}
	record, ok := shard.records[artifactID]
	shard.mu.Unlock()
	if !ok {
		return Record{}, nil, os.ErrNotExist
	}
	if record.EffectiveStorageKind() == StorageManaged {
		return s.openManaged(ctx, sessionID, record)
	}
	if record.EffectiveStorageKind() != StorageWorkspace {
		return record, nil, fmt.Errorf("unsupported artifact storage kind %q", record.StorageKind)
	}
	if sensitivePath(record.RelativePath) {
		return Record{}, nil, os.ErrNotExist
	}
	validated, err := validateWorkspaceFile(workspace, record.RelativePath)
	if err != nil {
		return Record{}, nil, err
	}
	root, err := os.OpenRoot(workspace)
	if err != nil {
		return Record{}, nil, fmt.Errorf("open artifact workspace: %w", err)
	}
	defer func() { _ = root.Close() }()
	file, err := root.Open(filepath.FromSlash(record.RelativePath))
	if err != nil {
		return Record{}, nil, fmt.Errorf("open artifact file: %w", err)
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return Record{}, nil, fmt.Errorf("stat artifact file: %w", err)
	}
	if !info.Mode().IsRegular() {
		_ = file.Close()
		return Record{}, nil, fmt.Errorf("artifact path must identify a regular file")
	}
	if !os.SameFile(validated.info, info) || hasMultipleHardLinks(info) {
		_ = file.Close()
		return Record{}, nil, fmt.Errorf("artifact path changed or has multiple hard links")
	}
	record.Size = info.Size()
	record.Status = StatusAvailable
	return record, file, nil
}

func stableID(sessionID, relativePath string) string {
	digest := sha256.Sum256([]byte(sessionID + "\x00" + relativePath))
	return base64.RawURLEncoding.EncodeToString(digest[:])[:22]
}

type validatedFile struct {
	relativePath string
	absolutePath string
	info         os.FileInfo
}

func validateWorkspaceFile(workspace, requested string) (validatedFile, error) {
	requested = strings.TrimSpace(requested)
	if workspace == "" || requested == "" || filepath.IsAbs(requested) || strings.Contains(requested, "\\") {
		return validatedFile{}, fmt.Errorf("artifact path must be a relative slash-separated workspace file")
	}
	clean := filepath.Clean(filepath.FromSlash(requested))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return validatedFile{}, fmt.Errorf("artifact path escapes the workspace")
	}
	relativePath := filepath.ToSlash(clean)
	if sensitivePath(relativePath) {
		return validatedFile{}, fmt.Errorf("artifact path is sensitive and cannot be registered")
	}
	canonicalRoot, err := filepath.EvalSymlinks(filepath.Clean(workspace))
	if err != nil {
		return validatedFile{}, fmt.Errorf("resolve artifact workspace: %w", err)
	}
	absolute := filepath.Join(canonicalRoot, clean)
	if err := rejectArtifactSymlinks(canonicalRoot, clean); err != nil {
		return validatedFile{}, err
	}
	canonicalTarget, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return validatedFile{}, fmt.Errorf("resolve artifact file: %w", err)
	}
	rel, err := filepath.Rel(canonicalRoot, canonicalTarget)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return validatedFile{}, fmt.Errorf("artifact path escapes the workspace through a symbolic link")
	}
	if sensitivePath(filepath.ToSlash(rel)) {
		return validatedFile{}, fmt.Errorf("artifact target is sensitive and cannot be registered")
	}
	info, err := os.Stat(canonicalTarget)
	if err != nil {
		return validatedFile{}, fmt.Errorf("stat artifact file: %w", err)
	}
	if !info.Mode().IsRegular() {
		return validatedFile{}, fmt.Errorf("artifact path must identify a regular file")
	}
	if hasMultipleHardLinks(info) {
		return validatedFile{}, fmt.Errorf("artifact files with multiple hard links are not supported")
	}
	return validatedFile{relativePath: relativePath, absolutePath: canonicalTarget, info: info}, nil
}

func rejectArtifactSymlinks(root, relative string) error {
	current := root
	for _, segment := range strings.Split(relative, string(filepath.Separator)) {
		current = filepath.Join(current, segment)
		info, err := os.Lstat(current)
		if err != nil {
			return fmt.Errorf("inspect artifact path: %w", err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("artifact path cannot contain symbolic links")
		}
	}
	return nil
}

func hasMultipleHardLinks(info os.FileInfo) bool {
	if info == nil || info.Sys() == nil {
		return false
	}
	value := reflect.ValueOf(info.Sys())
	if value.Kind() == reflect.Pointer {
		value = value.Elem()
	}
	if !value.IsValid() || value.Kind() != reflect.Struct {
		return false
	}
	links := value.FieldByName("Nlink")
	if !links.IsValid() {
		return false
	}
	switch links.Kind() {
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return links.Uint() > 1
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return links.Int() > 1
	default:
		return false
	}
}

func sensitivePath(relativePath string) bool {
	segments := strings.Split(strings.ToLower(relativePath), "/")
	for _, segment := range segments {
		if segment == ".git" || segment == ".jcode" || segment == ".ssh" {
			return true
		}
	}
	base := segments[len(segments)-1]
	if base == ".env" || strings.HasPrefix(base, ".env.") || base == "id_rsa" || base == "id_ed25519" {
		return true
	}
	switch strings.ToLower(filepath.Ext(base)) {
	case ".pem", ".key", ".p12", ".pfx":
		return true
	default:
		return false
	}
}

func classifyFile(path string, hint Kind) (Kind, string, error) {
	if hint == "" {
		hint = KindAuto
	}
	if !validKind(hint) {
		return "", "", fmt.Errorf("unsupported artifact kind %q", hint)
	}
	ext := strings.ToLower(filepath.Ext(path))
	mediaType := mime.TypeByExtension(ext)
	file, err := os.Open(path)
	if err != nil {
		return "", "", err
	}
	defer func() { _ = file.Close() }()
	var sample [512]byte
	n, readErr := file.Read(sample[:])
	if readErr != nil && !errors.Is(readErr, io.EOF) && n == 0 {
		return "", "", readErr
	}
	if mediaType == "" {
		mediaType = http.DetectContentType(sample[:n])
	}
	detected := kindForExtension(ext, mediaType)
	if hint != KindAuto {
		detected = hint
	}
	return detected, strings.Split(mediaType, ";")[0], nil
}

func validKind(kind Kind) bool {
	switch kind {
	case KindAuto, KindText, KindMarkdown, KindCode, KindHTML, KindImage, KindPDF, KindCSV, KindBinary:
		return true
	default:
		return false
	}
}

func kindForExtension(ext, mediaType string) Kind {
	switch ext {
	case ".md", ".markdown":
		return KindMarkdown
	case ".html", ".htm":
		return KindHTML
	case ".csv", ".tsv":
		return KindCSV
	case ".pdf":
		return KindPDF
	case ".go", ".rs", ".py", ".js", ".jsx", ".ts", ".tsx", ".css", ".scss", ".json", ".yaml", ".yml", ".toml", ".sql", ".sh":
		return KindCode
	}
	if strings.HasPrefix(mediaType, "image/") {
		return KindImage
	}
	if strings.HasPrefix(mediaType, "text/") {
		return KindText
	}
	return KindBinary
}
