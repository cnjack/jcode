package session

import (
	"os"
	"path/filepath"
	"testing"

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
	if got := LoadLastSession("/proj/never-saved"); got != "" {
		t.Fatalf("unknown project: expected empty, got %q", got)
	}

	// Overwrite moves the project's pointer.
	SaveLastSession("/proj/a", "22222222-2222-2222-2222-222222222222")
	if got := LoadLastSession("/proj/a"); got != "22222222-2222-2222-2222-222222222222" {
		t.Fatalf("proj/a after overwrite: got %q", got)
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
