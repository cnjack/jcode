# 子代理系统 V2 — 技术设计文档

## 架构概述

### 设计原则

1. **渐进增强**：在现有同步子代理基础上增加异步能力，保持向后兼容
2. **复用现有模式**：参考 `BackgroundManager` 的 goroutine + channel 模式
3. **最小侵入**：不修改 `agent.NewAgent()` 核心接口，通过工具层扩展能力
4. **资源安全**：所有 goroutine 与父 context 绑定，支持级联取消

### 架构总览

```
┌─────────────────────────────────────────────────────────┐
│                     Main Agent                          │
│  (internal/agent/agent.go — NewAgent)                   │
│                                                         │
│  工具集:                                                │
│  ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌───────────┐  │
│  │ subagent │ │task_list │ │ task_get │ │ task_stop │  │
│  │ (v2)     │ │          │ │          │ │           │  │
│  └────┬─────┘ └────┬─────┘ └────┬─────┘ └─────┬─────┘  │
│       │             │            │              │        │
│  ┌────▼─────────────▼────────────▼──────────────▼────┐  │
│  │           SubagentTaskManager                     │  │
│  │  (goroutine 调度 + 任务生命周期 + 通知队列)        │  │
│  └──────────┬────────────────────────────────────────┘  │
│             │                                           │
│  ┌──────────▼────────────────────────────────────────┐  │
│  │           ModelFactory                            │  │
│  │  (provider/model → ChatModel 实例缓存)             │  │
│  └───────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────┘
         │                    │                  │
    ┌────▼───┐          ┌────▼───┐         ┌────▼───┐
    │Worker 1│          │Worker 2│         │Worker N│
    │(explore│          │(general│         │(coord) │
    │ async) │          │ sync)  │         │        │
    └────────┘          └────────┘         └───┬────┘
                                               │
                                    ┌──────────▼──────────┐
                                    │  Nested Workers     │
                                    │  (depth ≤ 3)        │
                                    └─────────────────────┘
```

### 与现有系统的关系

| 现有组件 | 变更方式 |
|---------|---------|
| `internal/tools/subagent.go` | 重构：拆分为 subagent 工具 + SubagentTaskManager |
| `internal/tools/env.go` | 扩展：Env 增加 `depth` 字段 |
| `internal/tools/background.go` | 不变：子代理任务管理独立于命令后台管理 |
| `internal/agent/agent.go` | 不变：NewAgent 接口不修改 |
| `internal/agent/reminder.go` | 扩展：注入子代理完成通知 |
| `internal/config/config.go` | 扩展：Config 增加 SubagentConfig |
| `internal/model/chatmodel.go` | 扩展：新增 ModelFactory |
| `internal/session/session.go` | 扩展：新增子代理事件类型 |

---

## 核心组件

### 1. SubagentTaskManager

管理所有子代理任务的生命周期，是整个 V2 系统的核心调度器。

