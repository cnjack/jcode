# 技能系统增强 — 产品需求文档 (PRD)

## 1. 背景与目标

### 1.1 背景

jcode 当前的技能系统（`internal/skills/`）采用简洁的**两层注入模式**：
- **第一层**：技能描述注入系统提示（低 token 开销）
- **第二层**：通过 `load_skill` 工具按需加载完整内容

技能来源支持三级优先级：内置（embed.FS）→ 用户（`~/.jcoding/skills/`）→ 项目（`.jcoding/skills/`）。

通过与 Claude Code 技能系统的深度对比分析（参见 [analyse/skills.md](../analyse/skills.md)），发现当前实现在以下方面存在显著差距：

| 维度 | 当前状态 | 目标状态 |
|------|---------|---------|
| 执行模式 | 仅内联注入 | 内联 + Fork 隔离执行 |
| frontmatter | 3 个字段 | 10+ 字段 |
| 技能源 | 内置 + 文件系统 | + MCP 动态发现 |
| 动态重载 | 手动 Rescan | fsnotify 自动监听 |
| 遥测 | 无 | 使用频率 + 执行效果 |
| 权限控制 | 无 | allowedTools 精细化 |

### 1.2 目标

构建一个**可扩展、安全、可观测**的技能系统，使得：

1. 技能作者能通过 frontmatter 声明式地控制执行行为（模型、工具权限、上下文隔离）
2. 高复杂度技能在独立子 agent 中执行，失败不影响主对话流
3. MCP 服务器可发布技能，实现跨工具链的技能共享
4. 技能文件变更自动生效，无需重启 CLI
5. 运营团队可通过遥测了解技能使用情况，驱动技能迭代

### 1.3 非目标

- 远程技能市场/注册中心（后续版本）
- 技能版本管理与回滚
- 图形化技能编辑器
- 多语言技能 runtime（仅 markdown 指令）

---

## 2. 用户故事

### US-1：Fork 隔离执行
> 作为一名开发者，我希望 security-review 等重型技能在独立子 agent 中运行，这样即使技能执行超时或失败，我的主对话上下文不会被污染，token 预算也不会被意外耗尽。

### US-2：模型覆盖
> 作为技能作者，我希望为代码生成类技能指定使用高能力模型（如 Claude Opus），而简单查询类技能使用低成本模型（如 GPT-4o-mini），以优化整体使用成本。

### US-3：工具权限限制
> 作为团队管理员，我希望 PR review 技能只能使用 `read`、`grep`、`execute(gh)` 工具，不能调用 `write` 或 `edit`，防止外部 SKILL.md 注入恶意写入指令。

### US-4：MCP 技能发现
> 作为一名平台工程师，我已有 MCP 服务器提供数据库管理工具。我希望这些工具能自动作为技能出现在 jcode 中，无需手动复制 SKILL.md 文件。

### US-5：文件监视热重载
> 作为技能开发者，我希望修改 `~/.jcoding/skills/my-skill/SKILL.md` 后，jcode 能在数秒内自动加载新内容，无需退出重启。

### US-6：执行遥测
> 作为运营人员，我希望查看每个技能的调用次数、平均 token 消耗、成功率，以此决定哪些技能需要优化或推广。

### US-7：Hook 机制
> 作为技能作者，我希望技能执行前自动运行 `git stash`、执行后运行 `git stash pop`，通过声明式的 pre/post hook 实现。

### US-8：模板变量
> 作为技能作者，我希望在 SKILL.md 中使用 `${SKILL_DIR}` 引用技能目录路径，`${SESSION_ID}` 引用当前会话 ID，以便技能产生的临时文件可追溯。

---

## 3. 功能需求

### P0 — 必须实现

#### F-01：Fork 执行模式

- 技能 frontmatter 声明 `context: fork`（默认 `inline`）
- Fork 模式下，`load_skill` 工具委托 `subagent` 工具创建子 agent
- 子 agent 继承当前 `Env`（CloneForSubagent）、但拥有独立消息历史
- 子 agent 使用技能声明的 `allowedTools`（若为空则继承父 agent 全部工具）
- 子 agent 完成后仅返回最终结果文本，中间 tool_call 不回流父 agent
- 超时硬限制：默认 120s，可通过 `timeout` frontmatter 配置

#### F-02：扩展 Frontmatter 解析

