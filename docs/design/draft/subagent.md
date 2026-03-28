# Sub-Agent Architecture

> **Last verified**: 2026-03-28 against codebase at `github.com/cloudwego/eino v0.7.37`
> **Priority**: P0 — Core capability gap
> **Reference**: Claude Code v2.1.86 `Agent/Task/Fork` tool + subagent type system

## 1. Problem Statement

The current agent is a single-threaded, single-context loop. Three real failures from this:

1. **Context pollution**: User asks "what files handle routing?" — the agent runs 15 grep/read calls, fills its context with search results, then has less room for the actual implementation work that follows.
2. **No parallelism**: User asks "refactor the auth module and update the tests" — the agent must do both sequentially, even though they could be explored independently.
3. **No isolation**: A research question (e.g. "is there an existing utility for X?") cannot be explored without its intermediate tool outputs permanently occupying the main context.

Claude Code addresses this with a Task/Agent tool that can:
- **Fork** (inherit full conversation context, share prompt cache)
- **Spawn specialized subagents** (Explore, Plan, General, code-reviewer, etc.)
- Run **async** in the background while the main agent continues

OpenCode/Crush had a simpler `agent` tool that ran a sub-task with a fresh prompt.

## 2. Goals & Non-Goals

**Goals**
- Add a `subagent` tool that spawns a child agent to handle a delegated task.
- The child agent runs with a fresh context (system prompt + task description) and a subset of tools.
- The child reports back a structured result to the parent.
- Support at least two agent types: `explore` (read-only) and `general` (full tools).

**Non-Goals**
- No parallel execution in v1 (child runs synchronously; parent waits). Async is Phase 2.
- No fork with inherited context in v1 (too complex without prompt cache sharing).
- No worktree isolation (git worktrees for parallel edits).
- No inter-agent communication during execution.

## 3. Current State

### Agent creation
`internal/agent/agent.go` — `NewAgent()` creates a single `ChatModelAgent`. There is no concept of creating a second agent from within the first.

### Runner
`internal/runner/runner.go` — `Run()` drives a single agent. It has no awareness of child agents.

### Tools
All tools in `internal/tools/` share an `*Env` (executor, pwd, TodoStore). This is mutable and not safe for concurrent access by multiple agents.

### Model
`internal/model/chatmodel.go` — `TokenTracker` is global. If two agents run concurrently, token counts would be incorrect. For synchronous subagents, this is acceptable.

## 4. Proposed Design

### 4.1 Subagent Tool

```go
// internal/tools/subagent.go

type SubagentInput struct {
    Name        string `json:"name" desc:"Short name for the subagent task (1-3 words)" required:"true"`
    Description string `json:"description" desc:"Brief description of what the subagent should do" required:"true"`
    Prompt      string `json:"prompt" desc:"Detailed instructions for the subagent. Include all necessary context." required:"true"`
    AgentType   string `json:"agent_type" desc:"Type of subagent: 'explore' (read-only) or 'general' (full tools). Default: explore"`
}
```

The tool:
1. Creates a fresh `ChatModelAgent` with the appropriate tool set and system prompt.
2. Runs it synchronously with the provided prompt as the user message.
3. Collects the final assistant response (not intermediate tool calls).
4. Returns the response as the tool result to the parent agent.

### 4.2 Agent Types

| Type | Tools available | System prompt suffix | Use case |
|------|----------------|---------------------|----------|
| `explore` | read, grep, execute (read-only) | "You are a research subagent. Analyze code and report findings concisely. Do NOT make any changes." | Codebase exploration, research questions |
| `general` | All tools except `subagent` (no nesting) | "You are a task subagent. Complete the assigned task and report what you did." | Implementation subtasks |

No nesting: subagents cannot spawn further subagents. This prevents runaway agent trees.

### 4.3 Subagent System Prompt

```go
func subagentSystemPrompt(agentType, parentPwd, parentPlatform string) string {
    base := fmt.Sprintf(`You are a subagent working on a delegated task.

Current work path: %s
Platform: %s
Date: %s

`, parentPwd, parentPlatform, time.Now().Format("2006-01-02"))

    switch agentType {
    case "explore":
        return base + `You are a research/exploration subagent. Your job is to:
- Search and read code to answer the question in your prompt
- Report findings concisely (under 500 words)
- Do NOT make any file changes

Report your findings in a structured format.`
    case "general":
        return base + `You are a task subagent. Your job is to:
- Complete the specific task described in your prompt
- Report what you did and any issues encountered
- Keep your scope narrow — only do what was asked`
    }
    return base
}
```

### 4.4 Subagent Execution Flow

