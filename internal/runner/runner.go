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
	"github.com/cnjack/jcode/internal/usage"
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
	// Snapshot cumulative usage so we can record this turn's delta on completion.
	var startSnap internalmodel.TokenUsageDetail
	if tokenUsage != nil {
		startSnap = tokenUsage.GetFull()
	}
	// Resolve the context limit once (config + registry lookup) and reuse it for
	// every live update below.
	ctxLimit := modelContextLimit()
	// Real-time token display: push a fresh snapshot after every LLM call (not
	// just at turn end) so the UI's context indicator ticks up live during a run.
	if tokenUsage != nil {
		ctx = internalmodel.WithUsageNotifier(ctx, func() {
			h.OnTokenUpdate(buildTokenUsage(tokenUsage, ctxLimit))
		})
	}
	h.OnAgentStart()

	resp, done := runInner(ctx, ag, messages, h, rec)
	if done {
		// runInner already signaled completion (cancellation or a real error).
		return resp
	}
	// pending is the assistant text produced since messages was last extended;
	// each continuation appends only this delta, never the accumulated resp,
	// so earlier turns are not duplicated into the context.
	pending := resp

	// Completion guard: if the agent finished but there are still incomplete
	// todos, re-run with a reminder so nothing is left behind.
	const maxGuardRetries = 3
todoLoop:
	for i := 0; i < maxGuardRetries; i++ {
		// Respect cancellation (e.g. user stop): a canceled context must not
		// drive the auto-continue path, mirroring the goal loop below. Without
		// this, a stop floods the chat with paired "Incomplete todos
		// detected" / "context canceled" messages for every retry.
		select {
		case <-ctx.Done():
			config.Logger().Printf("[runner] todo continuation cancelled")
			break todoLoop
		default:
		}
		if todoStore == nil || !todoStore.HasIncomplete() {
			break
		}
		reminder := todoStore.IncompleteSummary()
		h.OnAgentText("\n⚠️ Incomplete todos detected, continuing...\n")
		messages = append(messages, &schema.Message{Role: schema.Assistant, Content: pending})
		messages = append(messages, schema.UserMessage(reminder))
		extra, done := runInner(ctx, ag, messages, h, rec)
		resp += extra
		pending = extra
		if done {
			return resp
		}
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
			extra, done := runInner(ctx, ag, messages, h, rec)
			resp += extra
			pending = extra
			if done {
				return resp
			}
		}
	}

	// Send a final token usage update before signalling done. Prefer the
	// context-local tracker (per-agent) and fall back to the passed-in one.
	tracker := tokenUsage
	if local := internalmodel.TokenTrackerFromContext(ctx); local != nil {
		tracker = local
	}
	h.OnTokenUpdate(buildTokenUsage(tracker, ctxLimit))

	// Persist this turn's token delta to the global usage log for stats.
	recordUsageTurn(tokenUsage, startSnap, rec)

	h.OnAgentDone(nil)
	return resp
}

func runInner(
	ctx context.Context,
	ag *adk.ChatModelAgent,
	messages []adk.Message,
	h handler.AgentEventHandler,
	rec *session.Recorder,
) (string, bool) {
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
			// Cancellation (e.g. user stop): report the clean context error so
			// the TUI shows its cancel notice and the web classifies it as a
			// calm "Stopped". runInner owns this OnAgentDone (returns done=true),
			// so Run does not emit a second one.
			config.Logger().Printf("[runner] context cancelled, stopping iteration")
			h.OnAgentDone(ctx.Err())
			return assistantText.String(), true
		default:
		}

		event, ok := iterator.Next()
		if !ok {
			config.Logger().Printf("[runner] iterator done after %d events", eventCount)
			break
		}
		eventCount++
		if event.Err != nil {
			// If the run was canceled, the stream error is just fallout from the
			// cancellation (e.g. "[NodeRunError] context canceled"), not a real
			// failure — report a clean stop instead of surfacing the noise.
			if ctx.Err() != nil {
				// The stream error is just fallout from cancellation (e.g.
				// "[NodeRunError] context canceled"); report the clean context
				// error instead of the noisy wrapped one.
				config.Logger().Printf("[runner] event error during cancellation: %v", event.Err)
				h.OnAgentDone(ctx.Err())
				return assistantText.String(), true
			}
			config.Logger().Printf("[runner] event error: %v", event.Err)
			h.OnAgentDone(event.Err)
			return assistantText.String(), true
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

	// Clean completion: Run emits the single final OnAgentDone(nil).
	return assistantText.String(), false
}

// buildTokenUsage snapshots a tracker into a handler.TokenUsage. TotalTokens is
// the last call's total (current context occupancy); the rest are cumulative.
// Safe to call from any goroutine (the tracker uses atomics).
func buildTokenUsage(tracker *internalmodel.TokenUsage, ctxLimit int) handler.TokenUsage {
	tu := handler.TokenUsage{ModelContextLimit: ctxLimit}
	if tracker != nil {
		full := tracker.GetFull()
		tu.TotalTokens = tracker.GetLastTotal()
		tu.PromptTokens = int64(full.PromptTokens)
		tu.CompletionTokens = int64(full.CompletionTokens)
		tu.CachedTokens = int64(full.CachedTokens)
		tu.ReasoningTokens = int64(full.ReasoningTokens)
		tu.CacheWriteTokens = int64(full.CacheWriteTokens)
		tu.CallCount = int64(full.CallCount)
		tu.CacheHitRate = tracker.CacheHitRate()
		tu.CacheSupported = tracker.CacheObserved()
	}
	return tu
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

// recordUsageTurn appends this turn's token delta (cumulative-now minus the
// start-of-turn snapshot) to the global usage log. Best-effort: a nil tracker,
// an empty delta, or a write error never affects the run.
func recordUsageTurn(tracker *internalmodel.TokenUsage, start internalmodel.TokenUsageDetail, rec *session.Recorder) {
	if tracker == nil {
		return
	}
	delta := tracker.GetFull().Minus(start)
	if delta.TotalTokens <= 0 && delta.PromptTokens <= 0 {
		return
	}
	ev := usage.Event{
		Prompt:     delta.PromptTokens,
		Completion: delta.CompletionTokens,
		Cached:     delta.CachedTokens,
		Reasoning:  delta.ReasoningTokens,
		CacheWrite: delta.CacheWriteTokens,
		Total:      delta.TotalTokens,
		Calls:      delta.CallCount,
	}
	if rec != nil {
		ev.Session = rec.UUID()
		ev.Project = rec.Project()
		ev.Model = rec.Model()
	}
	usage.RecordEvent(ev)
}
