# jcode Hooks 设计文档

> 状态：设计草案（调研阶段产出）
> 目标：为 jcode 增加一套「在 agent 执行的关键节点触发用户可配置动作」的 hook 机制，
> 覆盖 TUI / Web / ACP 三个 transport，且不侵入现有工具与审批核心逻辑。

---

## 0. 结论先行（TL;DR）

- **产品形态**：采用 **Claude Code / Qoder 同款的「外部命令 hook」范式**——配置驱动、语言无关的脚本、
  JSON over stdin/stdout、退出码控制、matcher 过滤。jcode 的用户群与 Claude Code 高度重叠，沿用同一套
  心智模型和 schema 迁移成本最低。
- **内部架构**：借鉴 **Copilot SDK / codex** 的做法，把所有 hook 收敛到一个 **transport 无关的
  `internal/hooks.Dispatcher`**，而不是塞进某个 UI handler。这样 TUI/Web/ACP 自动共享同一套 hook，
  未来还能扩展「进程内 Go 回调」「HTTP webhook」等 hook 类型。
- **挂载点**（全部已在代码中核对，见 §3）：
  - 工具前后 → `internal/agent` 新增一个 hook 中间件，包在 `approvalMiddleware` 外层。
  - 审批 → 复用 `ApprovalState`（PreToolUse 的 allow/deny 直接影响审批）。
  - 会话/turn 生命周期 → `runner.Run` 与 `command/interactive.go:handlePrompt`。
  - 压缩 → `agent/compaction.go` 已有的 `onCompact` 回调点。
  - 会话文件 → `session.Recorder` 的创建与 `Close`。
- **v1 事件集**（7 个，够用且与生态对齐）：
  `SessionStart` · `UserPromptSubmit` · `PreToolUse` · `PostToolUse` · `PostToolUseFailure` · `PreCompact` · `Stop`。
- **安全**：hook = 执行任意命令，必须有信任门槛。v1 采用 **codex 式 trust-on-first-use（内容 hash 信任）**
  + 「项目级 hook 默认不信任、需显式确认」。

---

## 1. 三方参考对比

| 维度 | Claude Code / Qoder | Copilot SDK | codex |
|---|---|---|---|
| 范式 | 外部命令（脚本） | 进程内 Go/TS 回调 | 外部命令（脚本） |
| 面向 | 终端最终用户 | SDK 嵌入者/开发者 | 终端最终用户 |
| 事件数 | 5（`UserPromptSubmit`/`PreToolUse`/`PostToolUse`/`PostToolUseFailure`/`Stop`） | 7（`onSessionStart`…`onErrorOccurred`） | 10（含 `PermissionRequest`/`Pre/PostCompact`/`SubagentStart/Stop`/`SessionStart`） |
| 配置 | `settings.json` 三层合并（user/project/local） | `CreateSession(hooks:{...})` 代码注册 | `config.toml` + `hooks.json`，8 层优先级 |
| 输入 | stdin JSON | 函数入参结构体 | stdin JSON |
| 输出 | exit code（0 放行 / 2 阻断）+ stdout JSON | 返回结构体（allow/deny/ask、改写 result/prompt） | exit code + stdout JSON（`continue`/`decision`/`updatedInput`/`additionalContext`） |
| matcher | 精确 / `A\|B` / 正则 | 无（回调内自行判断） | 正则 / `*` / 别名 |
| 阻断/改写 | PreToolUse 可 deny + `updatedInput` + `additionalContext` | onPreToolUse 返回 allow/deny/ask；onPostToolUse 改 result | 同 Claude Code，另有 `PermissionRequest` 专门自动审批 |
| 信任模型 | 加载时展示、用户审阅 | 无（都是自己写的代码） | 内容 hash 信任 + 管理员 `allow_managed_hooks_only` |

**取舍**：
- 对外 schema **对齐 Claude Code**（`hook_event_name`/`tool_name`/`tool_input`/`tool_response`/`permissionDecision`/`updatedInput`/`additionalContext` 等字段名照抄），让熟悉 Claude Code 的用户零学习成本。
- 对内实现**对齐 codex**（discovery → dispatcher → command_runner → output_parser 四段式 + 并发执行 + 信任 hash）。
- 事件集取二者交集 + jcode 真实存在的节点，v1 收敛为 7 个，**不引入独立的 `PermissionRequest`**（用 PreToolUse 的 `permissionDecision` 覆盖），也**暂不做 `SubagentStart/Stop`**（留待 team/subagent 稳定后）。