```go
// internal/tools/subagent_manager.go

package tools

import (
    "context"
    "fmt"
    "sync"
    "time"
)

// SubagentTaskStatus 子代理任务状态
type SubagentTaskStatus string

const (
    TaskStatusPending   SubagentTaskStatus = "pending"
    TaskStatusRunning   SubagentTaskStatus = "running"
    TaskStatusCompleted SubagentTaskStatus = "completed"
    TaskStatusFailed    SubagentTaskStatus = "failed"
    TaskStatusStopped   SubagentTaskStatus = "stopped"
)

// SubagentTask 单个子代理任务的元数据和状态
type SubagentTask struct {
    ID        string
    Name      string
    AgentType string             // explore | general | coordinator
    Model     string             // provider/model 或空（使用默认）
    Status    SubagentTaskStatus
    Depth     int                // 嵌套深度
    ParentID  string             // 父任务 ID，顶层为空
    Output    string
    Error     string
    Started   time.Time
    Ended     time.Time
    Cancel    context.CancelFunc // 用于停止任务
}

// SubagentNotification 子代理完成通知，注入主代理上下文
type SubagentNotification struct {
    TaskID    string
    Name      string
    AgentType string
    Status    SubagentTaskStatus
    Summary   string             // 截断后的输出摘要
}

// SubagentTaskManager 子代理任务调度与生命周期管理
type SubagentTaskManager struct {
    mu            sync.RWMutex
    tasks         map[string]*SubagentTask
    notifications []SubagentNotification
    nextID        int
    maxParallel   int  // 最大并行数，默认 10
    maxCompleted  int  // 已完成任务缓存上限，默认 20

    // 回调
    notifier SubagentNotifier
}

// NewSubagentTaskManager 创建任务管理器
func NewSubagentTaskManager(maxParallel, maxCompleted int) *SubagentTaskManager

// Submit 提交一个子代理任务（同步或异步）
// background=true 时立即返回 task ID；background=false 时阻塞直到完成
func (m *SubagentTaskManager) Submit(ctx context.Context, task *SubagentTask, runFn func(ctx context.Context) (string, error), background bool) (taskID string, result string, err error)

// Get 获取指定任务的详情
func (m *SubagentTaskManager) Get(taskID string) (*SubagentTask, error)

// List 列出所有任务，可按状态过滤
func (m *SubagentTaskManager) List(statusFilter SubagentTaskStatus) []*SubagentTask

// Stop 停止正在运行的任务
func (m *SubagentTaskManager) Stop(taskID string) error

// DrainNotifications 消费并清空所有待处理通知（供 reminder middleware 调用）
func (m *SubagentTaskManager) DrainNotifications() []SubagentNotification

// RunningCount 返回当前运行中的任务数
func (m *SubagentTaskManager) RunningCount() int

// CompletedCount 返回已完成的任务数
func (m *SubagentTaskManager) CompletedCount() int
```

### 2. ModelFactory

根据 `provider/model` 标识符创建或复用 ChatModel 实例。

```go
// internal/model/factory.go

package model

import (
    "context"
    "fmt"
    "strings"
    "sync"

    einomodel "github.com/cloudwego/eino/components/model"
    "github.com/cnjack/jcode/internal/config"
)

// ModelFactory 根据 provider/model 创建 ChatModel 实例，内置缓存
type ModelFactory struct {
    mu       sync.RWMutex
    cfg      *config.Config
    cache    map[string]einomodel.ToolCallingChatModel
    fallback einomodel.ToolCallingChatModel // 主代理的默认模型
}

// NewModelFactory 创建模型工厂
func NewModelFactory(cfg *config.Config, fallback einomodel.ToolCallingChatModel) *ModelFactory

// GetModel 根据 "provider/model" 格式获取模型实例
// 空字符串返回 fallback 模型
func (f *ModelFactory) GetModel(ctx context.Context, providerModel string) (einomodel.ToolCallingChatModel, error)

// parseProviderModel 解析 "provider/model" 格式
// 示例: "openai/gpt-4o-mini" → provider="openai", model="gpt-4o-mini"
func parseProviderModel(s string) (provider, model string, err error)
```

### 3. 扩展 Env

在现有 `Env` 结构体中增加嵌套深度追踪。

```go
// internal/tools/env.go — 扩展部分

const MaxSubagentDepth = 3

// Env 扩展字段
type Env struct {
    Exec        Executor
    pwd         string
    platform    string
    TodoStore   *TodoStore
    Depth       int  // 子代理嵌套深度，顶层为 0
    OnEnvChange func(envLabel string, isLocal bool, err error)
}

// CloneForSubagent 创建子代理环境副本，深度 +1
func (e *Env) CloneForSubagent() *Env {
    return &Env{
        Exec:      e.Exec,
        pwd:       e.pwd,
        platform:  e.platform,
        TodoStore: NewTodoStore(),
        Depth:     e.Depth + 1,
    }
}

// CanNest 检查是否允许继续嵌套
func (e *Env) CanNest() bool {
    return e.Depth < MaxSubagentDepth
}
```

