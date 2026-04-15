# System Reminders

> **Implemented**: 2026-03-29, Eino v0.8.5
> **Priority**: P2

## 1. Problem

Deep in a long agent loop the model loses track of pending todos, token budget pressure, and repeating tool failures. Conditional mid-loop reminders inject targeted context exactly when needed.

## 2. Implementation

### 2.1 `internal/prompts/reminders.go`

```go
type ReminderContext struct {
    Iteration         int
    TokensUsed        int64
    ContextLimit      int
    HasIncompleteTodo bool
    IncompleteTodoN   int
    ConsecutiveErrors int
    EnvLabel          string
    IsRemote          bool
    PlanContent       string // non-empty when executing an approved plan
}

func CollectReminders(rc *ReminderContext) []string  // returns fired reminder messages
func FormatReminders(msgs []string) string           // joins with "[System Reminder]\n" header
```

### 2.2 Built-in Reminders

| Name | Condition | Message |
|------|-----------|---------|
| `plan_execution` | `PlanContent != ""` | Injects the approved plan (truncated to 2000 chars) + execution instructions |
| `todo_check` | has incomplete todos AND iteration > 5 | `"You have N incomplete todo(s). Check your task list before continuing."` |
| `token_warning` | tokens > 60% of limit | `"Context is X% full. Keep responses concise."` |
| `token_critical` | tokens > 85% of limit | `"Context is X% full. Wrap up the current task promptly."` |
| `tool_error_streak` | ≥ 2 consecutive `[tool error]` / `Tool execution failed:` results | `"Two or more tool calls have failed in a row. Try a different approach."` |

Deferred (not implemented): `long_run` (iteration > 20 with no active todos), `remote_env` (first iteration on SSH).

### 2.3 Reminder Middleware

`internal/agent/reminder.go` implements `ChatModelAgentMiddleware`:

```go
type ReminderConfig struct {
    TodoStore    *tools.TodoStore
    PlanStore    *tools.PlanStore
    EnvLabel     string
    IsRemote     bool
    ContextLimit int
}

type reminderMiddleware struct {
    *adk.BaseChatModelAgentMiddleware
    cfg               ReminderConfig
    iteration         int
    consecutiveErrors int
}

func (m *reminderMiddleware) BeforeModelRewriteState(
    ctx context.Context,
    state *adk.ChatModelAgentState,
    mc *adk.ModelContext,
) (context.Context, *adk.ChatModelAgentState, error) {
    m.iteration++
    m.updateErrorStreak(state)
    // build ReminderContext, call CollectReminders, append system message
}
```

The injected message has role `schema.System` and is appended to `state.Messages` before the model call.

`updateErrorStreak` scans backward through `state.Messages` — the first `schema.Tool` message found either increments or resets `consecutiveErrors` based on whether its content starts with an error prefix.

### 2.4 Integration

```go
// cmd/coding/main.go — createAgent()
reminderMw := agent.NewReminderMiddleware(agent.ReminderConfig{
    TodoStore:    env.TodoStore,
    PlanStore:    planStore,
    EnvLabel:     env.Exec.Label(),
    IsRemote:     env.IsRemote(),
    ContextLimit: contextLimit,
})
handlers = append(handlers, reminderMw) // innermost of the outer three
```

Handler order (outermost first): summarization → reduction → **reminder** → approval.

Reminder is placed between reduction and approval so reminder messages are not cleared by the reduction middleware, and approval sees them as clean system messages.

## 3. Files

| File | Role |
|------|------|
| `internal/prompts/reminders.go` | `ReminderContext`, reminder rules, `CollectReminders`, `FormatReminders` |
| `internal/agent/reminder.go` | `reminderMiddleware`, `NewReminderMiddleware`, `ReminderConfig` |