---

## 2. jcode 现状（已核对的事实）

| 事项 | 位置 | 说明 |
|---|---|---|
| Agent 单 turn 顶层 | `internal/runner/runner.go:24` `Run()` | 负责 tracing、token、todo/goal 续行、`OnAgentStart/OnAgentDone` |
| turn 内 LLM+工具迭代 | `internal/runner/runner.go:157` `runInner()` | 流式事件 → handler |
| 用户输入入口 | `internal/command/interactive.go:369` `handlePrompt()` | `RecordUser` → `runner.Run` |
| Agent 工厂 & 中间件装配 | `internal/agent/agent.go:25` `NewAgent()` | 链序（外→内）：`middlewares` → `handlers` → `approvalMiddleware` → `memory.UsageMiddleware` |
| 审批+安全中间件 | `internal/agent/middleware.go:30` `WrapInvokableToolCall` | 工具执行的唯一咽喉：审批 gate → `endpoint()` → 错误兜底 |
| 审批函数类型 | `internal/agent/agent.go:17` | `type ApprovalFunc func(ctx, toolName, toolArgs string) (bool, error)` |
| 审批决策权威 | `internal/runner/approval.go:238` `decide()` / `:359` `RequestApproval()` | 三档：AutoApprove / Prompt / PromptExternal |
| 事件总线接口 | `internal/handler/handler.go:19` `AgentEventHandler` | `OnAgentStart/OnAgentDone/OnToolCall/OnToolResult/OnTokenUpdate/RequestApproval` |
| 压缩回调 | `internal/agent/compaction.go:166` `NewCompactionMiddleware(..., onCompact)` | 已有 `onCompact(savedTokens int)` 回调 |
| 会话记录 | `internal/session/session.go:165` `NewRecorder()` / `Close()` | JSONL transcript，`transcript_path` 可得 |
| 已有 callback 惯例 | `GoalStore.OnUpdate`、budget `onWarn`、compaction `onCompact` | jcode 已大量用「函数回调」做扩展，hook dispatcher 沿用同风格 |
| 配置结构 | `internal/config/config.go:249` `Config`（扁平 JSON，`~/.jcode/config.json`） | 加一个 `Hooks *HooksConfig` 字段即可 |
| 项目级目录 | `.jcode/`（已托管项目级 skills） | 可托管 `.jcode/hooks.json` |

**关键洞察**：jcode 的工具审批与执行**已经全部收敛在 `approvalMiddleware` 一个点**，且审批是 transport 无关的（走 `ApprovalFunc`，不依赖具体 UI）。这意味着 hook 挂在中间件层，就天然对 TUI/Web/ACP 三端生效——**不要**把 hook 塞进 `AgentEventHandler`（那是 per-transport 的展示层）。

---

## 3. 挂载点设计

### 3.1 工具类事件：新增 `hookMiddleware`（`internal/agent/hook_middleware.go`）

放在中间件链里 **`approvalMiddleware` 的外层**（即作为 `handlers` 的最后一个，或在 `agent.go` 里 approval 之前 append），
使其包住「审批 + 执行」：

```text
tracing → budget/compaction/reminder → [hookMiddleware] → approvalMiddleware → memory.UsageMiddleware → tool
                                          │  PreToolUse             │ 审批 gate + endpoint()
                                          └─ PostToolUse/Failure ───┘
```

`WrapInvokableToolCall` 骨架（签名与 `middleware.go:30` 一致）：

