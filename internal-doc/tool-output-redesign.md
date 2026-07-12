# Tool Call 输出重设计:批次组 + 展示改进

> 状态:Wave 1 开发中(2026-07-12)。本文是设计与跟踪文档。

## 1. 背景与问题

目标效果(Claude Code desktop):同一条 assistant 消息并发发出的多个 tool call,聚合成一叠状态行——每行 = 状态动词 + 命令摘要 + 实时耗时,可独立展开;完成的行就地翻转状态。

jcode 之前做的 `groupExploringTimeline`(packages/jcode-ui-core/src/timeline/groupExploring.ts)是"相邻 + 只读类型"聚合成 Exploring 摘要卡,语义不同:它做的是**降噪**(压缩探索过程),而截图要的是**结构**(并发批次作为一个单元,每行保留身份)。两者不冲突,应当并存。

### 现状关键事实(2026-07 调研)

- **链路单一**:eino ADK → internal/runner/runner.go → `AgentEventHandler`(internal/handler/handler.go)→ TUI / Web / ACP 实现。
- **批次边界在 runner 里天然存在但被丢弃**:streaming 路径 runner.go:342-348 把同一条 assistant 消息的 tool_use 成批发出;非 streaming :351 对单条消息循环。`OnToolCall(name, args, toolCallID)` 三参数装不下批次信息。
- **TUI 真 bug**:handler/tui.go 丢弃 toolCallID,结果回填靠 replaceLastToolIcon 从末尾找 running 图标(LIFO 位置匹配),并发批量时结果与图标错配。
- **TUI 无单工具耗时**(仅整轮结束显示总时长);无分组;普通工具输出静态截断(头 6 行)不可展开(仅 subagent 有 ctrl+e)。
- **Web** `ToolCall` 类型无 batchId/messageId;耗时 badge 用 meta.duration_ms(仅 execute 有)。

## 2. 三家对比(codex / opencode / Claude Code)

| | 分组依据 | 特点 |
|---|---|---|
| Claude Code | 批次/相邻堆叠 | 每行保留身份 + 实时耗时,不看工具类型 |
| codex | 语义类型(Read/List/Search 全只读才进组) | 组内二次折叠(相邻 Read 合并 `Read a, b, c` + 去重);探索组持续吸附;普通命令永远独占;行内**不放**耗时/exit code(靠 bullet 变色 + Ctrl+T transcript) |
| opencode(web) | 相邻 + 类型(read/glob/grep/list) | "Gathering→Gathered context" 前缀形变动画 + 动画分类计数("3 files read, 2 searches");TUI 不合并,靠间距紧排;耗时 >2s 才显示 |

其他值得借鉴(详见 §4):codex 的 head/tail 双向截断、行感知截断、孤儿 end 事件保护、审批时暂停计时、reduced-motion 收口;opencode 的拒绝=删除线、turn 级 changed files 汇总、Task 子行进度、todo 移出时间线。

## 3. 批次组设计(Wave 1,开发中)

### 协议契约

Go 接口改结构体参数(破坏性,全仓内改):

```go
type ToolCallEvent struct {
    Name, Args, ToolCallID string
    BatchID    string    // 同一条 assistant 消息一个 id
    BatchIndex int       // 批内序号 0-based
    BatchSize  int
    StartedAt  time.Time
}
type ToolResultEvent struct {
    Name, Output, ToolCallID string
    Err      error
    Duration time.Duration // runner 计算
}
```

- 注入点:runner.go streaming :342 / 非 streaming :351,每个 `mo` 生成一次 BatchID;runner 记 map[toolCallID]start 算 Duration。
- WS 契约(snake_case):`tool_call` 增 `batch_id` / `batch_index` / `batch_size` / `started_at`(unix ms);`tool_result` 增 `duration_ms`。

### Web/UI 库(jcode-ui-core 0.3.0 / jcode-ui 0.3.0)

- `ToolCall` 增 batchId/batchIndex/batchSize/startedAt;新 ThreadItem kind `'batch'`(ToolBatchGroup{id, batchId, tools, status, explorative})。
- `groupToolTimeline`:同 batchId 聚组(≥2 才成组,approval 不打断组、原位渲染);无 batchId 回退现有 exploring 相邻聚合(老会话兼容);组内全只读 → explorative。
- `ToolBatchGroup` 组件:explorative → 升级版 Exploring 卡(分类计数摘要 + Read 合并去重);否则扁平行堆栈无组头,每行 ●/✓/✗ + title + subtitle + 耗时 + 独立展开(复用 toolRenderers registry)。running 行实时耗时(1s tick);完成行耗时 >2s 才显示;error 行显示 exit code。
- Thread.tsx mapItems 换 groupToolTimeline;所有现有导出保持兼容。

### TUI

- ToolCallMsg/ToolResultMsg 带 ToolCallID/BatchID/StartedAt/Duration;**id → 行映射精确回填图标**(修 LIFO bug,无 id 回退旧逻辑)。
- 完成行尾追加暗色耗时(>2s;失败行总是显示)。
- BatchSize>1:批次头行(`⏺ Running 3 tools` → 完成翻转 `✓ Ran 3 tools`),成员行缩进;结果盒随到随追加不变。
- 实时秒表(tick 重绘 running 行耗时)留到后续波次。

