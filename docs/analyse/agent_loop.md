# jcode (Go) vs Claude Code (JS/TS) — Agent Loop 实现比较分析

## 概述

**jcode (Go)** 是一个轻量级的命令行AI编码助手，采用**单一代理 + 中间件栈**的架构，主要使用 Eino 框架的 ChatModelAgent。

**Claude Code (JS/TS)** 是一个企业级的完整聊天IDE集成，采用**查询引擎 + 多任务管理**的架构，具有高级的上下文管理和协作者模式。

这两个系统虽然目标不同，但都围绕"代理循环"这一核心概念构建。本文详细对比两者的实现差异。

---

## jcode 实现分析

### 1. 核心循环架构

**文件**: [internal/runner/runner.go](../internal/runner/runner.go)

```
Run() → runInner() → ag.Run() → iterator.Next() → Event Processing
|
├─ Message → Iterator (Eino)
├─ Event Flow (Tool Call / Tool Result / Assistant Message)
└─ Stream Processing (support for streaming messages)
```

**关键特性**:
- **事件驱动**型循环 (Events: Tool, Assistant, MessageOutput)
- **流式处理支持**: Tool results 和 Assistant messages 均支持流式 (IsStreaming=true)
- **同步迭代**: `iterator.Next()` 阻塞式边界

**代码位置**:
- `runner.go L23-50`: 主循环函数签名与完成守护
- `runner.go L51-65`: 不完成 Todo 的重试逻辑 (maxGuardRetries=3)
- `runner.go L71-150`: runInner 核心迭代逻辑

### 2. Eino Agent 工厂

**文件**: [internal/agent/agent.go](../internal/agent/agent.go)

```go
NewAgent(ctx, chatmodel, tools, instruction, approvalFunc, middlewares, handlers)
  → adk.NewChatModelAgent(&adk.ChatModelAgentConfig{
      MaxIterations: 1000,
      ModelRetryConfig: {MaxRetries: 3},
      Handlers: [summarization, reduction, approval+safeTool],
      Middlewares: [langfuse, ...]
    })
```

**关键参数**:
- `MaxIterations: 1000` — 每个 agent 运行的最大迭代次数
- `ModelRetryConfig.MaxRetries: 3` — API 调用失败时的重试次数 (指数退避)
- **处理器堆栈顺序** (从外到内): langfuse → summarization → reduction → **approval + safeTool** (最内层)

### 3. 工具执行与批准流程

**文件**: [internal/agent/middleware.go](../internal/agent/middleware.go)

```
approvalMiddleware.WrapInvokableToolCall()
  → approval gate (ApprovalFunc)
    ├─ AUTO mode: 直接通过
    ├─ MANUAL mode: 调用 RequestApproval
    └─ User rejects: 返回特殊提示信息，agent 不会重试
  → safe execution (error → string, not panic)
```

**关键实现**:
- **错误大网**: Tool 执行错误被转换为 agent 可见的字符串 (不中断循环)
- **批准拒绝处理**: 用户拒绝时返回 "IMPORTANT: Do NOT attempt to perform the same action using alternative tools"
- **读写分离**: read-only tools (grep, read, todo*) 跳过批准

### 4. 批准状态机

**文件**: [internal/runner/approval.go](../internal/runner/approval.go)

```
ApprovalState
  ├─ mode: ModeManual | ModeAuto
  ├─ SetMode(mode) — 用户切换模式
  └─ RequestApproval(toolName, args)
      → Auto: return true
      → Manual:
          ├─ No-approval tools: auto true
          ├─ read-tool: 检查 filePath 是否在 workpath 内
          ├─ execute-tool: background=true / safe prefixes → auto true
          └─ Other tools: requestUserApproval() → TUI 提示
```

### 5. Todo 完成守护与重试

**文件**: `runner.go L35-50`

```go
for i := 0; i < maxGuardRetries; i++ {
  if !todoStore.HasIncomplete() { break }
  reminder := todoStore.IncompleteSummary()
  extra := runInner(ctx, ag, messages, p, rec, todoStore)
  resp += extra
}
```

**特点**: 最多 3 次重试，消息追加，跨迭代 TodoStore 状态保留

### 6. 会话记录

**文件**: [internal/session/session.go](../internal/session/session.go)

- 格式: JSONL 每行一条 Entry (EntryType 标识)
- 类型: session_start, user, assistant, tool_call, tool_result, subagent_start, subagent_result

---

## Claude Code 实现分析

### 1. 核心查询引擎架构

**文件**: `src/QueryEngine.ts`

```typescript
class QueryEngine {
  async *submitMessage(prompt, options?)  // 异步生成器
    → processUserInput()
    → for await (const message of query(...))
        ├─ normalizeMessage(message)
        ├─ recordTranscript(messages)
        └─ yield {type, message, ...}
}
```

**关键创新**:
- **异步生成器**: `async *submitMessage()` 允许调用者逐个处理消息，支持流式UI更新
- **双层消息处理**: 先处理用户输入 → 再进入主查询循环
- **许可拒绝跟踪**: 所有被拒绝的工具调用记录在 `permissionDenials[]`

### 2. 消息类型丰富度

