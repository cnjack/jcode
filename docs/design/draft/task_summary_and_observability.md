# Task Summary & Observability

> **Last verified**: 2026-03-28 against codebase at `github.com/cloudwego/eino v0.7.37`
> **Priority**: P3 — Low (existing Langfuse + TokenTracker + debug.log already cover most needs)
> **Scope reduction**: Task summary generation is nice but not critical. The Eino callback handler for debug logging is the only high-value item. Summary generation is **deferred** until context compaction is in place (they share the summarization infrastructure).

## 1. Problem Statement

Two related gaps make long sessions opaque and hard to resume:

**Gap 1 — No task summary**: When the agent completes a multi-step task (creating files, refactoring, fixing bugs), it just replies "Done." There is no structured record of what was accomplished, what files changed, or what the user should be aware of. When the user runs `coding --resume <uuid>` to continue the session later, they are dumped back into raw conversation history with no quick orientation.

**Gap 2 — Observability is manual and incomplete**: The current Langfuse integration in `internal/telemetry/langfuse.go` hooks `BeforeChatModel` / `AfterChatModel` via `adk.AgentMiddleware`. It does not cover:
- Tool call durations
- Individual model-call latency by iteration
- Error events per component

Eino provides a `callbacks.Handler` interface and a `utils/callbacks.HandlerHelper` builder that can register typed handlers for different component lifecycles (model calls, tool calls, errors, streaming) without manual wiring.

**2026 AI Landscape Note**: Task summaries are standard in modern AI coding agents. Claude Code generates structured summaries, Cursor tracks file changes in its sidebar. Observability has also matured — OpenTelemetry-based tracing is increasingly common. The debug-log approach here provides a solid foundation that can be extended to structured tracing later.

## 2. Goals & Non-Goals

**Goals**
- Generate a structured task-completion summary when a session's todos are all completed.
- Record the summary in the session JSONL so `--resume` can display it.
- Add an Eino-native `callbacks.Handler` for debug-log observability.

**Non-Goals**
- Does not change the TUI beyond possibly displaying the summary at the end of a run (optional, low priority).
- The summary is a best-effort enhancement — failure to generate it must not block the normal run completion.
- Callback observability targets the debug log (`~/.jcoding/debug.log`) only; external systems like Prometheus are out of scope.
- Does not replace or duplicate Langfuse — both can coexist.

## 3. Current State

### Task completion

`internal/runner/runner.go` — `Run()`:

```go
// After the completion guard loop:
promptTokens, completionTokens, totalTokens := internalmodel.GetTokenUsage()
p.Send(tui.TokenUpdateMsg{
    PromptTokens:      promptTokens,
    CompletionTokens:  completionTokens,
    TotalTokens:       totalTokens,
    ModelContextLimit: modelContextLimit(),
})
p.Send(tui.AgentDoneMsg{})
return resp
```

The completion guard logic re-runs the agent up to 3 times if `todoStore.HasIncomplete()` is true, appending `todoStore.IncompleteSummary()` as a user message. But there is no summary step after completion.

### Session recording

`internal/session/session.go` defines entry types:

```go
const (
    EntrySessionStart EntryType = "session_start"
    EntryUser         EntryType = "user"
    EntryAssistant    EntryType = "assistant"
    EntryToolCall     EntryType = "tool_call"
    EntryToolResult   EntryType = "tool_result"
)
```

No summary entry type exists.

### Observability

`internal/telemetry/langfuse.go` intercepts two points via `adk.AgentMiddleware`: `BeforeChatModel` creates a Langfuse generation span, and `AfterChatModel` ends it. The middleware uses a `sync.Mutex` to track `pendingGenID`. Tool-call timing and errors are not captured.

```go
func (t *LangfuseTracer) AgentMiddleware() adk.AgentMiddleware {
    return adk.AgentMiddleware{
        BeforeChatModel: func(ctx context.Context, state *adk.ChatModelAgentState) error {
            // Creates a Langfuse generation span
        },
        AfterChatModel: func(ctx context.Context, state *adk.ChatModelAgentState) error {
            // Ends the generation span
        },
    }
}
```

## 4. Proposed Design

### 4.1 Task Completion Summary

#### Trigger condition

Generate a summary at the end of `runner.Run()` if **all** of the following are true:
1. `todoStore.HasItems()` is true (at least one todo was created this turn).
2. `!todoStore.HasIncomplete()` (all todos are completed or cancelled).
3. The total assistant-turn count in `history` is ≥ 3 (exclude trivial single-exchange tasks).

This avoids generating a summary for simple Q&A turns.

#### New file: `internal/runner/summary.go`

