# jcode Automations（自动化）PRD

> 状态：草案 **v2**（2026-06-23，关键决策已定，待实现）
> 对标形态：Anthropic Claude Code 的 **Automations** —— 用 agent 处理「按节奏 / 手动触发」的重复性工作。
> 关联：[[web 任务化架构]]（`internal-doc/web-task-architecture.md`）、Goal（`docs/goal.md`）、Mode 选择器（Ask/Plan/Autopilot）、Skills、MCP。
>
> v1→v2 变更：①去掉事件/GitHub 触发（先不碰 gh）②去掉 Effort/推理力度 ③新增 agent 可调用的 `automation_create` 工具 + 渲染卡片 ④调度器定为「文件锁选主」⑤存储定为「flock 写锁 + 易变态分离」⑥运行时不加护栏（沿用既有哲学）。

---

## 1. 一句话定义与背景

**Automations = 让一条 jcode 任务（session）按定时 / 手动自动跑起来。**

它**不是新引擎**：一次运行 = 一条普通 jcode session（带转写、可 resume、可看 diff、可通知），只多打 `automation_id` 标签。新增的只有「定义 + 触发器 + 调度」三件，外加一个让 **agent 自己也能创建自动化** 的工具+卡片。

底座已就绪：任务型 web + 并行 Engine（每任务一份）+ 多项目 + 跨前端共核（TUI/Web/ACP/CLI）+ Goal + Mode + Skills + 远程工作区。

参考截图三屏：自动化列表页、新建自动化弹窗、模板页（逐元素映射见 §4）。

---

## 2. 目标 / 非目标

### 目标
- 创建、编辑、启停、手动运行、查看历史一条自动化。
- 触发：**定时**（Hourly/Daily/Weekly + 时分）+ **手动**（Run now）。
- **全前端一等公民**：定义/调度/历史下沉核心层（`internal/automation` + `~/.jcode/`）；Web/桌面完整 UI；CLI `jcode automation …`；TUI `/automation`；ACP 最小能力。
- **agent 可创建**：新增 `automation_create` 工具，agent 能从自然语言生成自动化草稿，**经用户在卡片上确认后**落库。
- **模板** + **技能转自动化**。
- 运行结束有通知（复用现有通道）。

### 非目标（明确不做）
- **事件/GitHub 触发**：先不碰 gh，整体推迟（不在本 PRD 实现范围）。
- **真·云端执行**：jcode 是本地工具。`RunInCloud` 字段保留（恒 false）但 v1 **不渲染 Cloud tab / 不放死开关**，改用「coming soon」tooltip。
- **常驻守护进程**：调度器跑在 `jcode web` 进程内（App 开着才触发）。`jcode daemon` + launchd/systemd 是后续承接「App 关闭也跑」的路径，不在 v1。
- **运行时安全护栏**：不做全局总开关 / 单次 MaxTurns·超时 / 强制审计通知。沿用既定哲学「Autopilot 接受全部风险、不设护栏」；安全把关只发生在**创建处**（§8 human-in-loop），armed 之后全信任。
- **推理力度（Effort）选择**：去掉。
- 并发上限 / 排队（沿用「本地单人工具，不设上限、不排队」）；多用户/团队共享；自动化间 DAG 编排。

---

## 3. 两个硬约束（研究证实，约束全局设计）

1. **无人值守的定时运行在结构上只能是 Autopilot/full_access。** 没有 WS 客户端连着时，Ask/Plan 的审批请求会永远阻塞（`internal/handler/web.go:410` 的 `RequestApproval` 仅靠 ctx 取消解开；full_access 在 `internal/runner/approval.go:253` 直接 auto-approve）。所以「定时跑」恒等于「自动批准一切的 full_access 跑」——这是结构所迫，不是偏好。
2. **桌面 App 一关，web 进程即被杀**（`desktop/src-tauri/src/main.rs:178` 显式 `child.kill()`，每次开 App 重起 sidecar）。任何进程内调度器在 App 关闭后都不跑；要「关 App/关机也跑」只有守护进程一条路（v2 之后）。

---

## 4. 截图逐元素 → jcode 映射

