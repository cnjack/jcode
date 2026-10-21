# Agent Plugins 兼容客户端 — 需求分析与技术设计

> 状态：设计稿（未实现）
> 依据规范：Agent Plugins Specification v1.0.0（https://agent-plugins.org/specification）
> 目标：让 jcode 成为 https://agent-plugins.org/compatible-clients 认可的兼容客户端

---

## 1. 背景与目标

Agent Plugins 是一个厂商中立的开放标准（TSC 含 Amazon、Cursor、Microsoft、OpenAI、Vercel 的核心维护者），定义了**可移植插件包格式**：一个插件 = 一个目录，内含必需清单 `plugin.json` + 固定位置的组件（v1 仅两种组件：**Agent Skills** 和 **MCP servers**）。规范只约束包结构、校验、发现、MCP 配置、插件变量与失败隔离；**安装来源、registry/marketplace、启停 UX、权限与沙箱均为 client 自定义行为**。

jcode 现状与该规范天然契合：

| 规范概念 | jcode 现状 |
| --- | --- |
| Agent Skills（`skills/<name>/SKILL.md`） | `internal/skills` Loader，已有 builtin → `~/.agents/skills` → `~/.jcode/skills` → project 覆盖链 |
| MCP servers（stdio / streamable-http / sse） | `internal/tools/mcp*.go` 完整 MCP 客户端（stdio + HTTP + OAuth），配置在 `config.json` 的 `mcp_servers` 和项目根 `mcp.json` |
| 启停管理 UI | Web Settings 已有 `mcp` / `skills` tab |

**目标**：以最小增量让 jcode 满足规范 §11 客户端一致性要求（skills + MCP 双组件、stdio + streamable-http 双传输），并提供插件的安装/启停/卸载体验（Web UI 为主，参考 GitHub Copilot 的 Plugins 设置页）。

**非目标（v1）**：
- 不做中心化 registry / 插件发布平台（规范明确不涉及）。
- 不支持规范外组件类型（commands、hooks、agents、rules —— 规范 v1 明确排除，留待格式收敛）。
- 不做插件沙箱（规范不要求；以"安装前信任确认 + 路径 containment 校验"代替）。

## 2. 需求分析

### 2.1 用户故事

1. 作为用户，我可以**从本地目录安装**一个 Agent Plugin（开发/调试场景）。
2. 作为用户，我可以**从 git 仓库 URL 安装**插件（团队/社区分发场景）。
3. 作为用户，我可以**添加一个 marketplace 源**（一个包含 `marketplace.json` 的 git 仓库或 URL，列出多个插件），浏览并按需安装（参考 Copilot 截图中的 `copilot-plugins` 分组）。
4. 作为用户，我可以在 Settings → Plugins 中**查看每个插件提供的组件**（skills ×N、MCP servers ×N）、**启用/停用**、**卸载**。
5. 作为用户，安装含 stdio MCP server 的插件时，我会看到**信任提示**（插件可启动本地进程）。
6. 作为 agent，安装启用后插件里的 skills 自动出现在系统提示和 `load_skill` 工具中，MCP tools 自动可用。

### 2.2 功能性需求（对齐规范 §11 + Appendix A 一致性清单）

**P0 — 插件加载器（新包 `internal/plugin/`）**

- F1. 从目录路径加载插件：建立 filesystem-resolved 插件根；读取并校验 `plugin.json`。
- F2. Manifest 校验（§5）：
  - 必需 `$schema` = `https://agent-plugins.org/schemas/1.0.0/plugin.schema.json`（本地识别，**禁止联网拉取 schema**）；
  - 必需 `name`，且满足 §5.5 命名约束（1–64 字符、`a-z0-9-.`、首尾字母数字、无连续 `--`/`..`）；
  - 封闭 schema：未知顶层字段**报告并忽略**（非致命）；其它 schema 违例 → 拒绝整个插件；
  - `extensions` 非对象 → 报告并忽略；未实现的 namespace 不校验其值。
- F3. 组件发现（§6/§7）：固定位置 `skills/`（仅直接子目录、含 `SKILL.md`，不递归）与 `mcp.json`；缺失不视为错误；组件级失败隔离（单个 skill/单个 server 失败不影响其它组件）。
- F4. MCP 配置（§7.2）：
  - `mcp.json` 封闭 union：`type: stdio | streamable-http | sse`；stdio 支持 `command`（单 token，裸名或 `./` 插件相对路径）、`args`、`env`、`cwd`；http/sse 支持 `url`（非 loopback 必须 HTTPS）、`headers`（不展开占位符）；
  - 占位符扩展（§9.2）：仅在 `args`/`env`/`cwd` 中做 `${PLUGIN_ROOT}`、`${PLUGIN_DATA}` 单次非递归替换；`command`/URL/header 不展开；`env` 禁止含 `PLUGIN_ROOT`/`PLUGIN_DATA` 键；
  - 子进程环境（§9.1）：注入 `PLUGIN_ROOT`（插件根绝对路径）与 `PLUGIN_DATA`（client 管理的每插件持久目录，先于启动创建、可写、跨更新保留）；
  - 与 jcode 原生 `config.MCPServer` 的**转换层**（见 §3.3）。
