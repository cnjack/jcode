# jcode 工具/压缩/Prompt 审计（对比 Claude Code & codex）

> 2026-07-05。方法：12 个主题横向对比 jcode / Claude Code（TS 重建源）/ codex（Rust）三库源码，
> 候选问题逐条回源对抗性核验（60 处确认 gap → 去重合并 32 条），按"无人值守长程任务影响"排序。
> 本文档同时作为修复跟踪表：每条带【状态】字段（待复核 / 确认-待修 / 已修 / 有更优解 / 暂缓-原因）。

## 总体结论

jcode 在三个维度上与 CC/codex 存在系统性护栏缺失，几乎全部集中打击长程无人值守运行：

- **工具层（21 条）最重**：同步 execute 输出、read 返回内容、并行工具批次均无字节/token 上限，
  唯一的 50000 字符裁剪建立在 eino `len/4` 假 token 估算之上；超时只杀 bash 直接子进程，
  后台任务无法取消，长跑累积孤儿进程与占用端口。
- **压缩层（5 条）有一个一行级致命 bug**：占用率提醒用累计 PromptTokens 判占用率，
  compact 之后永久卡 >100% 一直逼模型收尾；另有粗糙 500 字符策略抢先于 eino 高保真摘要器。
- **prompt 层（6 条）**：缺并行工具指令、缺验证纪律；env/git/日期/AGENTS.md 启动时冻结、
  外部改动文件不主动回灌，多小时运行基于陈旧世界模型行动。

核验中确认 jcode 做得对的（伪问题已剔除）：压缩触发是 model-aware 的真 provider token 计数
（ResolveContextLimit 单源、支持 1M）；用户取消已被 runner ctx.Done 预检干净处理；
edit 无匹配报错已做 curate（whitespace 提示、最相似行）。问题集中在"护栏缺失"而非"设计错误"。

## Quick Wins

1. reminder.go:59 `tokenUsage.Get()` → `GetLastTotal()`（一行，消除 rank 1）
2. 删除/降级 interactive.go:262-272 前置粗糙 ThresholdCompactionStrategy（rank 14）
3. execute/read 加 head+tail 字节上限 + 丢弃计数标记 + TaskLog spill（rank 2/5/13/15）
4. 移植 hooks/proc_unix.go 的 Setpgid+Kill(-pid) 到 tools exec 路径（rank 12）
5. reduction.Config 注入真 tokenizer TokenCounter（rank 3）
6. system.md 补并行工具调用、验证纪律章节（rank 18/19）
7. edit/write 对未 TrackRead 的存量文件硬拒绝（rank 7）
8. glob 换 `rg --files --glob --sort=modified`（一次修 rank 8/9）

## 问题清单（rank 1 → 32）

### 1. 占用率提醒用累计 token，compact 后永久卡满 【compaction·high】
- 证据：internal/agent/reminder.go:58-59; internal/prompts/reminders.go:75,89; internal/agent/compaction.go:187-190
- 问题：reminder.go:59 用 m.tokenUsage.Get() 取累计 PromptTokens 算占用率，而 agent 每个工具循环都重发整个窗口，累计值无界增长，几次工具调用后就越过 0.85 触发 token_critical 逼模型收尾。ResetContext() 只重置 LastTotalTokens 不重置累计账本（chatmodel.go:215-229 注释明确要求保留），compact 过一次后占用率永久 >100%。
- 对比：CC/codex 用最近一次真实 API 调用的总 token 占用（last-call total），压缩后归零。
- 修复建议：reminder.go:59 改用 m.tokenUsage.GetLastTotal()，与 compaction.go:189 同源。
- 状态：已修（reminder.go 改用 GetLastTotal；测试 internal/agent/reminder_test.go TestReminderOccupancy*，修前精确复现 147%/475% 卡满）

### 2. 同步 execute 输出完全不设上限、无截断标记 【tools·critical】
- 证据：internal/tools/env.go:304-312; internal/tools/execute.go:130-174; internal/agent/middleware.go:84-99
- 问题：LocalExecutor.Exec 把 stdout/stderr 捕获进无界 bytes.Buffer 原样返回；execute.go 构建结果零长度检查；middleware.go（所有工具结果唯一咽喉点）不加天花板；同步路径无 TaskLog 落盘。
- 对比：CC 对 bash 输出 ~30k 字节上限，head+tail 中间丢弃，带丢弃字节/行计数标记，可选全量落盘给路径。
- 修复建议：Executor.Exec 或 execute 构建结果前集中加字节上限（30k-256k），head+tail + 丢弃计数标记；全量 spill 到 TaskLog（复用 background.go）。
- 状态：已修（新建 truncate.go：truncateHeadTail 换行对齐 + 稳定标记；execute stdout 30k/stderr 15k 分流限额，尾注永不截；截断时全量 spill ~/.jcode/tasks/exec_*.log 并回传 [Full output: path]；覆盖 Local/SSH/Docker；测试 TestTruncateHeadTail* + TestExecute_* 6 个）