### 屏 A：新建自动化弹窗
| 截图元素 | jcode 映射 | 备注 |
|---|---|---|
| Name | `Automation.Name` | — |
| Trigger（Daily 下拉） | `Trigger.Type=schedule` + `Cadence`（hourly/daily/weekly） | — |
| Hours / Minute | `Trigger.Hour/Minute`（weekly 再带 `Weekday`） | 本地时区 |
| Run in the cloud（开关） | `RunInCloud`（恒 false） | v1 不放死开关，改 tooltip「coming soon」 |
| Prompt（`Type / for skills`） | `Automation.Prompt` + `/` 唤起技能 | 复用 `GET /api/slash-commands` 补全 |
| Autopilot（左下） | `Automation.Mode`（Ask/Plan/Autopilot） | **schedule 触发强制 Autopilot**（见 §3、§7.4） |
| Select project | `Automation.ProjectPath` | **必填**；空 = 无人值守不支持 → skip+停用（§7.5） |
| Claude Sonnet 4.6 | `Provider`/`Model` | 留空=全局默认 |
| ~~High（推理力度）~~ | **去掉** | — |
| 「Without a project … quick chat」 | `ProjectPath==""` | jcode **不支持**无人值守跑（§7.5），与截图分歧 |
| Cancel / Create / Create and run | `POST /api/automations`（可带 `run_now`） | — |

### 屏 B：自动化列表页
| 截图元素 | jcode 映射 |
|---|---|
| 左侧导航「Automations」 | Sidebar 新增一级入口 |
| Tabs：All / Local / ~~Cloud~~ | v1 **砍掉 Cloud tab**（保留字段，留待云端） |
| Your automations 卡片（名/节奏徽标/prompt 预览/最近运行/▶） | `GET /api/automations`；▶ = `POST …/{id}/run` |
| Recent runs（按日期分组、状态、时间戳） | `AutomationID != ""` 的 session 子集 |
| 搜索框 | 前端过滤 |

### 屏 C：模板页
| 截图元素 | jcode 映射 |
|---|---|
| 6 张模板卡（带 Daily/Weekly/Manual 徽标） | 内置模板（embed），点卡→预填新建弹窗 |
| Skills 区「Turn an existing agent skill into an automation」 | 列 `GET /api/skills`，选一个→预填 `prompt=/<skill>` |

---

## 5. 已定决策（汇总）

| # | 决策 | 来源 |
|---|---|---|
| D1 | 执行**仅本地**；`RunInCloud` 留字段、UI 砍死 tab/死开关 | owner |
| D2 | 触发 = **定时 + 手动**；事件/gh 推迟 | owner |
| D3 | **全前端一等公民**：核心层承载定义/调度/历史 | owner |
| D4 | 调度器 = **文件锁选主**（`~/.jcode/automation-scheduler.lock`）；并把这把锁复用为存储写锁 | owner |
| D5 | `automation_create` 工具 = **human-in-the-loop**：走 `ask_user` 式阻塞回路，用户在卡片确认才落库 | owner |
| D6 | **运行时不加护栏**（无总开关/无单次上限/无强制审计） | owner |
| D7 | 存储 = **flock 写锁 + 易变调度态与用户定义分文件** | owner |
| D8 | 去掉 Effort | owner |
| D9 | schedule 触发**强制 Autopilot**（Ask/Plan 会 hang，§3） | 研究结论 |

---

## 6. 核心概念与数据模型

运行历史复用既有 Task/Session，新增模型只有「定义」「易变态」「模板」。**用户定义与高频调度态分两个文件**（D7：避免调度器的频繁写与人工编辑互相覆盖）。

