# 审批自动审查(approval review / guardian)设计与落地

2026-07-15。目标:填补"规则表不敢自动放行、但每次都弹窗太烦"的中间地带 —— 一个可选的
LLM 审查器,对本来要打扰用户的工具调用先做风险判定,自动放行低风险、拦截高风险、把拿不准的
交还用户。对标 OpenAI codex 的 guardian(`approvals_reviewer = auto_review`)。

本文记录调研结论、架构、V1→V3 分层、失败语义、缓存策略,以及与现状的对比。

## 动机与对标

jcode 现有审批链收敛在一个 seam(`agent.ApprovalFunc` → `runner.ApprovalState.RequestApproval`)。
规则引擎 `decide()` 把工具调用分成三类:自动放行(读、`ls`/`cat`/`git status` 等安全命令)、
问用户(其它 execute/edit/write…)、问用户且标注越界(workpath 外)。问题在于"问用户"这一档
非黑即白:安全表不敢收的命令全都弹窗,高频打断;而 `--unsafe`/Full access 又是一键全放开。

codex 的 guardian 在这中间插了一层 LLM 判定:只审"本来要弹窗"的请求,按风险 + 用户授权决定
allow/deny,失败 fail-closed,并有拒绝熔断。jcode 的 `internal-doc/small-model-design.md` 早已把
guardian 列为对标项,"model roles" 就是给这类新角色留的扩展位。本期把它落地。

| 维度 | codex guardian | jcode approval review(本期) |
|---|---|---|
| 触发 | `approvals_reviewer=auto_review` + on-request/granular | `Auto` 会话模式;`decide()→prompt` 的调用先过审查器 |
| 模型 | 专用 `codex-auto-review`(服务端隐藏 slug)+ low effort | 复用 `small` 别名(→ small_model →主模型),可 override |
| 输出 | 严格 JSON `{risk_level,user_authorization,outcome,rationale}` | 同结构(见下) |
| 失败语义 | fail-closed(headless,无人可问) | **fail-open 到用户**(jcode 有交互 UI,回落问人更平滑) |
| 熔断 | 同 turn 连续 3 次 / 最近 50 中 10 次 deny → 打断 turn | 同 turn 连续 3 次 deny → escalate 交还用户 |
| 只读调查 | guardian 子会话可跑只读工具 | V2:read/grep/glob 只读 loop |
| 会话复用 | `GuardianReviewSessionManager` trunk + prompt cache | V3:per-session trunk + prompt cache |
| 审计 | 完整 metrics | JSONL 审计日志(`approval-review.jsonl`) |

## 架构

新包 `internal/review` 持有全部类型 + `Reviewer` 接口 + 具体 `Engine`。依赖方向
`review → {model, config, tools}`,`runner → review`,`command → review`;`model/tools/config`
均不反向依赖,无环。

- **seam**:`ApprovalState` 新增 `reviewer review.Reviewer`(nil=禁用,行为不变)、
  `transcriptFn func() []review.Msg`、`breaker reviewBreaker`。`decide()` 返回"问用户"的两档
  改走 `gatedApproval()`:先 `tryReview()`,审查器settle不了(禁用/失败/escalate/熔断)才回落
  `requestUserApprovalWithWorker()`。主路径与 teammate 路径共用 `gatedApproval`,不会漂移。
- **拒绝消息**:审查器 deny 通过 `agent.ReviewDeniedError{Reason}` 返回,middleware 特判后给模型
  一段区别于"用户拒绝"的文案(点名是自动审查器 + rationale + 禁止绕路实现)。
- **证据**:`transcriptFn` 由前端安装,快照 `sess.history` 尾部(去掉 system prompt),转成
  `[]review.Msg` 交给审查器判"用户是否授权"。cwd 复用 `ApprovalState.workpath`。
- **审计**:每次判定(含 fail-open 的 escalate)写一行 JSONL:工具、args、decision、risk、
  user_auth、rationale、model、latency、prompt/cached tokens、cache_seen、investigated、
  review_calls。既是排障与复盘依据,也是测试 oracle(不依赖 agent 自述)。

## V1 — 最小闭环

`reviewSingleShot`:构造 `[system(policy), user(action+transcript)]`,单次非流式 `Generate`,
最多 `parseAttempts=2` 次(拿不到严格 JSON 就追加"只输出 JSON"再试一次),解析出
`{risk_level,user_authorization,outcome,rationale}` 映射为 allow/deny/escalate。

