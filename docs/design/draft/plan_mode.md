# Plan Mode

> **Last verified**: 2026-03-28 against codebase at `github.com/cloudwego/eino v0.7.37`
> **Priority**: P0 — Core capability gap
> **Reference**: Claude Code v2.1.86 `EnterPlanMode` tool + 5-phase plan reminder

## 1. Problem Statement

When users request non-trivial tasks (new features, refactors, multi-file changes), the agent immediately starts editing code without verifying its understanding of the codebase or the user's intent. This leads to:

- **Wasted effort**: Agent edits 10 files based on a wrong assumption, then the user says "that's not what I meant."
- **Blind spots**: Agent doesn't discover existing utilities/patterns and re-implements from scratch.
- **No alignment**: For tasks with multiple valid approaches (caching strategy, state management pattern), the agent picks one without consulting the user.

The current system prompt has `2. Plan: think before acting and break into steps` — but this is purely advisory text, not an enforceable mechanism. The agent has no way to signal "I want to plan first" and the user has no way to say "plan before you act."

**Industry context**: Claude Code's Plan Mode (5-phase with parallel Explore/Plan subagents) is its most-invested feature in v2.1.x. OpenCode/Crush does not have plan mode. This is a significant differentiator for complex task quality.

## 2. Goals & Non-Goals

**Goals**
- Add a `plan` tool that the agent calls to enter plan mode (read-only, no edits).
- In plan mode, the agent explores the codebase, writes a plan to a file, and presents it to the user for approval.
- The user can approve (agent executes), reject (agent replans), or modify (agent adjusts).
- Plan mode should be triggerable by user command (`/plan`) as well as proactively by the agent.

**Non-Goals**
- No parallel subagent execution in v1 (requires subagent infrastructure from the Sub-Agent design).
- No separate plan-mode model (uses the same model).
- No worktree isolation (plan file is a regular file in `.jcoding/`).

## 3. Current State

### Agent capabilities
- `internal/agent/agent.go`: `NewAgent()` creates a single `ChatModelAgent` with all tools enabled at all times. There is no mechanism to restrict tools during a particular phase.
- `internal/runner/runner.go`: `Run()` executes the agent once per user turn. No multi-phase execution.
- `internal/prompts/system.md`: Workflow section says "Plan: think before acting" but is not enforced.

### User interaction
- `internal/tui/tui.go`: User types messages and the agent responds. No mode switching, no plan approval UI.
- No `/plan` command or keybinding exists.

## 4. Proposed Design

### 4.1 Plan Tool

A new tool `plan` that the agent calls proactively when it decides the task needs planning. It can also be triggered by the user typing `/plan <description>`.

```go
// internal/tools/plan.go

// PlanInput is the JSON schema for the plan tool.
type PlanInput struct {
    Action string `json:"action" desc:"One of: start, update, finish" required:"true"`
    Content string `json:"content" desc:"Plan content (markdown) for start/update; empty for finish"`
}
```

**Actions:**
- `start`: Agent signals it wants to enter plan mode. Writes initial plan to `~/.jcoding/plans/<session-id>.md`. Returns instructions telling the agent it is now in plan mode (read-only).
- `update`: Agent updates the plan file with refined content. Returns confirmation.
- `finish`: Agent signals plan is complete. The plan is shown to the user in a TUI approval dialog.

### 4.2 Plan Mode State Machine

```
        User message or /plan
              │
              ▼
    ┌──────────────┐
    │   NORMAL     │ ◄─── reject / user edits
    │   (all tools)│      and re-plan
    └──────┬───────┘
           │ agent calls plan(action:"start")
           │ or user types /plan
           ▼
    ┌──────────────┐
    │  PLANNING    │ ──── agent explores (read/grep/execute read-only)
    │  (read-only) │      writes plan file via plan(action:"update")
    └──────┬───────┘
           │ agent calls plan(action:"finish")
           ▼
    ┌──────────────┐
    │  APPROVAL    │ ──── TUI shows plan, user approves/rejects
    └──────┬───────┘
           │ approved
           ▼
    ┌──────────────┐
    │  EXECUTING   │ ──── agent follows plan (all tools)
    │  (all tools) │      todo list from plan
    └──────────────┘
```

### 4.3 Read-Only Enforcement in Plan Mode

During PLANNING state, the approval middleware is augmented to auto-reject write operations:

```go
// internal/agent/agent.go — within the approval wrapper
if planState.IsPlanning() {
    if !isReadOnlyTool(input.Name) {
        return &compose.ToolOutput{
            Result: "[plan mode] You are in plan mode. Only read-only tools (read, grep, execute with safe commands, todoread, todowrite, plan) are allowed. Use plan(action:'update') to refine your plan.",
        }, nil
    }
}
```

