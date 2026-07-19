package session

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSessionArtifactsUseOwnerOnlyPermissions(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	sessionsDir := filepath.Join(home, ".jcode", "sessions")
	if err := os.MkdirAll(sessionsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	recorder, err := NewRecorder("/private/project", "provider", "model")
	if err != nil {
		t.Fatal(err)
	}
	recorder.RecordUser("permission test")
	id := recorder.UUID()
	recorder.Close()

	assertPermission(t, sessionsDir, 0o700)
	assertPermission(t, filepath.Join(sessionsDir, id+".json"), 0o600)
	assertPermission(t, filepath.Join(sessionsDir, "session.json"), 0o600)

	SaveLastSession("/private/project", id)
	assertPermission(t, filepath.Join(sessionsDir, "last_session.json"), 0o600)

	// Resuming an older permissive file must tighten it before appending.
	sessionPath := filepath.Join(sessionsDir, id+".json")
	if err := os.Chmod(sessionPath, 0o644); err != nil {
		t.Fatal(err)
	}
	resumed, err := NewRecorder("/private/project", "provider", "model")
	if err != nil {
		t.Fatal(err)
	}
	resumed.SetUUID(id)
	resumed.RecordAssistant("resume")
	resumed.Close()
	assertPermission(t, sessionPath, 0o600)
}

func TestToolObservationRoundTripIsMetadataOnly(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	recorder, err := NewRecorder("/project", "provider", "model")
	if err != nil {
		t.Fatal(err)
	}
	recorder.RecordToolObservation(ToolObservation{
		Kind:              "tool_search",
		ModelRequestSeq:   2,
		QueryMode:         "keyword",
		QueryBytes:        17,
		TermCount:         2,
		RequiredTermCount: 1,
		MaxResults:        5,
		MatchNames:        []string{"mcp__fixture__target"},
		NewMatchNames:     []string{"mcp__fixture__target"},
		Success:           true,
	})
	id := recorder.UUID()
	recorder.Close()

	entries, err := LoadSession(id)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("entry count = %d, want session_start + observation", len(entries))
	}
	entry := entries[1]
	if entry.Type != EntryToolObservation || entry.ToolObservation == nil {
		t.Fatalf("observation entry = %#v", entry)
	}
	if entry.Content != "" || entry.Args != "" || entry.Output != "" || entry.Error != "" {
		t.Fatalf("metadata entry contains payload fields: %#v", entry)
	}
	if entry.ToolObservation.QueryBytes != 17 || entry.ToolObservation.MatchNames[0] != "mcp__fixture__target" {
		t.Fatalf("observation metadata = %#v", entry.ToolObservation)
	}
}

func assertPermission(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("permission %s = %#o, want %#o", path, got, want)
	}
}
