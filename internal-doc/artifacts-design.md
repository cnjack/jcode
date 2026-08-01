# JCode Artifacts Technical Design（Web / Desktop）

- 状态：Approved for implementation（Grok 4.5 + Kimi 双评审 GO，2026-08-01）
- 对应 PRD：`internal-doc/artifacts-prd.md`
- 目标：为 Web/Desktop 增加 session-scoped Artifact 登记、回放、预览，以及登录后可选的 E2EE Cloud 分享
- 关键约束：CLI/TUI、ACP 不注册、不暴露、不需要 enable 配置

## 1. 决策摘要

1. 新增 Web transport 专用只读工具 `show_artifact`。
2. Artifact 是会话级元数据，文件仍存放在任务工作区；不扫描整个工作区，不复制文件正文到 session JSONL。
3. `show_artifact` 完成四件事：严格校验路径、计算/更新 Artifact、写 session entry、发送 WebSocket 事件。
4. Web 和 Desktop 复用同一 React Artifacts 面板。Desktop 额外通过 Tauri command 打开或 reveal 本地文件。
5. 不扩展共享 `handler.AgentEventHandler`；Web 工具通过窄回调调用现有 `WebHandler.Emit`。
6. 工具仅加入 `internal/command/web.go` 的候选列表；Tool Search 路径再由 `ToolTransportWeb` policy 二次约束。`interactive.go` 和 `acp.go` 不加入候选。
7. MVP 只对本地工作区注册工具。远程工作区等有界流式读取能力完成后再开放。
8. Phase 3 复用 JCode Cloud sibling repository 的设备登录和对象存储提供显式分享；`show_artifact` 保持纯本地，登录不会触发自动上传，未登录不会阻塞或提示。
9. 本地 Artifact 的领域模型、路径策略和并发 Registry 放入 `internal/artifact`，避免 `internal/tools`、`internal/web` 和 `internal/session` 互相反向依赖。
10. UI 采用原型评审后的组合：Docked Workbench 是默认，Inline Quick Look 是紧凑入口，Focus Canvas/fullscreen 是同一 Viewer 的放大 presentation；三者共享选择、renderer 和 share state。
11. Cloud 分享采用 intent → bounded ciphertext upload → complete 的状态机。每次分享生成独立 Artifact Share Secret（字段 `share_key`），不复用账号 CEK 或 Account Sync Key，也不使用歧义缩写。

## 2. 现状与可复用能力

### 2.1 共享 Web/Desktop UI

`web/` 是 Browser Web 与 Tauri Desktop 共用的 React 产品 UI，构建后进入 `internal/web/dist/`。现有右侧面板由以下组件承载：

- `web/src/App.tsx`：维护 right panel 类型和开关状态。
- `web/src/components/RightPanel.tsx`：Plan / Files / Changes 标签及内容。
- `web/src/components/TopBar.tsx`：Browser Web 面板入口。
- `web/src/components/DesktopTitlebar.tsx`：Desktop 标题栏入口，复用相同状态。

因此 Artifact 不需要单独开发 Desktop 前端，只需在同一状态模型中增加 `artifacts` panel，并为 Tauri 环境注入额外 action。

已评审 UI 原型位于 `internal-doc/artifacts-ui/`。它验证了 Docked、Focus Canvas、Quick Look、Cloud 登录门和完整分享状态；生产组件遵循该信息架构，但使用现有 Heroicons、i18n 和 Redux runtime，不复制原型中的占位内容。

### 2.2 Web 事件通道

`internal/handler/web.go` 的 `WebHandler` 已支持通用 `Emit(event, data)`，WebSocket bridge 位于：

- `web/src/lib/ws.ts`
- `web/src/app/wsBridge.ts`
- `web/src/app/store.ts`

Artifact 事件可以复用此通道，不需要给所有 transport 的 `AgentEventHandler` 增加方法。现有 `Emit` 是有界 channel 上的 best-effort 通知，队列满时可能丢弃，因此持久化/list API 必须承担对账责任。

### 2.3 Transport-scoped tool catalog

`internal/command/tool_catalog.go` 已使用 `ToolTransportTUI`、`ToolTransportWeb`、`ToolTransportACP` 构建 Tool Search 模式下的最终工具计划。Artifact 应采用 direct、read-class、all-modes、Web-only policy。各 command 的候选注册是静态/eager 路径的隔离边界；catalog policy 是 Tool Search 路径的第二层保护。

### 2.4 Session JSONL

`internal/session/session.go` 已提供 append-only session entry 和 Recorder。Artifact entry 可以沿用同一记录/回放链路，使自动化任务、页面刷新和历史会话都不依赖内存事件。

### 2.5 OpenWorker 参考与差异

OpenWorker 的实现包含：

- session artifacts list/read/reveal API；
- RightRail Artifact 列表和 Viewer；
- HTML、Markdown、图片、PDF、CSV、表格等 renderer；
- `[Title](artifact:relative/path)` 对话链接。

JCode 保留其 Viewer 与右侧栏思路，但不采用“按后缀扫描 workspace”的列表语义。JCode 的 Artifact 必须来自显式登记，这样才能稳定做到会话隔离、回放、未读状态和后台任务处理。

实现时可对照 OpenWorker sibling repository 中的这些位置：

- `surfaces/gui/src/components/RightRail.tsx`：Artifact 列表、Viewer、HTML/Markdown/image/PDF/CSV/sheet/office 分流；
- `surfaces/gui/src/components/Markdown.tsx`：`artifact:` 链接转 Artifact chip；
- `surfaces/gui/src/api.ts`：list/read/reveal 客户端；
- `coworker/server/app.py`：session artifact HTTP routes；
- `coworker/server/manager.py`：workspace 扫描、类型识别和路径处理；
- `tests/test_artifact_walk.py`、`tests/test_server.py`：扫描和 API 行为测试。

其中全工作区扫描、向 Browser UI 返回绝对路径，以及 HTML iframe 同时启用 `allow-scripts allow-same-origin` 都不应照搬；JCode 方案分别以显式 Registry、opaque Artifact ID、opaque-origin sandbox 取代。

## 3. 总体架构

```mermaid
sequenceDiagram
    participant M as "Agent model"
    participant T as "show_artifact tool"
    participant R as "Artifact registry"
    participant S as "Session recorder"
    participant W as "WebHandler / WebSocket"
    participant U as "Web or Desktop UI"
    participant F as "Workspace file"

    M->>T: "show_artifact(path, title, kind, focus)"
    T->>F: "validate, stat, MIME sniff"
    F-->>T: "canonical file metadata"
    T->>R: "upsert(session, relative path)"
    R-->>T: "artifact metadata + revision"
    T->>S: "append artifact entry"
    T->>W: "emit artifact_upserted"
    T-->>M: "registered artifact id"
    W-->>U: "update list; focus only active task"
    U->>R: "GET content by artifact id"
    R->>F: "revalidate and stream"
    F-->>U: "bounded response"
```

Artifact registry 不是新的数据库。运行中它是由 session entries 构建的内存索引；持久化事实来源仍然是 JSONL，文件正文事实来源仍然是 workspace。

跨 Cloud 分享是另一条明确由用户触发的链路，不经过模型工具和 device relay：

```mermaid
sequenceDiagram
    participant U as "User"
    participant J as "JCode share service"
    participant C as "Cloud orchestrator"
    participant O as "Object storage"
    participant P as "Public share page"

    U->>J: "Share current revision"
    J->>J: "revalidate, bounded snapshot, digest, new share_key"
    J->>C: "create intent (device token, no key/plaintext)"
    C-->>J: "share_id, upload/complete URLs, base share URL"
    J->>J: "AES-256-GCM encrypt metadata + content"
    J->>C: "PUT bounded ciphertext"
    C->>O: "single-object PUT"
    J->>C: "complete(encrypted metadata, ciphertext digest)"
    C-->>J: "complete share metadata, base URL only"
    J-->>U: "base URL + #k=v1.<share_key>"
    U->>P: "open URL; fragment stays in browser"
    P->>C: "fetch encrypted metadata/content"
    P->>P: "WebCrypto decrypt and safe renderer"
```