Read-only tools: `read`, `grep`, `execute` (safe-prefix only), `todoread`, `todowrite`, `plan`.

### 4.4 Plan File Format

Stored at `~/.jcoding/plans/<session-id>.md`:

```markdown
# Plan: <task title>

## Context
<What the user asked and what was discovered during exploration>

## Approach
<Chosen approach and rationale>

## Steps
- [ ] Step 1: ...
- [ ] Step 2: ...
- [ ] Step 3: ...

## Files to modify
- `path/to/file.go` — reason
- ...

## Risks / Open questions
- ...
```

### 4.5 TUI Integration

#### New messages

```go
// internal/tui/messages.go
type PlanModeMsg struct {
    Active bool
}

type PlanApprovalMsg struct {
    PlanContent string
    PlanPath    string
}

type PlanApprovedMsg struct{}
type PlanRejectedMsg struct{ Feedback string }
```

#### Plan approval view

When `PlanApprovalMsg` arrives, the TUI shows a scrollable view of the plan with keybindings:
- `Enter` / `y`: Approve → send `PlanApprovedMsg`, agent enters EXECUTING with todos from plan.
- `n`: Reject → prompt for feedback → send `PlanRejectedMsg`, agent re-enters PLANNING.
- `e`: Edit → open plan file in `$EDITOR`, then re-present.

#### Status bar

The status bar (already exists in `internal/tui/statusbar_component.go`) shows the current mode:
```
NORMAL | PLANNING | EXECUTING
```

### 4.6 Plan-to-Todo Bridge

When the user approves a plan, the runner extracts the `## Steps` section and auto-populates the TodoStore:

```go
func planToTodos(planContent string, todoStore *tools.TodoStore) {
    // Parse markdown checkboxes from ## Steps section
    // Create todo items with status "pending"
}
```

This gives the agent a pre-loaded todo list to track progress against the approved plan.

### 4.7 Agent Prompt Injection

When plan mode is active, a system reminder is injected via the reminder middleware (see system_reminders design):

```
[Plan Mode Active]
You are in plan mode. DO NOT make any edits or run write operations.
Explore the codebase with read/grep, then write your plan using plan(action:"update").
When your plan is complete, call plan(action:"finish") to present it to the user.
```

When executing an approved plan:

```
[Executing Approved Plan]
Follow the approved plan at {path}. Track progress using the todo list.
If you need to deviate significantly from the plan, explain why.
```

## 5. Workflow Example

```
User: "Add authentication to the API endpoints"

Agent thinking: This is a non-trivial feature with multiple approaches...
Agent: plan(action:"start", content:"# Plan: Add authentication\n...")
System: [Plan mode entered. Read-only tools only.]

Agent: read("internal/server/routes.go")
Agent: grep("middleware|auth", "internal/")
Agent: read("go.mod")  # check existing auth libraries
Agent: plan(action:"update", content:"<refined plan with JWT approach>")
Agent: plan(action:"finish")

TUI: [Shows plan approval dialog]
User: [Approves with Enter]

System: [Plan approved. TodoStore populated with 5 steps.]
Agent: [Begins implementing step by step, tracking via todos]
```

## 6. Phase 2: Subagent-Enhanced Planning

Once the Sub-Agent infrastructure is available, plan mode can be enhanced:

- **Parallel Explore**: Launch 2-3 explore subagents to research different aspects of the codebase concurrently.
- **Design subagent**: Spawn a focused design agent with the exploration results.
- **Review**: Main agent reads the subagent outputs, synthesizes, and writes the final plan.

This mirrors Claude Code's 5-phase approach but is not required for v1.

## 7. Migration & Compatibility

- New files: `internal/tools/plan.go`, plan approval TUI views.
- `internal/agent/agent.go`: Plan state checked in approval wrapper.
- `internal/runner/runner.go`: Plan-to-todo bridge after approval.
- `internal/tui/messages.go`: New message types.
- `internal/tui/tui.go`: Plan approval view and status bar update.
- `~/.jcoding/plans/` directory created on first use.
- No breaking changes to existing features.

## 8. Open Questions

1. **Auto-enter plan mode**: Should the agent be instructed to auto-enter plan mode for tasks matching certain heuristics (multi-file changes, new features)? Or always let the agent decide?
2. **Plan persistence across sessions**: Should plans survive session boundaries? Currently they're stored in files, so yes by default. But should `--resume` reference the previous plan?
3. **Plan file vs in-memory**: Storing the plan as a file allows `$EDITOR` integration but adds filesystem complexity. An in-memory plan with TUI rendering is simpler for v1.
