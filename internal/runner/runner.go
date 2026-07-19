package runner

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"

	internalagent "github.com/cnjack/jcode/internal/agent"
	"github.com/cnjack/jcode/internal/config"
	"github.com/cnjack/jcode/internal/handler"
	"github.com/cnjack/jcode/internal/hooks"
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
	if rec != nil {
		ctx = internalagent.WithToolObservationSink(ctx, func(observation internalagent.ToolObservation) {
			rec.RecordToolObservation(session.ToolObservation{
				Kind:                 string(observation.Kind),
				ModelRequestSeq:      observation.ModelRequestSeq,
				VisibleNames:         observation.VisibleNames,
				VisibleCount:         observation.VisibleCount,
				SchemaBytes:          observation.SchemaBytes,
				SchemaTokensEstimate: observation.SchemaTokensEstimate,
				NewlyVisibleDeferred: observation.NewlyVisibleDeferred,
				ToolCallID:           observation.ToolCallID,
				QueryMode:            observation.QueryMode,
				QueryBytes:           observation.QueryBytes,
				TermCount:            observation.TermCount,
				RequiredTermCount:    observation.RequiredTermCount,
				MaxResults:           observation.MaxResults,
				ValidatedSelectNames: observation.ValidatedSelectNames,
				UnknownSelectCount:   observation.UnknownSelectCount,
				MatchNames:           observation.MatchNames,
				NewMatchNames:        observation.NewMatchNames,
				RepeatedQuery:        observation.RepeatedQuery,
				Redundant:            observation.Redundant,
				Success:              observation.Success,
				ToolName:             observation.ToolName,
				Reason:               observation.Reason,
			})
		})
	}
	// Per-run approval meter: the approval path (blocked inside tool execution)
	// records wait time + denied verdicts into it via ctx, and runInner's
	// result emission folds them back out (pause-during-approval + denied flag).
	ctx = withApprovalMeter(ctx, newApprovalMeter())
	if tokenUsage != nil {
		ctx = internalmodel.WithTokenTracker(ctx, tokenUsage)
	}
	// Snapshot cumulative usage so we can record this turn's delta on completion,
	// and mark the per-turn baseline so the budget middleware measures THIS turn.
	var startSnap internalmodel.TokenUsageDetail
	if tokenUsage != nil {
		startSnap = tokenUsage.GetFull()
		tokenUsage.BeginTurn()
	}
	// Persist this turn's token delta on EVERY exit path — success, user
	// cancellation, or a model error — so a turn that already made billable calls
	// before stopping is not dropped from the usage log. recordUsageTurn is
	// nil-tracker and zero-delta guarded, so a no-op turn records nothing.
	defer recordUsageTurn(tokenUsage, startSnap, rec)
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

	// Unified continuation pipeline. Three mechanisms can keep the agent going
	// after it stops calling tools — incomplete-todo guard, active-goal guard, and
	// a user-configured Stop hook. They share ONE loop, ONE umbrella budget, and
	// ONE cancellation check so they can't compound into an unbounded run or
	// silently bypass each other. Precedence: todo → goal → Stop hook. The Stop
	// hook only fires once the internal guards are quiet (the agent would truly
	// stop), and carries stop_hook_active so a script can stop forcing laps.
	const (
		maxTodoRetries = 3 // sub-cap so incomplete todos can't hog the budget
		// Umbrella budget shared by todo/goal/Stop. Sized as the goal's original
		// 25 plus the todo sub-cap of 3, so merging the loops does not shrink the
		// goal's effective continuation budget.
		maxContinuations = 25 + maxTodoRetries
	)
	disp := hooks.DispatcherFromContext(ctx)
	todoUsed := 0
	stopHookActive := false
