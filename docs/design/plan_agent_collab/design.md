# Plan-Agent Collaboration Design

> **Date**: 2026-03-29
> **Status**: Implemented
> **Depends on**: Phase 1 plan mode (complete), existing TodoStore

## 1. Problem Statement

Phase 1 plan mode is implemented: Ctrl+P switches the agent to a read-only system prompt with reduced tools. But the two modes are **disconnected** — the plan output is just assistant text in conversation history, with no structured handoff to execution.

Current gaps:
- **No completion signal**: Agent finishes planning silently (TUI shows `AgentDoneMsg`). No explicit "my plan is ready for review."
- **No approval flow**: User cannot approve/reject/edit a plan before execution begins.
- **No plan-to-execution bridge**: Switching back to normal mode loses the plan's structure. The agent must re-read conversation to recall its own plan.
- **No progress tracking**: TodoStore exists but isn't populated from plan steps. User manually expects the agent to create todos.
- **No execution context**: In normal mode, the agent has no persistent reminder of the approved plan.

The result: user toggles Ctrl+P → agent plans → user reads plan in chat → user toggles Ctrl+P back → user re-types "go ahead" → agent improvises from memory. This is fragile and error-prone.

## 2. Goals & Non-Goals

**Goals**
- Agent calls `submit_plan` to signal plan completion → triggers approval flow
- TUI shows plan in a review UI (scrollable, with approve/reject keybindings)
- Approved plan automatically populates TodoStore
- Approved plan injected as execution context via reminder middleware
- Mode transitions: Planning → Approval → Executing → Normal

**Non-Goals**
- Agent-initiated plan mode (agent decides "this needs a plan") — future v2
- Plan file persistence to `~/.jcoding/plans/` — future, in-memory only for now
- Parallel subagent exploration during planning — future
- Plan editing in external `$EDITOR` — future

## 3. Current State

### Mode switching infrastructure (complete)
- `tui.AgentMode`: `ModeNormal`, `ModePlanning`, `ModeExecuting` (defined but `ModeExecuting` unused)
- `planModeCh`: channel from TUI → main goroutine for mode changes
- `applyModeSwitch()`: rebuilds agent with different prompt/tools
- `buildPlanTools()`: read-only tool set (read, execute, grep, todowrite, todoread)

### TUI message types (defined, unused)
- `PlanApprovalMsg{PlanContent, PlanPath}` — ready for approval flow
- `PlanApprovedMsg{}`, `PlanRejectedMsg{Feedback}` — ready
- `planResponseCh` / `PlanResponse{Approved, Feedback}` — channel ready
- `GetPlanResponseChannel()` — accessor ready

### TodoStore (complete, reusable)
- Full-replacement semantics via `Update([]TodoItem)`
- Summary and progress tracking
- Reminder middleware already checks `HasIncomplete()`

### Runner (no plan awareness)
- `runner.Run()` processes agent turn, sends events to TUI
- No plan detection, no approval trigger, no execution context

## 4. Proposed Design

### 4.1 State Machine

```
  ┌───────────┐    Ctrl+P     ┌───────────┐
  │  NORMAL   │──────────────→│ PLANNING   │
  │ (all tools│←──────────────│ (read-only)│
  └───────────┘    Ctrl+P     └─────┬──────┘
        ↑                           │ agent calls submit_plan
        │                           ▼
        │                    ┌──────────────┐
        │                    │  APPROVAL    │ ← TUI shows plan
        │                    │ (y/n/e keys) │
        │                    └──┬────┬──────┘
        │            approve ───┘    └─── reject
        │                ↓                  ↓
        │         ┌──────────────┐    back to PLANNING
        │         │  EXECUTING   │    (with rejection feedback)
        │         │ (all tools   │
        │         │  + plan ctx) │
        │         └──────┬───────┘
        │                │ all todos complete / user Ctrl+P
        └────────────────┘
```

### 4.2 PlanStore

New `PlanStore` in `internal/tools/plan_store.go` — thread-safe in-memory storage for the active plan:

```go
type PlanStore struct {
    mu       sync.RWMutex
    title    string
    content  string     // full markdown
    status   PlanStatus // draft | submitted | approved | rejected
}

type PlanStatus string
const (
    PlanDraft     PlanStatus = "draft"
    PlanSubmitted PlanStatus = "submitted"
    PlanApproved  PlanStatus = "approved"
    PlanRejected  PlanStatus = "rejected"
)
```

Methods: `SetDraft(title, content)`, `Submit()`, `Approve()`, `Reject(feedback)`, `Content()`, `Status()`, `Clear()`.

The `PlanStore` is created in `main.go` and shared with `Env`, the `submit_plan` tool, and the reminder middleware.

