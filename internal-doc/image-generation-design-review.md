# 图片生成设计对抗审阅记录

> 日期：2026-08-08
> 最终结论：GO for implementation

> 后续产品决策：图片生成改为独立 `image_model` 角色，选择有效模型即在 normal mode 注册；没有 provider/session 图片开关，也没有 image-specific session grant。Web Search 等 provider-bound tools 只保留 provider policy，并且必须绑定当前 Agent/Chat Model 的 provider exact adapter；Composer/TUI/ACP 不再暴露 session override。Ask for approval / Auto 逐次费用审批；Full access 不询问，但 runtime revalidation、额度与 durable journal 保持不变。正式生成态选择扫描织网：8 横轨 + 8 纵轨 + 6 节点，generating `3.2s`，saving 使用独立 `4.6s` 收束；竖/方/横占位为 `12rem`/`16rem`/`18rem`，success 最大 `18rem` 且竖图最大高度 `22rem`；reduced-motion 下两状态静态可区分，live region 位于 `aria-busy` 媒体面的 sibling。

## 1. 审阅方式

在 POC、技术架构和 UI 设计完成后，由两条独立红队并行审阅：

- Security/architecture：费用授权、凭据边界、SSRF、崩溃恢复、managed storage、并发 ledger；
- Frontend/contract：timeline grouping、approval options、replay、状态 reducer、provider policy、Image Model 自动注册、响应式、无障碍、npm 发布拓扑。

两条红队首轮均给出 NO-GO；主设计按 finding 一对一修订，再由原 reviewer 复审。

## 2. 首轮 NO-GO 与处置

| Finding | 修订 |
| --- | --- |
| Provider POST 默认 redirect 可能泄露 Bearer/custom headers | 独立安全 client；POST 禁止 redirect；asset redirect 逐跳校验；Proxy/CookieJar 关闭 |
| 非 Full access 模式的 hook/reviewer/Approve All 可绕过费用确认 | typed `BillableIntent`；Ask for approval / Auto 使用 opaque options fail closed；Full access 只免交互批准，不绕过 intent/runtime/quota |
| POST 后崩溃会误报 cancelled 并诱发二次计费 | dispatch 前 durable `generation_operation`；uncertain 状态；同 key 恢复、禁止盲目重提 |
| Artifact 无 ToolCall/Operation 关联 | Record/session/result 增 `OperationID`、`ToolCallID`、typed outcome/error code |
| MCP `credential_ref` 可能被恶意项目配置滥用 | 禁止进入通用 MCP schema/CRUD；只允许人工 manifest 的内部 `ProviderMCPPreset` |
| Session limit 只有内存计数 | operation `dispatch_attempted` 是唯一 durable consume commit；resume replay 重建 ledger |
| Custom asset CDN 无法安全表达 | `asset_hosts` exact/受限 wildcard；默认 base64/same-origin；不自动学习 |
| `surface=standalone` 仍会被 Activity/batch 折叠 | 不参与 batch pull；关闭前后 group；Thread 直接分派 standalone renderer |
| queued 媒体占位把审批态和生成态混在一起 | 内部仍记录 queued；Web/Desktop 由独立 ApprovalBanner 显示审批，queued 与 pre-dispatch deny/cancel 均不渲染媒体卡；generating/saving 才出现紧凑占位 |
| Terminal/replay 只能解析字符串 | WS/core/result 增 typed `operationID/outcome/errorCode`；单调 phase merge |
| Artifact 已落盘但 operation terminal 缺失时 replay 矛盾 | terminal op > verified artifact > terminal result > non-terminal op；可补 recovery terminal |
| P0 同时出现单图与四图/partial 两套合同 | PRD/architecture/UI 统一为同步单图；多图/partial/async 移 P1 |
| Regenerate/Edit/Retry 没有后端 action | P0 全部隐藏；图片卡也不叠加 Download/Open/Reveal，管理操作统一进入 Artifact 面板 |
| Provider-bound session override 只有概念没有 API | 后续产品决策删除该层：Web Search 直接跟随当前 Chat Model exact provider 的 policy，Composer/TUI/ACP 不重复暴露开关 |
| Viewer 窄屏/键盘合同无 shell ownership | App/RightPanel 管 breakpoint/dialog；separator keyboard；per-artifact viewed after decode |
| package 发布拓扑与 checkout 的 workspace 依赖冲突 | 明确为发布前 fail-closed migration gate；不得用 workspace E2E 冒充 registry 安装验证 |