```go
func (m *hookMiddleware) WrapInvokableToolCall(
    ctx context.Context, endpoint adk.InvokableToolCallEndpoint, tCtx *adk.ToolContext,
) (adk.InvokableToolCallEndpoint, error) {
    return func(ctx context.Context, args string, opts ...tool.Option) (string, error) {
        // ---- PreToolUse ----
        dec := m.disp.Fire(ctx, hooks.PreToolUse, hooks.Payload{
            ToolName: tCtx.Name, ToolInput: json.RawMessage(args),
        })
        switch dec.Permission {
        case hooks.Deny:
            // 阻断：不执行，把理由回给模型（复用 approval 的“被拒绝”文案）
            return denyMessage(dec.Reason), nil
        case hooks.Allow:
            // 预授权：让内层 approvalMiddleware 跳过用户弹窗
            ctx = approval.WithPreApproved(ctx)
        }
        if dec.UpdatedInput != nil { // updatedInput：改写工具参数
            args = string(dec.UpdatedInput)
        }
        if dec.AdditionalContext != "" {
            // 作为工具结果前缀注入模型（或走 reminder 通道）
        }

        result, err := endpoint(ctx, args, opts...) // 审批 + 执行都在这里

        // ---- PostToolUse / PostToolUseFailure ----
        ev := hooks.PostToolUse
        if err != nil || looksLikeToolError(result) {
            ev = hooks.PostToolUseFailure
        }
        post := m.disp.Fire(ctx, ev, hooks.Payload{
            ToolName: tCtx.Name, ToolInput: json.RawMessage(args),
            ToolResponse: result,
        })
        if post.ModifiedResult != nil { // 允许改写/脱敏结果
            result = *post.ModifiedResult
        }
        return result, err
    }, nil
}
```

**PreToolUse 的 `Allow` 如何跳过弹窗**：在 `approvalMiddleware`（`middleware.go`）里加一行短路——
`if approval.IsPreApproved(ctx) { /* 跳过 approvalFunc */ }`。这是唯一需要动到既有中间件的地方，改动极小。

> 备选方案：把 PreToolUse 的自动 allow/deny 直接**接进 `ApprovalState.decide()`**（`approval.go:238`），
> 让审批只有一个权威、PostToolUse 仍留在中间件。二者取一即可；推荐上面的 ctx-flag 方案，改动更集中。

### 3.2 会话 / turn 生命周期

| 事件 | 触发点 | 备注 |
|---|---|---|
| `SessionStart` | `session/session.go` `NewRecorder()`/`ensureFile()` 首帧落盘时；或 `interactive.go:RunInteractive` 启动处 | payload 含 `session_id`/`cwd`/`model`；`additionalContext` 可注入系统提示 |
| `UserPromptSubmit` | `interactive.go:369 handlePrompt` 里 `RecordUser` 之后、`runner.Run` 之前 | 可 deny（拦下这次提交）或 `additionalContext`（追加上下文）；对齐 Claude Code |
| `Stop` | `runner.Run` 返回后 / `OnAgentDone` 处；或 `handlePrompt` 尾部 | 可 deny → 强制 agent 续跑（质量门禁，如「跑完测试再收尾」）。**必须带 `stop_hook_active` 防死循环** |
| `PreCompact` / `PostCompact` | `agent/compaction.go:166` `onCompact` 回调前后 | 已有回调点，直接扩展 |

`UserPromptSubmit` / `Stop` 走 `runner`/`interactive` 层，同样 transport 无关。

### 3.3 事件—代码对照总表

| Hook 事件 | jcode 挂载文件:符号 | 可阻断 | 可改写 | payload 关键字段 |
|---|---|---|---|---|
| `SessionStart` | `session/session.go NewRecorder` | 否 | `additionalContext` | session_id, cwd, model, source |
| `UserPromptSubmit` | `command/interactive.go:369 handlePrompt` | 是 | `additionalContext` | prompt |
| `PreToolUse` | `agent/hook_middleware.go`（新） | 是 | `updatedInput`,`additionalContext` | tool_name, tool_input |
| `PostToolUse` | `agent/hook_middleware.go`（新） | 否 | `modifiedResult` | tool_name, tool_input, tool_response |
| `PostToolUseFailure` | `agent/hook_middleware.go`（新） | 否 | `additionalContext` | tool_name, tool_input, tool_response(err) |
| `PreCompact` / `PostCompact` | `agent/compaction.go:166` | 否 | — | trigger, saved_tokens |
| `Stop` | `runner/runner.go:Run` 尾（统一续跑循环，见 §3.4） | 是 | — | stop_hook_active |

### 3.4 统一续跑管线（Stop hook × todo/goal，参考 codex）

