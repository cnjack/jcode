# JCode Artifacts PRD（Web / Desktop）

- 状态：Draft
- 目标版本：Phase 1 MVP + Phase 3 Cloud sharing
- 适用端：Web、Desktop
- 明确不适用：CLI/TUI、ACP
- 相关设计：`internal-doc/artifacts-design.md`
- 相关评审：`internal-doc/artifacts-prd-review.md`
- UI 原型与评审：`internal-doc/artifacts-ui/index.html`、`internal-doc/artifacts-ui/design-notes.md`

## 1. 背景

JCode 已经具备文件读写、代码修改、命令执行、Web 会话、Tauri Desktop、右侧 Files / Changes / Plan 面板，以及会话 JSONL 记录能力。但当 Agent 生成一个可直接消费的结果，例如 HTML 报告、Markdown 文档、图片、PDF 或 CSV 时，用户仍需要从对话中找到路径，再进入 Files 面板手动定位和打开。

OpenWorker 已经验证了 Artifact 交互的价值：Agent 在最终回复中提供 `artifact:` 链接，右侧栏列出工作区中可预览文件，并根据类型打开 HTML、Markdown、图片、PDF、CSV 等 Viewer。它的不足也很明确：Artifact 列表来自对整个工作区的后缀扫描，因此“产物”和“普通文件”没有真正的领域边界，也无法可靠表达某个会话明确交付了哪些结果。

OpenHands 的 `canvas_ui_control.show_preview(path)` 则提供了另一个有价值的方向：Agent 可以显式要求客户端展示一个文件，而不是只把文件路径写进文本。

JCode 应结合两者：

1. 采用 OpenHands 式的显式 `show_artifact` 工具，让 Agent 能把结果交付给界面。
2. 采用 OpenWorker 式的右侧 Artifact Viewer 和多格式预览。
3. 不扫描整个工作区，把 Artifact 建模为“会话显式登记的工作区文件”。
4. 利用 JCode 的 Web/Desktop 共用 React UI、会话回放和 Tauri 原生能力，提供 Web 预览与 Desktop 打开/定位文件。

## 2. 问题定义

当前交付链路存在四个断点：

- Agent 知道哪个文件是最终结果，界面不知道。
- Files 面板展示整个工作区，用户难以区分“源文件”和“交付物”。
- 对话中的路径只是文本，不能稳定触发预览或在会话恢复后重建产物列表。
- Web 与 Desktop 有可视化承载能力，但 CLI/TUI 和 ACP 没有统一的 Artifact UI 契约，强行暴露工具只会造成不可完成的工具调用。

## 3. 产品定义

Artifact 是由 Agent 或未来的用户操作显式登记、属于某个 JCode 会话、实际内容存放在该会话工作区中的文件。

Artifact 不是：

- 工作区内所有“看起来可预览”的文件；
- 文件内容在会话 JSONL 中的副本；
- 新的云存储或附件系统；
- 代码变更的替代品；
- CLI/ACP 协议中的通用输出类型。

Web 产品中的 `task_id` 与 Recorder 的 session UUID 是同一个标识；文档后续写作 `task_id/session_id`。Artifact 的最小身份由 `task_id/session_id + normalized_relative_path` 决定。同一会话再次展示同一路径时，更新同一个 Artifact，并增加 revision；会话记录保留登记历史，界面默认展示最新登记。

本地 revision 是“显式登记代数”，不是文件内容快照。JSONL 只记录元数据，Viewer 每次都重新校验并读取工作区当前文件；因此文件在未再次登记时发生变化，revision 不会自动增加，但 Viewer 仍会看到当前内容。只有 Phase 3 Cloud 分享会把某个 revision 的内容复制为不可变密文快照。

## 4. 目标

### 4.1 用户目标

- Agent 完成主要产物后，可以主动在右侧打开预览，而不是只回复文件路径。
- 用户能在当前会话中快速查看所有明确交付的产物。
- 刷新页面、重启 Desktop 或恢复历史会话后，Artifact 列表仍然存在。
- Web 可以安全预览常用格式；Desktop 可以进一步用系统默认应用打开文件或在 Finder/Explorer 中定位。
- **[Phase 3]** 在 JCode 已登录 Cloud 时，用户可以显式生成 Artifact 分享链接；未登录时维持纯本地体验，不出现登录阻塞。

