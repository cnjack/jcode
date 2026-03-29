# Agent Loop Enhancements — Gap Analysis and Enhancement Plan vs Claude Code

## 1. Problem Statement

Through comprehensive analysis of Claude Code v2.1.77's system prompt (110+ prompt strings, 40+ system reminders, 18 builtin tools), comparing against the current `coding` project, we identify key gaps at the agent loop level **without expanding existing tools or core functionality**.

The current `coding` agent loop is a **straight line**: `User → Agent → Tool → Response → Done`. Claude Code, in contrast, is a **multi-layered onion model**: rich middleware, reminders, context management, and summarization logic before and after each model call.

Core problems:
- **Longer conversations crash more easily**: No context compaction, token exhaustion causes errors directly
- **Agent "forgets"**: Loses initial goals and environmental context mid-conversation
- **Tool errors interrupt flow**: No SafeToolMiddleware, error handling not Eino-native
- **No retry on rate limiting**: 429 causes immediate failure
- **Weak environment awareness**: System prompt only knows pwd/platform/date, not git status, project type, directory structure
- **No summary mechanism**: No summary after long tasks, session resume only has raw history

## 2. Goals & Non-Goals

### Goals
1. Leverage Eino built-in middleware capabilities (summarization, reduction, SafeTool) to strengthen agent loop
2. Design System Reminders mechanism to dynamically inject contextual reminders in agent loop
3. Enhance system prompt's environment awareness capabilities
4. Add conversation summary step
5. Complete callback-based observability
6. Add model retry capability

### Non-Goals
- No new tools (no web search, plan mode, subagent, etc.)
- No changes to TUI main architecture
- No changes to session JSONL format (can extend fields but backward compatible)
- No changes to Eino ChatModelAgent core interface

## 3. Current State Analysis

### 3.1 Claude Code Agent Loop Key Mechanisms (Currently Missing)

| Mechanism | Claude Code Implementation | coding Current State | Priority |
|------|-----------------|----------------|--------|
| **System Reminders** | ~40 condition-triggered reminder injections | ❌ None | 🔴 Critical |
| **Context Compaction** | Auto-summarize when tokens exceed threshold | ❌ No compression at all | 🔴 Critical |
| **Tool Output Reduction** | Auto-truncate/offload when output too long | ❌ Raw output | 🔴 Critical |
| **SafeToolMiddleware** | Convert errors to strings, flow continues | ⚠️ Manual handling (agent.go) | 🟡 High |
| **Model Retry** | Exponential backoff retry for 429, etc. | ❌ None | 🟡 High |
| **Environment Awareness** | git status, diagnostics, file tracking | ⚠️ Only pwd/platform/date | 🟡 High |
| **Conversation Summary** | Generate on conversation end/compaction | ❌ None | 🟡 High |
| **Token Usage Awareness** | Runtime token reminders | ⚠️ TUI display only, agent unaware | 🟡 Medium |
| **BeforeModel/AfterModel hooks** | Modify state before/after each model call | ❌ Unused | 🟡 Medium |
| **Callback Observability** | Complete handler chain | ⚠️ Langfuse manual only | 🟢 Low |

### 3.2 Current Agent Loop Flow

```
main.go: User input
    └─→ runner.Run()
         ├─ tracer.WithNewTrace()
         ├─ runInner(ag, messages)
         │    └─ ag.Run(ctx, input) ← Eino ChatModelAgent
         │         ├─ model.Generate() → tool calls → tool execute → loop
         │         └─ Event stream → TUI messages
         ├─ TodoStore completion guard (max 3 times)
         ├─ Token usage → TUI
         └─ AgentDoneMsg
```

**Problem**: Inside the model call loop in `ag.Run()`, there is no middleware intervention.

### 3.3 Current Middleware Usage

```go
// agent.go — Only uses approval middleware (and not Eino ChatModelAgentMiddleware interface)
middlewares = append(middlewares, adk.AgentMiddleware{
    WrapToolCall: compose.ToolMiddleware{
        Invokable: func(next) { ... approval + error catch ... }
    },
})
```

Eino's `ChatModelAgentMiddleware` interface is not used, including:
- `BeforeAgent` / `BeforeModelRewriteState` / `AfterModelRewriteState`
- `WrapInvokableToolCall` / `WrapStreamableToolCall`
- `WrapModel`

## 4. Proposed Design

### 4.1 Architecture Overview