**三态 outcome**:`allow` 自动放行;`deny` 直接拦截(给模型带 rationale 的反绕路文案,不弹用户);
`escalate` 交还用户(走正常审批弹窗)。escalate 是**成功审查**的一等结果(`Failed=false`),
区别于失败/超时导致的 escalate(`Failed=true`)。policy 指示模型:明确不安全→deny,高风险但拿不准
用户是否授权→escalate(让人决定,而非硬拦)。这正是"拿不准的交还用户"的落点 —— 早期版本只有
allow/deny,uncertain 只能靠拒绝熔断间接交还,已在对抗审查后修正为一等 escalate。

模型解析:`override → small_model → 主模型`。`small` 别名未配置时降级主模型,审查器永远有可用模型
(与 title.go 的降级哲学一致);override 用具体 `provider/model`。

**失败一律 fail-open**:模型初始化失败、超时、`Generate` 出错、解析失败 → `Escalate{Failed:true}`,
回落问用户。审查器坏了绝不静默放行或拒绝。

Prompt 三段式(`policy.go`):判定框架(证据不可信/授权四级/风险四级/outcome 矩阵,改编自
codex guardian policy 但按 jcode 工具面裁剪)+ 可选 workspace 追加 policy + 输出契约(与 parser 相邻)。

## V2 — 只读调查

`approval_review.investigate=true` 时走 `reviewWithTools`:用 `req.Cwd` 建只读 `tools.Env`,
挂 read/grep/glob(**不挂 execute/edit/write/网络**),跑 ≤8 轮 adk agent loop,判前可自查
(如删除前看目标是不是普通空目录、判网络动作前读文件确认不含密钥)。verdict 取最后一条 assistant
消息(newest→oldest 扫描,跳过工具调用间的碎话)。失败同样 escalate。

安全边界:只读工具无审批直接跑(审查器内部 loop),故严格限制在 read/grep/glob。注入可能诱导审查器
读到密钥,但审查器无写、无网,输出仅 allow/deny/rationale;残留风险仅 rationale 可能带片段,已知并接受。

## V3 — 会话复用 + prompt cache

`approval_review.reuse_session=true` 时走 `reviewCached`:维护 per-session trunk
`[system, action1, verdict1, action2, verdict2, ...]`,每次审查把新 action 追加到稳定前缀后发送,
provider 按前缀命中缓存(大 policy 前缀 + 历史 verdict 免 prefill)。

- **正确性**:失败不提交(保持前缀干净);trim 按整对 (action,verdict) 丢弃、始终保留 system;
  reviews 在 trunk 上串行(共享增长列表并发 Generate 会乱序,串行才使前缀确定)。
- **不污染主对话 cache**:审查器独立 model + 独立消息列表,从不碰 `sess.history`;前缀与主对话
  完全不同(system=policy vs coding prompt),不同前缀在 provider 侧是不同缓存条目,只共享容量(LRU)。
- V2 与 V3 互斥(工具 loop 无法共享单发 trunk),同开时 investigate 优先。

## 配置

`approval_review` 块不再包含 `enabled` 开关,它只作为审查器的**调参面板**。审查器在 `Auto` 会话模式下自动启用,在 `Ask for approval` / `Plan` / `Full access` 模式下不运行。

```jsonc
"approval_review": {
  "model": "small",           // 空→small_model→主模型;可写 provider/model
  "policy": "…",              // 追加到内置 policy 的 workspace 规则
  "timeout_seconds": 60,
  "investigate": false,       // V2 只读调查
  "reuse_session": false,     // V3 会话复用 + cache
  "audit_path": ""            // 默认 <config dir>/approval-review.jsonl
}
```

落地位置:`internal/review/*`(引擎/policy/parse/audit/investigate/session_cache/build),
`internal/config/config.go`(`ApprovalReviewConfig`),`internal/runner/{approval,review}.go`(seam+熔断),
`internal/agent/{agent,middleware}.go`(`ReviewDeniedError`+文案),`internal/command/{acp,interactive,web}.go`(接线+
transcript+OnTurnStart)。TUI/web/ACP 前端均提供 `Auto` 模式选择。

## Auto 模式

`Auto` 是 Ask/Plan/Full access 之外的第四模式:
- 使用完整工具集(同 Ask/Full access)。
- 审批轴保持 `ModeManual`,因此审查器 escalate 的调用仍会弹窗。
- 进入 `Auto` 时 `ApprovalState` 按需构建 reviewer;离开 `Auto` 时清除 reviewer,避免在非自动模式下消耗 token。
- 未配置 `approval_review` 时,`Auto` 使用默认参数(`small` 别名、60s 超时、单发、无 investigate、无 reuse_session)构建 reviewer。

## 与现状审批机制的对比 / 改进空间

