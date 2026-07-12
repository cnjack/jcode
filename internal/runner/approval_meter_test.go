package runner

import (
	"context"
	"testing"
	"time"

	"github.com/cnjack/jcode/internal/agent"
	"github.com/cnjack/jcode/internal/handler"
)

// waitingStubHandler simulates a user sitting at the approval prompt: it
// sleeps for `wait`, records the request it saw, then answers with `resp`.
type waitingStubHandler struct {
	stubHandler
	wait time.Duration
	got  *handler.ApprovalRequest
}

func (h *waitingStubHandler) RequestApproval(_ context.Context, req handler.ApprovalRequest) (handler.ApprovalResponse, error) {
	if h.got != nil {
		*h.got = req
	}
	time.Sleep(h.wait)
	return h.resp, nil
}

func TestApprovalMeter_RecordAndTake(t *testing.T) {
	m := newApprovalMeter()

	// Unknown call → zero outcome.
	if wait, denied := m.take("missing"); wait != 0 || denied {
		t.Fatalf("take(missing) = (%v, %v), want (0, false)", wait, denied)
	}

	// Multiple records accumulate wait; denied is sticky.
	m.record("tc1", 30*time.Millisecond, false)
	m.record("tc1", 20*time.Millisecond, true)
	m.record("tc1", 10*time.Millisecond, false)
	wait, denied := m.take("tc1")
	if wait != 60*time.Millisecond {
		t.Errorf("accumulated wait = %v, want 60ms", wait)
	}
	if !denied {
		t.Error("denied should be sticky once recorded")
	}
	// take removes the entry.
	if wait, denied := m.take("tc1"); wait != 0 || denied {
		t.Errorf("second take = (%v, %v), want (0, false)", wait, denied)
	}

	// Empty ids are ignored (calls outside the middleware chain).
	m.record("", time.Second, true)
	if wait, denied := m.take(""); wait != 0 || denied {
		t.Errorf("empty-id take = (%v, %v), want (0, false)", wait, denied)
	}
}

func TestApplyApprovalOutcome_SubtractsWait(t *testing.T) {
	m := newApprovalMeter()
	m.record("tc1", 60*time.Millisecond, false)

	ev := handler.ToolResultEvent{ToolCallID: "tc1", Duration: 100 * time.Millisecond}
	applyApprovalOutcome(&ev, m)
	if ev.Duration != 40*time.Millisecond {
		t.Errorf("Duration = %v, want 40ms (100ms minus 60ms approval wait)", ev.Duration)
	}
	if ev.Denied {
		t.Error("Denied should stay false for an approved call")
	}
}

func TestApplyApprovalOutcome_ClampsAtZeroAndSetsDenied(t *testing.T) {
	m := newApprovalMeter()
	m.record("tc1", 500*time.Millisecond, true)

	ev := handler.ToolResultEvent{ToolCallID: "tc1", Duration: 100 * time.Millisecond}
	applyApprovalOutcome(&ev, m)
	if ev.Duration != 0 {
		t.Errorf("Duration = %v, want 0 (wait exceeds measured duration)", ev.Duration)
	}
	if !ev.Denied {
		t.Error("Denied should be true for a rejected call")
	}
}

func TestApplyApprovalOutcome_NilMeterAndUnknownDuration(t *testing.T) {
	// nil meter (e.g. teammate context) is a no-op.
	ev := handler.ToolResultEvent{ToolCallID: "tc1", Duration: 10 * time.Millisecond}
	applyApprovalOutcome(&ev, nil)
	if ev.Duration != 10*time.Millisecond || ev.Denied {
		t.Errorf("nil meter mutated event: %+v", ev)
	}

	// Unknown duration (0) stays 0 — never goes negative from a recorded wait.
	m := newApprovalMeter()
	m.record("tc2", time.Second, true)
	ev2 := handler.ToolResultEvent{ToolCallID: "tc2"}
	applyApprovalOutcome(&ev2, m)
	if ev2.Duration != 0 {
		t.Errorf("Duration = %v, want 0 for unknown start", ev2.Duration)
	}
	if !ev2.Denied {
		t.Error("Denied must transfer even when duration is unknown")
	}
}

// TestRequestApproval_DeniedRecordsMeter drives the real approval path with a
// handler that waits before rejecting, and asserts (a) the deny verdict and
// wall-clock wait land in the ctx meter keyed by the middleware-stamped tool
// call id, and (b) the ApprovalRequest forwarded the tool call id.
func TestRequestApproval_DeniedRecordsMeter(t *testing.T) {
	const sleep = 30 * time.Millisecond
	var got handler.ApprovalRequest
	h := &waitingStubHandler{
		stubHandler: stubHandler{resp: handler.ApprovalResponse{Approved: false, Mode: handler.ModeManual}},
		wait:        sleep,
		got:         &got,
	}
	s := NewApprovalState("/tmp/workdir", false)
	s.SetHandler(h)

	meter := newApprovalMeter()
	ctx := withApprovalMeter(context.Background(), meter)
	ctx = agent.WithToolCallID(ctx, "call_123")

	// execute with a non-safe command always prompts in MANUAL mode.
	approved, err := s.RequestApproval(ctx, "execute", `{"command":"rm -rf /tmp/x"}`)
	if err != nil {
		t.Fatalf("RequestApproval error: %v", err)
	}
	if approved {
		t.Fatal("expected denial")
	}
	if got.ToolCallID != "call_123" {
		t.Errorf("ApprovalRequest.ToolCallID = %q, want call_123", got.ToolCallID)
	}

	wait, denied := meter.take("call_123")
	if !denied {
		t.Error("meter should record denied=true")
	}
	if wait < sleep {
		t.Errorf("recorded wait %v < prompt wait %v", wait, sleep)
	}

	// End-to-end duration math: a 40ms-measured call that spent ≥30ms at the
	// prompt reports at most the non-wait remainder.
	meter.record("call_456", 30*time.Millisecond, false)
	ev := handler.ToolResultEvent{ToolCallID: "call_456", Duration: 40 * time.Millisecond}
	applyApprovalOutcome(&ev, meter)
	if ev.Duration != 10*time.Millisecond {
		t.Errorf("Duration = %v, want 10ms", ev.Duration)
	}
}

// TestRequestApproval_ApprovedNotDenied asserts an approved prompt records its
// wait but no denial.
func TestRequestApproval_ApprovedNotDenied(t *testing.T) {
	h := &waitingStubHandler{
		stubHandler: stubHandler{resp: handler.ApprovalResponse{Approved: true, Mode: handler.ModeManual}},
		wait:        5 * time.Millisecond,
	}
	s := NewApprovalState("/tmp/workdir", false)
	s.SetHandler(h)

	meter := newApprovalMeter()
	ctx := withApprovalMeter(context.Background(), meter)
	ctx = agent.WithToolCallID(ctx, "call_ok")

	approved, err := s.RequestApproval(ctx, "execute", `{"command":"make deploy"}`)
	if err != nil {
		t.Fatalf("RequestApproval error: %v", err)
	}
	if !approved {
		t.Fatal("expected approval")
	}
	wait, denied := meter.take("call_ok")
	if denied {
		t.Error("approved call must not be marked denied")
	}
	if wait <= 0 {
		t.Error("approved call should still record its prompt wait")
	}
}
