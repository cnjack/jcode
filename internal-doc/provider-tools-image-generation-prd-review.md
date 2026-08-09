# Provider Tools 与图片生成 PRD — Grok 4.5 评审记录

- 被评审文档：`internal-doc/provider-tools-image-generation-prd.md`
- Reviewer：本机 Grok CLI，model `grok-4.5`
- 日期：2026-08-08
- 最终结论：**GO — 可以进入技术设计**
- 评审范围：只读 PRD 合同；未向外部 reviewer 发送仓库源码、配置、密钥或实测原始响应

> 2026-08-08 产品决策更新：用户明确要求不把 capability test、`未测试/已验证` 等研发诊断状态放入产品。图片生成收敛为独立 `image_model` 角色：选择有效模型即在 normal mode 注册工具，不存在 provider image enable、session image enable 或 image-specific session grant。Ask for approval / Auto 每次真实生成走 `billable_external`；Full access 是统一会话预授权，不再询问。Provider Web Search 等 provider-bound tools 只保留 provider policy，并且只能跟随当前 Agent/Chat Model 的 provider exact adapter，不能跨 provider 注入；Composer/TUI/ACP 不再提供会话级工具开关。正式生成态动效最终选择扫描织网：8 条横轨、8 条纵轨、6 个节点，generating `3.2s`，saving 使用独立 `4.6s` 收束；`prefers-reduced-motion` 下两状态仍以静态几何区分，live region 位于 `aria-busy` 媒体面的 sibling。下文保留 Grok 当时评审记录以便追溯，被这些后续决策取代的建议按当前合同解释。

## 1. 评审方法

先由 JCode 侧核对仓库现状、官方资料和本机最小 live smoke，再把不含秘密的 PRD 副本放入隔离临时目录交给 Grok。Grok 禁用 Web Search、Memory 和 subagents，只允许审阅该目录中的 PRD。

采用两轮 gate：

1. 首轮要求给出 `GO/NO-GO`、P0 阻断项、P1、事实校正与最终发布门；
2. JCode 逐条修订 P0；
3. 第二轮只复核原 5 个 P0、检查新增 P0，并判断是否可以进入技术设计。

## 2. 首轮结论：NO-GO

Grok 认为总体方向、安全原则与四端交付意图完整，但有 5 个需求合同级阻断项。

### P0-1：图片卡状态机和事件合同冲突

首轮证据：

- PRD 状态机包含 `queued -> generating -> saving`；
- 旧事件描述却要求 `tool_call(generate_image)` 直接创建 generating card。

影响：审批前后和 provider request 是否已经发出没有统一事实，无法保证“拒绝审批时零调用/零计费”。

修订：

- `OnToolCall` 只创建内部 queued occurrence；Web/Desktop 不为 queued 渲染媒体卡，独立 ApprovalBanner 承担可见等待态；
- 审批通过且 dispatch 前发送 generating；
- provider 结果可读、进入下载/校验/持久化时发送 saving；
- 使用 optional `ToolProgressHandler`，不强制破坏全部既有 handler；
- 审批拒绝/queued 取消时明确 provider 请求数为 0，`approval_denied` / `cancelled_before_dispatch` 终态也不渲染媒体卡。

### P0-2：能力四元组没有进入可执行模型

首轮证据：PRD 宣称能力由 profile、credential、Base URL、model 共同决定，但旧 `CapabilityDescriptor` 缺少 profile 与 endpoint 绑定，也没有 mismatch 算法。

修订：

- 新增 `CapabilityKey{ProviderProfileID, CredentialKind, EndpointProfile, ModelID}`；
- 定义内置 endpoint 精确 allowlist、Base URL canonicalization 和 custom endpoint fingerprint；
- custom/反代默认 availability=`unknown`，不能继承供应商私有 adapter；
- `image_model` 同样走完整 key，禁止按 CogView/Wan/Qwen-Image 名称猜协议。

### P0-3：availability / policy 与发布状态混用

首轮证据：旧文档又出现未定义的 `unverified`，且“付费工具默认关闭”和配置示例 `enabled:true` 冲突。

Grok 评审时曾建议拆成 availability / verification / policy 三轴。后续产品决策进一步收敛：用户凭证诊断不进入产品，也不要求用户先发起一张可能计费的测试图片。图片生成只保留发布版 adapter availability 与 `image_model` role selection，选择即注册；Provider Web Search 等 provider-bound tools 只保留 provider policy。凭证与 endpoint 变化由每次真实调用前的 runtime revalidation 保护，provider-bound 能力还必须绑定当前 Chat Model exact provider。

### P0-4：Autopilot 的生图费用授权不明确

首轮证据：“不加入 noApprovalNeeded”不能推出 Autopilot 是否仍会自动批准，存在连续生图计费风险。

修订：

- 新增 `billable_external` approval class；
- Manual 和 Autopilot 都不能静默批准；
- 默认批准只对应一次完整 request tuple 和一个 idempotency key；
- 不提供图片专属 session/global grant；Ask for approval / Auto 中切 model/provider、增加 count 或加入 reference 都必须形成新的逐次批准；
- Ask for approval / Auto 中每次真实图片生成都使用独立的一次性批准；Full access 不生成 approval request，但不能绕过 typed intent/runtime/quota。