### 4.3 `submit_plan` Tool

New tool available **only in plan mode** (`buildPlanTools()`):

```go
// internal/tools/plan_submit.go

type submitPlanInput struct {
    Title   string `json:"title"`    // One-line plan title
    Content string `json:"content"`  // Full markdown plan
}
```

Behavior:
1. Stores plan in `PlanStore` with status `submitted`
2. Sends `PlanApprovalMsg{PlanContent, PlanPath}` to TUI via `tea.Program`
3. **Blocks** waiting on `planResponseCh` for user decision
4. If approved: returns `"Plan approved. You may now switch to execution mode."` (but mode switch happens in main goroutine)
5. If rejected: returns `"Plan rejected. Feedback: {feedback}. Please revise your plan and submit again."`

The blocking behavior is important — it keeps the agent paused while the user reviews. The agent's next action depends on the approval result.

### 4.4 Plan Approval TUI

When `PlanApprovalMsg` arrives in the TUI:

1. Switch to a **plan review view** (new TUI state):
   - Plan content rendered as markdown in a scrollable viewport
   - Bottom bar shows keybindings: `y: approve  |  n: reject  |  Esc: cancel`
2. On `y`/`Enter`: send `PlanResponse{Approved: true}` to `planResponseCh`
3. On `n`: prompt for optional rejection feedback, then send `PlanResponse{Approved: false, Feedback: "..."}`
4. On `Esc`: treat as rejection with no feedback

TUI state field: `planReviewActive bool` / `planReviewContent string`

### 4.5 Approval → Execution Transition (Main Goroutine)

The `submit_plan` tool handles the channel communication. But the mode switch needs to happen in the main goroutine. Two approaches:

**Approach A: Tool triggers mode switch directly**
The `submit_plan` tool, on approval, sends a mode switch to `planModeCh`. But this creates a dependency from tool → TUI channels, which is architecturally messy.

**Approach B: Main goroutine monitors PlanStore status** ✓
After each `runner.Run()` in plan mode, the main goroutine checks `PlanStore.Status()`:
- If `approved`: auto-switch to `ModeExecuting`, populate todos, inject plan context
- If `rejected`: stay in `ModePlanning`, agent already received feedback from tool

This keeps the main goroutine as the single controller of mode transitions.

```go
// In the main goroutine, after runner.Run() returns:
if agentMode == tui.ModePlanning && planStore.Status() == tools.PlanApproved {
    // 1. Extract todos from plan
    todos := extractTodosFromPlan(planStore.Content())
    env.TodoStore.Update(todos)
    p.Send(tui.TodoUpdateMsg{})
    
    // 2. Switch to executing mode
    applyModeSwitch(tui.ModeExecuting)
    
    // 3. Auto-send execution prompt
    history = append(history, schema.UserMessage(
        "Your plan has been approved. Execute it step by step, tracking progress with the todo list.",
    ))
    resp := runner.Run(ctx, ag, history, p, rec, env.TodoStore, langfuseTracer)
    // ...
}
```

### 4.6 Plan-to-Todo Bridge

Parse the `## Plan` or `## Steps` section from plan content:

```go
func extractTodosFromPlan(planContent string) []tools.TodoItem {
    // Find "## Plan" or "## Steps" section
    // Extract lines matching: /^\d+\.\s+/ or /^- \[[ x]\]\s+/
    // Create TodoItem per match with status=pending
}
```

Example input:
```markdown
## Plan
1. Add auth middleware in `internal/server/middleware.go`
2. Create JWT token service in `internal/auth/jwt.go`
3. Update route registration to use auth middleware
4. Add login endpoint
5. Write tests
```

Output: 5 `TodoItem` with `status: "pending"`.

### 4.7 Execution Context via Reminder Middleware

The existing `ReminderMiddleware` in `internal/agent/reminder.go` already fires before each model call. Extend its config:

```go
type ReminderConfig struct {
    TodoStore    *tools.TodoStore
    PlanStore    *tools.PlanStore  // NEW
    EnvLabel     string
    IsRemote     bool
    ContextLimit int
}
```

When `PlanStore.Status() == PlanApproved` and mode is `ModeExecuting`, inject:

```
[Executing Approved Plan]
You are executing a user-approved plan. Follow it closely.

Plan:
{plan content, possibly truncated to first ~2000 chars}

Track your progress using the todo list. If you need to deviate significantly, explain why.
```

This keeps the plan visible to the agent throughout execution without relying on conversation history alone.

### 4.8 Execution → Normal Transition

When `ModeExecuting` and all todos are completed (or cancelled), the main goroutine:
1. Switches to `ModeNormal`
2. Clears the `PlanStore`
3. Agent sends a summary
4. User regains normal mode

