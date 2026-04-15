# Implementation Plan — Agent Capabilities Roadmap

> **Created**: 2026-03-28
> **Based on**: Claude Code v2.1.86 system prompt analysis + current codebase audit
> **Module**: `github.com/cnjack/jcode` (Eino v0.7.37 + BubbleTea TUI)

---

## Executive Summary

Seven design documents cover the full roadmap. They are organized into three implementation phases based on impact and dependency ordering. Two existing drafts (middleware_pipeline, task_summary_and_observability) have been descoped; two new designs (plan_mode, subagent) have been added.

### Design Document Index

| # | Document | Priority | Status | Phase |
|---|----------|----------|--------|-------|
| 1 | [context_management.md](context_management.md) | **P0** | Implemented | Phase 1 |
| 2 | [plan_mode.md](plan_mode.md) | **P0** | Implemented | Phase 1 |
| 3 | [subagent.md](subagent.md) | **P0** | Implemented | Phase 1 |
| 4 | [middleware_pipeline.md](middleware_pipeline.md) | **P1** | Implemented | Phase 1 |
| 5 | [system_reminders.md](system_reminders.md) | **P2** | Implemented | Phase 2 |
| 6 | [environment_awareness.md](environment_awareness.md) | **P2** | Implemented | Phase 2 |
| 7 | [task_summary_and_observability.md](draft/task_summary_and_observability.md) | **P3** | Draft ready | Phase 3 |

Plus **two prompt-only changes** (no design doc needed, zero code cost):
- Output Efficiency (add conciseness rules to system.md)
- Tool Usage Policy (prefer built-in tools over bash equivalents)

---

## Phase 1 — Core Agent Loop (Must-Have)

> **Goal**: Make the agent viable for long sessions and complex tasks.
> **Dependency graph**: middleware_pipeline → context_management → plan_mode / subagent

### 1.1 Middleware Pipeline Hardening

**Files**: `internal/agent/agent.go`, new `internal/agent/middleware.go`

| Task | Description | Effort |
|------|-------------|--------|
| Extract `safeToolMiddleware()` | Move inline tool-error handling to a separate function. Add `Streamable` wrapper. Check `compose.IsInterruptRerunError`. | S |
| Add `ModelRetryConfig` | 3 retries with default backoff for 429/network errors. Optional `MaxRetries` config field. | S |
| Document middleware order | `langfuse → approval → safeTool`. Add comment block in agent.go. | XS |

**Why first**: All subsequent middlewares (reminders, compaction) depend on a clean, documented pipeline.

### 1.2 Context Management (Context Compaction)

**Files**: `internal/agent/agent.go`, new `internal/agent/compaction.go`, `internal/config/config.go`

| Task | Description | Effort |
|------|-------------|--------|
| Integrate `reduction.NewToolResultMiddleware` | Wire up Eino's built-in tool-result reduction. Config fields for thresholds. Create `~/.jcoding/reduction/` backend dir. | M |
| Implement `historyCompactionHook` | `BeforeChatModel` hook that trims old messages at 75% context usage. Uses `TokenTracker.Get()` for current usage, `GetModelContextLimit()` for ceiling. | M |
| Wire into middleware stack | `[langfuse, compaction, reduction, approval, safeTool]` ordering. | S |

**Why second**: Without context compaction, long sessions crash. This unblocks everything else.

### 1.3 Plan Mode

**Files**: new `internal/tools/plan.go`, `internal/agent/agent.go`, `internal/tui/messages.go`, `internal/tui/tui.go`, `internal/prompts/system.md`

| Task | Description | Effort |
|------|-------------|--------|
| Plan tool (`start`/`update`/`finish` actions) | New tool that manages plan state and writes plan file to `~/.jcoding/plans/`. | M |
| Plan mode enforcement in approval middleware | Block write tools when plan mode is active. | S |
| TUI plan approval dialog | Show plan content, keybindings: approve/reject/edit. Status bar mode indicator. | M |
| Plan-to-todo bridge | Parse `## Steps` from approved plan, auto-populate TodoStore. | S |
| System prompt injection | Plan-mode-specific reminder via system reminder middleware. | S |

**Why third**: Requires stable middleware pipeline. Is the highest-impact UX feature.

### 1.4 Sub-Agent

**Files**: new `internal/tools/subagent.go`, `internal/tools/env.go`, `internal/tui/messages.go`, `internal/prompts/system.md`

| Task | Description | Effort |
|------|-------------|--------|
| Subagent tool (synchronous) | New tool that creates a child `ChatModelAgent`, runs it, returns result. | M |
| Agent type system (`explore`/`general`) | Different tool sets and system prompts per type. No recursive nesting. | S |
| `Env.CloneForSubagent()` | Isolated TodoStore, shared executor and pwd. | S |
| TUI spinner/status for subagent | Show running/done messages. Intermediate tool calls hidden. | S |
| System prompt update | Add subagent tool description and workflow guidance. | XS |

**Why fourth**: Enables plan mode Phase 2 (parallel explore subagents). Standalone value for context isolation.

### 1.5 Prompt Improvements (Zero Code)

**File**: `internal/prompts/system.md` only

| Task | Description |
|------|-------------|
| Output efficiency | Add: "Be concise. Lead with the answer, not reasoning. Skip preamble. If you can say it in one sentence, don't use three." |
| Tool usage policy | Add: "Prefer built-in tools over shell equivalents. Use `read` not `cat`, `edit` not `sed`, `grep` not `rg`. Reserve `execute` for system commands only." |
| Safety guardrails | Add: "Consider reversibility before acting. For destructive operations (rm, git push --force, DROP TABLE), confirm with the user first." |

---

## Phase 2 — Experience Polish

