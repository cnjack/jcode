# 图片生成 POC 报告

> 日期：2026-08-08
> 状态：POC 通过，允许进入技术设计；不代表功能已经可发布

## 1. 要回答的问题

本 POC 只验证首个发布切片中最不确定的执行边界：JCode 能否用一个独立于聊天模型的 adapter 调用 OpenAI Images 兼容端点，安全接收并验证供应商返回的图片，并把它作为后续 `generate_image` 工具和 managed Artifact 的共同输入。

POC 不修改用户配置 schema、不注册生产工具、不实现最终 UI，也不把 BigModel 的成功外推为 Qwen/Kimi 能力。

## 2. 实现

新增 `internal/imagegen` 包：

- `Generator` / `Request` / `Result` provider-neutral seam；
- `openai_images` 协议 adapter，固定调用 `{base_url}/images/generations`；
- 同时接受 `data[].url` 与 `data[].b64_json`；
- provider 请求携带 Bearer 与自定义 headers，下载临时 URL 时不转发任何 provider secret；
- response JSON、base64 和图片 bytes 均有上限；
- 只接受可解码 PNG/JPEG/GIF，MIME、宽高来自真实 bytes，不信任 URL 扩展名或 Content-Type；
- HTTPS 默认强制，HTTP 只可由测试显式启用；
- 错误不回显任意上游 body，避免上游错误页泄露凭据或内容。

POC 代码保留为生产 adapter 的基础，但正式接线前仍要补齐 SSRF/DNS rebinding、redirect、WebP、最大像素和原子落盘门禁。

> 实现收口（2026-08-08）：生产 adapter 已完成上述门禁，并把格式合同收紧为 JPEG/PNG/静态 WebP；GIF（包括单帧 GIF）统一拒绝。本文前面的 GIF 描述只记录最初 POC 的行为，不代表最终产品合同。

## 3. 自动化结果

`go test ./internal/imagegen` 覆盖：

- OpenAI-compatible 请求路径、模型、prompt 与 Authorization；
- base64 返回的解码、真实 MIME 与尺寸；
- URL 返回以及下载请求不携带 Authorization/custom secret header；
- 无图片、非法图片、超限图片；
- 非结构化 401 body 不进入错误文本；
- 未声明协议、HTTP production endpoint、空模型的 fail-closed 行为。

结果：通过。

## 4. 真实供应商验证

在用户明确允许一次可能计费的调用后，测试从现有 JCode 配置读取凭据；key 未进入命令参数、fixture、日志或仓库。

调用：

```text
provider profile: zhipuai-coding-plan
protocol: openai_images
model: cogview-3-flash
endpoint: /images/generations
count: 1
requested size: 1024x1024
```

结果：

```text
status: passed
actual MIME: image/jpeg
actual dimensions: 1024x1024
actual bytes: 51,237
```

这同时验证了一个重要边界：供应商返回内容的真实格式可能与调用者预期或文件扩展名不同，最终文件名必须在 sniff/decode 后生成。

## 5. POC 决策

1. 图片生成是独立 workload，不依附当前 Chat Model。
2. 自定义 OpenAI-like provider 只有显式配置 `openai_images` 协议、endpoint 和 image model 后才获得能力；不能按模型名或品牌猜测。
3. 全局 Image Model 是图片生成的唯一启用角色：未选择、adapter 不支持、凭据缺失或 runtime 无法精确解析时，`generate_image` 不进入工具目录；选择有效模型后 normal mode 自动注册，不再要求 provider image enable 或 session image enable。
4. BigModel 使用 `openai_images` adapter；Alibaba Token Plan 的 Wan 2.7 使用独立同步 `token_plan_multimodal` adapter；Kimi 当前不声明图片生成。
5. URL 必须立即下载到 JCode managed storage，历史会话不能依赖会过期的 signed URL。
6. 工具批准后才允许调用 provider；审批拒绝必须能证明 upstream dispatch 次数为 0。

## 6. POC 后的新增 P0 风险

- 当前 `/api/mcp` 列表会把 `MCPServer.Headers` 原值回传 renderer。BigModel Search MCP 需要 Bearer header，因此 MCP preset 上线前必须先完成 secret mask 与 keep-on-empty 合并。
- BigModel Coding Plan credential 与通用平台/image credential 不能默认互换。即使本机 live smoke 成功，产品仍按 provider profile + endpoint + credential scope 精确解析。
- 当前 workspace-only Artifact 不能持久化工具生成媒体；必须先完成 managed Artifact v2。
- 当前 timeline 会把相邻工具折叠成 Activity；生图工具需要从初始 `tool_call` 就声明 `surface=standalone`。

