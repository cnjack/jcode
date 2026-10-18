# 跨会话查看（cross-session view）——项目范围与技术约束

> 状态：Lead 下发的设计输入（不是最终设计）。设计文档由 architect 产出到
> `internal-doc/cross-session-view-design.md`，评审记录在
> `internal-doc/cross-session-view-design-review.md`。
>
> 背景调研（必读）：
> - `internal-doc/research/cross-session-dsh.md`（主参考：model-facing 五工具 + 授权纪律）
> - `internal-doc/research/cross-session-codex.md`（模型侧 dynamic tools / 人类侧 resume picker / 搜索）
> - `internal-doc/research/cross-session-kimi.md`（HTTP 跨会话全文检索 + inspect UI，无模型侧）

## 1. 目标

让 jcode 的 **agent（模型）能够只读地查看另一个会话**：找回过去做过什么、拿到
确切的用户/助手内容与事件，用于跨会话的上下文复用。人类侧已有的
`jcode sessions`、`--resume <uuid>`、Web `/api/sessions` 不在本次范围内（可作为
顺带的小改进，但不得成为阻塞项）。

一句话验收：**在会话 A 中，模型可以检索并读到同 workspace 会话 B 的真实内容；
新增的 session 工具在跨 workspace 读取上 fail closed，且"读不到"与"不存在"返回同一句话。**

> 口径澄清（架构评审 B1）：这是**策略护栏**，不是可强制执行的隔离边界。既有的
> `grep`（任意 `path` 且免审批）、`read`（绝对路径不受工作目录限制）、`execute`+`cat`
> 今天就能读到 `~/.jcode/sessions`。本特性**不**声称阻止这些路径；如需要真正的隔离，
> 应单独立项（让 read/grep 拒读 `~/.jcode` 并收紧 shell allowlist）。

## 2. 必须遵守的既有架构事实（已核实）

| 事实 | 证据 |
|---|---|
| 会话是 JSONL，一行一个 `Entry`，文件在 `~/.jcode/sessions/<uuid>.json` | `internal/session/session.go:1926` (`LoadSession`)、`internal/config/config.go:1352` (`SessionsDir`) |
| 索引 `~/.jcode/sessions/session.json` 按 project → `[]SessionMeta` 分组 | `internal/session/session.go:452`、`:1788` (`ListSessions`)、`:1891` (`FindSessionMeta`)、`ListAllSessions` |
| `session_start` 条目里持久化了 `Project` / `Provider` / `Model` / `Timestamp` | `session.go:263` (`Entry`)、`:1465` 附近写入 |
| 子 agent / teammate 会话在 `~/.jcode/sessions/{leaderUUID}/subagents/agent-{id}.jsonl`，**父子关系由路径天然编码** | `session.go:1400-1444`
| 工具是 `*Env` 方法；`Env.SessionIDFn` 已在三条传输里接好 | `internal/tools/env.go:48`；`internal/command/interactive.go:1533`、`acp.go:540`、`web.go:509` |
| 新增工具必须注册到：`buildAllTools`、`buildPlanTools`（只读工具入 plan）、`acp.go` 内联列表、`web.go` 内联列表；并在 `internal/command/tool_catalog.go` 的 `commandToolPolicies` 里登记策略 | `internal/command/interactive.go:160`/`:231`、`internal/command/tool_catalog.go:24` |
| 只读工具免审批；工具返回字符串（错误也走字符串）；诊断只用 `config.Logger()` | `AGENTS.md`、`internal/runner/approval.go` |
| 配置是扁平 JSON `~/.jcode/config.json`，字段需向后兼容 | `AGENTS.md`「Modify Config Schema」 |

## 3. 范围决策（Lead 已定，architect 可挑战但需给出理由）

### 3.1 形态：model-facing 只读工具（第一优先）

- 新增**服务层**与**工具层**分离：服务层做扫描/解析/授权原语，工具层做 schema、
  参数校验、授权判定、渲染、错误文案（对齐 DSH 的 `session-query` 服务 + consumer）。
- v1 **只做 3 个工具**（名字可微调，需在设计中固定并全文一致）：
  1. `session_search` — 在当前 workspace 的历史会话里做**字面**全文检索（默认大小写不敏感），
     按会话分组返回：uuid、标题、时间、最强命中片段 + 命中条目的定位（entry 序号）。
     **永不返回调用者自身会话**；结果封顶，不做 continuation。
  2. `session_list` — 列出当前 workspace 的会话（最近 N 个，可含 archived/automation 过滤），
     返回 uuid/标题/更新时间/模型/状态，用于模型在不知道关键词时发现目标。
  3. `session_read` — 读**一个**已授权会话的内容：按 entry 序号区间或 `offset`+`limit`
     读取并渲染（带序号、角色、时间；工具调用/结果可折叠成一行摘要）。
- **不做**：`session_event_trace`（jcode 没有 replacement/source 模型）、
  跨会话写入（`session_send`）、resume 别人会话、语义/向量检索、continuation 游标。