```go
// internal/automation/types.go（新 core 叶子包，仿 internal/mode）

// —— 用户定义，存 ~/.jcode/automations.json（人工低频写）——
type Automation struct {
    ID          string   `json:"id"`
    Name        string   `json:"name"`
    Prompt      string   `json:"prompt"`
    Trigger     Trigger  `json:"trigger"`
    ProjectPath string   `json:"project_path"` // 必填本地路径；空 → 禁止 headless 跑（skip+停用，§7.5）
    Mode        string   `json:"mode"`         // approval|plan|full_access；schedule 触发强制 full_access
    Provider    string   `json:"provider,omitempty"`
    Model       string   `json:"model,omitempty"`
    RunInCloud  bool     `json:"run_in_cloud"` // v1 恒 false
    Enabled     bool     `json:"enabled"`
    Source      string   `json:"source"`       // manual|template:<id>|skill:<name>|agent
    CreatedAt   string   `json:"created_at"`
    UpdatedAt   string   `json:"updated_at"`
}

type Trigger struct {
    Type    string `json:"type"`              // "schedule" | "manual"
    Cadence string `json:"cadence,omitempty"` // "hourly" | "daily" | "weekly"
    Hour    int    `json:"hour,omitempty"`    // 0-23 本地
    Minute  int    `json:"minute,omitempty"`  // 0-59
    Weekday int    `json:"weekday,omitempty"` // 0=Sun..6=Sat
}

// —— 易变调度态，存 ~/.jcode/automation-state.json（调度器高频写）——
type RunState struct {
    LastRunAt     string `json:"last_run_at,omitempty"`
    LastStatus    string `json:"last_status,omitempty"`  // success|error|running
    LastSessionID string `json:"last_session_id,omitempty"`
    NextRunAt     string `json:"next_run_at,omitempty"`  // 纯函数算出，落盘防漂移
    LastFiredSlot string `json:"last_fired_slot,omitempty"` // date+H+M 去重键（防 DST 回拨重跑）
}

// —— 运行历史：给 session.SessionMeta 加两字段（不另建存储）——
//   AutomationID string  // 关联的自动化
//   TriggerKind  string  // scheduled | manual
//   「Recent runs」= ListAllSessions() 里 AutomationID != "" 的子集；
//   普通任务列表反过来排除 AutomationID != ""（§7.6）。
```

**模板**（内置 embed，仿内置技能）：

```go
type Template struct {
    ID, Name, Description, Badge string // Badge: Daily|Weekly|Manual（展示）
    Prompt   string
    Trigger  Trigger
    SuggestMode string
}
// 初始 6 个对齐截图：issue-triage / changelog-draft / repo-audit /
//                     perf-improvements / a11y-audit / cost-tips
```

**单一校验**（仿 `ValidateGoalObjective`，`internal/tools/goal.go:308` 那句"every entry point 共用一个校验"）：`automation.ValidateAutomation(a)` 是**所有创建路径（弹窗 HTTP / agent 工具 / CLI）唯一的校验入口**。

---

## 7. 功能需求

### 7.1 自动化列表页
卡片（名/节奏徽标/prompt 预览/最近运行+状态/▶）；Tabs **All / Local**（无 Cloud）；Recent runs 按日期分组、点进=打开该 session 转写；顶部「New automation」「Browse templates」+ 搜索。

### 7.2 新建/编辑弹窗
字段：Name、Trigger（节奏+时分）、Prompt（`/` 技能补全）、Mode、Project（**必填**）、Model。`RunInCloud` 开关→tooltip「coming soon」。「Create」保存；「Create and run」保存并立即手动跑一次。**无项目不允许保存**（提示需选项目，§7.5）。**Trigger=schedule 时 Mode 锁/警示为 Autopilot**（§3）。删除/启停走卡片右键。

### 7.3 触发类型
- **定时**：hourly/daily/weekly + 时分，本地时区。`NextRunAt` 由**纯函数** `ComputeNextRun(now, trigger, tz)` 每次触发后重算并落盘（可单测，避开沙箱时钟限制）。错过窗口（笔记本休眠等）**默认跳过不补跑**。DST「回拨」靠 `LastFiredSlot`（date+H+M）去重防双触发。
- **手动**：列表 ▶ / CLI / TUI `run`；不经调度器锁，任意前端进程直接 `StartRun`。

### 7.4 运行语义（关键）
- 一次运行 = 调 `internal/web` 的 `buildLocalEngine` 工厂建一条 Engine（按 `pwd/mode/provider/model`）→ `submitMessage(prompt)` 跑到结束（后台 goroutine，落 session JSONL，与用户手点任务同路径）→ 打 `automation_id`/`trigger_kind` 标签。
- **schedule 触发恒 full_access**（§3 硬约束）。manual 触发若有客户端连着，可用 Ask/Plan（用户在场能答审批）。
- **不加运行时护栏**（D6）：不设单次 MaxTurns/超时、无全局总开关。仅有的边界是既有 runner 轮次上限。
- 完成信号：`NotifyingHandler.SetDoneNotifier`（`internal/handler/notifying.go:49`）；完成发普通通知（native/浏览器/WeChat/BLE），**非强制审计**。
- 触发前**预检 `ProjectPath`**：路径缺失（被删/移走/未挂载）→ 跳过本次、`LastStatus=error`+原因、发失败通知；连续 N 次缺失→自动 `Enabled=false` 止损（防每晚重复空跑）。

