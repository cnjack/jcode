package runner

import (
	"context"
	"sync"
	"time"

	"github.com/cnjack/jcode/internal/handler"
)

// approvalMeter records, per tool call, the total time spent blocked on the
// interactive approval prompt and whether the user denied the call. The
// runner subtracts the wait from the reported ToolResultEvent.Duration so
// every frontend gets pure execution latency (codex-style "timer pauses
// during approval"), and surfaces the denied flag so UIs can render a
// rejection distinctly from an execution error (opencode-style strikethrough).
//
// One meter is created per Run and travels via context: ApprovalState's
// approval path (which blocks on the user) writes into it, and the runner's
// tool-result emission takes the record back out. Entries for calls whose
// results never reach this runner (e.g. teammate tools) are dropped with the
// meter at the end of the turn.
type approvalMeter struct {
	mu      sync.Mutex
	entries map[string]approvalOutcome // keyed by tool-call id
}

type approvalOutcome struct {
	wait   time.Duration
	denied bool
}

func newApprovalMeter() *approvalMeter {
	return &approvalMeter{entries: make(map[string]approvalOutcome)}
}

// record accumulates an approval wait for the given tool call. Multiple
// prompts for the same call (re-approvals) add up; denied is sticky.
func (m *approvalMeter) record(toolCallID string, wait time.Duration, denied bool) {
	if m == nil || toolCallID == "" {
		return
	}
	m.mu.Lock()
	e := m.entries[toolCallID]
	e.wait += wait
	e.denied = e.denied || denied
	m.entries[toolCallID] = e
	m.mu.Unlock()
}

// take removes and returns the recorded outcome for a tool call. A call that
// never hit the approval prompt returns (0, false).
func (m *approvalMeter) take(toolCallID string) (time.Duration, bool) {
	if m == nil || toolCallID == "" {
		return 0, false
	}
	m.mu.Lock()
	e, ok := m.entries[toolCallID]
	if ok {
		delete(m.entries, toolCallID)
	}
	m.mu.Unlock()
	return e.wait, e.denied
}

type approvalMeterCtxKey struct{}

// withApprovalMeter attaches the per-run meter to the context handed to the
// agent, so the approval path (reached from inside tool execution) and the
// result-emission path share one record without new plumbing.
func withApprovalMeter(ctx context.Context, m *approvalMeter) context.Context {
	return context.WithValue(ctx, approvalMeterCtxKey{}, m)
}

// approvalMeterFrom returns the meter attached by withApprovalMeter, or nil.
func approvalMeterFrom(ctx context.Context) *approvalMeter {
	m, _ := ctx.Value(approvalMeterCtxKey{}).(*approvalMeter)
	return m
}

// applyApprovalOutcome folds a meter record into a tool-result event:
// the approval wait is subtracted from Duration (clamped at zero — the
// remainder is pure execution time) and the denied flag is transferred.
func applyApprovalOutcome(ev *handler.ToolResultEvent, meter *approvalMeter) {
	if meter == nil {
		return
	}
	wait, denied := meter.take(ev.ToolCallID)
	ev.Denied = denied
	if wait > 0 && ev.Duration > 0 {
		ev.Duration -= wait
		if ev.Duration < 0 {
			ev.Duration = 0
		}
	}
}