### 3. 无真 token 计量，截断与清理全靠 len/4 字符估算 【tools·high】
- 证据：internal/command/interactive.go:222-232; internal/handler/acp.go:461-470; internal/web/…:573-582; eino reduction.go:250-251
- 问题：三个 surface 构建 reduction.Config 都设 MaxLengthForTrunc:50000 与 MaxTokensForClear 却从不设 TokenCounter，eino 回退到 int64(len/4)。对 code/JSON/CJK/base64 偏差 2-3 倍。
- 对比：CC 用真 tokenizer 做上下文占用与截断预算。
- 修复建议：给 reduction.Config 注入 tokenizer 支持的 TokenCounter（eino reduction.go:242 字段已打通）。
- 状态：已修-更优解（不引 tiktoken——多 provider 下 cl100k 本就错配；internal/model/token_estimate.go：EstimateTokens ASCII/3.6 + CJK 每字 1 token，CalibratedCounter 用 provider usage 做 EMA 自校准（钳制 [0.5,3.0]、子集计数不污染）；三 surface per-agent 注入；测试 TestEstimateTokens + TestCalibratedCounter_* 4 组）

### 4. 外部改动的文件不主动回灌，检测只在写时反应式触发 【prompt·high】
- 证据：internal/tools/storage.go:296-341; write.go:77; edit.go:419; internal/agent/reminder.go:17-25,49-121
- 问题：CheckConflict 仅 write/edit 时调用；ReminderConfig 无 FileTracker 字段，reminder 中间件无法每轮巡检已读文件；无 fsnotify。
- 对比：CC/codex 每轮对已读文件做 diff 注入，主动回灌外部改动。
- 修复建议：ReminderConfig 加 FileTracker，BeforeModelRewriteState 遍历已跟踪路径跑 CheckConflict，ConflictModified 注入带 diff 的有界 system-reminder（mtime 限流），ConflictFileGone 驱逐。
- 状态：已修（FileTracker.ScanExternalChanges 每轮巡检 mtime 快路径+hash 确认，同一改动只报一次，每轮上限 5 条；reminders 规则 external_file_changed 注入 re-read 指引（无 diff——FileTracker 只存 MD5 无内容快照）；测试 TestFileTrackerScan* 4 个 + TestReminderInjectsExternalFileChange）

### 5. read 返回内容无 token 上限，单次读可炸掉整个窗口 【tools·high】
- 证据：internal/tools/read.go:14-16,101-104,142-153
- 问题：唯一天花板是 len(content)>10MB 总文件门；defaultReadLimit=2000 行且无每行上限，长行文件可返回接近 10MB 文本。
- 对比：CC 对返回切片做 token 预算截断到 ~25000 token，带 offset/limit 续读提示。
- 修复建议：对返回格式化切片加 token/字节预算（~25K token），附 offset/limit 续读提示。
- 状态：已修（read.go 加 maxReadResultBytes=200KB 输出预算 + offset 续读提示；测试 TestRead_TotalBudget*/TestRead_NormalFileNoBudgetHit）

### 6. env/git 状态启动时冻结，会话中从不刷新 【prompt·high】
- 证据：internal/command/interactive.go:955; internal/prompts/prompts.go:56-62,196-218; interactive.go:584,1300
- 问题：envInfo 仅采集一次，GitBranch/GitDirty/LastCommit/ProjectType 烤进系统提示跨轮复用；BuildEnvDiff 只在 resume 路径调用。
- 对比：CC/codex 活循环里定期或 git 操作后重跑轻量采集并注入 delta。
- 修复建议：reminder 中间件每 N 轮或 git 相关 Bash 后重跑轻量 CollectEnvInfo（branch/dirty/HEAD），复用 SerializeEnvInfo+BuildEnvDiff 注入 delta。
- 状态：已修（新增 CollectEnvInfoLight 轻量采集；reminder 每 5 轮（可配）采集→BuildEnvDiff→env_drift 规则注入 delta，快照推进去重；远程执行器 Pwd 置空关闭；测试 TestReminderEnvRefreshCadence/Disabled + TestBuildEnvDiffBranchChange）