**codex 的关键教训：它根本没有「两套续跑」。** codex 一个 turn 只有一个内层采样循环
（`turn.rs:225`），模型自然停下时在**唯一一个决策点**跑 Stop hook（`turn.rs:373`）；hook 若 block，
就把 continuation 理由拼成一条 user 消息注入历史、置 `stop_hook_active=true`、`continue` 同一个循环
（`turn.rs:380-403`）。codex **没有独立的 todo/goal 自动续跑**，也**没有硬性次数上限**——只靠三样收敛：
hook 脚本自己看 `stop_hook_active` 决定何时放行、外层 `input_queue` 是否还有 pending、以及
`CancellationToken`（用户取消一票否决 → `TurnAborted`）。

**映射回 jcode**：现在 `runner.Run` 是 `todoLoop`（上限 3）+ `goalLoop`（上限 25）两个**顺序独立的
`for`**；直接再叠一个 Stop hook 会变成三套各自为政、无统一上限的强制续跑。按 codex 的形状，合并成
**一个续跑循环 + 一个决策聚合点**：

```text
runInner()                        // 代理采样到 LLM 不再调工具
for {                             // 单一续跑循环
    if ctx.Done() { break }       // 用户取消一票否决（保留现有语义）
    if budget exhausted { break } // 单一 umbrella 上限（见差异②）

    var reasons []string
    // ① 内建 guard 先——它们想续跑，就说明代理还没“真的要停”
    if todoStore.HasIncomplete()            { reasons = append(reasons, todoReminder) }
    if goalStore.IsActive() && goalCont!="" { reasons = append(reasons, goalCont) }

    // ② 内建 guard 都安静了，才 fire Stop hook（否则“插太早”，对着半成品跑）
    if len(reasons) == 0 {
        dec := disp.Fire(ctx, hooks.Stop, hooks.Payload{StopHookActive: stopHookActive})
        if dec.Block { reasons = append(reasons, dec.Reason); stopHookActive = true }
    }

    if len(reasons) == 0 { break }         // 三方都不续 → 真收尾
    inject(reasons); runInner()            // 注入成一条 user 消息，再跑一轮
}
h.OnAgentDone(nil)
```

**与 codex 三处有意的差异**：

1. **顺序**：内建 guard（todo/goal）排在 Stop hook **前**。Stop 语义是「代理真要停了」，jcode 内建 guard
   还想续跑就说明没到那步，此时 fire Stop hook 会对着半成品跑。每轮先清内建 guard，清空了才问 Stop hook。
2. **保留 umbrella 硬上限**（**jcode 必须与 codex 不同处**）：codex 敢不设上限，是因为它的续跑**永远由脚本
   显式驱动**；jcode 的 todo/goal 是**自动**续跑、无脚本兜底，一个永远证明不了完成的 goal 会无限滚。故必须
   把现有的 `maxGoalContinuations=25` 提升为**整个续跑循环共享的一个 budget**（todo/goal/hook 共用一个计数），
   而非各自为政的 3 与 25。
3. **`stop_hook_active` 跨轮携带**：Stop hook block 一次后置位，下一轮 fire 时带 `StopHookActive:true` 进
   payload，脚本据此放行（Qoder / Claude Code / codex 同款防自锁）。

**净效果**：三套「强制续跑」合成一条管线——**一个上限、一处取消检查、一个决策聚合点**——正是 codex 的形状，
只多保留了 jcode 的内建 guard 与一道机器上限做兜底。

---

## 4. 配置格式

### 4.1 文件位置与分层（**已定：方案 B — 独立 hooks.json 三层**）

- ~~方案 A：塞进 `~/.jcode/config.json` 的 `hooks` 键~~ —— 已否，缺项目级 / 本地层。
- **方案 B（已采纳）**：独立 `hooks` 配置，三层合并（对齐 Claude Code / Qoder，也贴合 jcode 已有的 `.jcode/` 项目目录）：
  1. `~/.jcode/hooks.json` —— 用户级（最低优先级，默认加载）
  2. `.jcode/hooks.json` —— 项目级（可随 git 分享）
  3. `.jcode/hooks.local.json` —— 项目本地（`.gitignore`，最高优先级）

  合并策略：同事件下的 hook 组**追加**（不覆盖），保证项目和用户 hook 都能跑。

