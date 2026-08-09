package tui

import (
	"errors"
	"testing"

	"github.com/cnjack/jcode/internal/mode"
)

// TestSelectorModeRoundTrip verifies the unified pill maps cleanly to and from
// the stored session mode.
func TestSelectorModeRoundTrip(t *testing.T) {
	for _, sm := range []mode.SessionMode{mode.Approval, mode.Plan, mode.Auto, mode.FullAccess} {
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
		{mode.Approval, ModeNormal, ModeManual},
		{mode.Plan, ModePlanning, ModeManual},
		{mode.Auto, ModeNormal, ModeManual},
		{mode.FullAccess, ModeNormal, ModeAuto},
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

// TestPromoteToFullAccess verifies the approval dialog's "Approve all" moves
// the pill to Full access and tells the backend, so the pill and the backend
// ApprovalState do not disagree about the session mode afterwards.
func TestPromoteToFullAccess(t *testing.T) {
	var m Model
	m.applySelectorMode(mode.Approval)
	var notified []bool
	m.OnApprovalModeChange = func(enabled bool) error {
		notified = append(notified, enabled)
		return nil
	}

	if err := m.promoteToFullAccess(); err != nil {
		t.Fatal(err)
	}

	if got := m.selectorMode(); got != mode.FullAccess {
		t.Errorf("selectorMode()=%v, want %v", got, mode.FullAccess)
	}
	if m.approvalMode != ModeAuto {
		t.Errorf("approvalMode=%v, want %v", m.approvalMode, ModeAuto)
	}
	if len(notified) != 1 || !notified[0] {
		t.Errorf("OnApprovalModeChange calls=%v, want exactly [true]", notified)
	}
	// The next Shift+Tab must cycle from Full access, not from the stale mode.
	if got := m.selectorMode().Next(); got != mode.Approval {
		t.Errorf("Next() after promote=%v, want %v", got, mode.Approval)
	}
}

// TestPromoteToFullAccessInPlan pins that "Approve all" is one unified durable
// transition: it cannot expose an auto-approval axis behind a stale Plan pill.
func TestPromoteToFullAccessInPlan(t *testing.T) {
	var m Model
	m.applySelectorMode(mode.Plan)

	if err := m.promoteToFullAccess(); err != nil {
		t.Fatal(err)
	}

	if got := m.selectorMode(); got != mode.FullAccess {
		t.Errorf("selectorMode()=%v, want %v", got, mode.FullAccess)
	}
	if m.approvalMode != ModeAuto {
		t.Errorf("approvalMode=%v, want %v", m.approvalMode, ModeAuto)
	}
	if m.agentMode != ModeNormal {
		t.Errorf("agentMode=%v, want %v", m.agentMode, ModeNormal)
	}
}

func TestPromoteToFullAccessFailureKeepsApprovalPendingState(t *testing.T) {
	var m Model
	m.applySelectorMode(mode.Approval)
	m.approvalPending = true
	m.approvalQueue = []ToolApprovalRequestMsg{{Name: "queued"}}
	m.OnApprovalModeChange = func(bool) error { return errors.New("journal unavailable") }

	if err := m.promoteToFullAccess(); err == nil {
		t.Fatal("promotion unexpectedly succeeded")
	}
	if m.selectorMode() != mode.Approval || m.approvalMode != ModeManual || !m.approvalPending {
		t.Fatalf("failed promotion changed selector=%v approval=%v pending=%v",
			m.selectorMode(), m.approvalMode, m.approvalPending)
	}
	if len(m.approvalQueue) != 1 {
		t.Fatalf("failed promotion released queued approvals: %d", len(m.approvalQueue))
	}
}

// TestSelectorCycleViaPill verifies Shift+Tab's cycle order through the pill.
func TestSelectorCycleViaPill(t *testing.T) {
	var m Model // zero value => Approval
	want := []mode.SessionMode{mode.Plan, mode.Auto, mode.FullAccess, mode.Approval}
	for i, w := range want {
		next := m.selectorMode().Next()
		m.applySelectorMode(next)
		if m.selectorMode() != w {
			t.Errorf("cycle step %d: got %v, want %v", i, m.selectorMode(), w)
		}
	}
}