```go
// Summarize asks the agent to produce a concise structured summary of the
// completed task. It appends a summary-request message and runs a single
// agent iteration with tools disabled.
//
// Returns empty string on any error — summary generation is best-effort.
func Summarize(
    ctx context.Context,
    ag *adk.ChatModelAgent,
    history []adk.Message,
    p *tea.Program,
    rec *session.Recorder,
) string
```

The summary request message appended to history:

```
Please provide a brief structured summary of the work just completed.
Use this format (markdown):

## Summary
### What was done
- ...
### Files changed
- ...
### Notes
- ...

Be concise. Do not use tools.
```

The agent's response is streamed to the TUI as normal `AgentTextMsg` events (the user sees it as the final reply), recorded as an `EntryAssistant` entry, and also recorded as the new `EntrySummary` entry.

#### Updated `runner.Run()`

```go
// After completion guard, before sending AgentDoneMsg:
if shouldSummarize(todoStore, history) {
    summary := summary.Summarize(ctx, ag, history, p, rec)
    _ = summary // used only for recording; already streamed to TUI
}

p.Send(tui.TokenUpdateMsg{...})
p.Send(tui.AgentDoneMsg{})
```

### 4.2 Session Summary Entry Type

#### `internal/session/session.go`

```go
const EntrySummary EntryType = "session_summary"
```

```go
// RecordSummary appends a session_summary entry.
func (r *Recorder) RecordSummary(summary string) {
    _ = r.writeEntry(Entry{Type: EntrySummary, Content: summary})
}
```

No other changes to the JSONL schema. Existing `LoadSession` and `ReconstructHistory` code skips unknown entry types by design.

#### Resume display

`session.ReconstructHistory` already skips non-user/assistant entries. A future enhancement could prepend the most recent `session_summary` as a system message when resuming — this is intentionally left as an open question rather than in scope here.

### 4.3 Eino Callback Observability

#### New file: `internal/telemetry/callbacks.go`

Eino v0.7.37 provides `callbacks.Handler` and `callbacks.AppendGlobalHandlers()` for process-global callback registration. The `HandlerHelper` builder is located at `github.com/cloudwego/eino/utils/callbacks` and allows registering typed handlers for specific components.

```go
package telemetry

import (
    "context"
    "github.com/cloudwego/eino/callbacks"
    eutils "github.com/cloudwego/eino/utils/callbacks"
    "github.com/cloudwego/eino/components/model"
    "github.com/cloudwego/eino/components/tool"
    "github.com/cnjack/coding/internal/config"
)

// NewDebugLogHandler returns a callbacks.Handler that writes lifecycle events
// to the application debug log (~/.jcoding/debug.log).
func NewDebugLogHandler() callbacks.Handler {
    return eutils.NewHandlerHelper().
        ChatModel(&eutils.ModelCallbackHandler{
            OnStart: func(ctx context.Context, info *callbacks.RunInfo, input *model.CallbackInput) context.Context {
                if info != nil {
                    config.Logger().Printf("[eino] model start %s", info.Name)
                }
                return ctx
            },
            OnEnd: func(ctx context.Context, info *callbacks.RunInfo, output *model.CallbackOutput) context.Context {
                if info != nil {
                    config.Logger().Printf("[eino] model end %s", info.Name)
                }
                return ctx
            },
            OnError: func(ctx context.Context, info *callbacks.RunInfo, err error) context.Context {
                if info != nil {
                    config.Logger().Printf("[eino] model error %s: %v", info.Name, err)
                }
                return ctx
            },
        }).
        Tool(&eutils.ToolCallbackHandler{
            OnStart: func(ctx context.Context, info *callbacks.RunInfo, input *tool.CallbackInput) context.Context {
                if info != nil {
                    config.Logger().Printf("[eino] tool start %s", info.Name)
                }
                return ctx
            },
            OnEnd: func(ctx context.Context, info *callbacks.RunInfo, output *tool.CallbackOutput) context.Context {
                if info != nil {
                    config.Logger().Printf("[eino] tool end %s", info.Name)
                }
                return ctx
            },
            OnError: func(ctx context.Context, info *callbacks.RunInfo, err error) context.Context {
                if info != nil {
                    config.Logger().Printf("[eino] tool error %s: %v", info.Name, err)
                }
                return ctx
            },
        }).
        Handler()
}
```

> **API Correction**: The original draft used a generic `OnStart`/`OnEnd`/`OnError` pattern directly on `callbacks.NewHandlerHelper()`. The actual API uses typed handlers (`ModelCallbackHandler`, `ToolCallbackHandler`, etc.) registered via builder methods (`.ChatModel()`, `.Tool()`, etc.). Each handler type has component-specific input/output types.

