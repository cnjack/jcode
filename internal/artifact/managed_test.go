package artifact

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestArtifactJSONPreservesExplicitNoFocusInstruction(t *testing.T) {
	encoded, err := json.Marshal(Record{ID: "generated", Focus: false})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(encoded, []byte(`"focus":false`)) {
		t.Fatalf("artifact JSON omitted focus=false: %s", encoded)
	}
}

func TestCreateManagedImageRoundTripUsesPrivateHashedStorage(t *testing.T) {
	root := filepath.Join(t.TempDir(), "artifacts")
	now := time.Date(2026, 8, 8, 10, 0, 0, 0, time.UTC)
	service := NewServiceWithManagedRoot(nil, func() time.Time { return now }, root)
	sink := &recordSink{}
	pixels := managedPNG(t, 3, 2)
	digest := sha256.Sum256(pixels)

	record, err := service.CreateManagedImage(context.Background(), ManagedImageRequest{
		SessionID: "private-session-name", Title: "Quiet desk", Reader: bytes.NewReader(pixels),
		ProviderID: "provider-a", ModelID: "image-a", OperationID: "operation-a",
		ToolCallID: "tool-a", Focus: true, ExpectedMediaType: "image/png",
		ExpectedWidth: 3, ExpectedHeight: 2, ExpectedSHA256: hex.EncodeToString(digest[:]),
	}, sink)
	if err != nil {
		t.Fatal(err)
	}
	if record.ID == "" || record.StorageKind != StorageManaged || record.RelativePath != "" ||
		record.RelativeKey == "" || record.MediaType != "image/png" || record.Width != 3 || record.Height != 2 ||
		record.SHA256 != hex.EncodeToString(digest[:]) || record.Shareable {
		t.Fatalf("record=%+v", record)
	}
	if len(sink.records) != 1 || sink.records[0] != record {
		t.Fatalf("persisted records=%+v", sink.records)
	}
	if strings.Contains(record.RelativeKey, record.SessionID) {
		t.Fatalf("relative key leaked session ID: %q", record.RelativeKey)
	}

	storedPath := filepath.Join(root, sessionDirectory(record.SessionID), filepath.FromSlash(record.RelativeKey))
	if strings.Contains(storedPath, record.SessionID) {
		t.Fatalf("managed path leaked session ID: %q", storedPath)
	}
	assertMode(t, root, 0o700)
	assertMode(t, filepath.Join(root, sessionDirectory(record.SessionID)), 0o700)
	assertMode(t, filepath.Dir(storedPath), 0o700)
	assertMode(t, storedPath, 0o600)

	openedRecord, file, err := service.Open(context.Background(), record.SessionID, "", record.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = file.Close() }()
	opened, err := io.ReadAll(file)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(opened, pixels) || openedRecord.Ref() != record.Ref() {
		t.Fatalf("opened record=%+v bytes=%d", openedRecord, len(opened))
	}
	resolvedRecord, resolvedPath, err := service.Resolve(context.Background(), record.SessionID, "", record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if resolvedRecord.ID != record.ID || resolvedPath != storedPath {
		t.Fatalf("resolved record=%+v path=%q want=%q", resolvedRecord, resolvedPath, storedPath)
	}
	records, err := service.List(context.Background(), record.SessionID, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].Status != StatusAvailable {
		t.Fatalf("records=%+v", records)
	}
}

func TestCreateManagedImagePreservesCommittedFileWhenMetadataAppendFails(t *testing.T) {
	root := filepath.Join(t.TempDir(), "artifacts")
	service := NewServiceWithManagedRoot(nil, time.Now, root)
	sink := &recordSink{err: io.ErrClosedPipe}
	record, err := service.CreateManagedImage(context.Background(), ManagedImageRequest{
		SessionID: "session-a", Reader: bytes.NewReader(managedPNG(t, 1, 1)),
	}, sink)
	if err == nil || record.ID == "" {
		t.Fatalf("record=%+v err=%v", record, err)
	}
	storedPath := filepath.Join(root, sessionDirectory(record.SessionID), filepath.FromSlash(record.RelativeKey))
	if _, statErr := os.Stat(storedPath); statErr != nil {
		t.Fatalf("provider output must survive metadata failure: %v", statErr)
	}
	records, listErr := service.List(context.Background(), record.SessionID, "")
	if listErr != nil {
		t.Fatal(listErr)
	}
	if len(records) != 0 {
		t.Fatalf("failed metadata must not be published: %+v", records)
	}
}

func TestCreateManagedImageDoesNotReportFailureAfterDurableMetadataAppend(t *testing.T) {
	root := filepath.Join(t.TempDir(), "artifacts")
	service := NewServiceWithManagedRoot(func(string) ([]Record, error) {
		return nil, errors.New("transient projection failure")
	}, time.Now, root)
	sink := &recordSink{}
	record, err := service.CreateManagedImage(context.Background(), ManagedImageRequest{
		SessionID: "session-a", Reader: bytes.NewReader(managedPNG(t, 1, 1)),
	}, sink)
	if err != nil {
		t.Fatalf("durably persisted generation was reported failed: %v", err)
	}
	if record.ID == "" || len(sink.records) != 1 || sink.records[0].ID != record.ID {
		t.Fatalf("record=%+v persisted=%+v", record, sink.records)
	}
}

func TestCreateManagedImageRejectsInvalidExpectedAndOversizedContentWithoutTempLeak(t *testing.T) {
	for _, tc := range []struct {
		name    string
		reader  io.Reader
		expect  string
		request func(io.Reader) ManagedImageRequest
	}{
		{
			name: "invalid", reader: strings.NewReader("<html>not an image</html>"), expect: "JPEG, PNG, or WebP",
			request: func(reader io.Reader) ManagedImageRequest {
				return ManagedImageRequest{SessionID: "session-a", Reader: reader}
			},
		},
		{
			name: "metadata mismatch", reader: bytes.NewReader(managedPNG(t, 2, 1)), expect: "width differs",
			request: func(reader io.Reader) ManagedImageRequest {
				return ManagedImageRequest{SessionID: "session-a", Reader: reader, ExpectedWidth: 99}
			},
		},
		{
			name: "oversized", reader: io.LimitReader(zeroReader{}, maxManagedImageSize+1), expect: "exceeds",
			request: func(reader io.Reader) ManagedImageRequest {
				return ManagedImageRequest{SessionID: "session-a", Reader: reader}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "artifacts")
			service := NewServiceWithManagedRoot(nil, time.Now, root)
			_, err := service.CreateManagedImage(context.Background(), tc.request(tc.reader), &recordSink{})
			if err == nil || !strings.Contains(err.Error(), tc.expect) {
				t.Fatalf("error=%v want substring %q", err, tc.expect)
			}
			images := filepath.Join(root, sessionDirectory("session-a"), "images")
			entries, readErr := os.ReadDir(images)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if len(entries) != 0 {
				t.Fatalf("failed write left files: %+v", entries)
			}
		})
	}
}

