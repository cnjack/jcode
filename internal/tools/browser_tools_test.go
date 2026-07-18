package tools

import (
	"context"
	"strings"
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

func TestBrowserPlanSchemasOnlyAdvertiseAllowedOperations(t *testing.T) {
	openSchema, err := browserPlanOpenInfo().ToJSONSchema()
	if err != nil {
		t.Fatal(err)
	}
	if openSchema.Properties.Value("new_tab") != nil {
		t.Fatal("Plan browser_open schema advertises new_tab")
	}

	tabsSchema, err := browserPlanTabsInfo().ToJSONSchema()
	if err != nil {
		t.Fatal(err)
	}
	op := tabsSchema.Properties.Value("op")
	if op == nil {
		t.Fatal("Plan browser_tabs schema is missing op")
	}
	if got := op.Enum; len(got) != 2 || got[0] != "list" || got[1] != "select" {
		t.Fatalf("Plan browser_tabs op enum = %v, want [list select]", got)
	}

	if browserOpenInfoSchema, err := browserOpenInfo().ToJSONSchema(); err != nil {
		t.Fatal(err)
	} else if browserOpenInfoSchema.Properties.Value("new_tab") == nil {
		t.Fatal("normal browser_open schema unexpectedly lost new_tab")
	}
}

func TestBrowserPlanEndpointRejectsDisallowedOperationsBeforeSession(t *testing.T) {
	env := &Env{}
	open := &browserTool{env: env, info: browserPlanOpenInfo(), planOnly: true}
	if _, err := open.InvokableRun(context.Background(), `{"url":"https://example.com","new_tab":true}`); err == nil ||
		!strings.Contains(err.Error(), "not allowed in Plan mode") {
		t.Fatalf("Plan browser_open new_tab error = %v", err)
	}

	tabs := &browserTool{env: env, info: browserPlanTabsInfo(), planOnly: true}
	for _, op := range []string{"new", "claim", "close"} {
		args := `{"op":"` + op + `","tab_id":"tab"}`
		if _, err := tabs.InvokableRun(context.Background(), args); err == nil ||
			!strings.Contains(err.Error(), "not allowed in Plan mode") {
			t.Errorf("Plan browser_tabs op=%s error = %v", op, err)
		}
	}
	for _, args := range []string{`{}`, `{"op":"list"}`, `{"op":"select","tab_id":"tab"}`} {
		if err := validatePlanBrowserCall("browser_tabs", args); err != nil {
			t.Errorf("validatePlanBrowserCall(browser_tabs, %s) error = %v", args, err)
		}
	}

	normal := &browserTool{env: env, info: browserOpenInfo()}
	if _, err := normal.InvokableRun(
		context.Background(), `{"url":"https://example.com","new_tab":true}`,
	); err == nil || strings.Contains(err.Error(), "Plan mode") {
		t.Fatalf("normal browser_open should retain new_tab behavior; error=%v", err)
	}
}
