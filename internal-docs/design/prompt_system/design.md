# 提示系统增强 — 技术设计文档

## 1. 架构概述

### 1.1 现状

```
GetSystemPrompt()
  ├── text/template 渲染 system.md
  ├── 串行收集环境信息 (EnvInfo)
  ├── loadAgentsMd() 单文件加载
  └── 字符串拼接返回

ReminderMiddleware
  ├── 每次模型调用前执行
  ├── 收集条件提醒 (token_warning/todo/plan/errors)
  └── 注入 system message
```

### 1.2 目标架构

```
PromptBuilder (新)
  ├── AsyncEnvLoader        — 并行加载环境信息
  ├── MemoryLoader          — 三级 AGENTS.md 合并 + @include
  ├── PromptBlockCache      — 静态/动态分块缓存
  └── ContextCompactor      — 自动/手动上下文压缩

ReminderMiddleware (增强)
  └── 新增 compaction_trigger 提醒条件
```

### 1.3 模块依赖

```
internal/prompts/
  ├── builder.go        — PromptBuilder 主入口
  ├── memory.go         — MemoryLoader + @include 解析
  ├── cache.go          — PromptBlockCache
  ├── async_env.go      — AsyncEnvLoader
  ├── compact.go        — ContextCompactor
  ├── compact_prompt.md — 压缩指令模板 (embed)
  ├── prompts.go        — 保留，向后兼容
  ├── reminders.go      — 保留，新增条件
  └── system.md / plan.md — 保留
  
internal/agent/
  └── reminder.go       — 增强：压缩触发集成
```

---

## 2. 核心组件

### 2.1 PromptBuilder — 提示构建器

统一入口，替代当前散落在 `GetSystemPrompt()` 中的各项逻辑。

```go
// internal/prompts/builder.go

// PromptBlock 表示系统提示的一个分块。
type PromptBlock struct {
    Content    string
    CacheScope string // "global" | "session" | "" (不缓存)
}

// PromptResult 是 PromptBuilder 的构建产物。
type PromptResult struct {
    Blocks []PromptBlock // 有序分块，拼接后为完整系统提示
    Full   string        // 拼接后的完整文本（兼容现有调用方）
}

// BuilderConfig 可配置参数。
type BuilderConfig struct {
    Platform          string
    Pwd               string
    EnvLabel          string
    SkillDescriptions string
    PlanMode          bool
    CacheEnabled      bool // 是否启用分块缓存
}

// PromptBuilder 构建系统提示。
type PromptBuilder struct {
    cfg         BuilderConfig
    envLoader   *AsyncEnvLoader
    memLoader   *MemoryLoader
    cache       *PromptBlockCache
}

// NewPromptBuilder 创建构建器实例。
func NewPromptBuilder(cfg BuilderConfig) *PromptBuilder

// Build 构建系统提示，内部并行加载环境信息与内存文件。
func (b *PromptBuilder) Build(ctx context.Context) (*PromptResult, error)
```

### 2.2 MemoryLoader — 多层次内存加载

```go
// internal/prompts/memory.go

// MemoryConfig 内存加载配置。
type MemoryConfig struct {
    MaxTotalChars int    // 总字符上限，默认 40000
    MaxIncDepth   int    // @include 最大嵌套深度，默认 5
}

// MemoryLoader 加载并合并多层次 AGENTS.md。
type MemoryLoader struct {
    cfg MemoryConfig
}

func NewMemoryLoader(cfg MemoryConfig) *MemoryLoader

// Load 按优先级加载全局/项目/本地 AGENTS.md 并合并。
// 加载顺序:
//   1. ~/.jcoding/AGENTS.md       (全局)
//   2. {pwd}/AGENTS.md            (项目)
//   3. {pwd}/AGENTS.local.md      (本地，gitignore)
// 返回合并后的内容，超出 MaxTotalChars 时截断。
func (m *MemoryLoader) Load(pwd string) (string, error)

// resolveIncludes 递归解析 @include 指令。
// visited 用于循环引用检测。
func (m *MemoryLoader) resolveIncludes(content string, basePath string, visited map[string]bool, depth int) string
```

**@include 解析规则**：

```
输入:
  @include rules/coding.md
  @include /absolute/path/file.md

解析:
  1. 正则匹配 ^@include\s+(.+)$ (逐行)
  2. 相对路径基于当前文件所在目录解析
  3. 绝对路径直接使用
  4. visited 集合检测循环，已访问则跳过并 log.Warn
  5. depth > MaxIncDepth 时停止递归并 log.Warn
  6. 文件不存在时跳过并 log.Warn
```

### 2.3 PromptBlockCache — 分块缓存