| 消息类型 | 来源 | 用途 |
|--------|------|-----|
| `assistant` | API | Agent 响应，可能含 tool_use |
| `tool_result` | Tool Executor | 工具返回值 |
| `user` | QueryEngine | 用户消息或任务通知 |
| `progress` | Tool Streaming | 工具执行进度 |
| `attachment` | Query Engine | 非消息数据 (structured_output, max_turns_reached) |
| `stream_event` | API Stream | 原始推送事件 |
| `compact_boundary` | Snipping Engine | 历史压缩边界 |
| `tombstone` | Message Store | 消息删除 |

### 3. 任务管理系统

**文件**: `src/Task.ts` + `src/tasks/`

```typescript
type TaskType = 
  | 'local_bash'          // 本地 shell 命令
  | 'local_agent'         // 本地代理 (子代理)
  | 'remote_agent'        // 远程 SSH 代理
  | 'in_process_teammate' // 同进程队友 (团队协作)
  | 'local_workflow'      // 工作流引擎
  | 'monitor_mcp'         // MCP 服务监控
  | 'dream'               // 梦想模式 (搜索/规划)

type TaskStatus = 'pending' | 'running' | 'completed' | 'failed' | 'killed'
```

**Task ID 生成** (安全性): 前缀 + 8 字节加密随机 (base36) → 36^8 ≈ 2.8 trillion 组合

### 4. 协调模式 (Coordinator Mode)

**文件**: `src/coordinator/coordinatorMode.ts`

```
User Question → Coordinator Agent (thinks strategically)
  ├─ Spawns Worker #1 (via agent_tool)
  ├─ Spawns Worker #2 (in parallel)
  └─ Synthesizes results

(Workers run in background as tasks)
```

**Worker 工具访问**:
- SIMPLE 模式: `[bash, read, edit, mcp_tools]`
- 完整模式: `[agent, bash, read, edit, ..., skill_tool, mcp_tools]`

### 5. 高级错误恢复

```
第一层: Max Output Tokens 恢复 (最多3次)
第二层: Prompt Too Long 恢复 (snipping/compaction)
第三层: Fallback Model 恢复
```

### 6. 成本跟踪与预算

- 消息级成本累加
- 转数预算 (token budget per turn)
- 任务预算 (task_budget API)

### 7. 上下文管理与压缩

```
Auto Compact → Snipping → Reactive Compact → Context Collapse
```

---

## 差异对比表

| 维度 | jcode (Go) | Claude Code (JS/TS) |
|------|-----------|------------------|
| **核心循环模式** | 同步迭代 (`iterator.Next()`) | 异步生成器 (`async *submitMessage()`) |
| **框架** | Eino (CloudWeGo) | 自研 Query Engine |
| **Max Iterations** | 1000 (per agent) | configurable (maxTurns) |
| **错误重试** | 3 次 (ModelRetryConfig) | 分层重试 (MaxOutputTokens, FallbackModel, API Retry) |
| **代理并发** | 顺序执行 (子代理) | 并行执行 (多 tasks) + 协调模式 |
| **任务类型** | subagent (2 种: explore/general) | 7 种 (bash, agent, remote_agent, teammate, workflow, mcp, dream) |
| **工具批准** | 二元 (AUTO/MANUAL) | 三元 (auto/manual/deny) + 细粒度规则 |
| **背景任务** | BackgroundManager (简单队列) | TaskState 系统 (完整生命周期管理) |
| **会话记录** | JSONL + 索引 | JSONL + UUID 跟踪 |
| **上下文管理** | 无压缩 | Snipping + Reactive Compact + Context Collapse |
| **成本跟踪** | Token counter 仅日志 | 完整成本追踪 + 预算控制 |
| **协调模式** | N/A | 完全支持 (worker spawn/continue) |
| **结构化输出** | N/A | 完全支持 (schema validation) |
| **会话恢复** | --resume UUID | 完整重建 + 压缩感知恢复 |

---

## Claude Code 优势分析

### 1. 异步生成器架构
- 非阻塞流式处理，UI 可以逐条渲染消息
- 支持中断和取消操作
- 更好的内存管理（不需要缓存所有消息）

### 2. 多层错误恢复
- MaxOutputTokens → snipping → fallback model 三级降级
- jcode 仅有 API 重试，无上下文压缩

### 3. 协调者模式
- 支持多 worker 并行执行
- 上游/下游通信
- 共享 scratchpad 知识库

### 4. 上下文压缩
- 自动检测上下文快满，触发压缩
- 保留关键信息（用户消息、最近工具调用）
- jcode 无此能力，长对话会超出上下文

### 5. 预算与成本控制
- 每转/每任务的 token 预算
- 实时成本追踪
- jcode 仅记录 token 使用量

---

## 改进建议

1. **引入异步生成器模式** — 使用 Go channel 实现类似的流式处理
2. **实现上下文压缩** — 检测 token 使用量，自动摘要历史消息
3. **添加分层错误恢复** — MaxOutput → Snip → Fallback Model
4. **支持协调模式** — 多 subagent 并行 + coordinator
5. **完善成本追踪** — 每转预算控制，实时 cost tracking
6. **增强任务类型** — 支持 dream/workflow/monitor 等高级任务
