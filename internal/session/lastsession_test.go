package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cnjack/jcode/internal/config"
)

// TestLastSessionRoundTrip covers save → load keyed per project.
func TestLastSessionRoundTrip(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	// Nothing recorded yet → empty.
	if got := LoadLastSession("/proj/a"); got != "" {
		t.Fatalf("expected empty before any save, got %q", got)
	}

	// The loader only accepts sessions whose JSONL exists (a "new chat" that
	// was never written must not resurrect), so materialize the session files.
	dir, err := config.SessionsDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"11111111-1111-1111-1111-111111111111", "22222222-2222-2222-2222-222222222222"} {
		if err := os.WriteFile(filepath.Join(dir, id+".json"), []byte("{}\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	SaveLastSession("/proj/a", "11111111-1111-1111-1111-111111111111")
	SaveLastSession("/proj/b", "22222222-2222-2222-2222-222222222222")

	if got := LoadLastSession("/proj/a"); got != "11111111-1111-1111-1111-111111111111" {
		t.Fatalf("proj/a: got %q", got)
	}
	if got := LoadLastSession("/proj/b"); got != "22222222-2222-2222-2222-222222222222" {
		t.Fatalf("proj/b: got %q", got)
	}
	if project, id := LoadMostRecentSession(); project != "/proj/b" || id != "22222222-2222-2222-2222-222222222222" {
		t.Fatalf("recent = (%q, %q), want project b", project, id)
	}
	if got := LoadLastSession("/proj/never-saved"); got != "" {
		t.Fatalf("unknown project: expected empty, got %q", got)
	}

	// Overwrite moves the project's pointer.
	SaveLastSession("/proj/a", "22222222-2222-2222-2222-222222222222")
	if got := LoadLastSession("/proj/a"); got != "22222222-2222-2222-2222-222222222222" {
		t.Fatalf("proj/a after overwrite: got %q", got)
	}
	if project, id := LoadMostRecentSession(); project != "/proj/a" || id != "22222222-2222-2222-2222-222222222222" {
		t.Fatalf("recent after switch-back = (%q, %q), want project a", project, id)
	}
}

func TestMostRecentSessionTracksSwitchBackToExistingProjectEntry(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir, err := config.SessionsDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	const first = "11111111-1111-1111-1111-111111111111"
	const second = "22222222-2222-2222-2222-222222222222"
	for _, id := range []string{first, second} {
		if err := os.WriteFile(filepath.Join(dir, id+".json"), []byte("{}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	SaveLastSession("ssh://root@example.test:22/work", first)
	SaveLastSession("/local/work", second)
	// The project mapping already points to first. Saving it again must still
	// advance the global recent pointer back to the remote workspace.
	SaveLastSession("ssh://root@example.test:22/work", first)
	project, id := LoadMostRecentSession()
	if project != "ssh://root@example.test:22/work" || id != first {
		t.Fatalf("recent = (%q, %q), want remote switch-back", project, id)
	}
}

func TestMostRecentSessionMigratesLegacyPerProjectFile(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir, err := config.SessionsDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	const older = "11111111-1111-1111-1111-111111111111"
	const newer = "22222222-2222-2222-2222-222222222222"
	oldTime := time.Unix(100, 0)
	newTime := time.Unix(200, 0)
	for id, stamp := range map[string]time.Time{older: oldTime, newer: newTime} {
		path := filepath.Join(dir, id+".json")
		if err := os.WriteFile(path, []byte("{}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, stamp, stamp); err != nil {
			t.Fatal(err)
		}
	}
	legacy, err := json.Marshal(lastSessionFile{Projects: map[string]string{
		"ssh://root@example.test:22/work": older,
		"docker://builder/work":           newer,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "last_session.json"), legacy, 0o600); err != nil {
		t.Fatal(err)
	}
	project, id := LoadMostRecentSession()
	if project != "docker://builder/work" || id != newer {
		t.Fatalf("legacy recent = (%q, %q), want newest Docker session", project, id)
	}
}

// TestLastSessionSkipsStaleIDs: a recorded id whose session file disappeared
// (deleted conversation, or an empty chat that never hit disk) loads as "".
func TestLastSessionSkipsStaleIDs(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	SaveLastSession("/proj/a", "33333333-3333-3333-3333-333333333333") // file never created
	if got := LoadLastSession("/proj/a"); got != "" {
		t.Fatalf("stale id: expected empty, got %q", got)
	}
	if project, id := LoadMostRecentSession(); project != "" || id != "" {
		t.Fatalf("stale recent: got (%q, %q)", project, id)
	}
}

// TestLastSessionRejectsBadInput: empty/unsafe values are no-ops, not errors.
func TestLastSessionRejectsBadInput(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	SaveLastSession("", "11111111-1111-1111-1111-111111111111")
	SaveLastSession("/proj/a", "")
	SaveLastSession("/proj/a", "../escape")
	if got := LoadLastSession("/proj/a"); got != "" {
		t.Fatalf("expected empty after bad saves, got %q", got)
	}
	if got := LoadLastSession(""); got != "" {
		t.Fatalf("empty project: expected empty, got %q", got)
	}
}