### 7.5 无项目自动化：禁止 headless 跑（skip + stop）
`ProjectPath==""` 时 headless 没有可继承 pwd（`activeEngine().pwd` 回退为空，`server.go:1020`），full_access 会落到启动 cwd 跑 `edit/rm`（机制确认：`env.go:98/215` 空 pwd→`cmd.Dir` 未设→launch cwd，逃逸守护失效）——**风险不可接受**。**决策（owner）：不做 scratch 兜底**，改为 **`ProjectPath` 为空/缺失 → 跳过本次 run + 停用该自动化（disable）+ 通知**。`ValidateAutomation` 对 schedule 触发**要求非空本地 ProjectPath**（与 §10.4 远程路径拒绝合并）；弹窗 Project 必填、空不让存。截图的"无项目=quick chat"在 jcode 无人值守场景**不支持**。

### 7.6 运行历史不污染主列表
`ListAllSessions()`（`internal/session/session.go:873`）是主侧栏与自动化页共用源。**一个谓词两处用**：主任务列表**排除** `AutomationID!=""`，自动化页**只收** `AutomationID!=""`。保留策略：每条自动化只留最近 N 条运行（独立于用户会话清理）。

### 7.7 模板 & 技能转自动化
模板页 6 卡，点卡→预填弹窗（含默认节奏/建议模式）。Skills 区列 `GET /api/skills`（可搜），选一个→预填 `Prompt=/<skill>` + 默认 daily + Autopilot。

---

## 8. agent_create 工具 + 渲染卡片（human-in-the-loop）

让 agent 从自然语言创建自动化（"以后每天早上帮我跑测试并总结失败项"）。**安全闸门在创建处**：工具不直接落库，走 `ask_user` 式阻塞回路，用户在卡片上确认/编辑后才提交（D5）——挡住 prompt 注入静默造一条"每天自动批准"的后门。

**机制**（照搬 `ask_user` 的 请求→卡片→resolve 阻塞回路，非 goal 那种被动 banner）：
1. agent 调 `automation_create`（参数=解析出的 name/prompt/trigger/project/mode）。
2. 工具 handler 调 `WebHandler.RequestAutomation(ctx, draft)` → emit `automation_request`（带唯一 id）→ **阻塞在 channel**（仿 `RequestAskUser`，`internal/handler/web.go:283-318/515-532`）。
3. WS → `AutomationCard.vue` 渲染草稿预览 + Confirm/Edit/Cancel。
4. 用户 Confirm → `POST /api/automations`（带 request id）→ `ResolveAutomation(id, draft')` → 经**唯一** `automation.Store.Create`+`ValidateAutomation` 落库 → 解开 channel，工具返回「已创建」。Cancel → 工具返回「用户取消」。
5. 弹窗路径与工具路径**共用同一个 `Store.Create`**（仿 goal 单校验），唯一差别是"谁点的 Create"。

**新增/改动文件**（研究已勘定）：
- 新增 `internal/tools/automation.go`（`automation_create` 工具 + 草稿类型）。注意：工具落库的是 `internal/automation.Store`，不在 tools 包重造存储——tools 包仅持一个对 Store 的引用（挂在 `tools.Env`，仿 `Env.GoalStore`，`internal/tools/env.go`）。
- 注册：两处 `buildAllTools` 各加一行 —— `internal/command/web.go:286-313`、`internal/command/interactive.go:82-110`（全前端自动获得）。
- `internal/handler/web.go`：加 `WebAutomationRequestData` + `RequestAutomation`/`ResolveAutomation`（镜像 ask_user）。
- `internal/web/server.go`：`POST /api/automations` 兼作 resolve（带 request id 时）。
- 前端：新增 `web/src/components/AutomationCard.vue`（仿 `AskUserCard.vue`）；`ws.ts` 加 `automation_request` 派发；`stores/chat.ts` 加 `onAutomationRequest`/`submitAutomation`；`types/api.ts`、`api.ts` 加类型与端点。

