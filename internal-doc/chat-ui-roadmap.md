# jcode-ui 提升路线图(2026-07)

> **状态(2026-07-11)**:P0–P6 + workflow 画布 + 语音套件 + site 增强已全部落地,双包 0.2.0(未发 npm),site 已部署(/chat-ui 200)。剩余:npm publish、cloud console 升级接入(删 PermissionCard)、Checkpoint(5.5,依赖后端 rewind)。变更明细见 packages/jcode-ui/CHANGELOG.md。

来源:两份独立评审的合并 —— ① 本仓库竞品分析([chat-ui-competitive-analysis.md](chat-ui-competitive-analysis.md),对比 assistant-ui / AI Elements / CopilotKit);② 外部审计(评分:通用完整度 6.5 / coding-agent 垂直 8.0 / 默认好看 7.6 / 定制上限 8.2)。两份结论一致:**差距不在 CSS,在产品状态闭环、组件广度和展示包装;强项是 runtime 底座 + coding-agent 纵深 + 品牌辨识度,不需要推倒重来。**

**战略**:不复制 AI Elements 的 48 个组件,做"最好用的 coding-agent UI"。保住审批安全、工具执行清晰度、subagent/长任务时间线三个优势,补 assistant-ui 的会话闭环与 AI Elements 的视觉完整度,再以 AG-UI adapter 把适用面扩到所有 agent 后端。

**三个受益方**:
- **jcode**(web/desktop,workspace 直连)
- **cloud console**(`/Users/jack/workpath/jjj/cloud/console`,npm 锁 0.1.1;现状:headless Thread + Message/ToolCallCard 直用,但 ApprovalBanner 契约不够用,自维护 PermissionCard;自有 `--jc-*`+语义 `--color-*` token 与库的 `:root --color-*` 并存)
- **生态推广**(npm 已发布 0.1.x,目标:任何 AI agent 的 React 前端)

依赖主线:`P0 门面 → P1 库安全化(scoping/approval 泛化)→ P2 会话闭环 → P3/P4/P5 并行 → P6 远期`。P1 是推广和 cloud 收敛的前置,必须早做(越晚破坏面越大)。

---

## P0 — 门面与信任(推广的前提,全部 S/M)

| # | 事项 | 细节 | 验收 | 量 |
|---|---|---|---|---|
| 0.1 | **修线上 404** | www.j-code.net 部署落后,`/chat-ui` 与 `/chat-ui/docs` 均 404;README 第 5 行 Live demo/Docs 链接全挂。先修 0.2 再用 release-site skill 发布。已建后台任务卡片(task_49d80d13)。 | 两个 URL 200;README 链接可点 | S |
| 0.2 | **修 site dev 报错** | `three` 被 `site/public/showcase-projects/city3d/js/city.js` 引用,vite dep-scan 失败;首页 `<Reveal>` 连锁 React 报错。public/ 下的裸 js 不该被扫描,查 `site/vite.config.ts` 排除,或补依赖。 | `pnpm -C site dev` 首页与 /chat-ui 控制台零报错 | S |
| 0.3 | **Demo 从"自动播放"升级为"可交互产品感"** | 现 demo 播完即静。加:Welcome 空态、Suggestions 起始动作、可输入的 mock 会话(mock runtime 已支持 scriptable)、**light/dark/mobile 三态切换**展示、streaming 回放按钮。 | /chat-ui 页可切三态、可交互发消息 | M |
| 0.4 | **修消息→工具卡垂直空白偏大** | fixture 实测 message 文本与下方 tool 卡之间空白明显(疑 `.jcode-message` padding 与 tool shell margin 叠加,或空 detail 段落)。查 `components.css` §Message polish / §Tool call shell。 | fixture 目测节奏紧凑;web 不回归 | S |
| 0.5 | **components.md 对照表更新** | `site/docs/chat-ui/components.md` 的 assistant-ui 映射表把本路线图的新组件补进"计划中"列,做 roadmap 透明化(assistant-ui 的 deprecation policy 是信任信号,学)。 | 表格含 P2/P3 项的 planned 状态 | S |

