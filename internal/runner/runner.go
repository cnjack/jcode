package runner

import (
	"context"
	"io"
	"sort"
	"strings"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"

	"github.com/cnjack/jcode/internal/config"
	"github.com/cnjack/jcode/internal/handler"
	internalmodel "github.com/cnjack/jcode/internal/model"
	"github.com/cnjack/jcode/internal/session"
	"github.com/cnjack/jcode/internal/telemetry"
	"github.com/cnjack/jcode/internal/tools"
)

// Run executes the agent for a single turn, wrapping the response with a
// Langfuse trace when a tracer is present, enforcing todo-completion guards,
// and sending token-usage updates to the handler when done.
func Run(
	ctx context.Context,
	ag *adk.ChatModelAgent,
	messages []adk.Message,
	h handler.AgentEventHandler,
	rec *session.Recorder,
	todoStore *tools.TodoStore,
	goalStore *tools.GoalStore,
	tracer *telemetry.LangfuseTracer,
	tokenUsage *internalmodel.TokenUsage,
) string {
	if tracer != nil {
		ctx = tracer.WithNewTrace(ctx, "coding_agent")
	}
	if tokenUsage != nil {
		ctx = internalmodel.WithTokenTracker(ctx, tokenUsage)
	}
	h.OnAgentStart()

	resp := runInner(ctx, ag, messages, h, rec)
	// pending is the assistant text produced since messages was last extended;
	// each continuation appends only this delta, never the accumulated resp,
	// so earlier turns are not duplicated into the context.
	pending := resp

	// Completion guard: if the agent finished but there are still incomplete
	// todos, re-run with a reminder so nothing is left behind.
	const maxGuardRetries = 3
	for i := 0; i < maxGuardRetries; i++ {
		if todoStore == nil || !todoStore.HasIncomplete() {
			break
		}
		reminder := todoStore.IncompleteSummary()
		h.OnAgentText("\n⚠️ Incomplete todos detected, continuing...\n")
		messages = append(messages, &schema.Message{Role: schema.Assistant, Content: pending})
		messages = append(messages, schema.UserMessage(reminder))
		extra := runInner(ctx, ag, messages, h, rec)
		resp += extra
		pending = extra
	}

	// Goal continuation guard: if an active goal exists and the agent stopped
	// without proving it complete, keep injecting a continuation prompt and
	// re-running — mirroring codex's idle auto-continuation. Bounded by a hard
	// turn cap and context cancellation.
	if goalStore != nil {
		const maxGoalContinuations = 25
	goalLoop:
		for turns := 0; turns < maxGoalContinuations; turns++ {
			select {
			case <-ctx.Done():
				config.Logger().Printf("[runner] goal continuation cancelled")
				break goalLoop
			default:
			}
			// Record observed tokens for the informational usage display.
			if tokenUsage != nil {
				goalStore.RecordTokens(tokenUsage.GetLastTotal())
			}
			if !goalStore.IsActive() {
				break
			}
			cont := goalStore.ContinuationPrompt()
			if cont == "" {
				break
			}
			config.Logger().Printf("[runner] goal continuation #%d", turns+1)
			h.OnAgentText("\n🎯 Goal active — continuing toward objective...\n")
			messages = append(messages, &schema.Message{Role: schema.Assistant, Content: pending})
			messages = append(messages, schema.UserMessage(cont))
			extra := runInner(ctx, ag, messages, h, rec)
			resp += extra
			pending = extra
		}
	}

	// Send token usage update before signalling done.
	var lastTotalTokens int64
	if tokenUsage != nil {
		lastTotalTokens = tokenUsage.GetLastTotal()
	}
	if local := internalmodel.TokenTrackerFromContext(ctx); local != nil {
		lastTotalTokens = local.GetLastTotal()
	}
	h.OnTokenUpdate(handler.TokenUsage{
		TotalTokens:       lastTotalTokens,
		ModelContextLimit: modelContextLimit(),
	})

	h.OnAgentDone(nil)
	return resp
}