### 7. 编辑不强制 read-before-edit，可对陈旧记忆改从未读的文件 【tools·high】
- 证据：internal/tools/storage.go:296-302; edit.go:178-180,194; write.go:76-87; internal/prompts/system.md:31
- 问题：CheckConflict 对未跟踪路径直接返回 ConflictNone 直通写入；唯一护栏是 system.md 散文。
- 对比：CC errorCode-6 硬拒绝 "File has not been read yet."
- 修复建议：加 ConflictNeverRead 状态；edit/write 在目标磁盘存在但不在 FileTracker 时拒绝（create 豁免）。
- 状态：已修（ConflictNeverRead 新状态：未跟踪且磁盘存在→拒绝并指引先 read，create/远程豁免；关键前提修复：NewEnv 真正构造 FileTracker（生产原为 nil 死代码），read TrackRead→edit/write 放行链路闭环；测试 TestEdit_RejectsUnread*/AllowsEditAfterCreate/AfterRead + TestWrite_RejectsUnread* 等 6 个）

### 8. glob 用 find -name 静默破坏它宣传的 ** 递归 【tools·high】
- 证据：internal/tools/glob.go:39,147; glob_test.go:11-16
- 问题：find -name 只匹配 basename，/ 和 ** 当字面量；schema 宣传的 '**/*.test.ts'、'src/**/*.go' 全部静默返回零文件。
- 对比：CC/codex 用 gitignore 风格 ** 全路径 glob 引擎。
- 修复建议：换 rg --files --glob（rg 已是 grep 硬依赖），同步修 schema 描述。
- 状态：已修（glob 换 `cd <path> && rg --files --hidden --sortr=modified --glob`——实测 rg --glob 以 CWD 为根，故用 cd 而非路径参数；** 递归与锚定语义 e2e 测试 TestGL05/GL06；schema Desc 同步更新）

### 9. glob 结果无序、截断丢任意文件 【tools·high】
- 证据：internal/tools/glob.go:150,174-179; 对比 grep.go:316-322
- 问题：shell 层 head 截断 + 无排序（find 目录遍历序）；grep 却按 mtime 降序，行为不一致；被丢文件 Go 侧不可见。
- 对比：CC glob 按 mtime 排序后截断。
- 修复建议：rg --files --sort=modified --glob，或截断移进 Go 按 mtime 排序再切片。
- 状态：已修（--sortr=modified 排序先于 head 截断，确定性保留最新 N 个；测试 TestGL07/GL08）

### 10. 子代理不带安全错误/panic 恢复中间件，一个工具错误整体中止 【tools·high】
- 证据：internal/tools/subagent.go:156-179,256-259; 对比 internal/agent/agent.go:42; middleware.go:38-43,84-95
- 问题：subagent runFn 的 Handlers 只有可选 Langfuse，无审批/安全错误中间件；event.Err 无条件 break；工具大量 return "",err，任一非 nil 杀死子代理返回空答案且不向父传播。
- 对比：CC 子代理同样有错误折叠与 panic 恢复。
- 修复建议：subagent 挂安全错误中间件（nil approvalFunc 的审批中间件即退化为 panic 恢复+错误折叠）；event.Err 作可恢复信号或向父传播。
- 状态：已修（subagent.go 本地 safeToolMiddleware：panic 恢复+错误折叠与 agent 侧格式逐字节一致（两侧测试互锁）；runSubagent 改返回 (string,error) 向父传播、token 汇总移入 defer；runAsync 加 recover 防后台 panic 崩进程；测试 TestSafeToolMiddleware_* 3 个 + TestTaskManager_AsyncPanicRecovered）

### 11. 无 per-turn 聚合预算，并行工具调用可灌满单条消息 【tools·high】
- 证据：internal/command/interactive.go:225; internal/agent/budget.go:110-138
- 问题：唯一上限是 per-result 50000 字符，独立应用；N 个并行调用各贡献 ~49KB。
- 对比：CC applyToolResultBudget 对一批结果求和（真 token），卸载最大的新结果到 turn 预算以下。
- 修复建议：BeforeModelRewriteState 加 per-turn 聚合上限，卸载最大新结果。
- 状态：已修（internal/agent/turn_budget.go：per-turn 聚合预算 150k 字符，只处理尾部新 Tool 批，超限按大小降序截为 head+tail+丢弃计数，copy-on-write 不污染 session history，排除名单复用单源；无条件注册使 reduction 失败时护栏仍在；测试 TestTurnBudget_* 6 个）