### 4.2 平台目标

- Artifact 只在 Web transport 注册；Desktop 复用 Web sidecar，因此自然获得该能力。
- CLI/TUI 与 ACP 的工具列表、提示词和协议均不出现 `show_artifact`。
- 不增加需要用户设置的 `artifact.enable` 开关；能力是否存在由 transport 和运行环境决定。
- 不修改所有 transport 共享的 `AgentEventHandler` 接口。
- 文件内容仍以工作区为唯一事实来源，会话只持久化安全、可回放的元数据。
- `show_artifact` 永远只负责本地交付；Cloud 上传必须由用户显式触发，不能因登录状态自动发生。

## 5. 非目标

MVP 不包含：

- 自动扫描工作区并推断 Artifact；
- 在 CLI/TUI 中做终端内预览；
- 在 ACP 中增加 Artifact notification 或 capability negotiation；
- MVP 阶段跨机器同步或分享 Artifact 文件内容（规划在 Phase 3）；
- Artifact 评论、协同编辑或版本 diff；
- 通用 Office 在线渲染；
- 远程 SSH/Docker 工作区的二进制流式预览；
- 用 Artifact 替代 Files、Changes 或 Plan 面板。

## 6. Surface 范围

| Surface | 是否提供 | 行为 |
| --- | --- | --- |
| Browser Web（`jcode web`） | 是 | 注册工具、显示 Artifacts 面板、浏览器内预览、下载 |
| Tauri Desktop | 是 | 继承 Web 能力，并增加系统默认应用打开、文件管理器定位 |
| CLI/TUI（interactive） | 否 | 不注册工具、不注入说明、不增加 UI |
| ACP | 否 | 不注册工具、不改变 JSON-RPC 协议 |
| Web automation/background run | 是 | 复用 `ToolTransportWeb` 工具计划；可以登记并持久化，automation run 详情提供 Artifacts 入口，非前台任务不得抢占当前面板 |
| Remote workspace | MVP 否 | 不向模型注册工具；后续在有界流式读取完成后启用 |
| JCode Cloud | Phase 3 | 已登录时提供显式 E2EE 分享；未登录时不显示分享动作、不影响本地 Artifact |

## 7. 核心用户故事

### 7.1 生成并预览报告

用户说：“分析这些数据，给我一个交互式 HTML 报告。”

1. Agent 生成并验证 `reports/analysis.html`。
2. Agent 调用 `show_artifact`。
3. 当前 Web/Desktop 会话自动打开 Artifacts 面板并展示报告。
4. Agent 最终回复中同时给出可点击的 Artifact 卡片或链接。

### 7.2 查看会话交付物

用户稍后打开历史会话，Artifacts 标签显示数量。点击后可以看到该会话曾交付的报告、图表和说明文档；文件缺失时保留元数据并显示 missing 状态，下载、分享和原生打开动作禁用，同时提供“重新检测”。恢复文件后用户可重新检测并继续预览。

### 7.3 Desktop 深度使用

用户在 Desktop 预览 PDF 后，选择“在默认应用中打开”或“在 Finder 中显示”。原生层只接受已经由服务端验证过的 Artifact ID，不能由 WebView 直接打开任意路径。

### 7.4 后台任务完成

自动化任务使用与 Web chat 相同的 `ToolTransportWeb` Artifact 工具计划。生成 Artifact 时，系统记录产物并在 automation run 列表和对应 session/task 上显示未查看状态，但不会切换用户正在查看的会话或右侧面板。用户进入 `automation-run` 详情后，通过该页面自己的 Artifacts 入口打开列表和 Viewer；查看后清除该 run 的未读状态。

### 7.5 登录后分享

用户已经登录 JCode Cloud，在 Artifact Viewer 中点击“分享”。JCode 对当前 revision 重新校验，端到端加密后上传到 Cloud 对象存储，并返回一个可复制、可撤销的分享链接。未登录用户看不到该动作；Artifact 仍然可以本地预览、下载和外部打开，系统不会弹登录框，也不会让 Agent 等待。

Artifact 分享是独立、明确的单文件授权。它不自动打开该会话的 Cloud sync，也不上传会话历史、其他 Artifact 或整个 workspace。