## 3. 最终复审

Security reviewer：GO for implementation。确认 provider redirect/proxy、custom asset allowlist、typed intent、durable operation/ledger、可信 MCP preset、managed root、hook fallback 和单图 PRD 均闭合。

Frontend reviewer：GO。确认单图/逐次审批/隐藏无后端动作、durable generating、replay 优先级、typed terminal fields、standalone grouping、provider policy、Image Model 自动注册与 a11y 合同均闭合。

## 4. GO 的含义

GO 只代表设计允许进入实现，不代表可发布。发布仍必须实际通过：

- race/concurrency 与 crash recovery；
- provider POST/asset SSRF 与 credential non-disclosure；
- approval bypass negative tests；
- TUI/ACP/Web/Desktop 同一 Artifact E2E；
- registry package 发布/安装 smoke；
- BigModel 单图与 Search MCP 一次性 live gate；
- 最终代码对抗审阅与回归。

## 5. 实现后对抗审阅

实现完成后又进行了多轮独立的 security、product、frontend 与 transport 审阅。发布阻断项均以回归测试闭合，主要包括：

- MCP headers/OAuth secret 掩码与显式删除；config/session 跨进程锁、CAS、目录 fsync 和 stale-FD 防护；
- provider runtime/config epoch/credential/header digest 的 dispatch-time 复核，session hard cap 的跨进程原子 reserve；
- SSRF special-use 网段拒绝、DNS pin、redirect/asset host 限制，provider response/error 不进入 session/UI；
- runner 生成 Operation UUID；坏 security journal fail closed；truncate 不删除费用/策略证据；
- 一次性 opaque approval option 对 intent/context 精确绑定；Ask for approval / Auto 中 Approve All、hook/reviewer 不能绕过；Full access 不创建 approval option，但仍校验同一 intent/context；
- TUI/ACP/Web/Desktop 的 Provider Web Search 等 provider-bound tools 共用当前 Chat Model exact provider 的 policy；没有 task/session 工具开关。图片生成只由全局 Image Model 角色注册。mode journal 先 fsync 再发布，resume 对坏记录与 Plan fail closed；
- TUI Approve All 不在 BubbleTea `Model.Update` 内同步 `Program.Send`；ACP Allow All 写失败保持 claimed pending 并重新请求权限，真实非运行 Program 与 `io.Pipe` lifecycle 回归覆盖这两个时序；
- image-only custom provider、`base_url:null` 清除、BigModel 空 BaseURL 默认 profile、显式 endpoint 同名模型优先；
- Artifact stale-FD、remote managed content、per-artifact/per-revision viewed CAS 与 same-ID revision reload；
- Web replay 按 JSONL 中的 tool-call occurrence 与宿主 `operation_id` 关联；模型跨 turn 复用 `tool_call_id` 时不会把后一次 Artifact/终态覆盖到前一次审批，漏首帧 WS 会单飞 refresh 后再附着；
- Web/Desktop standalone 图片卡、单调 lifecycle reducer、queued/pre-dispatch cancel media-card suppression、独立 ApprovalBanner、8 横轨 + 8 纵轨 + 6 节点的扫描织网、`3.2s` generating 与独立 `4.6s` saving 收束、`12rem`/`16rem`/`18rem` 占位、success `18rem`/竖图 `22rem` 与 `contain`、reduced-motion 生成/保存静态区分、`aria-busy` 媒体面 sibling live region、typed error、provider/model immutable snapshot，以及无需产品探针的精确 adapter 门控。

最终已知边界不是本地发布旁路：Cloud orchestrator 的严格 approval schema 尚无 opaque `option_id`，因此 Cloud relay 下的 billable provider tools 保持 fail closed；需要按“先 cloud、后 jcode”的跨仓库顺序另行升级。package 源码也仍需在正式发布前按 core → UI 顺序发 npm 并做 registry install smoke，本地 workspace build 不能替代该 gate。
