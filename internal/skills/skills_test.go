package skills

import "testing"

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
