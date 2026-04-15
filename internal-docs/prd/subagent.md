# 子代理系统 V2 — 产品需求文档 (PRD)

## 背景与目标

### 背景

jcode 当前的子代理系统（`internal/tools/subagent.go`）采用**同步阻塞式执行**模型：主代理调用 `subagent` 工具后必须等待子代理执行完毕才能继续。子代理仅支持两种类型（`explore` / `general`），不支持嵌套、并行执行或模型选择，也没有任务生命周期管理能力。

对比 Claude Code 的异步后台任务系统，jcode 在以下方面存在差距：

| 能力 | 当前状态 | 目标状态 |
|------|---------|---------|
| 执行模式 | 同步阻塞 | 同步 + 异步可选 |
| 任务并行 | 不支持 | 支持多子代理并行 |
| 子代理嵌套 | 禁止 | 有限深度嵌套 |
| 模型选择 | 固定（继承父代理模型） | Per-subagent 模型覆盖 |
| 任务管理 | 无 | 完整 CRUD + 生命周期 |
| 协调模式 | 无 | Coordinator + Worker |

### 目标

构建 jcode 子代理系统 V2，使其具备：

1. **异步非阻塞执行**：子代理在后台运行，主代理可继续对话或发起更多子代理
2. **多模型支持**：不同子代理可使用不同模型（如用轻量模型做探索、强模型做编码）
3. **任务管理**：提供标准化的任务 CRUD API，支持查询状态、获取输出、停止任务
4. **有限嵌套**：子代理可再次创建子代理，最大深度限制为 3 层
5. **协调模式**：支持 coordinator 分解任务并分配给多个 worker 并行执行

### 非目标

- 不实现跨机器分布式子代理（当前仅限本地 / 单 SSH 连接）
- 不实现 git worktree 级别的文件系统隔离（V2 范围外）
- 不实现子代理间的双向消息通信（仅支持结果返回）
- 不实现 remote agent 类型（保留为未来扩展）

---

## 用户故事

### US-1：异步子代理执行
> 作为 jcode 用户，我希望在子代理执行长时间任务时，主对话不被阻塞，这样我可以继续提问或启动其他任务。

**验收标准**：
- 用户可通过 `run_in_background: true` 参数启动后台子代理
- 后台子代理完成后，通知注入主代理上下文
- 用户可随时查询后台子代理状态
- TUI 状态栏显示正在运行的后台子代理数量

### US-2：子代理模型选择
> 作为 jcode 用户，我希望为不同子代理指定不同模型，这样探索任务可以用低成本模型，复杂编码任务用高能力模型。

**验收标准**：
- `subagent` 工具支持可选 `model` 参数
- 模型标识符使用 `provider/model` 格式（如 `openai/gpt-4o-mini`）
- 未指定时使用主代理的模型
- 指定的模型必须在 `~/.jcoding/config.json` 中已配置

### US-3：任务管理
> 作为 jcode 用户，我希望能够查看、管理所有正在运行和已完成的子代理任务。

**验收标准**：
- 主代理可调用 `task_list` 列出所有任务
- 主代理可调用 `task_get` 获取任务详情和输出
- 主代理可调用 `task_stop` 停止正在运行的子代理
- 已完成任务的输出可按需获取（非阻塞）

### US-4：子代理嵌套
> 作为 jcode 用户，当子代理发现任务可以进一步拆分时，我希望子代理能创建自己的子代理来完成子任务。

**验收标准**：
- `general` 类型子代理可创建嵌套子代理
- 最大嵌套深度为 3 层
- 超出深度限制时返回清晰错误
- 嵌套子代理的资源（goroutine、内存）随父代理取消而释放

### US-5：协调模式
> 作为 jcode 用户，面对复杂的多文件重构任务，我希望 jcode 能自动将任务拆分为多个子任务并行执行。

**验收标准**：
- 主代理可创建 coordinator 类型子代理
- Coordinator 负责任务分解并创建多个 worker 子代理
- Workers 并行执行，各自有独立的 TodoStore
- Coordinator 收集所有 worker 结果并合成最终报告
- 任何 worker 失败不影响其他 worker 执行

---

## 功能需求

### P0 — 核心功能（必须实现）

#### F-1：异步子代理执行引擎

**描述**：基于 goroutine + channel 实现异步子代理执行，复用现有 `BackgroundManager` 模式。

**子需求**：
- F-1.1：`subagent` 工具新增 `run_in_background` 布尔参数
- F-1.2：后台子代理返回 task ID，主代理可通过 task ID 查询状态
- F-1.3：后台子代理完成时生成通知，通过 reminder middleware 注入主代理上下文
- F-1.4：子代理 context 与父代理关联，支持级联取消
- F-1.5：后台子代理输出截断上限 4000 字符（保持上下文精简）

#### F-2：任务管理工具集

**描述**：提供标准化任务管理 API，以工具形式暴露给主代理。

