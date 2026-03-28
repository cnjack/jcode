# Environment Awareness

> **Last verified**: 2026-03-28 against codebase at `github.com/cloudwego/eino v0.7.37`
> **Priority**: P2 — Nice to have
> **Scope reduction**: Claude Code only injects `git status` (97 tokens) + platform/cwd at session start. We follow suit: git branch + dirty flag + project type. Directory tree is **deferred** (the agent can use `grep`/`read` to explore).

## 1. Problem Statement

The agent's system prompt currently contains only four pieces of environment context:

```
Current work path: /home/user/myproject
Platform: linux/amd64
Date: 2026-03-17
Current Environment: local
```

This is enough for the agent to locate files, but it leaves out context that meaningfully changes how it should behave:

- **Git state**: The agent doesn't know which branch it's on, whether there are uncommitted changes, or what the last commit was. It may make assumptions (e.g. safe to edit, already in a clean state) that are wrong.
- **Project type**: The agent can't know without reading files whether it's in a Go module, a Node.js project, or a Python package. Without this, the first thing it does in every session is `cat go.mod` or `ls`.
- **Directory structure**: The agent has no mental map of the project layout. It relies on `grep` and `read` to locate files it should already know about.

These facts are knowable at session start with a few shell commands (< 100ms total) and remain stable for the duration of a session.

**2026 AI Landscape Note**: Rich environment context injection is standard practice across all production coding assistants (Cursor, Windsurf, Claude Code, Copilot). All of them inject git state, project markers, and directory overviews into their system prompts. This design aligns with established patterns.

## 2. Goals & Non-Goals

**Goals**
- Inject git branch, dirty-state, and last-commit summary into the system prompt.
- Auto-detect project type from common marker files.
- Include a shallow directory tree (depth ≤ 2, ignoring noise directories).
- Suppress any of the above gracefully when data is unavailable (non-git repo, network error, etc.).

**Non-Goals**
- Dynamic re-injection during the session (git state is read once at start; see System Reminders design for runtime context).
- SSH environments — git info collection runs on the local machine only at session start. When `switch_env` activates, the new agent is recreated, but remote git info gathering is out of scope for this design.
- Adding new tools.

## 3. Current State

### `internal/prompts/system.md`

The system prompt template currently includes:

```markdown
Current work path: {{ .Pwd }}
Platform: {{ .Platform }}
Date: {{ .Date }}
Current Environment: {{ .EnvLabel }}

{{if .SSHAliases}}Available target environments for 'switch_env' tool:
- local
{{range .SSHAliases}}- {{.Name}} ({{.Addr}})
{{end}}{{end}}
```

The system prompt also includes a `# Rules` section, `# Tools Available` section, and `# Workflow` section. Additionally, `GetSystemPrompt()` appends the content of `AGENTS.md` if present in the working directory.

Template data struct in `internal/prompts/prompts.go`:

```go
struct {
    Platform   string
    Pwd        string
    Date       string
    EnvLabel   string
    SSHAliases []config.SSHAlias
}
```

`GetSystemPrompt` function signature: `func GetSystemPrompt(platform, pwd, envLabel string) string`

### `internal/util/util.go`

Package `utils` (note: Go package name is `utils`, import path is `internal/util`). Only `GetWorkDir()` and `GetSystemInfo()` exist. No git or project-type utilities.

## 4. Proposed Design

### 4.1 New File: `internal/util/envinfo.go`

Collect all session-start environment facts in one place.

```go
// EnvInfo is collected once at session start and injected into the system prompt.
type EnvInfo struct {
    GitBranch   string // empty if not a git repo
    GitDirty    bool
    LastCommit  string // one-line: hash + subject, empty if no commits
    ProjectType string // comma-joined list of detected project markers
    DirTree     string // shallow directory tree, empty on error
}

// Collect gathers environment facts for pwd.
// All errors are suppressed — missing data is represented as empty strings/false.
func Collect(pwd string) *EnvInfo
```

#### Git info

Three fast commands, each with a 2-second timeout:

```
git -C <pwd> rev-parse --abbrev-ref HEAD          → branch name
git -C <pwd> status --porcelain                   → dirty if output non-empty
git -C <pwd> log -1 --format="%h %s"              → last commit
```

Errors (not a git repo, no commits yet) produce empty strings.

#### Project type detection

Check for marker files in `pwd` (no recursion):

| File | Label |
|------|-------|
| `go.mod` | `Go module` |
| `package.json` | `Node.js` |
| `Cargo.toml` | `Rust` |
| `pyproject.toml` / `setup.py` | `Python` |
| `pom.xml` | `Java (Maven)` |
| `build.gradle` | `Java (Gradle)` |
| `Makefile` | `Make` |
| `Dockerfile` | `Docker` |

Multiple labels are comma-joined. If none match, the field is empty and the section is omitted from the prompt.

#### Directory tree

A Go implementation — no shell dependency. Walks `pwd` to depth 2, ignoring `.git`, `node_modules`, `vendor`, `.cache`, `dist`, `build`, `__pycache__`. Produces a compact indented tree:

```
myproject/
  cmd/
    coding/
  internal/
    agent/
    config/
  docs/
  go.mod
  Makefile
```

Capped at 200 lines to avoid prompt bloat.

### 4.2 Enhanced Template Data

```go
// internal/prompts/prompts.go

type promptData struct {
    Platform   string
    Pwd        string
    Date       string
    EnvLabel   string
    SSHAliases []config.SSHAlias
    // New:
    GitBranch  string
    GitDirty   bool
    LastCommit string
    ProjectType string
    DirTree    string
}
```

### 4.3 Updated `internal/prompts/system.md`

```markdown
Current work path: {{ .Pwd }}
Platform: {{ .Platform }}
Date: {{ .Date }}
Current Environment: {{ .EnvLabel }}
{{ if .GitBranch }}
Git branch: {{ .GitBranch }}{{ if .GitDirty }} (uncommitted changes present){{ end }}
{{ if .LastCommit }}Last commit: {{ .LastCommit }}{{ end }}
{{ end }}
{{ if .ProjectType }}
Project type: {{ .ProjectType }}
{{ end }}
{{ if .DirTree }}
## Directory Overview

{{ .DirTree }}
{{ end }}
```

### 4.4 Wiring in `cmd/coding/main.go`

Current `GetSystemPrompt` signature is `func GetSystemPrompt(platform, pwd, envLabel string) string`. Two options for integration:

**Option A — Extend signature** (recommended):
```go
envInfo := utils.Collect(pwd)
systemPrompt := prompts.GetSystemPrompt(platform, pwd, "local", envInfo)
```

**Option B — Inline into template data**:
```go
envInfo := utils.Collect(pwd)
// Pass envInfo fields directly to the template struct
```

Either way, the new fields are populated before agent creation and injected once.

When `env.OnEnvChange` fires (SSH connect/disconnect), `envInfo` is re-used as-is (local git info remains unchanged). Re-collecting for SSH environments is out of scope.

### 4.5 Token Budget

Estimated token cost of the new sections per session start:

| Section | Typical size | Tokens (est.) |
|---------|-------------|--------------|
| Git info | 2–3 lines | ~20 |
| Project type | 1 line | ~10 |
| Directory tree (depth 2) | 10–30 lines | ~80–150 |
| **Total overhead** | | **~110–180** |

This is a one-time cost at session start, well within acceptable bounds.

## 5. Alternatives Considered

### Run git commands via the `execute` tool during the session
Rejected. This costs a model iteration and requires approval (or safe-prefix check). Static injection at prompt-render time is free and available before the first message.

### Inject directory overview via the `BeforeChatModel` middleware hook instead of template
Possible, but the directory tree is static (depth-2 snapshot of cwd at start). Adding it to an `AgentMiddleware.BeforeChatModel` hook provides no benefit over the template and complicates the middleware ordering. Only dynamic data belongs in middleware hooks.

## 6. Codebase Verification Notes

- **Confirmed**: `internal/prompts/system.md` template matches the described current state, including `SSHAliases` conditional block.
- **Confirmed**: `internal/prompts/prompts.go` template data struct matches described fields.
- **Confirmed**: `internal/util/util.go` (package `utils`) has only `GetWorkDir()` and `GetSystemInfo()`.
- **Confirmed**: `prompts.GetSystemPrompt()` also appends `AGENTS.md` content via `loadAgentsMd(pwd)` — the new environment sections should be injected before this append.
- **Note**: The approval logic in `internal/runner/approval.go` auto-approves `git status` and `git log` commands, confirming these are considered safe. But collecting git info at prompt-render time is still preferred (zero model iterations).

### Include full `git diff --stat` of uncommitted changes
Rejected. This can be very large (hundreds of lines). The dirty flag is sufficient to inform the agent that changes exist; it can run `git diff --stat` itself if needed.

## 6. Migration & Compatibility

- New file `internal/util/envinfo.go` — no existing code deleted.
- `internal/prompts/prompts.go` and `system.md` are extended. The template struct gains new optional fields; zero values render empty sections (no visual impact when data is unavailable).
- `cmd/coding/main.go` gains a `util.Collect(pwd)` call before agent creation — a fast, error-swallowing function.
- No changes to tools, TUI, session format, or config schema.

## 7. Open Questions

1. **SSH environments**: When the agent switches to a remote machine via `switch_env`, should the system prompt be re-rendered with remote git info? This would require running git commands over SSH, which is possible (the `SSHExecutor` supports arbitrary command execution) but adds latency and complexity.
2. **Directory tree depth**: Depth 2 is proposed as the default. Projects with very flat or very deep layouts may need adjustment. Should depth be configurable in `config.json`?
3. **Refresh on `cd`**: If the agent is instructed to work in a subdirectory (e.g. a monorepo subpackage), the initial directory tree may become misleading. The env info is currently collected once. A `BeforeAgent` hook could re-collect on each user turn, but this adds ~50ms overhead per turn.
