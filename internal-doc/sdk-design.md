# jcode 官方 SDK 最终设计蓝图

> 状态:设计初稿(定稿版)。本文综合四份候选方案(Go 进程内库优先 / app-server 协议优先 / 薄绑定 spawn 二进制 / 分层混合)及其对抗式评审,给出 jcode SDK 的最终推荐形态与可开工路线。
>
> 所有 jcode 侧签名/行号均经真实源码核对。关键锚点见文末《文件锚点》。

---

## 1. 一句话结论 + 核心决策

**一句话结论:** jcode SDK 采用**分层混合形态,但以"先库、库为契约实现之锚"为纲**——底层是一个可被外部 Go 程序 `import` 的**真进程内核心库**(`pkg/jcode`,Claude/Codex 因语言绑定做不到),其上叠一层 transport-agnostic 的**控制协议 daemon**(演进现有 ACP,而非另起炉灶),最上层是从**单一 Go schema 生成**的 TS/Python 薄绑定。三层共享**同一个 `Session` 装配器和同一份事件 schema**,重复由结构消除,而非靠纪律避免。

这是四份方案评审后的合流点:分层混合(方案四,加权 4.15)得分最高且事实基础最扎实;它吸收方案一的"进程内是免费优势 + headless 路径已存在于 web.Engine"这一最强洞察,吸收方案二/三的"协议是契约、写 SDK = 写第 4 个 AgentEventHandler"的可行性证明,同时规避各家评审点出的坑。但我们对方案四做三处**明确修正**(见决策 4、决策 5、以及红线章节的结构化输出纠错)。

### 核心架构决策

**决策 1:先库,后协议,协议演进 ACP —— 不新建 app-server 从零起。**
- **为什么这样:** 三个 surface(TUI/ACP/Web)运行态最终都调**同一个** `runner.Run(...)`;`web.Engine`(engine.go:37)已经是"绕过 TUI 直调 runner.Run 的 per-task headless 运行态",把它提纯成 `Session` 是**搬迁 + 提纯**,不是从零造。协议层随后只是"把 `Session` 的方法映射成 method 表 + 把 `Event` 序列化成帧",是机械转换。库先立住,协议才有可映射的稳定对象。
- **为什么不那样:** 方案二"协议优先/新建 app-server"被其评审判为"把非-daemon 的 Phase 0 债务和 daemon 愿景捆绑销售"——真正该先做的只有 Session 装配核(不需要 daemon),daemon 应由真实的远程/多语言需求触发。且现有 ACP 已是事实对外协议(coder/acp-go-sdk 线协议,Zed 已能连),从零新建 app-server 会丢弃这份既有资产并制造第二套协议债。

**决策 2:进程内 Go 直调是一等公民,多语言绑定是二等公民 —— 但绝不放弃多语言。**
- **为什么这样:** jcode 的内核是 Go,jcode 自己的下游(desktop / automation / web / team)全是 Go,已经在直接吃 `runner.Run`。进程内工具/审批/hook 是**真函数回调**(零跨进程、零序列化),这是 Claude(`structuredIO.ts` 把工具调用包成 control_request 的伪进程内 MCP)和 Codex(Dynamic Tool 反向 RPC)结构上做不到的差异化。
- **为什么不那样:** 但方案一评审的诚实拆穿成立——进程内优势**只惠及 Go 用户**;TS/Python 用户仍吃 spawn + 序列化的全部复杂度。因此不能像方案一那样把多语言"推到 Phase 5、当次要薄壳"。协议表面(尤其 steer / 结构化输出 / 多语言契约)是能力天花板所在,越晚定义越被内核循环锁死。故:进程内先落地兑现价值,但协议 schema 的**类型定义**与库同期设计(哪怕 daemon 实现延后)。

**决策 3:审批策略一份(纯函数 `decide`),送达方式因传输而异 —— 富化决策枚举。**
- **为什么这样:** `ApprovalState` 已经把策略层(纯函数 `decide`:白名单 / `isSafeCommand` / browser 分级 / workpath 边界)与交互层(`RequestApproval` 回调)分离(approval.go)。SDK 只需替换交互层的"送达":Go 是闭包直呼,daemon 是 `approval/request` 反向 RPC。策略层完全不动。
- **为什么不那样:** 现状决策类型只有 `{Approved bool, Mode}`(贫瘠)。要支持 `AcceptForSession` / `Decline`(拒绝但 turn 继续)/ `Cancel`(中断整个 turn)/ `UpdatedArgs`(改写入参),需吸收 Codex 富枚举 + Claude `updatedInput`。**注意红线:** 这不是"扩富枚举"这么轻——`UpdatedArgs` 与 `Cancel` 要穿透 `runner.Run` 消费审批结果的下游逻辑(见决策 5 与红线章节)。

**决策 4:SDK 表面用 jcode 自有类型,但 `Tool` 设计成 eino `tool.BaseTool` 的可零成本互转超集。**
- **为什么这样:** 不把 `[]adk.Message` / `schema.Message` 直接泄漏到公开表面,避免 SDK 用户绑死 eino 版本、内核升级破坏 SDK 兼容(方案一评审的真实长期债)。`Event` / `Result` / `Input` 是 jcode 自有的可序列化类型——这也是协议层能透传它们的前提。
- **为什么不那样:** 但对 Go 进程内的**工具作者**,强行包一层新接口是净开销(工具作者本就在 eino 生态)。折中:`jcode.Tool` 接口是 `tool.BaseTool` 的超集,`adaptTool()` 双向零成本转换;高级 Go 用户仍可传原生 `tool.BaseTool`。这是"翻译层 vs 透传"没有免费午餐的取舍,我们选翻译层护住协议边界,但给 Go 用户留一条透传快车道。**这是需 jack 拍板的开放问题之一(见风险章节)。**