```
                    ┌─────────────────────────────────────────────────────┐
                    │                    Agent Loop                       │
                    │                                                     │
User Input ──┬──→  │  ┌─────────────────┐                               │
             │     │  │ BeforeAgent      │ ← 环境感知注入               │
             │     │  │ (System Remind)  │                               │
             │     │  └────────┬─────────┘                               │
             │     │           ↓                                         │
             │     │  ┌─────────────────────┐                           │
             │     │  │ BeforeModelRewrite  │ ← Token 提醒 / Compaction │
             │     │  │ (Context Manager)   │                           │
             │     │  └────────┬────────────┘                           │
             │     │           ↓                                         │
             │     │  ┌──────────────┐  retry  ┌────────────────┐       │
             │     │  │ WrapModel    │ ←──────→ │ ModelRetry     │       │
             │     │  │              │          │ (429, timeout) │       │
             │     │  └──────┬───────┘          └────────────────┘       │
             │     │         ↓                                           │
             │     │  ┌─────────────────────┐                           │
             │     │  │ Tool Execution      │                           │
             │     │  │ ┌─SafeToolMW──────┐ │                           │
             │     │  │ │ ┌─ReductionMW─┐ │ │ ← 输出截断/卸载          │
             │     │  │ │ │ Tool.Run()  │ │ │                           │
             │     │  │ │ └─────────────┘ │ │                           │
             │     │  │ └─────────────────┘ │                           │
             │     │  └────────┬────────────┘                           │
             │     │           ↓                                         │
             │     │  ┌─────────────────────┐                           │
             │     │  │ AfterModelRewrite   │ ← 更新 token 计数        │
             │     │  │ (Token Tracking)    │                           │
             │     │  └────────┬────────────┘                           │
             │     │           ↓                                         │
             │     │     [next iteration or done]                        │
             │     └─────────────────────────────────────────────────────┘
             │
             └──→  Post-Run: Summary Step → Session Record
```

### 4.2 Enhancement 1: System Reminders Mechanism

**Location**: `internal/prompts/reminders.go` (new file)

**Design Approach**: Reference Claude Code's ~40 system reminders to implement a conditional reminder injection system. Reminders are injected at the end of messages before each model call via `BeforeModelRewriteState` middleware.

#### Reminder Types

| Reminder | Trigger Condition | Content |
|----------|----------|------|
| **TodoReminder** | TodoStore has items AND iterations > 5 | "You have incomplete todos, please check progress" |
| **TokenUsageReminder** | token usage > 60% context limit | "Used X% context, be mindful of output length" |
| **TokenCriticalReminder** | token usage > 85% context limit | "Context almost exhausted, please finish or summarize soon" |
| **LongRunningReminder** | iterations > 20 AND no todo | "Running for many turns, consider updating plan" |
| **ToolErrorReminder** | 2+ consecutive tool errors | "Multiple tool errors, try a different approach" |
| **EnvironmentReminder** | SSH environment + iterations == 1 | "Currently on remote X environment, watch paths" |

#### Interface Design

```go
// internal/prompts/reminders.go

// Reminder is a conditional reminder
type Reminder struct {
    Name      string
    Condition func(ctx *ReminderContext) bool
    Message   func(ctx *ReminderContext) string
}

// ReminderContext carries agent loop runtime state
type ReminderContext struct {
    Iteration       int      // Current iteration count
    TokensUsed      int64    // Tokens used
    ContextLimit    int      // Model context limit
    TodoStore       *tools.TodoStore
    RecentToolErrors int     // Recent consecutive tool error count
    EnvLabel        string
    IsRemote        bool
}

// CollectReminders returns list of reminders to inject based on current context
func CollectReminders(ctx *ReminderContext) []string
```

#### Injection Method

Via Eino's `ChatModelAgentMiddleware.BeforeModelRewriteState`:

```go
func (m *reminderMiddleware) BeforeModelRewriteState(
    ctx context.Context,
    state *adk.ChatModelAgentState,
    mc *adk.ModelContext,
) (context.Context, *adk.ChatModelAgentState, error) {
    reminders := prompts.CollectReminders(m.buildContext(state))
    if len(reminders) > 0 {
        reminderMsg := "[System Reminder]\n" + strings.Join(reminders, "\n")
        state.Messages = append(state.Messages, schema.SystemMessage(reminderMsg))
    }
    return ctx, state, nil
}
```

### 4.3 Enhancement 2: Context Summarization (Compaction)

**Location**: `internal/agent/agent.go` (Handlers config)

**Use Eino Built-in**: `github.com/cloudwego/eino/adk/middlewares/summarization`

When conversation tokens exceed threshold, automatically compress history messages into summary to prevent context overflow.

