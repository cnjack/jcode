# Artifact UI 设计说明

- 状态：Prototype review
- 原型：`internal-doc/artifacts-ui/index.html`
- 目标 surface：Browser Web、Tauri Desktop
- 明确排除：CLI/TUI、ACP

## 1. 设计假设

- 叙事角色：Agent delivery workbench。Artifact 是一次任务的可消费交付物，不是第二套文件浏览器。
- 观看距离：桌面/笔记本约 1 米，优先高信息密度、稳定布局和键盘可达。
- 视觉语气：沿用 JCode 当前克制、工具化、单色为主的界面；橙色只用于主动作和关键焦点。
- 信息密度：对话、Artifact 历史、预览内容和分享状态经常需要同时可见，不能依赖只有一张大卡片的低密度布局。

## 2. 三种交互变体

### A. Docked Workbench（推荐）

Artifact 使用现有右栏的信息架构。列表固定在右栏顶部，Viewer 在同一栏内展开；用户可把栏宽拖到约 480px，并进一步全屏。

- 优点：与 Plan / Files / Changes 一致；对话与交付物可并排核对；实现风险最低。
- 缺点：窄屏或复杂 HTML/PDF 仍需切到全屏。
- 适用：默认路径、持续查看日志/报告、绝大多数 coding-agent 任务。

### B. Focus Canvas

打开 Artifact 后，Viewer 成为主工作区画布，顶部保留 Artifact strip 和“返回对话”。右栏索引退化为一条紧凑 rail。

- 优点：最大化 HTML、PDF、图片和 CSV 的可读面积。
- 缺点：打断对话上下文，频繁往返时成本更高。
- 适用：深度阅读、演示报告、视觉验收。

### C. Inline Quick Look

点击 tool result card 后，在对话中直接展开有界预览；右侧只保留可展开的 Artifact 索引。用户可从 Quick Look 升级到右栏或全屏。

- 优点：几乎不打断阅读流；“Agent 刚交付什么”最清楚。
- 缺点：长内容会挤压对话，历史 Artifact 的可发现性弱于 A。
- 适用：Markdown 摘要、小图、短 CSV、快速确认。

## 3. 推荐组合

生产实现采用 A 作为默认框架，同时吸收 B 和 C：

1. `show_artifact(focus=true)` 打开 A 的右栏 Viewer。
2. tool result card 提供 C 的小型 Quick Look，仅对适合的短内容启用。
3. Viewer 的 Expand 动作进入 B 的 Focus Canvas；再次 Expand 才进入浏览器 fullscreen overlay。
4. 三种方式共享同一个选中 Artifact、renderer 和分享状态，不创建三套数据模型。

## 4. 分享状态设计

- 未登录：整个分享动作不存在，不显示锁、登录提示或 disabled button。
- 已登录、未分享：Viewer header 显示 `Share`。
- 上传中：显示进度、文件 revision 和取消；本地预览保持可用。
- 已分享：提供 Copy link、expiry 和 Revoke。
- stale share：同时展示 `Shared rev 2` 与 `Current rev 3`，旧链接继续有效；明确按钮为 `Share latest`。
- expired / revoked：保留历史状态，不制造仍可访问的错觉；可以重新分享当前 revision。
- Cloud 错误：只影响分享区域，不覆盖 Viewer，不改变本地 Artifact 状态。

## 5. 关键视觉规则

- Artifacts tab 与现有 panel tab 同权，不做全局主导航。
- 列表行优先显示标题；relative path 使用单行 mono 辅助文本。
- 类型与 revision 使用低强调 chip；missing/error 使用语义色但不整块染红。
- Viewer header 始终保留文件身份、revision、Download/Expand；Desktop 才增加 Open/Reveal。
- HTML 预览里显示“Sandboxed · network blocked”作为安全状态，而不是技术报错。
- 动画只用于 panel/overlay 过渡，支持 `prefers-reduced-motion`。

## 6. 原型交互覆盖

- 切换三种展示方案。
- 从对话 tool card 打开 Artifact。
- 在三个 Artifact 间切换，覆盖 HTML、Markdown、CSV。
- 切换 Cloud 登录状态；验证未登录时 Share 完全隐藏。
- 切换 unshared、uploading、shared、stale、expired、revoked 状态。
- Copy link、Revoke、Share latest、Expand、返回对话。
- 全屏 Viewer、关闭 Viewer、重新打开。
- 桌面宽屏和较窄 Web viewport。

## 7. 评审评分

已使用 Chromium 在 1440×900 和 1024×768 viewport 执行交互回归，并逐张检查 Docked、Focus Canvas、Quick Look、logged-out、shared 和 stale-share 截图。自动化脚本为 `internal-doc/artifacts-ui/prototype.test.cjs`。

| 维度 | 分数（10） | 结论 |
| --- | ---: | --- |
| 信息架构 | 9.2 | 三层结构和三种 presentation 共用同一 Artifact 选择；默认路径明确。 |
| 视觉层级 | 8.8 | 列表、Viewer、内容层级稳定；分享 popover 不会夺走整个页面。 |
| 交互完整度 | 9.0 | 已覆盖打开、切换、全屏、登录门、上传、复制、撤销、stale/expired/revoked。 |
| 产品一致性 | 9.1 | 沿用 JCode sidebar、titlebar、right panel、黑灰表面和克制橙色。 |
| 无障碍与韧性 | 8.6 | 支持键盘 focus、Esc、快捷键、reduced motion 和窄桌面布局；生产实现仍需完整 i18n/ARIA 测试。 |

综合：8.94 / 10，UI 架构可以冻结进入工程设计。

### 评审后修改

- 增加全局 `[hidden]` 规则，保证未登录时 Share 在任何按钮 display 样式下都真正消失。
- Focus Canvas 的安全状态改为随 renderer 更新，Markdown/CSV 不再错误显示 HTML sandbox 文案。
- 将 Artifact path/revision 辅助文字提高到至少 10px，避免高密度布局中不可读。
- 窄 viewport 只保留 active panel tab，避免 tab strip 把 Viewer action 挤出屏幕。
- stale-share 同时显示 shared revision、current revision 和旧链接语义，避免“Share latest”像覆盖原链接。

### 生产实现注意

- 原型中的字符图标只是交互占位；生产 React 组件必须使用现有 Heroicons outline。
- 原型内的报告内容明确标注为 sample，不可当成真实发布数据或遥测。
- 生产浏览器测试需增加 screen reader name、focus trap、200% zoom 和真正 `prefers-reduced-motion` 断言。
