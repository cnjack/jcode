package mode

import "testing"

func TestStringRoundTrip(t *testing.T) {
	for _, m := range []SessionMode{Ask, Plan, Autopilot} {
		if got := Parse(m.String()); got != m {
			t.Errorf("round-trip %v: String()=%q Parse()=%v, want %v", m, m.String(), got, m)
		}
	}
}

func TestParseLegacy(t *testing.T) {
	cases := map[string]SessionMode{
		// canonical
		"ask": Ask, "plan": Plan, "autopilot": Autopilot,
		// legacy tool-axis strings written before the unified selector
		"normal": Ask, "executing": Ask, "planning": Plan,
		// legacy approval-axis / frontend aliases
		"auto": Autopilot, "manual": Ask, "agent": Ask, "build": Ask,
		// unknown / empty fall back to the safe default
		"": Ask, "garbage": Ask,
	}
	for in, want := range cases {
		if got := Parse(in); got != want {
			t.Errorf("Parse(%q)=%v, want %v", in, got, want)
		}
	}
}

func TestDerivedAxes(t *testing.T) {
	cases := []struct {
		m         SessionMode
		isPlan    bool
		autoApprv bool
	}{
		{Ask, false, false},
		{Plan, true, false},
		{Autopilot, false, true},
	}
	for _, c := range cases {
		if c.m.IsPlan() != c.isPlan {
			t.Errorf("%v.IsPlan()=%v, want %v", c.m, c.m.IsPlan(), c.isPlan)
		}
		if c.m.AutoApprove() != c.autoApprv {
			t.Errorf("%v.AutoApprove()=%v, want %v", c.m, c.m.AutoApprove(), c.autoApprv)
		}
	}
}

func TestNextCycle(t *testing.T) {
	want := []SessionMode{Plan, Autopilot, Ask}
	cur := Ask
	for i, w := range want {
		cur = cur.Next()
		if cur != w {
			t.Errorf("cycle step %d: got %v, want %v", i, cur, w)
		}
	}
}

func TestLabel(t *testing.T) {
	cases := map[SessionMode]string{Ask: "Ask", Plan: "Plan", Autopilot: "Autopilot"}
	for m, want := range cases {
		if got := m.Label(); got != want {
			t.Errorf("%v.Label()=%q, want %q", m, got, want)
		}
	}
}