本地 revision 与 Cloud snapshot 不是同一种版本语义：

| 维度 | 本地 Artifact | Cloud share |
| --- | --- | --- |
| 内容事实来源 | workspace 当前文件 | 创建分享时固定的 ciphertext object |
| revision | 显式登记代数 | 分享绑定的登记 revision |
| 文件未重新登记但已变化 | Viewer 读到新内容，revision 不变 | 已有链接完全不变 |
| 持久化 | session JSONL 只存 metadata | Cloud DB + object store 只存 routing metadata/ciphertext |
| 触发者 | Agent `show_artifact` | 用户 Viewer `Share` |

## 4. 领域模型

建议共享的逻辑模型：

```go
type ArtifactKind string

const (
    ArtifactAuto     ArtifactKind = "auto"
    ArtifactText     ArtifactKind = "text"
    ArtifactMarkdown ArtifactKind = "markdown"
    ArtifactCode     ArtifactKind = "code"
    ArtifactHTML     ArtifactKind = "html"
    ArtifactImage    ArtifactKind = "image"
    ArtifactPDF      ArtifactKind = "pdf"
    ArtifactCSV      ArtifactKind = "csv"
    ArtifactBinary   ArtifactKind = "binary"
)

type Artifact struct {
    ID           string       `json:"id"`
    SessionID    string       `json:"session_id"`
    RelativePath string       `json:"relative_path"`
    Title        string       `json:"title"`
    Kind         ArtifactKind `json:"kind"`
    MediaType    string       `json:"media_type"`
    Size         int64        `json:"size"`
    Revision     int          `json:"revision"`
    UpdatedAt    time.Time    `json:"updated_at"`
    Status       string       `json:"status"` // available | missing | unsupported | too_large | error
}
```

### 4.1 ID 与幂等性

`ID` 应是不可逆、稳定的标识，例如：

```text
base64url(sha256(session_id + "\x00" + normalized_relative_path))[0:22]
```

不要把绝对路径编码进 ID。相同 session + path 得到相同 ID；每次成功登记 revision 加一。标题或 kind 改变也视为一次 revision。

### 4.2 路径语义

- 工具输入只接受 slash-separated 相对路径。
- 存储前执行 `filepath.Clean` 并转换为 workspace-relative slash path。
- ID 计算和 session entry 始终使用 normalized relative path。
- 绝对路径只在服务端一次请求的校验过程中短暂存在，不返回 Browser Web；Desktop 原生 action 也优先传 Artifact ID，而不是路径。

### 4.3 revision、digest 与 share snapshot

- 本地 revision 只在 `show_artifact` 成功 append 一条 Artifact entry 时增加；文件 watcher 不修改 revision。
- list/content 每次重新 stat/canonicalize，不能把登记时的 size、media type 或路径校验当作永久授权。
- 分享开始时在 25 MiB 上限内读取一个有界内存 snapshot，并对 plaintext 计算 SHA-256；读取前后再次 stat，revision、size、mtime/file identity 任一变化都返回 `artifact_changed`。
- 加密和上传只使用该 snapshot，不再次读取 workspace，因此 share 完成后对应一个不可变 object。
- plaintext digest 只保存在 JCode 本地 share metadata 中，用于显示与重试；Cloud 只接收 ciphertext size/digest。

## 5. `show_artifact` 工具设计

### 5.1 输入与输出

```go
type ShowArtifactInput struct {
    Path  string `json:"path"`
    Title string `json:"title,omitempty"`
    Kind  string `json:"kind,omitempty"`
    Focus *bool  `json:"focus,omitempty"`
}
```

JSON schema：

- `path` required string；描述明确要求相对工作区、文件必须已存在。
- `title` optional string，建议上限 200 Unicode code points。
- `kind` optional enum：`auto|text|markdown|code|html|image|pdf|csv|binary`。
- `focus` optional boolean，缺省 true。

成功输出应是简短、模型可理解的纯文本或 JSON 字符串，例如：

```json
{
  "artifact_id": "Wwm0qdzpWDlUnqEOgc0Q8w",
  "path": "reports/analysis.html",
  "title": "销售分析报告",
  "kind": "html",
  "revision": 1,
  "message": "Artifact is available in the Artifacts panel."
}
```

### 5.2 依赖注入

工具不应依赖具体 WebHandler 类型，建议使用窄依赖：

```go
type ShowArtifactDeps struct {
    SessionID func() string
    Project   func() string
    Recorder  ArtifactRecorder
    Service   *artifact.Service
    Emit      func(event string, data any)
}
```

其中 `ArtifactRecorder.RecordArtifact` 必须返回 `error`，而不是沿用部分现有 Recorder helper 的 best-effort `void` 风格；Artifact UI 只有在 metadata 已经 durable 后才能报告成功。`artifact.Service` 位于 `internal/artifact`，拥有路径策略、MIME 分类、session 级写锁和 Registry；它只依赖一个窄 Recorder interface，不依赖 Web handler。

`NewShowArtifactTool(deps)` 仍是 `*tools.Env` 的方法，路径解析和执行环境来自 `Env`。这样工具逻辑可单测，且不会让 `internal/tools` 反向依赖 `internal/handler` 或 `internal/web`。

### 5.3 执行顺序

1. 检查 `Env.IsRemote()`；MVP remote 直接返回不支持错误。正常情况下 remote 根本不会注册该工具。
2. 校验 path 非空、非绝对路径、清理后不以 `..` 开头。
3. 以 task workspace 为 root 解析 canonical target。
4. 使用 symlink-aware containment 检查，确认 target 位于 canonical workspace root 下。
5. `stat` 确认存在且是 regular file；拒绝目录、socket、device、FIFO。
6. 读取最多 512 bytes 做 `http.DetectContentType`，结合扩展名映射得到服务端 kind/media type。
7. 应用敏感路径 deny rules。
8. Artifact service 在 session 级 mutex 下根据 Registry 计算稳定 ID 和下一个 revision。
9. 调用返回 error 的 Recorder method append Artifact entry；失败则不更新 Registry、不发 UI 事件，整个工具失败。
10. append 成功后把相同 record apply 到 Registry，并刷新 `SessionMeta` 中可重建的 Artifact 摘要；任意 durable append 都令 `ArtifactUpdatedAt > ArtifactViewedAt`，因此 `ArtifactUnseen=true`。摘要写失败不回滚已落盘 entry，由下一次 task list/list API reconciliation 修复。
11. 发出 best-effort `artifact_upserted`。Web channel 满导致事件丢弃不影响工具成功，客户端通过 list/replay 对账。
12. 返回成功结果。

### 5.4 Agent 指令

工具 description 应包含足够使用规则，不需要给所有系统提示词加一段全局 Artifact 文本。只有 Web 工具 schema 可见时，模型才会获得说明，从根源避免 CLI/ACP 误用。

如果后续实践证明 description 不够，再在 `internal/command/web.go` 构建 Web agent 时注入 Web-only prompt fragment；不得修改所有 transport 共用的基础 prompt。

## 6. Transport 隔离

### 6.1 Tool catalog policy

在 `internal/command/tool_catalog.go` 增加专用 transport slice：

```go
var webOnlyTransports = []string{agent.ToolTransportWeb}
```

策略语义：

```go
"show_artifact": scopedDirectPolicy(
    "web.artifact",
    allModes,
    webOnlyTransports,
    "read",
)
```

属性：

- execution：direct；
- risk/access：read；
- modes：normal + plan；
- transports：web only；
- approval：不需要。

Plan mode 允许调用是有意义的：Agent 可以在研究/规划过程中生成并展示调查报告或图表。工具本身不修改文件。

### 6.2 候选工具注册

只在 `internal/command/web.go` 中：

- `buildAllTools` 候选加入 `tenv.NewShowArtifactTool(...)`；
- plan candidate list 同样加入；
- 本地工作区才加入候选，remote 不加入。

明确不修改：

- `internal/command/interactive.go` 的 all/plan tool lists；
- `internal/command/acp.go` 的 all/plan tool lists；
- ACP capability 或 JSON-RPC schema；
- TUI 组件；
- 通用 `AgentEventHandler`。

