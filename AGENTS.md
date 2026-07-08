# AGENTS.md — JCode Project Development Guide

Go coding agent — [Eino](https://github.com/cloudwego/eino) + BubbleTea v2 TUI + Vue 3 web UI + Tauri 2 desktop shell.

- **Module:** `github.com/cnjack/jcode` | **Entry:** `cmd/jcode/` | **Config dir:** `~/.jcode/`

---

## Quick Start

```bash
make build          # generate → build-web → go build
make install        # generate → build-web → go install
make run            # go run ./cmd/jcode/
make lint           # golangci-lint + eslint/oxlint (lint-go / lint-web)
make doctor         # system check
make desktop-dev    # Tauri desktop app in dev mode (rebuilds the Go sidecar first)
make desktop-build  # distributable desktop bundle (.app/.dmg/.msi)
make build-ble      # optional jcode-ble helper (BLE is a separate binary by design)
```

- `make build-web` requires `pnpm`. Frontend builds to `internal/web/dist/`.
- `make generate` runs `go generate` for `internal/model/...` (fetches models.dev data → `registry_generated.go`) **and** `internal/theme/...` (palette → web CSS/TS tokens). **Never edit generated files by hand.**
- Build injects `Version`, `BuildTime`, `GitCommit` via ldflags into `internal/command`.

---

## Architecture Overview

```
cmd/jcode/           # Entry: cobra root (interactive) + subcommands; native-messaging host detour
cmd/jcode-ble/       # BLE helper binary (spawned by the main binary; keeps CoreBluetooth out of jcode)
internal/
  command/           # Subcommand implementations + interactive session orchestration + tool wiring
  agent/             # ChatModelAgent factory + middleware chain
  runner/            # Agent run loop + event bus + approval tiering
  handler/           # AgentEventHandler interface (TUI / ACP / Web implementations)
  tools/             # All built-in tools + Executor/Env abstraction
  model/             # OpenAI-compatible chat model + model registry (build-time generated)
  config/            # JSON config loader + logger
  prompts/           # System/plan prompts + AGENTS.md injection + env info
  session/           # JSONL session recording/replay
  skills/            # Skill loader (builtin → user → project override chain)
  team/              # Multi-agent team coordination
  mode/              # Unified session mode (Ask / Plan / Autopilot) shared by all transports
  automation/        # Scheduled/manual agent runs (each run = a tagged session)
  browser/           # Browser use: managed Chrome (CDP) + extension WS bridge + native-messaging host
  remote/            # SSH / Docker connection helpers (UI-agnostic; /api/remote/*)
  channel/           # External messaging channels (e.g. WeChat push)
  theme/             # Single source of truth for built-in themes (generates web CSS/TS)
  usage/             # Token-usage recording + aggregation (stats views)
  feature/           # Compile-time feature flags via build tags (e.g. desktop, jcode_headless, ble)
  telemetry/         # Optional Langfuse tracing
  tui/               # BubbleTea v2 TUI components
  web/               # HTTP server (REST + WS + PTY) + embedded Vue dist
web/                 # Vue 3 + Vite + TypeScript frontend source (the CURRENT product UI)
web-react/           # React 18 + Vite + RTK product app (migration in progress; parallel to web/)
packages/            # pnpm workspace: the reusable jcode-ui component library
  jcode-ui/          #   published styled React chat components (→ npm: jcode-ui)
  jcode-ui-core/     #   framework-agnostic core: types, ChatRuntime, headless primitives
site/                # React + Vite marketing/docs site → www.j-code.net (docs markdown in site/docs/)
desktop/             # Tauri 2 desktop shell; the Go binary runs as a sidecar
extension/           # jcode Browser Bridge Chrome extension (MV3) for the browser-use extension backend
internal-doc/        # Internal design docs (NOT published; site/docs is the published documentation)
script/              # Build-time code generation + install.sh
agent-eval/          # Agent evaluation harness + showcase generation
```

### Frontend migration (Vue → React) — in progress

The product UI is migrating from Vue 3 (`web/`) to React 18 (`web-react/`),
built on a new reusable component library (`packages/jcode-ui` + `jcode-ui-core`).
**During the migration both coexist:**

- `make build-web` (default) builds the **Vue** app → `internal/web/dist/` (production).
- `make build-web-react` builds the **React** app + packages → `internal/web/dist-react/` (parallel validation).
- `make lint-react` typechecks the React app + both packages.
- The Go `embed.FS` and Tauri `frontendDist` still point at the Vue `dist/`. The switch-over happens once `web-react` reaches feature parity.

The component library is the migration's organizing principle — see `packages/jcode-ui/README.md` and `site/docs/chat-ui/`. It's published to npm as `jcode-ui` (styled) + `jcode-ui-core` (headless). The runtime abstraction (`ChatRuntime` + `createExternalStoreRuntime`) is the seam that lets the components render from any Redux-shaped store.

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
- Exclude the script/ directory from the linter — it contains code generation scripts that may not follow all conventions.

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
4. Register it in **all three transports**: `buildAllTools()` / `buildPlanTools()` in `internal/command/interactive.go`, and the inline `all`/`plan` tool lists in `acp.go` and `web.go`
5. If the tool is read-only, also add it to the plan-mode list
6. **Approval policy:** Read-only tools skip approval. Mutating tools require approval unless `AutoApprove` is set. Match existing patterns in `internal/runner/approval.go` (browser tools additionally tier by action/origin there).

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
2. Add to `buildAllTools()`/`buildPlanTools()` in `interactive.go` and the inline tool lists in `acp.go` and `web.go`
3. If read-only and useful in plan mode, also add to the plan-mode lists
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

### Write Documentation
- **Published docs** live in `site/docs/**/*.md` (rendered at https://www.j-code.net/docs by the React site in `site/`). Pages need front-matter `title` + `nav_order` (+ `parent` for children) to appear in the nav.
- **Internal design docs** (PRDs, design notes, research) go in `internal-doc/` — never in `site/docs/`.
- Doc screenshots live in `site/docs/asset/` (for GitHub rendering) and `site/public/docs-asset/` (served by the site) — keep both copies in sync.

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

## Frontend (web/) — Vue (production)

> **Note:** the product UI is migrating to React (`web-react/` + `packages/jcode-ui`). The Vue app remains the production build during the migration. New reusable UI work goes in `packages/jcode-ui` (React); see the migration section above and `packages/jcode-ui/README.md`.

- **Stack:** Vue 3 + TypeScript + Vite
- **Build:** `cd web && pnpm install && npx vite build` (or `make build-web`)
- **Output:** builds to `internal/web/dist/`, embedded in Go binary via `//go:embed`
- **Lint:** `cd web && pnpm lint` (eslint + oxlint)
- Changes to the frontend require rebuilding via `make build-web` for the Go binary to pick them up
- **Don't confuse `web/` with `site/`:** `web/` (Vue) is the product UI embedded in the binary and reused by the desktop app; `site/` (React) is the public website + docs at www.j-code.net and is deployed separately (`cd site && pnpm build`).

### Icons & Styling

- **Icons:** use `@heroicons/vue/24/outline` exclusively. Import each icon by name from its subpath (`import { XMarkIcon } from '@heroicons/vue/24/outline'`) for per-file tree-shaking. Do **not** hand-write inline `<svg>` icons or `v-html` SVG path strings.
- **Icon sizing:** use Tailwind `w-N h-N` classes (e.g. `class="w-3.5 h-3.5"`), never a `:size` prop.
- **Colors:** every color must come from a CSS custom property defined in `src/styles/tokens.css`. Never hardcode hex/rgb/`#fff`/`white` in `.vue` or `.css`. Text on the primary/destructive fills uses `--color-on-primary` / `--color-on-destructive`; code blocks use `--code-bg` / `--code-border`; syntax highlighting uses `--hljs-*`.
- **Terminal (xterm) colors:** live in tokens (`--term-*` and the 16-color ANSI palette) and are read at runtime via `getComputedStyle` in `TerminalInstance.vue` — see `termTheme()`. Do not define terminal colors inline.
- **Adding a new color:** add the token to `tokens.css` (both `:root` light and `.dark`) first, then reference it by `var(...)`. To theme it per generated theme, edit `internal/theme/palette.go` and regenerate — never edit `tokens.generated.css` by hand.
