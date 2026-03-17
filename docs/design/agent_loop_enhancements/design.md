# Agent Loop Enhancements — 对标 Claude Code 的缺失分析与增强方案

## 1. Problem Statement

通过对 Claude Code v2.1.77 的系统提示全面分析（110+ prompt 字符串、40+ system reminders、18 builtin tools），对比当前 `coding` 项目，在 **不扩充现有 tool 和主体功能** 的前提下，识别出 agent loop 层面的关键缺失。

当前 `coding` 的 agent loop 是一条 **直线**：`User → Agent → Tool → Response → Done`。而 Claude Code 是一个 **多层洋葱模型**：在每一轮 model 调用前后都有丰富的中间件、reminders、context 管理和 summarization 逻辑。

核心问题：
- **对话越长越容易崩溃**：无 context compaction，token 用尽直接报错
- **Agent "失忆"**：长对话中期忘记初始目标、环境上下文
- **Tool 报错中断流程**：没有 SafeToolMiddleware，error 处理不够 Eino-native
- **模型限流没有重试**：429 一下就挂
- **环境感知薄弱**：System prompt 只知道 pwd/platform/date，不知道 git 状况、项目类型、目录结构
- **没有总结机制**：长任务结束无 summary，session resume 只有原始历史

## 2. Goals & Non-Goals

### Goals
1. 利用 Eino 内置 middleware 能力（summarization、reduction、SafeTool）强化 agent loop
2. 设计 System Reminders 机制，在 agent loop 中动态注入上下文提醒
3. 增强 system prompt 的环境感知能力
4. 增加对话 summary 步骤
5. 完善 callback-based observability
6. 增加 model retry 能力

### Non-Goals
- 不新增 tool（不加 web search, plan mode, subagent 等）
- 不改变 TUI 主体架构
- 不改变 session JSONL 格式（可扩展字段但向后兼容）
- 不改变 Eino ChatModelAgent 的核心接口

## 3. Current State Analysis

### 3.1 Claude Code 的 Agent Loop 关键机制（当前缺失）

| 机制 | Claude Code 实现 | coding 当前状态 | 重要性 |
|------|-----------------|----------------|--------|
| **System Reminders** | ~40 种条件触发的提醒注入 | ❌ 零 | 🔴 Critical |
| **Context Compaction** | token 超阈值自动 summarize | ❌ 无任何压缩 | 🔴 Critical |
| **Tool Output Reduction** | 输出过长自动截断/卸载 | ❌ 原始输出 | 🔴 Critical |
| **SafeToolMiddleware** | 错误转字符串，flow 继续 | ⚠️ 手动处理（agent.go） | 🟡 High |
| **Model Retry** | 指数退避重试 429 等 | ❌ 无 | 🟡 High |
| **环境感知** | git status, diagnostics, file tracking | ⚠️ 仅 pwd/platform/date | 🟡 High |
| **Conversation Summary** | 对话结束/compaction 时生成 | ❌ 无 | 🟡 High |
| **Token Usage Awareness** | runtime token 提醒 | ⚠️ 仅 TUI 展示，agent 不知道 | 🟡 Medium |
| **BeforeModel/AfterModel hooks** | 每轮 model 调用前后可修改 state | ❌ 未使用 | 🟡 Medium |
| **Callback Observability** | 完整 handler 链 | ⚠️ 仅 Langfuse manual | 🟢 Low |

### 3.2 当前 Agent Loop 流程

```
main.go: 用户输入
    └─→ runner.Run()
         ├─ tracer.WithNewTrace()
         ├─ runInner(ag, messages)
         │    └─ ag.Run(ctx, input) ← Eino ChatModelAgent
         │         ├─ model.Generate() → tool calls → tool execute → loop
         │         └─ 事件流 → TUI messages
         ├─ TodoStore completion guard (最多 3 次)
         ├─ Token usage → TUI
         └─ AgentDoneMsg
```

**问题**：在 `ag.Run()` 内部的 model 调用循环中，没有任何中间层介入。

### 3.3 当前 Middleware 使用情况

```go
// agent.go — 仅使用了 approval middleware（且非 Eino ChatModelAgentMiddleware 接口）
middlewares = append(middlewares, adk.AgentMiddleware{
    WrapToolCall: compose.ToolMiddleware{
        Invokable: func(next) { ... approval + error catch ... }
    },
})
```

