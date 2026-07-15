package command

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/cnjack/jcode/internal/computer"
	"github.com/cnjack/jcode/internal/config"
)

const fixtureJSON = `{
  "frontmost": "com.apple.Notes",
  "flip_frontmost_after": 2,
  "flip_to": "com.googlecode.iterm2",
  "apps": [
    {"bundle_id": "com.apple.Notes", "name": "Notes", "running": true},
    {"bundle_id": "com.googlecode.iterm2", "name": "iTerm", "running": true}
  ],
  "trees": {
    "com.apple.Notes": [
      {"id": "1", "role": "window", "name": "Notes", "child_ids": ["2"]},
      {"id": "2", "role": "button", "name": "New Note", "ref": 101}
    ]
  }
}`

// The agent-eval case computer_batch_frontmost_abort depends on the fixture's
// scripted focus steal actually firing. If it silently does not, the case
// passes or fails for the wrong reason and grades nothing.
func TestFixtureFocusStealFiresAndGateStopsBatch(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".jcode", "computer")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "fixture.json"), []byte(fixtureJSON), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{Computer: &config.ComputerConfig{Enabled: true, Backend: "fake"}}
	m := computer.NewManager(computer.FromConfig(cfg.Computer), home)
	installFakeComputerBackend(m, cfg)

	s, err := m.OpenSession(context.Background())
	if err != nil {
		t.Fatalf("OpenSession: %v (the fixture was probably not installed)", err)
	}
	if _, err := s.Open(context.Background(), "com.apple.Notes"); err != nil {
		t.Fatalf("Open: %v", err)
	}

	_, err = s.Act(context.Background(), []computer.ActRequest{
		{Action: "type", Text: "ALPHA"},
		{Action: "type", Text: "BRAVO"},
		{Action: "type", Text: "CHARLIE"},
		{Action: "type", Text: "DELTA"},
		{Action: "type", Text: "ECHO"},
	})
	if err == nil {
		t.Fatal("the batch completed despite a mid-batch focus steal; either the " +
			"fixture hook did not fire or the per-step gate is not running")
	}

	journal, rerr := os.ReadFile(filepath.Join(home, ".jcode", "computer", "actions.jsonl"))
	if rerr != nil {
		t.Fatalf("journal unreadable: %v", rerr)
	}
	got := string(journal)
	for _, want := range []string{"ALPHA", "BRAVO"} {
		if !contains(got, want) {
			t.Errorf("journal is missing %s, which should have landed before the steal:\n%s", want, got)
		}
	}
	for _, bad := range []string{"CHARLIE", "DELTA", "ECHO", "iterm2"} {
		if contains(got, bad) {
			t.Errorf("journal contains %s, which should have been stopped by the gate:\n%s", bad, got)
		}
	}
}

func contains(s, sub string) bool {
	return len(sub) > 0 && len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}