### 12. 超时/取消只杀 bash 直接子进程，遗留整个进程树 【tools·high】
- 证据：internal/tools/env.go:281,492,495; docker_exec.go:303-306; 对比 internal/hooks/proc_unix.go:13-21
- 问题：exec.CommandContext 无 SysProcAttr/Cancel 覆盖，只 SIGKILL bash pid；正确的进程组处理已存在于 hooks/proc_unix.go 但未用于主 exec 路径；SSH 只发一次 SIGTERM；Docker 超时只关 attach 流。
- 对比：CC 用进程组（setpgid）对整组发信号。
- 修复建议：移植 hooks 模式（Setpgid + Kill(-pid, SIGTERM→SIGKILL)）；SSH setsid/PTY；Docker 杀容器内 exec PID 树。
- 状态：已修（抽 internal/procutil 公共包 Setpgid+Kill(-pid)，hooks 改调；LocalExecutor 加 WaitDelay=2s 且 ErrWaitDelay 折叠为成功——顺带修了 `cmd &` 挂死；SSH SIGTERM→2s→SIGKILL；Docker 实测 ExecInspect.Pid 是宿主 ns pid 恒 no-op，改 pidfile 方案组杀；测试 TestLocalExec_* 3 个 + TestDockerExec_TimeoutKillsRemoteProcess 真容器跑通）

### 13. 无每行截断，一条 minified/长行灌无界文本 【tools·high】
- 证据：internal/tools/read.go:120,144-146
- 问题：逐行 verbatim 打印无每行长度检查；单条 ~9MB 行全量输出。
- 对比：CC 每行 N 字节截断带 "(line truncated)" 标记。
- 修复建议：每行 ~2000 字节截断带内联标记，独立于行数上限。
- 状态：已修（read.go 加 maxReadLineBytes=2000 每行截断，UTF-8 rune 边界安全；测试 TestRead_LongLine*/TestRead_ShortLineNotTruncated/TestRead_LineTruncationWithOffsetLimit）

### 14. 冗余自动压缩：粗糙 500 字符策略前置抢先于 eino 摘要器 【compaction·medium】
- 证据：internal/command/interactive.go:191-219,262-272; internal/agent/compaction.go:93-95
- 问题：同阈值注册 eino summMw（高保真）与 compactionMw（ThresholdCompactionStrategy）且后者 PREPEND 到最前，先跑把消息就地压成 500 字节/条的有损版并 ResetContext，可能同轮双重压缩。
- 对比：CC/codex 只有一条高保真结构化摘要路径。
- 修复建议：删除冗余 compactionMw；若要 fallback 用不同阈值并按 token 预算配额。
- 状态：已修（compactionMw 从无条件 prepend 改为仅 summarization.New 失败时的互斥 fallback；Finalize 补发 CompactDoneMsg；测试 TestSyncSummarization_ReplacesHistoryWithSummaryAndTail）

### 15. read/execute 无自身字节上限，全靠中间件（中间件失败静默跳过） 【tools·medium】
- 证据：internal/tools/read.go:14-15; execute.go:130-140,174; interactive.go:233-236; acp.go:471; web.go:583
- 问题：reduction.New 失败时 interactive 只日志跳过、acp/web if err==nil 静默丢弃——无 fallback 上限。
- 对比：CC 每个高流量工具自带字节/token 自限。
- 修复建议：read/execute 各自自带上限，reduction 失败非灾难。
- 状态：已修（read 半边由 #5/#13 工具自限承载；execute 半边由 #2 承载——单测路径无 reduction 中间件即证明；acp/web reduction 失败日志两行留给 Wave3 F cluster 单源化时补）

### 16. 无 fatal/非 fatal 错误分层，不可恢复故障被当可重试 【tools·medium】
- 证据：internal/agent/middleware.go:84-99; internal/model/retry.go:29-40; runner.go:206-216
- 问题：端点无条件 retErr=nil 把所有错误转字符串无中止哨兵，不可恢复基础设施故障循环到 maxContinuations/maxIterations 空耗预算。
- 对比：CC/codex 有 fatal 与 retryable 分层。
- 修复建议：typed fatal-error 哨兵（返回非 nil retErr/中止信号）。
- 状态：已修（internal/tools/errors.go：Fatal/IsFatal errors.As 穿透 %w 链；middleware fatal 分支先于折叠中止 run；docker isContainerGone（仅容器已删）与 ssh isSSHConnDead（EOF/closed connection）在各自 run() 咽喉处包装；测试 TestFatal_IsFatal + TestApprovalMiddleware_* 3 个 + 两个纯函数分类表驱动）

