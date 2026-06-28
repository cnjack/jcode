# jcode Web 任务化架构设计（任务型 + 并行 + 多项目）

> 状态：草案（已与维护者讨论定稿核心决策，待实现）
> 对标形态：ZCode 桌面端 —— 会话即"任务"，多任务并行，跨项目同时查看/运行。

## 目标

把 jcode 的 **web 前端 + web 服务端** 从「单会话 / 单项目 / 一次一个 agent」演进为：

- **任务型（task-centric）**：会话即任务，可置顶 / 归档 / 未读。
- **并行**：多个任务可同时运行各自的 agent。
- **多项目**：侧边栏一眼看到所有项目及其最近任务，并能跨项目并行运行。

## 已定决策

1. **改在 web 编排层（方案 B）**：一个任务 = 一份"引擎实例"；并行**只发生在 web 包内**。内核（`session` / `handler` / `runner`）与 TUI / ACP / CLI **一律不动**——它们仍各用一份单跑引擎。
2. **术语（三层，对齐 ZCode）**：
   - **Workspace（工作区）** = 侧边栏容器，装所有 Project。
   - **Project（项目）** = 仓库/文件夹（tpm、jcode…）。← UI 这层明确叫 Project。
   - **Task（任务）** = 某个 Project 下的一次对话/工作线程；置顶/归档/未读/并行都作用在这层。`New chat` → `New task`。
   - 存储与代码保持 `session`（JSONL 转写不变）。只改文案，不改底层。
3. **并发**：**不设上限**、不排队。jcode 是本地单人工具，不是多人服务，没必要限制。
4. **项目模型**：每个任务**自带 pwd**（创建时绑定）。去掉"全局当前项目"这一权威态；UI 的"当前项目"只是**新建任务的默认值 + 列表筛选器**。
5. **资源切分**：
   - **每任务一份**：`agent` / `history` / `recorder` / `pwd` / `ctx+cancel` / 审批计数器 / 终端（pty）
   - **共享一份**：MCP 连接（工具执行时带任务自己的 cwd）、技能 loader、模型配置 —— v1 先共享，后续按需 per-task
   - **审批**：per-task；后台任务需要审批时走**托盘/通知**冒出来，不抢当前任务焦点

## 核心模型

```
Server (编排层，只剩传输/HTTP/WS + 共享资源)
 ├── tasks    map[taskID]*Engine     // 每个活跃任务一份引擎
 ├── projects map[path]*ProjectCtx   // 按项目缓存 env / 项目技能
 ├── wsBroker  (带按 task_id 的订阅过滤)
 └── 共享: skillLoader, mcpManager, modelConfig, ptyMgr(按 task 分桶)

Engine  // = 今天 Server 的那些单例字段，按任务实例化一份
 ├── taskID, projectPath(pwd)
 ├── agent, history
 ├── recorder (该任务的 JSONL)
 ├── ctx, cancel
 ├── handler: WebHandler(taskID)   // 发事件时盖上自己的 task_id
 └── approvalCounter, todo/goal 快照

Task (持久层/元数据，对用户即"任务")
 ├── id(=sessionUUID), projectPath, title
 ├── pinned, archived, unread, status(idle/running/done/error)
 └── createdAt, updatedAt  + 一份 session JSONL 转写
```

关键点：**`AgentEventHandler` 接口不变**——每个任务给一个自己的 `WebHandler` 实例，由它在 emit 时盖 `task_id`，所以 TUI/ACP 共用的接口零改动。

## 服务端改造

- 从 `internal/web/server.go` 把单例字段（`running / agent / history / recorder / pwd / runCancel`）抽进 `Engine`，`Server` 改持 `map[taskID]*Engine`。
- 去掉 `handleChat` 的 `CompareAndSwap` 单跑门；`POST /api/chat` 带 `task_id`（缺省则建新任务）。
- `handleStop` → `/api/stop`（带 `task_id`），查表取对应 `cancel`。
- per-task 审批：审批计数器/待办表下沉到 `Engine`，审批 ID 带 `task_id`，回传也带。
- 切项目不再全局拆重建：任务自带 pwd，文件/exec/diff/git 命令都用 `task.pwd`。

## WS 协议

- 每个事件加 `task_id` 字段。
- 客户端 `subscribe { task_ids }` / `unsubscribe`；`WSBroker.Broadcast` 按订阅过滤（现在是全广播，多任务会风暴）。
- 旧客户端忽略未知 `task_id` 字段，平滑兼容。

## 持久化与元数据

- `SessionMeta` 增加：`Pinned / Archived / Unread / Status / UpdatedAt`。
- 任务创建即落盘元数据（现在是首条消息才建文件）。
- 索引文件 `session.json` 加 `version` 字段，做向后兼容迁移。
- `ListAllSessions()` **已存在但从未被调用**（死代码）→ 接 `GET /api/tasks`（跨项目只读列表），点亮侧边栏多项目树。

## Git（走技能，不做手动 git UI）

- **暴露状态**：`envinfo.GitBranch` 其实已在内存里，只是没 API 返回 → 新增 `GET /api/workspace`（或扩 `/api/status`）返回 `branch / dirty / diff 统计`，修掉 `TopBar.vue` 写死的 `null`。
- **提交/推送/开 PR**：一个 `submit-pr` 技能（+ 一个 commit/pr 工具）。你一句话触发 agent 跑它，**不做手动 git 按钮**。

## 分期落地

### Phase 0 — 零架构改动、并行无关的价值（先做）
- [ ] 暴露 git branch/status（`/api/workspace`）→ 修 TopBar 的 null 分支
- [ ] 接 `ListAllSessions` → `GET /api/tasks` 跨项目只读 → 侧边栏**多项目任务树**
- [ ] `SessionMeta` 加 `pinned/archived/unread/status/updatedAt` + 端点 → 右键菜单 + 未读
- [ ] `submit-pr` 技能（+ 工具）
- [ ] 命令面板 ⌘K、输入框 `$` 技能 / `#` 关联、浏览器通知（纯前端）

### Phase 1 — 任务键化（后端，仍串行）
把单例抽成 `Engine`、按 `task_id` 路由、WS 带 `task_id`、前端 store 改 `Map<taskId, TaskState>`，并发先按 1 跑通——把"拆单例"和"真并行"解耦验证。

### Phase 2 — 真并行（无上限）
放开并发、per-task 审批路由、WS 订阅过滤、任务自带 pwd、注意内存/句柄回收。

### Phase 3 — 并行 UX
运行中任务托盘、per-task 未读/通知、并行审批界面。

## 最大风险

真并行会放大三件事，Phase 1 先串行键化就是为了在没有并发压力时把它们打磨好：
1. **审批串扰** —— 必须 per-task 路由。
2. **WS 广播风暴** —— 必须订阅过滤。
3. **资源占用** —— 不设硬上限，但要做内存/文件句柄/PTY 的回收。
