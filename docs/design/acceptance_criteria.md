# 验收标准 (Acceptance Criteria)

## 1. Model Provider 重构

### AC-1.1: ModelFactory 模型工厂
- [ ] 新建 `internal/model/factory.go`，实现 `ModelFactory` 结构体
- [ ] 支持 `"provider/model"` 格式字符串解析（如 `"openai/gpt-4o"`）
- [ ] 内置实例缓存，相同 provider/model 复用实例
- [ ] 空字符串参数返回 fallback 默认模型
- [ ] 配置中不存在的 provider 或 model 返回清晰错误
- [ ] 单元测试覆盖：解析、缓存命中、错误场景

### AC-1.2: Config 扩展
- [ ] `Config` 新增 `Budget`、`FallbackModel`、`Compaction`、`Prompt`、`Subagent` 配置段
- [ ] 新增 `PromptConfig`、`BudgetConfig`、`CompactionConfig`、`SubagentConfig` 结构体
- [ ] 所有新字段标记 `omitempty`，向后兼容现有 config.json
- [ ] 零值时使用合理的默认值

### AC-1.3: ModelPricing 与 ModelInfo 增强
- [ ] `ModelInfo` 新增 `Cost` (input/output per 1M tokens) 和 `ContextLimit` 字段
- [ ] 扩展 `knownModelContextLimits` 为 `knownModelInfo`，包含定价信息

---

## 2. Agent Loop 增强

### AC-2.1: BudgetManager 预算控制器
- [ ] 新建 `internal/agent/budget.go`
- [ ] `BudgetManager` 支持 Track/Check/Status 方法
- [ ] `budgetMiddleware` 在模型调用后自动追踪 token 消耗
- [ ] 接近阈值时触发 WarningApproach 回调
- [ ] 超限时触发 WarningExceeded 回调
- [ ] TUI 新增 `BudgetWarningMsg` / `BudgetExceededMsg` 消息类型
- [ ] 单元测试覆盖：阈值检测、超限停止

### AC-2.2: Context Compaction 上下文压缩
- [ ] 新建 `internal/agent/compaction.go`
- [ ] `CompactionStrategy` 接口 + `ThresholdCompactionStrategy` 实现
- [ ] `compactionMiddleware` 在 `BeforeModelRewriteState` 中检查 token 使用率
- [ ] 超过阈值（默认 80%）时自动调用压缩
- [ ] 保留最近 N 条消息不压缩（默认 10）
- [ ] 压缩后插入 `CompactBoundary` 摘要消息
- [ ] 断路器机制：连续失败 3 次后停止尝试

### AC-2.3: Layered Error Recovery 分层错误恢复
- [ ] 新建 `internal/agent/recovery.go`
- [ ] Layer 1: 检测 `max_tokens` stop reason → 续写
- [ ] Layer 2: 检测 `context_length_exceeded` → 压缩重试
- [ ] Layer 3: 前两层失败 + 配置了 fallback model → 降级
- [ ] `RecoveryTracker` 防止无限重试循环
- [ ] 所有恢复操作记录日志

### AC-2.4: EventBus 事件总线
- [ ] 新建 `internal/runner/eventbus.go`
- [ ] `EventBus` 支持 Emit/Subscribe/Close
- [ ] 非阻塞发送，满时丢弃最旧事件并记录警告
- [ ] Subscribe 返回只读 channel
- [ ] 统一事件类型定义

### AC-2.5: Agent 中间件栈扩展
- [ ] `NewAgent` 支持 `AgentOption` 可选配置
- [ ] 中间件栈顺序：langfuse → compaction → budget → recovery → reduction → approval+safeTool → reminder
- [ ] 新增中间件均为可选，nil 时跳过

---

## 3. Prompt System 增强

### AC-3.1: PromptBuilder 提示构建器
- [ ] 新建 `internal/prompts/builder.go`
- [ ] `PromptBuilder` 统一入口，替代 `GetSystemPrompt()` 内部逻辑
- [ ] 支持 `PromptBlock` 分块（static + dynamic）
- [ ] 兼容现有调用方：`GetSystemPrompt()` 内部委托 `PromptBuilder`

### AC-3.2: MemoryLoader 多层内存加载
- [ ] 新建 `internal/prompts/memory.go`
- [ ] 三级加载：`~/.jcoding/AGENTS.md` → `{pwd}/AGENTS.md` → `{pwd}/AGENTS.local.md`
- [ ] `@include` 指令递归解析（最大深度 5）
- [ ] 循环引用检测
- [ ] 总内容上限 40000 字符
- [ ] 单元测试覆盖

### AC-3.3: PromptBlockCache 分块缓存
- [ ] 新建 `internal/prompts/cache.go`
- [ ] 基于 SHA256 内容哈希的缓存策略
- [ ] 支持手动 `Invalidate()`

### AC-3.4: AsyncEnvLoader 异步环境加载
- [ ] 新建 `internal/prompts/async_env.go`
- [ ] 并行加载 Git/项目类型/目录树信息
- [ ] 超时 3 秒，单项超时返回零值

### AC-3.5: ContextCompactor 上下文压缩
- [ ] 新建 `internal/prompts/compact.go` + `internal/prompts/compact_prompt.md`
- [ ] `ShouldCompact` / `Compact` / 断路器机制
- [ ] 生成结构化摘要（用户请求、关键决策、文件路径等）
- [ ] 与 ReminderMiddleware 集成

---

## 4. Subagent V2

### AC-4.1: SubagentTaskManager 任务管理器
- [ ] 新建 `internal/tools/subagent_manager.go`
- [ ] 支持 Submit/Get/List/Stop/DrainNotifications
- [ ] background=true 时异步执行，立即返回 task ID
- [ ] background=false 时同步阻塞（向后兼容）
- [ ] 最大并行数限制（默认 10）
- [ ] goroutine 绑定 context.WithCancel

### AC-4.2: Subagent V2 工具重构
- [ ] `subagentTool` 支持 `model`、`run_in_background` 新参数
- [ ] 新增 `coordinator` agent_type
- [ ] `model` 参数通过 ModelFactory 解析
- [ ] 异步模式返回 task ID 字符串

### AC-4.3: Task 管理工具集
- [ ] 新建 `internal/tools/task_tools.go`
- [ ] `task_list`: 列出所有子代理任务
- [ ] `task_get`: 获取任务详情和输出
- [ ] `task_stop`: 停止运行中的任务

### AC-4.4: Env 嵌套深度支持
- [ ] `Env` 新增 `Depth` 字段
- [ ] `CloneForSubagent()` 深度 +1
- [ ] `CanNest()` 检查深度限制（最大 3）
- [ ] 超过最大深度时 `buildTools()` 不包含 subagent 工具

### AC-4.5: Reminder 集成
- [ ] `SubagentReminderSource` 从 TaskManager 读取通知
- [ ] 完成通知以 XML 格式注入主代理上下文

### AC-4.6: Session 扩展
- [ ] 新增 `EntrySubagentAsync`、`EntryBudgetWarning` 等事件类型
- [ ] 记录异步子代理生命周期