### 17. 截断阈值三处硬编码 magic number，已出现漂移 【tools·medium】
- 证据：internal/command/interactive.go:222-232; acp.go:461-470; web.go:573-582
- 问题：reduction.Config 硬编码三次；interactive 设 TruncExcludeTools:['ask_user','load_skill'] 而 acp/web 省略，同工具跨 surface 行为不同。
- 对比：CC 截断配置单源。
- 修复建议：提取单一 builder（放 ResolveContextLimit 旁），三 surface 统一调用。
- 状态：已修（internal/agent/reduction.go 单源 BuildReductionConfig + ReductionThreshold + ReductionExcludeTools；三 surface 统一调用，acp/web 补齐 TruncExcludeTools 与失败日志；MaxTokensForClear 一并接入 EffectiveContextLimit；测试 TestReductionThreshold/TestBuildReductionConfig*）

### 18. 缺并行/批量工具调用指令 【prompt·high】
- 证据：internal/prompts/system.md:58-61,63-67; subagent.go:446
- 问题：Tool Usage Policy 无批量/并行指令；运行时 eino 已并行执行单轮工具调用但从不告诉模型。
- 对比：CC prompt 明确指示批量发独立调用。
- 修复建议：加条目：可在一条回复里批量独立 read/grep/exec，只对有依赖的排序。
- 状态：已修（system.md Tool Usage Policy 插入 Batch independent tool calls 指令；测试 TestGetSystemPromptSections）

### 19. 缺验证/测试纪律章节，配合 goal 自动续跑可自证虚假完成 【prompt·high】
- 证据：internal/prompts/system.md:44,67,42
- 问题：全部验证指引仅"检查结果确保正确"；无"先跑最窄相关测试再拓宽/如实报告失败/不给无测试仓加框架"的程序；system.md:42 在活跃 goal 上自动续跑。
- 对比：CC 有完整 Verification 章节。
- 修复建议：补 Verification 章节。
- 状态：已修（system.md 插入 # Verification 五条纪律 + Workflow 第 4 步改 Verify；测试 TestGetSystemPromptSections）

### 20. AGENTS.md 仅加载一次，会话中不重读 【prompt·medium】
- 证据：internal/prompts/prompts.go:72,134,144-154; memory.go:43-77
- 问题：只在系统提示构建时加载，无 watcher；稳定单模式活跑中永不刷新。
- 对比：CC/codex mtime 门控重读并注入 delta。
- 修复建议：reminder 中间件加 mtime 门控刷新，内容变则注入 system-reminder delta（hash 去重）。
- 状态：已修（LoadAgentsMdContent 导出复用 MemoryLoader 管线；reminder 每轮 1 次 stat mtime 门控 + md5 去重，更新注入 supersedes 语义 10k 截断，删除注入通知；无路径时按 env 节奏重扫捕捉新建；测试 TestReminderAgentsMd* 3 个 + TestAgentsMdChangedReminder）

### 21. 日期会话中不刷新，跨午夜留陈旧日期 【prompt·medium】
- 证据：internal/prompts/prompts.go:50,116,162; reminders.go:32-105
- 问题：Date 只在提示构建时设一次；持续运行的过夜会话无任何纠正。
- 对比：CC 跨日注入一行纠正。
- 修复建议：加 date_change 提醒规则（reminders.go），比对当前日期与会话起始日期。
- 状态：已修（寄生 #6：SerializeEnvInfo 已含 date 行、BuildEnvDiff 已比对 date 键，随周期刷新自动纠正并去重；测试 TestBuildEnvDiffDateChange + 并入 Cadence 用例）

### 22. 后台任务输出上限粗糙且欠信号，TaskLog 路径不回传 【tools·medium】
- 证据：internal/tools/background.go:155-160,117-153,304-316
- 问题：maxInMemoryOutput=4096 head-only 截断丢弃尾部错误；标记无丢弃计数；TaskLog 路径 formatTask 从不回传给模型。
- 对比：CC head+tail 带计数并回传全量日志路径。
- 修复建议：head+tail 截断带丢弃字节数，formatTask/通知回传 TaskLog 路径，与同步路径统一截断助手。
- 状态：已修（NewTaskLog 脱离死代码 StorageManager 参数，spill 无条件可用；head 1500+tail 2500 共享 truncateHeadTail；BgTask/通知带 LogPath，formatTask 回传 Full log: path；远端 executor 不回传本地路径仅记日志；测试 TestBackground_* 3 个 + TestFormatTask_IncludesLogPath）

