# jcode 与 Claude Code 任务管理系统对比分析

## 概述

jcode采用简化的内存存储+事件驱动完成守护，Claude Code实现了分层任务监视、持久化计划、主动代理总结和多模式权限控制。

---

## jcode 实现分析

### 待办事项系统

**文件**: [internal/tools/todo.go](../internal/tools/todo.go)

- **全量替换语义**: Update()替换整个列表
- **并发安全**: sync.RWMutex
- **验证**: 检查重复ID、状态有效性、最多1个in_progress
- **辅助**: HasIncomplete(), Summary(), IncompleteSummary()

### 计划存储

**文件**: [internal/tools/plan_store.go](../internal/tools/plan_store.go)

状态机: PlanDraft → PlanSubmitted → PlanApproved/PlanRejected

### 计划解析

**文件**: [internal/tools/plan_parse.go](../internal/tools/plan_parse.go)

- 正则匹配: 编号步骤 + 复选框
- 自动创建TodoItem

### 完成守护

**文件**: [internal/runner/runner.go](../internal/runner/runner.go)

最多3次重试，注入IncompleteSummary提醒

---

## Claude Code 实现分析

### 任务系统

**文件**: `src/Task.ts`

7种任务类型，完整生命周期管理，磁盘持久化输出

### 任务监视系统

**文件**: `src/hooks/useTasksV2.ts`

**四层监视**:
1. 文件系统监视 (fs.watch)
2. 进程内通知 (onTasksUpdated)
3. 备用轮询 (5s间隔)
4. 防抖合并 (50ms窗口)

**自动隐藏**: 任务完成后5秒隐藏

### 任务列表观察器

**文件**: `src/hooks/useTaskListWatcher.ts`

- 外部任务自动认领
- 依赖关系支持 (blockedBy必须全部完成)
- 防竞速条件

### 计划模式

- EnterPlanModeTool: 权限模式转换
- ExitPlanModeTool: 计划持久化 + 队友审批 + 模式恢复

### 代理总结服务

**文件**: `src/services/AgentSummary/agentSummary.ts`

30秒间隔后台周期总结

---

## 差异对比表

| 维度 | jcode | Claude Code |
|------|-------|------------|
| **存储** | 内存 | 磁盘持久化 |
| **监视** | 被动(完成时检查) | 4层主动监视 |
| **任务类型** | 单一TodoItem | 7种TaskType |
| **依赖关系** | 无 | blockedBy支持 |
| **计划审批** | TUI内联 | 多模式(终端/Web/Team) |
| **自动认领** | 无 | 外部任务自动拾取 |
| **总结** | 无 | 周期性代理总结 |

---

## 改进建议

1. **磁盘持久化** — 任务列表写入文件
2. **依赖关系** — blockedBy/blocks 支持
3. **主动监视** — fsnotify 监听任务变化
4. **代理总结** — 定期生成任务摘要
5. **丰富计划模式** — 计划编辑 + 审批工作流