## 8. 交互设计

### 8.1 右侧面板

Artifact 使用三层展示结构，后续 UI 原型在不改变该信息架构的前提下探索视觉与交互变体：

1. 对话内工具结果卡：完成时即时发现和一键打开。
2. 右侧 Artifacts tab：会话产物索引、状态和历史登记列表。
3. Expanded / fullscreen Viewer：用于 HTML、PDF、表格和需要大画布的产物；不把 Files/Changes/Plan 一起放大。

在现有 Plan / Files / Changes 后增加 Artifacts：

- 标签显示当前会话的 Artifact 数量；有未查看更新时显示圆点。
- 默认按最近更新时间倒序排列。
- 每项显示标题、相对路径、类型、更新时间和状态。
- 点击列表项打开 Viewer。
- `show_artifact(focus=true)` 来自当前前台会话时，自动打开 Artifacts 面板并选中该项。
- 用户可以关闭面板、返回列表、全屏预览或回到先前面板。
- TopBar 和 DesktopTitlebar 都提供 Artifacts 入口；建议快捷键为 `Shift+Cmd/Ctrl+A`。
- 关闭右栏后，面板总入口仍显示数量/未读点，避免产物失去发现路径。
- 多浏览器标签各自只响应本连接当前 active task 的 `focus=true`；不得切换另一个标签或任务。

Artifacts 列表继续使用当前 RightPanel 的宽度模型。选中产物后可进入 expanded Viewer：默认宽度建议 480px，允许拖拽到视口宽度的 80%，并提供全屏 overlay。Files / Changes / Plan 保持现有宽度规则。

### 8.2 对话中的表现

成功调用 `show_artifact` 后，工具结果渲染为紧凑 Artifact 卡片：标题、类型、路径和“打开”操作。最终回复可以引用已登记的 Artifact，但不要求模型手写特殊 `artifact:` URL。

第一版以工具结果卡片为主，避免仅依赖 Markdown 自定义协议。后续可支持受控链接形式，例如 `jcode-artifact:<artifact_id>`，但不能接受未经登记的任意路径。

### 8.3 文件类型与降级

| 类型 | Web MVP | Desktop MVP |
| --- | --- | --- |
| Markdown | 渲染预览，可查看源码 | 同 Web，可默认应用打开 |
| Text / source code | 等宽文本、语法高亮 | 同 Web，可默认应用打开 |
| HTML | 沙箱 iframe，默认禁止联网 | 同 Web，可外部浏览器打开 |
| PNG/JPEG/WebP/GIF/SVG | 图片预览、缩放 | 同 Web，可默认应用打开 |
| PDF | 内嵌 PDF 预览或浏览器 fallback | 同 Web，可默认应用打开 |
| CSV/TSV | 有界表格预览、原始文本下载 | 同 Web，可默认应用打开 |
| XLSX/Office/其他 | 元数据 + 下载 | 元数据 + 默认应用打开/定位 |

超出内嵌预览大小限制的文件仍可作为 Artifact 登记，但 Viewer 显示“文件过大，无法内嵌预览”，并提供下载或 Desktop 外部打开。unsupported、missing、loading、too-large 和 error 都必须是显式状态，不能退化为空白 Viewer。

### 8.4 Cloud 分享动作（Phase 3）

- UI 以现有 `/api/cloud/status` 的 `logged_in` 为唯一展示门：`false` 时完全隐藏分享动作。
- `logged_in=true` 且 device token 仍有效时，Viewer 提供“分享”按钮；第一次点击才上传当前 revision。分享不依赖 relay online、`auto_connect` 或 session sync。
- 分享成功后提供复制链接、查看过期时间和撤销分享。
- 文件更新为新 revision 后，旧链接继续指向旧快照并明确标记；用户需要再次分享才能生成新链接。
- token 过期或 Cloud 暂时不可达时，只显示可重试错误，绝不影响本地 Artifact 状态。
- 不提供“登录后自动分享”设置，也不让 `show_artifact` 隐式上传。
- 每次分享生成独立的 Artifact Share Secret，字段名统一为 `share_key`；不得简称 ASK，也不得复用账号 CEK 或 Account Sync Key。
- `share_key` 只保存在私有本地 secret store 和分享 URL fragment `#k=v1.<base64url>`。Cloud 数据库、对象存储、HTTP 请求、日志和事件都不能获得该 key。
- 完整分享 URL 不得写入 session JSONL、WebSocket、Cloud event、日志或遥测；Cloud 只返回不含 fragment 的 base URL。
- 分享前锁定当前 revision 和内容摘要；上传期间文件发生变化时以 `artifact_changed` 失败，不得生成半旧半新的对象。