```go
// internal/prompts/cache.go

// PromptBlockCache 管理静态提示块的缓存。
// 静态块(规则、工具描述)内容不随会话变化，可跨会话复用。
// 动态块(环境信息、Git状态)每次构建时重新生成。
type PromptBlockCache struct {
    mu          sync.RWMutex
    staticHash  string        // 静态内容的哈希
    staticBlock *PromptBlock  // 缓存的静态块
}

func NewPromptBlockCache() *PromptBlockCache

// GetOrBuild 如果静态内容未变化则返回缓存，否则重建。
// contentHash 为当前静态内容的 SHA256 前 16 字符。
func (c *PromptBlockCache) GetOrBuild(contentHash string, buildFn func() *PromptBlock) *PromptBlock

// Invalidate 清除缓存（配置变更时调用）。
func (c *PromptBlockCache) Invalidate()
```

### 2.4 AsyncEnvLoader — 异步环境加载

```go
// internal/prompts/async_env.go

// AsyncEnvLoader 并行加载环境信息。
type AsyncEnvLoader struct {
    timeout time.Duration // 单项超时，默认 3s
}

func NewAsyncEnvLoader(timeout time.Duration) *AsyncEnvLoader

// Load 并行获取所有环境信息，超时项返回零值。
func (a *AsyncEnvLoader) Load(ctx context.Context, pwd string) *utils.EnvInfo
```

**内部实现**：

```go
func (a *AsyncEnvLoader) Load(ctx context.Context, pwd string) *utils.EnvInfo {
    ctx, cancel := context.WithTimeout(ctx, a.timeout)
    defer cancel()

    var wg sync.WaitGroup
    info := &utils.EnvInfo{}

    // 并行获取各项
    wg.Add(4)
    go func() { defer wg.Done(); info.GitBranch, info.GitDirty = getGitStatus(ctx, pwd) }()
    go func() { defer wg.Done(); info.LastCommit = getLastCommit(ctx, pwd) }()
    go func() { defer wg.Done(); info.ProjectType = detectProjectType(pwd) }()
    go func() { defer wg.Done(); info.DirTree = buildDirTree(pwd) }()
    wg.Wait()

    return info
}
```

### 2.5 ContextCompactor — 上下文压缩

```go
// internal/prompts/compact.go

// CompactConfig 压缩配置。
type CompactConfig struct {
    Threshold        float64 // 触发阈值，默认 0.85
    MaxRetries       int     // 断路器阈值，默认 3
    BufferTokens     int     // 压缩后保留的缓冲 token 数，默认 13000
}

// CompactResult 压缩结果。
type CompactResult struct {
    Summary          string   // 压缩生成的摘要
    OriginalTokens   int64    // 压缩前 token 数
    CompactedTokens  int64    // 压缩后 token 数
    PreservedMsgCount int     // 保留的原始消息数
}

// ContextCompactor 管理上下文压缩。
type ContextCompactor struct {
    cfg              CompactConfig
    consecutiveFails int        // 断路器计数
    tripped          bool       // 断路器是否已触发
}

func NewContextCompactor(cfg CompactConfig) *ContextCompactor

// ShouldCompact 判断当前是否需要压缩。
func (c *ContextCompactor) ShouldCompact(tokensUsed int64, contextLimit int) bool

// Compact 执行上下文压缩。
// messages: 当前对话历史
// model: 用于生成摘要的 ChatModel
// 返回压缩后的消息列表与结果摘要。
func (c *ContextCompactor) Compact(
    ctx context.Context,
    messages []*schema.Message,
    model ChatModel,
    contextLimit int,
) ([]*schema.Message, *CompactResult, error)

// IsTripped 断路器是否已触发。
func (c *ContextCompactor) IsTripped() bool

// Reset 重置断路器（新会话时调用）。
func (c *ContextCompactor) Reset()
```

**压缩策略**：

```
输入: 完整对话消息列表
输出: [系统消息(摘要), ...最近 N 条消息]

步骤:
1. 分离: system messages / 历史对话 / 最近 K 条消息
2. 构建压缩 prompt (embed compact_prompt.md):
   - 要求结构化分析:
     a) 用户原始请求与意图
     b) 关键技术概念与决策
     c) 涉及的文件与代码段
     d) 遇到的错误与修复
     e) 已完成步骤
     f) 待执行任务
     g) 当前工作上下文
3. 调用 model 生成摘要
4. 组装: [原始 system message, 摘要 system message, 最近 K 条消息]
5. 成功 → consecutiveFails = 0; 失败 → consecutiveFails++
6. consecutiveFails >= MaxRetries → tripped = true
```

---

## 3. 数据流

### 3.1 系统提示构建流程