未使用 Eino 的 `ChatModelAgentMiddleware` 接口，包括：
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

**位置**: `internal/prompts/reminders.go` (新文件)

**设计思路**: 参考 Claude Code 的 ~40 种 system reminders，实现一个条件化的 reminder 注入系统。Reminders 通过 `BeforeModelRewriteState` middleware 在每次 model 调用前注入到 messages 末尾。

#### Reminder 类型

| Reminder | 触发条件 | 内容 |
|----------|----------|------|
| **TodoReminder** | TodoStore 有 items 且 iterations > 5 | "你还有未完成的 todos，请检查进度" |
| **TokenUsageReminder** | token 用量 > 60% context limit | "已使用 X% context，注意控制输出长度" |
| **TokenCriticalReminder** | token 用量 > 85% context limit | "Context 即将用尽，请尽快完成或总结" |
| **LongRunningReminder** | iterations > 20 且无 todo | "已运行多轮，考虑是否需要更新计划" |
| **ToolErrorReminder** | 连续 2+ 次 tool error | "多次工具报错，换一个思路试试" |
| **EnvironmentReminder** | SSH 环境 + iterations == 1 | "当前在远端 X 环境操作，注意路径" |

#### 接口设计

```go
// internal/prompts/reminders.go

// Reminder 是一个条件化的提醒
type Reminder struct {
    Name      string
    Condition func(ctx *ReminderContext) bool
    Message   func(ctx *ReminderContext) string
}

// ReminderContext 携带 agent loop 的运行时状态
type ReminderContext struct {
    Iteration       int      // 当前迭代次数
    TokensUsed      int64    // 已用 token
    ContextLimit    int      // model context 上限
    TodoStore       *tools.TodoStore
    RecentToolErrors int     // 最近连续 tool error 次数
    EnvLabel        string
    IsRemote        bool
}

// CollectReminders 根据当前上下文返回需要注入的提醒列表
func CollectReminders(ctx *ReminderContext) []string
```

#### 注入方式

通过 Eino 的 `ChatModelAgentMiddleware.BeforeModelRewriteState`:

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

**位置**: `internal/agent/agent.go` (Handlers 配置)

**利用 Eino 内置**: `github.com/cloudwego/eino/adk/middlewares/summarization`

当对话 token 超过阈值时，自动将历史消息压缩为摘要，防止 context overflow。

```go
import "github.com/cloudwego/eino/adk/middlewares/summarization"

summarizationMW, err := summarization.New(ctx, &summarization.Config{
    Model: chatModel,  // 用同一个 model 生成摘要
    Trigger: &summarization.TriggerCondition{
        ContextTokens: int(float64(contextLimit) * 0.75), // 75% 时触发
    },
})
```

**与 Reminder 的协同**：
- 当 token 使用 > 60% 但 < 75%（summarization 触发前），Reminder 提醒 agent 注意输出长度
- 当 token 使用 > 75%，summarization middleware 自动介入压缩
- 当 token 使用 > 85%，Critical Reminder 提醒尽快完成

```
Token 使用:
0%─────────60%──────75%───────85%──────100%
           |        |         |
     Reminder    Compact   Critical
     "注意长度"  (自动)    "尽快结束"
```

### 4.4 Enhancement 3: Tool Output Reduction

**位置**: `internal/agent/agent.go` (Handlers 配置)

**利用 Eino 内置**: `github.com/cloudwego/eino/adk/middlewares/reduction`

防止单个 tool 输出（比如 `read` 一个大文件、`grep` 大量匹配、`execute` 长输出）撑爆 context。

```go
import "github.com/cloudwego/eino/adk/middlewares/reduction"

reductionMW, err := reduction.New(ctx, &reduction.Config{
    Backend:           filesystemBackend, // 卸载到本地 ~/.jcoding/reduction/
    MaxLengthForTrunc: 50000,             // 单次 tool 输出 > 50000 字符截断
    MaxTokensForClear: 30000,             // 累计 tool 输出 token > 30000 时清理
})
```

**当前问题场景**：
```
用户: "读取这个 10000 行的文件"
→ read tool 返回全部内容 → context 立刻爆掉
```

**修复后**：
```
用户: "读取这个 10000 行的文件"
→ read tool 返回全部内容 → reduction middleware 截断
→ "[tool output truncated, full content saved to /tmp/xxx]"
→ agent 可以继续工作
```

