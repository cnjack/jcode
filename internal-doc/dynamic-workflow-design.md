# jcode Dynamic Workflow —— 落地设计（开工前考虑）

> 状态：开工设计（2026-07-05）。对标 Claude Code Dynamic Workflows / Qoder CLI Workflows。
> 架构已锁定（见 memory `jcode dynamic workflow roundtable`）：**goja 薄壳（纯控制流、零 I/O）+ 手写 Go 执行核**；`agent()` 复用 adk agent 执行机制（v1 的并发/取消用**信号量+goroutine**，非 `SubagentTaskManager`——后者后续可接入统一，见 §6）；非交互编排器 + 确定性 + journaled resume。
> 所有 jcode 侧签名均经真实源码核对。

---

## 1. 一句话

用户/agent 写一个 `.js` 脚本（`export const meta = {...}` + 纯 JS 主体 + top-level await），jcode 用 goja 在后台事件循环里执行它；脚本用 `agent()/parallel()/pipeline()/phase()/log()/workflow()` 把工作 fan-out 到子 agent，中间结果留在脚本变量里，只把最终结果收回会话。三端（TUI/Web/ACP）都能编辑、调用、直接生成 workflow，并实时看 phase/agent 进度。

---

## 2. e2e flow（典型用户旅程）

### F1 · 直接生成并运行（最重要，差异化）
1. 用户在 TUI/Web 说「用 workflow 审计这个仓库的 auth 模块」。
2. 主 agent 用一个内置工具 `workflow_run`（或 `workflow_generate`）**现场写一段 `.js`**（plan lives in code），把脚本 + meta 展示给用户。
3. 用户 Confirm（或看脚本、改反馈、取消）。→ 非 headless 下这是**唯一人工闸门**（挡 prompt 注入）。
4. 引擎后台启动，返回 run ID；用户可继续用会话。`/workflows` 面板实时显示 phase→agent 树 + log。
5. 完成：最终结构化结果 summarize 回主对话；脚本可一键存成 `.jcode/workflows/<name>.js`（saved command）。

### F2 · 调用已存 workflow
- 自然语言「run the repo-audit workflow for the auth module」→ 按 `meta.whenToUse`/name 匹配 `.jcode/workflows/*.js`。
- 或斜杠命令 `/repo-audit auth`（loader 自动生成 `/name`，args 从命令行尾传入 → 脚本全局 `args`）。
- headless：`jcode flow run repo-audit --args '{"area":"auth"}'`。

### F3 · 编辑 workflow
- TUI/Web「Workflows」管理页：列出 built-in + user + project 的 `.js`，可查看/编辑源码、看最近 run。
- 每次 run 的脚本也落盘到 session 目录，agent 拿到路径 → 可 read/diff/relaunch。

### F4 · headless / 定时（复用 automation）
- `automation Source="flow:<name>"`：定时触发一个 workflow run。
- headless 下**强制非交互**：脚本本就不能拿用户输入；子 agent 的审批走 full_access（见 automation 的 D9）。

### F5 · resume
- run 中途停/失败 → 会话内 resume：已完成的 `agent()` 调用按 `prompt+opts+runID` 命中 journal 缓存返回，其余 live 重跑。确定性守卫（`Date.now/Math.random` 抛）保证缓存不被污染。

---

## 3. 三端：编辑 / 调用 / 生成

事件核心：新增 `flow.EventSink` 接口（前端无关），三端各一个实现。run 进度事件（run_start / phase / agent_start / agent_done / log / run_done）经 EventSink 播出。

| 能力 | headless CLI | TUI | Web | ACP |
|---|---|---|---|---|
| **调用** | `jcode flow run <name\|file> [--args]` | `/name` 斜杠 + 自然语言 | `/name` + 自然语言 + Workflows 页「Run」 | prompt 内自然语言 |
| **生成** | agent 工具 `workflow_run`（写 .js→确认→跑） | 同左 + 确认卡片 | 同左 + 确认卡片（仿 AutomationCard） | 同左（ACP 无富卡→退化为文本确认） |
| **编辑** | 直接编辑 `.jcode/workflows/*.js` | Workflows 面板查看/打开编辑器 | WorkflowsView.vue 源码编辑 | N/A（编辑器客户端自己开文件） |
| **进度** | stdout 行 | `/workflows` 面板（仿 team panel） | WS `flow_progress` → WorkflowsView 树 | ACP session/update 文本 |

