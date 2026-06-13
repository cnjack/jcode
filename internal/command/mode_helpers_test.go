package command

import (
	"testing"

	acp "github.com/coder/acp-go-sdk"

	"github.com/cnjack/jcode/internal/config"
	"github.com/cnjack/jcode/internal/handler"
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
		{"unsafe forces autopilot over config", &config.Config{DefaultMode: "ask"}, true, mode.Autopilot},
		{"unsafe with nil cfg", nil, true, mode.Autopilot},
		{"default_mode plan", &config.Config{DefaultMode: "plan"}, false, mode.Plan},
		{"default_mode autopilot", &config.Config{DefaultMode: "autopilot"}, false, mode.Autopilot},
		{"default_mode wins over auto_approve", &config.Config{DefaultMode: "ask", AutoApprove: true}, false, mode.Ask},
		{"legacy auto_approve fallback", &config.Config{AutoApprove: true}, false, mode.Autopilot},
		{"empty defaults to ask", &config.Config{}, false, mode.Ask},
		{"nil cfg defaults to ask", nil, false, mode.Ask},
	}
	for _, c := range cases {
		if got := resolveStartupMode(c.cfg, c.unsafe); got != c.want {
			t.Errorf("%s: resolveStartupMode()=%v, want %v", c.name, got, c.want)
		}
	}
}

func TestACPModeID(t *testing.T) {
	cases := map[mode.SessionMode]acp.SessionModeId{
		mode.Ask:       acpModeAsk,
		mode.Plan:      acpModePlan,
		mode.Autopilot: acpModeAutopilot,
	}
	for m, want := range cases {
		if got := acpModeID(m); got != want {
			t.Errorf("acpModeID(%v)=%q, want %q", m, got, want)
		}
	}
	// Legacy "agent" wire id must normalize to the canonical "ask".
	if got := acpModeID(mode.Parse(string(acpModeAgent))); got != acpModeAsk {
		t.Errorf("legacy agent alias normalized to %q, want %q", got, acpModeAsk)
	}
}

func TestACPAdvertisedModes(t *testing.T) {
	st := acpModes(acpModeAsk)
	if st.CurrentModeId != acpModeAsk {
		t.Errorf("current=%q, want %q", st.CurrentModeId, acpModeAsk)
	}
	want := []acp.SessionModeId{acpModeAsk, acpModePlan, acpModeAutopilot}
	if len(st.AvailableModes) != len(want) {
		t.Fatalf("advertised %d modes, want %d", len(st.AvailableModes), len(want))
	}
	for i, w := range want {
		if st.AvailableModes[i].Id != w {
			t.Errorf("advertised[%d]=%q, want %q", i, st.AvailableModes[i].Id, w)
		}
	}
}

func TestSessionModeFrom(t *testing.T) {
	cases := []struct {
		am   tui.AgentMode
		apm  handler.ApprovalMode
		want mode.SessionMode
	}{
		{tui.ModeNormal, handler.ModeManual, mode.Ask},
		{tui.ModeNormal, handler.ModeAuto, mode.Autopilot},
		{tui.ModeExecuting, handler.ModeManual, mode.Ask},     // transient exec, manual → Ask
		{tui.ModeExecuting, handler.ModeAuto, mode.Autopilot}, // transient exec, auto → Autopilot
		{tui.ModePlanning, handler.ModeManual, mode.Plan},     // plan determined by tool axis
		{tui.ModePlanning, handler.ModeAuto, mode.Plan},       // plan wins regardless of approval
	}
	for _, c := range cases {
		if got := sessionModeFrom(c.am, c.apm); got != c.want {
			t.Errorf("sessionModeFrom(%v,%v)=%v, want %v", c.am, c.apm, got, c.want)
		}
	}
}