```go
import "github.com/cloudwego/eino/adk/middlewares/summarization"

summarizationMW, err := summarization.New(ctx, &summarization.Config{
    Model: chatModel,  // Use same model for summary generation
    Trigger: &summarization.TriggerCondition{
        ContextTokens: int(float64(contextLimit) * 0.75), // Trigger at 75%
    },
})
```

**Coordination with Reminder**:
- When token usage > 60% but < 75% (before summarization triggers), Reminder warns agent to watch output length
- When token usage > 75%, summarization middleware automatically intervenes to compress
- When token usage > 85%, Critical Reminder warns to finish soon

```
Token Usage:
0%─────────60%──────75%───────85%──────100%
           |        |         |
     Reminder    Compact   Critical
     "watch length"  (auto)    "finish soon"
```

### 4.4 Enhancement 3: Tool Output Reduction

**Location**: `internal/agent/agent.go` (Handlers config)

**Use Eino Built-in**: `github.com/cloudwego/eino/adk/middlewares/reduction`

Prevent single tool output (e.g., `read` large file, `grep` many matches, `execute` long output) from overflowing context.

```go
import "github.com/cloudwego/eino/adk/middlewares/reduction"

reductionMW, err := reduction.New(ctx, &reduction.Config{
    Backend:           filesystemBackend, // Offload to local ~/.jcoding/reduction/
    MaxLengthForTrunc: 50000,             // Truncate single tool output > 50000 chars
    MaxTokensForClear: 30000,             // Clear when cumulative tool output tokens > 30000
})
```

**Current Problem Scenario**:
```
User: "Read this 10000-line file"
→ read tool returns all content → context immediately overflows
```

**After Fix**:
```
User: "Read this 10000-line file"
→ read tool returns all content → reduction middleware truncates
→ "[tool output truncated, full content saved to /tmp/xxx]"
→ agent can continue working
```

### 4.5 Enhancement 4: SafeToolMiddleware

**Location**: `internal/agent/middleware.go` (new file)

**Replaces**: Current manual error → string conversion in `agent.go`

Problems with current implementation:
- Error handling at `compose.ToolMiddleware` layer, not Eino-native `ChatModelAgentMiddleware`
- No handling of streaming tool errors
- No distinction for `InterruptRerunError`

```go
type safeToolMiddleware struct {
    *adk.BaseChatModelAgentMiddleware
}

func (m *safeToolMiddleware) WrapInvokableToolCall(
    _ context.Context,
    endpoint adk.InvokableToolCallEndpoint,
    _ *adk.ToolContext,
) (adk.InvokableToolCallEndpoint, error) {
    return func(ctx context.Context, args string, opts ...tool.Option) (string, error) {
        result, err := endpoint(ctx, args, opts...)
        if err != nil {
            if _, ok := compose.IsInterruptRerunError(err); ok {
                return "", err // Interrupt errors need to propagate
            }
            return fmt.Sprintf("[tool error] %v", err), nil
        }
        return result, nil
    }, nil
}
```

### 4.6 Enhancement 5: Model Retry Config

**Location**: `internal/agent/agent.go`

Configure API retry strategy for ChatModelAgent to handle 429 rate limiting and transient network errors.

```go
ModelRetryConfig: &adk.ModelRetryConfig{
    MaxRetries: 3,
    IsRetryAble: func(_ context.Context, err error) bool {
        errStr := err.Error()
        return strings.Contains(errStr, "429") ||
            strings.Contains(errStr, "Too Many Requests") ||
            strings.Contains(errStr, "rate limit") ||
            strings.Contains(errStr, "connection reset")
    },
},
```

### 4.7 Enhancement 6: Enhanced Environment Awareness

**Location**: `internal/prompts/system.md` + `internal/prompts/prompts.go` + `internal/util/util.go`

#### 6a. Enhanced System Prompt Template

```markdown
Current work path: {{ .Pwd }}
Platform: {{ .Platform }}
Date: {{ .Date }}
Current Environment: {{ .EnvLabel }}
{{ if .GitInfo }}
## Git Status
Branch: {{ .GitInfo.Branch }}
{{ if .GitInfo.Dirty }}Working tree has uncommitted changes.{{ end }}
{{ if .GitInfo.LastCommit }}Last commit: {{ .GitInfo.LastCommit }}{{ end }}
{{ end }}
{{ if .ProjectType }}
## Project Type
{{ .ProjectType }}
{{ end }}
{{ if .DirectoryOverview }}
## Directory Overview
{{ .DirectoryOverview }}
{{ end }}
```

#### 6b. Auto-detect Project Type