## 7. 下一 gate

进入实现前必须完成 UI/architecture design，并经多 Agent 对抗审阅确认以下不变量：

- 配置真值表与条件工具注册只有一个 source of truth；
- billable approval、progress、result 和 replay 的事件顺序可证明；
- managed file 不暴露绝对路径给 Web，不泄露 signed URL/base64；
- TUI、ACP、Web、Desktop 消费同一个 artifact record；
- MCP credential 从设置 API、日志和错误路径中均不可回显。

## 8. 实现结果补记

POC 进入生产实现后还完成了以下闭环：

- `openai_images` 的显式 endpoint/model 优先于同名内置模型；未配置 endpoint 或未选择 Image Model 时没有图片工具；
- BigModel Coding profile 的空 `base_url` 按该 profile 的官方默认地址解析，显式代理地址不会被误认成官方 profile；
- Settings 不做图片 capability test；adapter 由研发 contract tests 与 maintainer live smoke 验证，实际调用前仍精确核对当前配置指纹；
- 图片生成使用持久化的全局 `image_model` 角色并在 dispatch-time 重验 runtime/config fingerprint；Web Search 等 provider-bound 能力使用当前 Chat Model exact provider 的 policy，不再使用会话 override。额度检查与 `dispatch_attempted` 日志在同一锁内提交；
- Provider-bound Search 必须跟随当前 Agent/Chat Model 的 provider exact adapter，不能因为配置里另存了另一家 provider 就跨 provider 注入；图片生成是唯一可由独立模型角色跨 chat provider 路由的能力；
- provider POST 禁止 redirect，asset 下载逐跳校验、固定解析后的公网 IP，并拒绝 private、loopback、link-local 与 IANA special-use 地址；
- 生成结果只写 JCode managed Artifact；Web/Desktop 通过 opaque ID 读取，TUI/ACP 指向同一实际文件。
- UI 投影与内部 lifecycle 分离：queued 仍用于 reducer/replay/审计，但 Web/Desktop 只显示独立 ApprovalBanner，不显示图片卡；`approval_denied` / `cancelled_before_dispatch` 同样不显示媒体卡。批准并进入 generating/saving 后才显示扫描织网占位：8 条横轨、8 条纵轨和 6 个节点，竖/方/横宽度分别为 `12rem`/`16rem`/`18rem`；generating 周期为 `3.2s`，saving 的轨道和节点切换到独立 `4.6s` 收束。success 最大宽度 `18rem`、竖图最大高度 `22rem` 且使用 `contain`。`prefers-reduced-motion` 下 generating 使用局部静态网格、saving 使用完整归位网格；隐藏 live region 位于 `aria-busy` 媒体面的 sibling。无 progress 客户端从 approval UI 直接进入 terminal。

## 9. 最终回归证据

2026-08-08 当前工作树完成以下验证：

- `go test ./...` 全包通过；核心 provider/config/image/artifact/session/tool/runner/handler/TUI/command/Web 包的 `go test -race` 通过；
- `make lint` 通过（Go 0 issues，三个 TypeScript project typecheck 通过）；
- `jcode-ui` 32/32、Web 64/64 Vitest 通过；Web 回归包含跨 turn 复用 `tool_call_id`、迟到旧 result 与 WS 漏初始帧的 operation 归属；
- `make build-web` 通过，默认 TUI binary 与 `jcode_headless desktop` binary 均完成编译；
- Web/Desktop 浏览器验证确认：空 BaseURL 的 BigModel profile 显示图片与搜索能力；显式 custom `openai_images` endpoint 只增加图片模型候选，选择有效 Image Model 后图片工具自动注册；普通 custom provider 不出现图片设置；Provider Web Search 默认 policy 关闭，开启后只跟随当前 Chat Model exact provider；Composer 不显示会话工具开关；Settings 加载和选择模型都不触发图片供应商请求。

最后一次隔离浏览器复验在完成上述 Settings/Composer 检查后因浏览器控制连接中断，没有重做窄屏、console 与生成卡 fixture；这些路径由前一次浏览器验收和自动化测试覆盖，不能把该次不完整复验表述成全流程通过。隔离服务启动期间还观察到既有 memory consolidation 用 dummy credential 发出一次 chat 请求并收到 401；没有真实 secret、图片请求或成功计费，但它不属于本功能的零外呼验证，应单独追踪启动期后台模型调用。