## 9. Agent 工具

工具名：`show_artifact`

建议输入：

```json
{
  "path": "reports/analysis.html",
  "title": "销售分析报告",
  "kind": "auto",
  "focus": true
}
```

- `path`：必填，只接受相对当前任务工作区的文件路径。
- `title`：可选；缺省时使用文件名。
- `kind`：可选提示，默认 `auto`；服务端 MIME 检测拥有最终决定权。
- `focus`：可选，默认 `true`；只对当前前台会话生效。

工具是直接执行、无需审批的 UI 交付工具。它不会创建或修改工作区文件，只验证文件、记录会话元数据并通知界面；审批策略把它加入明确的 auto-approved 工具集合。

工具在 Web normal mode 与 plan mode 都可见；plan mode 只能登记已存在的产物。工具 description 必须包含下面的使用规则。若真实模型测试证明仅靠 schema 不足，可在构建 Web agent 时增加 Web-only prompt fragment，但不得修改共用 prompt。

Agent 使用规则：

- 只在文件已经写完并完成必要验证后调用。
- 用于用户可以直接消费的主要结果，不为每个源码文件、临时文件、日志或构建产物调用。
- 同一路径有实质更新后可以再次调用。
- 普通代码修改继续由 Changes / Files 表达，不应登记为 Artifact。
- 工具成功后，最终回复简短说明产物已经可预览。
- 无论 Cloud 是否登录，工具本身都不上传或分享 Artifact。
- Web 会话运行中切换为 remote 环境后，已有工具调用必须返回可修正的 `artifact preview is not available for remote workspaces yet`；新建 remote agent 时不注册工具。

## 10. 功能需求

### FR-1：显式登记

Web transport 的 Agent 可以调用 `show_artifact` 登记当前工作区中的已有文件。不存在、是目录、越界、符号链接逃逸或不允许的路径必须失败并给出可自我修正的错误。

### FR-2：会话隔离

Artifact 必须属于一个明确的 `task_id/session UUID`（Web 中二者同值）。切换会话后，列表、未读状态和当前选择随之切换；不能看到其他会话登记的路径。

### FR-3：持久化与回放

每次登记写入会话 JSONL 的 Artifact entry。恢复历史会话时从 entry 重建最新 Artifact 索引，不复制文件正文。文件已被删除或工作区不可用时保留元数据并标记 missing。

### FR-4：实时通知

当前前台会话成功登记后，通过已有 WebSocket 通道发送 `artifact_upserted`。界面在 500ms 内更新列表；`focus=true` 时打开 Viewer。后台或非当前会话只更新未查看状态，不能抢焦点。automation run 详情和 reconnect 必须从 session entries/list API 对账，不能依赖 WebSocket 不丢事件。

### FR-5：安全预览

Web 只能通过 Artifact ID 获取经过再次校验的内容，不接受任意绝对路径。每次 content/download/open 都重新验证 session ownership、canonical containment、regular file 和 symlink，Artifact ID 不是永久授权。

安全预览的最低合同：HTML iframe 仅允许 `sandbox="allow-scripts"`，禁止 `allow-same-origin`、form、popup、top navigation 和联网；SVG 只能作为 image document；Markdown 默认禁用 raw HTML；不得把 Artifact HTML/SVG 注入主 React DOM；内容响应必须带 `X-Content-Type-Options: nosniff` 和对应 CSP。`.git/**`、`.jcode/**`、`.env`、private key 等敏感路径必须拒绝，Artifact 不能成为 Files 权限旁路。

### FR-6：Desktop 原生操作

Desktop 为已登记且再次验证的本地 Artifact 提供“默认应用打开”和“在文件管理器中显示”。Web 端不显示不可用的原生操作。

