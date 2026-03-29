# Environment Awareness

> **Implemented**: 2026-03-29, Eino v0.8.5
> **Priority**: P2

## 1. Problem

The agent's system prompt previously contained only `Pwd`, `Platform`, `Date`, and `EnvLabel`. Every session started with the agent running `cat go.mod` and `ls` to orient itself — facts that are knowable at startup in < 100ms.

## 2. Implementation

### 2.1 `internal/util/envinfo.go`

```go
type EnvInfo struct {
    GitBranch   string // empty if not a git repo
    GitDirty    bool
    LastCommit  string // "%h %s" format, empty if no commits
    ProjectType string // comma-joined detected markers, e.g. "Go module"
    DirTree     string // shallow directory tree, depth ≤ 2
}

func CollectEnvInfo(pwd string) *EnvInfo
```

All errors are suppressed — missing data is represented as empty strings/false. Each shell command runs with a 2-second timeout.

**Git info**: Three `git -C <pwd> ...` calls:
- `rev-parse --abbrev-ref HEAD` → branch name
- `status --porcelain` → non-empty means dirty
- `log -1 --format=%h %s` → last commit hash + subject

**Project type**: Checks for marker files in `pwd` (`go.mod`, `package.json`, `Cargo.toml`, `pyproject.toml`, `setup.py`, `pom.xml`, `build.gradle`). Multiple markers produce a comma-joined string.

**Dir tree**: `buildDirTree(pwd, depth=2, maxLines=200)` walks the directory, skipping hidden dirs, `vendor`, `node_modules`, and binary files.

### 2.2 Prompt Injection

Both `GetSystemPrompt` and `GetPlanSystemPrompt` accept `*utils.EnvInfo`:

```go
func GetSystemPrompt(platform, pwd, envLabel string, envInfo *utils.EnvInfo) string
func GetPlanSystemPrompt(platform, pwd, envLabel string, envInfo *utils.EnvInfo) string
```

The template conditionally renders each field:

```markdown
{{ if .GitBranch }}
Git branch: {{ .GitBranch }}{{ if .GitDirty }} (uncommitted changes present){{ end }}
{{ if .LastCommit }}Last commit: {{ .LastCommit }}{{ end }}
{{ end }}
{{ if .ProjectType }}Project type: {{ .ProjectType }}{{ end }}
{{ if .DirTree }}
## Directory Overview
\`\`\`
{{ .DirTree }}
\`\`\`
{{ end }}
```

### 2.3 Lifecycle

`CollectEnvInfo(pwd)` is called once at startup in `cmd/coding/main.go`:

```go
envInfo := util.CollectEnvInfo(pwd)
systemPrompt = prompts.GetSystemPrompt(platform, pwd, "local", envInfo)
```

When `switch_env` changes the active environment the agent is recreated; `envInfo` is passed to `GetPlanSystemPrompt` or `GetSystemPrompt`. Remote SSH environments receive `nil` (no git collection on remote machines).

## 3. Non-Goals

- Dynamic re-injection during session (git state is read once at start; runtime context is handled by [system reminders](system_reminders.md)).
- Git collection on remote SSH environments.
