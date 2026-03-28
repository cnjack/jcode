# System Reminders

> **Last verified**: 2026-03-28 against codebase at `github.com/cloudwego/eino v0.7.37`
> **Priority**: P2 — Nice to have (only implement the 3 highest-value reminders)
> **Scope reduction**: Claude Code has ~40 reminders. We implement **3** for v1: `todo_check`, `token_warning`, `tool_error_streak`. The `remote_env` and `long_run` reminders are deferred.

## 1. Problem Statement

When a model is deep in a long agent loop, it can lose track of important context — pending todos, token budget pressure, the remote environment it is operating in, or a repeating pattern of tool failures. Claude Code addresses this with ~40 conditional "system reminders" that are injected into the message stream at key moments during the agent loop. The `coding` project currently has zero equivalent mechanism.

Concrete failure cases observed today:
- Agent completes a long refactor run, replies "Done", and never marks todos complete — because it forgot they existed after 30 iterations.
- Agent in an SSH session refers to a local file path that doesn't exist on the remote machine.
- Agent repeatedly calls a tool that keeps returning the same error, wasting iterations instead of changing approach.
- Agent generates an increasingly verbose response as context fills, then the next call overflows the context window.

**2026 AI Landscape Note**: System reminders (also called "nudges" or "guardrails") are a well-established pattern in production AI agents. Claude Code, Cursor, and Windsurf all use conditional mid-loop reminders. Some newer approaches use model "thinking tokens" for self-regulation, but explicit reminders remain the most reliable mechanism for stateful context injection.

## 2. Goals & Non-Goals

**Goals**
- Inject short, targeted reminder messages into the agent state before each model call, based on runtime conditions.
- Keep individual reminders small (< 30 tokens each) to minimise overhead.
- Make the reminder set extensible without changing agent or runner code.

**Non-Goals**
- Reminders are informational — they do not enforce constraints or change tool availability.
- No new tools, no TUI changes.
- No persistence — reminders are ephemeral and not recorded in session JSONL.

## 3. Current State

`internal/prompts/system.md` is rendered once at agent creation and never updated during the loop. There is no mechanism to inject context-sensitive messages between model calls.

Eino's `adk.AgentMiddleware` struct provides a `BeforeChatModel` hook (`func(context.Context, *ChatModelAgentState) error`) that is called before each model invocation. This hook receives the `ChatModelAgentState` (which contains `Messages []adk.Message`) and can modify the message list. This is the correct insertion point for reminders.

> **Note**: The original draft referenced `BeforeModelRewriteState` on a `ChatModelAgentMiddleware` interface — this does not exist in Eino v0.7.37. The correct hook is `AgentMiddleware.BeforeChatModel`.

## 4. Proposed Design

### 4.1 New File: `internal/prompts/reminders.go`

```go
// ReminderContext carries the runtime state needed to evaluate reminder conditions.
type ReminderContext struct {
    Iteration         int
    TokensUsed        int64
    ContextLimit      int
    TodoStore         *tools.TodoStore
    ConsecutiveErrors int   // consecutive tool [tool error] results
    EnvLabel          string
    IsRemote          bool
}

// Reminder is a single conditional reminder rule.
type Reminder struct {
    Name      string
    Condition func(*ReminderContext) bool
    Message   func(*ReminderContext) string
}

// CollectReminders returns the messages for all reminders whose condition is met.
func CollectReminders(rc *ReminderContext) []string
```

### 4.2 Built-in Reminders

| Name | Condition | Message template |
|------|-----------|-----------------|
| `todo_check` | TodoStore has incomplete items AND iteration > 5 | `"You have {n} incomplete todo(s). Check your task list before continuing."` |
| `token_warning` | tokens used > 60% of context limit | `"Context is {pct}% full. Keep responses concise."` |
| `token_critical` | tokens used > 85% of context limit | `"Context is {pct}% full. Wrap up the current task promptly."` |
| `long_run` | iteration > 20 AND no active todos | `"You have been running for {n} iterations. Consider whether the task needs replanning."` |
| `tool_error_streak` | ≥ 2 consecutive `[tool error]` results | `"Two or more tool calls have failed in a row. Try a different approach."` |
| `remote_env` | IsRemote == true AND iteration == 1 | `"You are operating on remote environment '{label}'. Use absolute paths appropriate for that machine."` |

Reminders are additive — all matching reminders fire in a single injected message block.

### 4.3 Reminder Middleware as `AgentMiddleware.BeforeChatModel`

The reminder logic is implemented as a closure that captures mutable state (iteration counter, error counter) and is assigned to the `BeforeChatModel` field of an `adk.AgentMiddleware` struct.