### 4.5 Enhancement 4: SafeToolMiddleware

**位置**: `internal/agent/middleware.go` (新文件)

**替换**: 当前 `agent.go` 中手动的 error → string 转换

当前实现的问题：
- 在 `compose.ToolMiddleware` 层做 error 处理，不是 Eino-native 的 `ChatModelAgentMiddleware`
- 没有处理 streaming tool errors
- 没有区分 `InterruptRerunError`

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
                return "", err // 中断错误需要传播
            }
            return fmt.Sprintf("[tool error] %v", err), nil
        }
        return result, nil
    }, nil
}
```

### 4.6 Enhancement 5: Model Retry Config

**位置**: `internal/agent/agent.go`

为 ChatModelAgent 配置 API 重试策略，处理 429 限流和临时网络错误。

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

**位置**: `internal/prompts/system.md` + `internal/prompts/prompts.go` + `internal/util/util.go`

#### 6a. 增强 System Prompt 模板

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

#### 6b. 自动检测项目类型

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

#### 6c. Git 信息收集

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

#### 6d. 目录概览

```go
func GetDirectoryOverview(pwd string, maxDepth int) string {
    // 生成简化的目录树（最多 2 层，忽略 .git, node_modules 等）
    // 类似 Claude Code 的 codebase exploration
}
```

### 4.8 Enhancement 7: Summary Step

**位置**: `internal/runner/summary.go` (新文件)

在 agent 完成复杂任务后，生成一个结构化的 summary。这分为两个层面：

#### 7a. 自动 Conversation Summary（Context Compaction 时）

由 Eino `summarization` middleware 自动处理（见 4.3）。

#### 7b. Task Completion Summary（Runner 层）

当满足以下条件时，在 `runner.Run()` 的最后阶段生成 summary：
1. TodoStore 有 items 且全部完成
2. 对话轮次 > 5

```go
// internal/runner/summary.go

// GenerateSummary 用 agent 自身生成本次任务的结构化摘要
func GenerateSummary(
    ctx context.Context,
    ag *adk.ChatModelAgent,
    history []adk.Message,
    todoStore *tools.TodoStore,
) string {
    summaryPrompt := buildSummaryPrompt(todoStore)
    // 用 agent 生成 summary（单轮调用，不使用 tools）
    // 输出格式：
    // ## 任务摘要
    // ### 完成的工作
    // - ...
    // ### 修改的文件
    // - ...
    // ### 注意事项
    // - ...
}
```

#### 7c. Session Summary（录制到 JSONL）

扩展 session entry 类型，新增 `session_summary`:

```go
const EntrySummary EntryType = "session_summary"

func (r *Recorder) RecordSummary(summary string) {
    _ = r.writeEntry(Entry{Type: EntrySummary, Content: summary})
}
```

这样 `--resume` 时可以先展示 summary 而非全部历史。

### 4.9 Enhancement 8: Eino Callback Observability

**位置**: `internal/telemetry/callbacks.go` (新文件)

用 Eino 原生 `callbacks.Handler` 替代或补充当前的手动 Langfuse 集成。

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

// 在 main.go 初始化时注册
callbacks.AppendGlobalHandlers(NewObservabilityHandler())
```

### 4.10 Middleware 组合顺序

```go
// internal/agent/agent.go — NewAgent 改造

Handlers: []adk.ChatModelAgentMiddleware{
    reminderMW,        // 最外层：注入 system reminders
    summarizationMW,   // 对话历史摘要（token 超限时）
    reductionMW,       // tool 输出缩减
    &safeToolMiddleware{}, // 最内层：tool 错误捕获
},
ModelRetryConfig: &adk.ModelRetryConfig{
    MaxRetries: 3,
    IsRetryAble: retryableCheck,
},
```

洋葱模型执行顺序：
```
请求 → reminder → summarization → reduction → safeTool → 实际执行
响应 ← reminder ← summarization ← reduction ← safeTool ← 实际执行
```

## 5. Alternatives Considered

### 5.1 自己实现 Summarization vs 用 Eino 内置
**选择 Eino 内置**。Eino 的 `summarization.New()` 已经处理了 token 计算、trigger 逻辑、摘要 prompt。自己实现需要重复大量工作。

### 5.2 Reminder 用 system message vs user message 注入
**选择 system message**。Claude Code 的 System Reminders 都是以 system role 注入。这样不会干扰对话历史的 user/assistant 交替格式。注意某些 provider 对 system message 有位置限制，可能需要 fallback 到 user message。