func runInner(
	ctx context.Context,
	ag *adk.ChatModelAgent,
	messages []adk.Message,
	h handler.AgentEventHandler,
	rec *session.Recorder,
) string {
	input := &adk.AgentInput{
		Messages:        messages,
		EnableStreaming: true,
	}

	var assistantText strings.Builder

	config.Logger().Printf("[runner] runInner start, messages=%d", len(messages))
	iterator := ag.Run(ctx, input)
	eventCount := 0
	for {
		// Check context cancellation before each iteration so user
		// interrupts and timeouts are respected promptly.
		select {
		case <-ctx.Done():
			config.Logger().Printf("[runner] context cancelled, stopping iteration")
			h.OnAgentDone(ctx.Err())
			return assistantText.String()
		default:
		}

		event, ok := iterator.Next()
		if !ok {
			config.Logger().Printf("[runner] iterator done after %d events", eventCount)
			break
		}
		eventCount++
		if event.Err != nil {
			config.Logger().Printf("[runner] event error: %v", event.Err)
			h.OnAgentDone(event.Err)
			return assistantText.String()
		}
		if event.Output == nil || event.Output.MessageOutput == nil {
			config.Logger().Printf("[runner] event #%d: nil output", eventCount)
			continue
		}

		mo := event.Output.MessageOutput
		config.Logger().Printf("[runner] event #%d: role=%s, streaming=%v, hasMessage=%v", eventCount, mo.Role, mo.IsStreaming, mo.Message != nil)

		if mo.Role == schema.Tool {
			toolName := mo.ToolName
			if !mo.IsStreaming && mo.Message != nil {
				output := mo.Message.Content
				h.OnToolResult(toolName, output, mo.Message.ToolCallID, nil)
				if toolName == "todowrite" || toolName == "todoread" {
					h.OnTodoUpdate()
				}
				if rec != nil {
					rec.RecordToolResult(toolName, output, mo.Message.ToolCallID, nil)
				}
			} else if mo.IsStreaming {
				var sb strings.Builder
				var toolErr error
				var toolCallID string
				for {
					chunk, err := mo.MessageStream.Recv()
					if err == io.EOF {
						break
					}
					if err != nil {
						toolErr = err
						h.OnToolResult(toolName, "", toolCallID, err)
						break
					}
					if chunk != nil {
						sb.WriteString(chunk.Content)
						if toolCallID == "" && chunk.ToolCallID != "" {
							toolCallID = chunk.ToolCallID
						}
					}
				}
				if toolErr == nil {
					h.OnToolResult(toolName, sb.String(), toolCallID, nil)
					if toolName == "todowrite" || toolName == "todoread" {
						h.OnTodoUpdate()
					}
					if rec != nil {
						rec.RecordToolResult(toolName, sb.String(), toolCallID, nil)
					}
				} else if rec != nil {
					rec.RecordToolResult(toolName, "", toolCallID, toolErr)
				}
			}
			continue
		}

		if mo.Role != schema.Assistant {
			continue
		}

		if mo.IsStreaming {
			// Accumulate streaming tool call names, args, and IDs across chunks.
			type pendingTC struct {
				name string
				id   string
				args strings.Builder
			}
			pending := make(map[int]*pendingTC)
			for {
				chunk, err := mo.MessageStream.Recv()
				if err == io.EOF {
					break
				}
				if err != nil {
					break
				}
				if chunk == nil {
					continue
				}
				for _, tc := range chunk.ToolCalls {
					idx := 0
					if tc.Index != nil {
						idx = *tc.Index
					}
					if tc.Function.Name != "" {
						p := &pendingTC{name: tc.Function.Name, id: tc.ID}
						p.args.WriteString(tc.Function.Arguments)
						pending[idx] = p
					} else if p, ok := pending[idx]; ok {
						p.args.WriteString(tc.Function.Arguments)
					}
				}
				if chunk.Content != "" {
					assistantText.WriteString(chunk.Content)
					h.OnAgentText(chunk.Content)
				}
			}
			// Notify and record accumulated tool calls in index order.
			indices := make([]int, 0, len(pending))
			for idx := range pending {
				indices = append(indices, idx)
			}
			sort.Ints(indices)
			for _, idx := range indices {
				p := pending[idx]
				h.OnToolCall(p.name, p.args.String(), p.id)
				if rec != nil {
					rec.RecordToolCall(p.name, p.args.String(), p.id)
				}
			}
		} else if mo.Message != nil {
			if len(mo.Message.ToolCalls) > 0 {
				for _, tc := range mo.Message.ToolCalls {
					h.OnToolCall(tc.Function.Name, tc.Function.Arguments, tc.ID)
					if rec != nil {
						rec.RecordToolCall(tc.Function.Name, tc.Function.Arguments, tc.ID)
					}
				}
			}
			if mo.Message.Content != "" {
				assistantText.WriteString(mo.Message.Content)
				h.OnAgentText(mo.Message.Content)
			}
		}
	}

	if rec != nil && assistantText.Len() > 0 {
		rec.RecordAssistant(assistantText.String())
	}

	return assistantText.String()
}

func modelContextLimit() int {
	cfg, err := config.LoadConfig()
	if err != nil {
		return 0
	}
	provider, modelName := cfg.GetProviderModel()
	registry := internalmodel.NewModelRegistryWithConfig(cfg)
	return internalmodel.ResolveContextLimit(registry, cfg, provider, modelName)
}
