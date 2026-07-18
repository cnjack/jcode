package tools

import (
	"context"
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
	if got := len(env.NewBrowserTools()); got != 6 {
		t.Fatalf("enabled browser exposed %d full tools, want 6", got)
	}
	if got := len(env.NewBrowserPlanTools()); got != 5 {
		t.Fatalf("enabled browser exposed %d plan tools, want 5", got)
	}

	mgr.SetConfig(browser.Config{Enabled: true, Backend: "auto", DevMode: true})
	if got := len(env.NewBrowserTools()); got != 7 {
		t.Fatalf("developer-mode browser exposed %d full tools, want 7", got)
	}
}

func TestBrowserSessionRejectsCachedSessionAfterDisable(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	env := NewEnv(t.TempDir(), "darwin")
	mgr := browser.NewManager(browser.Config{Enabled: true, Backend: "auto"})
	t.Cleanup(func() { _ = mgr.Close() })
	env.Browser = mgr
	env.browserSession = browser.NewSession(nil)

	mgr.SetConfig(browser.Config{Backend: "auto"})
	if _, err := env.BrowserSession(context.Background()); err == nil {
		t.Fatal("cached browser session remained usable after browser use was disabled")
	}
}

func TestBrowserReadSchemaOnlyAdvertisesSupportedInput(t *testing.T) {
	info := browserReadInfo()
	js, err := info.ToJSONSchema()
	if err != nil {
		t.Fatal(err)
	}
	if js.Properties.Value("limit") == nil {
		t.Fatal("browser_read schema is missing limit")
	}
	if js.Properties.Value("kind") != nil {
		t.Fatal("browser_read schema advertises unsupported console/network kinds")
	}
}
