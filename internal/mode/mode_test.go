package mode

import "testing"

func TestStringRoundTrip(t *testing.T) {
	for _, m := range []SessionMode{Approval, Plan, Auto, FullAccess} {
		if got := Parse(m.String()); got != m {
			t.Errorf("round-trip %v: String()=%q Parse()=%v, want %v", m, m.String(), got, m)
		}
	}
}

func TestParse(t *testing.T) {
	cases := map[string]SessionMode{
		"approval":    Approval,
		"plan":        Plan,
		"auto":        Auto,
		"full_access": FullAccess,
		"":            Approval,
		"garbage":     Approval,
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
		{Approval, false, false},
		{Plan, true, false},
		{Auto, false, false},
		{FullAccess, false, true},
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
	want := []SessionMode{Plan, Auto, FullAccess, Approval}
	cur := Approval
	for i, w := range want {
		cur = cur.Next()
		if cur != w {
			t.Errorf("cycle step %d: got %v, want %v", i, cur, w)
		}
	}
}

func TestLabel(t *testing.T) {
	cases := map[SessionMode]string{Approval: "Ask for approval", Plan: "Plan", Auto: "Auto", FullAccess: "Full access"}
	for m, want := range cases {
		if got := m.Label(); got != want {
			t.Errorf("%v.Label()=%q, want %q", m, got, want)
		}
	}
}