### 23. 多编辑缺 old_string/new_string 重叠守卫与逐编辑歧义检查 【tools·medium】
- 证据：internal/tools/edit.go:481-491,194-196
- 问题：applyMultiEdits 无逐 op 唯一性检查、无"old_string 是前序 new_string 子串"守卫、无逐 op replace_all；单编辑的 count>1 保护在多编辑路径静默消失。
- 对比：CC multi-edit 检查每处唯一性与前序重叠。
- 修复建议：应用 op i 前拒绝与更早 new_string 重叠；套用歧义错误；EditOp 加 ReplaceAll。
- 状态：已修（applyMultiEdits 逐 op：空串/identical 拒绝、前序 new_string 重叠守卫、count>1 歧义检查、EditOp.ReplaceAll 逐 op 支持；测试 TestEdit_MultiEdit* 4 个）

### 24. 写入非原子，中途崩溃/取消截断损坏文件 【tools·high】
- 证据：internal/tools/env.go:254-260; edit.go:146,210,293,497; write.go:102
- 问题：os.WriteFile 就地 truncate-then-write，无临时文件/fsync/rename；Docker/SSH 同样。
- 对比：CC/codex 写临时文件、fsync、rename 原子覆盖。
- 修复建议：LocalExecutor.WriteFile 改 sibling 临时文件→fsync→os.Rename；失败回退就地。
- 状态：已修（atomicWriteFile：同目录 CreateTemp→Sync→Chmod→Rename，保留 mode、写穿 symlink、失败清理回退就地；SSH 也改 base64→tmp→chmod→mv -f；测试 TestAtomicWrite_* 5 个 + TestSSHWriteCmd_AtomicMv）

### 25. 无压缩输出/摘要余量预留，激进阈值可溢出真实窗口 【compaction·medium】
- 证据：internal/agent/compaction.go:53-57; interactive.go:194; context_limit.go:24-45; config.go:365-370
- 问题：ShouldCompact 与 eino 触发都用原始 contextLimit 乘阈值，无输出预留；阈值可设 0.95 贴原始窗口。
- 对比：CC getEffectiveContextWindowSize 先减 max-output 预留再乘阈值。
- 修复建议：乘阈值前减 min(maxOutputTokens,20000) 预留并钳制。
- 状态：已修-核心（context_limit.go 加 EffectiveContextLimit：reserve=min(20000,limit/4)；interactive.go summ Trigger 已接入；acp/web 接入留给 Wave3 F cluster；测试 TestEffectiveContextLimit 七断言）

### 26. grep 内容模式按文件而非全局限制匹配数 【tools·medium】
- 证据：internal/tools/grep.go:194,506,360-375,384-391
- 问题：--max-count 是每文件的；totalLines 在每文件封顶流上算导致 '(N total)' 低估；offset 分页与每文件上限交互丢匹配。
- 对比：CC 全局 offset+limit，total 精确。
- 修复建议：内容模式去掉 --max-count，Go 侧全局 offset+limit 切片。
- 状态：已修（去掉 --max-count，runLocalRg 流式读取+早停 cancel，capped 时诚实尾注不谎报 total；远端加 head 守卫；测试 TestG19-G22）

### 27. grep.output_mode 无 enum 无校验，未知值静默产内容模式 【tools·medium】
- 证据：internal/tools/grep.go:86-90,157-159,187-195,286-293
- 问题：裸 schema.String 无 Enum；switch default 落内容模式，近似值（'files'/'matches'）静默错模式。
- 对比：codex/CC enum 约束，非法值报错。
- 修复建议：加 Enum:['files_with_matches','content','count'] + 运行时守卫。
- 状态：已修（schema 加 Enum + 运行时守卫，非法值报错列出合法值；测试 TestG23/G24）

### 28. 原始 Go 错误链原样泄露给模型且无指引无分类 【tools·medium】
- 证据：internal/agent/middleware.go:87,89; read.go:65,81,97; write.go:55,103; execute.go:94
- 问题：畸形 JSON 与 syscall 失败返回裸 fmt.Errorf %w，无下一步提示无稳定 errorCode（edit 的无匹配路径已 curate，不在此列）。
- 对比：CC 高频失败附短祈使提示 + 稳定 errorCode。
- 修复建议：高频非 edit 失败附短提示（arg-parse/file-not-found/缺必填），typed error 中间件统一应用。
- 状态：已修（ToolError{Code,Hint,Err}：hint 烘焙进 Error() 所有出口受益，Err 原文在前保留 grep 与 errors.Is/As 链；read/write/execute 高频错误 curated（invalid JSON/missing param/not-found 带 grep 指引/permission 分流）；测试 TestToolError_Format + 6 个 hint 测试）