增加 catalog 单测，确保 Tool Search 路径即使将 `show_artifact` 候选误传给 TUI/ACP build plan，也会被 policy 拒绝。静态/eager 路径另做 CLI/TUI 与 ACP 工具列表测试，因为该路径目前不会调用 `buildCommandToolPlan`。

### 6.3 Approval policy

在 `internal/runner/approval.go` 的 `noApprovalNeeded` 中显式加入 `show_artifact`。虽然工具会写入会话 metadata，但不会修改工作区或外部系统；如果把它留作未知工具，MANUAL mode 会出现与“主动展示结果”相冲突的无意义审批。

同时增加 approval 单测，确保 MANUAL mode 自动批准 `show_artifact`。这一 allowlist 不能替代 transport 隔离：工具是否对模型可见仍由 Web 注册列表和 catalog policy 决定。

### 6.4 Desktop 的 transport 归属

Tauri Desktop 运行 Go sidecar 并消费同一 Web UI/API，因此仍使用 `ToolTransportWeb`，不需要增加 `ToolTransportDesktop`。Desktop 特有动作由前端 `isTauri()` 和原生 command availability 决定，不影响模型工具注册。

## 7. Session 持久化与 Registry

### 7.1 Session entry

在 `internal/session/session.go` 增加 `EntryArtifact`，并给 `Entry` 增加可选字段：

```go
ArtifactID        string `json:"artifact_id,omitempty"`
ArtifactPath      string `json:"artifact_path,omitempty"`
ArtifactTitle     string `json:"artifact_title,omitempty"`
ArtifactKind      string `json:"artifact_kind,omitempty"`
ArtifactMediaType string `json:"artifact_media_type,omitempty"`
ArtifactSize      int64  `json:"artifact_size,omitempty"`
ArtifactRevision  int    `json:"artifact_revision,omitempty"`
ArtifactFocus     bool   `json:"artifact_focus,omitempty"`
```

Recorder 增加 `RecordArtifact(ArtifactRecord)`。Entry 不保存绝对路径、文件正文、缩略图或 base64。

### 7.2 回放算法

读取 session entries 时：

1. 过滤 `EntryArtifact`。
2. 按 entry 顺序处理。
3. 以 Artifact ID 为 key，revision 高者覆盖低者。
4. 当前文件状态在请求 list/content 时重新校验，不相信历史 size/media type。
5. session 中有 metadata 但文件不存在时返回 `status=missing`。

不需要单独迁移老会话；没有 Artifact entry 的会话返回空列表。

### 7.3 Registry 生命周期

Web command 创建一个进程级 `artifact.Service`，内部按 session UUID 分片保存 Registry；这不是全局可见的领域状态，只有 Web server 与 Web-only tool 持有该 service：

- task 创建/恢复或首次 API 请求时从 session entries hydrate；
- tool 调用时 upsert；
- inactive task 的 workspace root 从 `session.SessionMeta.Project` 解析，不相信 Browser 提交的 pwd；
- task 关闭后可以按 LRU/显式 release 释放分片；再次访问时从 JSONL 重建；
- API 如果 task runtime 未加载，从 `session.LoadSession` 临时 hydrate。

每个 session shard 使用 `sync.RWMutex`，Artifact service 在写锁中串行化“分配 revision → append entry → apply registry”。同一路径并发登记时 revision 必须原子递增，entry append 顺序与返回 revision 一致。读取 list/content 只在复制 metadata 时持有读锁，不能在 stat、文件流或 Cloud 上传期间持锁。

### 7.4 Automation/run list 的 durable unseen contract

`artifact_upserted` WebSocket 不能承担后台 run 的未读事实。`SessionMeta` 增加以下物化字段：

```go
ArtifactCount    int       `json:"artifact_count,omitempty"`
ArtifactUnseen   bool      `json:"artifact_unseen,omitempty"`
ArtifactUpdatedAt time.Time `json:"artifact_updated_at,omitempty"`
ArtifactViewedAt time.Time `json:"artifact_viewed_at,omitempty"`
```

- 每次 Artifact entry durable append 后，Registry 的 distinct ID 数写入 `ArtifactCount`，entry 时间写入 `ArtifactUpdatedAt`；只要 `ArtifactUpdatedAt > ArtifactViewedAt`，无论 foreground/background/automation 都把 `ArtifactUnseen` 置为 true。
- `SessionMeta` 是 task/run list 的快速物化索引，不是事实来源。task list、automation run details 和 reconnect reconciliation 在 `ArtifactUpdatedAt > ArtifactViewedAt` 或摘要缺失/不一致时从 JSONL 重建并修复它。
- 用户从 automation run header 或 task badge 打开 Artifact Viewer 后，调用 `PATCH /api/tasks/{taskID}/artifacts/viewed`；服务端把当前 latest Artifact entry 时间写入 `ArtifactViewedAt`，再把 `ArtifactUnseen=false`。重复调用幂等。
- foreground active task 的 `focus=true` 只在 Viewer 确实打开后 clear unseen；后台 tool call 永远不能自行 clear。
- JSONL append 成功但 `SessionMeta` 更新失败时工具仍可成功，因为 entry 已 durable；必须记录诊断并依赖上述 reconciliation 修复。不得为了重试摘要写入而追加重复 revision。
- viewed 是 **session 级已读水位**，同一 task 的多个 tab/窗口共享；任一连接成功提交 viewed PATCH 后其他连接的 badge 也在下次 event/reconcile 清除。focus/presentation 仍是 **每个 WebSocket 连接** 的本地 UI 状态，一个 tab 打开 Viewer 不得替另一个 tab 切 panel。
- 所有进入 Viewer 的入口统一调用一个 `openArtifact(taskID, artifactID, presentation)` action；只有 Viewer 成功挂载后该 action 才提交 viewed PATCH。列表项、工具结果卡片、automation run header、快捷键和 focus event 不得各自实现水位更新。

## 8. Web API

建议路由：

```text
GET /api/tasks/{taskID}/artifacts
GET /api/tasks/{taskID}/artifacts/{artifactID}/content
GET /api/tasks/{taskID}/artifacts/{artifactID}/download
PATCH /api/tasks/{taskID}/artifacts/viewed
```

### 8.1 List

返回该 task 最新 Artifact metadata 数组。服务端在返回前对每一项做轻量 stat，更新 `status` 和 `size`；不读取正文。

### 8.2 Content

Content endpoint：

- 只接收 task ID + Artifact ID，不接收 path query；
- 从 Registry 查到相对路径，再执行实时 canonical containment 校验；
- 根据 renderer 和大小限制决定 inline 或返回 `413 artifact_too_large`；
- 设置正确的 `Content-Type`、`X-Content-Type-Options: nosniff` 和按类型的 CSP；
- 支持 HTTP range 以改善 PDF/媒体预览；
- 不把内容包进 JSON/base64。

### 8.3 Download

Download 做相同的 ID、session 和路径检查，使用 `Content-Disposition: attachment`。可以允许比 inline 更大的文件，但仍需要总大小上限、取消传播和流式发送。

List API 本身也是显式的 re-detect 操作：每次请求都重新 stat、MIME detect、canonical containment，并返回最新 `available|missing|unsupported|too_large|error`。Viewer 的 “Check again” 只重新请求 list/content，不增加 revision，也不需要额外 mutating endpoint。若文件在读取过程中增长并超过对应上限，stream 立即终止并返回/记录 `artifact_too_large`，不能继续把剩余字节送入 renderer。

### 8.4 路由归属

Artifact API 不应复用现有 `/api/files/content?path=...`。后者是通用 Files 浏览接口，仍接受 path；Artifact endpoint 需要更严格的 session ownership 和 ID capability 边界。

建议新增：

- `internal/web/artifacts.go`：HTTP handlers、content headers、range/stream；
- `internal/tools/artifact.go`：模型工具、路径校验、类型检测；
- `internal/session/artifact.go`：record DTO 或 recorder helper（如果可保持 session.go 简洁）；
- `internal/web/artifact_adapter.go`：HTTP/task runtime 到 `artifact.Service` 的 hydration 适配；不得持有第二套 Registry map。session shard 与 revision 的唯一 owner 是 `internal/artifact.Service`。

