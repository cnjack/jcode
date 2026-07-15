package runner

import (
	"context"
	"errors"
	"testing"

	"github.com/cnjack/jcode/internal/agent"
	"github.com/cnjack/jcode/internal/handler"
	"github.com/cnjack/jcode/internal/review"
)

// fakeReviewer returns a canned verdict and counts calls.
type fakeReviewer struct {
	res   review.Result
	calls int
}

func (f *fakeReviewer) Review(context.Context, review.Request) review.Result {
	f.calls++
	return f.res
}

// countingHandler records how many times the user was prompted and answers with
// a fixed response.
type countingHandler struct {
	prompts int
	resp    handler.ApprovalResponse
}

func (countingHandler) OnAgentText(string)                   {}
func (countingHandler) OnToolCall(handler.ToolCallEvent)     {}
func (countingHandler) OnToolResult(handler.ToolResultEvent) {}
func (countingHandler) OnTodoUpdate()                        {}
func (countingHandler) OnAgentStart()                        {}
func (countingHandler) OnAgentDone(error)                    {}
func (countingHandler) OnTokenUpdate(handler.TokenUsage)     {}
func (h *countingHandler) RequestApproval(context.Context, handler.ApprovalRequest) (handler.ApprovalResponse, error) {
	h.prompts++
	return h.resp, nil
}

// a command that decide() routes to a user prompt (not auto-approved, not
// auto-denied): a non-safelisted shell command.
const promptArgs = `{"command":"rm -rf /some/path"}`

func newReviewState(t *testing.T, rev review.Reviewer, h handler.AgentEventHandler) *ApprovalState {
	t.Helper()
	s := NewApprovalState("/tmp/workdir", false) // manual mode
	s.SetHandler(h)
	if rev != nil {
		s.SetReviewer(rev)
	}
	return s
}

func TestReviewer_AllowSkipsUserPrompt(t *testing.T) {
	h := &countingHandler{resp: handler.ApprovalResponse{Approved: false}}
	rev := &fakeReviewer{res: review.Result{Outcome: review.Allow}}
	s := newReviewState(t, rev, h)

	approved, err := s.RequestApproval(context.Background(), "execute", promptArgs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !approved {
		t.Fatalf("expected auto-allow to approve the call")
	}
	if h.prompts != 0 {
		t.Fatalf("expected no user prompt on auto-allow, got %d", h.prompts)
	}
	if rev.calls != 1 {
		t.Fatalf("expected reviewer to be consulted once, got %d", rev.calls)
	}
}

func TestReviewer_DenyReturnsReviewDeniedError(t *testing.T) {
	h := &countingHandler{resp: handler.ApprovalResponse{Approved: true}}
	rev := &fakeReviewer{res: review.Result{Outcome: review.Deny, Rationale: "destroys unpushed work"}}
	s := newReviewState(t, rev, h)

	approved, err := s.RequestApproval(context.Background(), "execute", promptArgs)
	if approved {
		t.Fatalf("expected denial")
	}
	var denied *agent.ReviewDeniedError
	if !errors.As(err, &denied) {
		t.Fatalf("expected *agent.ReviewDeniedError, got %v", err)
	}
	if denied.Reason != "destroys unpushed work" {
		t.Fatalf("rationale not propagated, got %q", denied.Reason)
	}
	if h.prompts != 0 {
		t.Fatalf("expected no user prompt on auto-deny, got %d", h.prompts)
	}
}

func TestReviewer_EscalateFallsBackToUser(t *testing.T) {
	h := &countingHandler{resp: handler.ApprovalResponse{Approved: true}}
	rev := &fakeReviewer{res: review.Result{Outcome: review.Escalate, Failed: true}}
	s := newReviewState(t, rev, h)

	approved, err := s.RequestApproval(context.Background(), "execute", promptArgs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !approved {
		t.Fatalf("expected fallback user prompt to approve")
	}
	if h.prompts != 1 {
		t.Fatalf("expected exactly one user prompt on escalate, got %d", h.prompts)
	}
}

func TestReviewer_NilReviewerAlwaysPrompts(t *testing.T) {
	h := &countingHandler{resp: handler.ApprovalResponse{Approved: true}}
	s := newReviewState(t, nil, h)

	if _, err := s.RequestApproval(context.Background(), "execute", promptArgs); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if h.prompts != 1 {
		t.Fatalf("expected user prompt when no reviewer set, got %d", h.prompts)
	}
}

func TestReviewer_AutoApprovedCallsSkipReviewer(t *testing.T) {
	h := &countingHandler{resp: handler.ApprovalResponse{Approved: true}}
	rev := &fakeReviewer{res: review.Result{Outcome: review.Deny}}
	s := newReviewState(t, rev, h)

	// `ls` is on the safe-command allowlist → decide() auto-approves before the
	// reviewer is ever consulted.
	approved, err := s.RequestApproval(context.Background(), "execute", `{"command":"ls -la"}`)
	if err != nil || !approved {
		t.Fatalf("expected safe command auto-approved, got approved=%v err=%v", approved, err)
	}
	if rev.calls != 0 {
		t.Fatalf("expected reviewer NOT consulted for allowlisted command, got %d", rev.calls)
	}
}

func TestReviewer_CircuitBreakerEscalatesAfterConsecutiveDenials(t *testing.T) {
	h := &countingHandler{resp: handler.ApprovalResponse{Approved: true}}
	rev := &fakeReviewer{res: review.Result{Outcome: review.Deny, Rationale: "risky"}}
	s := newReviewState(t, rev, h)

	// The first maxConsecutiveReviewDenials-1 calls auto-deny (no user prompt);
	// the Nth trips the breaker and escalates to the user, who approves it.
	for i := 0; i < maxConsecutiveReviewDenials-1; i++ {
		approved, err := s.RequestApproval(context.Background(), "execute", promptArgs)
		if approved {
			t.Fatalf("call %d: expected auto-deny before breaker trips", i)
		}
		var denied *agent.ReviewDeniedError
		if !errors.As(err, &denied) {
			t.Fatalf("call %d: expected *agent.ReviewDeniedError, got %v", i, err)
		}
	}
	if h.prompts != 0 {
		t.Fatalf("expected no user prompt before breaker trips, got %d", h.prompts)
	}
	// The breaker-tripping call escalates to the user (who approves here).
	approved, err := s.RequestApproval(context.Background(), "execute", promptArgs)
	if err != nil {
		t.Fatalf("breaker-tripping call errored: %v", err)
	}
	if !approved {
		t.Fatalf("expected breaker-tripping call to escalate and be user-approved")
	}
	if h.prompts != 1 {
		t.Fatalf("expected exactly one user prompt when breaker trips, got %d", h.prompts)
	}

	// OnTurnStart resets the breaker, so auto-deny resumes.
	s.OnTurnStart()
	_, err = s.RequestApproval(context.Background(), "execute", promptArgs)
	var denied *agent.ReviewDeniedError
	if !errors.As(err, &denied) {
		t.Fatalf("expected auto-deny again after OnTurnStart reset, got %v", err)
	}
}