### 5.3 Summary 用独立 model 调用 vs agent 自身
**选择 agent 自身**。不引入额外的 model 调用成本。在 `runner.Run()` 结束前追加一个 summary 请求 message 让 agent 生成。

### 5.4 环境感知在 prompt template 里静态注入 vs middleware 动态注入
**选择结合**。Git info、project type、directory overview 是静态的（对话开始时确定），放在 prompt template。Token usage、todo status 是动态的，通过 reminder middleware 注入。

## 6. Implementation Plan (Priority Order)

### Phase 1: 安全和稳定性基础 (P0)

| # | 任务 | 涉及文件 | 改动量 |
|---|------|---------|--------|
| 1.1 | SafeToolMiddleware — 用 Eino-native 方式替换 agent.go 手动 error handling | `internal/agent/middleware.go` (新), `internal/agent/agent.go` | S |
| 1.2 | ModelRetryConfig — 添加 429/rate-limit 重试 | `internal/agent/agent.go`, `internal/config/config.go` | S |
| 1.3 | Tool Output Reduction — 集成 Eino reduction middleware | `internal/agent/agent.go` | S |

### Phase 2: Context 管理 (P1)

| # | 任务 | 涉及文件 | 改动量 |
|---|------|---------|--------|
| 2.1 | Summarization Middleware — 集成 Eino summarization | `internal/agent/agent.go` | M |
| 2.2 | System Reminders 框架 — 实现 Reminder 机制 + BeforeModelRewriteState | `internal/prompts/reminders.go` (新), `internal/agent/middleware.go` | M |
| 2.3 | Token/Todo/Error Reminders — 实现具体 reminder 规则 | `internal/prompts/reminders.go` | S |

### Phase 3: 环境感知 (P1)

| # | 任务 | 涉及文件 | 改动量 |
|---|------|---------|--------|
| 3.1 | Git Info 收集 | `internal/util/git.go` (新) | S |
| 3.2 | Project Type 检测 | `internal/util/project.go` (新) | S |
| 3.3 | Directory Overview | `internal/util/project.go` | S |
| 3.4 | System Prompt 模板增强 | `internal/prompts/system.md`, `internal/prompts/prompts.go` | S |

### Phase 4: Summary & Observability (P2)

| # | 任务 | 涉及文件 | 改动量 |
|---|------|---------|--------|
| 4.1 | Task Completion Summary | `internal/runner/summary.go` (新), `internal/runner/runner.go` | M |
| 4.2 | Session Summary 录制 | `internal/session/session.go` | S |
| 4.3 | Eino Callback Handler | `internal/telemetry/callbacks.go` (新), `cmd/coding/main.go` | S |

## 7. Migration & Compatibility

- **Middleware 迁移**: `agent.go` 的 `AgentMiddleware.WrapToolCall` 要迁移到 `ChatModelAgentMiddleware` 接口。approval middleware 保留在最外层但需要适配新接口。
- **Config 扩展**: `config.json` 可新增 `MaxRetries`、`SummarizationThreshold` 等字段，给予合理默认值，不需要用户手动配置。
- **Session 向后兼容**: `session_summary` 是新 entry type，旧版 resume 代码会自动跳过未知 type。
- **Go module 依赖**: 需要确认 `github.com/cloudwego/eino/adk/middlewares/summarization` 和 `reduction` 的版本兼容性。

## 8. Open Questions

1. **Eino ChatModelAgentMiddleware vs AgentMiddleware**: 当前代码用的是 `adk.AgentMiddleware`（Eino 旧接口？）。需要确认 Eino 最新版是否已统一为 `ChatModelAgentMiddleware` 接口，以及 approval middleware 如何迁移。
2. **Summarization Model**: 用同一个 chatModel 做 summarization 是否会产生额外费用问题？是否需要配置一个更廉价的 model 专门做摘要？
3. **Reduction Backend**: Tool 输出卸载到 `~/.jcoding/reduction/` 还是 `/tmp`？前者可跨 session 复用，后者自动清理。
4. **System Reminder 注入位置**: 某些 API provider 不支持在消息序列中间插入 system message，需要 fallback 策略。
5. **Directory Overview 深度**: 太浅不够信息量，太深浪费 token。需要用户测试确定最佳 `maxDepth`（建议默认 2 层）。