新增字段：

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `model` | string | 空（使用全局模型） | 模型标识，如 `gpt-4o`, `claude-sonnet` |
| `allowedTools` | []string | 空（全部工具） | 允许使用的工具名白名单 |
| `context` | string | `inline` | `inline` 或 `fork` |
| `timeout` | int | 120 | Fork 模式超时秒数 |
| `hooks.pre` | string | 空 | 执行前 shell 命令 |
| `hooks.post` | string | 空 | 执行后 shell 命令 |
| `tags` | []string | 空 | 分类标签 |
| `aliases` | []string | 空 | 触发别名 |

向后兼容：仅识别 `name`、`description`、`slash` 的旧技能文件正常工作。

#### F-03：工具权限过滤

- `allowedTools` 非空时，创建子 agent 前过滤工具列表
- 过滤逻辑：`tool.Info().Name` 必须在 `allowedTools` 集合中
- 未匹配的工具不传入子 agent 的 `ToolsConfig`
- 日志记录被过滤的工具名称（debug 级别）

### P1 — 应该实现

#### F-04：MCP 技能加载

- 配置格式：`config.json` 的 `MCPServers` 中标记 `"skills": true` 的服务器
- 启动时通过 MCP `tools/list` 获取工具列表
- 每个 MCP 工具映射为一个 `Skill`：`Name` = tool name, `Description` = tool description
- MCP 技能的 `load_skill` 返回工具描述 + 参数 schema（JSON）
- MCP 技能的执行模式固定为 `fork`（MCP 工具调用在独立 agent 中完成）

#### F-05：fsnotify 文件监视

- 监听目录：`~/.jcoding/skills/`、当前项目 `.jcoding/skills/`
- 事件类型：Create、Write、Remove、Rename
- 防抖：500ms 内多次变更合并为一次 Rescan
- Rescan 完成后通知 TUI 刷新技能列表
- 错误处理：监听失败降级为手动模式，日志记录

#### F-06：执行遥测

- 每次 `load_skill` 调用记录：技能名称、执行模式、开始时间
- 技能执行结束记录：耗时、token 消耗（输入/输出）、成功/失败
- 存储：追加写入 `~/.jcoding/skill_telemetry.jsonl`
- 集成现有 Langfuse tracer：技能执行作为 span 附加到主 trace
- 不阻塞主流程：写入失败仅日志记录

### P2 — 可以实现

#### F-07：Hook 机制

- `hooks.pre` / `hooks.post` 为 shell 命令字符串
- 通过 `Env.Executor.Exec()` 执行，继承当前工作目录
- pre hook 失败时中断技能加载，返回错误信息
- post hook 失败时仅日志警告，不影响结果

#### F-08：模板变量替换

- 在 `GetContent()` 返回前执行变量替换
- 支持变量：`${SKILL_DIR}`（技能目录路径）、`${SESSION_ID}`（会话 UUID）、`${PWD}`（工作目录）
- 内置技能的 `${SKILL_DIR}` 为空字符串

#### F-09：安全加固

- 读取技能文件时检查符号链接深度（最多 3 层）
- 验证技能文件大小（上限 100KB）
- frontmatter `allowedTools` 不能包含 `subagent`（防止递归 fork）

---

## 4. 非功能需求

### 4.1 性能

- 技能加载（全量扫描 + 解析）< 100ms（50 个技能）
- Fork 子 agent 创建开销 < 50ms
- fsnotify 事件响应 < 1s（含防抖）
- 遥测写入不增加主流程延迟

### 4.2 可靠性

- Fork 子 agent 超时/panic 不影响父 agent 运行
- MCP 连接失败降级：技能标记为不可用，不阻塞其他技能
- fsnotify watcher 异常自动重试（最多 3 次，间隔 5s）

### 4.3 兼容性

- 现有 SKILL.md 文件无需修改即可正常工作
- 新 frontmatter 字段忽略未知键
- Go 1.21+，无 CGO 依赖

### 4.4 可观测性

- 所有技能操作通过 `config.Logger()` 记录（`[skills]` 前缀）
- Langfuse span 包含：技能名称、执行模式、工具列表、token 用量
- 遥测 JSONL 支持外部工具分析

---

## 5. 成功指标

| 指标 | 目标 | 度量方式 |
|------|------|---------|
| Fork 执行隔离率 | 重型技能 100% 使用 Fork | 遥测 JSONL 统计 |
| 技能热重载延迟 | P99 < 2s | fsnotify 事件到 Rescan 完成时间 |
| MCP 技能发现成功率 | > 95%（网络可达时） | MCP 连接状态统计 |
| 向后兼容性 | 0 个现有技能文件需要修改 | 升级后回归测试 |
| 遥测数据覆盖率 | 100% 的 load_skill 调用被记录 | JSONL 条目 vs 调用次数对比 |
| frontmatter 解析正确率 | 100%（含向后兼容场景） | 单元测试覆盖 |