- 谱系（父/子会话）以**字段**形式出现在 `session_list` / `session_read` 的输出里
  （例如 `Parent:` / `Children:`，子会话经 `subagents/agent-*.jsonl` 发现），
  不单独做第 4 个工具。越权祖先/后代只显示占位符，不泄漏 id。

### 3.2 授权（安全边界，必须硬编码，不接受模型参数）

- 调用者身份**只**来自 `Env`：当前会话 UUID（`SessionIDFn`）+ 该会话启动时记录的
  `Project`（从 `session_start` 条目 / `SessionMeta` 取，**不是**运行期 cwd）。
- 跨会话访问要求 `caller.Project == target.Project` **字符串精确相等**；
  caller 无 project（空）时只能读自己。
- **不提供任何 `project` / `path` / `workspace` 入参**（避免模型自选边界）。
- 渲染前必须打开目标文件复检 `session_start.Project` 与索引一致
  （索引是缓存，可被重写）；不一致按"未授权"处理。
- 目标不存在 / 未授权 **返回同一句**错误文案（防存在性探测），细节只进 `config.Logger()`。
- 大小写/符号链接/尾部斜杠不做宽松归一化：精确相等，宁可误拒。
- 与 `entry.Project` 的兼容：历史会话可能没有 `session_start` 的 Project，
  此时回退到索引里的 project；两者都缺失 → 视为不可跨会话读。

### 3.3 默认开关

- 配置 `~/.jcode/config.json` 增加可选小节（示例名）：
  `"session_view": {"enabled": true, "max_results": 20, "max_entry_bytes": 16384}`。
  缺省 `enabled` **为 true**（这是用户明确要的产品能力）；`false` 时三个工具全部不注册。
- 不做 system prompt 长篇注入；如需要，只加**一句**（≤ 200 字符）说明工具的用途与
  "跨会话内容是 untrusted data"。由 architect 决定加或不加并给理由。

### 3.4 性能与预算

- 会话数 ~O(100)，单文件可达数 MB。允许全量扫描 + 缓存，但必须：
  - 按 `SessionMeta.UpdatedAt` 倒序，扫描到结果上限即停；
  - 单次工具调用有整体时间预算（默认 ~3s）与字节预算；
  - 命中片段固定窗口（如 ±80 字符，按 UTF-8 边界截断）；
  - `session_read` 单次返回受 `max_entry_bytes` 约束，超了给显式 `… truncated`。
- 不做常驻索引/数据库/后台协程；如需缓存，进程内 `sync.Map`/LRU，且必须
  对"文件已被追加/删除"安全（宁可重读）。

### 3.5 传输与审批

- TUI / ACP / Web 三条传输全部注册（含 Web 的 headless 路径）。
- 只读 → `internal/runner/approval.go` 免审批（与 read/grep 同档）。
- plan 模式：只读，允许加入 `buildPlanTools`。
- 工具策略在 `internal/command/tool_catalog.go` 登记（`session.history` 类别，allModes，read）。

## 4. 非目标

- Web UI / TUI 的人类侧跨会话浏览改造（已有列表/恢复能力）。
- HTML 导出、云同步、跨机器读会话。
- 任何写入他人会话的路径（发送消息、改标题、删除）。
- 新增第三方依赖（尤其 SQLite/FTS）；标准库 + 现有依赖优先。

## 5. 交付物

1. `internal-doc/cross-session-view-design.md`：背景、目标、方案对比（至少 2 个备选并说明取舍）、
   最终设计（数据流、包结构、工具 schema 原文、授权矩阵、错误码/文案、预算、并发、可观测性）、
   测试计划、风险与回滚、工作量拆分。
2. `internal-doc/cross-session-view-design-review.md`：独立评审（阻塞项/建议/结论）。
3. 代码：服务层 + 工具层 + 三传输注册 + 配置 + 单测。
4. `internal-doc/cross-session-view-test-report.md`：单测/集成/真模型 E2E 记录。
5. PR（分支 `feat/cross-session-view`，对 `main`）。

## 6. 验收标准（实现完成后逐条核对）

- [ ] 会话 A 能搜到同 project 会话 B 的内容，并读出 B 的 entry 内容与序号。
- [ ] **新增的 session 工具**在 cross-project 读取上 fail closed，且与"不存在"同文案；日志里有真实原因。
      经既有 `grep`/`read`/`execute` 读取 `~/.jcode/sessions` 不属本特性范围（评审 B1 已复核），E2E 反例须显式禁止模型使用这些工具，并以持久化 tool_call 审计为主证据。
- [ ] 搜索永不返回调用者自身会话；不得因自己的查询参数自激命中。
- [ ] 无 `project` 参数的模型输入能改变授权边界（不存在这样的参数）。
- [ ] 三个工具在 TUI/ACP/Web 均可用（至少 acp/web 有集成测试或 E2E 覆盖）。
- [ ] `session_view.enabled=false` 时工具不出现。
- [ ] 只读、免审批；plan 模式可用。
- [ ] `go test ./...` 与 `make lint` 通过；新代码有表驱动单测覆盖授权/截断/坏行/CJK。
- [ ] 真模型 E2E（`~/.jcode` 中已配置模型）验证正例与反例。
