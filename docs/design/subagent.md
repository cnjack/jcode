# Sub-Agent Architecture

> **Implemented**: 2026-03-29, Eino v0.8.5
> **Priority**: P0

## 1. Problem

A single-threaded agent context pollutes itself with research tool calls. "What files handle routing?" triggers 15 grep/reads that permanently occupy context needed for the actual task. A subagent handles such research in isolation and returns only its findings.

## 2. Implementation

### 2.1 Tool Interface

`internal/tools/subagent.go` — `NewSubagentTool(deps)`:

```go
type SubagentDeps struct {
    ChatModel model.ToolCallingChatModel
    Notifier  SubagentNotifier // func(name, agentType string, done bool, result string, err error)
}

// Input schema
type subagentInput struct {
    Name        string `json:"name"`         // 1-3 words, shown in TUI
    Description string `json:"description"`   // brief, shown in TUI
    Prompt      string `json:"prompt"`        // detailed task instructions
    AgentType   string `json:"agent_type"`    // "explore" (default) or "general"
}
```

### 2.2 Agent Types

| Type | Tools | System prompt |
|------|-------|---------------|
| `explore` | read, grep, execute (no background) | "Research subagent. Report findings. Do NOT make changes." |
| `general` | read, grep, execute, edit, write, todowrite, todoread | "Task subagent. Complete the assigned task." |

No nesting: subagents do not receive the `subagent` tool. `subagentMaxIter = 50`.

### 2.3 Execution

The subagent runs synchronously. The parent agent blocks at the `subagent` tool call until the child finishes.

```go
func (s *subagentTool) InvokableRun(ctx context.Context, argumentsInJSON string, _) (string, error) {
    // 1. Notify TUI: subagent started
    s.deps.Notifier(input.Name, agentType, false, "", nil)

    // 2. Create child agent (fresh context, subset tools)
    childEnv := s.env.CloneForSubagent()    // same Executor + Pwd, fresh TodoStore
    childTools := s.buildTools(childEnv, agentType)
    ag, _ := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
        Instruction:      subagentSystemPrompt(agentType, pwd, platform),
        Model:            s.deps.ChatModel,
        ToolsConfig:      ...,
        MaxIterations:    subagentMaxIter,
        ModelRetryConfig: &adk.ModelRetryConfig{MaxRetries: 2},
    })

    // 3. Run and collect only final text response
    result := s.runSubagent(ctx, ag, input)

    // 4. Notify TUI: subagent done
    s.deps.Notifier(input.Name, agentType, true, result, nil)
    return result, nil
}
```

### 2.4 Env Cloning

```go
func (e *Env) CloneForSubagent() *Env {
    return &Env{
        Exec:      e.Exec,        // same executor (SSH or local)
        pwd:       e.pwd,
        TodoStore: NewTodoStore(), // isolated — not shown in parent TUI
        platform:  e.platform,
    }
}
```

Subagent todos are invisible to the parent and discarded after the subagent finishes.

### 2.5 TUI Integration

The `SubagentNotifier` callback sends events to the TUI:
- On start: TUI displays `🔍 Subagent "<name>" (<type>)...`
- On done/error: TUI updates the display with result summary

The subagent's intermediate tool calls are not shown in the parent TUI — only the `SubagentNotifier` events appear.

### 2.6 Approval

The `subagent` tool is auto-approved in `internal/runner/approval.go` (in the `noApprovalNeeded` map). The subagent's own tool calls follow normal approval rules (explore type is always read-only → auto-approved; general type may prompt).

## 3. Files

| File | Role |
|------|------|
| `internal/tools/subagent.go` | Full subagent tool implementation |
| `internal/tools/env.go` | `CloneForSubagent()` |
| `cmd/coding/main.go` | `SubagentDeps` wiring, `subagentNotifier` callback |
| `internal/runner/approval.go` | `subagent` in `noApprovalNeeded` |
