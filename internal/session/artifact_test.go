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

func TestRecorderRoundTripsManagedArtifactMetadataWithoutAbsolutePath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	workspace := t.TempDir()
	recorder, err := NewRecorder(workspace, "provider-a", "chat-a")
	if err != nil {
		t.Fatal(err)
	}
	record := artifact.Record{
		ID: "managed-opaque", SessionID: recorder.UUID(), StorageKind: artifact.StorageManaged,
		RelativeKey: "images/generated.png", Title: "Generated", Kind: artifact.KindImage,
		MediaType: "image/png", Size: 123, Width: 20, Height: 10,
		SHA256: strings.Repeat("a", 64), ProviderID: "provider-a", ModelID: "image-a",
		ParentArtifactID: "parent-a", OperationID: "operation-a", ToolCallID: "tool-a",
		Revision: 1, Status: artifact.StatusAvailable, Focus: true, Shareable: false,
	}
	if err := recorder.RecordArtifact(record); err != nil {
		t.Fatal(err)
	}
	records, err := LoadArtifactRecords(recorder.UUID())
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 {
		t.Fatalf("records=%+v", records)
	}
	got := records[0]
	if got.ID != record.ID || got.StorageKind != artifact.StorageManaged ||
		got.RelativeKey != record.RelativeKey || got.RelativePath != "" ||
		got.Width != record.Width || got.Height != record.Height || got.SHA256 != record.SHA256 ||
		got.ProviderID != record.ProviderID || got.ModelID != record.ModelID ||
		got.ParentArtifactID != record.ParentArtifactID || got.OperationID != record.OperationID ||
		got.ToolCallID != record.ToolCallID {
		t.Fatalf("managed record=%+v want=%+v", got, record)
	}
	raw, err := os.ReadFile(filepath.Join(home, ".jcode", "sessions", recorder.UUID()+".json"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	artifactLine := lines[len(lines)-1]
	if strings.Contains(artifactLine, home) || strings.Contains(artifactLine, workspace) || strings.Contains(artifactLine, "absolute_path") {
		t.Fatalf("managed artifact entry leaked an absolute path: %s", artifactLine)
	}
}

func TestLoadLegacyArtifactDefaultsToWorkspaceStorage(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".jcode", "sessions")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	entries := []Entry{
		{Type: EntrySessionStart, UUID: "legacy", Project: "/work", Timestamp: "2026-08-01T00:00:00Z"},
		{Type: EntryArtifact, ArtifactID: "legacy-id", ArtifactPath: "report.md", ArtifactTitle: "Report",
			ArtifactKind: "markdown", ArtifactMediaType: "text/markdown", ArtifactSize: 12,
			ArtifactRevision: 1, Timestamp: "2026-08-01T01:00:00Z"},
	}
	var body strings.Builder
	for _, entry := range entries {
		encoded, err := json.Marshal(entry)
		if err != nil {
			t.Fatal(err)
		}
		body.Write(encoded)
		body.WriteByte('\n')
	}
	if err := os.WriteFile(filepath.Join(dir, "legacy.json"), []byte(body.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	records, err := LoadArtifactRecords("legacy")
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].StorageKind != "" ||
		records[0].EffectiveStorageKind() != artifact.StorageWorkspace || records[0].ID != "legacy-id" {
		t.Fatalf("legacy records=%+v", records)
	}
}
