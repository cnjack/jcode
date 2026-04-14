# AGENTS.md — Coding Assistant Codebase Guide

## Project Overview

Go CLI coding assistant ("Little Jack") — [Eino](https://github.com/cloudwego/eino) + BubbleTea v2 TUI + Vue 3 web UI.

- **Entry point:** `cmd/jcode/` | **Config:** `~/.jcode/` | **Module:** `github.com/cnjack/jcode`
- **Build:** `make build` / `make run` / `make install` / `make doctor`

---

## Directory Structure

```
cmd/jcode/           # main: subcommands (mcp, acp, web), flags, main event loop
internal/
  agent/             # ChatModelAgent factory + middlewares
  config/            # JSON config loader + logger (→ ~/.jcode/debug.log)
  handler/           # Event handler interface (TUI/ACP/Web implementations)
  model/             # OpenAI-compatible chat model + token tracker + model registry (static build-time data)
  prompts/           # System prompt (system.md) + plan prompt (plan.md) + AGENTS.md injection
  runner/            # Agent run loop, todo-completion guard, approval state, event bus
  session/           # JSONL session recording/replay with state reconstruction
  skills/            # Skill loader + builtin skills (review-pr, pr-comments, security-review)
  team/              # Multi-agent team (Manager, Mailbox, SpawnConfig)
  telemetry/         # Langfuse tracing
  tools/             # All built-in tools + Executor/Env abstraction
  tui/               # BubbleTea v2 TUI
  util/              # GetWorkDir, GetSystemInfo, CollectEnvInfo
  web/               # Go HTTP server (REST + SSE + PTY) + Vue frontend dist
script/              # Build-time code generation scripts
web/                 # Vue 3 + Vite + TypeScript frontend → builds to internal/web/dist/
```

---

## Entry Point (`cmd/jcode/`)

### Subcommands: `mcp` (add/list), `acp` (headless JSON-RPC), `web` (--port, --host)

### Flags
`-p`/`-prompt` (one-shot), `-doctor` (system check), `-version`, `-resume <UUID>`, `-session` (list sessions)

### Main Loop
Channel-based `for/select` on: `autoApproveCh`, `planModeCh`, `configCh`, `promptCh`, `pendingPromptCh`, `resumeCh`, `sshCh`, `compactCh`, `addModelCh`

### Agent Modes
| Mode | Behavior |
|---|---|
| `ModeNormal` (0) | Full tools, standard operation |
| `ModePlanning` (1) | Read-only exploration, generates plan for approval |
| `ModeExecuting` (2) | Full tools, executes approved plan step-by-step |

**Plan lifecycle:** planning → generate plan → TUI review → approve/reject → rejected: revise loop → approved: extract todos → transition to executing → auto-transition to normal when todos complete.

---

## Config (`internal/config/`)

**File:** `~/.jcode/config.json`

| Field | Type | Description |
|---|---|---|
| `Providers` | `map[string]*ProviderConfig` | `{api_key, base_url, models[]}` |
| `Model` | `string` | Active model in `"provider/model"` format |
| `SmallModel` | `string` | For summaries/compaction |
| `FallbackModel` | `string` | Fallback when primary fails |
| `MaxIterations` | `int` | Default 1000 |
| `SSHAliases` | `[]SSHAlias` | `{name, addr, path}` |
| `MCPServers` | `map[string]*MCPServer` | `{type, command, args[], env[], url, headers}` |
| `Telemetry` | `*TelemetryConfig` | `{langfuse: {host, public_key, secret_key}}` |
| `Budget` | `*BudgetConfig` | `{max_tokens_per_turn, max_cost_per_session, warning_threshold}` |
| `Compaction` | `*CompactionConfig` | `{enabled, threshold, keep_recent, summary_model}` |
| `Prompt` | `*PromptConfig` | `{compaction, memory_max_chars, memory_max_depth, cache_enabled, async_env_timeout}` |
| `Subagent` | `*SubagentConfig` | `{max_parallel, max_completed, max_depth}` |
| `Team` | `*TeamConfig` | `{max_teammates (5), mailbox_poll_ms (500), message_cap (50)}` |
| `AutoApprove` | `bool` | Auto-approve all tool calls |
| `DisabledProviders` | `[]string` | Providers to skip |

### Key Paths
`~/.jcode/config.json`, `~/.jcode/debug.log`, `~/.jcode/sessions/{uuid}.jsonl`, `~/.jcode/skills/{name}/SKILL.md`, `~/.jcode/teams/{name}/`

---

## Tools (`internal/tools/`)

All implement `tool.InvokableTool` — JSON in, string out, shared `*Env` (local or SSH).

### Tool Inventory

| Tool | Approval | Key Params |
|---|---|---|
| `read` | Auto within workpath; approval for external | `file_path, offset, limit` |
| `edit` | **Required** | `file_path, old_string, new_string, start_line, end_line` |
| `write` | **Required** | `file_path, content` |
| `execute` | Auto for background + safe prefixes; else **required** | `command, background, timeout` |
| `grep` | Auto | `pattern, path, file_type, include, case_insensitive, max_results, context` |
| `glob` | Auto | `pattern, path, max_depth, limit (100 default, 500 max)` |
| `todowrite` | Auto | `{id, title, status: pending/in_progress/completed/cancelled}` |
| `todoread` | Auto | (no params) |
| `subagent` | Auto | `name, description, prompt, agent_type (explore/general/coordinator), model, run_in_background` |
| `check_background` | Auto | `task_id` (omit to list all) |
| `ask_user` | Auto | `question, header, options[]` (supports batch) |
| `switch_env` | **Required** (only loaded if SSH configured) | `target` ("local" or SSH alias) |
| `load_skill` | Auto | `name` |
| `team_create` | Auto | `team_name, description` |
| `team_spawn` | Auto | `name, prompt, agent_type, model, cwd, mode` |
| `team_send_message` | Auto | `to, message, summary` (`"*"` = broadcast) |
| `team_list` | Auto | (no params) |
| `team_delete` | Auto | `force` |
| MCP tools | Per-tool | Loaded dynamically from config |

### Safe Execute Prefixes (auto-approved)
`ls`, `pwd`, `env`, `cat`, `echo`, `which`, `git status`, `git log`

### Executor / Env (`env.go`)
- `Executor` interface: `ReadFile`, `WriteFile`, `MkdirAll`, `Stat`, `Exec`, `Platform`, `Label`
- `LocalExecutor` / `SSHExecutor` (via `golang.org/x/crypto/ssh`)
- `MaxSubagentDepth = 3`

### Subagent System
- Types: `explore` (read-only), `general` (full tools), `coordinator` (can spawn sub-subagents)
- `subagentMaxIter = 50`, sync and async modes

### Plan Store (`plan_store.go`)
State machine: `draft → submitted → approved/rejected`. `plan_parse.go`: `ExtractTodosFromPlan()` parses `1. ` steps and `- [ ]` checkboxes from markdown.

---

## Agent (`internal/agent/`)

### Iteration Caps
Top-level: 1000 | Teammates: 200 | Subagents: 50

### Middleware Stack (outermost → innermost)
1. **Langfuse tracer** (optional)
2. **budgetMiddleware** (optional) — token tracking, budget warnings
3. **compactionMiddleware** (optional) — `ThresholdCompactionStrategy`
4. **recoveryMiddleware** (optional) — `MaxOutputContinuationLayer` + `ContextOverflowLayer`
5. **approvalMiddleware** — **always innermost**, gates tool calls

### Recovery Layers
- `MaxOutputContinuationLayer` — `max_tokens`/`length`/`truncated` (3 retries)
- `ContextOverflowLayer` — `context_length_exceeded`/`too many tokens` (2 retries, keep recent 10)

### Reminder Conditions (`reminders.go`)
`plan_execution` (approved plan exists), `todo_check` (incomplete todos after iter 5), `token_warning` (>60%), `token_critical` (>85%), `tool_error_streak` (2+ failures)

### Model Retry
`ModelRetryConfig{MaxRetries: 5, SmartBackoff with jitter}`

---

## Handler (`internal/handler/`)

`AgentEventHandler` interface: `OnAgentText`, `OnToolCall`, `OnToolResult`, `OnTodoUpdate`, `OnAgentDone`, `OnTokenUpdate`, `RequestApproval`

| Handler | Transport |
|---|---|
| **TUIHandler** | BubbleTea messages |
| **ACPHandler** | ACP JSON-RPC |
| **WebHandler** | SSE + REST API |

Approval modes: `ModeManual` (default), `ModeAuto`.

---

## Runner (`internal/runner/`)

`Run(ctx, ag, messages, h, rec, todoStore, tracer)` — streams agent events → handler. **Todo completion guard**: re-runs up to 3 times if incomplete todos remain.

### EventBus (`eventbus.go`)
`EventAssistantText`, `EventAssistantDone`, `EventToolCall`, `EventToolResult`, `EventError`, `EventBudgetWarning`, `EventCompaction`, `EventWorkerStatus`

---

## Session (`internal/session/`)

JSONL at `~/.jcode/sessions/{uuid}.jsonl`. Teammate recordings at `~/.jcode/sessions/{leaderUUID}/subagents/agent-{agentID}.jsonl`.

13 entry types: `session_start`, `user`, `assistant`, `tool_call`, `tool_result`, `plan_update`, `todo_snapshot`, `subagent_start`, `subagent_result`, `subagent_async`, `mode_change`, `compact`, `budget_warning`.

`ReconstructState()` rebuilds full state. Compact-aware: discards history before compact entries.

---

## Prompts (`internal/prompts/`)

- **System prompt** (`system.md`): vars `Pwd`, `Platform`, `Date`, `EnvLabel`, `SSHAliases`, `GitBranch`, `GitDirty`, `LastCommit`, `ProjectType`, `DirTree`, `SkillDescriptions`
- **Plan prompt** (`plan.md`): restricted to read, grep, execute (read-only), todowrite, todoread, ask_user
- **AGENTS.md injection**: `MemoryLoader` loads `AGENTS.md` recursively from project dir. Max 40K chars, depth 5.
- **PromptBuilder**: parallel env-info + AGENTS.md loading, block caching

---

## Skills (`internal/skills/`)

Sources (later overrides earlier): **Builtin** (`//go:embed builtin`) → **Agents** (`~/.agents/skills/{name}/SKILL.md`) → **User** (`~/.jcode/skills/{name}/SKILL.md`) → **Project** (`.jcode/skills/{name}/SKILL.md`)

Two-layer: descriptions in system prompt → full content on-demand via `load_skill`.

Builtin: `review-pr` (`/review-pr`), `pr-comments` (`/pr-comments`), `security-review` (`/security-review`)

---

## Team (`internal/team/`)

- **Manager** coordinates team, **Mailbox** for file-based message passing
- Teammate lifecycle: `pending → idle → running → idle (loop) → completed/failed/killed`
- Constants: `maxTeammateMessages=50`, `mailboxPollInterval=500ms`, `teammateMaxIter=200`
- Lead: `TeamLeadName="team-lead"`, Agent ID: `"{name}@{team}"`

---

## Model (`internal/model/`)

- **ChatModel**: wraps `go-openai`, implements `ToolCallingChatModel` (Generate, Stream, WithTools)
- **ModelRegistry**: static model metadata generated at build time from models.dev via `go generate`
- **Retry**: error types `Transient`, `RateLimit`, `ContextOverflow`, `Auth`, `Fatal`

Build-time code generation: `script/generate_models.go` fetches models.dev API and generates `internal/model/registry_generated.go`. Run via `go generate ./internal/model/...` (automatically invoked by `make build` and `make install`).

---

## Conventions

- **Lint Compliance:** All code changes MUST pass linter checks. Run `make lint` before finishing.
- **Diagnostics** → `config.Logger()` (never stdout/stderr)
- **Tool errors** → returned as strings (agent-visible, not panics)
- **File paths** → absolute or relative to `Env.Pwd`
- **Tool params** → `schema.ParamsOneOf` with Type/Desc/Required
- **Approval** → read-only tools skip; mutating tools prompt user; `AutoApprove` bypasses all

---

## Code Quality & Linting

### Lint Tools
- **Go:** `golangci-lint` (config: `.golangci.yml`)
- **Web:** `eslint` + `oxlint` (config: `web/package.json`)

### Agent Workflow
- **Mandatory Check:** Before completing a coding task, run `make lint`. Fix any reported issues.

---

## Build

| Target | Description |
|---|---|
| `make generate` | Generate code (models registry) via go:generate |
| `make build-web` | `pnpm install && vite build` in `web/` |
| `make build` | `generate` → `build-web` → `go build -o jcode ./cmd/jcode/` |
| `make install` | `generate` → `build-web` → `go install ./cmd/jcode/` |
| `make run` | `go run ./cmd/jcode/` |
| `make doctor` | `go run ./cmd/jcode/ --doctor` |
| `make lint` | Run Go and Web linters |
| `make clean` | Remove binary + `internal/web/dist` |

Build ldflags: `Version`, `BuildTime`, `GitCommit`.
