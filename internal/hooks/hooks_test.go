package hooks

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func cfg(hooks map[string][]HookGroup) Config { return Config{Hooks: hooks} }

func group(matcher string, cmds ...HookSpec) HookGroup {
	return HookGroup{Matcher: matcher, Hooks: cmds}
}

func cmd(command string) HookSpec { return HookSpec{Type: "command", Command: command} }

func newTestDispatcher(t *testing.T, c Config) (Dispatcher, string) {
	t.Helper()
	dir := t.TempDir()
	d := NewDispatcher(c, Options{CWD: dir, SessionID: "sess-1"})
	return d, dir
}

func TestMatchesTool(t *testing.T) {
	cases := []struct {
		matcher, tool string
		want          bool
	}{
		{"", "write", true},
		{"*", "anything", true},
		{"write", "write", true},
		{"write|edit", "edit", true},
		{"write|edit", "read", false},
		{"^execute$", "execute", true},
		{"^execute$", "execute_something", false},
		{"mcp__.*", "mcp__github__list", true},
		{"mcp__.*", "read", false},
		{"Bash", "execute", true},       // alias: execute ↔ Bash
		{"Write", "write", true},        // alias: write ↔ Write
		{"read", "write", false},        // matcher miss
		{"[invalid(", "write", false},   // bad regex → no match, no panic
		{"write", "todowrite", false},   // exact-by-default: no substring footgun
		{"read", "todoread", false},     // exact-by-default
		{"read", "browser_read", false}, // exact-by-default
		{"^write$", "todowrite", false}, // explicit anchor also excludes substring
		{"todo.*", "todowrite", true},   // explicit regex still opts in
	}
	for _, c := range cases {
		if got := matchesTool(c.matcher, c.tool); got != c.want {
			t.Errorf("matchesTool(%q,%q)=%v want %v", c.matcher, c.tool, got, c.want)
		}
	}
}