## P1 — 第三方安全化(cloud 直接受益;0.2.0 breaking 窗口)

| # | 事项 | 细节 | 验收 | 量 |
|---|---|---|---|---|
| 1.1 | **Token 范围化** | `:root`/`.dark` 全局注入 → `[data-jcode-ui]` 作用域 + `--jcode-*` 前缀(外部审计核心意见)。涉及:`tokens.css`(292 行全量)、`components.css`(1184 行 var 引用)、`animations.css`、组件 inline style 的 var()。dark 模式:`[data-jcode-ui].dark, .dark [data-jcode-ui]` 双选择器(参考 CopilotKit `[data-copilotkit]` 方案)。发 `compat.css` 把旧 `--color-*` alias 到新名,保一个 minor 周期。**注意**:jcode web 自身 theme 生成器(Go palette→CSS)同步改输出前缀。 | cloud console 引入后 `lint:tokens` 通过、`--color-*` 语义命名不再撞车;jcode web 全主题(dracula/nord…)不回归;宿主页面 body 样式零污染 | L |
| 1.2 | **shadcn 互操作桥** | 发可选 `shadcn-bridge.css`:`[data-jcode-ui] { --jcode-primary: var(--primary); --jcode-background: var(--background); … }`(≈15 个映射)。让 shadcn 用户零配置继承宿主主题——吃最大用户池的最低成本动作。 | shadcn 模板项目 import 两个 css 后主题自动一致 | S |
| 1.3 | **Approval 契约泛化** | 现 `Approval` 只有 boolean approved;cloud 的 ACP `permission_request` 是任意 option ID,被迫自维护 PermissionCard(`cloud/console/src/runview/PermissionCard.tsx`)。改:`Approval.options?: ApprovalOption[]`(`{id, label, kind: 'allow_once'|'allow_always'|'deny'|'custom', description?}`),`resolveApproval(id, optionId)`;无 options 时回退现有 allow/deny 渲染(含两步 arming)。ApprovalBanner 渲染 N 选项,`allow_always` 保留 arming 交互。 | cloud 删除自有 PermissionCard 改用库组件;jcode web 不回归;类型向后兼容 | M |
| 1.4 | **ExternalStoreRuntime actions 扩展** | 承接 1.3 的 `resolveApproval` 新签名;同时为 P2 预留 `regenerate`/`submitFeedback`/`retry` 的可选 action 位(全部 optional,host 不实现则 UI 不渲染对应按钮——沿用 canEdit 的 fail-visible 惯例)。 | 类型编译通过;未实现 action 时按钮不出现 | S |
| 1.5 | **发布纪律** | 0.2.0 起:CHANGELOG.md、SemVer 承诺、deprecation policy 页(docs)、迁移指南(0.1→0.2 token 更名表)。 | npm 0.2.0 发布;docs 有 migration 页 | S |

## P2 — 会话闭环(两个产品的及格线三件套+)