---

## 9. 技术设计

### 9.1 新 core 包 `internal/automation`（叶子包）
`types.go`（§6）、`store.go`（读写两个 json + flock + version 迁移）、`scheduler.go`（选主+循环）、`validate.go`（`ValidateAutomation`）、`templates/`（embed）。不依赖 web/tui；各前端注入「如何跑一条 run」的 `Runner` 回调（避免 import 环，与 mode/goal 同模式）。

### 9.2 存储（D7）
- `~/.jcode/automations.json`（定义，低频人工写）+ `~/.jcode/automation-state.json`（易变态，调度器高频写）。
- **跨进程写锁**：所有写者（任意进程）先抢 `~/.jcode/automation.lock`（flock/`syscall.Flock`）再 read-modify-write。这把锁与调度选主锁**复用同一基础设施**。理由：`session.json` 只有进程内 `sync.Mutex`（`session.go:590-599`，注释自承「lost updates, last one wins」），`config.json` 是裸 `os.WriteFile`（`config.go:408-427`）——都不能跨进程；自动化是跨前端多进程并写，必须 flock。
- 不塞进 `config.json`（整文件覆盖写，并发更差）。

### 9.3 调度器：文件锁选主（D4）
- `~/.jcode/automation-scheduler.lock`（flock）。任意常驻进程启动时尝试抢锁：**赢家**跑定时循环；**输家**仅管理定义+手动跑（手动不经锁，直接 `StartRun`）。owner 优先级：未来 `jcode daemon` > `jcode web`。v1 实际就是 `jcode web` 持锁。
- 启动清理 stale lock（崩溃残留）；`ctx.Done()`/退出释放。
- 循环：每 ~30s tick，扫 enabled 且 schedule 的自动化，`NextRunAt<=now` 且 `slot!=LastFiredSlot` → 预检 pwd → `StartRun(scheduled)` → 重算落盘 `NextRunAt`+`LastFiredSlot`。
- **不排队、不限并发**（沿用既定原则）；同刻多触发=多 Engine 同起，惊群风险**接受**（与 web 任务化"不设上限"一致）。

### 9.4 Runner（复用 Engine，headless）
`internal/automation` 定义 `Runner` 接口，`internal/web` 实现：
```
StartRun(a Automation, kind) (sessionID, done<-chan error):
  if engineCount() >= cap → 记 LastStatus=error + 通知, 直接返回   // 无 idle-evict, §10.1 C1
  eng := buildLocalEngine("", resolvePwd(a), "full_access")   // schedule 恒 full_access；resolvePwd 见 §7.5
  设 provider/model；工具集剔除 ask_user + automation_create  // headless 不可交互/不可再造, §10.1 C2
  SetDoneNotifier(→ 写 terminal status + deleteEngine(eng) + 通知 → done)
  sessionID := submitMessage(eng, a.Prompt, ...)  // 后台跑，立即返回
  // 落 JSONL 不依赖 WS 客户端（事件泵无背压，server.go:303-318）
  stampSessionMeta(sessionID, a.ID, kind)         // 写 AutomationID/TriggerKind
```
关键事实/纠正（研究证实）：headless 跑不需要 WS 订阅者，recorder 同步落盘；完成靠 `SetDoneNotifier`/`OnAgentDone`。**纠正两处原假设（见 §10.1）**：①Engine **没有 idle-evict**（`internal/web` 无 reaper），cap=64 命中即 `errTooManyTasks` 硬失败 → 定时 run **必须在完成回调里 `deleteEngine` 自销**，否则累积泄漏直至静默失败；②full_access 只旁路审批，**`ask_user`/`automation_create` 仍阻塞**（`handler/web.go:490` 无模式旁路）→ headless 下这类工具调用必须 auto-fail，否则 run 永久 hang。

### 9.5 HTTP API（`internal/web/server.go` 新增）
- `GET/POST /api/automations`（POST 带 `run_now` = Create and run；带 request id = resolve agent 草稿）
- `GET/PUT/DELETE /api/automations/{id}`
- `POST /api/automations/{id}/run`（手动，返回 session_id）
- `GET /api/automations/runs[?automation_id=]`（= `ListAllSessions` 过滤）
- `GET /api/automation-templates`
- 复用 `GET /api/skills`、`GET /api/slash-commands`