**接线点（verbatim 已勘）：**
- Web 事件：`internal/handler/web.go` 加 `OnFlowProgress(data)` → `h.emit("flow_progress", WebFlowProgressData{...})`；`internal/web/engine.go:startPump` 自动 `WSBroker.Broadcast(WSEvent{Type,TaskID,Data})`；前端 `web/src/lib/ws.ts` handlerMap 加 `flow_progress`。
- Web API：`internal/web/server.go` 加 `GET/POST /api/workflows`、`GET /api/workflows/runs`、`POST /api/workflows/{name}/run`（仿 automations）。Vue：`stores/workflow.ts` + `components/WorkflowsView.vue` + App.vue `activeView` 加 `'workflows'` + Sidebar 入口。
- TUI：`internal/tui/messages.go` 加 `FlowProgressMsg`；`internal/tui/update.go` 加 `case FlowProgressMsg`（仿 `SubagentProgressMsg`）；`internal/command/interactive.go` 加 `s.flowProgress(...)` → `s.p.Send(...)`；斜杠复用 `skillSlashCommands` 模式，新增 flow slash 源。
- 命令：`internal/command/workflow.go` 新建 `NewWorkflowCmd()`（`Use:"workflow" Aliases:["workflows","flow"]`），`cmd/jcode/main.go:47` 注册。
- 生成工具：`internal/tools/workflow_tool.go` `workflow_run`，注册进 web/TUI/ACP 三处 `buildAllTools`；headless run 里排除 `ask_user`/`workflow_run`（仿 automation 的工具剔除）。

**生成路径的安全闸门**：`workflow_run` 工具**不 headless 直跑未确认脚本**——非 headless 走 `ask_user` 式确认回路（emit `workflow_request`→前端卡片→Confirm→执行）；headless（automation）下工具集直接剔除该工具（agent 拿不到，防注入后门）。这与 automation 的 D5 同构。

---

## 4. UI 渲染

### /workflows 面板（TUI + Web 同构信息架构）
```
▶ repo-audit  ⣾ running   3/6 agents · 12.3k tok · 00:18
  ▸ Scan        ✓ done      (1 agent)
  ▸ Analyze     ⣾ running   ├─ audit:auth      ⣾  read×4, grep×2
  │                          ├─ audit:session   ✓  8 steps
  │                          └─ audit:tokens    ⣾  running
  ▸ Summarize   … pending
  log: 3/6 areas audited …
```
- 层级：run → phase → agent；每个 agent 显示 label + 状态 + 最近工具/步数 + token。
- 复用 TUI team panel 的 `TeamViewState` 范式做 `FlowViewState`（AppendFlowLine / RefreshRuns）。
- Web 用 WorkflowsView.vue：run 卡片 + 可折叠 phase/agent 树 + log 流；WS 增量更新。

### 确认卡片（生成路径）
- 展示 meta（name/description/phases）+ 脚本源码（可折叠）+ Run/View/Reject/Cancel。仿 `AutomationCard.vue`/`AutomationEditorDialog.vue`。

### 状态色
- pending 灰 / running 橙(accent) 脉冲 / done 绿 / failed 红 / stopped 黄。沿用现有 token 契约（accent-wash 等）。

---

## 5. 引擎架构（执行核）

```
internal/flow/
  types.go     Meta / Workflow / RunOptions / RunInfo / EventSink / AgentSpec / AgentResult / event structs
  loader.go    扫 .jcode/workflows + ~/.jcode/workflows（项目优先）；parseMeta；SlashCommands()（镜像 skills.Loader）
  engine.go    Engine：Compile→Start loop→run wrapped program→阻塞 done/ctx；caps(16/1000)+watchdog Interrupt；journal
  host.go      host 函数 agent/parallel(内)/pipeline(内)/phase/log/workflow/__flowResolve/__flowReject；async 桥（NewPromise+RunOnLoop）
  prelude.js   parallel/pipeline（Promise.all 之上）+ 确定性守卫（Date.now/Math.random/无参 new Date 抛）
  spawn.go     SpawnFunc：CloneForSubagent + 按 agentType 建工具 + ModelFactory.GetModel + adk.NewChatModelAgent + 运行收文本；opts.schema→prompt 注入 schema + 提取 JSON + 重试（v1 best-effort，不强校验 schema 符合性，见 §8）
  spawn_test.go / engine_test.go / loader_test.go
```

**async 桥（关键正确性）**：
- `loop := eventloop.NewEventLoop()`；`loop.Start()` 常驻（`run(true)`：jobCount 从 1 起，直到 `Stop()`；用 `Run()` 会在在途 goroutine `RunOnLoop` 前提前退出——已核 eventloop.go run()）。
- `agent(prompt,opts)`（在 loop goroutine 执行）：`p,resolve,reject := vm.NewPromise()`；`go func(){ 取 sema(16) → runSubagent → loop.RunOnLoop(func(vm){ resolve/reject(marshal) }) }()`；`return vm.ToValue(p)` 立即返回 → loop 空出来跑其它 thunk → 真并行。**resolve/reject 与任何 goja.Value 只在 loop goroutine 碰。**
- 顶层：源码转换成 `globalThis.__flowMain = async () => { <src(把 export 去掉)> }; __flowMain().then(v=>__flowResolve(v), e=>__flowReject(e));`（top-level `return` 因包在 async 里而合法）。`__flowResolve/__flowReject`（loop 上执行）`Export` 结果 → 送 Go `done` chan。主 goroutine `select{done / ctx.Done→loop.Terminate}`。
- 取消/超时：watchdog goroutine 到点 `vm.Interrupt("timeout")`；ctx 取消 → `loop.Terminate()`（清 timers + 拒后续 RunOnLoop）。

**caps**：并发信号量 16（agent goroutine 内 acquire）；总量原子计数 1000，超则 reject。两者 `log()` 出被丢弃/排队情况，不静默截断。

