package tools

import (
	"context"
	"testing"

	"github.com/cnjack/jcode/internal/automation"
)

// The automation_create tool must write through the Env's live store so the
// created automation is immediately visible to the server's in-memory cache,
// REST API, and scheduler — not just persisted to disk via a throwaway store.
func TestAutomationCreateTool_UsesEnvStore(t *testing.T) {
	store, err := automation.NewStoreDir(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	env := NewEnv(t.TempDir(), "darwin/arm64")
	env.AutomationStore = store

	tl := env.NewAutomationCreateTool()
	out, err := tl.InvokableRun(context.Background(),
		`{"name":"Nightly","prompt":"do the thing","cadence":"daily","hour":9,"minute":0,"project_path":"`+t.TempDir()+`"}`)
	if err != nil {
		t.Fatalf("InvokableRun: %v (%s)", err, out)
	}

	// Visible in the SAME store instance the server would serve from.
	list := store.List()
	if len(list) != 1 {
		t.Fatalf("want 1 automation in the live store, got %d", len(list))
	}
	got := list[0]
	if got.Enabled {
		t.Fatal("agent-created automation must be DISABLED (human-in-the-loop)")
	}
	if got.Source != automation.SourceAgent {
		t.Fatalf("source = %q, want %q", got.Source, automation.SourceAgent)
	}
}
