package flow

import (
	"context"
	"testing"
	"time"
)

// TestAllBuiltinsParseAndRun loads every embedded builtin workflow and runs it
// end-to-end with a fake spawn. This guards the shipped .js artifacts against
// syntax errors and engine-incompatible constructs.
func TestAllBuiltinsParseAndRun(t *testing.T) {
	l := NewLoader()
	all := l.All()
	if len(all) < 3 {
		t.Fatalf("expected at least 3 builtins (repo-audit, roundtable, pr-review), got %d", len(all))
	}

	// Per-workflow args so those that require input actually do work.
	argsFor := map[string]interface{}{
		"repo-audit": map[string]interface{}{"area": "internal/flow"},
		"roundtable": map[string]interface{}{"question": "Should we cache the model factory?"},
		"pr-review":  map[string]interface{}{},
	}

	for _, wf := range all {
		wf := wf
		t.Run(wf.Meta.Name, func(t *testing.T) {
			f := &fakeSpawn{perCall: func(spec AgentSpec) (AgentResult, error) {
				return AgentResult{Text: "response for " + spec.Label, Tokens: 2}, nil
			}}
			eng := New(f.fn, NopSink{})
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			_, err := eng.Run(ctx, wf, RunOptions{Args: argsFor[wf.Meta.Name]})
			if err != nil {
				t.Fatalf("builtin %q failed to run: %v", wf.Meta.Name, err)
			}
			if f.calls == 0 {
				t.Fatalf("builtin %q spawned no agents", wf.Meta.Name)
			}
		})
	}
}
