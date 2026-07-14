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
| 触发 | `approvals_reviewer=auto_review` + on-request/granular | `approval_review.enabled` + Approval 模式下 `decide()→prompt` |
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

```jsonc
"approval_review": {
  "enabled": true,            // 开关;false=从不构造,行为不变
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
`internal/agent/{agent,middleware}.go`(`ReviewDeniedError`+文案),`internal/command/acp.go`(接线+
transcript+OnTurnStart)。TUI/web 前端接线同法(本期先 ACP,PR 一并补)。

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

## 后续

- 把 seam 接到 TUI/web 前端(与 ACP 同法)。
- V2+V3 合并(带工具的 trunk 复用)。
- 审查器 metrics/遥测(latency、allow/deny 率、cache 命中)接入 telemetry。
- provider cache 能力差异:zhipuai 支持前缀缓存(实测命中),tencent-tokenhub 代理不回传缓存;
  reuse_session 的收益取决于 provider。
