# Task Summary & Observability

## 1. Problem Statement

Two related gaps make long sessions opaque and hard to resume:

**Gap 1 — No task summary**: When the agent completes a multi-step task (creating files, refactoring, fixing bugs), it just replies "Done." There is no structured record of what was accomplished, what files changed, or what the user should be aware of. When the user runs `coding --resume <uuid>` to continue the session later, they are dumped back into raw conversation history with no quick orientation.

**Gap 2 — Observability is manual and incomplete**: The current Langfuse integration in `internal/telemetry/langfuse.go` manually hooks `BeforeChatModel` / `AfterChatModel` via `adk.AgentMiddleware`. It does not cover:
- Tool call durations
- Individual model-call latency by iteration
- Error events per component

Eino provides a first-class `callbacks.Handler` interface that fires automatically for every component lifecycle event (model calls, tool calls, errors, streaming) without any manual wiring.

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
p.Send(tui.TokenUpdateMsg{...})
p.Send(tui.AgentDoneMsg{})
return resp
```

There is no summary step.

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

`internal/telemetry/langfuse.go` manually intercepts two points: `BeforeChatModel` and `AfterChatModel`. Tool-call timing and errors are not captured.

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

```go
package telemetry

import (
    "context"
    "github.com/cloudwego/eino/callbacks"
    "github.com/cnjack/coding/internal/config"
)

// NewDebugLogHandler returns a callbacks.Handler that writes lifecycle events
// to the application debug log (~/.jcoding/debug.log).
func NewDebugLogHandler() callbacks.Handler {
    return callbacks.NewHandlerHelper().
        OnStart(func(ctx context.Context, info *callbacks.RunInfo, _ callbacks.CallbackInput) context.Context {
            if info != nil {
                config.Logger().Printf("[eino] start %s/%s", info.Component, info.Name)
            }
            return ctx
        }).
        OnEnd(func(ctx context.Context, info *callbacks.RunInfo, _ callbacks.CallbackOutput) context.Context {
            if info != nil {
                config.Logger().Printf("[eino] end   %s/%s", info.Component, info.Name)
            }
            return ctx
        }).
        OnError(func(ctx context.Context, info *callbacks.RunInfo, err error) context.Context {
            if info != nil {
                config.Logger().Printf("[eino] error %s/%s: %v", info.Component, info.Name, err)
            }
            return ctx
        }).
        Handler()
}
```

#### Registration in `cmd/coding/main.go`

```go
import "github.com/cloudwego/eino/callbacks"

// Near top of main, after logger setup:
callbacks.AppendGlobalHandlers(telemetry.NewDebugLogHandler())
```

`AppendGlobalHandlers` is process-global; it does not need to be called again when the agent is recreated.

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

## 7. Open Questions

1. **Summary on resume**: When `--resume <uuid>` reloads a session that has a `session_summary` entry, should it be displayed as an orientation header? This would require a TUI change (`SessionResumedMsg` could carry a `Summary string` field). Deferred to a follow-up.
2. **Disabling summary**: Should there be a config flag to disable automatic summary generation? Some users may find it adds unwanted latency at the end of every complex task.
3. **Summary tool access**: The summary request currently asks the agent to respond without using tools. Should it be allowed to call `todoread` to get the final todo state? This would make the "What was done" section more accurate.