### P0-5：Provider Search policy 和 native/MCP runtime 可能串线

首轮证据：旧配置把 `mechanism=provider_mcp` 放在 `builtin_tools`，同时又存在 `ApplyNativeTools` 和 MCP preset，容易双开/双搜/双计费。

修订：

- 用户配置只保存 `ProviderToolPolicy`；runtime 不可编辑，只由 `CapabilityKey` 解析；
- resolver 先锁定当前 Agent/Chat Model 的 provider/profile/model；不得从其他已配置 provider 跨 provider 注入 Search；
- 定义 `responses_harness | builtin_function | formula | mcp_tool`；
- 每个 key/capability 最多一个 runtime；
- server-native lifecycle 由 adapter 吸收，不进入 ToolsNode；
- BigModel Search 是 MCP-backed Eino tool proxy，只由 ToolsNode 转发到官方 MCP，不再注入 native search；
- native 与 MCP runtime 双开必须 fail fast；本地 function、MCP-backed 与 native policy 不互相覆盖。

## 3. 第二轮结论：GO

Grok 第二轮逐项给出的结果：

| 原 P0 | 复核结果 | 主要闭合证据 |
| --- | --- | --- |
| queued/generating/saving 与事件冲突 | `closed` | §7.4、§11.3 的事件驱动状态与 optional progress contract |
| capability 四元组不可执行 | `closed` | §9.1 的 CapabilityKey 与六步 resolver 算法 |
| capability 与发布语义混用 | `closed` | 图片 adapter availability × Image Model role；provider-bound availability × policy；无产品探针 |
| Autopilot 计费授权不明 | `closed` | §10 逐次 `billable_external`、无 session grant、idempotency |
| Search policy/runtime 串线 | `closed` | §8/§9.2 的 policy/runtime 拆分和互斥不变量 |

Grok 的最终原文结论为：

> Verdict: GO
>
> 上一轮 5 个 P0 均已在可执行合同层闭合；未发现新的自相矛盾、不可实现或安全/费用授权缺失。剩余事项可进入技术设计。

新增 P0：**无**。

## 4. 带入技术设计的 P1 项

这些不是 PRD 阻断项，但技术设计必须逐项落地：

1. progress sink 的 context 注入和 runner/handler 时序；
2. 图片调用逐次 approval option 的持久化与 reconnect 规则；图片不设计 session grant；
3. Base URL canonicalize 的精确规则，包括 scheme、host、path、尾斜杠和 query；
4. adapter contract test 与 maintainer live smoke 仅作为发布门，不进入产品 API 或用户流程；
5. native/MCP 双开 fail-fast 的断言位置和错误码；
6. sync/async adapter 的 idempotency 查询/恢复接口；
7. 切 Chat Model 后按 exact provider 重新解析 provider-bound policy 的算法；图片生成不进入该解析；
8. Managed Artifact opaque ID 与既有 workspace Artifact path 解析兼容；Cloud schema 如需变化继续 defer 到 P1。

## 5. 最终 gate 条件

1. CapabilityKey 覆盖 preset/custom Base URL，custom 不继承私有 adapter，无精确规则不 fallback。
2. 图片 adapter availability 与 `image_model` selection 统一驱动 picker、工具注册和正常调用；provider-bound availability/policy 只驱动当前 Chat Model exact provider 的能力。
3. 产品不暴露 credential verification/probe 状态；mock 不代替发布前 provider live gate。
4. Ask for approval / Auto 的每次 `billable_external` 都必须获得当前一次性批准；Full access 不询问。拒绝时 provider request count 为 0；无论模式，一个 intent/OperationID 只提交一次。
5. 每个 CapabilityKey 只有一个 Search runtime；native lifecycle、MCP proxy 和 local function tools 不串线。
6. 内部事件状态从 queued 到 optional progress，再以 `tool_result.artifacts`/`artifact_upserted` 对账；Web/Desktop queued 只显示独立 ApprovalBanner，不显示媒体卡。无 progress 时 approval UI 直接进入 terminal，pre-dispatch deny/cancel 仍无媒体卡。
7. P0-B BigModel 搜索、生图、Artifact v2、四端交付与 `jcode-ui` 发布形成独立可发布闭环，不依赖 Qwen/Kimi live gate。
8. HTTPS/host allowlist、MIME sniff、下载/像素上限、原子落盘与内容不进日志/JSONL/WS 是安全硬门。
9. Web/Desktop 生成态使用 8 横轨 + 8 纵轨 + 6 节点的扫描织网；generating 为 `3.2s`，saving 的轨道和节点均使用独立 `4.6s` settle keyframes。竖/方/横占位分别为 `12rem`/`16rem`/`18rem`，success 最大 `18rem` 且竖图最大高度 `22rem`；reduced-motion 静态状态可区分，live region 不得嵌在 `aria-busy` subtree 内。