## 4. 改进清单与波次

### Wave 1(已完成,2026-07-12)
- [x] P0-1 TUI toolCallID 精确回填(id→行映射,校验失败回退旧逻辑)
- [x] P0-2 协议层 ToolCallEvent/ToolResultEvent + runner 注入 + WS 字段;批次字段持久化进 session Entry(batch_id 用 `b<epoch>-<seq>` 防跨进程撞车),回放两端免费获得分组
- [x] P1-3 Web ToolBatchGroup 批次组(复用 ToolCallCard slots.header → registry 渲染;useElapsed 1s tick)
- [x] P1-4 Exploring 升级(Read 合并去重 + summarizeExploringCounts 分类计数)
- [x] TUI 批次堆叠(批次头行翻转 ✓/✗)+ 完成行耗时(>2s,失败行 Duration>0 总显示)
- [x] 版本:jcode-ui-core@0.3.0、jcode-ui@0.3.0 已 publish(2026-07-12,pnpm publish;tarball 校验含新导出,依赖字段 workspace:* 已正确转换)

### Wave 2(TUI 部分完成,2026-07-12)
- [x] P1-5 TUI 输出截断升级:head/tail 双向(尾部常含错误信息)+ 行感知截断(wrap 后按实际行数分配预算,ansi.Truncate 不切 grapheme);文案统一 `… +K lines (ctrl+t transcript)`(internal/tui/format.go)
- [x] P2-7 TUI ctrl+t 全屏 transcript(行内永远静态截断 + 引导文案;pager 放完整输出/耗时/错误全文;**快照式,无 live tail**;team 会话下 ctrl+t 仍归协调面板,transcript 走 ctrl+o)(internal/tui/transcript.go)
- [x] P2-8 审批语义:denied 信号全链路(runner approvalMeter 按 toolCallID 记录,非字符串匹配;WS `denied` + `approval_request.tool_call_id`;session Entry 持久化)。web:denied=删除线+muted+"Denied" 徽章(三处一致),等审批=warning 黄+`approval…`;TUI:denied=muted `⊘ denied`(非红),不渲染拒绝样板输出盒,批次头不因 denial 翻 ✗。Duration 已扣除审批等待(clamp 0)。剩余:TUI 等审批行黄色高亮(有审批对话框顶着,低优)、AGUI adapter 未映射 denied(随 cloud 接入补)
- [x] P2-9 状态行结构化:spinner + Working + 耗时(审批期间冻结)+ `esc interrupt`(esc 已接 cancel 确认)+ 1 行 detail(`└ Shell: …` / `N tools running`)(internal/tui/content_render.go)

### Wave 3(按需)
- [x] P3-10 turn 级改动汇总(web,2026-07-12):聚合纯函数在 jcode-ui-core(timeline/turnChanges.ts:`summarizeTurnChanges` 按 file_path 去重保留最后一次、跳过 denied/error、回合内有 running 工具则不产出、上限 10 超出进 overflow;`diffStatForTool` 前端从 args 推 ± 行数——后端不发 diff 统计:edit=old/new_string 行数、multi-edit=edits 求和、write=content 行数记新增;`appendTurnChangeSummaries` 以 user message 为回合边界在回合末插入 `turnchanges` ThreadItem,isRunning 时最后一回合不插,seq=末项+0.5)。组件 jcode-ui `TurnChangesCard`:头 `Changed N files` + 绿红 `+A −R` 徽章,展开每文件一行(路径+±),点行经 ToolCallCard slot-header 展开该次调用的 registry diff 正文(与批次行同一复用路径),超出 10 个收进 "… N more"。Thread mapItems 管道 groupToolTimeline 后追加一步,web 零改动自动生效。单测 18 例全绿(turnChanges.test.ts);**不 bump 版本**,两包 CHANGELOG 记 Unreleased
- [x] P3-11 subagent 并行进度(TUI,2026-07-12):事件源本就带 name(Notifier/ProgressFn),仅 SubagentTokenFn 补了 agentName 参数;TUI 由单数字段改为按 name 分槽(`subagentSlots []*subagentSlot`,internal/tui/subagent_state.go)。live 盒:单 subagent 保持经典尾部布局("… (N earlier steps)" 计数改用总步数,存储行 tail-cap 32 不再撒谎);多 subagent 每活跃槽一节(名字 + `↳` 最近 1-2 步),完成槽收成 `✓ name · N steps · 时长` 单行,全部结束才清空。时间线完成行升级为 `✓ Subagent name · N steps · 时长`(无数据回退旧文案);AgentDone/取消时清槽防泄漏。盒缓存改 rev 计数键
  - [x] P3-11 web 部分(2026-07-12):ToolCallCard SubagentHeader 运行中显示 `↳ 当前工具 title subtitle`(children 里最后一个 running 的,shimmer-running 动效;无 running 子项时保持 Running 标签);完成后右侧显示 `N toolcalls · 时长`(时长优先 meta.duration_ms——web store 对所有工具都会把事件级 duration 合并进 meta,没有则在 running→done 转换时用 startedAt 冻结一次,均无则只显示 N toolcalls)。批次组行里的 subagent 走 slot-header(BatchRowHeader),展开子项路径经 ToolCallView 不受影响、维持 Wave 1 行为;fixture 加了 running subagent 演示。**不 bump 版本**,CHANGELOG 记 Unreleased