**决策 5:结构化输出走"合成工具 + 校验重试",不发明机制、不误引 Claude。**
- **为什么这样:** `runner.Run` 今天只返回裸 `string`(runner.go:34),无结构化输出。落地:`OutputSchema` 非空时注入一个合成 `submit_structured_output` 工具(schema 即入参),捕获其入参进 `Result.Structured`;失败**按 Claude 真实做法做 re-prompt 重试**,超上限报专门的 `stop_reason = error_max_structured_output_retries`。不改 `runner.Run` 签名,靠 `env` 上挂一个 `outputStore`(与 `TodoStore`/`GoalStore` 同款)取回产物。
- **为什么不那样:** 方案四把"合成工具 + Stop-hook 强制"归因给 Claude,这是**事实误引**(Claude 实为 schema 校验 → 不匹配 re-prompt → 超限报错,claudeSdkDocs.md:425-426)。我们保留合成工具(比 `response_format` 可控、有独立失败信号),但**吸收 Claude 真实方案里最有价值的重试 + 专门失败 subtype**,不依赖 Stop-hook 强制(它可能与 runner 现有 continuation loop 的 Stop hook 打架)。

---

## 2. 分层架构图

```
┌────────────────────────────────────────────────────────────────────────────┐
│ L3 · 语言薄绑定 (TS / Python) —— 二等公民,但与 schema 同期设计                │
│    sdk-ts/ , sdk-py/                                                          │
│    spawn `jcode serve --stdio` → NDJSON 控制协议客户端                        │
│    query()/Session 形状对齐 Claude query() + Codex Thread                     │
│    类型 100% 由 L2 schema 生成 (go:generate),零手抄 → 无漂移                  │
└──────────────────────────┬───────────────────────────────────────────────────┘
       stdin/stdout NDJSON  │ (仅跨语言/远程时存在;Go 用户完全不经过这条线)
       控制帧 + request_id   │
┌──────────────────────────▼───────────────────────────────────────────────────┐
│ L2 · 控制协议 Daemon 壳 —— 演进 ACP,不新建                                    │
│    pkg/jcode/rpc/                                                             │
│    transport 抽象: Stdio | WebSocket | Unix (借 Codex 4-transport)            │
│    一份 method 表 (session/* turn/* item/* approval/*) —— 由 L1 类型生成       │
│    单 writer goroutine + request_id RPC + 反向审批 RPC + broadcast            │
│    ACP = 这一层的一个 codec 兼容层 (Zed 等既有客户端继续连)                    │
│    `jcode serve` / `jcode acp` / `jcode exec` 都是它的 CLI 皮                 │
└──────────────────────────┬───────────────────────────────────────────────────┘
       in-process Go 调用    │ (无序列化,无子进程 —— jcode 独有免费优势)
┌──────────────────────────▼───────────────────────────────────────────────────┐
│ L1 · 进程内核心库 (唯一 agent 逻辑所在) —— 一等公民                            │
│    pkg/jcode/                                                                 │
│    type Client   ← 工厂 + 进程级配置                                          │
│    type Session  ← 唯一装配器 (收敛现三份 command 装配逻辑)                   │
│    Session.Prompt(ctx,in) → Stream (事件流+控制句柄) / Run → Result           │
│    内置 streamHandler = 第 4 个 AgentEventHandler(8 回调 → chan Event)        │
│    复用: runner.Run · ApprovalState · hooks · mode · Recorder · LoadMCPTools  │
│    新增: ToolRegistry · OutputSchema · 统一 Spawn(subagent)                   │
└──────────────────────────┬───────────────────────────────────────────────────┘
       复用现有接缝,内核语义几乎不改                                            │
┌──────────────────────────▼───────────────────────────────────────────────────┐
│ L0 · jcode 现有内核 (改造点少,作为库的实现细节)                              │
│    runner.Run · agent.NewAgent · ApprovalState · hooks.Dispatcher            │
│    mode.SessionMode · session.Recorder · tools.Env/LoadMCPTools · eino/adk    │
└──────────────────────────────────────────────────────────────────────────────┘
        ▲ Go 应用直接 import pkg/jcode(L1),不碰 L2/L3
```

### 层职责边界与依赖方向

- **依赖方向严格单向向下:** L3 → L2 → L1 → L0。L1 不知道 L2/L3 存在;L0 不知道 L1 存在(靠 `pkg/jcode` 作为 `internal/` 的唯一批准出口,facade 模式,防止泄漏整个 `internal/tools`)。
- **L1 边界:** 只暴露 jcode 自有可序列化类型(`Event`/`Result`/`Input`/`Tool`/`ApprovalRequest`);内部持有 eino 类型,边界处像 ACP handler 那样映射。
- **L2 边界:** 纯编解码 + 传输 + RPC 关联,**零 agent 逻辑**。它拿到一个 `*jcode.Session` 就够了。ACP 是它的一个 codec,不是平行实现。
- **L3 边界:** 只做三件事——flag 翻译、进程/心跳管理、回调回跳(把反向帧派发给宿主闭包)。零 agent 逻辑。
- **进程内旁路:** Go 用户 `import pkg/jcode` 直接拿到 L1,`Event` 是内存 channel、`Approver`/`Tool`/`Hook` 是直接函数调用,**整条链没有一条进程边界**。这是与 Claude/Codex 架构图的本质区别。

---

## 3. 对外 API 契约

### 3.1 L1 · Go 进程内库(真实签名)

包路径:`github.com/cnjack/jcode/pkg/jcode`(与既有 `pkg/weixin` 并列;`internal/` 内核不变)。

