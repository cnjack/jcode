# Agent Loop 增强 — 技术设计文档

## 架构概述

本设计基于 `docs/analyse/agent_loop.md` 的分析结论，在保持与现有 Eino 框架和 BubbleTea TUI 兼容的前提下，增量引入五大核心增强：

```
┌─────────────────────────────────────────────────┐
│                   TUI Layer                      │
│  (BubbleTea: AgentTextMsg, ToolCallMsg, etc.)   │
└──────────────────────┬──────────────────────────┘
                       │ tea.Msg
┌──────────────────────▼──────────────────────────┐
│              Runner Layer (enhanced)             │
│  ┌──────────┐ ┌──────────┐ ┌──────────────────┐│
│  │ EventBus │ │ Budget   │ │ Error Recovery   ││
│  │ (chan)   │ │ Manager  │ │ (3-layer)        ││
│  └──────────┘ └──────────┘ └──────────────────┘│
│  ┌──────────────────────────────────────────────┐│
│  │          Coordinator (optional)              ││
│  │  ┌─────────┐ ┌─────────┐ ┌─────────┐       ││
│  │  │Worker 1 │ │Worker 2 │ │Worker N │       ││
│  │  └─────────┘ └─────────┘ └─────────┘       ││
│  └──────────────────────────────────────────────┘│
└──────────────────────┬──────────────────────────┘
                       │
┌──────────────────────▼──────────────────────────┐
│             Agent Layer (Eino ADK)               │
│  ┌──────────────────────────────────────────────┐│
│  │ Middleware Stack:                            ││
│  │  langfuse → compaction → reduction →        ││
│  │  budget → approval+safeTool                  ││
│  └──────────────────────────────────────────────┘│
└──────────────────────┬──────────────────────────┘
                       │
┌──────────────────────▼──────────────────────────┐
│           Model Layer (OpenAI-compatible)        │
│  ┌────────────┐  ┌───────────────────┐          │
│  │ Primary    │  │ Fallback Model    │          │
│  │ Model      │  │ (optional)        │          │
│  └────────────┘  └───────────────────┘          │
└─────────────────────────────────────────────────┘
```

### 设计原则

1. **增量扩展**：所有增强通过 Eino 的 `ChatModelAgentMiddleware` 或独立组件实现，不修改 Eino 核心代码
2. **向后兼容**：新功能默认关闭或可选，不影响现有行为
3. **关注分离**：上下文压缩、错误恢复、预算控制作为独立中间件，可独立测试和配置
4. **Go 惯用**：使用 channel、context、errgroup 等 Go 原生并发原语

---

## 核心组件设计

### 1. 上下文压缩引擎 (Context Compactor)

#### 职责

监控对话 token 使用量，在接近模型上下文限制时自动触发历史消息压缩。

#### 接口定义

```go
// internal/agent/compaction.go

// CompactionStrategy 定义压缩策略
type CompactionStrategy interface {
    // ShouldCompact 判断是否需要压缩
    // currentTokens: 当前对话总 token 数
    // limit: 模型上下文窗口大小
    ShouldCompact(currentTokens, limit int) bool

    // Compact 执行压缩，返回压缩后的消息列表
    // messages: 原始消息列表
    // keepRecent: 保留最近 N 条消息不压缩
    Compact(ctx context.Context, messages []schema.Message, keepRecent int) ([]schema.Message, error)
}

// ThresholdCompactionStrategy 基于阈值的压缩策略
type ThresholdCompactionStrategy struct {
    threshold    float64            // 触发压缩的 token 使用率阈值 (0.8 = 80%)
    summarizer   model.ChatModel    // 用于生成摘要的模型 (可以是更轻量的模型)
    keepRecent   int                // 始终保留的最近消息数
}

// compactionMiddleware 实现 adk.ChatModelAgentMiddleware
type compactionMiddleware struct {
    *adk.BaseChatModelAgentMiddleware
    strategy     CompactionStrategy
    tokenCounter TokenCounter
    contextLimit int
}

// TokenCounter 统计消息 token 数
type TokenCounter interface {
    Count(messages []schema.Message) (int, error)
}
```