### 4.2 Schema（对齐 Claude Code）

```json
{
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "Bash|run_shell",
        "hooks": [
          { "type": "command", "command": "~/.jcode/hooks/guard.sh", "timeout": 10 }
        ]
      }
    ],
    "PostToolUse": [
      {
        "matcher": "write_file|edit_file",
        "hooks": [
          { "type": "command", "command": "gofmt -w \"$JCODE_TOOL_FILE\"" }
        ]
      }
    ],
    "Stop": [
      { "hooks": [ { "type": "command", "command": "~/.jcode/hooks/test-gate.sh" } ] }
    ]
  }
}
```

Go 结构（`internal/config/config.go` 新增，挂到 `Config`）：

```go
type Config struct {
    // ...既有字段...
    Hooks *HooksConfig `json:"hooks,omitempty"`
}

type HooksConfig struct {
    // event name → 若干 matcher 组
    Events map[string][]HookGroup `json:"-"` // 反序列化时按事件名铺平
}

type HookGroup struct {
    Matcher string     `json:"matcher,omitempty"` // 空/“*”=全匹配；支持 A|B；支持正则
    Hooks   []HookSpec `json:"hooks"`
}

type HookSpec struct {
    Type    string `json:"type"`              // v1 仅 "command"
    Command string `json:"command"`
    Timeout int    `json:"timeout,omitempty"` // 秒，默认 60
    Async   bool   `json:"async,omitempty"`   // 非阻断事件可 fire-and-forget
}
```

### 4.3 matcher 语义（**已实现：精确优先**）

matcher 按 `|` 拆分，**每个片段独立判定**：

