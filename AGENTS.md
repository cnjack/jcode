# AGENTS.md — Coding Assistant Codebase Guide

## Project Overview

A Go-based CLI coding assistant built on the [Eino](https://github.com/cloudwego/eino) framework with a [BubbleTea](https://github.com/charmbracelet/bubbletea) TUI.

- **Binary entry point:** `cmd/coding/`
- **Config directory:** `~/.jcoding/` (config.json, session files, history, debug.log)
- **Module:** `github.com/cnjack/coding`
- **Build:** `make build` / `make run` / `make install`

---

## Directory Structure

```
cmd/coding/          # main package: flags, main loop, MCP subcommand, SSH setup
internal/
  agent/             # Eino ChatModelAgent factory (approval middleware injected here)
  config/            # JSON config loader + shared application logger (log.go)
  model/             # OpenAI-compatible chat model wrapper, token usage tracker
  prompts/           # System prompt template (system.md), AGENTS.md injection
  runner/            # Agent run loop, todo-completion guard, approval state
  session/           # JSONL session recording/replay, session index
  telemetry/         # Langfuse tracing integration
  tools/             # All Eino tools: read, edit, write, execute, grep, todo, MCP, SSH auth
  tui/               # BubbleTea UI: messages, pickers, formatting, SSH handlers
  util/              # GetWorkDir, GetSystemInfo
```

---

## Key Packages & Patterns

### Config (`internal/config/`)

- Config file: `~/.jcoding/config.json`
- Load with `config.LoadConfig()` — returns `*Config`
- Key fields: `Models` (map of provider → `ProviderConfig`), `Provider`, `Model`, `MCPServers`, `SSHAliases`, `Telemetry`, `MaxIterations`
- `config.ConfigPath()` returns the resolved path for display

#### Application Logger (`internal/config/log.go`)

Use `config.Logger()` for any internal diagnostic/error logging — **never `fmt.Print*` or `log.Print*` directly**.

```go
config.Logger().Printf("[component] something happened: %v", err)
```

- Lazily initialised on first call (thread-safe via `sync.Once`)
- Writes to `~/.jcoding/debug.log` (append, mode 0600)
- Falls back to stderr if the log file cannot be opened
- The log file is the single authoritative location for runtime diagnostics — users inspect it to debug issues without any output polluting the TUI

### Tools (`internal/tools/`)

All tools implement the Eino `tool.BaseTool` / `tool.InvokableTool` interface:

```go
Info(ctx) (*schema.ToolInfo, error)
InvokableRun(ctx, argumentsInJSON string, opts ...tool.Option) (string, error)
```

- Input: JSON string — unmarshal with `encoding/json`
- Output: plain string
- All tools accept a shared `*Env` (wraps `Executor`) so they work identically on local or SSH targets

| Tool | Description |
|------|-------------|
| `read` | File reading with offset/limit |
| `edit` | Exact string replacement; empty `old_string` → create file |
| `write` | Full file overwrite/create |
| `execute` | Bash execution with configurable timeout |
| `grep` | ripgrep/grep pattern search |
| `todowrite` / `todoread` | In-memory todo list managed via `TodoStore` |
| MCP tools | Loaded dynamically from configured MCP servers |

### Executor / Env (`internal/tools/env.go`)

```go
type Executor interface {
    ReadFile, WriteFile, MkdirAll, Stat, Exec, Platform, Label
}
```

- `LocalExecutor` — uses `os` package
- `SSHExecutor` — uses `golang.org/x/crypto/ssh`
- `Env.SetSSH(...)` / `Env.ResetToLocal()` — switch at runtime

### Agent (`internal/agent/agent.go`)

`agent.NewAgent(ctx, chatModel, tools, instruction, approvalFunc, middlewares...)` wraps `adk.NewChatModelAgent` with:
- Hard iteration cap: `maxIterations = 1000`
- Tool-call approval middleware injected first
- Additional middlewares (e.g. Langfuse) appended after

### Runner (`internal/runner/runner.go`)

`runner.Run(ctx, ag, messages, p, rec, todoStore, tracer)` is the single entry point per conversation turn:

1. If `tracer != nil`, wraps `ctx` with a new Langfuse trace
2. Calls `runInner` (streams events → TUI messages)
3. **Todo-completion guard**: re-runs up to 3 times if `todoStore.HasIncomplete()` after completion
4. Sends `TokenUpdateMsg` and `AgentDoneMsg` to TUI when done

### Telemetry (`internal/telemetry/langfuse.go`)

- `telemetry.NewLangfuseTracer(cfg)` — returns `nil` if credentials are absent
- Wraps the `eino-ext/libs/acl/langfuse` client
- Records: one `CreateTrace` per `runner.Run` call; `CreateGeneration`/`EndGeneration` around each model call; `CreateSpan`/`EndSpan` per tool call
- All errors and diagnostic messages go through `config.Logger()` (→ `~/.jcoding/debug.log`)
- Call `tracer.Flush()` at program exit (already done in `cmd/coding/main.go`)
- Config keys in `~/.jcoding/config.json`:

```json
"telemetry": {
  "langfuse": {
    "LANGFUSE_BASE_URL": "https://your-langfuse-host",
    "LANGFUSE_PUBLIC_KEY": "pk-lf-...",
    "LANGFUSE_SECRET_KEY": "sk-lf-..."
  }
}
```

### Session Recording (`internal/session/`)

JSONL file per session under `~/.jcoding/` (path derived from project pwd + UUID).

Entry types: `session_start`, `user`, `assistant`, `tool_call`, `tool_result`

- `session.NewRecorder(pwd, provider, model)` — lazy file creation
- `session.LoadSession(uuid)` — reconstruct history
- `session.ListSessions(pwd)` — list by project

### MCP Integration (`internal/tools/mcp.go`)

`tools.LoadMCPTools(ctx, mcpConfig)` connects to HTTP, SSE, or stdio MCP servers and returns `([]tool.BaseTool, []MCPStatus)`.

---

## Conventions

- **No `fmt.Print*` for diagnostics** — use `config.Logger()` so output goes to `~/.jcoding/debug.log`
- **Tool errors are surfaced as string results** (`fmt.Sprintf("Tool execution failed: %v", err)`) — the agent sees them and can react
- **All file paths in tools are absolute** or resolved relative to `Env.Pwd`
- **JSON schema for tool params**: use `schema.ParamsOneOf` with `Type`/`Desc`/`Required` fields
- **Config changes**: re-call `config.LoadConfig()` — no global mutable state outside `model.TokenTracker`
- **Approval**: read/grep/todowrite/todoread/question/webfetch skip approval; `execute` checks safe prefixes; everything else prompts the user via TUI

---

## Debugging

1. Check `~/.jcoding/debug.log` for Langfuse init/trace errors and other runtime diagnostics
2. Run `make doctor` (or `coding --doctor`) to test model + MCP connectivity
3. Run `coding --session` to list recorded sessions for the current directory
4. Resume a session: `coding --resume <UUID>`