Or: the user presses Ctrl+P at any time to manually exit executing mode.

### 4.9 System Prompt for Executing Mode

A brief addition to the normal system prompt (or a separate `execute.md`):

```
You are executing an approved plan. Your todo list has been pre-populated with the plan steps.
Work through them one by one. Use todoread to check remaining tasks.
Mark each task complete as you finish it.
If you need to deviate from the plan, explain your reasoning.
```

Since ModeExecuting uses the full tool set (same as normal), we reuse the normal system prompt but append the execution context via the reminder middleware. No separate template needed.

## 5. Data Flow Summary

```
User Ctrl+P → TUI → planModeCh → main: applyModeSwitch(Planning)
                                        ↓
User prompt → promptCh → main → runner.Run(planAgent)
                                        ↓
                              Agent explores, calls submit_plan
                                        ↓
                              submit_plan → PlanStore.Submit()
                                         → p.Send(PlanApprovalMsg)
                                         → blocks on planResponseCh
                                        ↓
TUI shows plan review ← PlanApprovalMsg
User presses 'y'     → planResponseCh → submit_plan unblocks → returns to agent
                                        ↓
                              PlanStore.Approve()
                              runner.Run() returns
                                        ↓
                              main: detects PlanApproved
                                    extractTodosFromPlan → TodoStore
                                    applyModeSwitch(Executing)
                                    auto-prompt: "Execute the plan"
                                        ↓
                              runner.Run(normalAgent + plan reminder)
                                        ↓
                              Agent executes step by step
                              TodoStore tracks progress
                                        ↓
                              All todos done → main: applyModeSwitch(Normal)
                                               PlanStore.Clear()
```

## 6. File Changes Summary

| File | Change |
|------|--------|
| `internal/tools/plan_store.go` | **NEW** — `PlanStore` type |
| `internal/tools/plan_submit.go` | **NEW** — `submit_plan` tool |
| `internal/tools/plan_parse.go` | **NEW** — `ExtractTodosFromPlan()` |
| `internal/tools/ask_user.go` | **NEW** — `ask_user` tool with optional choices |
| `cmd/coding/main.go` | Add PlanStore init, submit_plan/ask_user deps, channel bridging, post-run plan status check, auto-execute on approval |
| `internal/tui/tui.go` | Plan review state fields (`planReviewActive`, etc.), ask_user state fields (`askUserActive`, etc.), key handlers for both, View routing |
| `internal/tui/pickers.go` | `planReviewView()` — scrollable plan review dialog; `askUserView()` — question dialog with selectable options + free text |
| `internal/tui/messages.go` | `AskUserQuestionMsg`, `AskUserResponse`, `askUserResponseCh`, `GetAskUserResponseChannel()` |
| `internal/agent/reminder.go` | PlanStore ref in ReminderConfig, plan context injection in execution mode |
| `internal/prompts/reminders.go` | `PlanContent` field in ReminderContext, `plan_execution` reminder |
| `internal/prompts/plan.md` | Updated to reference `submit_plan` and `ask_user` tools |
| `internal/prompts/system.md` | Added `ask_user` tool description |

## 7. Alternatives Considered

### A. Plan as a tool with start/update/finish actions (draft design)
The original draft design proposed a `plan` tool with three actions. Rejected because:
- `start` is redundant (mode switch already signals planning)
- `update` adds complexity (agent can just think and produce plan in one shot)
- Three-action API is more for the agent to learn vs. single `submit_plan`

### B. No tool — detect plan in assistant text
Parse the agent's text output for the plan structure. Rejected because:
- Fragile (model may not produce exact format)
- No explicit "I'm done" signal
- No clean blocking point for approval flow

### C. Plan mode uses a subagent
The original (pre-Phase 1) approach. Rejected previously because:
- Isolated context (subagent can't see conversation history)
- No visible streaming output
- Extra complexity

## 8. Open Questions

1. **Rejection → replanning**: Should the agent get the full rejection feedback as a user message, or as a tool result? Tool result (from `submit_plan` returning the feedback) seems cleaner.

2. **Partial approval**: Should the user be able to approve part of a plan? For v1, no — approve or reject the whole thing.

3. **Plan editing**: The draft design allows `e` to open `$EDITOR`. Worth including in v1? It requires plan file persistence (currently in-memory). Defer to v2.

4. **Conversation history in executing mode**: Should we carry the planning conversation into execution, or start fresh? Carrying it over means the agent has context but also noise. Recommendation: carry over, rely on summarization middleware to compact.

5. **Auto-execute vs. manual prompt**: After approval, should the main goroutine auto-send "execute the plan" or wait for user input? Recommendation: auto-send, since the user already approved.