## 9. WebSocket 事件

事件：

```json
{
  "type": "artifact_upserted",
  "task_id": "task-123",
  "data": {
    "artifact": {
      "id": "Wwm0qdzpWDlUnqEOgc0Q8w",
      "relative_path": "reports/analysis.html",
      "title": "销售分析报告",
      "kind": "html",
      "media_type": "text/html",
      "size": 18304,
      "revision": 2,
      "status": "available",
      "updated_at": "2026-08-01T10:00:00Z"
    },
    "focus": true
  }
}
```

前端规则：

- event task 等于当前 active task：upsert store；`focus=true` 时打开并选择 Artifact。
- event task 不是 active task：不改变当前 panel，只给对应 task 标记 artifact unseen。
- 初次连接或 reconnect 后，通过 list API/session replay 对账，不能把 WebSocket 当唯一事实来源。
- 事件重复到达必须幂等，以 ID + revision 判断是否更新。

## 10. 前端状态与组件

### 10.1 类型与 store

`web/src/lib/types.ts` 增加 `Artifact` 和 session entry 可选字段。Redux 建议按 task 存储：

```ts
type ArtifactState = {
  byTask: Record<string, {
    byId: Record<string, Artifact>
    order: string[]
    selectedId?: string
    unseenCount: number
    loading: boolean
    error?: string
  }>
}
```

如果当前 store 已按 task 管理 chat state，可把 artifact state 放入对应 task state，避免再建平行生命周期。

### 10.2 RightPanel 改动

- `PanelType` 增加 `'artifacts'`。
- `RightPanel.tsx` 增加 Artifacts tab 和 count/badge。
- `TopBar.tsx`、`DesktopTitlebar.tsx` 的 panel menu 增加 Artifacts。
- 新增 `ArtifactPanel.tsx`：列表、空状态、missing/too-large/unsupported 状态。
- 新增 `ArtifactViewer.tsx`：按 kind 路由 renderer。
- Artifact 模式支持 480px 默认宽度、更大拖拽上限和 fullscreen；其他 panel 保持原行为。

UI 原型冻结后的 presentation contract：

```ts
type ArtifactPresentation = 'docked' | 'inline' | 'focus' | 'fullscreen'

type ArtifactViewerState = {
  taskId: string
  selectedId?: string
  presentation: ArtifactPresentation
  previousPanel?: 'plan' | 'files' | 'changes'
}
```

presentation transition 是单一状态机，而不是四套组件各自切换：

| 当前状态 | 事件 | 下一状态 | 恢复规则 |
| --- | --- | --- | --- |
| closed/其他 panel | open Artifact | docked | 记录 `previousPanel` |
| docked | quick look eligible | inline | selected ID 不变 |
| inline | open | docked | 复用同一 renderer cache |
| docked/inline | focus | focus | main canvas 接管 presentation |
| docked/focus | fullscreen | fullscreen | 记住调用前 presentation |
| fullscreen | Esc/close | 调用前状态 | focus 返回触发按钮 |
| focus | back | docked | 恢复 conversation scroll/focus |
| 任意 | task changed | 该 task 保存的状态或 docked | 不沿用另一 task 的 selected ID |

- `docked` 是默认：RightPanel 上部是 Artifact index，下部是 Viewer；宽度初值 480px，上限为 viewport 的 80%。
- `inline` 是 tool result card 的 Quick Look；只对短 Markdown/text/CSV 和图片启用，HTML/PDF 的 Open 仍进入 docked，避免在对话流中运行主动内容。
- `focus` 把 Viewer 提升为 main 区域画布，保留 Back to conversation 与紧凑 Artifact strip；不创建第二个 renderer instance state。
- `fullscreen` 是同一 Viewer 的 modal presentation，Esc/close 恢复上一个 presentation，focus 返回触发按钮。
- selected ID、zoom、source/render mode 与 share state 归一化存储；presentation 组件不能各自 fetch 一份内容并造成 revision 漂移。
- `show_artifact(focus=true)` 只把当前浏览器连接 active task 的 presentation 切到 docked；后台 task 只增加 unseen。
- `Shift+Cmd/Ctrl+A` 打开/关闭 Artifacts。关闭后 TopBar/DesktopTitlebar 入口继续显示 count/unseen。

### 10.3 Renderer

建议拆分：

```text
web/src/components/artifacts/
  ArtifactPanel.tsx
  ArtifactViewer.tsx
  MarkdownArtifact.tsx
  TextArtifact.tsx
  HtmlArtifact.tsx
  ImageArtifact.tsx
  PdfArtifact.tsx
  CsvArtifact.tsx
```

渲染要求：

- Markdown：复用项目已有 Markdown renderer；MVP 明确关闭 raw HTML（不采用“可选 sanitizer”分支），link protocol 只允许 `https/http/mailto`，外链使用 `noopener noreferrer`。
- Text/code：按需 fetch；超过行数后虚拟化；编码不是 UTF-8 时降级为 binary。
- HTML：只使用 sandbox iframe，固定 `sandbox="allow-scripts"`，不加 `allow-same-origin`、`allow-forms`、`allow-popups`、`allow-top-navigation`。本地 Viewer 以受保护 content endpoint 作为 iframe `src`，该响应固定 network-blocking CSP；公开分享页解密后使用 `srcdoc`，在原文之前注入等价 CSP `<meta>`，仍保持 opaque origin，绝不把 HTML 插入主 DOM。
- Image：使用受控 content URL；SVG 只能作为 image document，不能内联 DOM。
- PDF：优先浏览器内建 viewer；不可用时显示下载/外部打开。
- CSV/TSV：有界解析，默认最多 10,000 行或 5 MiB；展示截断提示。
- Binary/Office：显示 metadata，Web 下载，Desktop 外部打开。

### 10.4 工具结果卡片

`internal/handler/web.go` 的 tool display metadata 增加 `show_artifact` case，使 `web` 的工具消息可以识别 `artifact_id`。前端用专用紧凑卡片显示“Open artifact”，点击时只按 ID 打开，不解析路径。

automation run replay 使用同一个 card renderer。`AutomationRunReplay` header 增加 Artifact count/unseen 入口，点击后将该 run 的 session ID 设为 Viewer task ID；它不需要把 automation 变成当前 chat task，也不会触发后台 focus。

## 11. Desktop 原生能力

Desktop 增加两个 Tauri commands：

```text
open_artifact(task_id, artifact_id)
reveal_artifact(task_id, artifact_id)
```

command 通过 sidecar 的受保护 Desktop bridge 用 ID 换取已验证 canonical path。具体合同：

1. Tauri 启动 sidecar 前生成 32-byte 随机 token，以 base64url-no-padding 编码，只通过 child env `JCODE_DESKTOP_BRIDGE_TOKEN` 交给 Go sidecar，并保存在 Rust managed state；不得进入 WebView、localStorage、前端启动参数或日志。
2. Go command startup 只读取一次该 env，decode 后存入进程内 `ServerConfig.DesktopBridgeToken`，随即 `os.Unsetenv("JCODE_DESKTOP_BRIDGE_TOKEN")`；只在 token 存在且恰为 32 bytes 时注册 private resolve route。比较使用 constant-time compare，错误统一返回 404/unauthorized，不记录 header/token 值。所有 `execute`/tool/sidecar 子进程的环境构造还必须显式 scrub 该变量，不能只依赖一次 `Unsetenv`。
3. Rust command 只接受 `task_id + artifact_id + action`，向 loopback `POST /api/desktop/artifacts/{taskID}/{artifactID}/resolve` 发送 `Authorization: Bearer <token>`。普通 Browser API client 和 WebView JavaScript 都不持有该 header。
4. Go handler 验证 bridge token、session ownership、local workspace、canonical containment 和 regular file，才向 Rust 响应 canonical path；token 缺失、长度错误、比较失败或 Desktop mode 未启用时都不得返回路径。
5. Rust 收到 path 后调用平台 opener/reveal；Browser Web 没有 Tauri invoke 能力，也拿不到 bridge token。