### 9.6 前端
新增 `views/AutomationsView.vue`（列表：All/Local + Your automations + Recent runs + 搜索）、`AutomationEditorDialog.vue`（弹窗，复用 mode/model/WorkspacePicker 子组件 + cloud tooltip）、`AutomationTemplatesView.vue`、`AutomationCard.vue`（§8）。Sidebar 加入口；`stores/automation.ts`。主任务列表加 `AutomationID` 排除谓词（§7.6）。

### 9.7 CLI / TUI / ACP（全前端一等公民）
- CLI：`jcode automation list|show|create|run|enable|disable|delete`、`templates`。
- TUI：`/automation`（list/启停/手动 run；不在 TUI 内长跑调度）。
- ACP：暴露最小能力（list + run）。
- 三者读写同一 `automation.Store`（经 flock），定义全局一致。

### 9.8 通知
复用 `NotifyingHandler.SetDoneNotifier`（native/浏览器/WeChat/BLE）。无人值守→运行结束默认发通知（成功/失败均发）；非强制审计、无附加要求（D6）。

---

## 10. 边界条件与可靠性（reliability）

> 来源：可靠性/SRE 视角穷举（研究 wf）。戳破了原 PRD 两个**错误假设**（C1/C2），并补全审计与关闭语义。

### 10.1 两处错误假设的纠正（最高优先）
- **C1 · Engine 无 idle-evict、cap 64 硬失败**（原 §9.4 写错）：`internal/web` 无任何时间 reaper（`engine.go:263/277`，`registerEngine` 命中即 `errTooManyTasks`，不腾位）。→ 定时 run 必须**完成即 `deleteEngine` 自销**（throwaway engine，非用户 tab）；cap 命中→记 failed run + 通知，别静默丢。
- **C2 · `ask_user`/`automation_create` headless 照样阻塞**：full_access 只旁路审批（`approval.go:253`），`RequestAskUser`（`handler/web.go:490`）**无模式旁路**会永久 hang。**决策（owner）：自动化 run 的工具集直接 exclude `ask_user` + `automation_create`**（不是运行时 auto-fail，而是 `buildAllTools` 按 run 类型剔除）——agent 根本拿不到这两个工具，既不会 hang 也不会"自动化造自动化"。

### 10.2 审计日志（回答"我有 audit log 吗"）
- **已有·transcript 级（够用）**：每条 run = 一份 session JSONL，逐轮记 user/assistant + tool call↔result（按 toolCallID 配对、带 `error` 字段、毫秒时间戳、append-only 同步落盘）。"这条自动化做了什么"可完整追溯。
- **缺·run-outcome 级（必补）**：`SessionMeta.Status` 只有 idle/running 二态，**没有 success/error 终态、没有 EndTime、没有 error reason 落盘**（`OnAgentDone(err)` 的 err 只进瞬时 WS 事件，不落盘）→ **撑不起截图的 Success/Failed 过滤**。补：`SessionMeta` 加 `EndTime/TerminalStatus/ErrorReason`（+ `AutomationID/TriggerKind`），在 `OnAgentDone` 里 `RecordSessionEnd` 落盘。可选 `~/.jcode/automation-runs.jsonl` append-only 审计（仿 `usage/events.jsonl`，跨进程安全、易过滤）。

### 10.3 关闭/排空（回答"关闭能确认 task 都结束吗"）
- **今天不能，且有 bug**：`jcode web` 收 SIGINT 走 `http.Server.Shutdown(context.Background())` **无超时**、Engine teardown 只等 **1s** 即关 recorder（>1s 的 run 被半切、可能 mid-write）；桌面端 `child.kill()`=SIGTERM 但 Tauri **不等**；web/desktop **无"N 个任务在跑，确认退出？"对话框**（只有 TUI `pickers.go` 有）。
- **stale "running" bug**：进程被切后 `SessionMeta.Status` 永停 `running`，**无 startup reconciliation**（`engine.go:448` 异步写 + 无回收）。
- **补**：①调度 owner 启动时**扫 stale `running`→`interrupted/error`**（按 owning PID/lock 代际）；②给 `Shutdown` 加超时 ctx；③被切的 run 记一条 `run_terminated` marker（区分正常完成/被切）；④web/desktop 退出前若有 in-flight 自动化 run → drain（带超时）或确认对话框。flock 由 OS 在进程死时自动释放（含 SIGKILL），不会死锁 ✓。