```
用户发起对话
       │
       ▼
  PromptBuilder.Build(ctx)
       │
       ├──[并行]── AsyncEnvLoader.Load()
       │               ├── getGitStatus()
       │               ├── getLastCommit()
       │               ├── detectProjectType()
       │               └── buildDirTree()
       │
       ├──[并行]── MemoryLoader.Load(pwd)
       │               ├── 读取 ~/.jcoding/AGENTS.md
       │               ├── 读取 {pwd}/AGENTS.md
       │               ├── 读取 {pwd}/AGENTS.local.md
       │               └── resolveIncludes() 递归
       │
       ▼
   模板渲染 (text/template)
       │
       ├── 静态块: 规则 + 工具描述 + 技能
       │     └── PromptBlockCache 缓存
       │
       ├── 动态块: 环境信息 + Git + 目录树
       │
       └── 内存块: 合并后的 AGENTS.md 内容
              │
              ▼
        PromptResult { Blocks, Full }
```

### 3.2 上下文压缩流程

```
ReminderMiddleware.BeforeModelRewriteState()
       │
       ├── 检测 tokensUsed / contextLimit
       │
       ├── token > 85% 且 compactor 未 tripped?
       │     │
       │     ├── 否 → 继续正常流程
       │     │
       │     └── 是 → ContextCompactor.Compact()
       │               │
       │               ├── 分离消息: system / history / recent
       │               ├── 构建压缩 prompt
       │               ├── 调用 LLM 生成摘要
       │               │     │
       │               │     ├── 成功 → 替换 history 为摘要
       │               │     │         consecutiveFails = 0
       │               │     │
       │               │     └── 失败 → consecutiveFails++
       │               │               if >= 3 → tripped = true
       │               │
       │               └── 返回压缩后的 messages
       │
       ▼
   更新 state.Messages → 继续模型调用
```

### 3.3 @include 解析流程

```
MemoryLoader.resolveIncludes(content, basePath, visited, depth)
       │
       ├── depth > MaxIncDepth? → 返回 content (log.Warn)
       │
       ├── 逐行扫描正则 ^@include\s+(.+)$
       │     │
       │     ├── 解析路径 (相对 basePath 或绝对)
       │     │
       │     ├── visited[absPath] ? → 跳过 (log.Warn 循环引用)
       │     │
       │     ├── os.ReadFile(absPath) 失败? → 跳过 (log.Warn)
       │     │
       │     └── 成功 → visited[absPath] = true
       │               递归 resolveIncludes(子内容, 子basePath, visited, depth+1)
       │               替换 @include 行为子内容
       │
       └── 返回处理后的 content
```

---

## 4. 配置扩展

在 `~/.jcoding/config.json` 中新增字段：

```json
{
  "prompt": {
    "compaction": {
      "enabled": true,
      "threshold": 0.85,
      "maxRetries": 3,
      "bufferTokens": 13000
    },
    "memory": {
      "maxTotalChars": 40000,
      "maxIncludeDepth": 5
    },
    "cache": {
      "enabled": true
    },
    "asyncEnvTimeout": "3s"
  }
}
```

对应 Go 结构体扩展：

```go
// internal/config/config.go 新增

type PromptConfig struct {
    Compaction   CompactionConfig `json:"compaction"`
    Memory       MemoryConfig     `json:"memory"`
    Cache        CacheConfig      `json:"cache"`
    AsyncEnvTimeout string        `json:"asyncEnvTimeout"`
}

type CompactionConfig struct {
    Enabled      bool    `json:"enabled"`
    Threshold    float64 `json:"threshold"`
    MaxRetries   int     `json:"maxRetries"`
    BufferTokens int     `json:"bufferTokens"`
}

type MemoryConfig struct {
    MaxTotalChars   int `json:"maxTotalChars"`
    MaxIncludeDepth int `json:"maxIncludeDepth"`
}

type CacheConfig struct {
    Enabled bool `json:"enabled"`
}
```

---

## 5. 与现有系统的集成

### 5.1 向后兼容

- `GetSystemPrompt()` 和 `GetPlanSystemPrompt()` 保留，内部改为调用 `PromptBuilder`
- 不使用新配置时行为与当前完全一致
- `ReminderContext` 新增 `Compactor *ContextCompactor` 字段，nil 时跳过压缩逻辑

### 5.2 ReminderMiddleware 增强

```go
// internal/agent/reminder.go 修改

type ReminderConfig struct {
    TodoStore    *tools.TodoStore
    PlanStore    *tools.PlanStore
    EnvLabel     string
    IsRemote     bool
    ContextLimit int
    Compactor    *prompts.ContextCompactor // 新增
}
```

在 `BeforeModelRewriteState` 中插入压缩检查：