```
Parent Agent
    │
    ├── agent calls subagent(name:"auth-research", type:"explore",
    │       prompt:"Find all auth middleware in internal/. What patterns are used?")
    │
    ├── [subagent tool handler]
    │       │
    │       ├── Create fresh ChatModelAgent (explore type)
    │       │     └── tools: read, grep, execute (safe prefixes only)
    │       │
    │       ├── Run synchronously with prompt as user message
    │       │     └── subagent does: grep → read → read → respond
    │       │
    │       ├── Collect final response (not intermediate tool calls)
    │       │
    │       └── Return response as tool result
    │
    ├── Parent receives: "Found 3 auth patterns: JWT middleware in server/auth.go,
    │                      API key check in server/apikey.go, session middleware in..."
    │
    └── Parent continues with its original task, context uncluttered
```

### 4.5 Resource Management

#### Model reuse
The subagent uses the same `model.ToolCallingChatModel` instance as the parent. Since execution is synchronous, there is no contention.

#### Env sharing
The subagent receives a **clone** of the parent's `Env` with the same `Executor` and `Pwd` but a **separate** `TodoStore`. Subagent todos are isolated and not displayed in the parent's TUI.

```go
func (e *Env) CloneForSubagent() *Env {
    return &Env{
        executor:  e.executor,
        pwd:       e.pwd,
        todoStore: NewTodoStore(), // fresh, isolated
    }
}
```

#### Iteration limit
Subagents have a lower `MaxIterations` cap (default 50) to prevent runaways. The parent's iteration counter is not affected.

#### Token tracking
Subagent token usage is added to the global `TokenTracker`. This is correct — the user pays for all tokens regardless of which agent used them.

### 4.6 TUI Integration

#### New messages

```go
// internal/tui/messages.go
type SubagentStartMsg struct {
    Name string
    Type string
}

type SubagentDoneMsg struct {
    Name   string
    Result string
    Err    error
}
```

#### Display
When a subagent starts, the TUI shows a spinner/status:
```
🔍 Subagent "auth-research" (explore) running...
```

When done:
```
✅ Subagent "auth-research" completed (1,234 tokens)
```

The subagent's intermediate tool calls are **not** shown in the parent TUI (that would defeat the purpose of context isolation). Only the final result is shown as a collapsed tool result.

### 4.7 Approval

The `subagent` tool is **auto-approved** (like `read` and `grep`). The subagent's own tools follow normal approval rules:
- `explore` type: all tools are read-only → auto-approved.
- `general` type: write tools require approval → prompts appear in the parent TUI.

### 4.8 Integration in System Prompt

Add to `internal/prompts/system.md`:

```markdown
- **subagent**: Delegate a task to a subagent. Types: 'explore' (read-only research) or 'general' (full tools). Use for:
  - Codebase exploration that would clutter your context
  - Research questions with many search steps
  - Independent subtasks in a larger plan
  The subagent runs in a clean context and returns only its findings.
```

Add guidance to the Workflow section:

```markdown
# Workflow
1. Explore: use subagent(type:'explore') for broad codebase research to avoid polluting your context
2. Plan: think before acting and break into steps
3. Implement: use tools to implement the plan
4. Review: check the result and make sure it's correct
```

## 5. Phase 2: Async Subagents

Once the synchronous foundation is solid, async execution can be added:

1. **Background execution**: Subagent runs in a goroutine. Parent continues with other work.
2. **Completion notification**: When the subagent finishes, a system reminder is injected into the parent's next `BeforeChatModel` call.
3. **Result retrieval**: Parent calls `subagent_result(name:"auth-research")` to get the output.
4. **Parallel launch**: Multiple subagents can be launched in one tool call batch.

This requires:
- Thread-safe Env (separate executors or mutex-protected shared state).
- Subagent lifecycle management (cancel on parent abort, timeout).
- TUI updates for multiple concurrent spinners.

## 6. Phase 3: Context-Inheriting Fork

The most advanced mode (Claude Code's "fork"):
- Child agent inherits the parent's full conversation history.
- Shares prompt cache (if the model provider supports it).
- Useful when the child needs to know everything the parent knows.

Requires: prompt cache API support, which depends on the model provider.

## 7. Migration & Compatibility

- New files: `internal/tools/subagent.go`.
- `internal/tools/env.go`: Add `CloneForSubagent()` method.
- `internal/agent/agent.go`: Subagent tool added to the tools list. The subagent tool needs access to the model and agent factory — passed via closure.
- `internal/tui/messages.go`: New message types.
- `internal/prompts/system.md`: Tool description and workflow update.
- No breaking changes.

## 8. Open Questions

1. **Nesting depth**: Should we allow 1 level of nesting (subagent can spawn sub-subagent)? Claude Code allows forks of forks. For v1, flat (no nesting) is simpler.
2. **Subagent session recording**: Should subagent interactions be recorded in the parent's session JSONL? Probably yes (as nested entries), for debugging.
3. **Token budget**: Should subagents have a separate token budget? Or share the parent's remaining budget? Sharing is simpler but risks a subagent consuming all remaining context.
4. **Read-only execute enforcement**: In `explore` mode, `execute` should only allow safe prefixes. This reuses the existing safe-prefix list from the approval logic.