```go
// internal/agent/reminder_middleware.go

type reminderState struct {
    todoStore         *tools.TodoStore
    envLabel          string
    isRemote          bool
    contextLimit      int
    iteration         int
    consecutiveErrors int
}

func NewReminderMiddleware(todoStore *tools.TodoStore, envLabel string, isRemote bool, contextLimit int) adk.AgentMiddleware {
    rs := &reminderState{
        todoStore:    todoStore,
        envLabel:     envLabel,
        isRemote:     isRemote,
        contextLimit: contextLimit,
    }

    return adk.AgentMiddleware{
        BeforeChatModel: func(ctx context.Context, state *adk.ChatModelAgentState) error {
            rs.iteration++
            rs.updateErrorStreak(state)

            promptTokens, _, _ := internalmodel.TokenTracker.Get()

            rc := &prompts.ReminderContext{
                Iteration:         rs.iteration,
                TokensUsed:        promptTokens,
                ContextLimit:      rs.contextLimit,
                TodoStore:         rs.todoStore,
                ConsecutiveErrors: rs.consecutiveErrors,
                EnvLabel:          rs.envLabel,
                IsRemote:          rs.isRemote,
            }

            msgs := prompts.CollectReminders(rc)
            if len(msgs) > 0 {
                text := "[System Reminder]\n" + strings.Join(msgs, "\n")
                state.Messages = append(state.Messages, schema.SystemMessage(text))
            }
            return nil
        },
    }
}
```

`updateErrorStreak` scans the most recent tool result in `state.Messages` and increments or resets `consecutiveErrors`.

### 4.4 System Message vs User Message Injection

System messages are preferred (mirrors Claude Code behaviour and keeps the conversation history semantically clean). However, some providers enforce strict `system → user → assistant` alternation and reject a system message appearing mid-conversation.

Fallback rule: if the last message in `state.Messages` is already a system message, append the reminder to it. If the model name indicates a strict-alternation provider (configurable list), inject as a user message with the prefix `[SYSTEM REMINDER]` instead.

### 4.5 Integration in `agent.go`

```go
reminderMW := agent.NewReminderMiddleware(
    env.TodoStore,
    envLabel,
    env.IsRemote(),
    internalmodel.GetModelContextLimit(cfg.Model),
)

// All middlewares use the single Middlewares field:
Middlewares: []adk.AgentMiddleware{
    reminderMW,        // reminder injection before each model call
    compactionMW,      // history compaction (see context_management design)
    reductionMW,       // tool output reduction
    approvalMW,        // approval + safe tool error handling
},
```

`reminderMW` is outermost so reminder messages are appended to the already-compacted state. If it were placed inside `compactionMW`, reminders could be trimmed away.

### 4.6 Reminder Injection Flow

```
Each model call iteration:
        │
        ▼
┌───────────────────────┐
│  BeforeChatModel       │
│  (reminder middleware) │
│                        │
│  1. increment iter     │
│  2. check error streak │
│  3. CollectReminders() │
│  4. append if any      │
└──────────┬─────────────┘
           │
           ▼
   ┌───────────────┐
   │  ChatModel    │
   │  Generate()   │
   └───────────────┘
```

## 5. Alternatives Considered

### Inject reminders as part of the system prompt template (static)
Rejected. The value of reminders is that they are conditional and timely. A static prompt that says "check your todos every 5 iterations" is far less effective than an actual message that fires when there actually are incomplete todos at iteration 6.

### Implement reminders in `runner.go` by appending to `messages` before each `runInner` call
The runner only calls the agent once per user turn, not per model iteration. Reminders need to fire within an agent turn (every model call), not between turns. The `BeforeChatModel` hook is the correct insertion point.

### Use a single always-on reminder with all conditions checked inside
Possible, but would produce noisy repetitive messages even when nothing actionable is happening. The conditional model fires each reminder independently, keeping the injection minimal.

## 6. Migration & Compatibility

- Two new files: `internal/prompts/reminders.go` and `internal/agent/reminder_middleware.go`.
- `internal/agent/agent.go` updated to construct and include `reminderMW`.
- `cmd/coding/main.go` must pass `envLabel` and `isRemote` (both already available via `Env`) when constructing the middleware.
- No config changes, no TUI changes, no session format changes.
- When the environment changes (SSH connect/disconnect), the agent is recreated via `createAgent()` — the new `reminderState` will receive the updated `envLabel` and `isRemote` values automatically.

## 7. Codebase Verification Notes

- **Confirmed**: `adk.AgentMiddleware.BeforeChatModel` exists as `func(context.Context, *ChatModelAgentState) error` — this is the correct hook (not `BeforeModelRewriteState`).
- **Confirmed**: `adk.ChatModelAgentState` exists with `Messages []adk.Message` field.
- **Confirmed**: `internalmodel.TokenTracker` is global with `Get()` method returning `(prompt, completion, total int64)`.
- **Confirmed**: `tools.TodoStore` has `HasIncomplete()`, `IncompleteSummary()`, `HasItems()`, `Items()` methods.
- **Confirmed**: `Env.IsRemote()` exists in `internal/tools/env.go`.
- **Not found**: `BeforeModelRewriteState`, `ChatModelAgentMiddleware` interface, `BaseChatModelAgentMiddleware` — none exist in Eino v0.7.37.

## 8. Resolved Questions

1. **System message placement**: `ChatModelAgentState.Messages` is a `[]adk.Message` slice that can be freely modified in `BeforeChatModel`. System messages can be appended at any position.
2. **Provider compatibility**: Strict-alternation detection should be based on the `base_url` from provider config. A hardcoded list can start with known strict providers and be extended via config.
3. **Consecutive error detection**: Using `[tool error]` string matching in `state.Messages` is adequate for the initial implementation. A cleaner approach using a shared counter via context (between `safeToolMiddleware` and `reminderMiddleware`) is a valid future improvement.