| 工具 | 参数 | 返回 |
|------|------|------|
| `task_list` | `status?` (filter) | 任务列表（ID、名称、状态、时长） |
| `task_get` | `task_id` | 任务详情 + 输出 |
| `task_stop` | `task_id` | 确认停止 |

- F-2.1：`SubagentTaskManager` 管理所有子代理任务生命周期
- F-2.2：任务状态：`pending` → `running` → `completed` / `failed` / `stopped`
- F-2.3：最多保留 20 个已完成任务记录（FIFO 淘汰）
- F-2.4：完成通知格式与现有 `BgNotification` 对齐

#### F-3：多模型支持

**描述**：允许创建子代理时指定模型。

- F-3.1：`subagent` 工具新增 `model` 可选参数，格式 `provider/model`
- F-3.2：`SubagentDeps` 中注入 `ModelFactory func(provider, model string) (ChatModel, error)`
- F-3.3：未指定模型时 fallback 到 `SubagentDeps.ChatModel`（当前行为不变）
- F-3.4：模型验证——不在 config 中的 provider/model 组合返回错误

### P1 — 重要功能

#### F-4：子代理嵌套

**描述**：允许 `general` 类型子代理创建嵌套子代理。

- F-4.1：`Env` 增加 `depth int` 字段追踪嵌套层级
- F-4.2：`CloneForSubagent()` 时 `depth++`，超过 `MaxSubagentDepth=3` 时拒绝创建
- F-4.3：`buildTools()` 在 depth < MaxSubagentDepth 时为 general 类型包含 subagent 工具
- F-4.4：嵌套子代理共享父代理的 `SubagentTaskManager` 实例

#### F-5：TUI 增强

**描述**：TUI 展示后台子代理状态。

- F-5.1：状态栏显示 `[agents: 2 running, 1 done]` 格式
- F-5.2：子代理完成时在输出区域显示通知消息
- F-5.3：支持通过快捷键查看活跃子代理列表

### P2 — 锦上添花

#### F-6：协调模式

**描述**：实现 coordinator + worker 架构。

- F-6.1：新增 `coordinator` agent_type
- F-6.2：Coordinator 系统提示词引导其分解任务并创建 worker
- F-6.3：Coordinator 可创建多个并行 worker 子代理
- F-6.4：Coordinator 等待所有 worker 完成后合成结果
- F-6.5：Worker 失败时 coordinator 决定是否重试或跳过

#### F-7：Session 记录增强

**描述**：增强 session JSONL 记录以支持异步任务追踪。

- F-7.1：新增 `subagent_background_start`、`subagent_background_done` 事件类型
- F-7.2：记录 task ID、模型、agent_type 等元数据
- F-7.3：session replay 时能正确重放子代理事件

---

## 非功能需求

### NFR-1：性能
- 子代理启动延迟 < 100ms（不含首次 API 调用）
- 并行运行子代理数量硬上限 10 个
- 单个子代理最大迭代次数保持 50 次
- 后台任务管理器内存占用 < 10MB（20 个已完成任务缓存）

### NFR-2：可靠性
- 子代理 panic 不崩溃主进程（recover 保护）
- 父 context 取消时所有子代理在 5s 内优雅退出
- 网络临时错误自动重试（已有 `ModelRetryConfig`）

### NFR-3：可观测性
- 所有子代理事件记录到 `~/.jcoding/debug.log`
- Langfuse trace 中子代理作为独立 span
- Session JSONL 记录完整的子代理生命周期

### NFR-4：向后兼容
- 现有 `subagent` 工具的同步调用行为保持不变（`run_in_background` 默认为 `false`）
- 现有 `explore` / `general` agent_type 语义不变
- 现有 session 格式向后兼容

---

## 成功指标

| 指标 | 目标值 | 度量方式 |
|------|-------|---------|
| 异步子代理启动成功率 | > 99% | debug.log 分析 |
| 并行子代理平均完成时间（vs 串行） | 降低 40%+ | session 时间戳对比 |
| 子代理嵌套使用率 | > 10% 的 general 子代理会嵌套 | session 事件统计 |
| 多模型选择使用率 | > 20% 的子代理指定非默认模型 | session 元数据统计 |
| 任务管理工具调用频率 | 每 session 平均 > 2 次 | session 事件统计 |
| TUI 通知延迟（子代理完成到展示） | < 500ms | TUI 事件时间戳 |

---

## 里程碑

| 阶段 | 内容 | 估计周期 |
|------|------|---------|
| **M1** | 异步执行引擎 + 任务管理 API (F-1, F-2) | 1 周 |
| **M2** | 多模型支持 (F-3) | 0.5 周 |
| **M3** | 子代理嵌套 (F-4) + TUI 增强 (F-5) | 1 周 |
| **M4** | 协调模式 (F-6) + Session 增强 (F-7) | 1 周 |
| **M5** | 集成测试 + 性能调优 + 文档 | 0.5 周 |