**结构化输出 opts.schema（v1 实际做法）**：spawn 把 schema 注入 prompt（"只返回符合此 schema 的 JSON"），`extractFlowJSON` 从回复里提取首个合法 JSON 值；提取失败 re-prompt 重试 ≤2；超限 reject（`error_max_structured_output_retries`）。**注意：v1 只保证是合法 JSON，不强校验其是否符合传入 schema（见 §8 已知限制）。** 升级路径 = 合成工具 `submit_structured_output`（params=schema，需 eino schema 转换 + 真校验）。

**journal（resume）**：每次 `agent()` 按 key=`sha(prompt+opts+runID)` 写 `~/.jcode/flow-runs/<runID>/journal.jsonl`；resume 时命中即返回缓存，未命中 live 跑。确定性守卫保证 key 稳定。

---

## 6. 与三端复用映射

| flow 需要 | 复用 jcode 现成 | 动作 |
|---|---|---|
| agent 执行单元 | `subagent.go` runFn 范式（adk.NewChatModelAgent+ag.Run） | flow spawn.go 用 Env 的 exported New*Tool 自建，不耦合 subagentTool |
| 并发+取消 | `SubagentTaskManager`（可选） | v1 直接用信号量+goroutine，够用；后续可接 TaskManager 统一 |
| 模型 | `ModelFactory.GetModel(ctx,"provider/model")` + `NewChatModelFromProvider` | fallback=会话默认 model；opts.model 覆盖 |
| Env | `tools.NewEnv` / `CloneForSubagent` | 每 agent 一个 childEnv |
| loader | `skills.Loader` 全套范式 | 镜像：scanDir/parseFrontmatter/SlashCommands/项目优先 |
| 触发 | `automation Source` 可扩展 | `flow:<name>` → Scheduler.Runner 分派 |
| 事件 | `handler.emit`/`WSBroker`/tui `p.Send` | EventSink 三实现 |

---

## 7. 分期（对齐 goal 顺序）

- **P0（本次核心）**：`internal/flow` 引擎 + spawn + loader + prelude + headless `jcode flow run/list` + 单测/in-process e2e。可跑 `.js`、真并行、结构化输出、确定性抛错、caps、取消。
- **P1**：TUI `/workflows` 面板 + slash + 生成工具确认卡片。
- **P2**：Web API + WorkflowsView + WS flow_progress + 生成确认卡片。
- **P3**：ACP 文本进度 + automation `flow:` 触发 + journal/resume 硬化 + `isolation:'worktree'`。
- **P4**：built-in workflow（repo-audit / deep-research / roundtable）实测 + site 文档。

红线：headless 非交互（不 askUser）；生成脚本必须过确认闸门；caps/超时先于放量；signature 以 Claude Code 文档为准并写测试锁定。

---

## 8. 对抗审核结论 + 已知限制（2026-07-05）

23-agent 对抗审核（6 维 finders + 逐条 verify）抓出 13 条确认项。**已修 3 个真 bug（带回归测试 + `-race` 验证）：**
1. **超时不解阻塞主 select（critical）**：watchdog 原只 `vm.Interrupt`，loop 空转（agent 卡死）时主 goroutine 永久阻塞。→ watchdog 改为 `r.finish(timeout err)` + interrupt；`TestTimeoutUnblocksIdleLoop`。
2. **vmReady 握手无 ctx 守卫**：`vm := <-vmReady` 理论可挂 + ctx 提前取消未处理。→ `select{vmReady / ctx.Done}`，`defer loop.Terminate()` 前移到 Start 后。
3. **stripExports 误删字符串内行首 export**：`(?m)^\s*export\s+` 会命中多行字符串里的 `export`。→ 正则收紧要求后跟声明关键字；`TestExportInsideStringNotStripped`。

**已评估为 by-design / 可接受的已知限制（记录不改）：**
- **loop 终止后 settle 被丢（0/1/3）**：仅发生在 run 已 finish/cancel、Go 调用方已拿到结果、VM 正销毁之际——不可观测。不变量：主 goroutine 必经 `r.done` 或 `ctx.Done` 解阻塞（修复 1 保证），之后的 late settle 是无害 no-op。已加注释。
- **flowTools 不含 workflow_run（8）**：**故意**——防 tool 级无限递归；嵌套只走 `workflow()` 原语，一层深。
- **结构化重试 × 模型 MaxRetries=3（9）**：有界（≤3×3），非"无界"；可接受。
- **结构化输出不校验 schema 符合性（12）**：v1 用 `extractFlowJSON` 取任意合法 JSON，不验证是否符合传入 schema。升级路径：合成 `submit_structured_output` 工具（需 eino schema 转换）。
- **nested workflow budget 不共享（11）**：子 workflow 各自 1000 cap，不计入父预算。nested 罕见、深度已限 1，follow-up。
- **matchBrace 不识别正则字面量（7）**：meta 里写正则字面量会误判 `//` 为注释。**约束：meta 必须是 JSON-like 纯数据**（name/description/whenToUse/phases 皆字符串/数组），不放正则/计算值。
