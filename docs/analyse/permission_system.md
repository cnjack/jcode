# jcode vs Claude Code 权限系统对比分析

## 概述

jCode采用简化的二元模式（手动/自动）配合基于前缀的工具白名单，而Claude Code实现了层级化、多模式、持久化的权限管理系统，支持细粒度规则、策略限制、分类器自动审批等高级特性。

---

## jCode 实现分析

### 核心架构

**文件**: [internal/runner/approval.go](../internal/runner/approval.go)

- **`ApprovalState` 结构体** (L17-30): mode(Manual/Auto) + workpath + TUI引用
- **批准逻辑** (L57-149):
  1. 自动模式直接通过
  2. 手动模式按优先级检查：
     - 无需批准工具：grep, todowrite, todoread, subagent, check_background
     - 读文件工具：workpath内自动批准
     - 执行命令：后台任务自动批准 + 安全前缀列表(ls, pwd, git status等)
     - 其他工具：触发用户批准对话

### 路径检查

**文件**: [internal/runner/approval_test.go](../internal/runner/approval_test.go)

- `isWithinWorkpath()`: filepath.Abs → filepath.Rel → 禁止 `..` 逃逸

### 中间件集成

**文件**: [internal/agent/middleware.go](../internal/agent/middleware.go)

- 在代理执行工具前拦截
- 拒绝时返回特定错误信息防止规避

---

## Claude Code 实现分析

### 多模式架构

**5种许可模式**:
| 模式 | 用途 |
|------|------|
| `default` | 标准交互式批准 |
| `plan` | 计划模式（暂停） |
| `acceptEdits` | 自动接受编辑 |
| `bypassPermissions` | 紧急模式 |
| `dontAsk` | 全部拒绝 |

### 三层规则系统

- `allow` / `deny` / `ask` 行为
- 规则来源: userSettings / projectSettings / localSettings / session / cliArg
- 支持 MCP 服务器级别权限: `mcp__server1__tool1`

### 持久化权限

```typescript
destination:
  'userSettings'    // ~/.claude-code/config.json
  'projectSettings' // project/.claude-code/settings.json
  'localSettings'   // .gitignored
  'session'         // 当前会话
```

### 分类器自动批准

- Bash命令分类器（LLM驱动）
- 支持 prompt 规则描述
- 高置信度自动批准

### 政策限制

- 组织级策略API + 1小时轮询
- ETag缓存 + 指数退避重试
- 故障开放策略

### 权限队列与并行处理

```
ToolUseConfirm 队列
→ 并行运行 hooks + classifier
→ 竞速用户交互
→ Promise.race([hookResults, classifierResults, userInput])
```

---

## 差异对比表

| 特性 | jCode | Claude Code |
|------|-------|------------|
| **模式数量** | 2个 | 5个 |
| **规则类型** | 前缀白名单 | 3层(allow/deny/ask) + 正则 |
| **规则来源** | 静态列表 | 5源 + 策略 |
| **持久化** | 无 | 完整设置系统 |
| **路径检查** | workpath | 额外工作目录集合 |
| **安全前缀** | 8个硬编码 | 分类器 + 命令分析 |
| **MCP支持** | 无 | 服务器级/工具级权限 |
| **批准队列** | 同步对话 | 异步队列 + 并行处理 |
| **分类器** | 无 | LLM驱动 Bash分类 |
| **政策限制** | 无 | 组织级策略API |
| **遥测** | 基础 | 详细分析事件 + OTel |

---

## 改进建议

1. **实现持久化规则存储** — `~/.jcoding/permissions.toml`
2. **扩展为三层规则系统** — allow/deny/ask + 正则匹配
3. **支持MCP权限** — mcp__serverName__toolName 模式
4. **权限钩子接口** — 可扩展的审批逻辑
5. **异步批准队列** — 并行处理 + 分类器加速
6. **添加政策限制** — 组织级配置API