不得暴露通用 `open_path(path)`，也不得把 canonical path 返回普通 Browser API 或写入 session/WS。

执行前必须再次确认：

- 当前是本地 task；
- Artifact ID 属于 task；
- 文件仍在 task workspace 内；
- target 是 regular file；
- path 没有发生 symlink swap/escape。

操作系统行为：

- macOS：默认应用打开；Finder reveal。
- Windows：ShellExecute；Explorer select。
- Linux：`xdg-open`；可用文件管理器 reveal 或退化为打开父目录。

前端通过现有 `web/src/lib/useDesktop.ts` 暴露 typed methods。Browser Web 不渲染 reveal/open-native 按钮，只显示 download。

## 12. 安全设计

### 12.1 路径与 symlink

不能只使用 `filepath.Rel` 做 lexical containment。最低要求：

1. canonicalize workspace root；
2. canonicalize target 的已有路径；
3. 再次 `filepath.Rel(canonicalRoot, canonicalTarget)`；
4. 拒绝 `..`、绝对 rel、非 regular file；
5. content/open 时重复检查，不能只相信注册时结果。

如果平台支持，content 读取采用 openat/no-follow 风格，减少检查后替换的 TOCTOU 窗口。否则需要在打开后对 fd 做 stat，并记录剩余风险。

### 12.2 敏感文件

Artifact 只用于生成结果，不应成为绕过 Files 访问控制的通道。MVP 建议拒绝：

- `.git/**`、`.jcode/**`；
- 已知 credential/key 文件，例如 `.env`、`*.pem`、`*.key`、SSH key；
- socket、device、FIFO 和目录；
- workspace 外任意文件。

deny rule 应集中在服务端并单测。未来若需要预览 `.env.example`，应精确 allow，而不是取消整个规则。

### 12.3 MIME 与主动内容

- `kind` 只是 Agent hint，服务端扩展名 + sniff 结果为准。
- 所有内容响应加 `nosniff`。
- HTML 默认 CSP：`default-src 'none'; img-src data: blob:; style-src 'unsafe-inline'; script-src 'unsafe-inline'; connect-src 'none'; frame-src 'none'; object-src 'none'; base-uri 'none'; form-action 'none'`。
- HTML 不允许同源身份，因此即使存在脚本也不能读父页面、cookie、localStorage 或调用 JCode API。
- 禁止在主 React DOM 中直接注入 Artifact HTML/SVG。
- Markdown raw HTML 默认关闭。

MVP HTML 应定位为“单文件、自包含预览”。需要多文件静态站点时，后续设计单独的 asset manifest 和 capability URL，不能放宽为任意 workspace 路由。

### 12.4 大小与资源限制

建议默认：

| 内容 | Inline 上限 | 行为 |
| --- | ---: | --- |
| text/markdown/code/csv/html | 5 MiB | 超限不 inline |
| image/pdf | 25 MiB | 超限不 inline |
| download | 250 MiB | 超限提示外部打开（Desktop） |

服务端流式返回并尊重 request cancellation。CSV 解析放在 Web Worker 或做渐进/有界解析，避免阻塞 React 主线程。

### 12.5 Web 与 Desktop 信任边界

- Browser Web 永远看不到绝对路径。
- WebView 不能提交任意路径给 Rust opener。
- Artifact ID 不是永久授权；每次操作都要做 session ownership 和实时路径验证。
- 日志只记录 ID、相对路径和错误，不记录内容。

## 13. Remote、Automation 与 Cloud

### 13.1 Remote workspace

当前通用 Executor 的 `ReadFile` 不提供安全的有界流式 metadata/read 契约，现有 `/api/files` 也主要面向本地文件。MVP 在 `Env.IsRemote()` 时不将 `show_artifact` 加入工具候选。

后续启用 remote 前需要：

- Executor 增加 stat size/media metadata；
- range/stream 或明确的 bounded read；
- remote canonical path containment；
- download cancellation 与响应上限；
- Desktop native open 明确禁用，仅允许 Web stream/download。

### 13.2 Automation/background run

Web automation 可以登记 Artifact，因为 session entry 是持久化事实；但 WebSocket 事件只能对当前 active task 执行 focus。后台 task 只增加 unseen count。没有任何连接时，工具仍应成功，因为 Recorder 已写入。

### 13.3 Cloud Artifact sharing（Phase 3）

Phase 3 增加的是用户显式触发的“分享一个固定 revision”，不是 Artifact 自动同步，也不是 `show_artifact` 的副作用。可复用能力包括 JCode 的 device credentials/Cloud client，以及 Cloud 的 device principal、S3-compatible object store 和 bounded attachment proxy；通用 Artifact share 仍使用独立领域对象，不能复用 run diff/review artifact。

#### 13.3.1 产品、登录与隐私边界

- 前端只在 `/api/cloud/status.logged_in=true` 时渲染 Share；未登录时没有 disabled button、登录提示或分享 API 请求。
- 登录只意味着可以发起分享。分享不读取或修改 session sync、`cloud-sessions.json`、relay `auto_connect` 或 transcript。
- `auto_connect=false` 时仍使用 device token 发起一次性 HTTP 请求；Cloud 不可达/401 只更新 share error，不影响 Viewer 和本地 Registry。
- 不新增 `share_artifact` 模型工具；CLI/TUI 与 ACP 没有 Artifact 分享入口。
- 完整 URL 只在用户明确创建/复制 share 时由本地 API 返回当前 Web/Desktop 页面；不得进入 JSONL、WebSocket、Cloud event、日志、错误上报或遥测。

#### 13.3.2 JCode 本地 API、快照与 secret store

```text
POST   /api/tasks/{taskID}/artifacts/{artifactID}/shares
GET    /api/tasks/{taskID}/artifacts/{artifactID}/shares
DELETE /api/tasks/{taskID}/artifacts/{artifactID}/shares/{shareID}
```

POST body：

```json
{"revision":3,"expires_in_seconds":604800}
```

执行流程：

1. 校验实时 Cloud credentials 与请求中的 revision；stale UI 返回 `artifact_revision_conflict`。
2. 重新做 session ownership、canonical containment、sensitive-path、regular-file 和 25 MiB 上限检查。
3. 有界读取 snapshot，计算 plaintext SHA-256，并用读取前后 stat/file identity 防止 TOCTOU；变化返回 `artifact_changed`。
4. 用 CSPRNG 生成独立 32-byte Artifact Share Secret，变量和 JSON 字段统一命名 `shareKey` / `share_key`，禁止使用其他缩写。
5. 创建 Cloud intent，取得 server-generated `share_id`；用该 ID 参与 AAD 后分别加密 metadata 和 snapshot。
6. PUT ciphertext，再调用 complete；任何失败都 best-effort DELETE intent，且不写成功状态。
7. Cloud 只返回 base URL；JCode 在内存中组合 `base_url#k=v1.<base64url-no-padding share_key>` 并仅响应当前显式 POST。
8. 成功后本地记录 share metadata 与 secret，Viewer 可在重启后 copy/revoke；旧 share 与新 revision 独立。

本地持久化分离：

- `~/.jcode/artifact-shares.json`（0600）只存 `share_id/cloud_url/base_url/artifact_id/revision/plaintext_sha256/expires_at/status`，不存 key 或完整 URL。
- `share_key` 存现有 Cloud secret service `net.j-code.jcode.cloud` 下的独立 account `artifact-share-secrets`；沿用 `JCODE_CLOUD_SECRET_BACKEND=file` 测试开关，file backend 为 `~/.jcode/cloud.artifact-share-secrets.json`（0600），不得新增第二套后端选择逻辑。
- 普通 logout 不删除 share secret，也不撤销链接；重新登录同一 Cloud account 后可恢复 copy/revoke。显式 `cloud forget` 删除本地 share secrets，但不伪装成 Cloud revoke。
- Cloud owner list 可恢复管理 metadata；本地 key 缺失时仍可 revoke，但 UI 必须显示“secret unavailable”，不能重建完整链接。owner list 必须按 authenticated user 查询，不能只按当前 device 查询，否则 device 重登/替换后无法恢复管理。