```go
package jcode

// ── Client:进程级工厂 + 全局配置。一个进程通常一个 Client,多个 Session 共享 ──
type Client struct { /* config, providers, tracer, default tool registry, sessions root */ }

func NewClient(opts ...Option) (*Client, error)

type Option func(*clientConfig)
func WithConfig(cfg *config.Config) Option            // 复用现有 config
func WithProviderModel(provider, model string) Option
func WithTracer(t *telemetry.LangfuseTracer) Option
func WithSessionsRoot(dir string) Option              // JSONL transcript 根,默认 ~/.jcode
func WithToolRegistry(r *ToolRegistry) Option

// Capabilities 暴露运行时能力发现(对齐 Claude initializationResult):
// 支持的模型、内置工具名、mode 列表 —— 供 L2/L3 与 UI 用。
func (c *Client) Capabilities() Capabilities

// ── Session:唯一装配器。收敛今天散落 interactive/acp/web 三处的
//    model→tools→middlewares→approval→hooks→recorder 装配逻辑。 ──
type Session struct { /* agent, history, env, recorder, approvalState, tokenUsage,
                         hookDispatcher, mode, registry, mu ... 见 §5 复用表 */ }

type SessionOptions struct {
    Cwd          string
    Mode         mode.SessionMode              // 复用 leaf,零改造
    Provider     string                        // "" → config 默认
    Model        string
    SystemPrompt string                         // "" → 默认 coding prompt
    MCPServers   map[string]*config.MCPServer   // 真正注入(修 ACP 忽略 McpServers 的 bug)
    Tools        []Tool                         // 追加自定义进程内工具
    OutputSchema json.RawMessage                // 非空 → 结构化输出;nil → 纯文本
    ResumeID     string                         // 非空 → 从该 UUID 续跑
    Hooks        []HookRegistration             // 进程内 Go hook
    OnApproval   ApprovalFunc                   // 进程内审批回调;nil → 默认保守策略
    MaxTurns     int                            // 0 → 用内核默认 continuation 上限
}

func (c *Client) NewSession(ctx context.Context, o SessionOptions) (*Session, error)
func (c *Client) ResumeSession(ctx context.Context, id string, o SessionOptions) (*Session, error)
func (c *Client) ListSessions(project string) ([]SessionInfo, error)   // 复用 session.ListSessions

// ── 一个 turn = 一次 Prompt(对齐 Claude submitMessage / Codex Thread.run)──
type Input struct {
    Text   string
    Images []session.EntryImage
}

// Prompt:headless、无 TUI、进程内。返回 Stream(事件源 + 控制句柄二合一,
// 正是 Claude query() 的双重身份)。channel 关闭 = turn 结束。
func (s *Session) Prompt(ctx context.Context, in Input) (*Stream, error)

// Run:便捷一次性 = collect(Prompt) 直到 EventResult(对齐 Codex run=collect(runStreamed))。
func (s *Session) Run(ctx context.Context, in Input) (*Result, error)

// ── 运行态控制(委托已有线程安全 setter)──
func (s *Session) SetMode(m mode.SessionMode)          // 复用 ApprovalState.SetSessionMode + agent 重建
func (s *Session) SetModel(provider, model string) error
func (s *Session) RegisterTool(t Tool) error           // 运行时加进程内工具
func (s *Session) AddMCP(name string, cfg *config.MCPServer) error // 复用 LoadMCPTools
func (s *Session) Fork(ctx context.Context) (*Session, error)      // 新增,见 §5
func (s *Session) Spawn(ctx context.Context, o SpawnOptions) (*Session, error) // 统一多-agent
func (s *Session) ID() string                          // == recorder UUID
func (s *Session) Close() error                        // 复用 recorder.Close / env.CloseRemote
```

**Stream —— 事件源 + 控制句柄:**

```go
type Stream struct { /* ch chan Event, cancel, steerFn, result */ }

func (st *Stream) Events() <-chan Event        // range 到 close 即 turn 结束
func (st *Stream) Interrupt()                  // → ctx cancel(区分 interrupt vs error,见红线)
func (st *Stream) Steer(in Input) error        // turn 进行中追加输入(见风险:内核暂无钩子)
func (st *Stream) Wait() (*Result, error)      // 阻塞收尾,drain 到 EventResult
```

**Event —— 单一 tagged union(把 8 个 AgentEventHandler 回调升维成一个可序列化类型):**

```go
type EventKind string
const (
    EventTurnStart   EventKind = "turn_start"      // ← OnAgentStart(补 ACP 缺的 agentStart)
    EventAgentText   EventKind = "agent_text"      // ← OnAgentText
    EventToolCall    EventKind = "tool_call"       // ← OnToolCall
    EventToolResult  EventKind = "tool_result"     // ← OnToolResult
    EventTodoUpdate  EventKind = "todo_update"      // ← OnTodoUpdate
    EventTokenUpdate EventKind = "token_update"     // ← OnTokenUpdate(补 ACP 缺的 token)
    EventApproval    EventKind = "approval_request" // ← RequestApproval(仅 daemon 路径序列化)
    EventSubagent    EventKind = "subagent"         // 统一多-agent 事件
    EventResult      EventKind = "result"           // ← OnAgentDone,turn 终结账本(权威结束信号)
)

type Event struct {
    Kind     EventKind           `json:"kind"`
    Text     string              `json:"text,omitempty"`
    Tool     *ToolEvent          `json:"tool,omitempty"`
    Tokens   *handler.TokenUsage `json:"tokens,omitempty"`
    Approval *ApprovalRequest    `json:"approval,omitempty"`
    Subagent *SubagentEvent      `json:"subagent,omitempty"`
    Result   *Result             `json:"result,omitempty"`
}

// Result = turn 终结账本(对齐 Claude result 帧 / Codex TurnResult)。
// L2/L3 只信 EventResult 判定 turn 结束(对齐 Claude session_state_changed:idle)。
type Result struct {
    Text       string             `json:"text"`                 // 累积 assistant 文本(runner.Run 返回值)
    Structured json.RawMessage    `json:"structured,omitempty"` // OutputSchema 命中时
    Usage      handler.TokenUsage `json:"usage"`
    NumTurns   int                `json:"num_turns"`            // continuation loop 圈数
    StopReason string             `json:"stop_reason"`          // completed|interrupted|max_continuations|
                                                                //  error_max_structured_output_retries|model_error
    Err        string             `json:"error,omitempty"`
}
```

**Tool —— eino `tool.BaseTool` 的可零成本互转超集(决策 4):**

```go
type Tool interface {
    Name() string
    Description() string
    InputSchema() json.RawMessage
    Invoke(ctx context.Context, argsJSON string) (string, error)
}

// ToolFunc:人体工学构造器(泛型 + JSON Schema 从 Go struct 反射)。
// 与 Claude tool()/Codex Dynamic Tool 对位,但它是【真进程内函数】——
// 无 mcp_message 帧、无反向 RPC、无 60s 超时告警。
func ToolFunc[In any, Out any](
    name, description string,
    fn func(ctx context.Context, in In) (Out, error),
) Tool

// 例:工具闭包直接捕获 *sql.DB —— Claude/Codex 结构上做不到。
db := openDB()
sess, _ := client.NewSession(ctx, jcode.SessionOptions{
    Tools: []jcode.Tool{
        jcode.ToolFunc("query_users", "Query users by role",
            func(ctx context.Context, in struct{ Role string `json:"role"` }) ([]User, error) {
                return queryUsers(ctx, db, in.Role) // 同进程,直接调
            }),
    },
})
```

