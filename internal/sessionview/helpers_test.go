package sessionview

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/cnjack/jcode/internal/config"
	"github.com/cnjack/jcode/internal/session"
)

// setupHome isolates every test from the real ~/.jcode.
func setupHome(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
}

func sessionsDir(t *testing.T) string {
	t.Helper()
	dir, err := config.SessionsDir()
	if err != nil {
		t.Fatalf("SessionsDir: %v", err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir sessions dir: %v", err)
	}
	return dir
}

// writeSessionEntries writes an authentic JSONL session file (one marshalled
// Entry per line).
func writeSessionEntries(t *testing.T, id string, entries []session.Entry) {
	t.Helper()
	var buf bytes.Buffer
	for _, e := range entries {
		raw, err := json.Marshal(e)
		if err != nil {
			t.Fatalf("marshal entry: %v", err)
		}
		buf.Write(raw)
		buf.WriteByte('\n')
	}
	writeRawSession(t, id, buf.Bytes())
}

// writeRawSession writes arbitrary bytes to <sessions>/<id>.json.
func writeRawSession(t *testing.T, id string, data []byte) {
	t.Helper()
	path := filepath.Join(sessionsDir(t), id+".json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write session %s: %v", id, err)
	}
}

type indexPayload struct {
	Sessions map[string][]session.SessionMeta `json:"sessions"`
}

func readIndexMap(t *testing.T) map[string][]session.SessionMeta {
	t.Helper()
	path := filepath.Join(sessionsDir(t), "session.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string][]session.SessionMeta{}
		}
		t.Fatalf("read index: %v", err)
	}
	var payload indexPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("unmarshal index: %v", err)
	}
	if payload.Sessions == nil {
		payload.Sessions = map[string][]session.SessionMeta{}
	}
	return payload.Sessions
}

func writeIndexMap(t *testing.T, sessions map[string][]session.SessionMeta) {
	t.Helper()
	data, err := json.MarshalIndent(indexPayload{Sessions: sessions}, "", "  ")
	if err != nil {
		t.Fatalf("marshal index: %v", err)
	}
	path := filepath.Join(sessionsDir(t), "session.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write index: %v", err)
	}
}

func addIndexMeta(t *testing.T, project string, metas ...session.SessionMeta) {
	t.Helper()
	idx := readIndexMap(t)
	idx[project] = append(idx[project], metas...)
	writeIndexMap(t, idx)
}

func mkStart(id, project string) session.Entry {
	return session.Entry{
		Type:      session.EntrySessionStart,
		UUID:      id,
		Project:   project,
		Provider:  "test-provider",
		Model:     "test-model",
		Timestamp: "2026-10-18T09:00:00+08:00",
	}
}

func mkMeta(id, project, updatedAt string) session.SessionMeta {
	return session.SessionMeta{
		UUID:      id,
		Project:   project,
		Provider:  "test-provider",
		Model:     "test-model",
		StartTime: "2026-10-18T09:00:00+08:00",
		UpdatedAt: updatedAt,
		Title:     "title-" + id,
	}
}

func mkUser(content string) session.Entry {
	return session.Entry{Type: session.EntryUser, Content: content, Timestamp: "2026-10-18T09:00:01+08:00"}
}

func mkAssistant(content string) session.Entry {
	return session.Entry{Type: session.EntryAssistant, Content: content, Timestamp: "2026-10-18T09:00:02+08:00"}
}

func openSessionFile(t *testing.T, id string) *os.File {
	t.Helper()
	f, err := os.Open(filepath.Join(sessionsDir(t), id+".json"))
	if err != nil {
		t.Fatalf("open session %s: %v", id, err)
	}
	t.Cleanup(func() { _ = f.Close() })
	return f
}