#### 压缩流程

```
消息列表 [M1, M2, M3, ..., Mn-3, Mn-2, Mn-1, Mn]
                                     ↓
检测: currentTokens / contextLimit > threshold (0.8)
                                     ↓
压缩: [M1..Mn-k] → summarize → [Summary]
保留: [Mn-k+1, ..., Mn] 原样保留
                                     ↓
结果: [CompactBoundary{summary}, Mn-k+1, ..., Mn]
```

#### 关键规则

- **不压缩的消息类型**: 用户审批确认、工具拒绝消息、最近 `keepRecent` 条消息
- **压缩边界标记**: 插入 `CompactBoundary` 消息，用于会话恢复时识别压缩点
- **摘要内容**: 包含关键决策、文件路径、已完成操作等结构化信息

### 2. 分层错误恢复 (Layered Error Recovery)

#### 职责

在模型调用和工具执行过程中提供三级自动恢复机制。

#### 接口定义

```go
// internal/agent/recovery.go

// RecoveryLayer 定义单层恢复策略
type RecoveryLayer interface {
    // CanHandle 判断是否能处理该错误
    CanHandle(err error) bool
    // Recover 尝试恢复，返回修正后的输入或 nil
    Recover(ctx context.Context, err error, state *RecoveryState) (*RecoveryAction, error)
}

// RecoveryState 恢复上下文
type RecoveryState struct {
    Messages       []schema.Message
    AttemptCount   int               // 当前层已尝试次数
    OriginalError  error
    TokenUsage     int
    ContextLimit   int
}

// RecoveryAction 恢复动作
type RecoveryAction struct {
    Type       RecoveryActionType
    Messages   []schema.Message    // 修正后的消息 (用于重试)
    ModelName  string              // 备用模型名称 (用于降级)
}

type RecoveryActionType int

const (
    ActionRetryWithContinuation RecoveryActionType = iota // 续写 (MaxOutput 截断)
    ActionRetryWithCompaction                              // 压缩后重试 (上下文过长)
    ActionFallbackModel                                    // 降级到备用模型
)

// recoveryMiddleware 实现 adk.ChatModelAgentMiddleware
type recoveryMiddleware struct {
    *adk.BaseChatModelAgentMiddleware
    layers       []RecoveryLayer
    maxRetries   map[RecoveryActionType]int // 每层最大重试次数
}
```

#### 恢复层级

```
错误发生
  │
  ▼
Layer 1: MaxOutput 续写 (最多 3 次)
  │ 检测: stop_reason == "max_tokens"
  │ 动作: 将已有输出追加到消息中，请求模型继续
  │ 失败 ↓
  │
Layer 2: 上下文压缩重试 (最多 2 次)
  │ 检测: error contains "context_length_exceeded" 或 token 估算超限
  │ 动作: 调用 CompactionStrategy.Compact()，用压缩后的消息重试
  │ 失败 ↓
  │
Layer 3: 备用模型降级 (最多 1 次)
  │ 检测: 前两层都失败，且配置了 fallback model
  │ 动作: 切换到 fallback model 重试
  │ 失败 ↓
  │
返回错误给用户
```

### 3. 预算控制器 (Budget Manager)

#### 职责

实时追踪 token 消耗和估算成本，提供预算阈值控制。

#### 接口定义

