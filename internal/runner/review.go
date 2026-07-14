package runner

import (
	"context"
	"sync"

	"github.com/cnjack/jcode/internal/agent"
	"github.com/cnjack/jcode/internal/config"
	"github.com/cnjack/jcode/internal/review"
)

// maxConsecutiveReviewDenials bounds how many times in a row the auto-reviewer
// may deny within one turn before control is handed back to the user. It stops a
// model⇄reviewer ping-pong from silently burning the iteration budget.
const maxConsecutiveReviewDenials = 3

// SetReviewer installs the optional LLM approval reviewer. A nil reviewer (the
// default) disables auto-review entirely, so behavior is unchanged.
func (s *ApprovalState) SetReviewer(r review.Reviewer) {
	s.mu.Lock()
	s.reviewer = r
	s.mu.Unlock()
}

// SetTranscriptFunc installs a provider of recent conversation context handed to
// the reviewer as evidence. nil means the reviewer judges the action alone.
func (s *ApprovalState) SetTranscriptFunc(fn func() []review.Msg) {
	s.mu.Lock()
	s.transcriptFn = fn
	s.mu.Unlock()
}

// OnTurnStart resets per-turn reviewer state (the denial circuit breaker). A
// frontend calls this once at the start of each user prompt turn.
func (s *ApprovalState) OnTurnStart() {
	s.breaker.reset()
}

// reviewBreaker tracks consecutive auto-review denials so a runaway loop trips
// out to the human instead of denying forever.
type reviewBreaker struct {
	mu          sync.Mutex
	consecutive int
}

// recordDenial counts a denial and reports whether the breaker tripped (meaning:
// stop auto-denying, escalate this call to the user).
func (b *reviewBreaker) recordDenial() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.consecutive++
	if b.consecutive >= maxConsecutiveReviewDenials {
		b.consecutive = 0
		return true
	}
	return false
}

func (b *reviewBreaker) recordNonDenial() {
	b.mu.Lock()
	b.consecutive = 0
	b.mu.Unlock()
}

func (b *reviewBreaker) reset() {
	b.mu.Lock()
	b.consecutive = 0
	b.mu.Unlock()
}

// tryReview runs the auto-reviewer for a call that decide() routed to a user
// prompt. handled=false means "escalate to the user" — the reviewer is disabled,
// failed, chose to escalate, or the circuit breaker tripped. When handled=true,
// the returned (approved, err) is the final answer: a denial carries a
// *agent.ReviewDeniedError so the middleware surfaces the reviewer's rationale.
func (s *ApprovalState) tryReview(ctx context.Context, toolName, toolArgs string, isExternal bool) (approved bool, err error, handled bool) {
	s.mu.Lock()
	r := s.reviewer
	tfn := s.transcriptFn
	cwd := s.workpath
	s.mu.Unlock()
	if r == nil {
		return false, nil, false
	}

	var transcript []review.Msg
	if tfn != nil {
		transcript = tfn()
	}
	res := r.Review(ctx, review.Request{
		ToolName:   toolName,
		ToolArgs:   toolArgs,
		Cwd:        cwd,
		IsExternal: isExternal,
		Transcript: transcript,
	})

	switch res.Outcome {
	case review.Allow:
		s.breaker.recordNonDenial()
		s.notifyToolInProgress(toolName, toolArgs)
		return true, nil, true
	case review.Deny:
		if s.breaker.recordDenial() {
			config.Logger().Printf("[review] denial circuit breaker tripped for %q; escalating to user", toolName)
			return false, nil, false
		}
		return false, &agent.ReviewDeniedError{Reason: res.Rationale}, true
	default: // review.Escalate
		return false, nil, false
	}
}

// gatedApproval runs the reviewer first (when enabled); if the reviewer does not
// settle the call, it falls back to the interactive user prompt. It is shared by
// the primary and teammate approval paths so the two cannot drift.
func (s *ApprovalState) gatedApproval(ctx context.Context, toolName, toolArgs string, isExternal bool, workerName, workerColor string) (bool, error) {
	if approved, err, handled := s.tryReview(ctx, toolName, toolArgs, isExternal); handled {
		return approved, err
	}
	return s.requestUserApprovalWithWorker(ctx, toolName, toolArgs, isExternal, workerName, workerColor)
}