```go
// internal/util/project.go

func DetectProjectType(pwd string) string {
    checks := map[string]string{
        "go.mod":         "Go module",
        "package.json":   "Node.js/JavaScript",
        "Cargo.toml":     "Rust",
        "pyproject.toml": "Python",
        "pom.xml":        "Java (Maven)",
        "build.gradle":   "Java (Gradle)",
        "Makefile":       "Make-based build",
    }
    var found []string
    for file, name := range checks {
        if _, err := os.Stat(filepath.Join(pwd, file)); err == nil {
            found = append(found, name)
        }
    }
    return strings.Join(found, ", ")
}
```

#### 6c. Git Info Collection

```go
// internal/util/git.go

type GitInfo struct {
    Branch     string
    Dirty      bool
    LastCommit string // one-line summary
}

func GetGitInfo(pwd string) *GitInfo {
    // git rev-parse --abbrev-ref HEAD
    // git status --porcelain
    // git log -1 --oneline
}
```

#### 6d. Directory Overview

```go
func GetDirectoryOverview(pwd string, maxDepth int) string {
    // Generate simplified directory tree (max 2 levels, ignore .git, node_modules, etc.)
    // Similar to Claude Code's codebase exploration
}
```

### 4.8 Enhancement 7: Summary Step

**Location**: `internal/runner/summary.go` (new file)

After agent completes a complex task, generate a structured summary. This has two levels:

#### 7a. Automatic Conversation Summary (During Context Compaction)

Handled automatically by Eino `summarization` middleware (see 4.3).

#### 7b. Task Completion Summary (Runner Layer)

Generate summary at the end of `runner.Run()` when these conditions are met:
1. TodoStore has items AND all completed
2. Conversation turns > 5

```go
// internal/runner/summary.go

// GenerateSummary uses agent itself to generate structured summary of this task
func GenerateSummary(
    ctx context.Context,
    ag *adk.ChatModelAgent,
    history []adk.Message,
    todoStore *tools.TodoStore,
) string {
    summaryPrompt := buildSummaryPrompt(todoStore)
    // Use agent to generate summary (single call, no tools)
    // Output format:
    // ## Task Summary
    // ### Work Completed
    // - ...
    // ### Files Modified
    // - ...
    // ### Notes
    // - ...
}
```

#### 7c. Session Summary (Record to JSONL)

Extend session entry types, add `session_summary`:

```go
const EntrySummary EntryType = "session_summary"

func (r *Recorder) RecordSummary(summary string) {
    _ = r.writeEntry(Entry{Type: EntrySummary, Content: summary})
}
```

This way `--resume` can show summary first instead of full history.

### 4.9 Enhancement 8: Eino Callback Observability

**Location**: `internal/telemetry/callbacks.go` (new file)

Use Eino native `callbacks.Handler` to replace or supplement current manual Langfuse integration.

```go
import "github.com/cloudwego/eino/callbacks"

func NewObservabilityHandler() callbacks.Handler {
    return callbacks.NewHandlerHelper().
        OnStart(func(ctx context.Context, info *callbacks.RunInfo, input callbacks.CallbackInput) context.Context {
            if info != nil {
                config.Logger().Printf("[trace] %s/%s start", info.Component, info.Name)
            }
            return ctx
        }).
        OnEnd(func(ctx context.Context, info *callbacks.RunInfo, output callbacks.CallbackOutput) context.Context {
            if info != nil {
                config.Logger().Printf("[trace] %s/%s end", info.Component, info.Name)
            }
            return ctx
        }).
        OnError(func(ctx context.Context, info *callbacks.RunInfo, err error) context.Context {
            if info != nil {
                config.Logger().Printf("[trace] %s/%s error: %v", info.Component, info.Name, err)
            }
            return ctx
        }).
        Handler()
}

// Register at main.go initialization
callbacks.AppendGlobalHandlers(NewObservabilityHandler())
```

### 4.10 Middleware Composition Order

```go
// internal/agent/agent.go — NewAgent refactoring

Handlers: []adk.ChatModelAgentMiddleware{
    reminderMW,        // Outermost: inject system reminders
    summarizationMW,   // Conversation history summary (when tokens exceed limit)
    reductionMW,       // Tool output reduction
    &safeToolMiddleware{}, // Innermost: tool error capture
},
ModelRetryConfig: &adk.ModelRetryConfig{
    MaxRetries: 3,
    IsRetryAble: retryableCheck,
},
```

Onion model execution order:
```
Request → reminder → summarization → reduction → safeTool → actual execution
Response ← reminder ← summarization ← reduction ← safeTool ← actual execution
```