```go
// internal/agent/budget.go

// BudgetManager 管理 token 和成本预算
type BudgetManager struct {
    mu              sync.RWMutex
    promptTokens    int64
    completionTokens int64
    totalCost       float64

    // 预算配置
    maxTokensPerTurn   int64    // 单轮最大 token (0 = 无限制)
    maxCostPerSession  float64  // 会话最大成本 (0.0 = 无限制)
    warningThreshold   float64  // 警告阈值 (0.8 = 80%)

    // 成本计算
    pricing  ModelPricing
}

// ModelPricing 模型价格配置
type ModelPricing struct {
    PromptCostPer1K     float64
    CompletionCostPer1K float64
}

// BudgetStatus 预算状态
type BudgetStatus struct {
    PromptTokens      int64
    CompletionTokens  int64
    TotalTokens       int64
    EstimatedCost     float64
    RemainingBudget   float64   // -1 表示无限制
    WarningLevel      WarningLevel
}

type WarningLevel int

const (
    WarningNone     WarningLevel = iota
    WarningApproach                      // 接近阈值
    WarningExceeded                      // 已超限
)

// Track 记录一次 API 调用的 token 消耗
func (b *BudgetManager) Track(promptTokens, completionTokens int64) BudgetStatus

// Check 检查是否可以继续
func (b *BudgetManager) Check() (BudgetStatus, bool)

// budgetMiddleware 实现 adk.ChatModelAgentMiddleware
type budgetMiddleware struct {
    *adk.BaseChatModelAgentMiddleware
    manager *BudgetManager
    onWarn  func(status BudgetStatus) // 回调: 发送警告到 TUI
}
```

#### TUI 消息类型

```go
// internal/tui/messages.go (新增)

// BudgetWarningMsg 预算警告消息
type BudgetWarningMsg struct {
    Status BudgetStatus
}

// BudgetExceededMsg 预算超限消息
type BudgetExceededMsg struct {
    Status BudgetStatus
}
```

### 4. 事件总线 (Event Bus) — Channel 流式架构

#### 职责

使用 Go channel 替换同步 `iterator.Next()` 阻塞模式，解耦事件生产与消费。

#### 接口定义

```go
// internal/runner/eventbus.go

// Event 统一事件类型
type Event struct {
    Type      EventType
    Text      string           // 文本内容 (assistant/tool output)
    ToolCall  *ToolCallEvent   // 工具调用事件
    ToolResult *ToolResultEvent // 工具结果事件
    Error     error            // 错误事件
    Meta      map[string]any   // 扩展元数据
}

type EventType int

const (
    EventAssistantText EventType = iota
    EventAssistantDone
    EventToolCall
    EventToolResult
    EventError
    EventBudgetWarning
    EventCompaction
    EventWorkerStatus
)

type ToolCallEvent struct {
    Name string
    Args string
}

type ToolResultEvent struct {
    Name   string
    Output string
    Err    error
}

// EventBus 事件总线
type EventBus struct {
    ch     chan Event
    done   chan struct{}
    cancel context.CancelFunc
}

// NewEventBus 创建事件总线
func NewEventBus(bufferSize int) *EventBus

// Emit 发送事件 (非阻塞，满时丢弃最旧事件并记录警告)
func (eb *EventBus) Emit(event Event)

// Subscribe 返回只读 channel 供消费者使用
func (eb *EventBus) Subscribe() <-chan Event

// Close 关闭事件总线
func (eb *EventBus) Close()
```

#### 与现有 Runner 的集成

```go
// 改造后的 runInner (伪代码)
func runInner(ctx context.Context, ag *adk.ChatModelAgent, messages []adk.Message,
    bus *EventBus, rec *session.Recorder, todoStore *tools.TodoStore) string {

    ctx, cancel := context.WithCancel(ctx)
    defer cancel()

    iterator := ag.Run(ctx, &adk.AgentInput{Messages: messages, EnableStreaming: true})

    go func() {
        defer bus.Emit(Event{Type: EventAssistantDone})
        for {
            event, ok := iterator.Next()
            if !ok { return }
            // ... 转换 Eino event → Event 并 bus.Emit(...)
        }
    }()

    // 消费端在 TUI goroutine 中：
    // for event := range bus.Subscribe() { p.Send(toTeaMsg(event)) }
}
```

### 5. 协调模式 (Coordinator)

#### 职责

将复杂任务分解为多个 worker subagent 并行执行，汇总结果。

#### 接口定义

