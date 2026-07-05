package agent

import (
	"context"
	"errors"
	"testing"

	"github.com/cloudwego/eino/adk"
	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// stubStrategy is a CompactionStrategy whose Compact behaviour is scripted per
// call, for driving the middleware directly (same style as budget_test.go).
type stubStrategy struct {
	compactCalls int
	fn           func(call int, msgs []*schema.Message) ([]*schema.Message, error)
}

func (s *stubStrategy) ShouldCompact(currentTokens, limit int) bool { return true }

func (s *stubStrategy) Compact(ctx context.Context, msgs []*schema.Message, keepRecent int) ([]*schema.Message, error) {
	s.compactCalls++
	return s.fn(s.compactCalls, msgs)
}

// errGenModel is a ToolCallingChatModel whose Generate always fails, standing
// in for a broken summarizer.
type errGenModel struct{}

func (errGenModel) Generate(context.Context, []*schema.Message, ...einomodel.Option) (*schema.Message, error) {
	return nil, errors.New("summarizer boom")
}

func (errGenModel) Stream(context.Context, []*schema.Message, ...einomodel.Option) (*schema.StreamReader[*schema.Message], error) {
	panic("Stream is not used by the compaction strategy")
}

func (m errGenModel) WithTools([]*schema.ToolInfo) (einomodel.ToolCallingChatModel, error) {
	return m, nil
}

func newTestState(n int) *adk.ChatModelAgentState {
	msgs := make([]adk.Message, 0, n)
	for i := 0; i < n; i++ {
		if i%2 == 0 {
			msgs = append(msgs, schema.UserMessage("u"))
		} else {
			msgs = append(msgs, &schema.Message{Role: schema.Assistant, Content: "a"})
		}
	}
	return &adk.ChatModelAgentState{Messages: msgs}
}

func asCompactionMiddleware(t *testing.T, strategy CompactionStrategy) *compactionMiddleware {
	t.Helper()
	mw := NewCompactionMiddleware(strategy, 100000, nil, nil)
	m, ok := mw.(*compactionMiddleware)
	if !ok {
		t.Fatalf("NewCompactionMiddleware returned %T, want *compactionMiddleware", mw)
	}
	return m
}

// TestCompactionMiddleware_FuseAfterConsecutiveFails: after 3 consecutive
// compaction failures the middleware must stop calling the summarizer for the
// rest of the session (fail-open every time, never an error to the run).
func TestCompactionMiddleware_FuseAfterConsecutiveFails(t *testing.T) {
	strategy := &stubStrategy{fn: func(int, []*schema.Message) ([]*schema.Message, error) {
		return nil, errors.New("boom")
	}}
	m := asCompactionMiddleware(t, strategy)
	state := newTestState(6)

	for i := 0; i < 6; i++ {
		_, got, err := m.BeforeModelRewriteState(context.Background(), state, nil)
		if err != nil {
			t.Fatalf("call %d: err = %v, want nil (fail-open)", i+1, err)
		}
		if len(got.Messages) != 6 {
			t.Fatalf("call %d: messages mutated on failure: %d, want 6", i+1, len(got.Messages))
		}
	}

	if strategy.compactCalls != 3 {
		t.Errorf("Compact called %d times, want exactly 3 (fused from the 4th trigger on)", strategy.compactCalls)
	}
	m.state.mu.Lock()
	fails := m.state.consecutiveFails
	m.state.mu.Unlock()
	if fails != 3 {
		t.Errorf("consecutiveFails = %d, want 3", fails)
	}
}

// TestCompactionMiddleware_FuseResetOnSuccess: failures below the fuse limit
// followed by a success must reset the counter and keep compaction available.
func TestCompactionMiddleware_FuseResetOnSuccess(t *testing.T) {
	strategy := &stubStrategy{fn: func(call int, msgs []*schema.Message) ([]*schema.Message, error) {
		if call <= 2 {
			return nil, errors.New("boom")
		}
		return msgs[:len(msgs)-1], nil // success: shrink by one
	}}
	m := asCompactionMiddleware(t, strategy)
	state := newTestState(6)

	for i := 0; i < 4; i++ {
		_, got, err := m.BeforeModelRewriteState(context.Background(), state, nil)
		if err != nil {
			t.Fatalf("call %d: err = %v, want nil", i+1, err)
		}
		state = got
	}

	if strategy.compactCalls != 4 {
		t.Errorf("Compact called %d times, want 4 (2 fails below the fuse + 2 successes)", strategy.compactCalls)
	}
	if len(state.Messages) != 4 {
		t.Errorf("messages after two successful compactions = %d, want 4", len(state.Messages))
	}
	m.state.mu.Lock()
	fails, count := m.state.consecutiveFails, m.state.compactionCount
	m.state.mu.Unlock()
	if fails != 0 {
		t.Errorf("consecutiveFails = %d, want 0 after success", fails)
	}
	if count != 2 {
		t.Errorf("compactionCount = %d, want 2", count)
	}
}

// TestCompactionMiddleware_NoShrinkCountsAsFail: a Compact that returns no
// fewer messages (compressed nothing) counts towards the fuse instead of being
// celebrated as a successful compaction.
func TestCompactionMiddleware_NoShrinkCountsAsFail(t *testing.T) {
	strategy := &stubStrategy{fn: func(_ int, msgs []*schema.Message) ([]*schema.Message, error) {
		return msgs, nil // same length, nil error
	}}
	m := asCompactionMiddleware(t, strategy)
	state := newTestState(6)

	_, got, err := m.BeforeModelRewriteState(context.Background(), state, nil)
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if len(got.Messages) != 6 {
		t.Fatalf("messages = %d, want 6 (unchanged)", len(got.Messages))
	}

	m.state.mu.Lock()
	fails, count := m.state.consecutiveFails, m.state.compactionCount
	m.state.mu.Unlock()
	if fails != 1 {
		t.Errorf("consecutiveFails = %d, want 1 (no-shrink counts as failure)", fails)
	}
	if count != 0 {
		t.Errorf("compactionCount = %d, want 0", count)
	}
}

// TestThresholdStrategy_PropagatesSummarizerError: the strategy must surface a
// summarizer error to its caller (the middleware counts it towards the fuse)
// instead of swallowing it and reporting a successful no-op.
func TestThresholdStrategy_PropagatesSummarizerError(t *testing.T) {
	strategy := NewThresholdCompactionStrategy(0.75, errGenModel{}, 2)

	msgs := make([]*schema.Message, 0, 10)
	for i := 0; i < 10; i++ {
		if i%2 == 0 {
			msgs = append(msgs, schema.UserMessage("u"))
		} else {
			msgs = append(msgs, &schema.Message{Role: schema.Assistant, Content: "a"})
		}
	}

	got, err := strategy.Compact(context.Background(), msgs, 0)
	if err == nil {
		t.Fatal("Compact swallowed the summarizer error, want it propagated")
	}
	if len(got) != len(msgs) {
		t.Errorf("Compact returned %d messages on error, want the original %d (fail-open)", len(got), len(msgs))
	}
}