**审批回调 —— 富决策枚举(决策 3):**

```go
type ApprovalFunc func(ctx context.Context, req ApprovalRequest) (ApprovalResponse, error)

type ApprovalRequest = handler.ApprovalRequest   // 直接复用现有类型

type ApprovalResponse struct {
    Decision    ApprovalDecision
    UpdatedArgs json.RawMessage `json:"updated_args,omitempty"` // 改写入参(Claude updatedInput)
    Scope       ApprovalScope   `json:"scope,omitempty"`        // Once | Session
}
type ApprovalDecision string
const (
    Accept           ApprovalDecision = "accept"
    AcceptForSession ApprovalDecision = "accept_for_session" // 记住,后续不问(Codex)
    Decline          ApprovalDecision = "decline"            // 拒绝但 turn 继续(喂回模型)
    Cancel           ApprovalDecision = "cancel"             // 拒绝且中断整个 turn(Codex Cancel)
)

// 默认保守:OnApproval == nil 时走策略层 decide;decide 对危险命令返回 prompt,
// 无回调则 DENY —— 绝不像 Codex Python 默认那样自动 accept(红线)。
```

> **实现注记(红线相关):** 现状 `ApprovalState.RequestApproval(ctx, toolName, toolArgs string) (bool, error)` 返回纯 bool。而 `AgentEventHandler.RequestApproval(ctx, req) (ApprovalResponse, error)` 是接口层。富决策要落到:(a) `handler.ApprovalResponse` 加 `UpdatedArgs`/`Interrupt` 字段(向后兼容,零值即旧行为);(b) approval 中间件在放行前用 `UpdatedArgs` 替换 tool args——**注意:改写 args 的现有代码在 hook 中间件的 PreToolUse 路径(明确 "OUTSIDE approval"),不是 approval 中间件里,所以这是新接线,不是纯复用**;(c) `Cancel` → 在审批返回后 `cancel()` 当前 run ctx。

**Hook —— 进程内 Go 回调(Claude/Codex 都做不到,它们的回调 hook 是反向 RPC):**

```go
type Hook func(ctx context.Context, p hooks.Payload) hooks.Decision
type HookRegistration struct { Event hooks.Event; Fn Hook }
// event ∈ {SessionStart, UserPromptSubmit, PreToolUse, PostToolUse, PostToolUseFailure, Stop}

// 内部:新增一个 callbackDispatcher 实现 hooks.Dispatcher,先跑外部命令 hook 再折叠
// 进程内 Go 回调,仍走 ctx = hooks.WithDispatcher(ctx, disp)。middleware / continuation
// loop 从 ctx 取,零改动。
```

### 3.2 L2 · 控制协议 daemon(JSON-RPC 方法表)

命名空间化 `<domain>/<action>`(借 Codex 范式,利于路由/权限/实验门控)。**NDJSON 换行分帧,不发 `"jsonrpc":"2.0"`**(与 Codex/Claude 一致)。**这张表由 L1 的 Go 类型生成,不手写。**

**客户端 → 服务端:**

| method | params | result | 映射 L1 |
|---|---|---|---|
| `initialize` | clientInfo, capabilities | serverInfo, models, tools, modes, commands | `Capabilities`(能力发现,抄 Claude initializationResult) |
| `session/new` | SessionOptions(JSON) | sessionId, modes | `Client.NewSession`(**修:真正注入 mcpServers**) |
| `session/resume` | sessionId, opts | sessionId, history? | `Client.ResumeSession` |
| `session/fork` | sessionId, upToEntry? | newSessionId | `Session.Fork` |
| `session/list` | project? | sessions[] | `Client.ListSessions` |
| `session/setMode` | sessionId, mode | ok | `Session.SetMode` |
| `session/setModel` | sessionId, provider, model | ok | `Session.SetModel` |
| `session/close` | sessionId | ok | `Session.Close` |
| `turn/prompt` | sessionId, Input, outputSchema? | turnId(流经通知) | `Session.Prompt` |
| `turn/steer` | sessionId, turnId, Input, expectedTurnId | ok | `Stream.Steer`(乐观并发,防 turn 覆盖粘性) |
| `turn/interrupt` | sessionId, turnId | ok | `Stream.Interrupt` |
| `tool/register` | sessionId, name, description, inputSchema | ok | 声明跨进程工具(执行走反向 `tool/execute`) |
| `mcp/add` | sessionId, servers | statuses[] | `Session.AddMCP` |
| `mcp/status` | sessionId | statuses[] | `MCPStatus` |
| `hook/register` | sessionId, event, matcher | ok | 进程内 hook 的跨语言版 |

**服务端 → 客户端(反向 request,交互特性的物理载体):**

| method | params | client 回 | 映射 |
|---|---|---|---|
| `approval/request` | sessionId, turnId, toolName, toolArgs, isExternal, worker? | decision, updatedArgs?, scope | `OnApproval` |
| `askUser/request` | sessionId, prompt, options[] | answer | web ask_user 提升为协议一等公民 |
| `hook/callback` | sessionId, event, payload | decision, updatedInput?, additionalContext? | 跨进程 hook |
| `tool/execute` | sessionId, name, argsJSON | output, isError | 跨进程语言注册的工具反向执行(Go 进程内跳过) |

**服务端通知(事件流,无 id):** `turn/started` · `item/agentText/delta` · `item/toolCall/started` · `item/toolCall/completed` · `todo/updated` · `turn/tokenUsage/updated` · `subagent/event` · `turn/completed`(携带 `{usage, finalResponse, items[], structuredOutput?}`)。每条 = 一个 `Event` 的 JSON。

**关键消息帧 schema(NDJSON):**

