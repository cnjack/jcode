package command

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/cloudwego/eino/components/tool"

	"github.com/cnjack/jcode/internal/config"
	"github.com/cnjack/jcode/internal/flow"
	"github.com/cnjack/jcode/internal/session"
	"github.com/cnjack/jcode/internal/skills"
	"github.com/cnjack/jcode/internal/tools"
)

func sessionViewTestBool(v bool) *bool { return &v }

func commandToolInfos(t *testing.T, list []tool.BaseTool) []string {
	t.Helper()
	names := make([]string, 0, len(list))
	for _, item := range list {
		info, err := item.Info(context.Background())
		if err != nil {
			t.Fatalf("tool Info() error = %v", err)
		}
		if info == nil {
			continue
		}
		names = append(names, info.Name)
	}
	return names
}

func hasToolName(names []string, want string) bool {
	for _, name := range names {
		if name == want {
			return true
		}
	}
	return false
}

func sessionViewToolNames() []string {
	return []string{"session_list", "session_search", "session_read"}
}

// TestSessionViewToolsRegistration covers the helper's gate and stable order.
func TestSessionViewToolsRegistration(t *testing.T) {
	env := tools.NewEnv(t.TempDir(), "darwin")
	enabled := &config.Config{}

	got := commandToolInfos(t, sessionViewTools(env, enabled))
	if want := sessionViewToolNames(); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("sessionViewTools() = %v, want %v", got, want)
	}

	cases := []struct {
		name string
		env  *tools.Env
		cfg  *config.Config
	}{
		{name: "explicitly disabled", env: env, cfg: &config.Config{
			SessionView: &config.SessionViewConfig{Enabled: sessionViewTestBool(false)},
		}},
		{name: "nil config fails closed", env: env, cfg: nil},
		{name: "nil env", env: nil, cfg: enabled},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if got := sessionViewTools(tt.env, tt.cfg); got != nil {
				t.Fatalf("sessionViewTools() = %d tools, want nil", len(got))
			}
		})
	}
}

// TestSessionViewToolsWiredInAllTransports is the N14 registration guard.
//
// The TUI builders are exercised for real (they are methods on interactiveState
// and need no transport server). The ACP and Web tool lists are assembled inside
// large per-task closures that require a live model/HTTP server, so for those we
// assert the actual call sites in the transport sources — including the exact
// append target — instead of grepping for a bare name. Together with
// TestSessionViewToolsRegistration (the helper returns nil when disabled) this
// proves the off-switch removes the tools from every transport.
func TestSessionViewToolsWiredInAllTransports(t *testing.T) {
	enabled := &config.Config{}
	disabled := &config.Config{
		SessionView: &config.SessionViewConfig{Enabled: sessionViewTestBool(false)},
	}

	t.Run("tui buildAllTools", func(t *testing.T) {
		names := commandToolInfos(t, newMinimalInteractiveState(t, enabled).buildAllTools())
		for _, want := range sessionViewToolNames() {
			if !hasToolName(names, want) {
				t.Fatalf("buildAllTools() missing %q: %v", want, names)
			}
		}
		names = commandToolInfos(t, newMinimalInteractiveState(t, disabled).buildAllTools())
		for _, unwanted := range sessionViewToolNames() {
			if hasToolName(names, unwanted) {
				t.Fatalf("buildAllTools() still exposes %q when disabled: %v", unwanted, names)
			}
		}
	})

	t.Run("tui buildPlanTools", func(t *testing.T) {
		names := commandToolInfos(t, newMinimalInteractiveState(t, enabled).buildPlanTools())
		for _, want := range sessionViewToolNames() {
			if !hasToolName(names, want) {
				t.Fatalf("buildPlanTools() missing %q: %v", want, names)
			}
		}
		names = commandToolInfos(t, newMinimalInteractiveState(t, disabled).buildPlanTools())
		for _, unwanted := range sessionViewToolNames() {
			if hasToolName(names, unwanted) {
				t.Fatalf("buildPlanTools() still exposes %q when disabled: %v", unwanted, names)
			}
		}
	})

	t.Run("acp and web call sites", func(t *testing.T) {
		assertTransportCallSites(t, "interactive.go", []string{
			"all = append(all, sessionViewTools(s.env, s.cfg)...)",
			"plan = append(plan, sessionViewTools(s.env, s.cfg)...)",
		})
		assertTransportCallSites(t, "acp.go", []string{
			"allTools = append(allTools, sessionViewTools(env, cfg)...)",
			"planTools = append(planTools, sessionViewTools(env, cfg)...)",
		})
		assertTransportCallSites(t, "web.go", []string{
			"all = append(all, sessionViewTools(tenv, agentCfg)...)",
			"plan = append(plan, sessionViewTools(tenv, taskCfg)...)",
		})
	})
}