#### 13.3.3 Cloud device API 状态机

所有 owner mutation/list route 使用现有 `authed` middleware 后再 `requireDevice`，严格 JSON decode：

```text
POST   /internal/v1/device/artifact-shares/intents
PUT    /internal/v1/device/artifact-shares/{shareID}/content
POST   /internal/v1/device/artifact-shares/{shareID}/complete
GET    /internal/v1/device/artifact-shares?artifact_id={opaqueID}
DELETE /internal/v1/device/artifact-shares/{shareID}
```

Intent request/response：

```json
{
  "protocol": "jcode-artifact-share-v1",
  "artifact_id": "Wwm0qdzpWDlUnqEOgc0Q8w",
  "revision": 3,
  "ciphertext_size": 18332,
  "expires_in_seconds": 604800
}
```

```json
{
  "share_id": "opaque-128-bit-id",
  "upload_url": "/internal/v1/device/artifact-shares/.../content",
  "complete_url": "/internal/v1/device/artifact-shares/.../complete",
  "base_url": "https://cloud.example/s/opaque-128-bit-id",
  "expires_at": "2026-08-08T00:00:00Z"
}
```

状态机：`pending -> uploading -> uploaded -> complete -> revoked`；`expired` 是由时间判定并由 GC 物化的终态。唯一公开可读状态是 `complete && now < expires_at && revoked_at IS NULL`。

| 操作 | 前置状态 | 成功后 | 重试/并发合同 |
| --- | --- | --- | --- |
| create intent | 无 | pending | 原子执行 per-user active count/bytes quota；生成 `intent_expires_at=now+1h`，不分配可写 object key |
| claim content PUT | pending，或上传 lease 已过期的 uploading | uploading | CAS 递增 `upload_generation`（最多 3），生成随机 `upload_claim_id`，写 `upload_claimed_at`、`upload_lease_expires_at=now+5m` 和 generation-specific server-only object key；其他并发 PUT 返回 409 |
| upload success | 当前请求持有 uploading lease | uploaded | Cloud proxy 计算 digest/size 后以 `state + upload_claim_id + upload_generation` 做 CAS；CAS 失败必须删除本 claim 的独立 object，不能改 DB digest |
| upload failure/cancel | 当前请求持有 uploading lease | pending | best-effort 删除本 generation object，并以 claim ID 释放 claim；总重试最多 3 次且受 intent TTL 限制 |
| complete | uploaded | complete | body digest/size/metadata 完全相同的重复 complete 返回同一结果；不同 payload 返回 409，不能覆盖 |
| revoke/delete | pending/uploading/uploaded/complete | revoked | 先 CAS `revoked_at`，重复 DELETE 204；in-flight upload 的后续 CAS 必须失败并删除对象 |
| expiry/GC | 非 revoked 且时间到期 | expired/revoked materialization | pending/uploading/uploaded 使用 `intent_expires_at`；complete 使用 `expires_at`；先使 read 404，再删除 object/释放 quota |

- content PUT 必须 ownership 匹配、state 可 claim、`Content-Length == ciphertext_size`；Cloud 通过 `MaxBytesReader + LimitReader + TeeReader(SHA-256)` **代理**到单对象存储，不把 presigned URL、object key 或 S3 redirect 返回客户端。
- 每次 claim 使用不同 object key，例如 server 生成的 `artifact-shares/{share_id}/{upload_generation}`；旧 claim 与新 claim 不能覆盖同一对象。takeover 后旧请求即使晚到，其 uploaded CAS 也因 claim/generation 不匹配而失败，只删除自己的 generation object，不能删除新 generation object。
- 第 3 个 generation 仍失败或 server 返回 `artifact_share_retry_exhausted` 时，JCode 不在旧 intent 上继续 PUT；它 best-effort revoke 旧 intent，并仅在用户点击 Retry 后创建全新的 intent/share ID。自动无限重建 intent 被禁止，避免绕过 quota 与制造 orphan。
- upload 成功记录 server-computed ciphertext digest 和 `uploaded_at`；读取超过声明长度、客户端断开或 object store 失败都按 upload failure 处理。
- complete body 只含 `ciphertext_sha256` 与有界 `encrypted_metadata` JSON envelope；必须与 upload 记录一致，才原子切为 complete。
- intent/上传 lease/完成过期都由同一个周期 GC 扫描；进程重启后残留 pending/uploading/uploaded 不能永久占 quota 或留下 orphan object。
- public read 在 complete 之前以及 expired/revoked 之后统一 404。
- DELETE 先原子写 `revoked_at` 使 public read 立即失效，再由 GC 删除 object；撤销幂等。

#### 13.3.4 Cloud domain、migration 与 object lifecycle

新增 migration `0071_device_artifact_shares.sql` 和独立 `domain.ArtifactShare`：

```text
device_artifact_shares(
  id PK, user_id FK users ON DELETE CASCADE,
  device_id FK devices ON DELETE SET NULL,
  artifact_id, revision, protocol, state,
  object_key UNIQUE, upload_generation, upload_claim_id,
  ciphertext_size, ciphertext_sha256,
  encrypted_metadata, intent_expires_at,
  upload_claimed_at, upload_lease_expires_at,
  expires_at, uploaded_at, completed_at,
  revoked_at, object_deleted_at, created_at
)
```

- `artifact_id` 是 JCode 生成的 opaque ID；不存 local session ID、title、relative path、MIME、plaintext size 或 plaintext digest。
- `encrypted_metadata` 最大 64 KiB；ciphertext object 最大为 25 MiB plaintext + v1 envelope overhead。
- memory store 与 PG store 实现同一 create/claim/uploaded/complete/list/revoke/GC interface，并覆盖 CAS 冲突、claim/generation 绑定、complete payload idempotency、lease takeover、intent TTL、orphan object 和 ownership 测试。
- 复用现有 `AttachmentObjectStore` 的 per-object PresignPut/PresignGet/Stat/Delete seam，不向 device/browser返回 object key 或 S3 credentials。
- `upload_generation` 最大为 3，generation object key 可由 `share_id + generation` 确定性重建且永不暴露。reconciler 对 revoked/expired row 枚举 `1..upload_generation` 删除所有代际对象，再写 `object_deleted_at`；失败保留 row 与 generation 上界供下轮重试。历史 row 暂不硬删除，以支持 owner 生命周期 UI 和审计。

#### 13.3.5 `jcode-artifact-share-v1` 加密合同

- `share_key`：32 random bytes；fragment 编码为无 padding base64url：`#k=v1.<43-char-key>`。
- 算法：AES-256-GCM；metadata/content 分别使用独立 random 12-byte nonce，tag 为 GCM 标准 16 bytes。
- AAD 是 UTF-8 精确字节：`jcode-artifact-share-v1\n{share_id}\n{part}\n{artifact_id}\n{revision}\n{plaintext_length}`，其中 `part` 只能是 `metadata` 或 `content`，数字使用无前导零十进制。
- metadata plaintext 是 UTF-8 JSON `{title,relative_path,media_type,kind,size}`。complete request 中 `encrypted_metadata` **明确是 JSON object**：`{"nonce":"<base64url-no-pad 12 bytes>","ciphertext":"<base64url-no-pad ciphertext+16-byte-tag>","plaintext_length":123}`；它作为有界 DB JSON/JSONB 字段保存，不上传对象存储。public metadata endpoint 原样返回该 JSON envelope。
- content wire format **明确是 binary body** `12-byte nonce || AES-GCM ciphertext || 16-byte tag`；不套 JSON、不做 base64。Cloud 只校验总长度与 ciphertext SHA-256，public content endpoint 以 `application/octet-stream` 代理原始字节。
- v1 的硬恒等式是 `ciphertext_size = content_plaintext_length + 12 + 16`，其中 `ciphertext_size` **包含 nonce 与 tag**，intent 的 `ciphertext_size`、HTTP `Content-Length`、实际 body byte length 三者必须完全相等。
- 解密端从 metadata envelope 的 `plaintext_length` 重建 metadata AAD；content plaintext length 固定由 `ciphertext_size - 12 - 16` 推导并重建 content AAD。声明/实际长度不等、长度小于 28、metadata nonce 不是 12 bytes、base64 解码长度不匹配或 GCM tag 失败都拒绝。
- metadata/content 解密失败统一显示 `artifact_decrypt_failed`，不回传 key、AAD 或 plaintext 片段。
- shared test vector 固定 share ID、artifact ID、revision、key、nonce、plaintext、AAD 和输出，Go 与 WebCrypto 两端都必须读取同一文件。