```jsonc
// request
{"id": 7, "method": "turn/prompt", "params": {"sessionId": "u-1", "text": "fix the bug", "outputSchema": {...}}}
// response
{"id": 7, "result": {"turnId": "t-3"}}
// notification(事件流)
{"method": "item/agentText/delta", "params": {"sessionId":"u-1","turnId":"t-3","text":"hel"}}
// reverse request(审批)
{"id": 101, "method": "approval/request", "params": {"toolName":"execute","toolArgs":"rm -rf ...","isExternal":true}}
// client → server 回审批
{"id": 101, "result": {"decision":"cancel"}}
// turn 终结帧(权威结束信号)
{"method": "turn/completed", "params": {"turnId":"t-3","usage":{...},"finalResponse":"...","structuredOutput":{"risk":"high"}}}
```

**结构化输出协议字段:** `turn/prompt` 带 `outputSchema`(JSON Schema);`turn/completed` 带 `structuredOutput`(捕获的合成工具入参);失败时 `turn/completed` 带 `stopReason: "error_max_structured_output_retries"`。

**关于 ACP 兼容(明确):**
- **演进 ACP,不新建 app-server。** ACP(`jcode acp`,coder/acp-go-sdk)成为 L2 的一个 **codec 兼容层**——既有 Zed 等编辑器客户端继续用 ACP 线协议连接,行为不变。
- 新增 `jcode serve --stdio|--ws|--unix` 讲**更完整的 jcode 控制协议**(补齐 token / agentStart / 结构化输出 / 富审批,这些是 ACP 偏"编辑器客户端"而缺的)。
- 两者共享 L2 的 Router + 单 writer + `SDKHandler`,只是**出站 codec 不同**。ACP 缺的三个事件(token/agentStart/result)通过 L1 的统一 `Event` 流自然补齐;ACP 忽略 `params.McpServers` 的 bug 在改调 `Session`(`WithMCPServers`)后自动兑现。
- **红线:** 不把还在快速演进的 jcode 招牌特性(goal/team/圆桌/automation/browser 三档)一次性冻进跨语言契约。先冻结一个小而稳的核心面(session/turn/approval/stream/mode/model),其余留 `experimental/*` 命名空间。

### 3.3 L3 · 多语言绑定(用法草图 + 类型生成)

**类型如何从单一 schema 生成:** 帧/事件/方法类型集中在**一份 Go 源**(`pkg/jcode/rpc/schema.go` 的 struct + jsonschema tag),`go:generate` 导出 JSON Schema + TS 类型 + Python pydantic。与 memory 里 theme 系统"one Go palette generates web CSS+TS"同构、jcode 已验证过的生成范式。这规避 Codex "真源在 Rust、目标语言改不动 + 手抄漂移(Usage i64 vs number)"的双重坑。

**TS(形状对齐 Claude query()):**

```ts
import { query, tool, createSession } from "@jcode/sdk";

// one-shot(内部 collect)
const res = await query({ prompt: "fix the bug", options: { mode: "plan" } });
console.log(res.text, res.usage);

// streaming + 控制句柄 + 审批(一等参数,不像 Codex 藏在 _client)+ 自定义工具 + 结构化输出
const session = createSession({
  mode: "approval",
  mcpServers: { fs: { type: "stdio", command: "npx", args: ["@x/fs-mcp"] } },
  tools: [ tool("lookup", "查库", { id: z.string() }, async ({id}) => ({content: db.get(id)})) ],
  onApproval: async (req) =>
    (req.toolName === "execute" && /rm -rf/.test(req.toolArgs))
      ? { decision: "cancel" } : { decision: "accept" },
  outputSchema: { type: "object", properties: { risk: { type: "string" } } },
});
for await (const ev of session.prompt("audit this repo")) {   // AsyncGenerator + 控制句柄
  if (ev.kind === "agent_text") process.stdout.write(ev.text);
  if (ev.kind === "result") console.log(ev.result.structured); // {risk:"..."}
}
// session.interrupt() / session.setMode("plan") / session.steer(...) 也可用
```

**Python(形状对齐 Codex Thread,审批一等公民):**

```python
from jcode import Client
c = Client(provider="anthropic", model="claude-...")
sess = c.new_session(
    mode="approval",
    on_approval=lambda req: {"decision": "cancel"} if "rm" in req.tool_args else {"decision": "accept"},
    output_schema={"type": "object", "properties": {"risk": {"type": "string"}}},
)
for ev in sess.prompt("refactor auth"):
    if ev.kind == "agent_text": print(ev.text, end="")
    if ev.kind == "result": print(ev.result.usage, ev.result.structured)
```

**绑定层全部职责(三件事):** flag 翻译(`Options` → `jcode serve` 命令行 + `initialize` 载荷);进程/心跳管理(spawn、keep_alive 心跳、崩溃重启、stderr 环形缓冲 ≥400 行、`JCODE_BIN` 离线逃生口);回调回跳(把 `approval/request`/`hook/callback`/`tool/execute` 反向帧派发给宿主闭包,应答回写)。**零 agent 逻辑。**

---

## 4. 复用现有接缝的具体映射表

