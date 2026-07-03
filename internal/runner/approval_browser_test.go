package runner

import "testing"

func TestDecideBrowserTiers(t *testing.T) {
	s := NewApprovalState("/tmp/workdir", false)

	// Read-only tier → auto-approve (via noApprovalNeeded).
	for _, tn := range []string{"browser_snapshot", "browser_screenshot", "browser_read"} {
		if got := s.decide(tn, `{}`); got != decisionAutoApprove {
			t.Errorf("%s: got %v want auto-approve", tn, got)
		}
	}

	// Interaction / navigation / eval → prompt when no site perm.
	for _, tn := range []string{"browser_open", "browser_act", "browser_eval"} {
		if got := s.decide(tn, `{"url":"https://github.com","action":"click"}`); got != decisionPrompt {
			t.Errorf("%s: got %v want prompt", tn, got)
		}
	}

	// tabs list/select → auto; new/claim/close → prompt.
	if got := s.decide("browser_tabs", `{"op":"list"}`); got != decisionAutoApprove {
		t.Errorf("tabs list: got %v want auto", got)
	}
	if got := s.decide("browser_tabs", `{"op":"close","tab_id":"x"}`); got != decisionPrompt {
		t.Errorf("tabs close: got %v want prompt", got)
	}
}

func TestDecideBrowserSitePermission(t *testing.T) {
	s := NewApprovalState("/tmp/workdir", false)
	// Pre-authorize navigation to github.com only.
	s.SetBrowserPermFunc(func(origin, class string) bool {
		return origin == "https://github.com" && class == "navigate"
	})

	if got := s.decide("browser_open", `{"url":"https://github.com/x"}`); got != decisionAutoApprove {
		t.Errorf("preapproved origin: got %v want auto", got)
	}
	if got := s.decide("browser_open", `{"url":"https://evil.com/x"}`); got != decisionPrompt {
		t.Errorf("other origin: got %v want prompt", got)
	}
	// interact class is not pre-approved even for github → prompt.
	if got := s.decide("browser_act", `{"action":"click"}`); got != decisionPrompt {
		t.Errorf("interact not preapproved: got %v want prompt", got)
	}
}

// TestDecideBrowserInteractUsesSessionOrigin guards the fix that browser_act
// scopes its per-site permission by the active tab's origin (from the session),
// not the args (a click carries no URL). Before the fix the origin was hardcoded
// to "" so interact=allow could never take effect.
func TestDecideBrowserInteractUsesSessionOrigin(t *testing.T) {
	s := NewApprovalState("/tmp/workdir", false)
	s.SetBrowserPermFunc(func(origin, class string) bool {
		return origin == "https://app.example.com" && class == "interact"
	})

	// No origin provider → unknown origin → prompt (never accidentally allow).
	if got := s.decide("browser_act", `{"action":"click"}`); got != decisionPrompt {
		t.Errorf("no origin provider: got %v want prompt", got)
	}

	// Active tab is the allowed origin → auto-approve.
	s.SetBrowserOriginFunc(func() string { return "https://app.example.com" })
	if got := s.decide("browser_act", `{"action":"fill","uid":"e3"}`); got != decisionAutoApprove {
		t.Errorf("interact on allowed origin: got %v want auto", got)
	}

	// Active tab is a different origin → prompt.
	s.SetBrowserOriginFunc(func() string { return "https://other.example.com" })
	if got := s.decide("browser_act", `{"action":"click"}`); got != decisionPrompt {
		t.Errorf("interact on other origin: got %v want prompt", got)
	}
}

func TestOriginFromArgs(t *testing.T) {
	cases := map[string]string{
		`{"url":"https://github.com/jack/x"}`: "https://github.com",
		`{"url":"http://localhost:3000"}`:     "http://localhost:3000",
		`{"url":"about:blank"}`:               "",
		`{}`:                                  "",
		`not json`:                            "",
	}
	for in, want := range cases {
		if got := originFromArgs(in, "url"); got != want {
			t.Errorf("originFromArgs(%q)=%q want %q", in, got, want)
		}
	}
}
