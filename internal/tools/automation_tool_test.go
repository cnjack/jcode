package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/cnjack/jcode/internal/automation"
)

// newAutomationTestEnv wires an Env to an isolated live store (mirroring the
// web server's shared-store setup).
func newAutomationTestEnv(t *testing.T) (*Env, *automation.Store) {
	t.Helper()
	store, err := automation.NewStoreDir(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	env := NewEnv(t.TempDir(), "darwin/arm64")
	env.AutomationStore = store
	return env, store
}

// The automation_create tool must write through the Env's live store so the
// created automation is immediately visible to the server's in-memory cache,
// REST API, and scheduler — not just persisted to disk via a throwaway store.
func TestAutomationCreateTool_UsesEnvStore(t *testing.T) {
	env, store := newAutomationTestEnv(t)

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

func TestAutomationCreateTool_OnceCadence(t *testing.T) {
	env, store := newAutomationTestEnv(t)
	tl := env.NewAutomationCreateTool()

	// RFC3339 form.
	at := time.Now().Add(2 * time.Hour).Format(time.RFC3339)
	out, err := tl.InvokableRun(context.Background(),
		`{"name":"Reminder","prompt":"check deploy","cadence":"once","at":"`+at+`","project_path":"`+t.TempDir()+`"}`)
	if err != nil {
		t.Fatalf("once RFC3339: %v (%s)", err, out)
	}
	list := store.List()
	if len(list) != 1 || list[0].Trigger.Type != automation.TriggerOnce {
		t.Fatalf("once automation not stored: %+v", list)
	}

	// Local "YYYY-MM-DD HH:MM" form.
	local := time.Now().Add(3 * time.Hour).Format("2006-01-02 15:04")
	out, err = tl.InvokableRun(context.Background(),
		`{"name":"Reminder2","prompt":"p","cadence":"once","at":"`+local+`","project_path":"`+t.TempDir()+`"}`)
	if err != nil {
		t.Fatalf("once local layout: %v (%s)", err, out)
	}

	// Past time must be rejected by the create gate.
	past := time.Now().Add(-time.Hour).Format(time.RFC3339)
	if _, err := tl.InvokableRun(context.Background(),
		`{"name":"Old","prompt":"p","cadence":"once","at":"`+past+`","project_path":"`+t.TempDir()+`"}`); err == nil {
		t.Fatal("past once time must be rejected")
	}
}

func TestAutomationCreateTool_CronCadence(t *testing.T) {
	env, store := newAutomationTestEnv(t)
	tl := env.NewAutomationCreateTool()

	out, err := tl.InvokableRun(context.Background(),
		`{"name":"Weekdays","prompt":"triage","cadence":"cron","cron_expr":"  0   9 * * 1-5  ","project_path":"`+t.TempDir()+`"}`)
	if err != nil {
		t.Fatalf("cron create: %v (%s)", err, out)
	}
	got := store.List()
	if len(got) != 1 {
		t.Fatalf("want 1 automation, got %d", len(got))
	}
	if got[0].Trigger.Expr != "0 9 * * 1-5" {
		t.Fatalf("expr not whitespace-normalized: %q", got[0].Trigger.Expr)
	}

	// Invalid expression is a model-correctable error.
	if _, err := tl.InvokableRun(context.Background(),
		`{"name":"Bad","prompt":"p","cadence":"cron","cron_expr":"* * * *","project_path":"`+t.TempDir()+`"}`); err == nil {
		t.Fatal("invalid cron expression must be rejected")
	}
	// Never-firing expression is rejected.
	if _, err := tl.InvokableRun(context.Background(),
		`{"name":"Bad","prompt":"p","cadence":"cron","cron_expr":"0 0 31 2 *","project_path":"`+t.TempDir()+`"}`); err == nil {
		t.Fatal("never-firing cron expression must be rejected")
	}
}

func TestAutomationListTool(t *testing.T) {
	env, store := newAutomationTestEnv(t)
	tl := env.NewAutomationListTool()

	out, err := tl.InvokableRun(context.Background(), `{}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "automations: 0") {
		t.Fatalf("empty list output unexpected: %q", out)
	}

	_, _ = store.Create(automation.Automation{Name: "Nightly", Prompt: "do things",
		ProjectPath: t.TempDir(), Enabled: true,
		Trigger: automation.Trigger{Type: automation.TriggerSchedule, Cadence: automation.CadenceDaily, Hour: 9}})
	_, _ = store.Create(automation.Automation{Name: "Once", Prompt: "once things",
		ProjectPath: t.TempDir(),
		Trigger:     automation.Trigger{Type: automation.TriggerOnce, At: time.Now().Add(time.Hour).Format(time.RFC3339)}})

	out, err = tl.InvokableRun(context.Background(), `{}`)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"automations: 2", "Nightly", "Daily at 09:00", "enabled: true", "Once", "Once at"} {
		if !strings.Contains(out, want) {
			t.Errorf("list output missing %q:\n%s", want, out)
		}
	}
}

func TestAutomationDeleteTool(t *testing.T) {
	env, store := newAutomationTestEnv(t)
	tl := env.NewAutomationDeleteTool()

	if _, err := tl.InvokableRun(context.Background(), `{"id":"nope"}`); err == nil {
		t.Fatal("delete of unknown id must be an error (model-correctable)")
	}

	a, err := store.Create(automation.Automation{Name: "Kill me", Prompt: "p",
		ProjectPath: t.TempDir(),
		Trigger:     automation.Trigger{Type: automation.TriggerSchedule, Cadence: automation.CadenceDaily, Hour: 9}})
	if err != nil {
		t.Fatal(err)
	}

	// Malformed body / empty id are model-correctable errors.
	if _, err := tl.InvokableRun(context.Background(), `{}`); err == nil {
		t.Fatal("delete without id must error")
	}

	out, err := tl.InvokableRun(context.Background(), mustJSON(t, map[string]string{"id": a.ID}))
	if err != nil {
		t.Fatalf("delete: %v (%s)", err, out)
	}
	if store.Get(a.ID) != nil {
		t.Fatal("automation still present after delete")
	}
	// Run-state is removed with the definition.
	if st := store.State(a.ID); st != (automation.RunState{}) {
		t.Fatalf("run state not cleaned up: %+v", st)
	}
}

func TestParseFlexibleTime(t *testing.T) {
	ref := time.Date(2026, 9, 4, 15, 4, 5, 0, time.Local)
	cases := []struct {
		in    string
		want  time.Time
		exact bool // exact match (local-zone forms); otherwise same wall clock
	}{
		{in: "2026-09-04T15:04:05+08:00", want: ref, exact: false}, // offset form: instant equality via parse
		{in: "2026-09-04 15:04", want: time.Date(2026, 9, 4, 15, 4, 0, 0, time.Local), exact: true},
		{in: "2026-09-04T15:04", want: time.Date(2026, 9, 4, 15, 4, 0, 0, time.Local), exact: true},
		{in: "2026-09-04 15:04:05", want: ref, exact: true},
		{in: "2026-09-04T15:04:05", want: ref, exact: true},
		{in: "2026-09-04T15:04:05.123456+00:00", want: ref.Add(-0), exact: false},
	}
	for _, c := range cases {
		got, err := parseFlexibleTime(c.in)
		if err != nil {
			t.Errorf("parseFlexibleTime(%q): %v", c.in, err)
			continue
		}
		if c.exact && !got.Equal(c.want) {
			t.Errorf("parseFlexibleTime(%q) = %v, want %v", c.in, got, c.want)
		}
	}
	for _, bad := range []string{"", "tomorrow", "2026-13-01 10:00", "15:04"} {
		if _, err := parseFlexibleTime(bad); err == nil {
			t.Errorf("parseFlexibleTime(%q): expected error", bad)
		}
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// The automation_create tool falls back to a fresh store when no live store is
// wired into the Env (CLI/ACP contexts) — it must not nil-panic.
func TestAutomationCreateTool_FreshStoreFallback(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // keep the fallback store out of the real ~/.jcode
	env := NewEnv(t.TempDir(), "darwin/arm64")
	if env.AutomationStore != nil {
		t.Fatal("precondition: no live store")
	}
	tl := env.NewAutomationCreateTool()
	out, err := tl.InvokableRun(context.Background(),
		fmt.Sprintf(`{"name":"Fallback","prompt":"p","cadence":"daily","hour":9,"minute":0,"project_path":%q}`, t.TempDir()))
	if err != nil {
		t.Fatalf("fallback create: %v (%s)", err, out)
	}
}