| 接缝 | 位置 | 角色 | 具体动作 |
|---|---|---|---|
| `runner.Run(...)` | runner/runner.go:25 | **直接用** | `Session.Prompt` 的 goroutine 体;9 参数从 Session 字段取。已 transport-agnostic、continuation loop 已封。仅结构化输出经 `env` 上挂 outputStore(不改签名) |
| `handler.AgentEventHandler`(8 回调) | handler/handler.go:19 | **保留 + 加第 4 个实现** | 新写 `streamHandler`,8 回调 → `Event` chan。TUI/ACP/Web 三实现不动 |
| `agent.NewAgent(...)` | agent/agent.go:25 | **直接用** | `Session` 装配时调它;middleware 栈(approval/hooks/memory)零改 |
| `ApprovalState` + `decide` | runner/approval.go | **直接用 + 富化决策** | 纯函数 `decide` 不动;`RequestApproval(bool)` 交互层升级到富枚举 + UpdatedArgs/Cancel(新接线,见 §3.1 注记) |
| `hooks.Dispatcher` + ctx 注入 | hooks/config.go:70, context.go:16 | **直接用 + 加载体** | 加 `callbackDispatcher`(进程内 Go 回调),仍走 `WithDispatcher`,接口不改 |
| `mode.SessionMode` | mode/mode.go:17 | **直接用** | `SessionOptions.Mode` / `SetMode` 原样透传;协议 `mode` 字段 = 其 `String()` |
| `session.Recorder/Entry/JSONL` | session/session.go:165/763/849 | **直接用 + 补 Fork** | resume/list 现成;**新增 `Recorder.Fork(newUUID)`**(复制 JSONL + entry 链重映射,仿 Claude forkSession) |
| `tools.LoadMCPTools` | tools/mcp.go:24 | **直接用 + 修 bug** | `AddMCP`/`SessionOptions.MCPServers` 直接调;**修 ACP 忽略 params.McpServers**(acp.go:269 只 log,359 用 cfg) |
| `tools.Env` + `NewEnv` | tools/env.go:59 | **直接用** | Session 持一个 `*tools.Env`(todo/goal/bg/outputStore 挂它) |
| `web.Engine` | web/engine.go:37 | **提炼来源(headless 原型)** | `Session` = 提纯 Engine:去掉 web 专有字段(runGen/broadcast/pumpCancel/pwd-immutable);Engine 退化为 Session 的薄持有者 |
| **三份装配逻辑** | interactive.go / acp.go:313 / web/engine.go | **改造:收敛为 `Client.NewSession`** | 删 2 份,统一为装配器;三 surface 改调它 |
| `buildAllTools()` ×3 | interactive.go 等 | **替换:ToolRegistry** | 硬编码列表 → `registry.Resolve(mode)`(Plan=只读子集) |
| `-p` TUI-gated | interactive.go(尾部 p.Run()) | **改造:`jcode exec -p` 走 Session** | `PromptSync/Run` 直出,不起 BubbleTea |
| 多-agent 三套 | tools/subagent.go / team.Manager(any 绕 cycle) / web Engine | **收敛为 `Session.Spawn`** | 子 agent = 子 Session(NewTeammateRecorder);team 的 `any` 因 Session 在 pkg/ 依赖反转自然消解 |

---

## 5. 必须先偿还的技术债(及顺序)

这些债的偿还与 SDK 交付是**同一次改动**;顺序按"解锁性 + blast radius 最小"排:

1. **【最优先】抽 `Client.NewSession` 装配器,消灭三份重复。** model→tools→middlewares→approval→hooks→recorder 现在在 interactive.go / acp.go:313 / web/engine.go 各写一遍(且已微妙分叉:plan-mode 工具子集、env.OnEnvChange 回调、teammate recorder、memory 工具条件注入)。SDK 第一件事就是把它收敛;否则 SDK 是第四次复制。**风险:三份"看似相同实则各有特例"的收敛最易埋回归,而本 sandbox 无法起 live server 验证"行为不变"——缓解见路线图 Phase 0 的验收策略。**
2. **加真正的 headless 执行路径。** `-p` 也起 BubbleTea 是硬伤;但 `web.Engine` 证明"不经 TUI 直调 runner.Run"的路径已存在(engine.go 已聚合所需全部字段)。`Session.Prompt` = 提纯它 + 内置 `streamHandler`。
3. **tool registry 取代硬编码 `buildAllTools`。** 内置 + 用户 + MCP 工具统一注册,按 mode 过滤。顺带打破 `internal/tools` 的重量(team 已被迫用 `any` 绕 import cycle)。
4. **修 ACP 忽略 MCP。** `session/new` 的 `mcpServers` 真正注入(acp.go:269 只 log)。改调 Session 后自动兑现。
5. **结构化输出。** `runner.Run` 只返回 `string`;`OutputSchema` → 合成工具 + 校验重试(决策 5),经 env outputStore 取回。
6. **富审批决策。** `handler.ApprovalResponse` 加 `UpdatedArgs`/`Interrupt`;approval 中间件消费(新接线)。
7. **Session.Fork。** `Recorder.Fork(newUUID)`。仿 Claude,警示"只 fork 对话历史,不 fork 文件系统改动"。

---

## 6. 分阶段路线图

每个 Phase **独立可 demo、可合并**;后一阶段不回改前一阶段公开面。任意 Phase 停下都是自洽产品切面。

### Phase 0 — 抽出 headless `Session`(纯还债,零新特性)
- **交付可 demo:** Go 程序 `import pkg/jcode` 三行跑通一个 coding turn,流式 range `Event`,拿到文本 + token 账本;`jcode exec -p "..."` 走它、**不再起 TUI、秒出无闪烁**。
- **改动包:** 新建 `pkg/jcode/{client,session}.go`;从 `web.Engine` 提炼装配到 `NewSession`;`command/interactive.go`/`acp.go`/`web` 改调它(删 2 份重复);内置 `streamHandler`。**内核 L0 不动。**
- **验收标准:** Go test `NewSession→Prompt→收到 EventResult`;三 surface 行为回归对拍(用 e2e agent-eval harness 的 ACP 驱动 + 决定论 oracle,见 memory `jcode agent-eval harness`);`jcode exec -p` 无 BubbleTea 生命周期痕迹。**因 sandbox 不能 bind socket(memory `jcode e2e sandbox limits`),"行为不变"验收用 in-process `Client.Run` + httptest 风格,而非 spawn——这一步反而让 e2e 更好写。**
- **既有特性衔接:** desktop / automation / web task 三条 Go 线**立刻**可改用 `Session`(它们已在吃 `runner.Run`);web Engine 退化为 Session 薄持有者。