### 4. 重构 Subagent Tool

将现有 `subagentTool` 重构为支持异步执行和多模型的版本。

```go
// internal/tools/subagent.go — V2 重构

const (
    AgentTypeExplore     = "explore"
    AgentTypeGeneral     = "general"
    AgentTypeCoordinator = "coordinator"
    subagentMaxIter      = 50
)

// SubagentDeps V2 依赖注入
type SubagentDeps struct {
    ChatModel    einomodel.ToolCallingChatModel  // 默认模型
    ModelFactory *model.ModelFactory             // 多模型工厂
    TaskManager  *SubagentTaskManager            // 任务管理器
    Notifier     SubagentNotifier
    ProgressFn   SubagentProgressFn
    Recorder     *session.Recorder
}

type subagentInput struct {
    Name            string `json:"name"`
    Description     string `json:"description"`
    Prompt          string `json:"prompt"`
    AgentType       string `json:"agent_type"`       // explore | general | coordinator
    Model           string `json:"model"`            // 可选，provider/model 格式
    RunInBackground bool   `json:"run_in_background"` // 异步执行
}

// NewSubagentTool V2 工具定义
func (e *Env) NewSubagentTool(deps *SubagentDeps) tool.InvokableTool {
    info := &schema.ToolInfo{
        Name: "subagent",
        Desc: "Delegate a task to a subagent. Supports sync (default) " +
              "and async (run_in_background=true) execution. " +
              "Async subagents return a task ID for later status checks.",
        ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
            "name":        {Type: schema.String, Desc: "Short name (1-3 words)", Required: true},
            "description": {Type: schema.String, Desc: "Brief description for UI", Required: true},
            "prompt":      {Type: schema.String, Desc: "Detailed instructions with context", Required: true},
            "agent_type": {
                Type: schema.String,
                Desc: "Type: 'explore' (read-only, default), 'general' (full tools), 'coordinator' (task decomposition)",
                Required: false,
            },
            "model": {
                Type: schema.String,
                Desc: "Model override in 'provider/model' format (e.g. 'openai/gpt-4o-mini'). Defaults to parent model.",
                Required: false,
            },
            "run_in_background": {
                Type: schema.Boolean,
                Desc: "Run asynchronously. Returns task ID immediately. Default: false.",
                Required: false,
            },
        }),
    }
    return &subagentTool{env: e, deps: deps, info: info}
}
```

### 5. 任务管理工具集

三个独立工具，委托给 `SubagentTaskManager` 实现。

```go
// internal/tools/task_tools.go

package tools

// NewTaskListTool 列出子代理任务
func NewTaskListTool(manager *SubagentTaskManager) tool.InvokableTool {
    // 工具名: task_list
    // 参数: status? (pending|running|completed|failed|stopped)
    // 返回: JSON 数组 [{id, name, agent_type, status, duration}]
}

// NewTaskGetTool 获取任务详情和输出
func NewTaskGetTool(manager *SubagentTaskManager) tool.InvokableTool {
    // 工具名: task_get
    // 参数: task_id (必填)
    // 返回: JSON {id, name, agent_type, model, status, output, error, started, ended}
}

// NewTaskStopTool 停止运行中的任务
func NewTaskStopTool(manager *SubagentTaskManager) tool.InvokableTool {
    // 工具名: task_stop
    // 参数: task_id (必填)
    // 返回: 确认消息或错误
}
```

### 6. Reminder 集成

通过现有的 reminder middleware 将子代理完成通知注入主代理上下文。

