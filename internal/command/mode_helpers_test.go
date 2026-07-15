package command

import (
	"testing"

	acp "github.com/coder/acp-go-sdk"

	"github.com/cnjack/jcode/internal/config"
	"github.com/cnjack/jcode/internal/mode"
	"github.com/cnjack/jcode/internal/tui"
)

func TestResolveStartupMode(t *testing.T) {
	cases := []struct {
		name   string
		cfg    *config.Config
		unsafe bool
		want   mode.SessionMode
	}{
		{"unsafe forces full access over config", &config.Config{DefaultMode: "approval"}, true, mode.FullAccess},
		{"unsafe with nil cfg", nil, true, mode.FullAccess},
		{"default_mode plan", &config.Config{DefaultMode: "plan"}, false, mode.Plan},
		{"default_mode auto", &config.Config{DefaultMode: "auto"}, false, mode.Auto},
		{"default_mode full access", &config.Config{DefaultMode: "full_access"}, false, mode.FullAccess},
		{"default_mode wins over auto_approve", &config.Config{DefaultMode: "approval", AutoApprove: true}, false, mode.Approval},
		{"legacy auto_approve fallback", &config.Config{AutoApprove: true}, false, mode.FullAccess},
		{"empty defaults to approval", &config.Config{}, false, mode.Approval},
		{"nil cfg defaults to approval", nil, false, mode.Approval},
	}
	for _, c := range cases {
		if got := resolveStartupMode(c.cfg, c.unsafe); got != c.want {
			t.Errorf("%s: resolveStartupMode()=%v, want %v", c.name, got, c.want)
		}
	}
}

func TestModeAfterToolSwitch(t *testing.T) {
	cases := []struct {
		name    string
		current mode.SessionMode
		newMode tui.AgentMode
		want    mode.SessionMode
	}{
		// An approved plan moves to execution with the full tool set, so the
		// session must not keep claiming the read-only Plan mode.
		{"approved plan starts executing", mode.Plan, tui.ModeExecuting, mode.Approval},
		{"plan reverts to normal after todos done", mode.Plan, tui.ModeNormal, mode.Approval},
		{"entering plan keeps plan", mode.Plan, tui.ModePlanning, mode.Plan},
		// Non-plan modes are chosen by the user and survive a tool-axis switch.
		{"approval survives", mode.Approval, tui.ModeNormal, mode.Approval},
		{"auto survives", mode.Auto, tui.ModeExecuting, mode.Auto},
		{"full access survives", mode.FullAccess, tui.ModeExecuting, mode.FullAccess},
		{"full access survives entering plan", mode.FullAccess, tui.ModePlanning, mode.FullAccess},
	}
	for _, c := range cases {
		if got := modeAfterToolSwitch(c.current, c.newMode); got != c.want {
			t.Errorf("%s: modeAfterToolSwitch(%v, %v)=%v, want %v", c.name, c.current, c.newMode, got, c.want)
		}
	}
}

func TestACPModeID(t *testing.T) {
	cases := map[mode.SessionMode]acp.SessionModeId{
		mode.Approval:   acpModeApproval,
		mode.Plan:       acpModePlan,
		mode.Auto:       acpModeAuto,
		mode.FullAccess: acpModeFullAccess,
	}
	for m, want := range cases {
		if got := acpModeID(m); got != want {
			t.Errorf("acpModeID(%v)=%q, want %q", m, got, want)
		}
	}
}

func TestACPAdvertisedModes(t *testing.T) {
	st := acpModes(acpModeApproval)
	if st.CurrentModeId != acpModeApproval {
		t.Errorf("current=%q, want %q", st.CurrentModeId, acpModeApproval)
	}
	want := []acp.SessionModeId{acpModeApproval, acpModePlan, acpModeAuto, acpModeFullAccess}
	if len(st.AvailableModes) != len(want) {
		t.Fatalf("advertised %d modes, want %d", len(st.AvailableModes), len(want))
	}
	for i, w := range want {
		if st.AvailableModes[i].Id != w {
			t.Errorf("advertised[%d]=%q, want %q", i, st.AvailableModes[i].Id, w)
		}
	}
}