// assertTransportCallSites requires the exact append statements. A plain name
// grep could pass with the result discarded, so the assertion is on the call.
func assertTransportCallSites(t *testing.T, file string, wantCalls []string) {
	t.Helper()
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatalf("resolve test source path")
	}
	body, err := os.ReadFile(filepath.Join(filepath.Dir(currentFile), file))
	if err != nil {
		t.Fatalf("read %s: %v", file, err)
	}
	source := string(body)
	if got := strings.Count(source, "sessionViewTools("); got != len(wantCalls) {
		t.Fatalf("%s calls sessionViewTools %d times, want %d", file, got, len(wantCalls))
	}
	for _, call := range wantCalls {
		if !strings.Contains(source, call) {
			t.Fatalf("%s is missing the registration %q", file, call)
		}
	}
}

// newMinimalInteractiveState builds just enough of the TUI state for
// buildAllTools/buildPlanTools. No model, recorder, or terminal is involved.
func newMinimalInteractiveState(t *testing.T, cfg *config.Config) *interactiveState {
	t.Helper()
	pwd := t.TempDir()
	env := tools.NewEnv(pwd, "darwin")
	return &interactiveState{
		cfg:         cfg,
		env:         env,
		bgManager:   tools.NewBackgroundManager(env),
		skillLoader: skills.NewLoader(),
		flowLoader:  flow.NewLoader(),
		pwd:         pwd,
		platform:    "darwin",
	}
}

// TestWireSessionCallerKeepsIdentityFromOneSource is the N11 guard: both
// identity closures must report the same recorder, and the closure must read
// the recorder live so a rebound recorder cannot report a stale UUID (R6).
func TestWireSessionCallerKeepsIdentityFromOneSource(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	env := tools.NewEnv(t.TempDir(), "darwin")
	wireSessionCaller(env, nil) // N6: nil recorder must not panic or wire anything
	if env.SessionIDFn != nil || env.SessionCallerFn != nil {
		t.Fatal("nil recorder must leave both identity closures unset")
	}

	project := t.TempDir()
	rec, err := session.NewRecorder(project, "anthropic", "claude-sonnet-4.5")
	if err != nil {
		t.Fatalf("session.NewRecorder: %v", err)
	}
	originalID := rec.UUID()
	wireSessionCaller(env, rec)

	if got, want := env.SessionIDFn(), originalID; got != want {
		t.Fatalf("SessionIDFn() = %q, want %q", got, want)
	}
	caller := env.SessionCallerFn()
	if caller.SessionID != env.SessionIDFn() {
		t.Fatalf("SessionCallerFn().SessionID = %q, SessionIDFn() = %q; both must come from the same recorder",
			caller.SessionID, env.SessionIDFn())
	}
	if caller.Project != project {
		t.Fatalf("SessionCallerFn().Project = %q, want %q", caller.Project, project)
	}

	// A resumed/rebound recorder must be observed live, not captured once.
	resumed := "11111111-2222-4333-8444-555555555555"
	rec.SetUUID(resumed)
	if got := env.SessionCallerFn().SessionID; got != resumed {
		t.Fatalf("SessionCallerFn() did not follow SetUUID: %q, want %q", got, resumed)
	}
	if got := env.SessionIDFn(); got != resumed {
		t.Fatalf("SessionIDFn() did not follow SetUUID: %q, want %q", got, resumed)
	}
}