```go
// 压缩检查 (在提醒注入之前)
if m.cfg.Compactor != nil && m.cfg.Compactor.ShouldCompact(promptTokens, m.cfg.ContextLimit) {
    newMsgs, result, err := m.cfg.Compactor.Compact(ctx, state.Messages, model, m.cfg.ContextLimit)
    if err == nil {
        state.Messages = newMsgs
        config.Logger().Info("context compacted",
            "original_tokens", result.OriginalTokens,
            "compacted_tokens", result.CompactedTokens)
    } else {
        config.Logger().Error("compaction failed", "error", err)
    }
}
```

### 5.3 Runner 集成

```go
// internal/runner/runner.go 修改

// 创建 ContextCompactor 并传入 ReminderConfig
compactor := prompts.NewContextCompactor(prompts.CompactConfig{
    Threshold:    cfg.Prompt.Compaction.Threshold,
    MaxRetries:   cfg.Prompt.Compaction.MaxRetries,
    BufferTokens: cfg.Prompt.Compaction.BufferTokens,
})

reminderCfg := agent.ReminderConfig{
    // ...现有字段...
    Compactor: compactor,
}
```

### 5.4 Session 记录

在 JSONL session 中新增事件类型：

```json
{"type":"compaction","timestamp":"...","original_tokens":95000,"compacted_tokens":45000,"preserved_messages":5}
```

---

## 6. 实现计划

### 第一阶段：P0 核心功能

| 步骤 | 任务 | 涉及文件 | 预计工作量 |
|------|------|---------|-----------|
| 1.1 | MemoryLoader: 三级加载 + 合并 | `internal/prompts/memory.go` | 中 |
| 1.2 | @include 指令解析 | `internal/prompts/memory.go` | 中 |
| 1.3 | MemoryLoader 单元测试 | `internal/prompts/memory_test.go` | 小 |
| 1.4 | ContextCompactor 核心逻辑 | `internal/prompts/compact.go` | 大 |
| 1.5 | 压缩 prompt 模板 | `internal/prompts/compact_prompt.md` | 小 |
| 1.6 | ReminderMiddleware 集成压缩 | `internal/agent/reminder.go` | 中 |
| 1.7 | config.json 扩展 | `internal/config/config.go` | 小 |
| 1.8 | PromptBuilder 重构入口 | `internal/prompts/builder.go` | 中 |
| 1.9 | 集成测试 + 端到端验证 | 多文件 | 中 |

### 第二阶段：P1 性能优化

| 步骤 | 任务 | 涉及文件 | 预计工作量 |
|------|------|---------|-----------|
| 2.1 | AsyncEnvLoader | `internal/prompts/async_env.go` | 中 |
| 2.2 | PromptBlockCache | `internal/prompts/cache.go` | 中 |
| 2.3 | 现有 GetSystemPrompt 迁移到 Builder | `internal/prompts/prompts.go` | 小 |
| 2.4 | 性能基准测试 | `internal/prompts/benchmark_test.go` | 小 |

### 第三阶段：P2 体验增强

| 步骤 | 任务 | 涉及文件 | 预计工作量 |
|------|------|---------|-----------|
| 3.1 | TUI 压缩状态反馈 | `internal/tui/messages.go`, `statusbar_component.go` | 小 |
| 3.2 | 手动 `/compact` 命令 | `internal/runner/runner.go` | 小 |
| 3.3 | Session 压缩事件记录 | `internal/session/session.go` | 小 |

---

## 7. 测试策略

| 测试类型 | 覆盖范围 |
|---------|---------|
| **单元测试** | MemoryLoader 三级加载、@include 递归与循环检测、ContextCompactor 断路器、PromptBlockCache 缓存命中/失效 |
| **集成测试** | PromptBuilder 完整构建流程、ReminderMiddleware + Compactor 联动 |
| **端到端测试** | 模拟长对话触发自动压缩、验证压缩后对话可继续 |
| **基准测试** | AsyncEnvLoader 并行 vs 串行对比、PromptBlockCache 命中率 |

---

## 8. 风险与缓解

| 风险 | 缓解措施 |
|------|---------|
| 压缩摘要遗漏关键信息 | 结构化模板强制保留用户请求+文件路径+待办事项；保留最近 K 条原始消息 |
| 压缩调用增加延迟 | 异步执行，TUI 显示进度；断路器防止反复失败 |
| @include 引用链过深 | MaxIncDepth=5 硬限制；每层独立超时 |
| 缓存一致性 | 基于内容哈希校验；配置变更时主动 Invalidate |
| 向后兼容 | GetSystemPrompt/GetPlanSystemPrompt 保留签名，内部委托 PromptBuilder |