v1 单 envelope 的 plaintext 上限为 25 MiB，不支持 range。未来大文件必须新增 `jcode-artifact-share-v2` chunked envelope，不能改变 v1 nonce/AAD/wire format。

#### 13.3.6 Public share page 与 unauthenticated read

```text
GET /s/{shareID}                                   # Console SPA public route
GET /api/v1/shared-artifacts/{shareID}             # encrypted metadata/lifecycle
GET /api/v1/shared-artifacts/{shareID}/content     # ciphertext stream
```

- `/s/:shareID` 在 React `App` 最外层绕过 `OnboardingGate`、`AppShell` 和 authenticated API providers；不能因为 `/api/v1/me` 为 401 跳转到登录。
- nginx 为 `/s/` HTML 增加 page-scoped CSP、`Referrer-Policy: no-referrer`、`X-Content-Type-Options: nosniff`、`Cache-Control: no-store` 和 `frame-ancestors 'none'`。
- public metadata/content API 不接受 fragment/key，不设置身份 cookie；metadata 响应只含 `share_id/protocol/artifact_id/revision/encrypted_metadata/ciphertext_size/ciphertext_sha256/expires_at`，content 响应只含 raw ciphertext。两者固定 `Cache-Control: no-store`、`Referrer-Policy: no-referrer`、`X-Content-Type-Options: nosniff`，且不返回 object key/presigned URL。
- page 从 `location.hash` 解析 key 后立即用 `history.replaceState` 清理地址栏 fragment；key 只驻留内存。
- WebCrypto 解密后复用安全 renderer contract：Markdown 禁 raw HTML；HTML 使用第二层 `sandbox="allow-scripts"` opaque-origin `srcdoc` iframe，并在原文前注入 network-blocking CSP meta；SVG 不进主 DOM；Office/unknown 只 client-side decrypt download。
- plaintext/Blob 不写 IndexedDB、Cache Storage、service worker、日志或错误遥测；unmount 时 revoke Blob URL。
- expired、revoked、not-found 对公开访问统一返回 404，降低枚举信息；owner device API 仍返回精确 lifecycle。

#### 13.3.7 跨仓库发布顺序

Cloud orchestrator 使用严格请求解码，因此先落 Cloud：migration/domain/store/API/object GC/public share page/test vector，再落 JCode client/UI。Artifact ciphertext 不走 device relay command/event，也不占用现有 32 MiB command body。

发布门禁顺序：Cloud 分支单测与 PG suite → build/push Cloud images → company K8s migration/rollout/public ciphertext smoke → JCode 本地/浏览器 E2E → 真实 Kimi 生成与 `show_artifact` → logged-in share/revoke against deployed Cloud。Cloud 失败时 JCode local Artifact 必须继续通过。

## 14. 失败语义

工具错误必须帮助 Agent 自我修正：

- `artifact path must be relative to the workspace`
- `artifact file does not exist: reports/result.html`
- `artifact path resolves outside the workspace`
- `artifact path is a directory, expected a regular file`
- `artifact type is blocked because it may contain credentials`
- `artifact preview is not available for remote workspaces yet`
- `artifact metadata could not be recorded; retry after the session recorder recovers`

HTTP 使用稳定错误码字段，例如 `artifact_not_found`、`artifact_missing`、`artifact_too_large`、`artifact_forbidden`、`artifact_unsupported`。前端按 code 呈现，不解析英文 message。

分享路径额外使用：`cloud_not_logged_in`、`artifact_revision_conflict`、`artifact_changed`、`artifact_share_too_large`、`artifact_share_quota_exceeded`、`artifact_share_unavailable`、`artifact_share_conflict`、`artifact_decrypt_failed`。Cloud 401 由 JCode 映射为 `cloud_not_logged_in` 并刷新 status，但不得删除本地 Artifact；公开 share 对 not-found/pending/revoked/expired 统一 404。

## 15. 测试方案

### 15.1 Go 单元测试

`internal/tools/artifact_test.go`：

- 正常相对路径登记；
- 缺省 title/kind/focus；
- 同路径 revision 与稳定 ID；
- 不存在、目录、绝对路径、`../`；
- symlink 逃逸；
- blocked credential 路径；
- MIME 与扩展名冲突；
- recorder 失败时不 emit；
- emit 失败时记录仍成功；
- remote 拒绝。

`internal/command/tool_catalog_test.go`：

- Web normal/plan 包含；
- TUI normal/plan 不包含；
- ACP normal/plan 不包含；
- Web remote candidate 不加入。

`internal/runner/approval_test.go`：

- MANUAL mode 中 `show_artifact` 自动批准；
- 加入 allowlist 不会让名称相近的未知工具自动批准。

`internal/web/artifacts_test.go`：

- task/session ownership；
- list hydration；
- missing 状态；
- forged ID；
- content type、CSP、nosniff；
- range/cancellation；
- inline size cap；
- 注册后 symlink swap。
- 并发同路径登记严格得到连续 revision；recorder append 失败不污染 Registry。
- inactive/automation session 从 JSONL + SessionMeta.Project hydrate，Browser 提交的伪造 pwd 不生效。
- background/automation registration 持久化 `ArtifactCount/ArtifactUnseen/ArtifactUpdatedAt`；进程重启后从 JSONL 修复缺失摘要，viewed PATCH 幂等 clear 且不切换 foreground task。
- “Check again” 重新检测 missing/MIME/size；读取过程中越过上限会停止 stream，不泄漏后续字节。

所有涉及 config HOME 的测试必须 `t.Setenv("HOME", t.TempDir())`。

### 15.2 Frontend 测试

- reducer 按 ID/revision 幂等 upsert；
- active task focus 与 background no-focus；
- reconnect list reconciliation；
- panel count/unseen/missing 状态；
- renderer routing；
- HTML sandbox attributes；
- Desktop-only action visibility；
- tool result card 通过 ID 打开。
- Docked / Inline / Focus / Fullscreen 共享 selected ID 与 renderer state；Esc/focus restore。
- 未登录时 Share DOM 不存在；logged-in 的 uploading/shared/stale/expired/revoked 状态不覆盖本地 Viewer。
- automation run Artifact 入口与 unseen clear，不切换 foreground chat task。

### 15.3 Desktop 测试

- 合法 Artifact 的 open/reveal；
- forged task/artifact ID；
- 删除后操作；
- symlink escape；
- Browser 环境无法调用 native action。
- 缺失/错误/非 32-byte `JCODE_DESKTOP_BRIDGE_TOKEN` 时 private route 不注册或 resolve 失败；constant-time auth path 不把 token 写日志，WebView 永远拿不到 canonical path。
- Go startup 读取后 process env 被清除，`execute`/tool 创建的子进程即使显式打印 env 也看不到 bridge token。

### 15.4 端到端验收

至少覆盖：HTML 报告、Markdown 文档、PNG、PDF、CSV、Office fallback；页面刷新恢复；会话切换隔离；后台任务不抢焦点；CLI/ACP 工具 schema 快照不包含 Artifact。Browser E2E 在 1440×900 与 1024×768 检查三种 presentation、keyboard/focus、200% zoom 和 `prefers-reduced-motion`。

真实模型门禁使用 JCode 配置中的 Kimi 模型启动 `jcode web`，从网页发送“生成自包含 HTML 报告与 Markdown/CSV 摘要”；断言真实 tool call 名为 `show_artifact`、Viewer 自动打开、刷新后恢复，且录制的 JSONL/WS 不含文件正文或绝对路径。