- F5. 路径 containment（§4.1）：插件内任何路径解析后不得逃逸插件根（符号链接等）；`./` 前缀强制；违例按最窄失败边界处理。
- F6. Skills 集成：插件 skills 进入 `skills.Loader`，新增 `Source = "plugin:<pluginName>"`，参与描述注入、slash 命令、`load_skill`；可被现有 disabled 机制停用。
- F7. MCP 集成：插件 MCP servers 合并进 MCP 管理器，工具名以插件名做命名空间隔离（如 `mcp_<plugin>_<server>_<tool>`），在 Web MCP tab 中以 provenance 标记展示（现有 global/project provenance 机制扩展）。

**P1 — 安装与生命周期（client 自定义行为）**

- F8. 安装来源：本地目录（复制或原地引用）、git URL（clone 到 `~/.jcode/plugins/`）。
- F9. Marketplace 源：用户可添加 marketplace（git 仓库或 URL，根含 `marketplace.json`，列出插件名、描述、来源 URL）；UI 按 marketplace 分组展示，逐个 Install。
- F10. 生命周期：install / enable / disable / uninstall / update（git pull 或按 `version` 字段提示）；卸载时删除插件目录，可选删除 `PLUGIN_DATA`。
- F11. 信任确认：安装含 stdio MCP server 的插件前，UI 明确提示"该插件可在本机启动进程"，需用户确认。

**P2 — 界面（严格复用 jcode Settings 现有设计语言，不引入新范式）**

- F12. Web Settings 左侧 section rail 新增 **Plugins** tab（heroicons `PuzzlePieceIcon`，位置在 Skills 与 Memory 之间），内容区沿用 `max-w-3xl` inset panel。
- F13. 页面结构完全对齐现有 MCPTab/SkillsTab 模式：
  - 头部：`SECTION_TITLE` + 计数（mono 11 muted），右侧 `BTN_SECONDARY BTN_SM`「Install plugin」按钮；
  - 说明行：11px muted 文本，内嵌 warning 色 "Only install plugins from sources you trust." 与 `Learn more ↗` 链接（指向 agent-plugins.org）；
  - 过滤行：现有 `Segmented` atom（All / Enabled / Disabled）+ `INPUT` 搜索框；
  - 插件行：现有 `ROW`/`CHIP`/`CHIP_ACCENT`/`Switch` atoms + MCP row 的 iconbox 模式（`h-7 w-7` rounded-md + PuzzlePieceIcon）；行内依次是 名称 + 版本 chip + 组件 chips（`skills 3`/`mcp 1`）+ 来源 chip（marketplace 名或 `git`）+ 状态点文本（`● enabled`，10px）；副标题为 mono 11 muted 的安装路径或描述；disabled 行用 `data-muted` 降透明度（现有模式）。
- F14. 详情**行内展开**（对齐 MCP OAuth 展开行的 `mt-2 pl-10` 语言，不用抽屉/弹窗）：SKILLS / MCP SERVERS 小标题（10px uppercase tracking-wide）+ mono 11 组件清单（server 带 transport 与连接状态点）+ data/repo 路径行 + ghost xs 按钮组（Check for updates / Uninstall）。
- F15. 诊断（被跳过的 server/skill）：行标题区显示 `CHIP` warning（`1 server skipped`），展开详情内用 10.5px warning-fg 文本 + 警告图标说明，与 MCP row 的 error 文本模式一致。
- F16. Install 用**内联表单视图**（复用 MCP edit form 卡片：muted header + `Field` + fc-foot Cancel/Save），不用模态框；来源用 `Segmented`（Local folder / Git URL / Marketplace）；解析 `plugin.json` 后在表单内渲染信任确认框（accent-border + accent-wash-soft 背景 + 警告图标 + mono 组件摘要），主按钮为「Trust & Install」。
- F17. 空态复用 `atoms.EmptyState`（icon box + 13px 标题 + 11px hint）+「Add marketplace」按钮；Marketplace 源管理为同页 `group-head`（11px uppercase muted + 分隔线）下的普通行（名称 + `6 plugins` chip + mono URL + Refresh/Remove ghost xs 按钮）。
- F18. TUI：v1 仅保证功能可用（配置文件 + 重启生效），`/plugin` 命令列为后续项。

### 2.3 非功能性需求