#### Registration in `cmd/coding/main.go`

```go
import (
    "github.com/cloudwego/eino/callbacks"
)

// Near top of main, after logger setup:
callbacks.AppendGlobalHandlers(telemetry.NewDebugLogHandler())
```

`AppendGlobalHandlers` is process-global and not thread-safe — call it once during initialization. It does not need to be called again when the agent is recreated.

#### Coverage gained

| Event | Before | After |
|-------|--------|-------|
| Model call start/end | Manual (Langfuse only) | ✅ Debug log |
| Tool call start/end | ❌ Not captured | ✅ Debug log |
| Tool errors | ❌ Not captured | ✅ Debug log |
| Streaming errors | ❌ Not captured | ✅ Debug log |

Langfuse integration continues to work alongside the new callback handler — both are registered and fire independently.

### 4.4 Component Diagram

```
runner.Run()
    │
    ├─ runInner() ─ ... ─ AgentDoneMsg
    │
    ├─ completion guard (up to 3×)
    │
    ├─ shouldSummarize()?
    │    └─ YES → summary.Summarize()
    │                 ├─ append summary request to history
    │                 ├─ single ag.Run() (no tools)
    │                 ├─ stream to TUI (AgentTextMsg)
    │                 ├─ rec.RecordAssistant()
    │                 └─ rec.RecordSummary()
    │
    ├─ TokenUpdateMsg
    └─ AgentDoneMsg
```

## 5. Alternatives Considered

### Generate summary with a separate API call (not via the agent)
Rejected. Using the agent ensures the summary uses the same model (consistent style), and the result is streamed to the TUI just like any other assistant response — the user sees it naturally as the final message.

### Always generate a summary, even for short sessions
Rejected. For a two-message exchange ("fix this typo" / "done"), a summary would be condescending and wasteful. The condition gates on `todoStore.HasItems()` — summaries only appear when the task was complex enough to warrant a todo list.

### Replace Langfuse integration with Eino callbacks entirely
Rejected. Langfuse provides structured span/generation data with timing and token counts, which the debug-log callback does not. Both serve different purposes: Langfuse for production tracing, debug log for local development diagnostics.

## 6. Migration & Compatibility

- New files: `internal/runner/summary.go`, `internal/telemetry/callbacks.go`.
- `internal/session/session.go`: new `EntrySummary` constant and `RecordSummary` method — additive only.
- `internal/runner/runner.go`: `Run()` gains a summary gate at the end — 5 lines.
- `cmd/coding/main.go`: one `callbacks.AppendGlobalHandlers` call added.
- Session JSONL files written before this change are fully compatible: `ReconstructHistory` skips unknown entry types.

## 7. Codebase Verification Notes

- **Confirmed**: `runner.Run()` code matches the described current state — completion guard re-runs up to 3× with `todoStore.IncompleteSummary()`, sends `TokenUpdateMsg` and `AgentDoneMsg` at the end.
- **Confirmed**: Session entry types match: `session_start`, `user`, `assistant`, `tool_call`, `tool_result`. No `session_summary` type exists yet.
- **Confirmed**: `Langfuse AgentMiddleware()` uses `adk.AgentMiddleware` struct with `BeforeChatModel`/`AfterChatModel` hooks.
- **Confirmed**: `callbacks.AppendGlobalHandlers()` exists in `github.com/cloudwego/eino/callbacks`.
- **Confirmed**: `utils/callbacks.NewHandlerHelper()` exists at `github.com/cloudwego/eino/utils/callbacks/template.go` with the builder pattern (`.ChatModel()`, `.Tool()`, `.Handler()`).
- **Confirmed**: `ModelCallbackHandler` and `ToolCallbackHandler` have `OnStart`, `OnEnd`, `OnEndWithStreamOutput`, `OnError` fields with component-specific types.
- **Confirmed**: `todoStore.HasItems()`, `todoStore.HasIncomplete()` methods exist for the summary trigger condition.

## 8. Open Questions

1. **Summary on resume**: When `--resume <uuid>` reloads a session that has a `session_summary` entry, should it be displayed as an orientation header? This would require a TUI change (`SessionResumedMsg` could carry a `Summary string` field). Deferred to a follow-up.
2. **Disabling summary**: Should there be a config flag to disable automatic summary generation? Some users may find it adds unwanted latency at the end of every complex task.
3. **Summary tool access**: The summary request currently asks the agent to respond without using tools. Should it be allowed to call `todoread` to get the final todo state? This would make the "What was done" section more accurate.
