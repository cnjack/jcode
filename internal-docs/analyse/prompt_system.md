# 提示系统对比分析：jcode (Go) vs Claude Code (JS/TS)

## 概述

jcode采用静态Go模板渲染系统提示，Claude Code采用异步动态构建+多层缓存+自动压缩的企业级方案。

---

## jcode 实现分析

### 系统提示生成

**文件**: [internal/prompts/prompts.go](../internal/prompts/prompts.go)

- 基于 Go `text/template` + `//go:embed system.md`
- 模板变量: Platform, Pwd, Date, EnvLabel, SSHAliases, GitBranch, GitDirty, LastCommit, ProjectType, DirTree, SkillDescriptions
- AGENTS.md注入: 项目根目录下自动加载

### 计划模式提示

**文件**: [internal/prompts/plan.md](../internal/prompts/plan.md)

- 单独的只读模式系统提示，禁止修改操作

### 提醒系统

**文件**: [internal/prompts/reminders.go](../internal/prompts/reminders.go)

| 提醒名称 | 触发条件 |
|---------|--------|
| `plan_execution` | PlanContent不为空 |
| `todo_check` | 有未完成todo且迭代>5 |
| `token_warning` | 60%-85%上下文已用 |
| `token_critical` | 85%+上下文已用 |
| `tool_error_streak` | ≥2个连续工具错误 |

### 提醒中间件

**文件**: [internal/agent/reminder.go](../internal/agent/reminder.go)

- 每次模型调用前执行
- 以系统消息方式注入提醒

---

## Claude Code 实现分析

### 上下文构建系统

**文件**: `src/context.ts`

- 异步并行加载: `Promise.all([getGitStatus(), getMemoryFiles(), ...])`
- Memoization缓存
- Git快照（防止mid-conversation变化）

### CLAUDE.md 多层次加载

1. 管理内存: `/etc/claude-code/CLAUDE.md`
2. 用户内存: `~/.claude/CLAUDE.md`
3. 项目内存: `CLAUDE.md`, `.claude/CLAUDE.md`, `.claude/rules/*.md`
4. 本地内存: `CLAUDE.local.md`

**@include 指令**: 支持相对/绝对路径引用，循环引用防护

### 系统提示分块与缓存

**文件**: `src/utils/api.ts`

**三种缓存模式**:
- MCP工具存在时: 3块(attribution + prefix + rest, org scope)
- 全局缓存模式: 4块(global scope跨会话复用)
- 默认模式: 3块(org scope)

**边界标记**: 区分静态(跨会话复用)和动态(每会话变化)内容

### 自动压缩系统

**文件**: `src/services/compact/autoCompact.ts`

```typescript
AUTOCOMPACT_BUFFER_TOKENS = 13_000
WARNING_THRESHOLD_BUFFER_TOKENS = 20_000
ERROR_THRESHOLD_BUFFER_TOKENS = 20_000
```

**断路器**: 3次连续压缩失败后停止

### 对话压缩

**文件**: `src/services/compact/compact.ts`

**详细分析框架**:
1. Primary Request and Intent
2. Key Technical Concepts
3. Files and Code Sections
4. Errors and fixes
5. Problem Solving
6. All user messages (critical!)
7. Pending Tasks
8. Current Work
9. Optional Next Step

### 提示生成建议

**文件**: `src/services/PromptSuggestion/promptSuggestion.ts`

- 基于推断的用户意图
- 多条件抑制(早期会话、错误、计划模式等)

---

## 差异对比表

| 功能维度 | jcode (Go) | Claude Code (JS/TS) |
|--------|-----------|-------------------|
| **系统提示生成** | 静态text/template | 动态异步构建 |
| **缓存策略** | 无 | 分块缓存(global/org) |
| **内存系统** | AGENTS.md单文件 | CLAUDE.md多层次+@include |
| **压缩策略** | 无 | 多模式compression |
| **提示建议** | 无 | 自适应生成 |
| **令牌管理** | 基础警告 | 多阈值预警+断路器 |
| **Git集成** | 基础字段 | 完整快照 |

---

## 改进建议

1. **实现上下文压缩** — 检测 token 接近上限时自动摘要
2. **多层次内存** — 支持全局/项目/本地三级配置
3. **分块缓存** — 静态内容跨会话复用
4. **@include 指令** — 支持跨文件引用
5. **提示建议** — 基于上下文智能推荐
6. **异步加载** — 并行获取git/环境信息
