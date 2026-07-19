package skills

import (
	"strings"
	"testing"
)

// The submit-pr builtin skill must be discoverable so the agent can commit,
// push and open a PR when the user triggers it by query (the "Git via skill"
// flow — there is no manual git UI).
func TestSubmitPRSkillLoaded(t *testing.T) {
	l := NewLoader()

	sk := l.Get("submit-pr")
	if sk == nil {
		t.Fatal("submit-pr builtin skill not loaded")
	}
	if !sk.Builtin {
		t.Errorf("submit-pr should be a builtin skill")
	}
	if sk.Description == "" {
		t.Errorf("submit-pr should have a description (used in the system prompt)")
	}

	// Reachable via its slash trigger.
	if bySlash := l.GetBySlash("/submit-pr"); bySlash == nil || bySlash.Name != "submit-pr" {
		t.Fatalf("submit-pr not reachable via /submit-pr slash command")
	}

	// Body content is non-empty (load_skill returns it to the agent).
	if l.GetContent("submit-pr") == "" {
		t.Errorf("submit-pr GetContent returned empty body")
	}
}

func TestBuiltinUISkillsBridgeDeferredToolsWithoutShellFallback(t *testing.T) {
	tests := []struct {
		name string
		want []string
	}{
		{
			name: "computer-use",
			want: []string{
				"`tool_search` is",
				"search once in a separate",
				"tool-call batch",
				"select:computer_open,computer_snapshot,computer_act",
				"Do **not** use `execute`",
				"native-UI control",
			},
		},
		{
			name: "browser-use",
			want: []string{
				"`tool_search` is",
				"search once in a separate",
				"tool-call batch",
				"select:browser_open,browser_snapshot,browser_act,browser_read",
				"Do not use `execute`",
				"browser control is unavailable",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			content, err := builtinFS.ReadFile("builtin/" + tt.name + "/SKILL.md")
			if err != nil {
				t.Fatalf("read builtin skill: %v", err)
			}
			for _, want := range tt.want {
				if !strings.Contains(string(content), want) {
					t.Errorf("builtin %s missing %q", tt.name, want)
				}
			}
		})
	}
}