### FR-7：Transport 隔离

CLI/TUI 与 ACP 的工具目录中不存在 `show_artifact`。静态/eager 工具路径依靠各 transport 独立的注册列表隔离；启用 Tool Search 时，catalog transport policy 还必须拒绝 Web 以外的暴露。验收必须同时覆盖 `interactive.go`/`acp.go` 候选工具列表、`tool_catalog` policy 和最终 schema snapshot。

### FR-8：无 enable 配置

不新增用户级或项目级 Artifact enable 开关。Web 本地工作区自动具备能力；Desktop 继承。CLI/TUI、ACP 和 MVP 远程工作区通过注册范围天然不具备能力。

### FR-9：可选 Cloud 分享（Phase 3）

只有 `cloud.status.logged_in=true` 时，Web/Desktop 才显示用户触发的分享操作。分享使用 JCode Cloud 的设备认证与对象存储合同，并以 Artifact revision + 内容摘要创建不可变上传快照。未登录、未开启 session sync、`auto_connect=false` 或 Cloud connector 未常驻，都不能阻止 Artifact 的本地登记和预览；分享不得修改 session sync store。

## 11. 非功能需求

- 预览元数据更新 P95 小于 500ms；大文件内容按需加载，不阻塞 Agent run。
- Artifact 登记必须是幂等操作，同一路径重试不会产生重复列表项。
- 文件大小采用三档合同：文本/HTML/CSV inline 5 MiB，图片/PDF inline 25 MiB；本地 download 250 MiB；Phase 3 Cloud share 25 MiB。超限分别返回 `artifact_too_large` 或 `artifact_share_too_large`，不影响本地登记。
- 不在 JSONL、WebSocket 或日志中写入 Artifact 文件正文。
- 所有诊断使用 `config.Logger()`，不得污染 TUI stdout/stderr。
- API 和前端状态要支持一个会话至少 100 个 Artifact，列表仍保持可用。
- Cloud 分享内容必须端到端加密；Cloud 服务端和对象存储不得获得文件明文或 URL fragment 中的解密密钥。
- 所有新 UI 文案必须走现有 i18n；交互支持键盘、ARIA、focus ring 和 `prefers-reduced-motion`。

## 12. 成功指标

MVP 发布后关注：

- Agent 生成用户可消费文件后成功登记 Artifact 的比例。
- 从 Agent 完成文件到用户首次打开产物的时间。
- `show_artifact` 失败率及主要失败原因。
- 会话恢复后 Artifact 重建成功率。
- 用户从 Artifact Viewer 回退到 Files 手动找文件的比例。

这些指标第一版可先通过结构化日志获得，不要求立即接入新的遥测产品。事件字段至少包括 `artifact_registered`、`artifact_open_latency_ms`、`show_artifact_error_code`、`artifact_missing`，且不记录绝对路径或内容。

## 13. Phase 1 MVP 验收标准

1. 在 Web/Desktop 中要求 Agent 生成 HTML、Markdown、图片、PDF 或 CSV，Agent 可以调用工具并自动打开对应 renderer；分别验证 HTML sandbox、Markdown raw HTML 禁止、图片缩放、PDF fallback 和 CSV 有界表格。
2. 刷新 Web、重启 Desktop、切换后再返回会话，Artifact 列表可以从 session entries 恢复。
3. 同一路径连续登记两次只显示一个列表项，revision 增加且内容刷新。
4. 文件删除后，历史 Artifact 仍在列表中并显示 missing。
5. 非当前会话或后台任务产生 Artifact 时，不改变当前右侧面板；automation run 列表出现未读点，进入 run 后能打开并查看 Artifact。
6. `../`、绝对路径、工作区外符号链接和伪造 Artifact ID 均无法读取。
7. 恶意 HTML 无法访问父页面、JCode API、文件系统或外部网络。
8. Desktop 能打开和定位合法本地 Artifact；Web 只提供预览/下载。
9. CLI/TUI 的工具 schema、ACP 工具 schema 和对应提示词中均找不到 `show_artifact`。
10. 不需要任何 enable 配置即可在本地 Web/Desktop 会话使用。
11. 使用 JCode 配置中的真实 Kimi 模型完成至少 HTML + Markdown/CSV 两类真实生成，断言模型调用 `show_artifact`、网页自动打开 Viewer、刷新后列表恢复。