### Phase 1 — 流式 + 富审批 + Fork(仍纯 Go)
- **交付可 demo:** `Session.Prompt → Stream`(流式打字机 + 中途 `Interrupt` + 危险命令弹 `OnApproval` 回调 + `Cancel` 中断整个 turn);`Session.Fork` 后两会话独立;`EventResult` 终结帧。
- **改动包:** `Stream`/`Event` 类型;`handler.ApprovalResponse` 加字段 + approval 中间件消费;`Recorder.Fork`;continuation loop 产出 `EventResult`。
- **验收标准:** interrupt 与 error 可区分(StopReason);`UpdatedArgs` 改写后工具收到新入参;fork 后写不互串。
- **既有特性衔接:** **hooks**(memory `jcode hooks design`)—— 进程内 `callbackDispatcher` 与外部命令 hook 并存;**goal**(memory `jcode goal feature`)—— goalStore 随 Session,continuation loop 已含 goal→Stop hook。

### Phase 2 — schema 单一事实源 + 控制 daemon(L2)+ ACP 演进
- **交付可 demo:** `jcode serve --stdio|--unix`;method 表 + 反向审批 RPC + 单 writer;`go:generate` 从 L1 类型出 JSON Schema/TS/Python 类型;**ACP 客户端(Zed)仍能连**(codec 兼容层);ACP 补发 token/agentStart、兑现 MCP 注入。
- **改动包:** 新建 `pkg/jcode/rpc/{transport,codec,server,schema}.go`;`command/serve.go`;`command/acp.go` 改调 Session。
- **验收标准:** 外部进程连 `--unix` 跑带审批的 turn(sandbox 用 Unix socket 而非 TCP,规避 bind 限制);并发 emit 下 NDJSON 帧完整性测试(单 writer 正确性);Zed 兼容回归。
- **既有特性衔接:** **desktop**(memory `jcode desktop app`)—— Tauri sidecar 复用 `jcode serve --ws`/`--unix` 同协议;**web/inline workspace + SSH**(memory `jcode web inline workspace + ssh`)—— 远程连 daemon;**browser-use**(memory `jcode browser use`)—— browser 三档审批经 `approval/request` 反向 RPC(isExternal/worker 字段)。

### Phase 3 — TS/Python 薄绑定(L3)
- **交付可 demo:** `@jcode/sdk`(npm)+ `jcode`(PyPI);spawn `jcode serve --stdio`;类型全生成;`query()`/`Client` 用法(§3.3);`JCODE_BIN` 离线逃生口。
- **改动包:** 新建 `sdk-ts/`、`sdk-py/`;生成脚本进 CI。
- **验收标准:** `npx`/`pip` 装完三行跑通;审批一等公民(不藏私有字段);类型与 Go schema 位对齐;能力矩阵对齐清单(先定命名规范防漂移)。
- **既有特性衔接:** **memory**(memory `jcode agent memory`)—— 跨会话记忆经 resume 透明工作;**MCP OAuth**(memory `jcode mcp oauth`)—— `mcp/add` + `mcp/oauthCompleted` 通知。

### Phase 4 — 结构化输出 + ToolRegistry + 统一 Spawn + 多-agent 收敛
- **交付可 demo:** `OutputSchema`(合成工具 + 校验重试)返回符合 schema 的 JSON;`ToolRegistry` 取代 `buildAllTools`;`Session.Spawn` 收敛 subagent/team/web 三套;team 去 `any`。
- **改动包:** 合成工具 + 校验重试(runner 内,非 Stop-hook);registry;team 依赖方向反转。
- **验收标准:** 结构化输出失败报 `error_max_structured_output_retries`;team 编译期无 `any`;subagent 复用主循环(不写第二个 loop)。
- **既有特性衔接:** **team/圆桌**(memory `jcode dynamic workflow roundtable`)—— `Session.Spawn` 统一入口,internal/flow 编排器复用 Session 工厂;**automations**(memory `jcode automations`)—— 无交互定时任务直接走 `pkg/jcode` 进程内(不必起 daemon),run = 打标签的普通 Session。

### Phase 5 —(可选,需求触发)可插拔 SessionStore + 远程 turn 控制
- 可插拔 `SessionStore`(云端/多主机 resume + conformance 测试套件,与 memory "文件+git+flock 无 SQLite" 同构);`turn/steer` 真正落地(需内核循环加钩子,见风险)。**由真实需求触发,可永不做。**

---

## 7. 明确的取舍与红线

### 借鉴 Claude/Codex 的具体设计(逐条)

- **【Claude】** `query()` 既是事件流又是控制句柄 → `Stream` 二合一。
- **【Claude】** 单一联合 `SDKMessage` 事件流 → 单一 `Event` tagged union。
- **【Claude】** `result` 帧作为权威 turn 终结信号 → `EventResult`。
- **【Claude】** 结构化输出 = schema 校验 + re-prompt 重试 + 专门失败 subtype → 决策 5(**吸收重试与失败信号,不抄不存在的 Stop-hook 强制**)。
- **【Claude】** 运行时能力发现(initializationResult:models/tools/modes)→ `Capabilities` / `initialize`。
- **【Claude】** 审批 `updatedInput` 改写入参 → `UpdatedArgs`。
- **【Claude】** 单 writer goroutine 串行化,防 control_request 撕裂流式文本(structuredIO.ts:161)→ L2 单 writer。
- **【Claude】** subagent 复用主循环(不写第二个 loop,只换 agentId + 独立 sidechain)→ `Session.Spawn`。
- **【Claude】** 可插拔 SessionStore + conformance 测试 → Phase 5。
- **【Codex】** 常驻双向 daemon + 反向 RPC 审批 → L2(交互特性物理前提)。
- **【Codex】** 命名空间化 method `<domain>/<action>` → 方法表。
- **【Codex】** 4-transport 抽象(stdio/ws/unix/off)→ L2 transport。
- **【Codex】** 富审批枚举(Accept/AcceptForSession/Decline/Cancel + scope Turn|Session)→ `ApprovalDecision`。
- **【Codex】** pending 事件缓存 + 回放(turn/started 早于 turn/start result 的竞态修复,_message_router.py:96)→ L2 Router。
- **【Codex】** `run = collect(runStreamed)`(避免两套逻辑漂移)→ `Run` = collect(Prompt)。
- **【Codex】** Rust 宏 → 生成多语言类型 → `go:generate` 从 Go schema 生成(同构,避坑见下)。
- **【Codex】** `turn/steer` 乐观并发(expectedTurnId)→ 协议保留,内核钩子待补。

### 绝不照抄的坑(逐条红线)