### 10.4 其余边界（分级，"还有其他吗"）
| 级别 | 边界 | 处理 |
|---|---|---|
| 关键 | 同一自动化上次没跑完、下个 tick 又到（重叠，racing 同 working tree 的 full_access `git`/`edit`） | **skip-if-running**（`LastStatus==running` 跳过，带 stale 守护防 §10.3 卡死） |
| 关键 | run 永不终止（agent 死循环；无 goal 时连 25 轮上限都没有，`runner.go:110`） | **决策**（§10.5）：scheduled-only 存活上限 vs 接受 engine 泄漏到重启 |
| 高 | 空 pwd full_access 在启动目录跑 `rm`（`env.go:98/215` 空 pwd→`cmd.Dir` 未设→launch cwd，逃逸守护失效） | **owner 定：empty pwd → skip + 停用自动化 + 通知**（不做 scratch 兜底，§7.5） |
| 高 | 选主锁与存储写锁**复用同一把**（D4/D7）→ 长持选主锁饿死短写 | **拆两把**：选主锁长持、存储写锁短持 |
| 高 | 远程(SSH/Docker) `ProjectPath` 被指过来 → Runner 用 `buildLocalEngine` 在本地跑一个错的本地路径 | `ValidateAutomation` v1 **拒绝非本地 ProjectPath** |
| 中 | prompt 里 `/skill` 已禁用/删、model/provider 已删/key 没了、MCP 挂了、配额/限流/断网 | 统一**触发前预检 + 连续失败 N 次自动禁用**（把 §7.4 的 pwd 预检扩成通用） |
| 中 | DST **春进**（02:30 这种 slot 当天不存在，原只处理了秋退去重） | `ComputeNextRun` 必须落到真实 instant；纯函数单测覆盖两次 DST 切换 |
| 中 | 时钟/NTP/时区跳变致 `NextRunAt` 卡死或漂移 | 每 tick 用 `ComputeNextRun(now,…)` **重算**，存值只当去重提示 |
| 中 | agent_create 卡片的 resolve 被伪造（毒 prompt `curl` localhost 自确认） | request_id 绑**服务端 nonce**，不进工具输出、agent 不可见 |
| 低/UX | 通知风暴（hourly = 24 条/天） | **owner 定：成功/失败全通知**（接受 hourly 量；嫌多可后续加合并） |
| 低 | corrupt `automation-state.json`、磁盘满 mid-transcript、编辑 schedule 与 run 回写互踩 | 解析失败→重建空 state 不崩启动；run 回写**只碰 state 文件、不碰定义文件**（D7 分文件天然帮到） |

### 10.5 决策（owner 已定 4 项 / 待定 1 项）
- ✅ **无项目**：empty/缺失 pwd → **skip + 停用 + 通知**（不做 scratch）；Project 必填、非本地路径拒绝（§7.5）。
- ✅ **headless 工具**：自动化 run 工具集 **exclude `ask_user` + `automation_create`**（§10.1 C2）。
- ✅ **通知**：成功/失败**全通知**（接受 hourly 量）。
- ✅ **退出语义**：关闭时 in-flight run **短超时 drain，超时标 interrupted**（§10.3）。
- ⏳ **待定 · 存活上限**：scheduled run 要不要加宽松 wall-clock 上限防永久 hang？这是**存活**护栏（防 run 卡死泄漏 engine），非你拒掉的**安全**护栏。建议加（仅 scheduled，如 30min）。

---

## 11. 分期落地

### Phase 0 —— 核心 + 手动（无 UI 也有值）
`internal/automation` 包（types/store 双文件+flock/validate/模板 embed）；`SessionMeta` 加 `AutomationID/TriggerKind/TerminalStatus/EndTime/ErrorReason` + 索引 version 迁移；`Runner` 接口 + web 实现（复用 Engine、完成即 `deleteEngine` 自销、工具集剔除 ask_user/automation_create、empty-pwd→skip+停用）；`RecordSessionEnd` 落终态；CLI `jcode automation …`；完成通知。