### 13.1 Phase 3 Cloud 分享验收

1. 未登录 Cloud 时不显示分享按钮，生成和预览 Artifact 全程不出现登录提示或失败。
2. 登录 Cloud 但关闭当前 session sync 时，用户仍可显式分享单个 Artifact，且不会因此上传会话历史。
3. `show_artifact` 成功不会产生任何 Cloud 请求；只有用户点击分享才开始上传。
4. 分享链接打开后可以预览/下载被分享的固定 revision，Cloud 数据库、对象存储和访问日志中没有明文内容或 `share_key`；分享页请求中不包含 URL fragment。
5. Artifact 后续更新不会悄悄改变旧分享链接的内容。
6. 用户撤销后链接失效；Cloud 上传失败、token 过期或离线不影响本地 Artifact。
7. 分享前后 `/api/cloud/sync` sessions map 保持不变，且 Cloud E2E/数据库审计证明 transcript、标题、路径和文件正文没有明文泄露。

## 14. 测试与发布门禁

### 14.1 Phase 1：JCode

| 层级 | 必过门禁 |
| --- | --- |
| Go unit | path/symlink/sensitive deny；stable ID/revision；recorder failure；approval auto-pass；Web/TUI/ACP tool catalog；content CSP/nosniff/size/range |
| Frontend unit | artifact reducer/replay；active/background focus；RightPanel/expanded/fullscreen；所有 renderer 状态；i18n/accessibility |
| Integration | list/content/download API + WebSocket upsert + session replay + automation no-focus + CLI/ACP schema negative snapshots |
| Real-model Web E2E | 启动真实 `jcode web`，使用当前配置的 Kimi 模型生成至少 HTML 与 Markdown/CSV；浏览器断言 tool call、Artifacts 入口、sandbox、内容、刷新恢复 |
| Desktop smoke | Tauri 环境 open/reveal 合法 Artifact；伪造 ID、missing、symlink swap 失败 |

Phase 1 不要求把 JCode Web 部署到 K8s；它以本地 Web/Desktop 为目标。真实模型测试不得使用 mock provider 代替 Kimi。

### 14.2 Phase 3：JCode Cloud

| 层级 | 必过门禁 |
| --- | --- |
| Orchestrator unit/store | migration、owner isolation、intent/upload/complete、quota、expiry/revoke、object GC、user delete cascade |
| Console/share-page | fragment 解密、renderer sandbox、no-referrer、expired/revoked/error、无 plaintext cache/telemetry |
| Cross-implementation crypto | JCode Go、Cloud Go、浏览器 WebCrypto 共享 versioned test vectors |
| Integration | logged-in gate；session sync off；auto_connect off；revision pin；upload retry；完整 share/revoke 流 |
| K8s deployment | 部署到目标集群，确认 migration、object store、Ingress 分享页、公开密文下载、撤销/过期、pod rollout 后状态 |
| Adversarial audit | psql/object/log zero-plaintext grep；fragment 不进请求；伪造 share ID/跨用户/重放/篡改 ciphertext 全部失败 |

Cloud K8s 测试必须使用真实对象存储和部署配置；mock 仅用于单元测试，不能作为部署验收。

## 15. 发布分期

### Phase 1：MVP

- Web-only 工具注册与 transport policy
- Artifact session entry、索引与 WebSocket 事件
- Artifacts 面板和常用格式 Viewer
- Web 下载
- Desktop 默认应用打开与文件管理器定位
- 安全、回放、隔离和 transport 测试

### Phase 2：增强

- XLSX 表格预览与更多 Office 元数据
- Artifact 全屏、多 Artifact 对比、手动“标记为 Artifact”
- 受控的对话内 Artifact 链接
- 内容摘要、缩略图和更精细的未读体验

### Phase 3：扩展

- SSH/Docker 远程工作区的有界流式读取
- 登录 JCode Cloud 后显式创建 E2EE Artifact 分享链接
- 未登录时隐藏分享能力并保持完整本地体验
- 分享 revision 快照、过期时间、复制链接和撤销
- Cloud Console/分享页的安全预览与下载
- Artifact 历史 revision 查看和导出包