- **【Codex】高层 API 吞掉 approval_handler:** Codex Python 把审批 handler 藏在私有 `_client`,默认自动 accept 是安全隐患。→ **红线:审批回调是公开一等参数;`OnApproval == nil` 时默认 DENY 危险命令,绝不自动放行。**
- **【Codex】turn 覆盖粘性:** `turn/start` 带的 `model?`/`outputSchema?` 覆盖"for this turn and subsequent turns"(turn.rs)。→ **红线:协议必须明确 turn 级覆盖是"仅此 turn",Session 级设置走 `session/setMode`/`setModel`;写测试锁定。**
- **【Claude/Codex】in-process MCP 伪进程内:** 它们的"in-process MCP"其实是 stdio-over-control-frame 跨进程 RPC。→ **红线:jcode 的 Go 工具是真进程内函数,不发明第二套跨进程工具协议;跨语言工具让用户写标准 MCP server + `AddMCP`,`tool/execute` 反向回跳仅作逃生口、默认关。**
- **【Codex TS】单向管道审批静默退化:** `child.stdin.end()` 后审批静默变 `Never`(exec/src/lib.rs)。→ **红线:jcode 无单向模式;审批无回调时是显式 DENY,不静默绕过。**
- **【Codex】手写类型漂移:** TS 手抄 Rust 类型漂移(Usage i64 vs number)。→ **红线:所有跨语言类型从单一 Go schema 生成,零手抄。**
- **【Codex】协议方法爆炸(100+ 方法维护失控):** → **红线:先冻结小核心面(session/turn/approval/stream/mode/model),其余 `experimental/*`;public/internal 切分(Options vs internalOptions)。**
- **【方案四自身】误引 Claude 结构化输出机制:** → **红线:按 Claude 真实做法(校验+重试),不发明"合成工具+Stop-hook 强制"并归因给它。**
- **【方案一】panic 零隔离被卖点掩盖:** 进程内"零边界"= 零隔离,eino 内核/流式/streamHandler goroutine 的 panic 会带走宿主程序(现有 recover 只兜工具执行 panic,middleware.go)。→ **红线:文档明写"进程内 SDK 适合单租户信任域,不适合托管不受信任的多租户 agent";多租户托管必须回退 daemon + 每会话子进程。补 streamHandler goroutine 的 recover。**

---

## 8. 风险与开放问题(需 jack 拍板)

1. **eino 是否泄露给 SDK 用户?**(决策 4 的悬而未决点)本蓝图选"L1 自有类型 + `Tool` 是 `tool.BaseTool` 超集 + 给 Go 用户留透传快车道"。代价:一个永久的 eino↔自有类型翻译层(`adaptTool`/Event 翻译),eino 上游演进时会漏。**替代:纯透传 eino(更省,但绑死用户 + 破坏协议边界)。需拍板取舍。**

2. **TS 与 Python 双绑定都维护吗?**多语言优势**只惠及 Go 用户**;TS/Python 用户吃 spawn 全套复杂度,相对 Claude/Codex 无差异化。双绑定 = 双发布流水线 + 能力矩阵永久对齐纪律。**开放问题:是否先只做 TS(生态大)、Python 由需求触发?抑或都推到 Phase 3 之后由真实用户拉动?**

3. **协议兼容 ACP 到什么程度?**本蓝图选"ACP 作为 L2 的 codec 兼容层,Zed 继续连;新特性走 `jcode serve` 的更完整协议"。**开放问题:ACP 是否长期双轨维护?还是给一个迁移期后让 ACP 只保留编辑器子集?**

4. **`turn/steer`(边跑边插话)值不值得改内核循环?**现 `runner.Run` 是 turn 粒度粗函数,turn 内无法注入消息,steer 需改内核循环——而改内核循环会削弱"复用接缝零改动"的立论。**开放问题:steer 是核心能力(Codex 已有)还是 Phase 5 可选?若做,是否接受一次内核循环改造?**

5. **Phase 0 的三份装配收敛 blast radius。**它触碰用户天天用的 web/TUI/ACP 核心路径,回归炸的是存量 surface 而非新 SDK,且 sandbox 无 live server 难验证"行为不变"。**缓解建议(需确认):Phase 0 先只让 web + `jcode exec` 改用 Session(它们最接近 headless),TUI/ACP 推到 Phase 2 分摊风险。**

6. **单进程单 config/凭证域。**同进程多 Session 共享全局 config/model 解析。要在同进程跑两个不同 API key/provider 隔离的 Session,现有全局 config 假设会打架(Claude/Codex 每会话子进程天然隔离)。**开放问题:是否需要 config 作用域化到 Session 级?**

---

## 文件锚点(实现时查阅,均已核对)

- 装配收敛目标:`internal/command/acp.go:313`(buildAgentSession)、`internal/web/engine.go:37/96`(headless 原型 + EngineConfig)、`internal/command/interactive.go`(尾部 `p.Run()`)
- 直接复用:`internal/runner/runner.go:25/34`(Run 返回裸 string)、`internal/runner/approval.go`(`RequestApproval(ctx, toolName, toolArgs) (bool, error)` + 纯函数 decide)、`internal/agent/agent.go:25`、`internal/agent/middleware.go`(approval recover 只兜工具 panic;args 改写在 hook 中间件 PreToolUse "OUTSIDE approval")、`internal/handler/handler.go:19`(8 回调 + `RequestApproval(ctx, req) (ApprovalResponse, error)`)、`internal/hooks/config.go:70`、`internal/hooks/context.go:16`、`internal/mode/mode.go:17`、`internal/session/session.go:165/763/849`、`internal/tools/env.go:59`、`internal/tools/mcp.go:24`、`internal/tools/subagent.go`
- 修 bug:`internal/command/acp.go:269`(log `len(params.McpServers)`)vs `:359`(用 `cfg.MCPServers`)—— MCP 注入缺口
- 新建:`pkg/jcode/{client,session,stream,event,tool,approval,hooks}.go`(L1)、`pkg/jcode/rpc/{transport,codec,server,schema}.go`(L2)、`sdk-ts/`、`sdk-py/`(L3)、`internal/command/serve.go`
