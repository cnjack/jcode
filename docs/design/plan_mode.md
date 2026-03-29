# Plan Mode

> **Implemented**: 2026-03-29, Eino v0.8.5
> **Priority**: P0
> **Note**: The original draft proposed a `plan` agent tool with `start/update/finish` actions. The actual implementation uses program-controlled mode switching instead — the agent writes the plan as its final text response, and the program auto-triggers the review dialog.

## 1. Problem

For non-trivial tasks the agent would immediately start editing files without verifying its understanding or checking for existing patterns. This led to wasted effort and mis-aligned implementations.

## 2. Implementation

### 2.1 Mode State Machine

```
User enables plan mode (TUI toggle)
              │
              ▼
    ┌──────────────────┐
    │   PLANNING       │  agent explores codebase, writes plan as final response
    │  (read-only tools)│
    └──────────┬───────┘
               │ runner.Run() returns
               ▼
    ┌──────────────────┐
    │  PLAN REVIEW     │  inline TUI prompt: Approve / Reject / Dismiss
    └──────┬─────┬─────┘
           │     │
      approved  rejected (+ optional feedback)
           │     │
           │     └──► agent re-runs with feedback
           ▼
    ┌──────────────────┐
    │   EXECUTING      │  full tools; todos auto-populated from plan
    └──────────────────┘
           │
    all todos complete
           ▼
    ┌──────────────────┐
    │   NORMAL         │  planStore cleared
    └──────────────────┘
```

### 2.2 Plan Mode Tools (`buildPlanTools`)

In `ModePlanning`, the agent receives a restricted tool set:

```go
func buildPlanTools() []tool.BaseTool {
    return []tool.BaseTool{
        env.NewReadTool(),
        env.NewExecuteTool(nil), // no background in plan mode
        env.NewGrepTool(),
        env.NewTodoWriteTool(), env.NewTodoReadTool(),
        tools.NewAskUserTool(askUserDeps),
    }
}
```

No write/edit tools, no subagent, no background execution. The system prompt (`internal/prompts/plan.md`) explicitly lists only these tools and instructs the agent to output the plan as its final response.

### 2.3 Plan Review: `handlePlanCompletion`

After `runner.Run()` returns in plan mode, `handlePlanCompletion(resp)` in `cmd/coding/main.go` handles the full review cycle:

```
1. planStore.Submit("Plan", resp)      // store agent's response as plan
2. p.Send(PlanApprovalMsg{...})        // show inline review prompt in TUI
3. planResp := <-tui.GetPlanResponseChannel()  // block for user response
4a. approved → planStore.Approve()
             ExtractTodosFromPlan(content) → TodoStore.Update()
             applyModeSwitch(ModeExecuting)
             runner.Run(execPrompt)    // auto-start execution
4b. rejected → planStore.Reject(feedback)
             append feedback as user message
             runner.Run()              // agent re-plans
             handlePlanCompletion(newResp)  // recurse
```

### 2.4 PlanStore

`internal/tools/plan_store.go` — thread-safe in-memory store:

```go
type PlanStatus string
const (
    PlanDraft     PlanStatus = "draft"
    PlanSubmitted PlanStatus = "submitted"
    PlanApproved  PlanStatus = "approved"
    PlanRejected  PlanStatus = "rejected"
)

type PlanStore struct { /* mu, title, content, status, feedback */ }
```

Key methods: `Submit`, `Approve`, `Reject`, `Clear`, `Content`, `HasApprovedPlan`.

### 2.5 Todo Extraction

`internal/tools/plan_parse.go` — `ExtractTodosFromPlan(content)` parses the `## Plan` section for numbered steps (`1. ...`) or markdown checkboxes (`- [ ] ...`):

```go
func ExtractTodosFromPlan(planContent string) []TodoItem
```

On approval the extracted todos are loaded into `env.TodoStore` and a `TodoUpdateMsg` is sent to the TUI to update the todo bar.

### 2.6 TUI Plan Review

The plan review is rendered as an inline bottom prompt (not fullscreen) so the user can see the chat content while deciding. Located in `internal/tui/pickers.go` — `planReviewPromptView()`.

Three options (keyboard shortcuts):
- `y` / `Enter` on Approve → `PlanResponse{Approved: true}`
- `n` / `Enter` on Reject → enter feedback mode in textarea
- `Esc` / `Enter` on Dismiss → `PlanResponse{Approved: false}`

### 2.7 Plan Execution Reminder

During `ModeExecuting`, the reminder middleware injects the plan content before every model call (see [system_reminders.md](system_reminders.md) — `plan_execution` reminder). This keeps the plan in the agent's attention throughout execution.

## 3. Files

| File | Role |
|------|------|
| `cmd/coding/main.go` | `buildPlanTools()`, `handlePlanCompletion()`, `applyModeSwitch()` |
| `internal/tools/plan_store.go` | `PlanStore` type |
| `internal/tools/plan_parse.go` | `ExtractTodosFromPlan()` |
| `internal/prompts/plan.md` | Plan mode system prompt |
| `internal/tui/pickers.go` | `planReviewPromptView()` |
| `internal/tui/messages.go` | `PlanApprovalMsg`, `PlanResponse`, `GetPlanResponseChannel()` |
| `internal/agent/reminder.go` | `plan_execution` reminder context |

## 4. What Differs from Original Draft

The original draft proposed:
- A `plan` agent tool with `start/update/finish` actions (not implemented)
- Plan files stored in `~/.jcoding/plans/<session-id>.md` (not implemented; PlanStore is in-memory)
- Agent-initiated entry into plan mode (not implemented; user toggles mode via TUI)

The key insight that changed the design: the agent itself has no need to "call submit" — the program can detect when the agent finishes in plan mode and auto-trigger the review. This removes a tool, simplifies the agent's task, and avoids the blocking-channel complexity of the original design.