- 省略 / `*` → 匹配全部工具
- **无正则元字符的片段 → 精确全等**（`"write"` 只命中 `write`，**不会**误匹配高频的 `todowrite`/`overwrite`）
- 含元字符（`.^$*+?()[]{}\`）→ 当正则（`"mcp__.*"`、`"^execute$"`）
- 竖线 → 片段并集（`"write|edit"` = 精确匹配 write 或 edit）
- **工具别名表**：真实工具名 `execute`/`write`/`edit`/`read`/`grep`/`glob` ↔ Claude Code 名 `Bash`/`Write`/`Edit`/`Read`/`Grep`/`Glob`，让抄来的配置可用。

> 「精确优先」是审查发现 H3 的修复：unanchored 正则会让 `"write"` 静默命中 `todowrite`（每次 todo 更新都触发），是明确 footgun。

---

## 5. 执行协议（对齐 Claude Code / codex）

### 5.1 输入（stdin，JSON）

```json
{
  "session_id": "uuid",
  "transcript_path": "/Users/.../.jcode/sessions/xxx.jsonl",
  "cwd": "/Users/jack/workpath/jjj/jcode",
  "hook_event_name": "PreToolUse",
  "tool_name": "write_file",
  "tool_input": { "path": "a.go", "content": "..." },
  "tool_response": "…",          // 仅 PostToolUse*
  "prompt": "…",                  // 仅 UserPromptSubmit
  "stop_hook_active": false       // 仅 Stop
}
```

同时注入环境变量，方便脚本免解析：`JCODE_SESSION_ID`、`JCODE_TOOL_NAME`、`JCODE_CWD`、`JCODE_TRANSCRIPT_PATH`、`JCODE_HOOK_EVENT`。

### 5.2 输出（exit code + stdout）

| exit code | 行为 |
|---|---|
| `0` | 放行；若 stdout 有 JSON 则解析 `hookSpecificOutput` |
| `2` | 阻断（仅可阻断事件）；stderr 注入对话反馈给模型 |
| 其它 | 非阻断错误；stderr 展示给用户，但**不**中断 agent |

stdout 结构化输出（exit 0）：

```json
{
  "hookSpecificOutput": {
    "hookEventName": "PreToolUse",
    "permissionDecision": "allow|deny|ask",
    "permissionDecisionReason": "…",
    "updatedInput":  { "...": "改写后的 tool_input" },
    "additionalContext": "注入给模型的额外上下文",
    "modifiedResult":  "PostToolUse 改写后的结果"
  }
}
```

### 5.3 fail-safe（对齐 Qoder）

- 超时（默认 60s，Qoder 30s）→ 当作放行，不阻断。
- 未知 exit code → 非致命，展示不阻断。
- hook 崩溃 → 记录 warning，agent 继续（`HookResult::FailedContinue` 语义）。
- `Stop` hook **必须**在 `stop_hook_active==true` 时 exit 0，否则死循环。

---

## 6. 内部架构：`internal/hooks/`

对齐 codex 的四段式，但用 Go 惯用法：

```text
internal/hooks/
├── config.go       // HooksConfig 解析 + 三层合并 + 别名表
├── dispatcher.go   // Fire(ctx, event, payload) → Decision；matcher 选择 + 并发执行 + 结果折叠
├── runner.go       // command_runner：起子进程、stdin 喂 JSON、超时、捕获 stdout/stderr/exit
├── parse.go        // output_parser：解析 exit code + hookSpecificOutput
├── trust.go        // 内容 hash 信任（trust-on-first-use）
└── types.go        // Payload / Decision / HookSpec
```

`Dispatcher` 单例在启动时构建（`interactive.go` / web engine / acp server 都注入同一个），
经由 `context` 或依赖注入传到中间件与 runner。**决策折叠规则**（多个 hook 命中同一事件时）：
任一 `deny` 即最终 deny（保守）；`updatedInput`/`modifiedResult` 按配置顺序链式套用；
`additionalContext` 拼接。

**为什么不用 Copilot SDK 的纯进程内回调**：jcode 的 hook 是**给最终用户配的**（不是给嵌入者写代码），
外部命令才是对的产品面。但架构上 `Dispatcher.Fire` 的返回类型 `Decision` 与 hook `type` 解耦——
未来加 `"type": "webhook"` 或 `"type": "builtin"`（进程内 Go 回调）只是多一个 runner 实现，事件与挂载点不变。

---

## 7. 安全与信任

hook 会执行任意命令，是明确的攻击面（尤其项目级 `.jcode/hooks.json` 可能来自不可信仓库）。

> **v1 已落地的门控**：hook 命令一旦其事件触发即为任意代码执行——`SessionStart` 甚至在开会话瞬间就跑，
> `PreToolUse` 的 `permissionDecision=allow` 还能静默绕过审批。因此 `hooks.Load(configDir, workDir, trustProject)`
> **默认只加载用户级 `~/.jcode/hooks.json`**；项目级 `.jcode/hooks.json` / `.jcode/hooks.local.json` 仅在
> `trustProject==true` 时加载（当前由环境变量 `JCODE_HOOKS_TRUST_PROJECT=1` 显式开启）。这是 trust-on-first-use
> 落地前的临时闸门，杜绝「clone 恶意仓库即被 getshell / 静默提权」。

后续完整信任模型：

1. **trust-on-first-use（codex 式）**：首次发现某 hook（按 `source_path + command` 内容算 sha256）时标记
   `untrusted`，在 TUI/Web 提示用户审阅并确认；确认后把 `trusted_hash` 写入 `~/.jcode/hooks-state.json`。
   内容变更 → 重新 untrusted。
2. **来源分级**：`~/.jcode/` 用户级默认信任（是用户自己机器上的配置）；`.jcode/*` 项目级默认**不信任**，
   需显式确认——防止 clone 一个仓库就被其 hook 提权。
3. **可观测**：每次 hook 运行发 `HookStarted`/`HookCompleted` 事件（对齐 codex `HookRunSummary`），
   在 TUI 状态行/Web 面板可见，stdout/stderr 落 transcript。
4. **企业锁**（可选，后续）：`allow_managed_hooks_only` 之类开关，禁用用户/项目 hook。
5. `/hooks` slash 命令 + Web 管理面板：列出、启用/禁用、查看信任状态（复用 MCP/skills 已有的管理 UI 模式）。

---

## 8. 分期实施计划

**Phase 1 — 骨架 + 工具类 hook（MVP）**
- `internal/hooks/`：config 解析、dispatcher、command runner、output parser。
- `Config.Hooks` 字段 + 方案 B 的三层加载。
- `agent/hook_middleware.go`：`PreToolUse` / `PostToolUse` / `PostToolUseFailure`。
- `approvalMiddleware` 加 `IsPreApproved(ctx)` 短路。
- 工具别名表。
- 单测：deny 阻断、updatedInput 改参、modifiedResult 改结果、超时放行。

**Phase 2 — 生命周期 hook**
- `UserPromptSubmit`（interactive）、`Stop`（runner，含 `stop_hook_active` 防死循环）、
  `SessionStart`（session）、`PreCompact/PostCompact`（compaction）。

**Phase 3 — 信任与可观测**
- trust-on-first-use + `hooks-state.json`；`HookStarted/Completed` 事件；transcript 落盘。
- `/hooks` 管理命令 + Web 面板；ACP 侧事件透出。

**Phase 4 — 扩展 hook 类型（可选）**
- `"type": "webhook"`（HTTP）/ `"type": "builtin"`（进程内 Go 回调，给 automation/team 复用）。
- `SubagentStart/Stop`（配合 team）。

---

## 9. 开放问题（需拍板）

1. ~~配置放哪~~ **已定：方案 B（独立 `hooks.json` 三层，见 §4.1）**。
2. **是否引入独立 `PermissionRequest` 事件**（codex 有）？v1 建议不引入，用 `PreToolUse.permissionDecision` 覆盖。
3. ~~`Stop` hook 与 todo/goal 续行如何叠加~~ **已定：合并成单一续跑管线（§3.4，参考 codex）**。
   实现时仅需敲定 umbrella budget 的具体数值（沿用 25，或按 turn 复杂度动态）。
4. **别名表维护成本**：jcode 工具名与 Claude Code 不完全一致，别名表要不要做成配置可覆盖？
5. **Web/ACP 的信任确认 UX**：非交互式（automation/cron）场景下 untrusted hook 怎么处理——跳过并告警，还是需预先信任？

---

## 10. 测试方案（实现前设计）

**沙箱现实**：jcode 的 `agent-eval/` e2e 用**真实 LLM**（无 mock model）+ 隔离 HOME + 决定论 oracle
（`agent-eval/suite/verify.py` 20+ 种），跑真 ACP 子进程。本开发沙箱不能联网 / 绑 socket、无真实 key，
**完整 ACP 套件在此跑不了**。故测试分五层，明确「哪些现在能真跑」：

| 层 | 位置 | 覆盖 | 沙箱可跑 |
|---|---|---|---|
| L1 hooks 包单测 | `internal/hooks/*_test.go` | 命令 runner（真起子进程/临时脚本）、exit code 0/2/其它、stdout JSON 解析、matcher（精确/`A\|B`/正则/别名）、三层 hooks.json 合并、超时=放行、决策折叠（任一 deny→deny） | ✅ 是 |
| L2 中间件单测 | `internal/agent/hook_middleware_test.go` | `WrapInvokableToolCall`：PreToolUse deny 阻断（endpoint 不被调）、`updatedInput` 改参、allow→ctx 置 pre-approved、PostToolUse `modifiedResult` 改结果、PostToolUseFailure 分支 | ✅ 是（mock dispatcher + spy endpoint） |
| L3 续跑逻辑单测 | `internal/runner/*_test.go` | 把统一续跑决策抽成纯函数 `nextContinuation(...)`，测：todo/goal/Stop 优先级、umbrella budget 收敛、`stop_hook_active` 跨轮、`ctx.Done()` 一票否决 | ✅ 是（纯函数，无需 fake agent） |
| L4 真 agent 集成 | `internal/agent/hook_e2e_test.go` | scripted `ToolCallingChatModel` 发一个 tool_call → 真 dispatcher + 真 hookMiddleware + 真 `NewAgent`：断言 hook 脚本落下证据文件 / deny 真阻断工具 | ⚠️ 尝试；AsyncIterator 接线繁琐则降级为文档 |
| L5 ACP e2e | `agent-eval/suite/testcases.json` | 真 LLM 驱动：PreToolUse deny→`file_absent`、PostToolUse 脚本→证据文件、Stop 门禁续跑。用隔离 HOME 注入 `.jcode/hooks.json` | ❌ 需真实 key，交付用户跑 |

**决定论要点**：L1–L4 全程无 LLM、无网络，用「hook 脚本写证据文件 / spy endpoint / 纯函数」三种确定性断言。
L5 沿用 jcode 既有 oracle（`file_absent`/`file_contains`/`bounded_tool_calls`/`final_text_contains`），
LLM 不确定但 oracle 只看**最终副作用**，故判定确定。

**L5 用例草案**（交付 `testcases.json`）：
```json
{ "id": "hook_pretooluse_deny", "prompt": "创建 forbidden.txt 内容 x",
  "home_fixtures": { ".jcode/hooks.json":
    "{\"hooks\":{\"PreToolUse\":[{\"matcher\":\"write_file\",\"hooks\":[{\"type\":\"command\",\"command\":\"exit 2\"}]}]}}" },
  "oracles": [ {"type":"file_absent","path":"forbidden.txt"}, {"type":"bounded_tool_calls","max":6} ] }
```

**验收标准（本会话）**：`go test ./internal/hooks/... ./internal/agent/... ./internal/runner/...` 全绿；
L5 用例写入 testcases.json 但标注需真实 key。

---

## 11. v1 实现状态 & 对抗审查结论

**已实现（三端通用）**：`internal/hooks/`（dispatcher/config 三层加载/matcher/命令 runner/parser）+
`internal/agent/hook_middleware.go`（pre 在 approval 外、post 在内）+ approval 预授权短路 + runner 统一续跑管线
（含 Stop hook）+ TUI/Web/ACP 三端经 `hooks.NewSessionDispatcher` 注入 ctx。事件：SessionStart /
UserPromptSubmit / PreToolUse / PostToolUse / PostToolUseFailure / Stop。测试 28 项全绿（dispatcher 18 +
中间件 8 + 续跑 2），另有 `agent-eval` 两个真实 LLM 用例（`hook_pretooluse_deny`/`hook_posttooluse_side_effect`）。

**对抗审查已修**：
- **C1/C2（RCE + allow 提权）** → 项目级 hooks **默认不加载**，仅 `~/.jcode/hooks.json`；项目层需
  `JCODE_HOOKS_TRUST_PROJECT=1` 显式开启（§7）。
- **H1（Web/ACP 未接线）** → 三端已统一注入 dispatcher。
- **H2（payload 被清空）** → `mustJSON` 失败时只丢 `tool_input`，保留 tool_name/事件名等。
- **H3（matcher 子串误匹配）** → 精确优先（§4.3）。
- **中危-4（续跑预算回归）** → umbrella = 25 + todo 3 = 28，保住 goal 原 25。
- **高危-3（async goroutine 泄漏）** → async hook 加 30s 硬上限。

**审查确认健壮**：续跑无界/死循环（25 硬顶 + todo 子顶 + `ctx.Done()` 一票否决 + `stop_hook_active` 防自锁）、
post hook 拿到真实 err、超时 fail-safe、regex 缓存并发安全、deny 短路不误伤 PostToolUse、pre-approved 不跨会话泄漏。

**v1 已知边界（后续）**：**工具类 hook（PreToolUse/PostToolUse/PostToolUseFailure/Stop）三端（TUI/Web/ACP）
均生效**；但 **prompt 级 hook（UserPromptSubmit / SessionStart）目前仅 TUI**——Web/ACP 上要正确接线需处理各自
的「拒绝 prompt」响应语义和 SessionStart 的 per-session 生命周期，列为 follow-up。因项目级 hook 默认不加载
（只用户自己的 `~/.jcode/hooks.json`），此差异是功能不完整而非提权。其余：`Decision.SystemMessage` 仅落日志未
surface 到 UI（M1）；PreToolUse 的 `additionalContext` 拼在结果后而非工具前（M3）；SessionStart payload 缺
`model`/`source`（L6）；Pre/PostCompact 事件（常量暂未定义，见 `types.go`）、trust-on-first-use（hash 信任 UI）、
`/hooks` 管理面板、teammate/subagent 的 hook 覆盖。

---

## 附：为什么挂中间件而不是 handler（一句话）

`AgentEventHandler`（`internal/handler`）是 **per-transport 的展示层**（TUI/Web/ACP 各一份实现）；
而工具审批/执行早已收敛在 **transport 无关的 `approvalMiddleware`**。把 hook 挂在中间件 + runner 层，
**一次实现，三端生效**；挂在 handler 层则要在每个 transport 重复接线，且拿不到「改写参数/阻断执行」的能力。