### 29. edit.edits 与 todowrite.todos 声明为裸数组无 item schema 【tools·medium】
- 证据：internal/tools/edit.go:74-77; todo.go:170-174
- 问题：Type:schema.Array 无 ElemInfo，对象形状与 status 集合只活在 Desc 散文；最终 JSON schema 为 {type:array} 无 items。
- 对比：codex/CC array-of-objects 带 items/properties。
- 修复建议：edits 设 ElemInfo+SubParams({old_string,new_string} 必填)；todos 设 {id,title,status(Enum)}。
- 状态：已修（edits/todos/enhanced items 全部挂 ElemInfo+SubParams+status Enum，验证 eino ToJSONSchema 产出 items/required/enum；测试 TestEditToolSchema/TestTodoWriteSchema 等 3 个）

### 30. 自动压缩仅内存态，TUI resume 丢失最近消息尾部 【compaction·high】
- 证据：internal/agent/compaction.go:209; internal/session/session.go:361-362; internal/session/history.go:216-220; internal/agent/history.go:102-127
- 问题：compactionMw 只改 state.Messages 不持久化；EntryCompact 只存 {Summary,CompactedN} 无 tail，replay 时丢弃保留的最近尾部；Web 按 raw 历史重建不受益。
- 对比：CC/codex 持久化摘要同时保留最近尾部。
- 修复建议：EntryCompact 连同保留尾部一起存，replay 重挂；持久化或退休 compactionMw。
- 状态：已修（EntryCompact 加 kept_n 字段，replay 保留尾部并做 tool 边界回退；旧文件无字段兼容旧行为；测试 TestReconstructState_Compact* 4 个 + TestSyncSummarization_RecordsKeptN）

### 31. 无压缩失败熔断，consecutiveFails 只写不读 【compaction·low】
- 证据：internal/agent/compaction.go:200-206,214,133,53-57
- 问题：consecutiveFails 只写从不读；poison context（单消息超窗/摘要器持续失败）每轮重触发无退避。
- 对比：CC/codex 有熔断/退避。
- 修复建议：接入 ShouldCompact，N 次连续失败后本会话抑制自动压缩并记日志。
- 状态：已修（maxConsecutiveCompactFails=3 熔断 fail-open + sync.Once 日志；无收缩也计失败；strategy 失败改为传播错误；测试 TestCompactionMiddleware_Fuse* 3 个 + TestThresholdStrategy_PropagatesSummarizerError）

### 32. ResolvePath 静默接受相对与逃逸目录路径仅记日志 【tools·low】
- 证据：internal/tools/env.go:114-124; read.go:34,71; write.go:29,61; edit.go:46,106
- 问题：相对路径 join pwd 返回；逃逸 pwd 只 Logger().Printf 仍返回使用；工具描述写 "absolute path" 却不强制。
- 对比：CC 非绝对路径返回模型可见错误，逃逸明确拒绝。
- 修复建议：强制绝对路径或改描述为 pwd-relative；逃逸改模型可见拒绝。
- 状态：已修-更优解（文档对齐现实：file_path Desc 改为 absolute preferred + relative 解析语义，行为零改动避免破坏现有 prompt 习惯；TestResolvePath 6 组表驱动固化语义）

## 修复波次规划（按文件重叠避免冲突）

- Wave 1（互不重叠）：B=read（5/13/15-read）；D=glob/grep（8/9/26/27）；H=subagent+错误处理（10/16/28）
- Wave 2：A=exec 输出上限+进程组+后台（2/12/15-exec/22）；E=压缩/提醒核心（1/14/25/30/31）
- Wave 3：C=edit/write 守卫+原子写+schema（7/23/24/29/32）；F=reduction 配置单源+tokenizer+per-turn 预算（3/11/17）；G=prompt 增补+env/AGENTS.md/日期刷新（4/6/18/19/20/21）

## 复核结论（8 cluster 逐 case 对照源码复核）

33 个 case 条目（#15 拆 exec/read 两半）**全部确认成立、全部可修**：22 个按原建议修（fix_as_proposed）、11 个采用更优替代方案（fix_with_better_alternative），无 defer、无 invalid。要点：

