package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cnjack/jcode/internal/artifact"
)

func TestRecorderPersistsArtifactMetadataAndMaterializesUnseenSummary(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	workspace := t.TempDir()
	recorder, err := NewRecorder(workspace, "kimi", "kimi-for-coding")
	if err != nil {
		t.Fatal(err)
	}
	record := artifact.Record{
		ID: "opaque-id", SessionID: recorder.UUID(), RelativePath: "reports/result.html",
		Title: "Result", Kind: artifact.KindHTML, MediaType: "text/html", Size: 42,
		Revision: 1, UpdatedAt: time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC),
		Status: artifact.StatusAvailable, Focus: true,
	}
	if err := recorder.RecordArtifact(record); err != nil {
		t.Fatal(err)
	}

	entries, err := LoadSession(recorder.UUID())
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || entries[1].Type != EntryArtifact || entries[1].ArtifactID != record.ID || entries[1].ArtifactPath != record.RelativePath {
		t.Fatalf("entries=%+v", entries)
	}
	raw, err := os.ReadFile(filepath.Join(home, ".jcode", "sessions", recorder.UUID()+".json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"<html>", "file_content", "absolute_path"} {
		if strings.Contains(string(raw), forbidden) {
			t.Fatalf("session entry leaked %q: %s", forbidden, raw)
		}
	}

	metas, err := ListSessions(workspace)
	if err != nil {
		t.Fatal(err)
	}
	if len(metas) != 1 || metas[0].ArtifactCount != 1 || !metas[0].ArtifactUnseen || metas[0].ArtifactUpdatedAt == "" {
		t.Fatalf("metas=%+v", metas)
	}
}

func TestLoadArtifactRecordsIgnoresOlderDuplicateRevision(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".jcode", "sessions")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	lines := []Entry{
		{Type: EntrySessionStart, UUID: "history", Project: "/work", Timestamp: "2026-08-01T00:00:00Z"},
		{Type: EntryArtifact, ArtifactID: "same", ArtifactPath: "a.md", ArtifactTitle: "new", ArtifactKind: "markdown", ArtifactMediaType: "text/markdown", ArtifactSize: 3, ArtifactRevision: 2, Timestamp: "2026-08-01T02:00:00Z"},
		{Type: EntryArtifact, ArtifactID: "same", ArtifactPath: "a.md", ArtifactTitle: "old", ArtifactKind: "markdown", ArtifactMediaType: "text/markdown", ArtifactSize: 2, ArtifactRevision: 1, Timestamp: "2026-08-01T01:00:00Z"},
	}
	var body strings.Builder
	for _, entry := range lines {
		encoded, err := json.Marshal(entry)
		if err != nil {
			t.Fatal(err)
		}
		body.Write(encoded)
		body.WriteByte('\n')
	}
	if err := os.WriteFile(filepath.Join(dir, "history.json"), []byte(body.String()), 0o600); err != nil {
		t.Fatal(err)
	}

	records, err := LoadArtifactRecords("history")
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].Revision != 2 || records[0].Title != "new" {
		t.Fatalf("records=%+v", records)
	}
}