> **Goal**: Improve agent intelligence with runtime context.
> **Prerequisite**: Phase 1 middleware pipeline and context management.

### 2.1 System Reminders (Descoped to 3 Reminders)

**Files**: new `internal/prompts/reminders.go`, new `internal/agent/reminder_middleware.go`, `internal/agent/agent.go`

| Task | Description | Effort |
|------|-------------|--------|
| Reminder framework | `ReminderContext` struct, `CollectReminders()`, `NewReminderMiddleware()`. | M |
| `todo_check` reminder | Fire when TodoStore has incomplete items after 5+ iterations. | S |
| `token_warning` reminder | Fire at 60% and 85% context usage. | S |
| `tool_error_streak` reminder | Fire after 2+ consecutive tool errors. | S |

Deferred reminders: `remote_env`, `long_run`, `file_modified`.

### 2.2 Environment Awareness (Descoped)

**Files**: new `internal/util/envinfo.go`, `internal/prompts/prompts.go`, `internal/prompts/system.md`

| Task | Description | Effort |
|------|-------------|--------|
| Git info collection | Branch name, dirty flag, last commit. Via `exec.Command("git", ...)` with 2s timeout. | S |
| Project type detection | Check for `go.mod`, `package.json`, `Cargo.toml`, etc. | XS |
| Template integration | Add `GitBranch`, `GitDirty`, `LastCommit`, `ProjectType` to prompt data struct. | S |

Deferred: directory tree (agent can use grep/read), SSH remote git info.

### 2.3 TodoWrite Enhancement

**File**: `internal/tools/todo.go`, `internal/prompts/system.md`

| Task | Description | Effort |
|------|-------------|--------|
| `activeForm` field | Add "Running tests" form alongside "Run tests" content form. | S |
| Enhanced system prompt guidance | Add Claude-Code-style examples of when to use/not use todos. | XS |

---

## Phase 3 — Nice to Have

> **Goal**: Observability and polish.
> **Prerequisite**: Phases 1-2.

### 3.1 Task Summary (Deferred)

| Task | Description | Effort |
|------|-------------|--------|
| Summary generation | Ask agent to produce structured summary after all todos complete. Requires context compaction to be stable first. | M |
| `EntrySummary` session type | New JSONL entry type for summaries. | S |
| Resume display | Show last summary when `--resume` loads a session. | S |

### 3.2 Eino Debug Callbacks

| Task | Description | Effort |
|------|-------------|--------|
| `NewDebugLogHandler()` | `utils/callbacks.NewHandlerHelper()` with model+tool lifecycle logging. | S |
| Register globally | `callbacks.AppendGlobalHandlers()` in `main.go`. | XS |

### 3.3 Async Subagents (Phase 2 of subagent design)

| Task | Description | Effort |
|------|-------------|--------|
| Background goroutine execution | Subagent runs async, parent continues. | L |
| Completion notification via reminder | Inject "subagent X finished" at next BeforeChatModel. | M |
| `subagent_result` retrieval tool | Parent retrieves result when ready. | S |
| Parallel launch | Multiple subagents in one turn. | M |

---

## Effort Estimates

| Size | Meaning |
|------|---------|
| XS | <1 hour, prompt/config change only |
| S | 1-3 hours, single file, straightforward |
| M | 3-8 hours, multiple files, some design decisions |
| L | 1-2 days, cross-cutting, requires testing |

---

## Dependency Graph

```
                    ┌─────────────────────┐
                    │ 1.1 Middleware       │
                    │    Pipeline          │
                    └──────────┬──────────┘
                               │
                    ┌──────────▼──────────┐
                    │ 1.2 Context         │
                    │    Management       │
                    └──┬──────────────┬───┘
                       │              │
            ┌──────────▼───┐   ┌──────▼──────────┐
            │ 1.3 Plan     │   │ 1.4 Sub-Agent   │
            │    Mode      │   │                  │
            └──────┬───────┘   └────────┬─────────┘
                   │                    │
                   ▼                    ▼
         ┌─────────────────────────────────────┐
         │ 2.1 System Reminders                │
         │ 2.2 Environment Awareness           │
         │ 2.3 TodoWrite Enhancement           │
         └──────────────────┬──────────────────┘
                            │
                            ▼
         ┌─────────────────────────────────────┐
         │ 3.1 Task Summary                    │
         │ 3.2 Eino Debug Callbacks            │
         │ 3.3 Async Subagents                 │
         └─────────────────────────────────────┘
```

---

## What NOT to Build

Based on Claude Code analysis, these features are **explicitly excluded**:

| Feature | Reason |
|---------|--------|
| Hooks / Agent Hooks | CI/enterprise feature, overkill for CLI tool |
| Learning Mode / Teach Mode | Niche, low ROI |
| Team / Swarm | Extreme complexity for marginal gain |
| Worktree Isolation | Requires mature subagent infra first |
| Insights / Usage Analytics | SaaS business feature, not CLI |
| Browser Automation (Computer tool) | Out of scope for a coding agent |
| Sandbox / Mandatory sandbox mode | Linux-only, complex, questionable value for trusted environments |
| Dream Memory Consolidation | Requires persistent memory system that doesn't exist yet |
| MCPSearch / ToolSearch | Dynamic tool discovery is overkill; static MCP config is sufficient |

---

## Quick Wins (Can Be Done Anytime)

These are independent of the phase ordering:

1. **Prompt conciseness rules** → edit `system.md` (5 min)
2. **Tool usage guidance** → edit `system.md` (5 min)
3. **Safety guardrails text** → edit `system.md` (10 min)
4. **`ModelRetryConfig`** → edit `agent.go` (15 min, but test needed)
5. **`safeToolMiddleware` extraction** → refactor `agent.go` (30 min)
