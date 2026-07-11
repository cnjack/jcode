# jcode-ui 竞品设计对比(2026-07)

对比对象:**assistant-ui**(YC,11k★)、**Vercel AI Elements**(2.2k★,npm CLI ~39.5k/周)、**CopilotKit**(35.9k★,$27M A 轮)。
视角:设计 —— 完整度 + 视觉质感。数据来自 2026-07 官方文档/GitHub 调研 + jcode-ui 源码走读 + fixture 实际渲染。

---

## 1. 四家定位一句话

| 库 | 定位 | 分发方式 | 硬依赖 |
|---|---|---|---|
| **jcode-ui** | coding-agent 会话 UI(工具调用/审批/ask-user 深耕) | npm 包(styled + headless core 双包) | 无(消费端只 import CSS,不要求 Tailwind) |
| **assistant-ui** | "ChatGPT 的 UX 装进你的 React 应用",通用 chat 全家桶 | npm primitives + shadcn registry copy-in | Tailwind + shadcn(styled 层) |
| **AI Elements** | AI SDK 官方配套 UI,五大类 48 组件 | 纯 shadcn copy-in(npm 只是安装器) | Next.js + AI SDK + shadcn + Tailwind 4 + React 19 |
| **CopilotKit** | "Agent 前端栈",AG-UI 协议 + 生成式 UI + HITL | npm 包(v2 shadcn 化) | 架构绑定重(runtime + 协议) |

## 2. 完整度矩阵

✅ 有 · 🟡 部分/产品层 · ❌ 无

| 能力 | jcode-ui | assistant-ui | AI Elements | CopilotKit |
|---|---|---|---|---|
| Thread(虚拟化+auto-follow) | ✅ | ✅ | ✅(stick-to-bottom) | ✅(TanStack Virtual) |
| Composer(附件/停止/快捷键) | ✅ +slash+队列 | ✅ +slash+@mention+历史+引用选区 | ✅ +截屏+语音输入+模型选择 | ✅ +语音转写+建议 |
| 消息编辑 | ✅ | ✅ | ✅ | ✅ |
| **分支切换(branch picker)** | ❌ | ✅ | ✅ | ✅ |
| Regenerate / feedback(👍👎) | ❌ | ✅ | ✅ | ✅ |
| 建议 pills(空态/跟进) | ❌ | ✅(+AI 生成) | ✅ | ✅(+AI 生成) |
| Reasoning/CoT | ✅ | ✅(2 种) | ✅(2 种) | ✅ |
| Sources/引用 | ✅ | ✅ | ✅ +inline citation hover | 🟡 |
| 工具调用卡片 | ✅ 注册表+9 renderer | ✅ toolkit+fallback+group | ✅ Tool(4 状态) | ✅ Zod schema 渲染 |
| **终端/diff/文件树等编码域渲染** | ✅ 深(runtime-wired,双通道 stdout/stderr+exit+耗时+截断) | 🟡 Diff Viewer | ✅ 广(15 个 Code 组件,但纯展示) | ❌ |
| 审批/HITL | ✅ 两步 arming+外部路径标记 | ✅ requires-action/approval | ✅ Confirmation(薄) | ✅ useHumanInTheLoop 状态机 |
| ask-user 中途提问 | ✅ 一等公民 | 🟡(interrupt 拼装) | ❌ | 🟡(interrupt) |
| Token/上下文用量 | ✅ ContextBar 环+明细 popover | ✅ Context Display | ✅ +成本估算(tokenlens) | ❌ |
| 计划/任务(todo/plan) | ✅ todo renderer+goal | 🟡 | ✅ Plan/Task/Queue/Checkpoint | 🟡 shared state |
| 子代理/多 agent 嵌套 | ✅ children 递归+team renderer+exploring 聚合 | 🟡 multi-agent 指南 | 🟡 Agent 卡片 | ✅ 协议级 |
| 生成式 UI(模型产出组件) | ❌ | ✅ JSON spec+编译校验 | ✅ JSXPreview | ✅ A2UI/MCP Apps |
| 语音 | ❌(明确不做) | ✅ | ✅ 6 组件套件 | ✅ 转写输入 |
| Mermaid/LaTeX/Streamdown | ❌(marked+hljs) | ✅ 全有 | ✅(Streamdown+KaTeX) | ✅(KaTeX) |
| ThreadList/历史 | 🟡 产品层 | ✅ +Cloud 持久化 | ❌ | ✅ 企业版 |
| 白板/工作流画布 | ❌ | ❌ | ✅ React Flow 7 组件 | 🟡 canvas demo |
| 移动/终端端 | ❌ | ✅ RN + ink | ❌ | ✅ RN+Angular+Vue |
| DevTools | ❌ | ✅ | ❌ | ✅ inspector |

**量化感受**:通用广度 AI Elements(48 组件)> assistant-ui > CopilotKit > jcode-ui(12 styled + 6 headless + 9 renderer);but 编码 agent 纵深 jcode-ui 第一 —— 双通道终端、exploring 聚合、subagent 树、两步审批、ask-user,这五件事三家都没有做到你这个完成度,AI Elements 的 Code 套件组件多但都是静态展示件,没接 runtime 语义。

