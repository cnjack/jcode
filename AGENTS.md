# AGENTS.md — jcode 项目开发指南

Go CLI coding assistant — [Eino](https://github.com/cloudwego/eino) + BubbleTea v2 TUI + Vue 3 web UI.

- **Module:** `github.com/cnjack/jcode` | **Entry:** `cmd/jcode/` | **Config dir:** `~/.jcode/`

---

## Quick Start

```bash
make build        # generate → build-web → go build
make install      # generate → build-web → go install
make run          # go run ./cmd/jcode/
make lint         # golangci-lint + eslint/oxlint
make doctor       # system check
```

- `make build-web` requires `pnpm`. Frontend builds to `internal/web/dist/`.
- `make generate` runs `go generate ./internal/model/...` — fetches models.dev data and generates `internal/model/registry_generated.go`. **Do NOT manually edit that file.**
- Build injects `Version`, `BuildTime`, `GitCommit` via ldflags into `internal/command`.

---

## Architecture Overview

```
cmd/jcode/           # Entry: subcommands (mcp, acp, web), main event loop
internal/
  command/           # Subcommand implementations + interactive session orchestration
  agent/             # ChatModelAgent factory + middleware chain
  runner/            # Agent run loop + event bus
  handler/           # AgentEventHandler interface (TUI / ACP / Web implementations)
  tools/             # All built-in tools + Executor/Env abstraction
  model/             # OpenAI-compatible chat model + model registry (build-time generated)
  config/            # JSON config loader + logger
  prompts/           # System/plan prompts + AGENTS.md injection + env info
  session/           # JSONL session recording/replay
  skills/            # Skill loader (builtin → user → project override chain)
  team/              # Multi-agent team coordination
  tui/               # BubbleTea v2 TUI components
  web/               # HTTP server (REST + SSE + PTY) + embedded Vue dist
web/                 # Vue 3 + Vite + TypeScript frontend source
script/              # Build-time code generation
```

### Key Design Decisions

- **Three transports, one interface:** TUI, ACP (JSON-RPC), and Web all implement `AgentEventHandler`. New transports only need to implement this interface.
- **Middleware chain in `agent/`:** Ordered outermost→innermost: langfuse → budget → compaction → recovery → approval. **Approval is always innermost** — never add middleware after it.
- **Tools are methods on `*Env`:** Each tool is created via `env.NewXxxTool()`, receives the shared `Env` for file I/O and command execution. This enables transparent local/SSH switching.
- **Eino framework:** We use [cloudwego/eino](https://github.com/cloudwego/eino) `adk.ChatModelAgent` — not raw LLM calls. Follow Eino's `tool.InvokableTool` + `schema.ToolInfo` patterns.

---

## Conventions (MUST follow)

### Logging & Output
- **All diagnostics go to `config.Logger()`** (writes to `~/.jcode/debug.log`). Never use `fmt.Print`, `log.Print`, or write to stdout/stderr directly — the TUI owns stdout.
- Tool execution errors are returned as plain strings (the agent reads them). Do NOT `panic` or `log.Fatal` in tool code.

### Error Handling
- Tools return `(string, error)`. Return descriptive error strings that help the agent self-correct. Include file paths, line numbers, or command output in error messages.
- Use `fmt.Errorf("tool_name: %w", err)` for wrapped errors in non-tool code.

### File Paths
- All file paths in tools must be resolved via `env.ResolvePath(path)`. This handles relative→absolute conversion and logs warnings for paths escaping the working directory.
- Store and pass absolute paths internally. Only accept relative paths at the tool input boundary.

### Tool Development Pattern
1. Define `XxxInput` struct with `json` tags
2. Create `func (e *Env) NewXxxTool() tool.InvokableTool` on `*Env`
3. Build `schema.ToolInfo` with `schema.NewParamsOneOfByParams(...)` — use `schema.String`, `schema.Integer`, `schema.Boolean`, `schema.Array`
4. Register in `buildAllTools()` in `internal/command/interactive.go` (and `acp.go`, `web.go`)
5. If the tool is read-only, also add to `buildPlanTools()`
6. **Approval policy:** Read-only tools skip approval. Mutating tools require approval unless `AutoApprove` is set. Match existing patterns in `approval.go`.

### Approval Policy
- Read-only tools (read, grep, glob, todoread, etc.): auto-approved
- Mutating tools (edit, write, execute): require user approval
- Execute exceptions: background commands and safe prefixes (`ls`, `cat`, `echo`, `which`, `git status`, `git log`, etc.) are auto-approved
- `switch_env`: always requires approval

### Code Style
- Follow standard Go conventions. The linter config (`.golangci.yml`) enforces: `errcheck`, `govet`, `staticcheck`, `unused`, `revive`, `gocritic`, `funlen` (max 800 lines/600 statements).
- Use `context.Context` as the first parameter. Thread cancellation properly.
- Prefer returning errors over panicking. Only `panic` for truly unrecoverable programmer errors.
- Interfaces live in the package that consumes them (e.g., `AgentEventHandler` in `handler/`, `Executor` in `tools/`).

### Concurrency
- `AgentEventHandler` implementations must be goroutine-safe — the runner may call methods from multiple goroutines simultaneously.
- Use `sync.RWMutex` for shared state in tools (see `TodoStore`, `BackgroundManager`).
- Channel-based coordination in `cmd/jcode/main.go` — the main event loop uses `for/select` over typed channels.

---

## How To: Common Tasks

### Add a New Tool
1. Create `internal/tools/xxx.go` with input struct + `NewXxxTool()` method on `*Env`
2. Add to `buildAllTools()` in `interactive.go`, `acp.go`, and `web.go`
3. If read-only and useful in plan mode, also add to `buildPlanTools()`
4. Set appropriate approval requirements following the approval policy above
5. If the tool needs external dependencies (like `BackgroundManager`), pass them as parameters to `NewXxxTool()`

### Add a New Middleware
1. Implement `adk.ChatModelAgentMiddleware` in `internal/agent/`
2. Add a functional option `WithXxx()` following the `WithCompaction`, `WithBudget`, `WithRecovery` pattern
3. Wire it up in `internal/command/interactive.go` where the agent is constructed
4. **Insert before approval** in the middleware chain — approval must remain innermost

### Add a New Handler (Transport)
1. Implement `handler.AgentEventHandler` interface in `internal/handler/`
2. Create a new subcommand in `internal/command/` if needed
3. The interface is the only contract — keep handler logic independent of agent internals

### Add a Builtin Skill
1. Create `internal/skills/builtin/{name}/SKILL.md` with frontmatter (`name`, `description`, optional `slash`)
2. It will be automatically embedded via `//go:embed builtin` and loaded by `skills.Loader`
3. Skill override chain (later wins): builtin → `~/.agents/skills/` → `~/.jcode/skills/` → `.jcode/skills/`

### Modify Config Schema
1. Add fields to the appropriate struct in `internal/config/config.go`
2. Use `json:"field_name,omitempty"` tags for optional fields
3. Config is a flat JSON file at `~/.jcode/config.json` — keep it backward-compatible

### Modify Model Registry
- `internal/model/registry_generated.go` is **auto-generated** — edit `script/generate_models.go` instead
- Run `make generate` to regenerate

---

## Things to Avoid

- **Don't write to stdout/stderr.** The TUI controls the terminal. Use `config.Logger()` for debug output.
- **Don't add middleware after approval.** The approval middleware must be the innermost handler in the chain.
- **Don't manually edit `registry_generated.go`.** It's overwritten by `make generate`.
- **Don't use `os.Exit()` in library code.** Only `cmd/jcode/main.go` should exit.
- **Don't store mutable state in tool closures.** Use `*Env` or pass state explicitly. Tools may be re-created across mode transitions (normal ↔ plan).
- **Don't skip `env.ResolvePath()`.** Raw path concatenation can escape the working directory without warning.
- **Don't import `internal/tui` from non-TUI packages.** The handler interface is the decoupling boundary.

---

## Testing

- Run tests: `go test ./...`
- Test files follow `xxx_test.go` convention in the same package
- For tool tests, use `NewEnv()` with a temp directory to isolate file operations
- Lint is mandatory: `make lint` must pass before any PR

---

## Frontend (web/)

- **Stack:** Vue 3 + TypeScript + Vite
- **Build:** `cd web && pnpm install && npx vite build` (or `make build-web`)
- **Output:** builds to `internal/web/dist/`, embedded in Go binary via `//go:embed`
- **Lint:** `cd web && pnpm lint` (eslint + oxlint)
- Changes to the frontend require rebuilding via `make build-web` for the Go binary to pick them up