- **安全**：不信任来源不安装（UI 固定警示文案，同 Copilot）；manifest/mcp.json 严格校验；路径 containment；`env`/`headers` 中禁止密钥的规范要求原样透传到文档。
- **韧性**：任何单组件失败不阻塞会话启动；所有插件加载错误进 `config.Logger()`，UI 展示可读的诊断（规范 §11.3 SHOULD report）。
- **兼容**：不破坏现有 `~/.jcode/skills`、项目 `mcp.json`、`config.json mcp_servers` 行为；插件仅是新的 source。
- **测试**：HOME 隔离（`t.Setenv("HOME", t.TempDir())`）；用规范 Appendix A 清单逐项对应用例。

## 3. 技术设计

### 3.1 包结构

```
internal/plugin/
  manifest.go      # plugin.json 解析 + §5 校验（封闭 schema、name 约束、extensions 容错）
  mcpconfig.go     # mcp.json 解析 + §7.2 校验（封闭 union、URL/header 规则）
  expand.go        # §9.2 占位符单次非递归扩展
  loader.go        # Loader：扫描 ~/.jcode/plugins/*/，聚合 manifest + skills + mcp，失败隔离 + 诊断收集
  install.go       # 安装源：本地目录 / git clone；uninstall/update
  marketplace.go   # marketplace.json 获取与解析（git 或 http）
  plugin_test.go   # 对齐 Appendix A 的用例（含恶意路径、逃逸 symlink、未知字段、版本不匹配）
```

存储布局：

```
~/.jcode/plugins/
  <plugin-name>/           # 插件根（安装副本或 git clone）
  data/<plugin-name>/      # PLUGIN_DATA（跨更新保留，卸载时可选清除）
  plugins.json             # 安装登记：source、enable 状态、marketplace 归属、安装时间、版本
```

`plugins.json` 由 plugin 包独占管理（带文件锁，仿 `config` 的 mutation 模式）；**不写进 config.json**，避免膨胀主配置。

### 3.2 加载流程（对齐 implementer 指南的 loading sequence）

1. 扫描 `~/.jcode/plugins/` 下已登记且 enabled 的插件目录 → `filepath.EvalSymlinks` 建立 resolved root。
2. 读 `plugin.json` → `$schema` 选择本地 1.0.0 校验规则 → 致命违例则拒绝插件并记录诊断。
3. 发现 `skills/`：仅直接子目录、含常规文件 `SKILL.md`、resolved path 不出 root → 生成 `skills.Skill{Source: "plugin:<name>"}` 注入 Loader（复用现有 disabled 集合，key 用 `<plugin>/<skill>` 防重名）。
4. 读 `mcp.json` → 版本需与 plugin.json 一致（§10.1）→ 逐 server 校验并转换为 `config.MCPServer`；单条失败仅跳过该条。
5. stdio server 启动时：`PLUGIN_DATA = ~/.jcode/plugins/data/<name>`（先创建），占位符扩展后 overlay env，再强制写入 `PLUGIN_ROOT`/`PLUGIN_DATA`；默认 cwd = 插件根。
6. 诊断（跳过的 skill/server、被忽略的未知字段、unsupported transport）汇总返回，Web API 暴露给 UI。

### 3.3 与现有 MCP 配置的映射

规范格式与 jcode 原生格式不同，做单向转换层：

| Agent Plugins mcp.json | jcode `config.MCPServer` |
| --- | --- |
| `type: "stdio"`, `command`, `args`, `env`, `cwd` | `Command`, `Args`, `Env`（cwd 由 mcp_manager 启动时设置，需要扩展一个 `WorkDir` 字段或经 env 传递——实现时选最小改动方案） |
| `type: "streamable-http"`, `url`, `headers` | `URL` + `Headers`（现有 HTTP 传输即 streamable-http） |
| `type: "sse"` | `URL` + sse 标记（现有客户端已支持 legacy SSE 回退） |

转换产物标注 `Provenance: "plugin:<name>"`（扩展现有 `Source` 标记机制，config.go L1160 附近已有 global provenance 打标先例）。插件 MCP 配置**不落盘到 config.json**，每次启动由 loader 动态合并。

### 3.4 Web API（`internal/web/plugins.go`）

| 路由 | 说明 |
| --- | --- |
| `GET /api/plugins` | 已安装插件列表（manifest、组件摘要、诊断、enable 状态） |
| `POST /api/plugins/install` | `{source: "local"|"git", path|url}` → 校验 + 信任信息返回/安装 |
| `POST /api/plugins/{name}/enable` `/disable` | 启停（写 plugins.json，触发 skills/MCP 重载） |
| `DELETE /api/plugins/{name}` | 卸载（`?keep_data=true` 可选） |
| `GET /api/plugins/marketplaces` / `POST / DELETE` | marketplace 源管理 |
| `GET /api/plugins/marketplaces/{id}/items` | 浏览某个 marketplace 的插件 |