- [x] P3-12 todowrite 收出时间线(2026-07-12):侧栏 todo 面板已存在(sidebar_component.go,resume 从 state 恢复 store),时间线收敛为单行 muted 摘要 `✓ Todos N/M · <in-progress 标题>`(formatTodoWriteOutput 解析两种 tool 输出格式,未识别格式回退首行);call 行 subtitle 由 extractToolDisplayInfo 生成 `N/M · 当前任务`(不再 dump todos JSON,web 同步受益);回放 EntryTodoSnapshot 同一单行格式。注:目标示例里的 `+2` 增量需要跨调用状态,纯函数渲染层拿不到,以 `N/M · 当前任务` 替代
- [x] P3-13 MCP 格式(2026-07-12):MCP 工具名无前缀(server 原名直出),故在 tools 层加 tool→server 注册表(RegisterMCPToolServer/MCPServerForTool,LoadMCPTools 与 MCPManager.doConnect 双路注册,热重载覆盖写);extractToolDisplayInfo 兜底分支命中注册表时 Title=`server.tool`、Subtitle=紧凑化 JSON args(json.Compact + 80 rune 截断),icon=mcp;TUI 与 web 都吃 display_info 同步受益。未注册的未知工具保持原兜底
- [ ] P3-14 动效工程化:web 动画计数/流式配速(未做)
  - [x] TUI shimmer(2026-07-12):Working 动词余弦光带(codex shimmer.rs 范式),逐字符在 colorDimText↔colorText 间 blend(internal/tui/shimmer.go),不引入新色相、主题安全;相位取 time.Now(),重绘骑现有 spinner tick 无新 timer;reduced-motion:`JCODE_REDUCED_MOTION` 或 NO_COLOR 置静态文本

### Wave 4:活动组折叠布局(用户拍板,2026-07-12,开发中)

用户反馈:Wave 1-3 的分组**布局**不对——Exploring 组步骤行常驻铺开 + 展开后另起盒子重复列一遍;相邻单工具散落组外。要的是 Claude Code / Codex app 的折叠形态。拍板的规范:

1. **相邻聚合**:相邻 tool 项(之间无 assistant/user 消息)一律进同一"活动组",不分只读/写、不分并发批次;approval 原位渲染不打断组;孤立单工具不成组
2. **完成后收起为单行组头**:分类计数 `Ran 3 commands · read 2 files`(全只读 → `Explored: …`);失败在收起态必须可见(error 图标 + `1 failed`),denied 追加 muted `1 denied`
3. **展开 = 边框卡**,内部每工具一行、行内再展开完整输出(registry 渲染);废除"第二份重复列表"
4. **运行中自动展开** live 行(实时耗时),全部完成自动收起;用户手动操作过则尊重手动状态
5. exploring/batch 两种旧 kind 从 Thread 管道退役(导出保留 deprecated);统一为 `activity` kind

另:回滚了一轮未经确认的内容级修改(git 写命令误判只读的分类收紧 + 摘要动词)——用户决定不做,如后续要做需单独立项。execute 计数桶(git 命令数成 "searches" 的问题)在 activity 组头的 summarizeActivityCounts 里按 commands 桶处理。

进度:**Wave 4 全部完成(2026-07-12)**。
- web/desktop:core 60 单测 + fixture 浏览器四形态截图验证;`groupActivityTimeline`/`summarizeActivityCounts`/`countActivityFlags`/`ActivityGroupCard`/共享 `ToolRow`。
- TUI 结构化行改造(internal/tui/activity_group.go):`activityGroupData`(成员列表 + rev 缓存键)替代字符串回填——live 形态(头行+成员行,数据更新自然翻转)/ 收起形态(单行计数摘要,桶与 web 对齐,failed/denied/interrupted 语义);结果盒退出时间线(错误摘要例外),完整输出进 ctrl+t transcript(renderTranscriptGroup 全展开);归组不变式 = "组行仍是最后一行"+ groupBatches 路由(approval 静默批准不关组,拒绝行插入即关组);回放重建收起态;旧字符串回填机制保留作无 ID 回退。live 实时耗时未做(完成时补耗时)。
- 待办小尾巴:TUI live 头行 spinner/实时耗时;真机跑一轮验证。

### 已知坑(实现时注意)
- 并发聚合的**孤儿 end 事件**:不相关的完成事件不能错误合并进活跃批次(codex ExecEndTarget 三态防御)。
- 回放/老会话无 batchId → 必须优雅降级为现状渲染。
- npm 发布必须 `pnpm publish`(workspace:* 泄漏问题)。