| # | 事项 | 细节 | 验收 | 量 |
|---|---|---|---|---|
| 2.1 | **Branch picker + regenerate** | 数据层:`Message.versions?: MessageVersion[]` + `activeVersion`(不动 ThreadItem 结构,版本挂在消息上,编辑/重生成都产生新 version);runtime action `regenerate(messageId)`、`switchVersion(messageId, versionId)`。UI:`‹ 2/3 ›` BranchPicker(hover 显示,复用 msg-actions 槽)。jcode 后端已有 editMessage 重放链路,cloud 无 → cloud 不实现 action 即不渲染(fail-visible)。 | mock runtime 演示分支切换;jcode web 编辑消息后可回看旧版本 | L |
| 2.2 | **Feedback(👍👎)** | `submitFeedback(messageId, 'up'|'down', comment?)` 可选 action + msg-actions 两个按钮 + 已提交态。host 决定落库(jcode 本地 jsonl / cloud API)。 | 按钮出现且状态持久(会话内) | S |
| 2.3 | **Error/retry + 连接状态** | ① assistant 消息失败态:`Message.level='error'` 已有,补 Retry 按钮(action `retry(messageId)`)。② 新组件 `ConnectionBanner`:`disconnected/reconnecting/reconnected` 三态(cloud SSE 断线、jcode ws 重连直接受益),吸附 Thread 顶部,token 化配色用 warning/info。 | 断网模拟下 banner 三态正确;retry 重发成功 | M |
| 2.4 | **Welcome + Suggestions** | 新组件 `ThreadWelcome`(logo slot + 标题 + 副标题)与 `Suggestions`(pill 列表;两个位置:空态 starter、turn 结束 follow-up)。数据:host 传静态列表或回调 `getSuggestions(ctx)`(AI 生成留给 host)。Thread 空态自动渲染 Welcome(可关)。 | 空态不再是白屏;点击 pill 即发送 | M |
| 2.5 | **会话导出** | `exportMarkdown(items)` 纯函数(core)+ Thread 头部可选下载按钮。工具调用导出为折叠代码块,审批导出为引用块。 | 导出的 md 在 GitHub 渲染正常 | S |
| 2.6 | **Quote 选区引用** | 选中消息文本浮出"引用回复"按钮 → composer 前置 `> …` 引文块。参考 assistant-ui SelectionToolbar。低风险可后置。 | 选区引用进 composer | M |

## P3 — Composer 二代

| # | 事项 | 细节 | 验收 | 量 |
|---|---|---|---|---|
| 3.1 | **通用文件附件 + AttachmentAdapter** | 现仅 base64 图片。定义 `AttachmentAdapter`(`add(file) → {id, kind, name, size, url?, progress$}`,host 决定上传/内联),支持 pdf/文本/任意文件 chip(类型图标+大小),图片走现有 ChatImage 快路径。上传进度条 + 失败重试。 | cloud(有对象存储)与 jcode(本地内联)各接一个 adapter 跑通 | L |
| 3.2 | **拖拽 + 粘贴** | drop zone 覆盖 Thread+Composer,粘贴截图直接成附件(AI Elements 的 add-screenshot 对标,但作为粘贴而非主动截屏)。 | 拖 3 类文件、⌘V 截图均成 chip | M |
| 3.3 | **Composer slots** | `ChatInput` 增加 `leadingControls`/`trailingControls`/`footer` 三个 slot props(外部审计"styled 层组合粒度不够"的最小解——一层 slot,不做递归)。jcode web 的 model/mode/workspace picker 从产品层移入 slot 用法示范;库内提供可选 `ModelSelector` styled 件(数据 host 供给)。 | jcode web 用 slot 装回现有 picker,零视觉回归 | M |
| 3.4 | **语音输入(可选,最后)** | Web Speech API 听写按钮,`enableDictation` 开关,默认关。纯增强,不做 voice 会话(与定位一致;仅为"通用库"补票)。 | Chrome 下听写进 textarea | M |

## P4 — 富渲染

| # | 事项 | 细节 | 验收 | 量 |
|---|---|---|---|---|
| 4.1 | **Streaming markdown 渐进渲染** | 现 marked 全量重渲。改:按块增量解析,未闭合 fence/表格稳定渲染不闪(Streamdown 思路);流式中代码块尾部 shimmer(animations.css 已有 shimmer 基建)。 | 长代码流式无闪烁;CPU 火焰图无全量重排 | L |
| 4.2 | **代码块 chrome** | 文件名/语言头 + 复制按钮 + 行号可选(现在纯 pre+hljs,无任何 chrome,已确认)。 | md 代码块带头部与 copy | S |
| 4.3 | **Mermaid + KaTeX 可选插件** | `jcode-ui/plugins/mermaid`、`/katex` 子入口,peer dep 动态 import,不进主包(守住 tree-shakeable 卖点)。 | 不装插件包体不变;装后图表/公式渲染 | M |

## P5 — 组件位阶提升(coding-agent 纵深变现)