```go
// internal/agent/reminder.go — 扩展部分

// SubagentReminderSource 从 SubagentTaskManager 读取通知并格式化为 reminder
type SubagentReminderSource struct {
    Manager *SubagentTaskManager
}

// GetReminders 返回待注入的子代理完成通知
func (s *SubagentReminderSource) GetReminders() []string {
    notifications := s.Manager.DrainNotifications()
    var reminders []string
    for _, n := range notifications {
        reminder := fmt.Sprintf(
            "<subagent-notification>\n"+
                "  <task-id>%s</task-id>\n"+
                "  <name>%s</name>\n"+
                "  <status>%s</status>\n"+
                "  <summary>%s</summary>\n"+
                "</subagent-notification>",
            n.TaskID, n.Name, n.Status, n.Summary,
        )
        reminders = append(reminders, reminder)
    }
    return reminders
}
```

---

## 数据流

### 同步子代理（默认，向后兼容）

```
主代理 → subagent(run_in_background=false)
           │
           ├─ SubagentTaskManager.Submit(background=false)
           │     │
           │     ├─ 创建 SubagentTask(status=running)
           │     ├─ 选择模型 (ModelFactory 或 fallback)
           │     ├─ CloneForSubagent() 创建子环境
           │     ├─ adk.NewChatModelAgent() 创建子代理
           │     ├─ 执行子代理 (阻塞)
           │     ├─ 更新 SubagentTask(status=completed)
           │     └─ 返回结果
           │
           └─ 返回文本结果给主代理
```

### 异步子代理

```
主代理 → subagent(run_in_background=true)
           │
           ├─ SubagentTaskManager.Submit(background=true)
           │     │
           │     ├─ 创建 SubagentTask(status=pending)
           │     ├─ 检查并行上限 (≤10)
           │     ├─ 启动 goroutine:
           │     │     ├─ 更新 status=running
           │     │     ├─ 选择模型
           │     │     ├─ CloneForSubagent()
           │     │     ├─ adk.NewChatModelAgent()
           │     │     ├─ 执行子代理
           │     │     ├─ 更新 status=completed/failed
           │     │     ├─ 写入 SubagentNotification
           │     │     └─ 通知 TUI
           │     │
           │     └─ 立即返回 task ID
           │
           └─ 返回 "Task bg_subagent_3 started" 给主代理
                    │
                    │  (后续某次迭代)
                    │
主代理 ← reminder middleware 注入通知:
         "<subagent-notification>
           <task-id>bg_subagent_3</task-id>
           <status>completed</status>
           <summary>Found 3 usages of ...</summary>
         </subagent-notification>"
```

### 协调模式

```
主代理 → subagent(agent_type=coordinator)
           │
           ├─ 创建 Coordinator 子代理
           │     │
           │     ├─ Coordinator 分析任务并分解
           │     ├─ Coordinator 调用 subagent() × N (创建 workers)
           │     │     ├─ Worker 1 (async) → task_id_1
           │     │     ├─ Worker 2 (async) → task_id_2
           │     │     └─ Worker 3 (async) → task_id_3
           │     │
           │     ├─ Coordinator 调用 task_get() 轮询 workers
           │     │     ├─ Worker 1 完成 ✓
           │     │     ├─ Worker 2 完成 ✓
           │     │     └─ Worker 3 失败 ✗ → 决定跳过
           │     │
           │     └─ Coordinator 合成最终报告返回
           │
           └─ 主代理收到合成结果
```

### 嵌套子代理

```
Main Agent (depth=0)
  └─ subagent "refactor" (general, depth=1)
       ├─ subagent "analyze" (explore, depth=2) → 返回分析结果
       ├─ 基于分析结果执行修改
       └─ subagent "verify" (explore, depth=2) → 返回验证结果
            └─ (depth=2 < MaxDepth=3, 如需要可再嵌套)
                 └─ subagent (depth=3) → 最大深度，不允许再嵌套
```

---

## 实现计划

### 阶段 1：异步执行引擎 + 任务管理（M1）

**目标**：实现核心的异步执行能力和任务 CRUD API。

#### 任务分解