func TestOpenManagedRejectsTamperingSymlinksHardlinksAndBindingChanges(t *testing.T) {
	newArtifact := func(t *testing.T) (*Service, Record, string, []byte) {
		t.Helper()
		root := filepath.Join(t.TempDir(), "artifacts")
		service := NewServiceWithManagedRoot(nil, time.Now, root)
		pixels := managedPNG(t, 2, 2)
		record, err := service.CreateManagedImage(context.Background(), ManagedImageRequest{
			SessionID: "session-a", Reader: bytes.NewReader(pixels),
		}, &recordSink{})
		if err != nil {
			t.Fatal(err)
		}
		storedPath := filepath.Join(root, sessionDirectory(record.SessionID), filepath.FromSlash(record.RelativeKey))
		return service, record, storedPath, pixels
	}

	t.Run("content hash", func(t *testing.T) {
		service, record, storedPath, pixels := newArtifact(t)
		mutated := append([]byte(nil), pixels...)
		mutated[len(mutated)-1] ^= 0xff
		if err := os.WriteFile(storedPath, mutated, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, file, err := service.Open(context.Background(), record.SessionID, "", record.ID); err == nil {
			_ = file.Close()
			t.Fatal("tampered content was opened")
		}
	})

	t.Run("symlink", func(t *testing.T) {
		service, record, storedPath, pixels := newArtifact(t)
		outside := filepath.Join(t.TempDir(), "outside.png")
		if err := os.WriteFile(outside, pixels, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(storedPath); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, storedPath); err != nil {
			t.Fatal(err)
		}
		if _, file, err := service.Open(context.Background(), record.SessionID, "", record.ID); err == nil {
			_ = file.Close()
			t.Fatal("symlink was opened")
		}
	})

	t.Run("hardlink", func(t *testing.T) {
		service, record, storedPath, _ := newArtifact(t)
		second := storedPath + ".link"
		if err := os.Link(storedPath, second); err != nil {
			t.Skipf("hard links unavailable: %v", err)
		}
		if _, file, err := service.Open(context.Background(), record.SessionID, "", record.ID); err == nil {
			_ = file.Close()
			t.Fatal("multiply-linked file was opened")
		}
	})

	t.Run("record binding", func(t *testing.T) {
		service, record, _, _ := newArtifact(t)
		tampered := record
		tampered.RelativeKey = "images/../outside.png"
		tampered.ID = managedID(tampered.SessionID, tampered.RelativeKey)
		loaded := NewServiceWithManagedRoot(func(string) ([]Record, error) {
			return []Record{tampered}, nil
		}, time.Now, service.managedRoot)
		if _, file, err := loaded.Open(context.Background(), tampered.SessionID, "", tampered.ID); err == nil {
			_ = file.Close()
			t.Fatal("traversal key was opened")
		}
	})
}

func TestManagedCreateRejectsSymlinkedTrustedDirectories(t *testing.T) {
	t.Run("root", func(t *testing.T) {
		parent := t.TempDir()
		outside := filepath.Join(parent, "outside")
		if err := os.Mkdir(outside, 0o755); err != nil {
			t.Fatal(err)
		}
		before, err := os.Stat(outside)
		if err != nil {
			t.Fatal(err)
		}
		root := filepath.Join(parent, "artifacts-link")
		if err := os.Symlink(outside, root); err != nil {
			t.Fatal(err)
		}
		service := NewServiceWithManagedRoot(nil, time.Now, root)
		if _, err := service.CreateManagedImage(context.Background(), ManagedImageRequest{
			SessionID: "session-a", Reader: bytes.NewReader(managedPNG(t, 1, 1)),
		}, &recordSink{}); err == nil {
			t.Fatal("symlinked managed root was accepted")
		}
		info, err := os.Stat(outside)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != before.Mode().Perm() {
			t.Fatalf("rejected symlink target mode changed from %o to %o", before.Mode().Perm(), info.Mode().Perm())
		}
	})

	t.Run("images directory", func(t *testing.T) {
		root := filepath.Join(t.TempDir(), "artifacts")
		if err := os.MkdirAll(filepath.Join(root, sessionDirectory("session-a")), 0o700); err != nil {
			t.Fatal(err)
		}
		outside := t.TempDir()
		images := filepath.Join(root, sessionDirectory("session-a"), "images")
		if err := os.Symlink(outside, images); err != nil {
			t.Fatal(err)
		}
		service := NewServiceWithManagedRoot(nil, time.Now, root)
		if _, err := service.CreateManagedImage(context.Background(), ManagedImageRequest{
			SessionID: "session-a", Reader: bytes.NewReader(managedPNG(t, 1, 1)),
		}, &recordSink{}); err == nil {
			t.Fatal("symlinked images directory was accepted")
		}
		entries, err := os.ReadDir(outside)
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 0 {
			t.Fatalf("write escaped into symlink target: %+v", entries)
		}
	})
}

func TestLegacyRecordWithoutStorageKindRemainsWorkspaceBacked(t *testing.T) {
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "legacy.txt"), []byte("legacy"), 0o600); err != nil {
		t.Fatal(err)
	}
	record := Record{
		ID: "legacy-id", SessionID: "legacy-session", RelativePath: "legacy.txt",
		Title: "Legacy", Kind: KindText, MediaType: "text/plain", Size: 6, Revision: 1,
	}
	service := NewServiceWithManagedRoot(func(string) ([]Record, error) {
		return []Record{record}, nil
	}, time.Now, filepath.Join(t.TempDir(), "managed"))
	if record.EffectiveStorageKind() != StorageWorkspace {
		t.Fatalf("effective storage=%q", record.EffectiveStorageKind())
	}
	opened, file, err := service.Open(context.Background(), record.SessionID, workspace, record.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = file.Close() }()
	if opened.ID != record.ID || opened.StorageKind != "" {
		t.Fatalf("legacy record changed: %+v", opened)
	}
}

func managedPNG(t *testing.T, width, height int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	img.Set(0, 0, color.RGBA{R: 80, G: 160, B: 220, A: 255})
	var output bytes.Buffer
	if err := png.Encode(&output, img); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func assertMode(t *testing.T, name string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(name)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("mode %s=%o want %o", name, got, want)
	}
}

type zeroReader struct{}

func (zeroReader) Read(buffer []byte) (int, error) {
	for index := range buffer {
		buffer[index] = 0
	}
	return len(buffer), nil
}
