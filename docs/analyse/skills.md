# jcode (Go) vs Claude Code (JS/TS) — Skills System 深度对比

## 概述

两个系统都实现了**分层技能加载模式**，但在架构、功能丰富度和扩展能力上存在显著差异：

- **jcode (Go)**：简洁的嵌入式资源 + 用户/项目技能扫描，两层注入模式（摘要→完整）
- **Claude Code (JS/TS)**：多源加载（捆绑/文件/MCP/远程），支持沙箱执行、动态改进、细粒度权限控制

---

## jcode 实现分析

### 核心架构

**文件**: [internal/skills/skills.go](../internal/skills/skills.go)

#### 技能结构体 (L16-24)
```go
type Skill struct {
    Name        string
    Description string
    Slash       string  // 可选的斜杠触发 e.g. "/review-pr"
    Body        string  // 完整markdown内容(第2层)
    Builtin     bool
    Path        string
}
```

#### 加载器模式 (L26-52)
- **嵌入式资源**: `builtinFS.ReadDir("builtin")` 读取编译时的资源
- **用户技能**: `~/.jcoding/skills/` 目录扫描
- **项目技能**: `<projectDir>/.jcoding/skills/` 优先级最高
- **多源合并**: 同名技能时，项目 > 用户 > 内置

#### Frontmatter 解析 (L199-235)
- 支持YAML风格的前置元数据：`name`, `description`, `slash`
- 仅支持简单的 `key: value` 格式

### 技能工具实现

**文件**: [internal/skills/tool.go](../internal/skills/tool.go) (L1-56)

- **一个工具**: `load_skill` — 只负责延迟加载完整内容
- **无执行隔离**: 内容直接注入系统提示
- **无权限控制**: 所有已加载的技能对主代理可见

### 内置技能
- **review-pr**: 使用gh CLI进行PR评审
- **security-review**: 三阶段安全分析法

---

## Claude Code 实现分析

### 捆绑技能系统

**文件**: `src/skills/bundledSkills.ts`

```typescript
export type BundledSkillDefinition = {
  name: string
  description: string
  aliases?: string[]
  whenToUse?: string
  allowedTools?: string[]        // 精细化权限
  model?: string                 // 模型覆盖
  disableModelInvocation?: boolean
  isEnabled?: () => boolean      // 条件启用
  hooks?: HooksSettings          // Hook机制
  context?: 'inline' | 'fork'   // 执行隔离
  agent?: string                 // Agent选择
  files?: Record<string, string> // 参考文件
  getPromptForCommand: (args, context) => ContentBlockParam[]
}
```

**安全特性**: O_NOFOLLOW | O_EXCL 标志防止符号链接攻击，nonce防预创建攻击

### 文件系统加载

**文件**: `src/skills/loadSkillsDir.ts`

**frontmatter 字段**: 15+ 字段（including model, hooks, effort, agent, paths, context）

**参数替换**: `${CLAUDE_SKILL_DIR}`, `${CLAUDE_SESSION_ID}` 模板变量

**去重机制**: 根据 symlink 解析的规范路径

### MCP 技能集成

**文件**: `src/skills/mcpSkillBuilders.ts`

写一次注册模式，避免循环依赖

### Skill Tool — 两种执行模式

**文件**: `src/tools/SkillTool/SkillTool.ts`

**内联执行** (默认): 技能内容直接注入系统提示

**Fork执行**: 子agent获得独立的令牌预算、权限上下文、消息历史
```typescript
for await (const message of runAgent({
    agentDefinition,
    promptMessages,
    model: command.model,      // Per-skill model override
}))
```

### 动态技能改进

**文件**: `src/hooks/useSkillImprovementSurvey.ts`

用户反馈驱动的技能自动改进

### 动态重载

**文件**: `src/hooks/useSkillsChange.ts`

文件系统变化自动触发（inotify），无需重启

---

## 差异对比表

| **维度** | **jcode (Go)** | **Claude Code (JS/TS)** |
|---------|------|------|
| **技能源** | 内置 + 用户 + 项目 | 内置 + 文件 + MCP + 远程 |
| **frontmatter 字段** | name, description, slash | 15+（model, hooks, effort, agent, paths, context） |
| **执行模式** | 仅内联（系统提示注入） | 内联 + Fork（子agent隔离） |
| **权限控制** | 无 | allowedTools（精细化权限） |
| **模型选择** | 单一模型 | per-skill覆盖 + agent选择 |
| **参数处理** | 无 | 模板替换 + shell命令执行 |
| **Hook机制** | 无 | HooksSettings |
| **去重** | 简单覆盖 | Symlink规范路径解析 |
| **文件安全** | 信任文件系统 | O_NOFOLLOW, nonce防护 |
| **动态重载** | 手动 ScanProjectSkills | 自动 skillChangeDetector |
| **遥测** | 无 | 完整事件记录 |
| **缓存策略** | 内存缓存 | 多层（磁盘 + memoization） |

---

## Claude Code 优势分析

### 1. 沙箱执行隔离
- Fork模式使失败隔离不影响主流程
- 独立的令牌预算

### 2. 多模型支持
- 每个技能可指定使用不同模型（适合成本优化）

### 3. 动态发现
- MCP技能集成
- 远程技能加载（实验性）

### 4. 安全防护
- 符号链接攻击防护
- 进程级别随机nonce

---

## 改进建议

1. **实现 Fork 执行模式** — 子agent隔离执行技能
2. **扩展 frontmatter** — 支持 model, allowedTools, hooks, context 字段
3. **添加 MCP 技能加载** — 从 MCP 服务器动态发现技能
4. **实现文件监视** — fsnotify 监听技能目录变化
5. **添加执行遥测** — 记录技能使用频率和效果
6. **安全加固** — O_NOFOLLOW 防止符号链接，权限验证