## 5. Alternatives Considered

### 5.1 Implement Summarization Ourselves vs Use Eino Built-in
**Choose Eino built-in**. Eino's `summarization.New()` already handles token calculation, trigger logic, and summary prompt. Implementing ourselves would require duplicating significant work.

### 5.2 Reminder via system message vs user message injection
**Choose system message**. Claude Code's System Reminders are all injected with system role. This doesn't interfere with the user/assistant alternating format of conversation history. Note that some providers have position restrictions on system messages, may need fallback to user message.

### 5.3 Summary via separate model call vs agent itself
**Choose agent itself**. Don't introduce additional model call cost. Append a summary request message before `runner.Run()` ends to let agent generate.

### 5.4 Environment awareness via static prompt template injection vs dynamic middleware injection
**Choose combination**. Git info, project type, directory overview are static (determined at conversation start), put in prompt template. Token usage, todo status are dynamic, inject via reminder middleware.

## 6. Implementation Plan (Priority Order)

### Phase 1: Safety and Stability Foundation (P0)

| # | Task | Files Involved | Change Size |
|---|------|---------|--------|
| 1.1 | SafeToolMiddleware — Replace manual error handling in agent.go with Eino-native approach | `internal/agent/middleware.go` (new), `internal/agent/agent.go` | S |
| 1.2 | ModelRetryConfig — Add 429/rate-limit retry | `internal/agent/agent.go`, `internal/config/config.go` | S |
| 1.3 | Tool Output Reduction — Integrate Eino reduction middleware | `internal/agent/agent.go` | S |

### Phase 2: Context Management (P1)

| # | Task | Files Involved | Change Size |
|---|------|---------|--------|
| 2.1 | Summarization Middleware — Integrate Eino summarization | `internal/agent/agent.go` | M |
| 2.2 | System Reminders Framework — Implement Reminder mechanism + BeforeModelRewriteState | `internal/prompts/reminders.go` (new), `internal/agent/middleware.go` | M |
| 2.3 | Token/Todo/Error Reminders — Implement specific reminder rules | `internal/prompts/reminders.go` | S |

### Phase 3: Environment Awareness (P1)

| # | Task | Files Involved | Change Size |
|---|------|---------|--------|
| 3.1 | Git Info Collection | `internal/util/git.go` (new) | S |
| 3.2 | Project Type Detection | `internal/util/project.go` (new) | S |
| 3.3 | Directory Overview | `internal/util/project.go` | S |
| 3.4 | System Prompt Template Enhancement | `internal/prompts/system.md`, `internal/prompts/prompts.go` | S |

### Phase 4: Summary & Observability (P2)

| # | Task | Files Involved | Change Size |
|---|------|---------|--------|
| 4.1 | Task Completion Summary | `internal/runner/summary.go` (new), `internal/runner/runner.go` | M |
| 4.2 | Session Summary Recording | `internal/session/session.go` | S |
| 4.3 | Eino Callback Handler | `internal/telemetry/callbacks.go` (new), `cmd/coding/main.go` | S |

## 7. Migration & Compatibility

- **Middleware Migration**: `agent.go`'s `AgentMiddleware.WrapToolCall` needs to migrate to `ChatModelAgentMiddleware` interface. Approval middleware stays at outermost layer but needs interface adaptation.
- **Config Extension**: `config.json` can add `MaxRetries`, `SummarizationThreshold` etc. fields with reasonable defaults, no user configuration needed.
- **Session Backward Compatibility**: `session_summary` is new entry type, old resume code will automatically skip unknown types.
- **Go Module Dependencies**: Need to confirm version compatibility of `github.com/cloudwego/eino/adk/middlewares/summarization` and `reduction`.

## 8. Open Questions

1. **Eino ChatModelAgentMiddleware vs AgentMiddleware**: Current code uses `adk.AgentMiddleware` (Eino old interface?). Need to confirm if latest Eino has unified to `ChatModelAgentMiddleware` interface, and how approval middleware should migrate.
2. **Summarization Model**: Will using same chatModel for summarization incur extra cost? Should we configure a cheaper model specifically for summaries?
3. **Reduction Backend**: Offload tool output to `~/.jcoding/reduction/` or `/tmp`? Former can be reused across sessions, latter auto-cleans.
4. **System Reminder Injection Position**: Some API providers don't support inserting system messages mid-sequence, need fallback strategy.
5. **Directory Overview Depth**: Too shallow lacks information, too deep wastes tokens. Need user testing to determine optimal `maxDepth` (suggest default 2 levels).