### Phase 1 —— Web UI + 定时调度 + agent 工具
列表页/弹窗/模板页/技能转自动化（cloud tooltip）；调度器（flock 选主）+ 定时（hourly/daily/weekly，纯函数 NextRunAt + slot 去重 + pwd 预检）；`automation_create` 工具 + `AutomationCard`（human-in-loop）；HTTP API 全量；Sidebar 入口 + 主列表排除谓词；Recent runs。

### Phase 2 —— 其余前端 + 守护进程承接
TUI `/automation`、ACP 最小能力；`jcode daemon`（launchd/systemd）作为更高优先级调度 owner，承接「App 关闭也跑」；（事件/gh 触发、真云端仍后续再议）。

---

## 12. 决策记录 + 仍开放的小问题
**已按推荐定（P2 默认）：** 无项目→skip+停用（不做 scratch，§7.5）；主列表排除自动化 run（§7.6）；项目缺失跳过+止损（§7.4）；DST `LastFiredSlot` 去重（§7.3）；不排队、惊群接受（§9.3）；删/禁用进行中的 run "让它跑完、只停后续"；resume 自动化 run = 首条人工消息起 fork 成普通会话（保留 `AutomationID` 溯源、不再重发通知）；砍 Cloud tab、留字段。

**仍开放（实现时定）：**
1. 每条自动化运行历史保留 N 的具体值。
2. 连续 pwd-缺失自动禁用的阈值 N。
3. manual 触发 + Ask/Plan：是否在列表 ▶ 时要求当前有 web 客户端连着（否则也会 hang）→ 倾向：▶ 仅 web 内可用且自动连当前客户端；CLI/TUI 的 `run` 强制 full_access。

## 13. 验收 / 测试
受 [[jcode e2e 沙箱限制]]：live server 沙箱内绑不了 socket → 用 in-process `httptest` + 预置 json 测 API；`ComputeNextRun` 抽纯函数单测（不依赖系统时钟，**覆盖 DST 春进/秋退两次切换**）；flock 单 owner：起两个进程断言只触发一次。可靠性单测：skip-if-running（上次 running 时不重叠触发）；完成回调 `deleteEngine` 自销（engine 数回落）；自动化 run 工具集**不含** `ask_user`/`automation_create`；empty/缺失 pwd → skip+停用；startup 把 stale `running` 扫成 interrupted；终态 `TerminalStatus` 落盘可重建 Success/Failed。冒烟（桌面/本地手动）：建 daily → Create and run → 侧栏出现 run（且**不**进主列表）→ 收完成通知 → Recent runs 有记录、状态过滤 Success/Failed 生效；让 agent 说一句话→出 AutomationCard→Confirm→落库。

## 14. 风险
1. **重复触发**（多前端进程）→ flock 选主（§9.3）。
2. **prompt 注入造后门** → 创建处 human-in-loop 卡片是唯一提交边界（§8）+ resolve 绑服务端 nonce（§10.4）。
3. **engine 泄漏致静默失败**（无 idle-evict + cap 64 硬失败）→ 完成即 `deleteEngine` 自销（§10.1 C1）。
4. **headless 工具阻塞致永久 hang**（`ask_user`/`automation_create`）→ 自动化 run 工具集直接 exclude 这两个（§10.1 C2）。
5. **关闭切断 run + stale "running"** → startup reconciliation + drain/超时 + terminated marker（§10.3）。
6. **Success/Failed 审计撑不起** → 补 `TerminalStatus/EndTime/ErrorReason` + `RecordSessionEnd`（§10.2）。
3. **无人值守 full_access 无护栏**（D6 主动接受）→ 风险集中在创建处把关；运行处沿用既有哲学。文档讲清。
4. **进程不常驻**致定时漏触发 → v1 接受（App 开着才跑）；Phase 2 daemon 承接。
5. **空 pwd full_access 误操作** → empty/缺失 pwd 直接 skip + 停用，不做 scratch 兜底（§7.5）。
6. **运行历史膨胀** → 主列表排除 + 每条保留 N（§7.6）。
