package agent

import (
	"context"
	"strings"
	"sync"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"

	"github.com/cnjack/jcode/internal/config"
)

// RecoveryActionType categorises the kinds of recovery a layer can perform.
type RecoveryActionType int

const (
	ActionRetryWithContinuation RecoveryActionType = iota
	ActionRetryWithCompaction
	ActionFallbackModel
)

// RecoveryLayer is a single tier of error recovery.
type RecoveryLayer interface {
	CanHandle(err error, state *adk.ChatModelAgentState) bool
	Recover(ctx context.Context, err error, state *adk.ChatModelAgentState) (*RecoveryAction, error)
}

// RecoveryAction describes what the recovery middleware should do.
type RecoveryAction struct {
	Type     RecoveryActionType
	Messages []*schema.Message // replacement or patched messages
}

// RecoveryTracker keeps per-action-type retry counts so we don't loop forever.
type RecoveryTracker struct {
	mu         sync.Mutex
	attempts   map[RecoveryActionType]int
	maxRetries map[RecoveryActionType]int
}

// NewRecoveryTracker creates a tracker with default max retries.
func NewRecoveryTracker() *RecoveryTracker {
	return &RecoveryTracker{
		attempts: make(map[RecoveryActionType]int),
		maxRetries: map[RecoveryActionType]int{
			ActionRetryWithContinuation: 3,
			ActionRetryWithCompaction:   2,
			ActionFallbackModel:         1,
		},
	}
}

func (rt *RecoveryTracker) canRetry(action RecoveryActionType) bool {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	return rt.attempts[action] < rt.maxRetries[action]
}

func (rt *RecoveryTracker) record(action RecoveryActionType) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	rt.attempts[action]++
}

func (rt *RecoveryTracker) reset() {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	for k := range rt.attempts {
		rt.attempts[k] = 0
	}
}

// recoveryMiddleware is a ChatModelAgentMiddleware that inspects model errors
// and tool errors in the conversation history, applying layered recovery.
type recoveryMiddleware struct {
	*adk.BaseChatModelAgentMiddleware
	layers  []RecoveryLayer
	tracker *RecoveryTracker
}

// NewRecoveryMiddleware creates a ChatModelAgentMiddleware with the given
// recovery layers tried in order.
func NewRecoveryMiddleware(layers []RecoveryLayer) adk.ChatModelAgentMiddleware {
	return &recoveryMiddleware{
		BaseChatModelAgentMiddleware: &adk.BaseChatModelAgentMiddleware{},
		layers:                       layers,
		tracker:                      NewRecoveryTracker(),
	}
}

// AfterModelRewriteState inspects the last message for errors and applies
// recovery if a suitable layer is found.
func (m *recoveryMiddleware) AfterModelRewriteState(
	ctx context.Context,
	state *adk.ChatModelAgentState,
	mc *adk.ModelContext,
) (context.Context, *adk.ChatModelAgentState, error) {
	lastErr := m.detectError(state)
	if lastErr == nil {
		m.tracker.reset()
		return ctx, state, nil
	}

	for _, layer := range m.layers {
		if !layer.CanHandle(lastErr, state) {
			continue
		}

		action, err := layer.Recover(ctx, lastErr, state)
		if err != nil {
			config.Logger().Printf("[recovery] layer recover failed: %v", err)
			continue
		}
		if action == nil {
			continue
		}

		if !m.tracker.canRetry(action.Type) {
			config.Logger().Printf("[recovery] action type %d exhausted retries", action.Type)
			continue
		}
		m.tracker.record(action.Type)

		config.Logger().Printf("[recovery] applying action type %d", action.Type)

		if action.Messages != nil {
			state.Messages = action.Messages
		}

		return ctx, state, nil
	}

	return ctx, state, nil
}

// detectError looks at the last few messages for signs of model or tool error.
func (m *recoveryMiddleware) detectError(state *adk.ChatModelAgentState) error {
	for i := len(state.Messages) - 1; i >= 0 && i >= len(state.Messages)-3; i-- {
		msg := state.Messages[i]
		if msg.Role == schema.Tool {
			if strings.HasPrefix(msg.Content, "Tool execution failed:") {
				return &toolError{message: msg.Content}
			}
		}
	}
	return nil
}

type toolError struct {
	message string
}

func (e *toolError) Error() string { return e.message }

// --- Built-in Recovery Layers ---

// MaxOutputContinuationLayer handles "max output tokens" errors by asking
// the model to continue from where it stopped.
type MaxOutputContinuationLayer struct{}

func (l *MaxOutputContinuationLayer) CanHandle(err error, state *adk.ChatModelAgentState) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "max_tokens") ||
		strings.Contains(msg, "length") ||
		strings.Contains(msg, "truncated")
}

func (l *MaxOutputContinuationLayer) Recover(ctx context.Context, err error, state *adk.ChatModelAgentState) (*RecoveryAction, error) {
	// Append a user message asking the model to continue.
	continuationMsg := &schema.Message{
		Role:    schema.User,
		Content: "Your previous response was truncated. Please continue from where you left off.",
	}
	msgs := make([]*schema.Message, len(state.Messages), len(state.Messages)+1)
	copy(msgs, state.Messages)
	msgs = append(msgs, continuationMsg)

	return &RecoveryAction{
		Type:     ActionRetryWithContinuation,
		Messages: msgs,
	}, nil
}

// ContextOverflowLayer handles context-too-long errors by trimming older messages.
type ContextOverflowLayer struct {
	KeepRecent int
}

func (l *ContextOverflowLayer) CanHandle(err error, state *adk.ChatModelAgentState) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "context_length_exceeded") ||
		strings.Contains(msg, "too many tokens") ||
		strings.Contains(msg, "maximum context length")
}

func (l *ContextOverflowLayer) Recover(ctx context.Context, err error, state *adk.ChatModelAgentState) (*RecoveryAction, error) {
	keepRecent := l.KeepRecent
	if keepRecent <= 0 {
		keepRecent = 10
	}
	if len(state.Messages) <= keepRecent {
		return nil, nil // can't help
	}

	// Keep system messages + last keepRecent messages.
	var systemMsgs []*schema.Message
	var rest []*schema.Message
	for _, m := range state.Messages {
		if m.Role == schema.System {
			systemMsgs = append(systemMsgs, m)
		} else {
			rest = append(rest, m)
		}
	}

	if len(rest) <= keepRecent {
		return nil, nil
	}

	trimmed := rest[len(rest)-keepRecent:]
	msgs := make([]*schema.Message, 0, len(systemMsgs)+len(trimmed))
	msgs = append(msgs, systemMsgs...)
	msgs = append(msgs, trimmed...)

	config.Logger().Printf("[recovery] trimmed context from %d to %d messages", len(state.Messages), len(msgs))

	return &RecoveryAction{
		Type:     ActionRetryWithCompaction,
		Messages: msgs,
	}, nil
}
