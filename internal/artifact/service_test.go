package artifact

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type recordSink struct {
	records []Record
	err     error
}

func (s *recordSink) RecordArtifact(record Record) error {
	if s.err != nil {
		return s.err
	}
	s.records = append(s.records, record)
	return nil
}

func TestRegisterUsesStableIDAndOnlyAdvancesAfterDurableAppend(t *testing.T) {
	workspace := t.TempDir()
	if err := os.MkdirAll(filepath.Join(workspace, "reports"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "reports", "result.md"), []byte("# one"), 0o600); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	service := NewService(nil, func() time.Time { return now })
	sink := &recordSink{}

	first, err := service.Register(context.Background(), RegisterRequest{
		SessionID: "session-a", Workspace: workspace, RelativePath: "reports/result.md", Focus: true,
	}, sink)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID == "" || first.Revision != 1 || first.Kind != KindMarkdown || first.RelativePath != "reports/result.md" {
		t.Fatalf("first=%+v", first)
	}

	sink.err = errors.New("disk full")
	if _, err := service.Register(context.Background(), RegisterRequest{
		SessionID: "session-a", Workspace: workspace, RelativePath: "reports/result.md", Title: "failed revision",
	}, sink); err == nil {
		t.Fatal("recorder failure must fail registration")
	}
	sink.err = nil
	second, err := service.Register(context.Background(), RegisterRequest{
		SessionID: "session-a", Workspace: workspace, RelativePath: "reports/result.md", Title: "Result",
	}, sink)
	if err != nil {
		t.Fatal(err)
	}
	if second.ID != first.ID || second.Revision != 2 || second.Title != "Result" {
		t.Fatalf("second=%+v first=%+v", second, first)
	}
	if len(sink.records) != 2 {
		t.Fatalf("durable records=%d want 2", len(sink.records))
	}
}

func TestRegisterRejectsWorkspaceEscapeSensitiveAndNonRegularPaths(t *testing.T) {
	workspace := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, ".env"), []byte("TOKEN=secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "secret.txt"), filepath.Join(workspace, "escape.txt")); err != nil {
		t.Fatal(err)
	}
	service := NewService(nil, time.Now)
	for _, relativePath := range []string{"../secret.txt", filepath.Join(outside, "secret.txt"), ".env", "escape.txt", "."} {
		t.Run(relativePath, func(t *testing.T) {
			_, err := service.Register(context.Background(), RegisterRequest{
				SessionID: "session-a", Workspace: workspace, RelativePath: relativePath,
			}, &recordSink{})
			if err == nil {
				t.Fatalf("path %q should be rejected", relativePath)
			}
		})
	}
}

func TestRegisterRejectsLinksToSensitiveWorkspaceFiles(t *testing.T) {
	workspace := t.TempDir()
	secret := filepath.Join(workspace, ".env")
	if err := os.WriteFile(secret, []byte("TOKEN=secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(".env", filepath.Join(workspace, "report.txt")); err != nil {
		t.Fatal(err)
	}
	if _, err := NewService(nil, time.Now).Register(context.Background(), RegisterRequest{
		SessionID: "session-a", Workspace: workspace, RelativePath: "report.txt",
	}, &recordSink{}); err == nil {
		t.Fatal("symlink to a sensitive in-workspace file must be rejected")
	}

	hardlink := filepath.Join(workspace, "hardlink.txt")
	if err := os.Link(secret, hardlink); err != nil {
		t.Skipf("hard links are unavailable: %v", err)
	}
	if _, err := NewService(nil, time.Now).Register(context.Background(), RegisterRequest{
		SessionID: "session-b", Workspace: workspace, RelativePath: "hardlink.txt",
	}, &recordSink{}); err == nil {
		t.Fatal("hard link to a sensitive file must be rejected")
	}
}

func TestRegisterRejectsSymlinksEvenWhenTargetStaysInWorkspace(t *testing.T) {
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "target.txt"), []byte("safe"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("target.txt", filepath.Join(workspace, "report.txt")); err != nil {
		t.Fatal(err)
	}
	if _, err := NewService(nil, time.Now).Register(context.Background(), RegisterRequest{
		SessionID: "session-a", Workspace: workspace, RelativePath: "report.txt",
	}, &recordSink{}); err == nil {
		t.Fatal("artifact paths containing symbolic links must be rejected")
	}
}

func TestListHydratesLatestRevisionAndReportsMissingFiles(t *testing.T) {
	workspace := t.TempDir()
	loaded := []Record{
		{ID: "same", SessionID: "history", RelativePath: "report.csv", Title: "old", Kind: KindCSV, MediaType: "text/csv", Size: 10, Revision: 1},
		{ID: "same", SessionID: "history", RelativePath: "report.csv", Title: "latest", Kind: KindCSV, MediaType: "text/csv", Size: 12, Revision: 2},
	}
	service := NewService(func(sessionID string) ([]Record, error) {
		if sessionID != "history" {
			t.Fatalf("load session=%q", sessionID)
		}
		return loaded, nil
	}, time.Now)

	records, err := service.List(context.Background(), "history", workspace)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].Revision != 2 || records[0].Title != "latest" || records[0].Status != StatusMissing {
		t.Fatalf("records=%+v", records)
	}
}
