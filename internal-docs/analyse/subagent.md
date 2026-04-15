# jcode 与 Claude Code 子代理系统分析

## 概述

- **jcode (Go)** — 轻量级同步子代理模型，基于 Eino 框架
- **Claude Code (JS/TS)** — 完整的异步后台任务与多代理团队协调系统

jcode 采用**同步阻塞式执行**，Claude Code 实现了**异步非阻塞式执行**，支持后台任务管理、任务生命周期跟踪、多代理团队协调和工作者间通信。

---

## jcode 实现分析

### 子代理工具架构

**文件**: [internal/tools/subagent.go](../internal/tools/subagent.go)

**两种代理类型**:
| 类型 | 工具集 | 用途 |
|------|-------|------|
| `explore` (默认) | read, grep, execute | 代码搜索/分析 |
| `general` | read, grep, execute, edit, write, todo* | 实际修改 |

**执行流程**: 同步阻塞，最大50次迭代，最终返回文本结果

**环境克隆**: 共享执行器，隔离TodoStore，无文件系统沙箱

**限制**:
- 不允许子代理嵌套
- execute禁用后台模式
- 无并行执行

---

## Claude Code 实现分析

### 异步执行架构

**文件**: `src/tools/AgentTool/AgentTool.tsx`

```typescript
baseInputSchema: z.object({
    description: z.string(),
    prompt: z.string(),
    model?: z.enum(['sonnet', 'opus', 'haiku']),
    run_in_background?: z.boolean(),  // 异步标记
})

fullInputSchema: baseInputSchema.extend({
    name?: z.string(),          // 可寻址名称
    team_name?: z.string(),     // 团队上下文
    mode?: z.enum(['plan', 'normal']),
    isolation?: z.enum(['worktree', 'remote']),
})
```

### 多层任务系统

```typescript
type TaskType = 
  | 'local_bash' | 'local_agent' | 'remote_agent'
  | 'in_process_teammate' | 'local_workflow'
  | 'monitor_mcp' | 'dream'
```

**任务通知格式**:
```xml
<task-notification>
  <task-id>a7k2m9f1</task-id>
  <status>completed</status>
  <summary>...</summary>
</task-notification>
```

### 任务管理工具集

| 工具 | 功能 |
|------|------|
| TaskCreateTool | 创建任务列表项 |
| TaskGetTool | 查询任务 |
| TaskUpdateTool | 更新任务状态 |
| TaskListTool | 列出任务(含依赖) |
| TaskOutputTool | 获取后台任务输出(支持阻塞/非阻塞) |
| TaskStopTool | 停止运行中的任务 |

### 多代理团队系统

**TeamCreateTool**: 创建团队（一个领导者管理一个团队）

**协调模式**: Coordinator Agent → 生成 Worker → 并行执行 → 合成结果

### 与 jcode 的关键区别

| 维度 | jcode | Claude Code |
|------|-------|------------|
| **执行模式** | 同步阻塞 | 异步后台 |
| **嵌套** | 禁止 | 允许 |
| **并行** | 不支持 | 支持 |
| **任务类型** | 2种 | 7种 |
| **模型选择** | 固定 | per-agent覆盖 |
| **隔离** | TodoStore隔离 | worktree/remote隔离 |
| **团队** | 无 | 完整团队系统 |
| **通信** | 仅返回文本 | 邮箱+消息传递 |

---

## 改进建议

1. **实现异步子代理** — go routine + channel 非阻塞模型
2. **支持多模型** — per-subagent 模型选择
3. **添加任务管理API** — TaskCreate/Get/Update/List/Stop
4. **支持子代理嵌套** — 有限深度嵌套
5. **实现工作树隔离** — git worktree 文件系统隔离
6. **添加协调模式** — coordinator + worker 架构
