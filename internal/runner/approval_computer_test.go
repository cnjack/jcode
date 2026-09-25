package runner

import "testing"

// Mirrors approval_browser_test.go. The two problems are the same problem:
// browser origin ↔ app bundle id.

func TestDecideComputerTiers(t *testing.T) {
	s := NewApprovalState("/tmp", false)

	// Read-only tier is handled by noApprovalNeeded, ahead of decideComputer.
	for _, name := range []string{"computer_snapshot", "computer_screenshot", "computer_apps"} {
		if got := s.decide(name, `{}`); got != decisionAutoApprove {
			t.Errorf("%s should auto-approve (read-only tier), got %v", name, got)
		}
	}

	// With no permission hook installed, everything that acts must prompt.
	for _, tc := range []struct{ name, args string }{
		{"computer_open", `{"app":"com.apple.Notes"}`},
		{"computer_act", `{"action":"click","uid":"e1"}`},
	} {
		if got := s.decide(tc.name, tc.args); got != decisionPrompt {
			t.Errorf("%s with no perm hook should prompt, got %v", tc.name, got)
		}
	}
}

func TestDecideComputerAppPermission(t *testing.T) {
	s := NewApprovalState("/tmp", false)
	s.SetComputerPermFunc(func(bundleID, class string) bool {
		return bundleID == "com.apple.Notes" && class == "launch"
	})

	if got := s.decide("computer_open", `{"app":"com.apple.Notes"}`); got != decisionAutoApprove {
		t.Errorf("a pre-approved app should auto-approve on launch, got %v", got)
	}
	if got := s.decide("computer_open", `{"app":"com.apple.Terminal"}`); got != decisionPrompt {
		t.Errorf("a non-pre-approved app must prompt, got %v", got)
	}
}

// computer_act's target apps come from the session's resolution of the args
// (explicit app, or the app whose snapshot minted the uid) — not whichever
// window is frontmost, which during approval is jcode itself.
func TestDecideComputerInteractUsesResolvedTargets(t *testing.T) {
	s := NewApprovalState("/tmp", false)
	var asked []string
	s.SetComputerPermFunc(func(bundleID, class string) bool {
		asked = append(asked, bundleID+"/"+class)
		return bundleID == "com.apple.Notes" && class == "interact"
	})

	// No resolver → unknown app → must prompt, never auto-approve.
	if got := s.decide("computer_act", `{"action":"click"}`); got != decisionPrompt {
		t.Errorf("computer_act with an unknown target must prompt, got %v", got)
	}

	var gotArgs string
	s.SetComputerTargetsFunc(func(args string) []string {
		gotArgs = args
		return []string{"com.apple.Notes"}
	})
	if got := s.decide("computer_act", `{"action":"click","uid":"e1"}`); got != decisionAutoApprove {
		t.Errorf("computer_act on a pre-approved target should auto-approve, got %v", got)
	}
	if gotArgs != `{"action":"click","uid":"e1"}` {
		t.Errorf("the resolver did not receive the tool args: %q", gotArgs)
	}
	if len(asked) == 0 || asked[len(asked)-1] != "com.apple.Notes/interact" {
		t.Errorf("the permission check did not use the resolved target: %v", asked)
	}

	// A different target is a different decision, even with identical args.
	s.SetComputerTargetsFunc(func(string) []string { return []string{"com.googlecode.iterm2"} })
	if got := s.decide("computer_act", `{"action":"click"}`); got != decisionPrompt {
		t.Errorf("computer_act must prompt when the target is not pre-approved, got %v", got)
	}

	// A batch spanning apps is auto-approved only if every app is.
	s.SetComputerTargetsFunc(func(string) []string { return []string{"com.apple.Notes", "com.googlecode.iterm2"} })
	if got := s.decide("computer_act", `{"steps":[]}`); got != decisionPrompt {
		t.Errorf("a batch touching a non-approved app must prompt, got %v", got)
	}
}

// An app we cannot name is an app the user cannot have approved.
func TestDecideComputerEmptyAppNeverPreapproves(t *testing.T) {
	s := NewApprovalState("/tmp", false)
	s.SetComputerPermFunc(func(string, string) bool { return true }) // maximally permissive
	s.SetComputerTargetsFunc(func(string) []string { return nil })

	if got := s.decide("computer_act", `{"action":"click"}`); got != decisionPrompt {
		t.Error("an unresolved target must never pre-approve, even with a permissive hook")
	}
	if got := s.decide("computer_open", `{"app":"  "}`); got != decisionPrompt {
		t.Error("a blank app arg must never pre-approve")
	}
}

func TestDecideComputerIgnoresOtherTools(t *testing.T) {
	s := NewApprovalState("/tmp", false)
	if _, ok := s.decideComputer("execute", `{"command":"ls"}`); ok {
		t.Error("decideComputer claimed a non-computer tool")
	}
	if _, ok := s.decideComputer("browser_act", `{"action":"click"}`); ok {
		t.Error("decideComputer claimed a browser tool")
	}
}
