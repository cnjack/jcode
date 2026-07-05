package flow

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestBuiltinRepoAuditLoads(t *testing.T) {
	l := NewLoader()
	wf, ok := l.Get("repo-audit")
	if !ok {
		t.Fatal("builtin repo-audit not loaded")
	}
	if wf.Scope != ScopeBuiltin {
		t.Fatalf("scope = %v, want builtin", wf.Scope)
	}
	if len(wf.Meta.Phases) != 3 {
		t.Fatalf("phases = %d, want 3", len(wf.Meta.Phases))
	}
	if wf.Meta.WhenToUse == "" {
		t.Fatal("whenToUse should be parsed")
	}
	// SlashCommands should include /repo-audit.
	found := false
	for _, sc := range l.SlashCommands() {
		if sc.Slash == "/repo-audit" {
			found = true
		}
	}
	if !found {
		t.Fatal("/repo-audit slash command missing")
	}
}

// TestBuiltinRepoAuditRunsEndToEnd runs the real builtin workflow with a fake
// spawn — an in-process e2e that exercises phase()/parallel()/pipeline-free
// fan-out and the .then() mapping in the script, with no LLM.
func TestBuiltinRepoAuditRunsEndToEnd(t *testing.T) {
	l := NewLoader()
	wf, _ := l.Get("repo-audit")

	f := &fakeSpawn{perCall: func(spec AgentSpec) (AgentResult, error) {
		return AgentResult{Text: "findings for " + spec.Label, Tokens: 5}, nil
	}}
	sink := &collectSink{}
	eng := New(f.fn, sink)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	res, err := eng.Run(ctx, wf, RunOptions{Args: map[string]interface{}{"area": "internal/auth"}})
	if err != nil {
		t.Fatalf("run error: %v", err)
	}
	report, _ := res.(string)
	if !strings.Contains(report, "findings for summarize") {
		t.Fatalf("final report unexpected: %q", report)
	}
	// 1 scan + 4 analyze + 1 summarize.
	if f.calls != 6 {
		t.Fatalf("agent calls = %d, want 6", f.calls)
	}
	if sink.agentStarts != 6 || sink.agentDones != 6 {
		t.Fatalf("agent starts/dones = %d/%d, want 6/6", sink.agentStarts, sink.agentDones)
	}
	// Phases: Scan, Analyze, Summarize (each fired once).
	if len(sink.phases) != 3 {
		t.Fatalf("phases = %v, want 3", sink.phases)
	}
}
