# JCode Artifacts PRD 双模型评审

- 评审日期：2026-08-01
- 被评审文档：`internal-doc/artifacts-prd.md`
- Kimi CLI：`kimi-code/kimi-for-coding-highspeed`（OAuth 默认模型）
- Grok CLI：请求模型 `grok-4.5`
- 评审方式：两个 CLI 独立只读仓库与 PRD，不允许修改文件
- 原始结论：Kimi = NO-GO；Grok = NO-GO

## 1. 共识结论

两位评审都认为产品方向正确，尤其认可以下边界：

- Artifact 必须显式登记，不能扫描整个工作区；
- Web/Desktop 共用产品 UI，CLI/TUI/ACP 不暴露工具；
- `show_artifact` 只做本地交付，不因 Cloud 登录自动上传；
- Cloud 分享由用户显式触发，独立于 session sync；
- Cloud 分享必须是不可变 revision 快照，Cloud 只保存密文。

当前版本仍不能冻结为工程合同，必须先修复以下 P0：

1. 明确本地 revision 只是“登记代数”，不是内容快照；本地 Viewer 始终读取工作区当前文件。
2. 明确 Web 的 `task_id` 与 session UUID 的映射，避免 API、JSONL 和前端 store 分裂。
3. 补齐 automation/background run 的工具注册、未读入口和回放 UI。
4. 把真实 Kimi Web E2E 与 Cloud K8s 部署写成发布门禁。
5. Artifact 分享密钥不能简称 ASK；Cloud 已使用 ASK 表示 Account Sync Key。
6. PRD 必须写清 URL fragment `share_key`、Cloud 无明文密钥、禁止完整分享 URL 进入日志/事件/session。

## 2. Kimi 评审重点

- MVP 与 Phase 3 内容混写，需要显式阶段标签和分开的验收门禁。
- 当前 `RightPanel` 只有 Plan/Files/Changes 且宽度上限 600px；Artifact list 与沉浸式 Viewer 应拆成可验证的两种呈现状态。
- 自动化能持久化 Artifact 不等于用户在 automation run 页面能发现它；需要写清入口。
- HTML/SVG/Markdown 的 sandbox、CSP、`nosniff` 和 raw HTML 策略必须提升到 PRD，而不是只存在设计文档。
- CLI/TUI/ACP 负面验收需要同时覆盖 eager/static list 与 Tool Search catalog。
- Cloud 分享必须在上传期间锁定 revision/digest，防止生成半旧半新的密文对象。

## 3. Grok 评审重点

- 本地 revision 与 Cloud immutable snapshot 是两种语义，必须用表格明确区分。
- `Artifact Share Key (ASK)` 与现有 Account Sync Key 冲突，统一改为 `Artifact Share Secret` / `share_key`。
- 分享动作只依赖有效 device 登录，不依赖 relay online、`auto_connect` 或 session sync。
- 分享前后 `cloud-sessions.json` / `/api/cloud/sync` 状态必须保持不变，作为隐私负面验收。
- 未登录本地 happy path、后台 automation、同路径多 revision、大文件、远程切换和多窗口 focus 都需要完整用户流程。
- PRD 中不能写本机绝对仓库路径，应引用 JCode Cloud 合同而不是个人目录。

## 4. 放行条件

PRD 修订后满足以下条件才可进入架构冻结：

- Phase 1 与 Phase 3 的需求、验收、测试门禁分离；
- local revision、Cloud snapshot、task/session identity 已无歧义；
- 至少三种 UI 展示方式有可点击原型并完成专家评审；
- Kimi/Grok 对修订后的架构设计给出无 P0 的结论；
- 测试矩阵覆盖 unit、integration、browser E2E、Kimi 真模型、Cloud K8s 与 ciphertext audit。

## 5. 修订后门禁复审

同日使用相同两个 CLI 对修订后的 PRD 再次进行只读 P0 门禁评审：

| Reviewer | 上一轮 P0 | 新 P0 | 结论 |
| --- | --- | --- | --- |
| Kimi CLI | 全部 closed | 无 | GO |
| Grok CLI (`grok-4.5`) | 全部 closed | 无 | GO |

复审确认 local revision、task/session identity、automation、真实 Kimi Web E2E、Cloud K8s、`share_key`、URL fragment E2EE 和 Phase 1/3 边界均已进入产品合同。PRD 可以作为 UI 与架构冻结输入。