## 3. 架构/主题化对比

- **jcode-ui**:双包(styled/headless)+ ChatRuntime(ExternalStore/Mock)+ ToolRendererRegistry。与 assistant-ui 的三层架构(UI/Runtime/Backend)同构,规模小一个数量级(~6k 行)。主题 = 纯 CSS 变量(radius/shadow/motion/z-index/accent-wash/neutral-wash/hljs×2/xterm×2 全 token 化),`.dark` class 切换,color-mix 派生 wash。**消费端零 Tailwind 依赖**是差异点(三家 styled 层全绑 Tailwind/shadcn)。
- **assistant-ui**:headless primitives(npm)+ styled(shadcn copy-in,代码归你)。定制梯子:改源码 → components prop → 换 renderer → 纯 headless。
- **AI Elements**:全 copy-in,继承宿主 shadcn 主题("existing themes apply automatically"),自身零 token 体系。
- **CopilotKit v2**:oklch shadcn token 挂 `[data-copilotkit]`,五级定制梯(token → Tailwind slot → props → 换组件 → headless)。

jcode-ui 的取舍:npm 包 + CSS 变量 = 升级容易、定制边界窄(改不了 DOM 结构,只能覆 token 或下沉 headless 自己拼)。shadcn copy-in 派 = 定制无上限、升级靠手动 diff。两条路线都成立,但 **shadcn 生态互操作缺失**(`--color-primary` vs shadcn `--primary`)会挡住最大的潜在用户群。

## 4. 好看程度(视觉质感)

- **三家默认皮全部是 shadcn 中性风**:assistant-ui 刻意仿 ChatGPT;AI Elements 是 flat shadcn(user 气泡 secondary 底、assistant 全宽平铺);CopilotKit v2 是 Radix+Lucide 的标准干净。共同问题:同质化,"AI 应用长得都一样"。
- **jcode-ui 有真实的自有品牌语言**:暖橙 accent 但克制使用(orange 只给 hero/send,其余控件走 neutral wash,"calm and monochrome");warm-tinted 阴影梯;Geist + JetBrains Mono;无气泡 flat 消息布局(avatar+role label+全宽 prose,更像 Claude/Linear 的编辑器感);终端卡失败态橙描边 + exit code 语义色。fixture 实测:exploring 组、终端双通道、截断标记的排版都干净利落。
- 结论:**辨识度你赢,普适性他们赢**。作为独立库推广时,"有个性的默认皮"是双刃剑 —— 用户第一眼印象好,但想融入自家品牌时你只有 token 覆盖一条路。
- 实测小瑕疵:消息文本与下方工具卡之间垂直空白偏大(fixture 中 message.detail 空段落?值得查);Thread 内滚动在嵌入宿主页时手感需要再验证。

## 5. 生态信号(客观差距)

| | jcode-ui | assistant-ui | AI Elements | CopilotKit |
|---|---|---|---|---|
| Stars | ~0(内部) | 11k | 2.2k(CLI 39.5k DL/周) | 35.9k(AG-UI 14.7k) |
| 资金 | — | YC pre-seed | Vercel | $27M Series A |
| 文档 | site docs+live demo+生成 API(**但线上 /chat-ui 404,部署落后**) | llms.txt+25 指南+deprecation policy | 每组件 live preview+Figma kit+agent skill | 全面但 v1/v2 混杂 |
| 迭代 | 内部节奏 | 高频 | 11 个月 20 release | 高频 |

## 6. 差距收敛建议(按 ROI)

1. **门面**:重新部署 site —— README 第一个链接(live demo)现在是 404。
2. **Branch picker + regenerate + 👍👎**:表驱动数据结构已支持 editMessage,补 UI 成本低,是"完整 chat 库"的及格线三件套。
3. **建议 pills(空态+跟进)**:感知强、成本低。
4. **Streaming markdown 渐进渲染**(Streamdown 式)+ Mermaid/KaTeX:文档/研究型输出场景的硬需求。
5. **shadcn 互操作层**:发一个 token 别名映射(shadcn `--primary` → `--color-primary`)或 Tailwind preset,吃到最大用户池。
6. 不建议跟进:语音、workflow 画布、多端 —— 与 coding-agent 定位无关,是三家的规模游戏。

## 7. 顺手发现的问题

- www.j-code.net 部署落后于仓库:`/chat-ui` 路由 404(README/docs 多处链接指向)。
- `site` dev 启动报错:`three` 被 `site/public/showcase-projects/city3d/js/city.js` 引用但未安装,vite dep-scan 失败,首页 `<Reveal>` 组件连锁报错。
- `.claude/launch.json` 新增了 `jcode-ui-fixture` 配置(经 `web/fixture-tool-ux` 跑 vite,端口 5199),可视化验收 jcode-ui 渲染。
