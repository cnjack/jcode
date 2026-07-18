package tools

import (
	"testing"

	"github.com/cnjack/jcode/internal/browser"
)

func TestBrowserToolsFollowManagerEnabledState(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	env := NewEnv(t.TempDir(), "darwin")
	mgr := browser.NewManager(browser.Config{Backend: "auto"})
	t.Cleanup(func() { _ = mgr.Close() })
	env.Browser = mgr

	if got := len(env.NewBrowserTools()); got != 0 {
		t.Fatalf("disabled browser exposed %d full tools, want 0", got)
	}
	if got := len(env.NewBrowserPlanTools()); got != 0 {
		t.Fatalf("disabled browser exposed %d plan tools, want 0", got)
	}

	mgr.SetConfig(browser.Config{Enabled: true, Backend: "auto"})
	if got := len(env.NewBrowserTools()); got != 7 {
		t.Fatalf("enabled browser exposed %d full tools, want 7", got)
	}
	if got := len(env.NewBrowserPlanTools()); got != 5 {
		t.Fatalf("enabled browser exposed %d plan tools, want 5", got)
	}
}