| # | 任务 | 文件 | 依赖 | 估计 |
|---|------|------|------|------|
| 1.1 | 实现 `SubagentTaskManager` | `internal/tools/subagent_manager.go` (新建) | 无 | 4h |
| 1.2 | 重构 `subagentTool.InvokableRun` 支持 background 模式 | `internal/tools/subagent.go` | 1.1 | 3h |
| 1.3 | 实现 `task_list` / `task_get` / `task_stop` 工具 | `internal/tools/task_tools.go` (新建) | 1.1 | 3h |
| 1.4 | 扩展 reminder middleware 注入子代理通知 | `internal/agent/reminder.go` | 1.1 | 2h |
| 1.5 | 在 runner 中注册新工具到主代理工具列表 | `internal/runner/runner.go` | 1.2, 1.3 | 1h |
| 1.6 | SubagentTaskManager 单元测试 | `internal/tools/subagent_manager_test.go` (新建) | 1.1 | 2h |
| 1.7 | 异步子代理集成测试 | `internal/tools/subagent_test.go` (新建) | 1.2 | 2h |

#### 关键设计决策

- **goroutine 管理**：使用 `context.WithCancel` 创建子 context，`Stop` 时调用 `cancel()`
- **并行控制**：使用 `sync.Semaphore`（`golang.org/x/sync/semaphore`）限制并行数
- **通知队列**：复用 `BackgroundManager` 的 `[]Notification` + `DrainNotifications()` 模式
- **ID 生成**：格式 `bg_subagent_{递增数字}`，与现有 `bg_{N}` 命名空间分离

### 阶段 2：多模型支持（M2）

**目标**：允许子代理使用不同模型。

| # | 任务 | 文件 | 依赖 | 估计 |
|---|------|------|------|------|
| 2.1 | 实现 `ModelFactory` | `internal/model/factory.go` (新建) | 无 | 2h |
| 2.2 | `SubagentDeps` 中注入 `ModelFactory` | `internal/tools/subagent.go` | 2.1 | 1h |
| 2.3 | `subagentTool` 解析 `model` 参数并调用工厂 | `internal/tools/subagent.go` | 2.1 | 1h |
| 2.4 | 在 runner/main 中初始化 `ModelFactory` | `internal/runner/runner.go`, `cmd/coding/handlers.go` | 2.1 | 1h |
| 2.5 | ModelFactory 单元测试 | `internal/model/factory_test.go` (新建) | 2.1 | 1h |

#### 关键设计决策

- **缓存策略**：`ModelFactory` 内部维护 `map[string]ChatModel`，相同 provider/model 组合复用实例
- **验证**：`model` 参数中的 provider 必须存在于 `config.Models` 中，model 必须在对应 `ProviderConfig.Models` 列表中
- **Fallback**：空 `model` 参数 → 使用 `SubagentDeps.ChatModel`（当前行为不变）

### 阶段 3：子代理嵌套 + TUI 增强（M3）

**目标**：支持有限深度嵌套，TUI 展示后台子代理状态。

| # | 任务 | 文件 | 依赖 | 估计 |
|---|------|------|------|------|
| 3.1 | `Env` 增加 `Depth` 字段，`CloneForSubagent` 递增 | `internal/tools/env.go` | 无 | 0.5h |
| 3.2 | `buildTools()` 根据深度决定是否包含 subagent 工具 | `internal/tools/subagent.go` | 3.1, M1 | 1h |
| 3.3 | 嵌套子代理共享 `SubagentTaskManager` | `internal/tools/subagent.go` | 3.2 | 0.5h |
| 3.4 | TUI 状态栏显示子代理计数 | `internal/tui/statusbar_component.go` | M1 | 2h |
| 3.5 | TUI 子代理完成通知展示 | `internal/tui/messages.go` | M1 | 1h |
| 3.6 | 嵌套子代理集成测试 | `internal/tools/subagent_test.go` | 3.2 | 2h |

#### 关键设计决策

