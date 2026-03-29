package agent

import (
	"context"
	"strings"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"

	internalmodel "github.com/cnjack/coding/internal/model"
	"github.com/cnjack/coding/internal/prompts"
	"github.com/cnjack/coding/internal/tools"
)

// ReminderConfig holds the static configuration for the reminder middleware.
type ReminderConfig struct {
	TodoStore    *tools.TodoStore
	PlanStore    *tools.PlanStore
	EnvLabel     string
	IsRemote     bool
	ContextLimit int
}

// reminderMiddleware implements ChatModelAgentMiddleware to inject conditional
// system reminders before each model call.
type reminderMiddleware struct {
	*adk.BaseChatModelAgentMiddleware
	cfg               ReminderConfig
	iteration         int
	consecutiveErrors int
}

// NewReminderMiddleware creates a ChatModelAgentMiddleware that injects
// conditional reminders (todo check, token warning, error streak) into the
// message stream before each model invocation.
func NewReminderMiddleware(cfg ReminderConfig) adk.ChatModelAgentMiddleware {
	return &reminderMiddleware{
		BaseChatModelAgentMiddleware: &adk.BaseChatModelAgentMiddleware{},
		cfg:                          cfg,
	}
}

func (m *reminderMiddleware) BeforeModelRewriteState(
	ctx context.Context,
	state *adk.ChatModelAgentState,
	mc *adk.ModelContext,
) (context.Context, *adk.ChatModelAgentState, error) {
	m.iteration++
	m.updateErrorStreak(state)

	promptTokens, _, _ := internalmodel.TokenTracker.Get()

	var incompleteTodoN int
	var hasIncomplete bool
	if m.cfg.TodoStore != nil {
		items := m.cfg.TodoStore.Items()
		for _, item := range items {
			if item.Status != tools.TodoCompleted && item.Status != tools.TodoCancelled {
				incompleteTodoN++
			}
		}
		hasIncomplete = incompleteTodoN > 0
	}

	rc := &prompts.ReminderContext{
		Iteration:         m.iteration,
		TokensUsed:        promptTokens,
		ContextLimit:      m.cfg.ContextLimit,
		HasIncompleteTodo: hasIncomplete,
		IncompleteTodoN:   incompleteTodoN,
		ConsecutiveErrors: m.consecutiveErrors,
		EnvLabel:          m.cfg.EnvLabel,
		IsRemote:          m.cfg.IsRemote,
	}

	// Inject approved plan context for execution mode.
	if m.cfg.PlanStore != nil && m.cfg.PlanStore.HasApprovedPlan() {
		rc.PlanContent = m.cfg.PlanStore.Content()
	}

	msgs := prompts.CollectReminders(rc)
	text := prompts.FormatReminders(msgs)
	if text != "" {
		state.Messages = append(state.Messages, &schema.Message{
			Role:    schema.System,
			Content: text,
		})
	}

	return ctx, state, nil
}

// updateErrorStreak scans the last tool result message in the state and
// increments or resets the consecutive error counter.
func (m *reminderMiddleware) updateErrorStreak(state *adk.ChatModelAgentState) {
	for i := len(state.Messages) - 1; i >= 0; i-- {
		msg := state.Messages[i]
		if msg.Role == schema.Tool {
			if strings.HasPrefix(msg.Content, "Tool execution failed:") ||
				strings.HasPrefix(msg.Content, "[tool error]") {
				m.consecutiveErrors++
			} else {
				m.consecutiveErrors = 0
			}
			return
		}
		// Stop scanning once we pass tool messages.
		if msg.Role == schema.Assistant || msg.Role == schema.User {
			m.consecutiveErrors = 0
			return
		}
	}
}
