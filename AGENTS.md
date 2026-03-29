# AGENTS.md — Coding Assistant Codebase Guide

## Project Overview

Go CLI coding assistant — [Eino](https://github.com/cloudwego/eino) framework + [BubbleTea](https://github.com/charmbracelet/bubbletea) TUI.

- **Entry point:** `cmd/coding/` | **Config:** `~/.jcoding/` | **Module:** `github.com/cnjack/jcode`
- **Build:** `make build` / `make run` / `make install`

---

## Specialised Agents

| Task | Agent file | When to use |
|------|-----------|-------------|
| **Architecture & Design** | `agents/architect.md` | Design docs, architecture decisions, system modelling. Outputs to `docs/design/{domain}/design.md`. Does NOT implement unless asked. |

---

## Directory Structure

```
cmd/coding/          # main: flags, main loop, MCP subcommand, SSH setup
internal/
  agent/             # Eino ChatModelAgent factory + middlewares (approval, reminder, etc.)
  config/            # JSON config loader + logger (→ ~/.jcoding/debug.log)
  model/             # OpenAI-compatible chat model + token tracker
  prompts/           # System prompt template (system.md) + AGENTS.md injection
  runner/            # Agent run loop, todo-completion guard, approval state
  session/           # JSONL session recording/replay
  skills/            # Skill loader + built-in skills (PR review, security review, etc.)
  telemetry/         # Langfuse tracing
  tools/             # Built-in tools: read, edit, write, execute, grep, todo, switch_env, ask_user, subagent, load_skill, MCP, plan
  tui/               # BubbleTea UI components
  util/              # GetWorkDir, GetSystemInfo
```

---

## Key Patterns

### Config (`internal/config/`)
- File: `~/.jcoding/config.json` — fields: `Models`, `Provider`, `Model`, `MaxIterations`, `SSHAliases`, `MCPServers`, `Telemetry`
- **Logger:** `config.Logger()` → `~/.jcoding/debug.log`. Never use `fmt.Print*` for diagnostics.

### Tools (`internal/tools/`)
All implement `tool.InvokableTool` — JSON in, string out, shared `*Env` (local or SSH).

| Tool | Approval |
|------|----------|
| read, grep, todoread, todowrite, check_background | Skipped (read-only) |
| edit, write, switch_env | Required |
| execute | Auto-approved for safe prefixes (ls, pwd, git status…) + background mode; else required |
| ask_user, subagent, load_skill | Skipped (interaction/delegation) |
| MCP tools | Loaded dynamically from config |

### Executor / Env (`internal/tools/env.go`)
`Executor` interface (ReadFile, WriteFile, Exec, …) — `LocalExecutor` or `SSHExecutor`. `Env` wraps executor + pwd + TodoStore + PlanStore; switchable at runtime via `switch_env` tool.

### Agent (`internal/agent/agent.go`)
`NewAgent(ctx, model, tools, prompt, approvalFn, middlewares…)` — Eino `ChatModelAgent`, iteration cap 1000. Middleware chain: langfuse → summarization → reduction → approval+safeTool → reminder.

### Plan Mode (`internal/tools/plan_store.go`, `internal/tools/plan_parse.go`)
State machine (empty → planned → executing). Agent explores read-only, presents a plan for user approval, then executes step-by-step.

### Skills (`internal/skills/`)
Two-layer system: skill descriptions injected into system prompt for discovery; full instructions loaded on demand via `load_skill` tool. Built-in skills live in `internal/skills/` as markdown files.

### Runner (`internal/runner/runner.go`)
`runner.Run()` — streams agent events → TUI, records session JSONL, re-runs up to 3× if TodoStore has incomplete items. Wraps with Langfuse tracing and token usage reporting.

### Session (`internal/session/`)
JSONL per session. Entry types: `session_start`, `user`, `assistant`, `tool_call`, `tool_result`. Resume with `--resume <UUID>`.

### Prompt (`internal/prompts/`)
Template vars: Platform, Pwd, Date, EnvLabel, SSHAliases, GitBranch, GitDirty, LastCommit, ProjectType, DirTree, SkillDescriptions. AGENTS.md appended as `## Custom Agent Instructions`.

---

## Conventions

- **Diagnostics** → `config.Logger()` (never stdout/stderr)
- **Tool errors** → returned as strings (agent-visible, not panics)
- **File paths** → absolute or relative to `Env.Pwd`
- **Tool params** → `schema.ParamsOneOf` with Type/Desc/Required
- **Approval** → read-only tools skip; mutating tools prompt user
- **Background commands** → `execute(background=true)` returns task ID; check with `check_background`

---

## Debugging

1. `~/.jcoding/debug.log` — runtime diagnostics
2. `make doctor` — test model + MCP connectivity
3. `coding --session` — list sessions | `coding --resume <UUID>` — resume