continuationLoop:
	for i := 0; i < maxContinuations; i++ {
		// User cancellation is a one-vote veto over every continuation mechanism.
		select {
		case <-ctx.Done():
			config.Logger().Printf("[runner] continuation cancelled")
			break continuationLoop
		default:
		}
		if goalStore != nil && tokenUsage != nil {
			goalStore.RecordTokens(tokenUsage.GetLastTotal())
		}

		todoIncomplete := todoStore != nil && todoStore.HasIncomplete()
		goalPrompt := ""
		if goalStore != nil && goalStore.IsActive() {
			goalPrompt = goalStore.ContinuationPrompt()
		}
		goalActive := goalPrompt != ""

		// Only consult the Stop hook when the internal guards are quiet, so it sees
		// the true end of the turn rather than a half-finished state.
		todoWantsMore := todoIncomplete && todoUsed < maxTodoRetries
		var stopBlock bool
		var stopReason string
		if !todoWantsMore && !goalActive && disp.Configured(hooks.Stop) {
			dec := disp.Fire(ctx, hooks.Stop, hooks.Payload{StopHookActive: stopHookActive})
			stopBlock = dec.Block
			stopReason = dec.Reason
		}

		var reason, banner string
		switch continuationSource(todoIncomplete, todoUsed, maxTodoRetries, goalActive, stopBlock) {
		case "todo":
			reason = todoStore.IncompleteSummary()
			banner = "\n⚠️ Incomplete todos detected, continuing...\n"
			todoUsed++
		case "goal":
			reason = goalPrompt
			banner = "\n🎯 Goal active — continuing toward objective...\n"
		case "stop":
			reason = stopReason
			if reason == "" {
				reason = "A Stop hook requested that you keep working before finishing."
			}
			banner = "\n🛑 Stop hook active — continuing...\n"
			stopHookActive = true
		default:
			break continuationLoop // nothing wants to continue → truly done
		}

		h.OnAgentText(banner)
		messages = append(messages, &schema.Message{Role: schema.Assistant, Content: pending})
		messages = append(messages, schema.UserMessage(reason))
		extra, done := runInner(ctx, ag, messages, h, rec)
		resp += extra
		pending = extra
		if done {
			return resp
		}
	}

	// Send a final token usage update before signalling done. Prefer the
	// context-local tracker (per-agent) and fall back to the passed-in one.
	tracker := tokenUsage
	if local := internalmodel.TokenTrackerFromContext(ctx); local != nil {
		tracker = local
	}
	h.OnTokenUpdate(buildTokenUsage(tracker, ctxLimit))

	// This turn's token delta is persisted by the deferred recordUsageTurn, which
	// also covers the early-return (cancel/error) paths above.
	h.OnAgentDone(nil)
	return resp
}

// continuationSource picks which mechanism drives the next continuation lap,
// encoding the precedence todo → goal → Stop hook and the todo sub-cap. It is a
// pure function so the precedence and bounding are unit-testable without a live
// agent. Returns "" when nothing should continue (the turn is truly done).
func continuationSource(todoIncomplete bool, todoUsed, todoCap int, goalActive, stopBlock bool) string {
	if todoIncomplete && todoUsed < todoCap {
		return "todo"
	}
	if goalActive {
		return "goal"
	}
	if stopBlock {
		return "stop"
	}
	return ""
}

// batchSeq issues process-wide sequence numbers for tool-call batch IDs.
// batchEpoch (process start, unix ms) is baked into every ID so batches
// recorded into the same session file across restarts can never collide.
var batchSeq atomic.Int64
var batchEpoch = time.Now().UnixMilli()

// nextBatchID returns a fresh batch ID. One assistant message = one batch.
func nextBatchID() string {
	return fmt.Sprintf("b%d-%d", batchEpoch, batchSeq.Add(1))
}

