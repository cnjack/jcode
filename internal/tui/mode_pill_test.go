package tui

import (
	"testing"

	"github.com/cnjack/jcode/internal/mode"
)

// TestSelectorModeRoundTrip verifies the unified pill maps cleanly to and from
// the two low-level TUI fields (tool axis + approval axis).
func TestSelectorModeRoundTrip(t *testing.T) {
	for _, sm := range []mode.SessionMode{mode.Ask, mode.Plan, mode.Autopilot} {
		var m Model
		m.applySelectorMode(sm)
		if got := m.selectorMode(); got != sm {
			t.Errorf("applySelectorMode(%v) then selectorMode()=%v, want %v", sm, got, sm)
		}
	}
}

// TestApplySelectorModeFields pins the exact field mapping each mode produces.
func TestApplySelectorModeFields(t *testing.T) {
	cases := []struct {
		sm       mode.SessionMode
		wantAgnt AgentMode
		wantAprv ApprovalMode
	}{
		{mode.Ask, ModeNormal, ModeManual},
		{mode.Plan, ModePlanning, ModeManual},
		{mode.Autopilot, ModeNormal, ModeAuto},
	}
	for _, c := range cases {
		var m Model
		m.applySelectorMode(c.sm)
		if m.agentMode != c.wantAgnt {
			t.Errorf("%v: agentMode=%v, want %v", c.sm, m.agentMode, c.wantAgnt)
		}
		if m.approvalMode != c.wantAprv {
			t.Errorf("%v: approvalMode=%v, want %v", c.sm, m.approvalMode, c.wantAprv)
		}
	}
}

// TestSelectorCycleViaPill verifies Shift+Tab's cycle order through the pill.
func TestSelectorCycleViaPill(t *testing.T) {
	var m Model // zero value => Ask
	want := []mode.SessionMode{mode.Plan, mode.Autopilot, mode.Ask}
	for i, w := range want {
		next := m.selectorMode().Next()
		m.applySelectorMode(next)
		if m.selectorMode() != w {
			t.Errorf("cycle step %d: got %v, want %v", i, m.selectorMode(), w)
		}
	}
}
