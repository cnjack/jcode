package runner

import (
	"context"
	"io"
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
	tracer *telemetry.LangfuseTracer,
) string {
	if tracer != nil {
		ctx = tracer.WithNewTrace(ctx, "coding_agent")
	}
	resp := runInner(ctx, ag, messages, h, rec, todoStore)

	// Completion guard: if the agent finished but there are still incomplete
	// todos, re-run with a reminder so nothing is left behind.
	const maxGuardRetries = 3
	for i := 0; i < maxGuardRetries; i++ {
		if todoStore == nil || !todoStore.HasIncomplete() {
			break
		}
		reminder := todoStore.IncompleteSummary()
		h.OnAgentText("\n⚠️ Incomplete todos detected, continuing...\n")
		messages = append(messages, &schema.Message{Role: schema.Assistant, Content: resp})
		messages = append(messages, schema.UserMessage(reminder))
		extra := runInner(ctx, ag, messages, h, rec, todoStore)
		resp += extra
	}

	// Send token usage update before signalling done.
	promptTokens, completionTokens, totalTokens := internalmodel.GetTokenUsage()
	h.OnTokenUpdate(handler.TokenUsage{
		PromptTokens:      promptTokens,
		CompletionTokens:  completionTokens,
		TotalTokens:       totalTokens,
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
	todoStore *tools.TodoStore,
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
				h.OnToolResult(toolName, output, nil)
				if toolName == "todowrite" || toolName == "todoread" {
					h.OnTodoUpdate()
				}
				if rec != nil {
					rec.RecordToolResult(toolName, output, nil)
				}
			} else if mo.IsStreaming {
				var sb strings.Builder
				var toolErr error
				for {
					chunk, err := mo.MessageStream.Recv()
					if err == io.EOF {
						break
					}
					if err != nil {
						toolErr = err
						h.OnToolResult(toolName, "", err)
						break
					}
					if chunk != nil {
						sb.WriteString(chunk.Content)
					}
				}
				if toolErr == nil {
					h.OnToolResult(toolName, sb.String(), nil)
					if toolName == "todowrite" || toolName == "todoread" {
						h.OnTodoUpdate()
					}
					if rec != nil {
						rec.RecordToolResult(toolName, sb.String(), nil)
					}
				} else if rec != nil {
					rec.RecordToolResult(toolName, "", toolErr)
				}
			}
			continue
		}

		if mo.Role != schema.Assistant {
			continue
		}

		if mo.IsStreaming {
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
				if len(chunk.ToolCalls) > 0 {
					for _, tc := range chunk.ToolCalls {
						if tc.Function.Name != "" {
							h.OnToolCall(tc.Function.Name, tc.Function.Arguments)
							if rec != nil {
								rec.RecordToolCall(tc.Function.Name, tc.Function.Arguments)
							}
						}
					}
				}
				if chunk.Content != "" {
					assistantText.WriteString(chunk.Content)
					h.OnAgentText(chunk.Content)
				}
			}
		} else if mo.Message != nil {
			if len(mo.Message.ToolCalls) > 0 {
				for _, tc := range mo.Message.ToolCalls {
					h.OnToolCall(tc.Function.Name, tc.Function.Arguments)
					if rec != nil {
						rec.RecordToolCall(tc.Function.Name, tc.Function.Arguments)
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
	// Try registry lookup first (provider/model format)
	provider, modelName := cfg.GetProviderModel()
	if provider != "" && modelName != "" {
		registry := internalmodel.NewModelRegistry()
		if limit := registry.GetModelContextLimit(provider, modelName); limit > 0 {
			return limit
		}
	}
	// Fall back to hardcoded model limits
	return internalmodel.GetModelContextLimit(modelName)
}