| # | 事项 | 细节 | 验收 | 量 |
|---|---|---|---|---|
| 5.1 | **TaskList 独立组件** | todo renderer(189 行)→ 一等 `TaskList`(compound:Item/Status/Progress),runtime `todos` 已在 RuntimeState。cloud run 详情页、jcode goal 面板复用。 | 两产品各一处落地 | M |
| 5.2 | **FileTree / TestResults / StackTrace renderer** | 对标 AI Elements Code 套件但 runtime-wired:FileTree(list_dir/glob 输出)、TestResults(go test/vitest 解析)、StackTrace(panic/throw 高亮+路径可点)。注册进 default registry。 | fixture 三个新 renderer 各一屏 | L |
| 5.3 | **Artifact 容器** | 生成物卡片(标题+类型+动作条+内容区),先做卡片形态,不做侧栏画布(那是产品层)。file-viewer/diff 可作为 Artifact 内容复用。 | mock 演示"生成一个文件"场景 | M |
| 5.4 | **Message/ToolCallCard slots** | 与 3.3 同思路:`avatar`/`header`/`footer`/`actions` 一层 slot。cloud 的 AttributedUserMessage(author 署名)就是现成需求——slot 化后 cloud 删自有包装。 | cloud Timeline 改用 slot,删 AttributedUserMessage | M |
| 5.5 | **Checkpoint(依赖后端)** | 会话回滚标记 UI。**前置**:jcode 后端 rewind 能力;无则不做纯摆设 UI。先立项调研,不排期。 | — | ? |

## P6 — Agent-App 协作(远期,一季度后再评估)

| # | 事项 | 细节 | 量 |
|---|---|---|---|
| 6.1 | **AG-UI runtime adapter** | `createAGUIRuntime(endpoint)`:AG-UI 16 事件 → RuntimeState 映射。杠杆:AG-UI 生态(LangGraph/CrewAI/Mastra/Bedrock…)的 UI 侧现在只有 CopilotKit/assistant-ui 两个选择,这是"推广给所有 agent"的最短路径,让非 jcode 后端零胶水接入。 | L |
| 6.2 | **ThreadList 契约** | `ThreadStore` 接口(list/create/rename/archive)+ 默认侧栏 UI。jcode session 列表与 cloud run 列表是两个现成实现。 | L |
| 6.3 | shared agent state / typed generative registry / interrupt contract | CopilotKit 对标,等 1-6.2 站稳再议。 | — |

## 生态推广 workstream(与功能并行,都是 S)

| # | 事项 | 细节 |
|---|---|---|
| E1 | npm 包面:keywords(`ai`,`chat`,`agent`,`react`,`assistant-ui-alternative`…)、README badges、社交卡图 |
| E2 | **对比页**:docs 增 "vs assistant-ui / AI Elements / CopilotKit" 诚实对比(何时选谁),SEO 主入口 |
| E3 | **agent skill**:发 `jcode-ui` 使用 skill(对标 `npx skills add vercel/ai-elements`),llms.txt 已有,补组件级 llms-full |
| E4 | StackBlitz 一键模板 ×3:minimal(mock)/zustand/AG-UI(6.1 后) |
| E5 | 发布节奏公开:GitHub Releases + changelog 页;每个 minor 一篇短 blog(site 已有 changelog.md 基建) |
| E6 | 首发内容:0.2.0(scoping)+ 0.3.0(会话闭环)后做一轮 Show HN / X 发布;演示视频用 fixture 录 |

## 排期建议(单人节奏)

- **第 1-2 周**:P0 全部 + 1.5(门面即刻可信)
- **第 3-5 周**:1.1–1.4(0.2.0 发版;cloud 同步升级、删 PermissionCard)
- **第 6-9 周**:P2(0.3.0;jcode web 全量接入)+ E1/E2
- **第 10-13 周**:P3(0.4.0)+ E3/E4
- **之后按季度**:P4 → P5 → P6,每阶段先在 jcode/cloud 落地再发版(dogfood-first)

## 明确不做

- 语音会话套件、workflow 画布、RN/多框架端(规模游戏,偏离定位)
- shadcn copy-in 分发切换(保 npm 包 + token/slot 定制路线;copy-in 的升级成本对两个自家产品是负资产)
- 48 组件军备竞赛