```go
// internal/runner/coordinator.go

// Coordinator 协调多个 worker 的执行
type Coordinator struct {
    factory     AgentFactory       // 创建 worker agent 的工厂
    maxWorkers  int                // 最大并行 worker 数
    bus         *EventBus          // 事件总线
    budget      *BudgetManager     // 共享预算
}

// AgentFactory 创建 worker agent
type AgentFactory interface {
    CreateWorker(ctx context.Context, task WorkerTask) (*adk.ChatModelAgent, error)
}

// WorkerTask 单个 worker 的任务定义
type WorkerTask struct {
    ID          string
    Instruction string
    Tools       []string   // 该 worker 可用的工具子集
    Messages    []schema.Message
}

// WorkerResult 单个 worker 的执行结果
type WorkerResult struct {
    TaskID  string
    Output  string
    Err     error
    Tokens  int64
}

// RunParallel 并行执行多个 worker 任务
func (c *Coordinator) RunParallel(ctx context.Context, tasks []WorkerTask) ([]WorkerResult, error)
```

#### 并行执行模型

```
Coordinator.RunParallel(tasks)
  │
  ├─ errgroup.Go → Worker 1 (ctx, task1)
  │    └─ agent.Run() → result1
  │
  ├─ errgroup.Go → Worker 2 (ctx, task2)
  │    └─ agent.Run() → result2
  │
  └─ errgroup.Go → Worker 3 (ctx, task3)
       └─ agent.Run() → result3
  │
  ▼
errgroup.Wait()
  │
  ▼
return []WorkerResult{result1, result2, result3}
```

---

## 数据流图

### 主循环数据流

```
User Input
  │
  ▼
Runner.Run()
  ├─ BudgetManager.Check() ──→ 超限? → BudgetExceededMsg → TUI
  │
  ▼
Agent.Run(ctx, input)
  │
  ├─ [Middleware: langfuse] → trace span
  │
  ├─ [Middleware: compaction]
  │    └─ TokenCounter.Count(messages)
  │         ├─ < threshold → pass through
  │         └─ ≥ threshold → CompactionStrategy.Compact()
  │              └─ summarize older messages
  │              └─ emit Event{Type: EventCompaction}
  │
  ├─ [Middleware: budget]
  │    └─ after model call → BudgetManager.Track(usage)
  │         ├─ WarningApproach → emit BudgetWarningMsg
  │         └─ WarningExceeded → cancel context
  │
  ├─ [Middleware: recovery]
  │    └─ on error → Layer1 → Layer2 → Layer3 → give up
  │
  ├─ [Middleware: approval+safeTool]
  │    └─ existing logic unchanged
  │
  ▼
Model API Call
  │
  ├─ Success → stream tokens → EventBus → TUI
  ├─ Tool Call → tool execution → EventBus → TUI
  └─ Error → recoveryMiddleware intercept
```

### Coordinator 数据流

```
User: "重构 pkg/parser 和 pkg/lexer"
  │
  ▼
Main Agent (Coordinator Mode)
  ├─ 分析任务 → 可并行化
  │
  ├─ Coordinator.RunParallel([
  │    {ID: "w1", Instruction: "重构 pkg/parser ..."},
  │    {ID: "w2", Instruction: "重构 pkg/lexer ..."},
  │  ])
  │
  ├─ Worker 1 ──→ EventBus: WorkerStatus{w1, running}
  │    └─ agent.Run() → 读文件/编辑/测试
  │    └─ EventBus: WorkerStatus{w1, done}
  │
  ├─ Worker 2 ──→ EventBus: WorkerStatus{w2, running}
  │    └─ agent.Run() → 读文件/编辑/测试
  │    └─ EventBus: WorkerStatus{w2, done}
  │
  ▼
Main Agent 汇总 worker 结果 → 最终响应
```

---

## 状态管理

### 上下文压缩状态

```go
// CompactionState 跟踪压缩历史
type CompactionState struct {
    CompactionCount   int       // 已执行的压缩次数
    LastCompactedAt   time.Time // 上次压缩时间
    OriginalMsgCount  int       // 压缩前的消息数
    CurrentMsgCount   int       // 当前消息数
    SavedTokens       int       // 累计节省的 token 数
}
```

