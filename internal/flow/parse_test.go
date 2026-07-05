package flow

import (
	"context"
	"strings"
	"testing"
)

func contextBG() context.Context { return context.Background() }

func TestParseMetaEdgeCases(t *testing.T) {
	cases := []struct {
		name     string
		src      string
		wantName string
		wantErr  bool
		phases   int
	}{
		{
			name: "nested objects and arrays",
			src: `export const meta = {
  name: "audit",
  description: "d",
  phases: [{ title: "A", detail: "x" }, { title: "B", detail: "y" }],
};
return 1;`,
			wantName: "audit",
			phases:   2,
		},
		{
			name: "braces inside strings",
			src: `export const meta = {
  name: "tricky",
  description: "has } and { in it",
  whenToUse: "use when {x}",
};`,
			wantName: "tricky",
		},
		{
			name: "line and block comments in meta",
			src: `export const meta = {
  // a comment with } brace
  name: "cmt", /* block } comment */
  description: "d",
};`,
			wantName: "cmt",
		},
		{
			name:    "no meta block",
			src:     `const x = 1; return x;`,
			wantErr: true,
		},
		{
			name:    "meta missing name",
			src:     `export const meta = { description: "d" };`,
			wantErr: true,
		},
		{
			name: "const meta without export",
			src: `const meta = { name: "noexport", description: "d" };
return 1;`,
			wantName: "noexport",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m, err := ParseMeta(c.src)
			if c.wantErr {
				if err == nil {
					t.Fatalf("expected error, got meta %+v", m)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if m.Name != c.wantName {
				t.Fatalf("name = %q, want %q", m.Name, c.wantName)
			}
			if c.phases > 0 && len(m.Phases) != c.phases {
				t.Fatalf("phases = %d, want %d", len(m.Phases), c.phases)
			}
		})
	}
}

func TestValidate(t *testing.T) {
	cases := []struct {
		name    string
		src     string
		wantErr bool
	}{
		{"valid", `export const meta = { name: "ok", description: "d" };
const r = await agent("x");
return r;`, false},
		{"syntax error in body", `export const meta = { name: "bad", description: "d" };
const r = await agent("x"   // missing paren
return r;`, true},
		{"missing meta", `const x = 1; return x;`, true},
		{"meta missing name", `export const meta = { description: "d" };`, true},
		{"top-level return is allowed", `export const meta = { name: "r", description: "d" };
return 42;`, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := Validate(c.src)
			if c.wantErr && err == nil {
				t.Fatalf("expected validation error")
			}
			if !c.wantErr && err != nil {
				t.Fatalf("unexpected validation error: %v", err)
			}
		})
	}
}

func TestParseMetaCannotRunSideEffects(t *testing.T) {
	// ParseMeta must not execute the body; if it did, agent() (undefined in the bare
	// VM) would throw or a side effect would run. A body referencing an undefined
	// host function must still parse meta cleanly.
	src := `export const meta = { name: "safe", description: "d" };
await agent("this must not run during ParseMeta");
throw new Error("body should not execute");`
	m, err := ParseMeta(src)
	if err != nil {
		t.Fatalf("ParseMeta should ignore the body, got: %v", err)
	}
	if m.Name != "safe" {
		t.Fatalf("name = %q, want safe", m.Name)
	}
}

func TestStripExports(t *testing.T) {
	cases := []struct{ in, mustContain, mustNotContain string }{
		{"export const meta = {}", "const meta = {}", "export"},
		{"export let x = 1", "let x = 1", "export"},
		{"export function f(){}", "function f(){}", "export"},
		{"export default foo", "foo", "export"},
		{"  export const y = 2", "const y = 2", "export"},
	}
	for _, c := range cases {
		out := stripExports(c.in)
		if !strings.Contains(out, c.mustContain) {
			t.Errorf("stripExports(%q) = %q, want contains %q", c.in, out, c.mustContain)
		}
		if strings.Contains(out, c.mustNotContain) {
			t.Errorf("stripExports(%q) = %q, still contains %q", c.in, out, c.mustNotContain)
		}
	}
}

func TestWrapSourceTopLevelReturnAndAwait(t *testing.T) {
	// The wrapper must make top-level `return` and top-level `await` legal.
	f := &fakeSpawn{}
	src := `export const meta = { name: "w", description: "d" };
const r = await agent("x");   // top-level await
if (r) return { ok: true };   // top-level return
return { ok: false };`
	res, err := runScript(t, src, f.fn, NopSink{}, RunOptions{})
	if err != nil {
		t.Fatalf("run error: %v", err)
	}
	m, _ := res.(map[string]interface{})
	if m == nil || m["ok"] != true {
		t.Fatalf("result = %v, want {ok:true}", res)
	}
}

func TestNestedWorkflow(t *testing.T) {
	child := Workflow{Source: `export const meta = { name: "child", description: "d" };
const r = await agent("child-work");
return "child:" + r;`, Scope: ScopeUser}
	f := &fakeSpawn{}
	resolver := func(name string) (Workflow, bool) {
		if name == "child" {
			return child, true
		}
		return Workflow{}, false
	}
	parent := `export const meta = { name: "parent", description: "d" };
const c = await workflow("child", { k: 1 });
return { fromChild: c };`
	eng := New(f.fn, NopSink{}, WithResolver(resolver))
	res, err := eng.Run(contextBG(), Workflow{Source: parent, Scope: ScopeInline}, RunOptions{})
	if err != nil {
		t.Fatalf("run error: %v", err)
	}
	m, _ := res.(map[string]interface{})
	if m == nil || m["fromChild"] != "child:ok:child-work" {
		t.Fatalf("result = %v, want child result threaded through", res)
	}
	// 1 parent agent? No — parent only calls workflow(); child calls 1 agent.
	if f.calls != 1 {
		t.Fatalf("agent calls = %d, want 1", f.calls)
	}
}

func TestNestedWorkflowDepthGuard(t *testing.T) {
	// A child that itself calls workflow() must be rejected (one level deep).
	grandchild := Workflow{Source: `export const meta = { name: "gc", description: "d" };
return "gc";`, Scope: ScopeUser}
	child := Workflow{Source: `export const meta = { name: "child", description: "d" };
try { await workflow("gc"); return "no-guard"; } catch (e) { return "guarded:" + String(e); }`, Scope: ScopeUser}
	resolver := func(name string) (Workflow, bool) {
		switch name {
		case "gc":
			return grandchild, true
		case "child":
			return child, true
		}
		return Workflow{}, false
	}
	f := &fakeSpawn{}
	parent := `export const meta = { name: "parent", description: "d" };
return await workflow("child");`
	eng := New(f.fn, NopSink{}, WithResolver(resolver))
	res, err := eng.Run(contextBG(), Workflow{Source: parent, Scope: ScopeInline}, RunOptions{})
	if err != nil {
		t.Fatalf("run error: %v", err)
	}
	s, _ := res.(string)
	if !strings.HasPrefix(s, "guarded:") || !strings.Contains(s, "one level deep") {
		t.Fatalf("result = %q, want depth guard message", s)
	}
}

func TestDeterminismGuardNotBypassableViaClosure(t *testing.T) {
	// Attempt to reach the original Math.random via a saved reference should fail
	// because the guard overwrites the property before the body runs.
	f := &fakeSpawn{}
	src := `export const meta = { name: "bypass", description: "d" };
const r = Math["random"];
return r();`
	_, err := runScript(t, src, f.fn, NopSink{}, RunOptions{})
	if err == nil || !strings.Contains(err.Error(), "not allowed") {
		t.Fatalf("Math['random']() should be guarded, got err=%v", err)
	}
}