func TestLoadMerge(t *testing.T) {
	home := t.TempDir()
	work := t.TempDir()
	must := func(path, content string) {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	must(filepath.Join(home, "hooks.json"),
		`{"hooks":{"PreToolUse":[{"matcher":"write","hooks":[{"type":"command","command":"echo user"}]}]}}`)
	must(filepath.Join(work, ".jcode", "hooks.json"),
		`{"hooks":{"PreToolUse":[{"matcher":"edit","hooks":[{"type":"command","command":"echo project"}]}],"Stop":[{"hooks":[{"type":"command","command":"echo stop"}]}]}}`)
	must(filepath.Join(work, ".jcode", "hooks.local.json"),
		`{"hooks":{"PreToolUse":[{"matcher":"read","hooks":[{"type":"command","command":"echo local"}]}]}}`)

	// trustProject=true loads all three layers.
	merged, warnings := Load(home, work, true)
	if len(warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", warnings)
	}
	if got := len(merged.Hooks["PreToolUse"]); got != 3 {
		t.Errorf("PreToolUse groups: got %d want 3 (user+project+local append)", got)
	}
	if got := len(merged.Hooks["Stop"]); got != 1 {
		t.Errorf("Stop groups: got %d want 1", got)
	}
	if merged.Empty() {
		t.Error("merged config should not be Empty()")
	}

	// trustProject=false loads only the user layer; project/local are ignored.
	userOnly, _ := Load(home, work, false)
	if got := len(userOnly.Hooks["PreToolUse"]); got != 1 {
		t.Errorf("untrusted load: PreToolUse groups=%d want 1 (user only)", got)
	}
	if len(userOnly.Hooks["Stop"]) != 0 {
		t.Error("untrusted load must not include the project-layer Stop hook")
	}
}

func TestLoadMalformedIsWarnedNotFatal(t *testing.T) {
	home := t.TempDir()
	work := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "hooks.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	merged, warnings := Load(home, work, false)
	if len(warnings) == 0 {
		t.Error("expected a warning for malformed hooks.json")
	}
	if !merged.Empty() {
		t.Error("malformed file should be skipped, leaving config empty")
	}
}

func TestFirePreToolUseDenyExit2(t *testing.T) {
	d, _ := newTestDispatcher(t, cfg(map[string][]HookGroup{
		"PreToolUse": {group("write", cmd("echo blocked >&2; exit 2"))},
	}))
	dec := d.Fire(context.Background(), PreToolUse, Payload{ToolName: "write", ToolInput: json.RawMessage(`{"path":"x"}`)})
	if !dec.Denied() {
		t.Fatal("expected denied")
	}
	if dec.Permission != PermDeny {
		t.Errorf("permission=%q want deny", dec.Permission)
	}
	if dec.Reason != "blocked" {
		t.Errorf("reason=%q want 'blocked' (from stderr)", dec.Reason)
	}
}

func TestFirePreToolUseAllowStdout(t *testing.T) {
	d, _ := newTestDispatcher(t, cfg(map[string][]HookGroup{
		"PreToolUse": {group("write", cmd(`echo '{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"allow"}}'`))},
	}))
	dec := d.Fire(context.Background(), PreToolUse, Payload{ToolName: "write"})
	if dec.Denied() {
		t.Fatal("allow should not be denied")
	}
	if dec.Permission != PermAllow {
		t.Errorf("permission=%q want allow", dec.Permission)
	}
}

func TestFireUpdatedInput(t *testing.T) {
	d, _ := newTestDispatcher(t, cfg(map[string][]HookGroup{
		"PreToolUse": {group("write", cmd(`echo '{"hookSpecificOutput":{"updatedInput":{"path":"safe.txt"}}}'`))},
	}))
	dec := d.Fire(context.Background(), PreToolUse, Payload{ToolName: "write", ToolInput: json.RawMessage(`{"path":"danger.txt"}`)})
	if string(dec.UpdatedInput) != `{"path":"safe.txt"}` {
		t.Errorf("updatedInput=%s want {\"path\":\"safe.txt\"}", dec.UpdatedInput)
	}
}

func TestFirePostToolUseModifiedResult(t *testing.T) {
	d, _ := newTestDispatcher(t, cfg(map[string][]HookGroup{
		"PostToolUse": {group("read", cmd(`echo '{"hookSpecificOutput":{"modifiedResult":"REDACTED"}}'`))},
	}))
	dec := d.Fire(context.Background(), PostToolUse, Payload{ToolName: "read", ToolResponse: "secret"})
	if dec.ModifiedResult == nil || *dec.ModifiedResult != "REDACTED" {
		t.Errorf("modifiedResult=%v want REDACTED", dec.ModifiedResult)
	}
}

func TestFireMatcherMiss(t *testing.T) {
	d, _ := newTestDispatcher(t, cfg(map[string][]HookGroup{
		"PreToolUse": {group("read", cmd("exit 2"))},
	}))
	dec := d.Fire(context.Background(), PreToolUse, Payload{ToolName: "write"})
	if dec.Denied() {
		t.Error("matcher 'read' must not match tool 'write'")
	}
}

func TestFireFoldDenyWins(t *testing.T) {
	d, _ := newTestDispatcher(t, cfg(map[string][]HookGroup{
		"PreToolUse": {
			group("write", cmd("exit 0")),              // allow/neutral
			group("write", cmd("echo no >&2; exit 2")), // deny
		},
	}))
	dec := d.Fire(context.Background(), PreToolUse, Payload{ToolName: "write"})
	if !dec.Denied() {
		t.Error("any deny must fold to denied")
	}
}

func TestFireDenyShortCircuits(t *testing.T) {
	d, dir := newTestDispatcher(t, cfg(map[string][]HookGroup{
		"PreToolUse": {
			group("write", cmd("exit 2")),                  // deny first
			group("write", cmd("echo ran > sentinel.txt")), // must NOT run
		},
	}))
	d.Fire(context.Background(), PreToolUse, Payload{ToolName: "write"})
	if _, err := os.Stat(filepath.Join(dir, "sentinel.txt")); err == nil {
		t.Error("second hook ran despite earlier deny (no short-circuit)")
	}
}

func TestFireTimeoutFailSafe(t *testing.T) {
	d, _ := newTestDispatcher(t, cfg(map[string][]HookGroup{
		"PreToolUse": {group("write", HookSpec{Type: "command", Command: "sleep 3", Timeout: 1})},
	}))
	start := time.Now()
	dec := d.Fire(context.Background(), PreToolUse, Payload{ToolName: "write"})
	if dec.Denied() {
		t.Error("timeout must fail-safe to allow, not deny")
	}
	if elapsed := time.Since(start); elapsed > 2500*time.Millisecond {
		t.Errorf("timeout not enforced, took %s", elapsed)
	}
}

func TestFireStopBlockExit2(t *testing.T) {
	d, _ := newTestDispatcher(t, cfg(map[string][]HookGroup{
		"Stop": {group("", cmd("echo tests failing >&2; exit 2"))},
	}))
	dec := d.Fire(context.Background(), Stop, Payload{StopHookActive: false})
	if !dec.Block {
		t.Error("Stop hook exit 2 should block (force continue)")
	}
	if dec.Reason != "tests failing" {
		t.Errorf("reason=%q", dec.Reason)
	}
}

func TestFireStopBlockContinueFalse(t *testing.T) {
	d, _ := newTestDispatcher(t, cfg(map[string][]HookGroup{
		"Stop": {group("", cmd(`echo '{"continue":false,"reason":"finish tests"}'`))},
	}))
	dec := d.Fire(context.Background(), Stop, Payload{})
	if !dec.Block {
		t.Error("continue:false should block")
	}
	if dec.Reason != "finish tests" {
		t.Errorf("reason=%q want 'finish tests'", dec.Reason)
	}
}

func TestFireNonBlockableExit2Ignored(t *testing.T) {
	d, _ := newTestDispatcher(t, cfg(map[string][]HookGroup{
		"PostToolUse": {group("read", cmd("exit 2"))},
	}))
	dec := d.Fire(context.Background(), PostToolUse, Payload{ToolName: "read"})
	if dec.Denied() || dec.Block {
		t.Error("PostToolUse is non-blockable; exit 2 must not block")
	}
}

func TestFireStdinPayloadDelivered(t *testing.T) {
	d, dir := newTestDispatcher(t, cfg(map[string][]HookGroup{
		"PreToolUse": {group("write", cmd("cat > payload.json"))},
	}))
	d.Fire(context.Background(), PreToolUse, Payload{ToolName: "write", ToolInput: json.RawMessage(`{"path":"a"}`)})
	data, err := os.ReadFile(filepath.Join(dir, "payload.json"))
	if err != nil {
		t.Fatal(err)
	}
	var p Payload
	if err := json.Unmarshal(data, &p); err != nil {
		t.Fatalf("stdin was not valid Payload JSON: %v", err)
	}
	if p.ToolName != "write" || p.HookEventName != "PreToolUse" {
		t.Errorf("payload mismatch: tool=%q event=%q", p.ToolName, p.HookEventName)
	}
}

func TestFireEnvVars(t *testing.T) {
	d, dir := newTestDispatcher(t, cfg(map[string][]HookGroup{
		"PreToolUse": {group("write", cmd(`printf '%s' "$JCODE_TOOL_NAME" > toolname`))},
	}))
	d.Fire(context.Background(), PreToolUse, Payload{ToolName: "write"})
	data, err := os.ReadFile(filepath.Join(dir, "toolname"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "write" {
		t.Errorf("JCODE_TOOL_NAME=%q want write", data)
	}
}

func TestNopDispatcherWhenEmpty(t *testing.T) {
	d := NewDispatcher(Config{}, Options{})
	if d.Configured(PreToolUse) {
		t.Error("empty config should report not configured")
	}
	dec := d.Fire(context.Background(), PreToolUse, Payload{ToolName: "write"})
	if dec.Denied() || dec.Block || dec.UpdatedInput != nil {
		t.Error("nop dispatcher must return zero Decision")
	}
}

func TestParentCancelAborts(t *testing.T) {
	d, _ := newTestDispatcher(t, cfg(map[string][]HookGroup{
		"PreToolUse": {group("write", cmd("exit 2"))},
	}))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	dec := d.Fire(ctx, PreToolUse, Payload{ToolName: "write"})
	if dec.Denied() {
		t.Error("cancelled ctx should abort hook (no-op), not surface a deny")
	}
}
