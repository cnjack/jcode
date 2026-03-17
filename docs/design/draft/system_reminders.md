# System Reminders

## 1. Problem Statement

When a model is deep in a long agent loop, it can lose track of important context — pending todos, token budget pressure, the remote environment it is operating in, or a repeating pattern of tool failures. Claude Code addresses this with ~40 conditional "system reminders" that are injected into the message stream at key moments during the agent loop. The `coding` project currently has zero equivalent mechanism.

Concrete failure cases observed today:
- Agent completes a long refactor run, replies "Done", and never marks todos complete — because it forgot they existed after 30 iterations.
- Agent in an SSH session refers to a local file path that doesn't exist on the remote machine.
- Agent repeatedly calls a tool that keeps returning the same error, wasting iterations instead of changing approach.
- Agent generates an increasingly verbose response as context fills, then the next call overflows the context window.

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

The `BeforeModelRewriteState` hook in Eino's `ChatModelAgentMiddleware` interface provides exactly this capability but is unused.

## 4. Proposed Design

### 4.1 New File: `internal/prompts/reminders.go`

```go
// ReminderContext carries the runtime state needed to evaluate reminder conditions.
type ReminderContext struct {
    Iteration        int
    TokensUsed       int64
    ContextLimit     int
    TodoStore        *tools.TodoStore
    ConsecutiveErrors int   // consecutive tool [tool error] results
    EnvLabel         string
    IsRemote         bool
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

### 4.3 New File: `internal/agent/reminder_middleware.go`

A `ChatModelAgentMiddleware` that evaluates reminders before each model call. It maintains internal loop state (iteration counter, consecutive error counter).

```go
type reminderMiddleware struct {
    *adk.BaseChatModelAgentMiddleware
    todoStore   *tools.TodoStore
    envLabel    string
    isRemote    bool
    contextLimit int

    iteration         int
    consecutiveErrors int
    lastErrorResult   string
}

func NewReminderMiddleware(todoStore *tools.TodoStore, envLabel string, isRemote bool, contextLimit int) *reminderMiddleware

func (m *reminderMiddleware) BeforeModelRewriteState(
    ctx context.Context,
    state *adk.ChatModelAgentState,
    mc *adk.ModelContext,
) (context.Context, *adk.ChatModelAgentState, error) {
    m.iteration++
    m.updateErrorStreak(state)

    rc := &prompts.ReminderContext{
        Iteration:         m.iteration,
        TokensUsed:        internalmodel.TokenTracker.PromptTokens, // atomic read
        ContextLimit:      m.contextLimit,
        TodoStore:         m.todoStore,
        ConsecutiveErrors: m.consecutiveErrors,
        EnvLabel:          m.envLabel,
        IsRemote:          m.isRemote,
    }

    msgs := prompts.CollectReminders(rc)
    if len(msgs) > 0 {
        text := "[System Reminder]\n" + strings.Join(msgs, "\n")
        state.Messages = append(state.Messages, schema.SystemMessage(text))
    }
    return ctx, state, nil
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

// Handlers ordering (outermost first):
Handlers: []adk.ChatModelAgentMiddleware{
    reminderMW,        // reminder injection before each model call
    summarizationMW,   // context compression (see context_management design)
    reductionMW,       // tool output truncation
    &safeToolMiddleware{},
},
```

`reminderMW` is outermost so reminder messages are appended to the already-summarized state. If it were placed inside `summarizationMW`, reminders could be compressed away.

### 4.6 Reminder Injection Flow

```
Each model call iteration:
        │
        ▼
┌───────────────────────┐
│  BeforeModelRewrite    │
│  (reminderMiddleware)  │
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
The runner only calls the agent once per user turn, not per model iteration. Reminders need to fire within an agent turn (every model call), not between turns. The `BeforeModelRewriteState` hook is the correct insertion point.

### Use a single always-on reminder with all conditions checked inside
Possible, but would produce noisy repetitive messages even when nothing actionable is happening. The conditional model fires each reminder independently, keeping the injection minimal.

## 6. Migration & Compatibility

- Two new files: `internal/prompts/reminders.go` and `internal/agent/reminder_middleware.go`.
- `internal/agent/agent.go` updated to construct and include `reminderMW`.
- `cmd/coding/main.go` must pass `envLabel` and `isRemote` (both already available) when constructing the middleware.
- No config changes, no TUI changes, no session format changes.
- When the environment changes (SSH connect/disconnect), the agent is recreated via `createAgent()` — the new `reminderMiddleware` will receive the updated `envLabel` and `isRemote` values automatically.

## 7. Open Questions

1. **System message placement**: Does the Eino `ChatModelAgentState.Messages` slice allow inserting system messages at arbitrary positions? Or must they be at the head? The Eino docs should clarify this before implementation.
2. **Provider compatibility list**: Which provider base URLs should be tagged as strict-alternation and receive user-message fallback? Currently only `api.anthropic.com` is a known candidate.
3. **Consecutive error detection**: Scanning `state.Messages` for `[tool error]` strings is fragile if the user's own files happen to contain that text. A cleaner approach would be a counter maintained by `safeToolMiddleware` and passed via context. Should this be an open design point for the `middleware_pipeline` design?