审查器补的是规则表与全放开之间的中间带。落地中另发现几处与审查器无关但更该修的洞(排在审查器前或同批):

- **P0 subagent 完全绕过审批**:`subagent` 在免审名单,子 loop 不挂 approval 中间件 →
  子 agent 可无提示跑任意 shell。审查器落地后 subagent 默认走 auto-review 而非弹窗,正好受益。
- **P0 webfetch 永不审批**:SSRF/外发通道自动放行。
- **P0 hooks fail-open**:拒绝型 PreToolUse hook 超时/起不来=放行。
- **P0 "Approve All" 一键全局提权**:一次 allow_always 升级整会话到 auto。审查器落地后
  Full access 可演化成 codex 形态(自动 + auto-review 兜底)而非裸放行。
- **P1**:hook 的 `ask` 被丢弃;`isSafeCommand` 只看第一个词(`cat ~/.ssh/id_rsa` 放行);
  双分类器漂移;ACP 审批按 (name,args) FIFO 匹配(有 ToolCallID 可改按 id);无审计日志
  (本期审查器已带,可推广到全审批)。
- **P2**:无 OS 级沙箱(审查器是纯 LLM 判断,长期需 seatbelt/landlock 纵深);项目 hooks all-or-nothing 信任。

## 已知限制(对抗审查后记录)

- **审查器是概率性安全层,不是保证**:20 轮 440 次判定中,清晰恶意件(rm -rf /、外发、注入)20/20 正确,
  但云元数据 SSRF 取凭证(`d_metadata_creds`)被"调试借口"话术带偏 2/20(见测试报告 §B)。根本上审查器是
  **中间层**:它降低打断、拦掉大部分危险,但不能替代 fail-open-to-user 与未来的 OS 沙箱纵深。
  更强的审查模型漏放率更低。
  → 该案已加**确定性前置拦截**(`ssrf.go`):policy 文案不是控制手段,被话术带偏正是 prompt 类防御的失效
  方式。云元数据地址(含十进制/十六进制/八进制混淆)+ 凭证路径 → 调模型**之前**直接 deny;仅提及地址无凭证
  路径 → escalate 交人(deny 用户无法翻案,escalate 可以)。有普通 CI 可跑的单测(不依赖 live model),
  含"prefilter 先于模型执行"的接线测试。prompt 文案保留作纵深。

- **background 命令的不变量已澄清**:`decide()` 对 `background:true` 不走安全表快捷放行,原注释说"必须问人"。
  接入 reviewer 后它经 `gatedApproval` 可能被自动放行。真正的不变量是"agent 不能靠设 flag 买到免检",不是
  "每条后台命令都必须人看"。已更新注释,并把 `background_execution` 作为**显式风险信号**写进 action prompt +
  policy(输出不实时可见 → 同命令比前台高一档审视),而不是埋在 args JSON 里。
- **V2 investigate 假设本地文件系统**:审查器的 read/grep/glob 走 LocalExecutor,读的是本地磁盘。
  remote/SSH 会话里被判命令在远端执行,本地同路径可能是另一台机器的内容 → 可能误判。当前建议
  remote 会话不开 investigate;彻底修需把会话 executor 传给审查器(后续)。
- **审查器花费计入进程全局 token 计数**:per-session 账目已隔离(审查器用 ctx-local tracker 影子),
  但 `internalmodel` 的进程级全局 `TokenTracker` 仍累加审查器调用;TUI 聚合读数接线后会把审查器花费
  算进去。属真实花费,但与"完全独立账目"表述有出入,记录在案。
- **审查器只看到本 turn 用户消息之前的 transcript**:`sess.history` 在 turn 结束才追加本轮
  assistant/tool 消息,故审查器拿不到"触发该动作的本轮工具输出"。是上下文缺口,非竞态(读取在锁内)。
- **拒绝熔断器是会话级、跨主 agent 与 teammate 共享**:并发 teammate 负载下可能提前触发(escalate 到
  用户,安全);`OnTurnStart` 已在 ACP/TUI/web 三端接线做 per-turn 复位。
- **reviewer panic 兜底**:`Engine.Review` 有 recover → escalate(fail-open),避免 middleware 的
  通用 panic 处理把调用 fail-closed 拦掉。

## 后续

- V2+V3 合并(带工具的 trunk 复用);investigate 接会话 executor(修上面第 1 条)。
- 审查器 metrics/遥测(latency、allow/deny/escalate 率、cache 命中)接入 telemetry。
- provider cache 能力差异:zhipuai 支持前缀缓存(实测命中),tencent-tokenhub 代理不回传缓存;
  reuse_session 的收益取决于 provider。