// toolMessageText returns the human/model-readable portion of a tool result.
// Enhanced tools place text next to image parts in UserInputMultiContent and
// leave Content empty; the UI and session recorder must still receive the text
// marker (for example image_ref) without ever persisting the Base64 image.
func toolMessageText(msg *schema.Message) string {
	if msg == nil {
		return ""
	}
	if msg.Content != "" {
		return msg.Content
	}
	var parts []string
	for _, part := range msg.UserInputMultiContent {
		if part.Type == schema.ChatMessagePartTypeText && part.Text != "" {
			parts = append(parts, part.Text)
		}
	}
	return strings.Join(parts, "\n")
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

	// toolStarts records when each tool call was announced so results can
	// carry a call→result latency. The event loop below is the only reader
	// and writer (single goroutine), so no locking is needed.
	type toolStart struct {
		at   time.Time
		name string
	}
	toolStarts := make(map[string]toolStart)
	// meter carries approval wait/denied outcomes from the approval path (see
	// Run) so the emitted Duration is pure execution time and denied calls are
	// flagged. emitToolResult also owns session recording so the persisted
	// entry matches the emitted event exactly (denied + adjusted duration).
	meter := approvalMeterFrom(ctx)
	emitToolResult := func(name, output, toolCallID string, err error) {
		ev := handler.ToolResultEvent{Name: name, Output: output, ToolCallID: toolCallID, Err: err}
		if started, ok := toolStarts[toolCallID]; ok {
			ev.Duration = time.Since(started.at)
			delete(toolStarts, toolCallID)
		}
		applyApprovalOutcome(&ev, meter)
		h.OnToolResult(ev)
		if rec != nil {
			rec.RecordToolResult(name, output, toolCallID, err, ev.Denied, ev.Duration)
		}
	}
	// drainDanglingToolResults backfills a result for every announced tool
	// call that never produced one (user stop, fatal tool-node failure). The
	// session must never persist a tool_call without its tool_result: the next
	// resume would reconstruct a history the model API rejects ("assistant
	// message with 'tool_calls' must be followed by tool messages..."). The
	// backfill carries no error: an interrupted call is not a failed call, and
	// a non-nil Err would paint every front-end's tool row red (raw
	// "context.Canceled" text) right next to the calm stop notice.
	drainDanglingToolResults := func() {
		for id, started := range toolStarts {
			emitToolResult(started.name, session.InterruptedToolOutput, id, nil)
		}
	}

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
			drainDanglingToolResults()
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
				drainDanglingToolResults()
				h.OnAgentDone(ctx.Err())
				return assistantText.String(), true
			}
			// Log the provider's raw payload, hand the frontends a sentence a
			// human can act on. This is the single choke point for model errors,
			// so wrapping here fixes the display in the TUI, the web UI and ACP
			// at once — and stops the next frontend from having to remember.
			config.Logger().Printf("[runner] event error: %v", event.Err)
			drainDanglingToolResults()
			h.OnAgentDone(internalmodel.WrapFriendly(event.Err, "", ""))
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
				output := toolMessageText(mo.Message)
				emitToolResult(toolName, output, mo.Message.ToolCallID, nil)
				if toolName == "todowrite" || toolName == "todoread" {
					h.OnTodoUpdate()
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
						emitToolResult(toolName, "", toolCallID, err)
						break
					}
					if chunk != nil {
						sb.WriteString(toolMessageText(chunk))
						if toolCallID == "" && chunk.ToolCallID != "" {
							toolCallID = chunk.ToolCallID
						}
					}
				}
				if toolErr == nil {
					emitToolResult(toolName, sb.String(), toolCallID, nil)
					if toolName == "todowrite" || toolName == "todoread" {
						h.OnTodoUpdate()
					}
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
			// All tool calls from this assistant message form one batch.
			indices := make([]int, 0, len(pending))
			for idx := range pending {
				indices = append(indices, idx)
			}
			sort.Ints(indices)
			if len(indices) > 0 {
				batchID := nextBatchID()
				startedAt := time.Now()
				for i, idx := range indices {
					p := pending[idx]
					toolStarts[p.id] = toolStart{at: startedAt, name: p.name}
					h.OnToolCall(handler.ToolCallEvent{
						Name:       p.name,
						Args:       p.args.String(),
						ToolCallID: p.id,
						BatchID:    batchID,
						BatchIndex: i,
						BatchSize:  len(indices),
						StartedAt:  startedAt,
					})
					if rec != nil {
						rec.RecordToolCall(p.name, p.args.String(), p.id, batchID, i, len(indices))
					}
				}
			}
		} else if mo.Message != nil {
			if len(mo.Message.ToolCalls) > 0 {
				batchID := nextBatchID()
				startedAt := time.Now()
				size := len(mo.Message.ToolCalls)
				for i, tc := range mo.Message.ToolCalls {
					toolStarts[tc.ID] = toolStart{at: startedAt, name: tc.Function.Name}
					h.OnToolCall(handler.ToolCallEvent{
						Name:       tc.Function.Name,
						Args:       tc.Function.Arguments,
						ToolCallID: tc.ID,
						BatchID:    batchID,
						BatchIndex: i,
						BatchSize:  size,
						StartedAt:  startedAt,
					})
					if rec != nil {
						rec.RecordToolCall(tc.Function.Name, tc.Function.Arguments, tc.ID, batchID, i, size)
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
		// Record cache *support* (details object seen), not just a positive hit, so
		// the stats page can show "—" vs a real 0% even on a cold-cache turn.
		CacheSeen: tracker.CacheObserved(),
	}
	if rec != nil {
		ev.Session = rec.UUID()
		ev.Project = rec.Project()
		ev.Model = rec.Model()
	}
	usage.RecordEvent(ev)
}