### 15.5 Phase 3 Cloud 分享测试

- logged out 时 Share action 不渲染，`show_artifact` 不产生 Cloud 请求；
- logged in + session sync off 仍可显式分享，sync store 保持不变；
- token expired、Cloud offline、upload retry 不影响本地 Registry；
- 上传对象和 metadata 不包含 title/path/content 明文；
- URL fragment 从不进入 Cloud request、日志、analytics 或 referrer；
- Cloud/JCode/share-page 使用同一 E2EE test vector；
- 上传过程中本地文件变化会失败，不生成可读 share；
- revision immutable、expiry、revoke、object GC；
- share size/quota、伪造 ID、跨用户读取与 object key 泄露测试。
- intent/upload/complete 的 strict decode、state conflict、并发 claim、5 分钟 lease takeover、重复 complete/revoke 幂等性；重复 complete payload 不同返回 409。
- lease takeover 使用不同 generation object key；旧 PUT 晚到时 claim/generation CAS 失败且只删除旧对象，active object 与 DB digest 永远一致。
- pending/uploading/uploaded 超过 1 小时 intent TTL 后由 GC 释放 quota 并删除 orphan object；in-flight upload 与 revoke 竞争时不可复活 share。
- metadata JSON envelope 与 content raw binary wire 分别做 golden HTTP 测试；断言 `ciphertext_size = plaintext_length + 28 = Content-Length = actual body length`，public content 必须由 Cloud proxy 且无 redirect/presigned URL。
- public endpoint 在非 complete/revoked/expired 时统一 404；所有公开响应验证 `no-store/no-referrer/nosniff`。
- keyring/file-backend 分离：metadata 文件无 key/full URL，logout 保留、forget 删除本地 secret，Cloud 永远无法恢复 key。
- Go 与 WebCrypto 读取同一 `jcode-artifact-share-v1` vector，逐字节验证 AAD、nonce、ciphertext 和 tag。
- K8s migration/rollout 后实际上传 ciphertext，下载对象与 API/Pod 日志做 plaintext canary 扫描；公开页面在未登录浏览器完成解密、预览、下载、expiry 与 revoke。

## 16. 实施顺序

### Step 1：领域与 transport boundary

- 定义 model、registry interface、tool schema。
- 增加 Web-only tool policy 和 registration tests。
- 确认 CLI/ACP schema 快照不变。

### Step 2：持久化与服务端

- 增加 session artifact entry/recorder。
- 实现 registry hydration/upsert。
- 实现 list/content/download API。
- 实现 `artifact_upserted` event。

### Step 3：Web UI

- 增加 state、WS bridge、API client。
- 增加 Artifacts panel、Viewer 和工具结果卡片。
- 完成 HTML/Markdown/text/image/PDF/CSV renderer。

### Step 4：Desktop

- 增加安全的 open/reveal commands。
- 在 Tauri 环境显示原生 actions。

### Step 5：Hardening

- 完成安全测试、大文件和 reconnect 测试。
- 跑 `go test ./...`、`make lint-web`、`make build-web`、`make lint`。

### Step 6：Phase 3 Cloud 分享（跨仓库）

- 先在 Cloud sibling repository 实现 schema、device API、object lifecycle、E2EE test vectors 和分享页。
- 完成 Cloud unit/PG/browser 测试并部署 K8s，确认 strict contract 与 public route 可用。
- 再在 JCode 实现 logged-in gate、本地 share service/secret store 和 Viewer actions。
- 验证未登录、session sync off、connector off、离线和 token 过期降级。
- 以真实 Kimi Web session 创建 Artifact，并对已部署 Cloud 完成 share/copy/open/revoke 的跨仓库 E2E。

## 17. 预计改动地图

后端：

- `internal/artifact/*`
- `internal/tools/artifact.go`
- `internal/tools/artifact_test.go`
- `internal/session/session.go`
- `internal/web/artifact_adapter.go`（只适配 `artifact.Service`，不拥有 Registry）
- `internal/web/artifacts.go`
- `internal/web/server.go`
- `internal/handler/web.go`
- `internal/command/tool_catalog.go`
- `internal/command/web.go`
- `internal/runner/approval.go`
- `internal/cloud/artifact_share_client.go`
- `internal/cloud/artifact_share_store.go`

前端：

- `web/src/lib/types.ts`
- `web/src/lib/api.ts`
- `web/src/lib/ws.ts`
- `web/src/app/wsBridge.ts`
- `web/src/app/store.ts`
- `web/src/App.tsx`
- `web/src/components/RightPanel.tsx`
- `web/src/components/TopBar.tsx`
- `web/src/components/DesktopTitlebar.tsx`
- `web/src/components/artifacts/*`

Desktop：

- `desktop/src-tauri/src/*` 中的 bridge token、command 注册与原生 open/reveal
- `web/src/lib/useDesktop.ts`

Cloud sibling repository：

- `orchestrator/internal/store/migrations/0071_device_artifact_shares.sql`
- `orchestrator/internal/domain/artifact_share.go`
- `orchestrator/internal/store/artifact_shares*.go`
- `orchestrator/internal/api/device_artifact_shares.go`
- `orchestrator/internal/api/shared_artifacts.go`
- `orchestrator/internal/reconcile/*` 的 object GC wiring
- `console/src/pages/SharedArtifactPage.tsx` 与安全 renderer
- `console/nginx/default.conf.template` 的 `/s/` headers
- Cloud/JCode 共用的 protocol test vector

不应改动：

- `internal/command/interactive.go` 的 tool registration
- `internal/command/acp.go` 的 tool registration/protocol
- `internal/handler/handler.go` 的通用 handler contract
- `internal/tui/*`

## 18. 风险与取舍

| 风险 | 取舍/缓解 |
| --- | --- |
| 任意 HTML 带来 XSS 或本地 API 访问 | opaque-origin sandbox + CSP + 禁止同源/导航/联网 |
| Artifact 变成第二个 Files 浏览器 | 只接受显式登记，不扫描工作区 |
| ephemeral WebSocket 丢失 | JSONL 是事实来源，reconnect 通过 list/replay 对账 |
| Desktop opener 扩大本地执行面 | 只接受 task + Artifact ID，每次重新校验，不提供通用 path opener |
| 文件变化但本地 revision 不自动增加 | list/content 实时 stat 并明确本地 revision 是登记代数；Cloud 分享前读取有界 snapshot 并校验文件 identity/digest |
| 后台任务抢 UI | 只有 active task 可响应 focus，其他任务只标未查看 |
| transport 以后重构误暴露到 ACP/TUI | 静态路径用注册列表测试，Tool Search 路径用 catalog policy，最终再做 schema snapshot test |
| Remote 读取一次性吃入大文件 | MVP 不注册；先补 bounded streaming Executor 契约 |
| 登录后自动上传造成隐私意外 | `show_artifact` 永远本地；只有用户点击 Share 才上传 |
| 公开分享无法使用账号 CEK | 每个 share 使用独立 Artifact Share Secret，key 只放 URL fragment，Cloud 只存 ciphertext |
| 分享链接内容随 workspace 变化 | 上传绑定 revision + digest，生成不可变 Cloud 快照 |
| Cloud sync 与单文件分享耦合 | share 记录不依赖 `device_sessions`，不得修改 session sync 开关 |
| fragment key 泄露到日志/分析 | no-referrer + 禁止记录完整 URL + 前端错误上报清洗 + e2e 测试 |

## 19. 明确保留的后续问题

以下不阻塞 MVP，但实施 Phase 2/3 前需要单独决策：

- 是否允许用户从 Files 面板手动“标记为 Artifact”；
- Artifact revision 是否需要内容快照，还是继续指向工作区当前文件；
- 多文件 HTML/site 是否采用 manifest；
- XLSX 是浏览器内渲染还是只提供 Desktop 外部打开；
- Cloud 分享链接是否需要密码、一次性访问或账号访问控制等额外策略；
- 25 MiB 以上 Artifact 的 chunked E2EE/range 协议；
- Remote Executor 的 range/stream 标准接口。
