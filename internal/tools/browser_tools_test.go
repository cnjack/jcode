package tools

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

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

func TestBrowserOpenRejectsUnsafeURLsBeforeSession(t *testing.T) {
	tests := []string{
		`{}`,
		`{"url":""}`,
		`{"url":" example.com "}`,
		`{"url":"example.com/path"}`,
		`{"url":"file:///etc/passwd"}`,
		`{"url":"data:text/plain,hello"}`,
		`{"url":"javascript:alert(1)"}`,
		`{"url":"ftp://example.com/file"}`,
		`{"url":"https:///missing-host"}`,
		`{"url":"https://:443/path"}`,
		`{"url":"https://user@example.com/private"}`,
		`{"url":"https://user:password@example.com/private"}`,
	}
	for _, args := range tests {
		for _, planOnly := range []bool{false, true} {
			endpoint := &browserTool{env: &Env{}, info: browserOpenInfo(), planOnly: planOnly}
			if planOnly {
				endpoint.info = browserPlanOpenInfo()
			}
			_, err := endpoint.InvokableRun(context.Background(), args)
			if err == nil {
				t.Errorf("planOnly=%v args=%s unexpectedly passed", planOnly, args)
				continue
			}
			if strings.Contains(err.Error(), "not available") {
				t.Errorf("planOnly=%v args=%s reached BrowserSession: %v", planOnly, args, err)
			}
			for _, secret := range []string{"/etc/passwd", "password", "javascript:alert"} {
				if strings.Contains(err.Error(), secret) {
					t.Errorf("planOnly=%v error exposed URL material %q: %v", planOnly, secret, err)
				}
			}
		}
	}

	for _, args := range []string{
		`{"url":"http://localhost:8080/path?q=1"}`,
		`{"url":"https://example.com/#fragment"}`,
	} {
		if err := validateBrowserCall("browser_open", args, false); err != nil {
			t.Errorf("normal browser_open rejected %s: %v", args, err)
		}
		if err := validateBrowserCall("browser_open", args, true); err != nil {
			t.Errorf("Plan browser_open rejected %s: %v", args, err)
		}
	}
}

func TestRunBrowserOperationBoundsTimeoutAndRedactsInternalError(t *testing.T) {
	const sensitive = "https://user:password@example.com/private/path"
	_, err := runBrowserOperation(
		context.Background(),
		"browser_snapshot",
		5*time.Millisecond,
		func(operationCtx context.Context) (string, error) {
			<-operationCtx.Done()
			return "", errors.New("backend timeout while reading " + sensitive)
		},
	)
	if err == nil || err.Error() != "browser_snapshot operation timed out" {
		t.Fatalf("timeout error = %v", err)
	}
	if strings.Contains(err.Error(), sensitive) {
		t.Fatalf("timeout error exposed internal URL: %v", err)
	}
}

func TestRunBrowserOperationPreservesParentCancellation(t *testing.T) {
	parent, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := runBrowserOperation(
		parent,
		"browser_read",
		30*time.Second,
		func(operationCtx context.Context) (string, error) {
			<-operationCtx.Done()
			return "", operationCtx.Err()
		},
	)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("parent cancellation error = %v, want context.Canceled", err)
	}
}

func TestRunBrowserOperationNormalizesNestedDeadline(t *testing.T) {
	_, err := runBrowserOperation(
		context.Background(),
		"browser_open",
		30*time.Second,
		func(context.Context) (string, error) {
			return "", errors.Join(errors.New("nested browser deadline"), context.DeadlineExceeded)
		},
	)
	if err == nil || err.Error() != "browser_open operation timed out" {
		t.Fatalf("nested deadline error = %v", err)
	}
}