### 错误恢复状态

```go
// RecoveryTracker 跟踪恢复历史，防止无限循环
type RecoveryTracker struct {
    attempts map[RecoveryActionType]int // 每层已尝试次数
    maxRetries map[RecoveryActionType]int
}

func (rt *RecoveryTracker) CanRetry(action RecoveryActionType) bool {
    return rt.attempts[action] < rt.maxRetries[action]
}

func (rt *RecoveryTracker) Record(action RecoveryActionType) {
    rt.attempts[action]++
}
```

### 预算状态生命周期

```
Session Start → BudgetManager{tokens: 0, cost: 0.0}
  │
  ├─ Each API call → Track(prompt, completion)
  │    ├─ tokens < warning threshold → WarningNone
  │    ├─ tokens ≥ warning threshold → WarningApproach → TUI 提示
  │    └─ tokens ≥ max budget → WarningExceeded → 停止 agent
  │
  └─ Session End → 生成报告 (P2)
```

### Coordinator 状态机

```
                    ┌──────────┐
                    │  Idle    │
                    └────┬─────┘
                         │ RunParallel(tasks)
                         ▼
                    ┌──────────┐
             ┌──────│ Planning │
             │      └────┬─────┘
             │           │ spawn workers
             │           ▼
             │      ┌──────────┐
             │      │ Running  │──→ WorkerStatus events → TUI
             │      └────┬─────┘
             │           │ all workers done
             │           ▼
             │      ┌──────────┐
             └──────│ Done     │──→ 汇总结果
                    └──────────┘
```

---

## 错误处理策略

### 错误分类

| 错误类型 | 来源 | 处理策略 |
|---------|------|---------|
| `max_tokens` stop reason | 模型输出截断 | Layer 1: 续写 |
| `context_length_exceeded` | 上下文过长 | Layer 2: 压缩 + 重试 |
| API 速率限制 (429) | 模型 API | Eino 内置指数退避重试 (已有) |
| API 服务错误 (500/503) | 模型 API | Eino 内置重试 + Layer 3 降级 |
| 工具执行失败 | 工具层 | 现有 safeTool 逻辑 (错误转字符串) |
| 用户取消 | TUI | context.Cancel → 优雅停止 |
| 预算超限 | BudgetManager | 优雅停止，保留已有输出 |
| Worker 失败 | Coordinator | 单 worker 失败不影响其他 worker，coordinator 汇总部分结果 |

### 关键不变量

1. **错误不 panic**: 所有工具错误和恢复失败都转换为 agent 可见的字符串
2. **日志完备**: 所有错误恢复操作记录到 `config.Logger()`
3. **会话完整**: 错误和恢复操作完整记录到 JSONL session 文件
4. **用户知情**: 自动恢复操作通过 TUI 消息通知用户

---

## 与现有代码的集成点

### 1. `internal/agent/agent.go` — 中间件注册

```go
// 改造 NewAgent，增加新的中间件
func NewAgent(
    ctx context.Context,
    chatmodel model.ToolCallingChatModel,
    tools []tool.BaseTool,
    instruction string,
    approvalFunc ApprovalFunc,
    middlewares []adk.AgentMiddleware,
    handlers []adk.ChatModelAgentMiddleware,
    opts ...AgentOption, // 新增: 可选配置
) (*adk.ChatModelAgent, error) {
    options := defaultOptions()
    for _, opt := range opts {
        opt(options)
    }

    // 按顺序插入新中间件 (外 → 内):
    // langfuse → compaction → budget → reduction → approval+safeTool
    if options.compaction != nil {
        handlers = append([]adk.ChatModelAgentMiddleware{options.compaction}, handlers...)
    }
    if options.budget != nil {
        handlers = append([]adk.ChatModelAgentMiddleware{options.budget}, handlers...)
    }
    if options.recovery != nil {
        handlers = append([]adk.ChatModelAgentMiddleware{options.recovery}, handlers...)
    }

    // 现有逻辑不变
    handlers = append(handlers, newApprovalMiddleware(approvalFunc))
    // ...
}
```

