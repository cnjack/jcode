# jcode vs Claude Code 执行工具详细对比分析

## 概述

jcode实现了基础的前台/后台命令执行（单Shell），Claude Code提供了多Shell、自适应后台化、REPL虚拟机、Cron调度、远程触发等企业级能力。

---

## jcode 实现分析

### ExecuteTool

**文件**: [internal/tools/execute.go](../internal/tools/execute.go)

- **参数**: command, timeout(默认120s,最大600s), background
- **后台**: BackgroundManager异步执行，返回task ID
- **前台**: 阻塞等待结果

### BackgroundManager

**文件**: [internal/tools/background.go](../internal/tools/background.go)

- 递增任务ID (`bg_1`, `bg_2`)
- `context.WithoutCancel()` 分离上下文
- 5分钟超时 + 2000字符输出截断
- 最多100个未读通知
- `check_background`工具查询状态

### Executor接口

**文件**: [internal/tools/env.go](../internal/tools/env.go)

- LocalExecutor: exec.CommandContext
- SSHExecutor: 远程bash执行

---

## Claude Code 实现分析

### BashTool

**文件**: `src/tools/BashTool/BashTool.tsx`

**扩展参数**: command, timeout, description, run_in_background, dangerouslyDisableSandbox

**AsyncGenerator流式进度**: yield实时进度更新

### 自适应后台化（关键优势）

```typescript
const ASSISTANT_BLOCKING_BUDGET_MS = 15_000  // 15秒自动后台化
```

- 时间预算机制
- Sleep模式检测与阻止
- 动态前台→后台转换

### LocalShellTask框架

- 完整生命周期: running → completed/failed/killed
- Stall看门狗: 45秒无进展检测交互式提示
- XML标签任务通知
- 磁盘持久化输出(64MB)

### PowerShellTool

Windows特化Shell支持

### REPL虚拟机

- VM内部访问原始工具
- 单一连续执行环境
- 适合脚本编程

### Cron调度

- 标准5字段cron表达式
- 一次性/重复任务
- 持久化/会话内存
- 最多50个任务

### 远程触发

- OAuth2认证
- HTTP REST操作
- 远程任务同步

---

## 差异对比表

| 特性 | jcode | Claude Code |
|------|-------|------------|
| **Shell** | Bash only | Bash + PowerShell |
| **默认超时** | 120s | 30s(可配置) |
| **自动后台化** | 无 | 15s时间预算 |
| **进度流式** | 无 | AsyncGenerator |
| **输出截断** | 2000字符 | 64MB磁盘 |
| **REPL** | 无 | 完整VM |
| **Cron** | 无 | 5字段标准 |
| **远程触发** | 仅SSH | OAuth2 API |
| **Stall检测** | 无 | 45s看门狗 |
| **Sleep阻止** | 无 | 模式检测 |

---

## 改进建议

1. **自适应后台化** — 超时自动转为后台任务
2. **流式进度** — 通过channel流式返回输出
3. **Stall检测** — 监控输出增长，检测交互式提示
4. **输出持久化** — 写入磁盘而非内存截断
5. **Sleep检测** — 阻止长时间sleep命令
6. **REPL模式** — 支持交互式执行环境