- **#2 execute 截断放工具层而非 Executor 层**：Exec 接口还有 grep/glob/read 三个内部调用方（各有自己的输出控制），Executor 层注入标记会污染它们；工具层一处覆盖 Local/SSH/Docker 三种 executor。stdout/stderr 分别限额（30k/15k，head+tail 尾偏置），合计留在 eino 50000 阈值之下避免二次截断破坏标记；截断时全量 spill 到 ConfigDir/tasks/ 并回传路径。
- **#12 抽 internal/procutil 公共包**：hooks/proc_unix.go 的 Setpgid+Kill(-pid) 迁入共用；LocalExecutor 加 WaitDelay=2s 顺带修复 `cmd &` 后台孙进程持管道导致 Wait 挂死的隐藏 bug（须折叠 exec.ErrWaitDelay 为成功）；SSH SIGTERM→2s→SIGKILL 尽力而为；Docker inspect Pid 后组杀回退单杀。
- **#7 比审计更严重**：Env.FileTracker 在生产从未赋值（只有测试构造），read-before-edit 强制要先把 FileTracker 接线到 NewEnv/各 surface，再加 ConflictNeverRead 拒绝。
- **#10 复用 agent.approvalMiddleware 不可行**（agent→tools 已有依赖，反向即循环）：subagent.go 本地实现 safeToolMiddleware（~35 行，panic 恢复+错误折叠，格式与 agent/middleware.go 锁定一致——acp.go:448 依赖该前缀做分类）；runSubagent 改返回 (string, error) 向父传播模型层错误；subagent_manager runAsync 加 recover 防后台 panic 崩进程。
- **#16 sentinel 放 tools 包**（internal/tools/errors.go：Fatal/IsFatal），agent/middleware 消费；第一波只标"容器已删/SSH 连接永久断开"两类确定永久性故障，context.Canceled 不标（runner 已处理）。
- **#28 hint 烘焙进 ToolError.Error() 而非中间件追加**：所有出口（子代理/TaskManager/runner）自动受益，且不动 acp.go 依赖的折叠前缀。
- **#4 FileTracker 只存 MD5+mtime 无内容快照，产不了 diff**：改为注入"文件已被外部修改，重读后再改"的有界提醒（不含 diff），mtime 快路径每轮巡检。
- **#21 日期刷新寄生到 #6 的 env_drift**：SerializeEnvInfo 已含 date 行、BuildEnvDiff 已比对 date 键，#6 落地即免费获得，不加独立规则。
- **#17 先行**：単源 BuildReductionConfig 是 #3（TokenCounter 注入）与 #15（acp/web 日志）的结构前提。
- **#18 与 #11 耦合**：并行工具指令会放大单消息灌满风险，与 per-turn 聚合预算同波次落地。

### 实施波次（按文件冲突图重排）

- Wave 1（并行）：B-read（read.go）· D-search（glob/grep.go）· E-compaction（compaction/session/interactive 压缩段；#1 已提前完成）
- Wave 2（并行）：A-exec（execute/env/docker/background/procutil/hooks）· G-prompt（system.md/prompts/reminders/agent reminder）
- Wave 3（并行）：C-editwrite（edit/write/storage/todo/env 写路径）· F-budget（interactive/acp/web reduction 单源+tokenizer+per-turn 预算）
- Wave 4：H-errors（subagent/middleware/errors.go + read/write/execute 错误路径收尾，叠在 A/B/C 之上）

完整逐 case 实施方案与测试计划见 scratchpad plans/（A-exec.md 等 8 份），最终以代码与测试为准。

## 验证结果（2026-07-05）

- 单元/集成：`go test -count=1 ./...` 全仓 22 个含测试包全绿；`go test -race` tools/agent 绿；`GOOS=windows go build` 绿；新增测试约 90 个（全部 TDD 先红后绿）。
- e2e（agent-eval ACP + 真模型 glm-5.2 + 决定论 oracle）：新增 case `robust_huge_output`——强制模型运行 `seq 1 200000`（约 1.26MB 输出）。修复前该输出会无标记灌爆上下文；实测 ACP 事件流出现 `output truncated: 1258901 bytes (~194809 lines) dropped` 与 `[Full output: <path>]`，会话 4 次工具调用、104k token、干净 end_turn，oracle 验证 answer.txt == "200000"（模型从保留的 tail 中取到答案）。PASS 1/1。

## 遗留（后续小 PR，非阻塞）

- storage.go 的 WriteQueue/StorageManager.Write 换原子写（复核时裁定单独 PR）。
- #16 fatal 分类第二波：MCP/网络类故障的分级退避（首波只收容器已删/SSH 连接死两类确定项）。
- FileTracker 备份功能需接 StorageManager 才启用（CreateBackup 现安全返回空）。
- eino 子代理 NodeRunError 是否保留 Unwrap 链未定——fatal 在子代理边界可能退化为普通错误（可接受降级，已有测试锁定主路径）。