### 2. `internal/runner/runner.go` — 主循环改造

```go
// Run 增加 BudgetManager 和 EventBus 参数
func Run(
    ctx context.Context,
    ag *adk.ChatModelAgent,
    messages []adk.Message,
    p *tea.Program,
    rec *session.Recorder,
    todoStore *tools.TodoStore,
    tracer *telemetry.LangfuseTracer,
    budget *BudgetManager,         // 新增
) string {
    // 预算检查
    if budget != nil {
        if status, ok := budget.Check(); !ok {
            p.Send(tui.BudgetExceededMsg{Status: status})
            return ""
        }
    }

    // 其余逻辑与现有一致，通过中间件自动生效
    // ...
}
```

### 3. `internal/config/config.go` — 配置扩展

```go
// Config 新增字段
type Config struct {
    // ... 现有字段 ...

    // Budget 预算配置
    Budget *BudgetConfig `json:"budget,omitempty"`

    // FallbackModel 备用模型
    FallbackModel string `json:"fallback_model,omitempty"`

    // Compaction 压缩配置
    Compaction *CompactionConfig `json:"compaction,omitempty"`
}

type BudgetConfig struct {
    MaxTokensPerTurn  int64   `json:"max_tokens_per_turn,omitempty"`   // 0 = 无限制
    MaxCostPerSession float64 `json:"max_cost_per_session,omitempty"`  // 0.0 = 无限制
    WarningThreshold  float64 `json:"warning_threshold,omitempty"`     // 默认 0.8
}

type CompactionConfig struct {
    Enabled       bool    `json:"enabled"`           // 默认 true
    Threshold     float64 `json:"threshold"`         // 默认 0.8
    KeepRecent    int     `json:"keep_recent"`       // 默认 10
    SummaryModel  string  `json:"summary_model,omitempty"` // 空则使用主模型
}
```

### 4. `internal/tui/messages.go` — 新增消息类型

```go
// 新增 TUI 消息，通过 tea.Msg 接口兼容现有架构

type BudgetWarningMsg struct {
    PromptTokens     int64
    CompletionTokens int64
    EstimatedCost    float64
    RemainingBudget  float64
}

type BudgetExceededMsg struct {
    TotalTokens int64
    TotalCost   float64
}

type CompactionMsg struct {
    SavedTokens    int
    MessagesBefore int
    MessagesAfter  int
}

type WorkerStatusMsg struct {
    WorkerID string
    Status   string // "running" | "done" | "failed"
    Output   string // worker 输出摘要
}
```

### 5. `internal/session/session.go` — 新增 Entry 类型

```go
// 新增 session entry 类型
const (
    // ... 现有类型 ...
    EntryCompaction   EntryType = "compaction"       // 上下文压缩事件
    EntryRecovery     EntryType = "recovery"         // 错误恢复事件
    EntryBudgetEvent  EntryType = "budget_event"     // 预算事件
    EntryWorkerStart  EntryType = "worker_start"     // Worker 启动
    EntryWorkerResult EntryType = "worker_result"    // Worker 结果
)
```

---

## 实现计划

### Phase 1: 基础设施 (P0)

**目标**: 预算控制 + Token 追踪增强

**任务**:
- [ ] 实现 `BudgetManager` 及其 `budgetMiddleware`
- [ ] 扩展 `Config` 增加 `BudgetConfig`
- [ ] 新增 `BudgetWarningMsg` / `BudgetExceededMsg` TUI 消息
- [ ] TUI 状态栏增加 token/cost 实时展示
- [ ] 单元测试: 预算阈值、超限停止

**涉及文件**:
- `internal/agent/budget.go` (新建)
- `internal/config/config.go` (修改)
- `internal/tui/messages.go` (修改)
- `internal/tui/statusbar_component.go` (修改)
- `internal/runner/runner.go` (修改)

**预计工作量**: 3-5 天

### Phase 2: 上下文压缩 (P0)