启停/安装/卸载后触发 `skills.Loader.Rescan` + MCP 重载（复用现有 mcp 热重载链路）。

### 3.5 失败与诊断模型

- Loader 返回 `[]Diagnostic{Plugin, Component, Level, Message}`；致命（manifest 违例）→ 插件整体禁用；组件级 → 跳过该组件。
- UI 在插件行内展示 warning badge，hover/展开看明细。

## 4. UI 设计

原型：`design/plugins-settings.html`（jcode tokens，light/dark，3 个状态，已在浏览器走查）。

**原则：不引入任何新 UI 范式**，全部复用 `SettingsView.tsx`（桌面设置页：section rail + inset 2xl 面板、`max-w-3xl` 内容列、卡片间距 `space-y-5`）与 `settings/atoms.tsx` 的既有语言（ROW / CHIP / Switch / Segmented / INPUT / EmptyState / MCP edit-form 卡片 / `data-muted` 降透明度 / MCP OAuth 行内展开）。界面文案跟随应用 i18n（中文环境示例见原型）。与 Copilot 截图的差异：无独立弹窗、无大卡片分组、无下拉菜单 —— 安装走内联表单，marketplace 分组退化为现有 `偏好设置` 式分组小标题（10px uppercase tracking-wider muted）。

1. **列表态**：rail 新增「插件」tab（`PuzzlePieceIcon`，位于「技能」与「记忆」之间）；内容区 = SECTION_TITLE + 计数 + 右侧「安装插件」`BTN_SECONDARY BTN_SM`、11px muted 说明行（内嵌 warning 色"仅安装来自可信来源的插件" + "了解更多 ↗" 链接）、Segmented 过滤（全部/已启用/已停用）+ 搜索框；插件卡复用 GeneralTab ROW 结构（`h-7 w-7` iconbox + 12px 名称 + chips（版本/技能数/MCP 数）+ 10px 状态点 + 11px muted 描述 + 右侧 Switch）；未安装的市场条目右侧为 `BTN_SECONDARY BTN_XS`「安装」。
2. **行内详情**（对齐 MCP OAuth 展开行 `mt-2 pl-10`）：技能 / MCP 服务器分组小标题 + mono 11 组件清单（服务器带 transport 与连接状态）+ 来源与数据目录 mono 行 + ghost xs 操作（检查更新 / 卸载）；诊断以 warning chip（"1 个服务器已跳过"）+ 展开区 warning-fg 文本呈现。
3. **安装表单态**：内联 form-card（muted 大写 header + Field + Segmented 来源（本地目录/Git URL/插件市场）+ INPUT_MONO + 信任确认框（accent-border + accent-wash-soft + 警告图标 + mono 组件摘要）+ 取消 / 信任并安装）。
4. **空态 + 插件市场管理**：EmptyState（icon box + 标题 + hint）+「添加插件市场」；已添加的市场以普通 ROW 列出（名称 + 插件数 chip + mono URL + 刷新/移除 ghost xs）。

## 5. 分阶段计划

| 阶段 | 内容 | 验收 |
| --- | --- | --- |
| P0 | `internal/plugin` 加载器 + skills/MCP 集成 + `~/.jcode/plugins` 本地目录安装（手动放目录即可用） | Appendix A 清单对应测试全绿；手动放一个示例插件，skills 出现在系统提示、MCP tools 可用 |
| P1 | Web API + Settings Plugins tab（安装/启停/卸载/详情）+ git 安装 + 信任确认 | 原型确认后实现；`make lint` + UI 走查 |
| P2 | Marketplace 源管理 + 浏览/安装 + 更新提示 | e2e：添加示例 marketplace → 安装 → 启用 |
| P3 | 向 agentplugins 组织提交 compatible-clients 登记；site/docs 用户文档；TUI `/plugin` | 官网列表出现 jcode |

每个阶段独立 PR（遵循项目 PR 惯例）。

## 6. 风险与开放问题

1. **`cwd` 支持**：jcode 现有 stdio MCP 启动是否支持自定义工作目录需确认；若不支持，需在 mcp_manager 增加（规范强制：默认 cwd=插件根）。
2. **streamable-http vs sse**：确认现有 HTTP 传输的协商行为，必要时为 `type: "sse"` 固定 legacy 模式。
3. **marketplace.json 格式**：规范不管，需自定义最小格式（name/description/source-url/version）。可参考 Copilot 的 marketplace 结构，但属 jcode 私有约定，文档中明确说明。
4. **skills 重名冲突**：插件 skill 与用户/内置 skill 同名时的优先级——建议插件来源最低（builtin/user/project 可覆盖插件），待确认。
5. **远程环境**：SSH/Docker 环境下插件目录在哪一侧生效（建议 v1 仅 local 支持安装，远程环境只读加载已安装插件的 skills——待确认）。