- **深度限制**：`MaxSubagentDepth = 3`，在 `CloneForSubagent()` 时检查
- **资源传递**：嵌套子代理与父代理共享同一个 `SubagentTaskManager` 实例，保证全局视图
- **工具过滤**：`depth >= MaxSubagentDepth` 时 `buildTools()` 不包含 subagent 工具

### 阶段 4：协调模式 + Session 增强（M4）

**目标**：实现 coordinator + worker 架构，增强 session 记录。

| # | 任务 | 文件 | 依赖 | 估计 |
|---|------|------|------|------|
| 4.1 | 新增 `coordinator` agent_type 和系统提示词 | `internal/tools/subagent.go` | M1 | 2h |
| 4.2 | Coordinator 工具集（含 subagent、task_*） | `internal/tools/subagent.go` | 4.1, M1 | 1h |
| 4.3 | Coordinator 提示词设计与调优 | `internal/tools/subagent.go` | 4.1 | 2h |
| 4.4 | Session 新增子代理事件类型 | `internal/session/session.go` | 无 | 1h |
| 4.5 | Session 记录异步子代理生命周期 | `internal/session/session.go` | 4.4, M1 | 1h |
| 4.6 | 协调模式端到端测试 | `internal/tools/subagent_test.go` | 4.2 | 2h |

#### Coordinator 系统提示词设计

```
You are a coordinator subagent. Your job is to:
1. Analyze the task and decompose it into independent subtasks
2. Create worker subagents (run_in_background=true) for each subtask
3. Monitor worker progress using task_list and task_get
4. Handle failures: retry or skip based on severity
5. Synthesize all worker results into a comprehensive final report

Guidelines:
- Create no more than 5 workers per coordination task
- Each worker should have a clear, focused scope
- Use 'explore' type for read-only analysis, 'general' for modifications
- Wait for all workers before synthesizing results
```

---

## 文件变更清单

| 文件 | 操作 | 说明 |
|------|------|------|
| `internal/tools/subagent_manager.go` | **新建** | SubagentTaskManager 实现 |
| `internal/tools/subagent_manager_test.go` | **新建** | 单元测试 |
| `internal/tools/task_tools.go` | **新建** | task_list / task_get / task_stop 工具 |
| `internal/tools/subagent.go` | **重构** | 支持 background、model、coordinator |
| `internal/tools/subagent_test.go` | **新建** | 集成测试 |
| `internal/tools/env.go` | **修改** | 增加 Depth 字段和 CanNest() |
| `internal/model/factory.go` | **新建** | ModelFactory |
| `internal/model/factory_test.go` | **新建** | 单元测试 |
| `internal/agent/reminder.go` | **修改** | 增加 SubagentReminderSource |
| `internal/session/session.go` | **修改** | 新增事件类型 |
| `internal/runner/runner.go` | **修改** | 注册新工具、初始化 TaskManager |
| `internal/tui/statusbar_component.go` | **修改** | 显示子代理计数 |
| `internal/tui/messages.go` | **修改** | 子代理完成通知 |
| `cmd/coding/handlers.go` | **修改** | 初始化 ModelFactory |

---

## 风险与缓解

| 风险 | 概率 | 影响 | 缓解措施 |
|------|------|------|---------|
| goroutine 泄露 | 中 | 高 | 所有子代理 goroutine 绑定 `context.WithCancel`；`SubagentTaskManager` 在 shutdown 时调用所有 `cancel()` |
| 并行子代理竞态修改同一文件 | 中 | 高 | 协调模式文档中指导用户按文件/目录划分 worker 范围；V2 不做运行时文件锁（留作 V3） |
| 多模型配置错误导致运行时 panic | 低 | 中 | `ModelFactory.GetModel()` 返回清晰错误信息；`subagentTool` 在创建子代理前验证模型 |
| 嵌套子代理递归失控 | 低 | 高 | 硬编码 `MaxSubagentDepth=3`；全局并行上限 10 个 |
| TUI 高频通知导致界面闪烁 | 中 | 低 | 通知合并：500ms 内的多条通知 batch 展示 |