**目标**: 实现自动上下文压缩，解决长对话溢出

**任务**:
- [ ] 实现 `TokenCounter` (基于 tiktoken-go 或模型 API 估算)
- [ ] 实现 `ThresholdCompactionStrategy`
- [ ] 实现 `compactionMiddleware`
- [ ] 扩展 `Config` 增加 `CompactionConfig`
- [ ] 新增 `CompactionMsg` TUI 消息
- [ ] Session 记录压缩事件
- [ ] 单元测试: 压缩触发条件、消息保留策略

**涉及文件**:
- `internal/agent/compaction.go` (新建)
- `internal/agent/agent.go` (修改)
- `internal/config/config.go` (修改)
- `internal/tui/messages.go` (修改)
- `internal/session/session.go` (修改)

**预计工作量**: 5-7 天

### Phase 3: 分层错误恢复 (P0)

**目标**: 实现三级错误自动恢复

**任务**:
- [ ] 实现 `RecoveryLayer` 接口和三个具体 Layer
- [ ] 实现 `recoveryMiddleware`
- [ ] 支持 `FallbackModel` 配置
- [ ] Session 记录恢复事件
- [ ] 集成测试: 模拟 max_tokens / context_length_exceeded / API 错误

**涉及文件**:
- `internal/agent/recovery.go` (新建)
- `internal/agent/agent.go` (修改)
- `internal/config/config.go` (修改)
- `internal/model/chatmodel.go` (修改 — 增加 fallback 支持)

**预计工作量**: 5-7 天

### Phase 4: Channel 流式架构 (P1)

**目标**: 使用 EventBus 替换同步迭代，支持取消

**任务**:
- [ ] 实现 `EventBus`
- [ ] 重构 `runInner` 使用 EventBus
- [ ] 实现 context cancellation 支持
- [ ] TUI 增加取消操作入口
- [ ] 回归测试: 确保流式输出行为不变

**涉及文件**:
- `internal/runner/eventbus.go` (新建)
- `internal/runner/runner.go` (修改)
- `internal/tui/tui.go` (修改)

**预计工作量**: 4-6 天

### Phase 5: Coordinator 模式 (P1)

**目标**: 支持多 worker 并行执行

**任务**:
- [ ] 实现 `Coordinator` 和 `AgentFactory`
- [ ] 实现 `WorkerStatusMsg` TUI 展示
- [ ] 重构 subagent tool 以支持 coordinator 模式
- [ ] Coordinator 共享 BudgetManager
- [ ] 集成测试: 并行执行、部分失败、结果汇总

**涉及文件**:
- `internal/runner/coordinator.go` (新建)
- `internal/tools/subagent.go` (修改)
- `internal/tui/messages.go` (修改)
- `internal/tui/tui.go` (修改)

**预计工作量**: 7-10 天

### Phase 6: 增强与打磨 (P2)

**目标**: 智能压缩、会话报告、压缩感知恢复

**任务**:
- [ ] 基于消息重要性评分的选择性压缩
- [ ] 会话结束生成 token/cost 报告
- [ ] `--resume` 支持压缩边界
- [ ] 高级任务类型 (dream/workflow)

**预计工作量**: 7-10 天

---

## 风险与缓解

| 风险 | 概率 | 影响 | 缓解措施 |
|------|------|------|---------|
| Eino 中间件 API 不支持所需的拦截点 | 中 | 高 | 预研 Eino WrapModelGenerate / WrapInvokableToolCall 的能力边界；必要时提 PR |
| 上下文压缩导致信息丢失 | 中 | 高 | 保守的 keepRecent 默认值；关键消息白名单；可配置关闭 |
| Token 计数不精确 | 低 | 中 | 使用 tiktoken-go 精确计数；预留 10% 安全裕度 |
| 并行 Worker 竞争文件系统 | 中 | 中 | Worker 工具集限制；文件级锁或沙箱隔离 |
| 备用模型能力不足 | 低 | 低 | 降级时提示用户，保留手动切换选项 |
