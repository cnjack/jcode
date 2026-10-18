# 跨会话查看（cross-session view）——独立架构评审

> 评审对象：`internal-doc/cross-session-view-design.md`（699 行，rev 待评审稿）
> 评审基准：`internal-doc/cross-session-view-scope.md`（Lead 约束）、`internal-doc/research/cross-session-{dsh,codex,kimi}.md`、`AGENTS.md`
> 代码基线：`main` @ `24aba5d`（工作树除 `internal-doc/` 外干净），所有 `file:line` 由评审者本人打开代码核对，未采信设计文档的引用。
> 评审者未参与设计，立场为对立审查。本文只读评审，未改动设计文档、生产代码或测试。

---

## 0. 结论

**VERDICT: APPROVE WITH CHANGES**

| 项 | 结论 |
|---|---|
| 是否有阻塞项 | **有，3 个**（B1–B3）。均为文档/决定层面的修正，不需要推翻架构 |
| 架构骨架 | 成立。服务层/工具层分离、身份只来自 `Env`、无 workspace 参数、不存在与越权同文案，方向正确 |
| 最关键的假设 | **"fail closed" 对子 agent / teammate 成立**（已逐行验证，见 §3.1）；**"安全边界" 不成立**（见 B1） |
| 最需要 Lead 决策 | B1 的验收口径修正；B3 的读取保真度取舍 |
| 最值得肯定 | 用 `SessionCallerFn` 而非"文件反查"拿到 Project；把 `select:`/`tool_search` 的注册失败面提前用 catalog 测试锁住；对 JSONL 写入侧转义的预筛修正 |

三个阻塞项一句话：

1. **B1 威胁模型前提是假的**：设计第 1 节称 `~/.jcode/sessions`「不在任何工具可达范围内」，实际 `grep`/`read`/`execute cat` 今天就能读，且 `grep` 免审批 —— 因此本项目做的是**策略护栏**而非安全边界，scope §6「无法读到其它 workspace 的会话」按字面不可达，必须改写验收口径并把该事实写进测试与风险表。
2. **B2 索引来源的 session id 未校验**：`session_search` / `session_list` 的候选 id 直接取自 `session.json`，只对"模型传入的 id"做 `ValidateSessionID`，索引里一条 `"uuid":"../../x"` 就能让搜索打开任意路径。
3. **B3 单 entry 4 KiB 截断没有续读路径**：长 user/assistant 消息的尾巴永远读不到，与"拿到确切内容"的目标矛盾；要么让单条 entry 允许吃满整次调用预算，要么加 `entry_offset`。

---

## 1. 阻塞项（BLOCKING）

### B1（安全）威胁模型前提错误：`~/.jcode/sessions` 今天就是模型可达的

**设计原文**（`cross-session-view-design.md:27`）：

> 但模型只有 `read`/`grep`/`glob` 这类工作目录工具，`~/.jcode/sessions` 不在任何工具可达范围内。

**这条是错的。** 证据（评审者逐条打开）：

| 通道 | 证据 | 是否需要审批 |
|---|---|---|
| `grep` 传任意 `path` | `internal/tools/grep.go:21-23`（`GrepInput.Path`，无工作目录约束）；`internal/runner/approval.go:240`（`"grep": true`）→ `decide()` 在 `:291` 直接 `decisionAutoApprove` | **不需要**（任何模式、任何传输、任何路径） |
| `glob` 传任意 `path` | `internal/tools/glob.go:15-18` | 不需要（`approval.go:239`） |
| `read` 绝对路径 | `internal/tools/read.go:84` → `internal/tools/env.go:141-143`：`filepath.IsAbs` 直接 `Clean` 返回，**不检查是否在工作目录内**；只有相对路径逃逸才打日志（`env.go:146-148`） | MANUAL 模式下 `approval.go:359-369` 对 workpath 外路径返回 `decisionPromptExternal`（会提示） |
| `execute` + `cat` | `internal/tools/shell_policy.go:5-11`（`readOnlyShellPrograms` 含 `cat`）；`internal/runner/approval.go:370-390`（read-only 命令直接 auto-approve） | **不需要**；Plan 模式同样放行（`internal/tools/execute.go:143`，`plan_execute` 共用同一 allowlist） |

也就是说：**模型一次免审批的 `grep(pattern="…", path="$HOME/.jcode/sessions")` 就能拿到任意 project 的会话内容**，成本低于调用 `session_search`。设计的整套授权矩阵在"能力"层面没有增加任何东西，它增加的是"**模型主动去做 vs 顺手做**"的差别。

**后果（为什么这是阻塞项而不是"备注"）**：

1. scope §6 验收第 2 条「跨 project 读取被拒绝」会被实现者理解为"安全属性已验证"，实际只是"新工具不越权"；把它写进 PR 与文档会误导后续维护者。
2. R6 的理由「与现状对齐（模型本就能 `read` 整个工作目录）」同样错误：模型能读的**不止工作目录**，是整块本地盘（除需审批的 `read` 外，`grep` 连审批都没有）。
3. §6.3 的反例 E2E 会**假通过/假失败**：让模型读另一 project 的会话时，它完全可能改用 `grep`/`execute cat` 拿到 `quokka-99`，此时"必须不出现 `quokka-99`"的断言失败，而失败原因不是被授权拦住，而是模型换了个工具。

**修复（必须落到文档 + 测试，不改架构）**：

- 改写 §1 该句与 R6 为：「跨会话工具是**策略护栏**（降低误跨项目与提示注入驱动的顺手外泄），不是可强制执行的隔离边界；`grep`/`read`/`execute` 仍可读 `~/.jcode/sessions`，v1 不解决」。
- 在 §5.4 的 `session_search` description 尾部加一句（可选，但建议）：「This tool is the supported way to read history; other tools may technically reach the same files, but relying on that is out of policy.」——**或者**明确决定不加，理由是 prompts 里已有 "function schemas are the source of truth"（见 §6.4），二者取一，不要沉默。
- §6.3 反例增加约束：prompt 明确 "Use only the session_* tools for this task; do not use execute/grep/read on `~/.jcode`"，并把 **tool_call 审计**（见 §8.2）作为主证据，终端文本只作辅证；若模型仍绕过，记录为**产品发现**而非用例失败。
- 向 Lead 提 scope 变更建议（见 §9）：把 §6 第 2 条改成「新工具自身在 project 边界上 fail closed；模型经既有 shell/文件工具绕过属于另行追踪的既有问题（建议后续单独立项给 `read`/`grep` 增加 `~/.jcode` 拒绝）」。**这是本次评审建议的唯一 scope 变更。**

---

### B2（安全）索引来源的 session id 未做 `ValidateSessionID`，存在路径拼接漏洞

**设计现状**：§5.3 只在 `(4)` 对**模型传入**的 `id` 校验 `ValidateSessionID`；而 `search`/`list` 的候选来自 `(6)` 之外的 `ListSessions(caller.Project)`，其 `SessionMeta.UUID` **直接来自索引文件**，未经任何校验。

**代码证据**：

- `internal/session/session.go:1788-1805`（`ListSessions`）：从 `session.json` 反序列化后原样返回 `idx.Sessions[project]`，无校验。
- `internal/session/session.go:1891-1914`（`FindSessionMeta`）：唯一带 `ValidateSessionID` 的入口，但搜索路径不经过它（搜索要批量扫，不会逐条 `FindSessionMeta`）。
- 会话文件路径构造方式（`session.go:1926-1930`）：`filepath.Join(dir, id+".json")`。

**攻击/故障面**：`session.json` 是 `0700` 目录下的普通文件，但**模型可以用 `execute` 写它**（B1 已证明），旧版本/外部工具/手工编辑同样可能写坏。一条 `{"uuid":"../../../etc/passwd","project":"<caller project>"}` 会让 `session_search` 打开会话目录外的文件；若该文件恰好按行 JSON 可解（或攻击者先放一个构造文件），`Snippet` 会把内容带回模型上下文。`session_list` 的 `SubagentTranscripts` 计数也会 `ReadDir(<SessionsDir>/<uuid>/subagents)`（§5.7），同样受影响。

**修复（约 5 行）**：

```go
// internal/sessionview 内唯一允许构造会话路径的入口
func sessionPath(id string) (string, error) {
    if err := session.ValidateSessionID(id); err != nil { // session.go:761-775
        return "", ErrSessionUnavailable
    }
    dir, err := config.SessionsDir()
    if err != nil { return "", err }
    return filepath.Join(dir, id+".json"), nil
}
```

- `search.go` / `candidates.go` / `list` 的 `SubagentTranscripts` 计数 / `lineage.go` 的 `ReadDir(<id>/subagents)`，**全部**经由此函数；失败计入 `Skipped++` 并记日志 `skip target=%s reason=invalid_id`。
- `ResolveTarget` 的 self 分支也要调用它（self 的 id 同样来自 `Env`，见 §3.2 的加固建议）。
- §6.1 增加用例：`TestSearchSkipsInvalidIndexUUID` —— 索引里放 `"../../x"`、`"a/b"`、`""`、`".."` 四条，断言全部 `Skipped`、零次文件打开（可用 `t.TempDir()` 外的 canary 文件验证未被读取）。

**为什么阻塞**：这是本设计里**唯一**真实存在的读取越界路径，修复成本极低；不修则"fail closed"这句话在索引被污染时直接失效。

---

### B3（正确性）单 entry 4096 字节封顶且无续读路径，与"读到确切内容"的目标冲突

**设计现状**：`max_entry_bytes=4096`（§5.6 预算常量、§5.8 配置默认值）+ 截断标记 `… [entry truncated: N more bytes]`；`session_read` 的参数只有 `session_id` / `start` / `limit`，**没有任何手段拿到同一条 entry 的后半段**（用同一个 `start` 重读只会再拿到同一个 head）。

**为什么这是问题**：

- 用户粘贴的长栈/长日志、助手的长解释经常 > 4 KiB（设计自己 §5.6 的示例里 `tool_call args` 就已经触发 `[entry truncated: 3072 more bytes]`）。
- scope §1 的一句话验收是「拿到确切的用户/助手内容与事件」；对 > 4 KiB 的条目，"确切内容"永远拿不到，且模型无从得知该换什么参数（工具描述里没有"如何读全"）。
- 默认 `limit=40` 与 `max_output_bytes=16384` 也不自洽：40 条 entry 的渲染上限是 160 KiB，实际几乎每次调用都会先撞 16 KiB 再打 `[output truncated …]` 尾巴 —— 默认参数组合注定产生一条噪声行。

**修复（二选一，都保持 3 工具形态）**：

- **方案 A（推荐，改动最小）**：预算语义改为"**首条 entry 可以吃掉本次调用的剩余预算**"：命中 `start` 的那条 entry 上限取 `max(max_entry_bytes, max_output_bytes)`（即 16 KiB），后续 entry 仍按 `max_entry_bytes` 递减；工具描述加一句「A single long entry may be returned up to max_output_bytes.」。仍读不全的极端条目按 `… [entry truncated: N more bytes]` 保留。
- **方案 B（保真度最高）**：`session_read` 增加可选参数 `entry_offset`（该 entry 内的 rune 偏移，默认 0），返回时在尾部给出 `next entry_offset=N`。schema 只多一个 integer，语义清晰，且与 `start` 正交。

同时把 `limit` 默认值从 40 调到 10（一次 16 KiB 大约 4–8 条 entry，40 只会制造截断噪声）。

**理由（为什么阻塞而不是建议）**：这是**面向模型的 schema**，一旦发布再改（尤其方案 B 加参数）就是 breaking；而且它会直接让 §6.3 的正例 E2E"quote the full line"在真实长文本上失败。

---

## 2. 非阻塞建议（NON-BLOCKING）

| # | 建议 | 依据 |
|---|---|---|
| N1 | **`session_list` 是否排除 self 要写清**。设计只承诺「self 永不参与搜索」（§5.5），`session_list` 没有表态。建议**包含** self 并在输出里标 `(this session)`，理由：模型拿到自己的 uuid 后才能用 `session_read` 读自己被压缩掉的历史，这是本设计的隐藏价值点 | §5.5 vs §5.6 |
| N2 | **`ListResult` 类型未定义**。§5.2 的 API 清单里 `List(...) (ListResult, error)` 引用了不存在的类型，`SessionRef`/`SearchResult`/`ReadResult` 都有定义 | design:206 |
| N3 | **渲染表漏了 `generation_operation` / `provider_tool_operation`**。`session.go:31-54` 共 21 个 `EntryType`，§5.6 的"其余"枚举没列这两个安全日志类型；请显式归入"一行结构化摘要"，并禁止渲染 `OperationCapabilityKey`/`OperationCredentialFingerprint` 等字段 | `session.go:52-53`、`:376-392` |
| N4 | **禁止渲染 `Entry.Images` 与 `OpaqueResponseItems`**。`session.go:311-321`：`Images[].Data` 是 base64，一条带图的 user entry 可达数 MB；任何"顺手 `json.Marshal(entry)`"的实现都会瞬间打爆 16 KiB 预算。请写进渲染约束 + 单测 | `session.go:311-320` |
| N5 | **`resolvePath` 的 TOCTOU**：§5.3 的 `ResolveTarget`（读首行复检 project）与 `read.go`（再打开文件流式读）是两次 open。请明确要求**同一个 `*os.File` 句柄**贯穿"首行校验 → 逐行读取"（或 `fstat` + `SameFile` 校验），否则授权检查的文件与实际读的文件可以是两个 | §5.3 步骤 (6) 与 §5.5 |
| N6 | **`SessionCallerFn` 闭包必须 nil 检查 `rec`**。§3.2 的示例代码直接 `rec.UUID()`；三条传输现有写法都是 `if rec != nil { env.SessionIDFn = rec.UUID }`（`interactive.go:1532-1534`、`acp.go:539-541`、`web.go:508-510`）。`rec` 为 nil 时会在第一次工具调用 panic | 同上三处 |
| N7 | **时间预算与调用方取消要区分**。§5.5 说超预算返回部分结果（不是 error），§5.10 说取消原样返回 `ctx.Err()`。实现必须用**派生 ctx 判 `parent.Err() == nil`** 来区分"预算到"与"用户取消"，否则要么把取消吞成成功，要么把预算做成错误 | §5.5、§5.10 |
| N8 | **超时 footer 文案里的数字要真实**。`Stopped after 3.0s (scanned 12 of 48 sessions)` 需要 `Eligible`/`Scanned` 在超时路径上也准确；建议单测断言 | §5.5 |
| N9 | **默认 `enabled: true` 与 DSH 的回归教训正面冲突，文档要表态**。`research/cross-session-dsh.md:495-505` 记录的是一次"默认发布 → 回滚"事故，结论是模型侧 session 工具**默认不挂**。scope §3.3 定了 true（本评审认可产品理由），但设计必须有**一句话承认这个冲突**并说明为什么 jcode 不同（工具数 3 vs 5、无 prompt section、有 `enabled:false` 一键全关、无跨机器面）——否则未来 review 会重复扯皮 | `research/cross-session-dsh.md:495-505`、`:708-712` |
| N10 | **`Entry` 渲染缺类型时 fail closed**：遇到未知 `EntryType` 只输出 `[n] <type> (unrecognized)`，绝不回退到 `Content` 全文（向前兼容的旧二进制读到新类型时的安全默认） | `session.go:31-54` |
| N11 | **`SessionCaller` 与 `SessionIDFn` 双来源的漂移风险**：建议 `SessionIDFn` 由 `SessionCallerFn` 派生（或加一条单测断言两者同源），避免将来只改一处 | `env.go:46-48` |
| N12 | **`session_search` 排序可用"命中数 → 新近"**：目前纯 recency，最新命中未必最相关；`MoreHits` 已在手，改排序是一行 | §5.5 |
| N13 | **LOC 估算偏低**：服务层 7 文件 + 工具层 2 文件 + 渲染/预算/UTF-8/表驱动测试，按 jcode 现有密度更可能在 2 300–2 700 行；建议把 T3–T6 的估算各上浮 20% 以免排期失真 | §8 |
| N14 | **`session_view_tools.go` 的守卫测试要"值"而不是"调用"**：`go/parser` 断言「文件里出现 `sessionViewTools(`」是弱守卫（把结果赋给 `_` 也能过）。更好的守卫是对三条传输各跑一次 `buildXxx()` 后断言工具名集合包含三者——即使需要抽个 `toolNamesForTest()` helper | §6.2 |
| N15 | **`sess_<uuid>` 前缀**：ACP 对外 id 是 `acp.go:446` 的 `sess_<uuid>`，而 `session_list` 返回裸 uuid。模型把 ACP id 当 `session_id` 传进来会得到 `NOT_FOUND`。建议在 `NOT_FOUND` 的 Hint 里补一句 "ids look like `<uuid>` (no prefix)" | `acp.go:446` |

---

## 3. 安全评审（逐项结论）

### 3.1 子 agent / teammate 是否真的拿不到工具 —— **成立（fail closed）**

| 路径 | 证据 | 结论 |
|---|---|---|
| subagent 工具集 | `internal/tools/subagent.go:561-605` `buildTools()` 手工构造 `read/grep/plan_execute/execute/edit/write/todo*/task_*`，**不含任何 session 工具**；不经过 `buildAllTools` | ✅ 未注册 |
| subagent 的 Env | `internal/tools/env.go:156-170` `CloneForSubagent()` 是白名单式结构体字面量，**不复制 `SessionIDFn`**；新增 `SessionCallerFn` 同理不会复制 | ✅ 身份为空（双保险） |
| teammate 工具集 | `internal/command/interactive.go:118-152` `buildTeamChildTools()` 两个分支（plan profile / write profile）都硬编码，**不含 session 工具** | ✅ 未注册 |
| teammate 的 Env | `internal/command/interactive.go:1676-1678` `EnvFactory: func(cwd string) any { return tools.NewEnv(cwd, platform) }` —— **全新 Env**，不是 clone | ✅ 身份为空 |
| flow (workflow_run) 子 agent | `internal/tools/flow_spawn.go:70` `deps.Env.CloneForSubagent()` + `:145-161` `flowTools()` 硬编码 | ✅ 未注册 |
| approval-reviewer | `internal/review/investigate.go:51-56` `tools.NewEnv(...)` + 固定 3 个只读工具 | ✅ 未注册 |
| memory 离线蒸馏 | `internal/memory/pipeline/phase2.go:193` 独立 toolset | ✅ 未注册 |

**结论：设计 §3.1 的 (d)/(e) 两行完全正确，"若误注册则 fail closed" 的两道保险也都真实存在。** 这是本次评审最想验证的点，结论是肯定的。

### 3.2 调用者身份来源

- 设计新增 `Env.SessionCallerFn` 而不是反查文件，理由（recorder 懒创建文件，`session.go:1422-1425`，首轮可能还没落盘）**成立**。
- 三个接线点行号全部正确：`interactive.go:1528-1534`、`acp.go:442`+`:540`、`web.go:501-509`。
- **加固建议（并入 B2 的修复）**：`ResolveTarget` 的 `id == caller.SessionID → self` 分支排在 `ValidateSessionID` **之前**（§5.3 步骤 (3) vs (4)），随后 self 路径会用 `caller.SessionID` 拼文件路径。当前三条传输写入 `SessionCallerFn` 的 UUID 都经 `Recorder.SetUUID` 校验（`session.go:781-800`），**今天不可利用**；但这是"靠外部不变量"，请让 self 分支也走 `sessionPath()`。
- **远程/SSH 语义（新发现，建议记录）**：TUI/ACP 的 recoder project 是**启动时的本地 pwd**（`interactive.go:1528`、`acp.go:442`），`switch_env` 切到 SSH 后 `Recorder.Project()` 不变（`session.go:686`，全文件无 `SetProject`）。于是"同 project"在这里意味着**同一台本地机器上的同一个路径**，而模型当前操作的是远端——授权键没有绑定 executor。Web 路径没有这个问题（远端用 `exec.ProjectLabel(taskPwd)`，`web.go:484`）。建议：`env.IsRemote()` 时在日志里记 `exec_label`，并在文档"已知差异"里写一句；是否禁用交给 Lead（本评审**不建议**禁用，因为用户的本地会话本就属于同一用户）。

### 3.3 空 / 缺 project

- caller project 空：`search`/`list` 直接 `ErrNoWorkspace`（§5.3 补充）✅；`read` 仅允许 self ✅。
- 目标缺 project：`start.Project` 空 → 回退索引；两者都空 → 不可跨会话 ✅（与 scope §3.2 一致）。
- 两者都存在但不一致 → `NOT_FOUND` ✅（`session.go:1845-1859` 的注释表明索引可能不含 `Project` 字段，`FindSessionMeta` 会从 key 补，所以"索引 project"总能拿到）。

### 3.4 索引 vs 文件（TOCTOU）

- 授权判定用**文件首行**为准、索引作为缓存交叉校验，方向正确（`session.go:1465-1478` 写入端证明 `session_start` 一定带 `Project`/`UUID`）。
- 残留问题：§5.3 的 `ResolveTarget`（`FindSessionMeta` + 打开首行）与后续流式读取是两次 open → 见 N5。威胁模型是同用户本机，实际风险低，但既然设计把"索引可被重写"当成威胁，就应把同一个句柄写进要求。
- 另：`FindSessionMeta` 对"同一 uuid 被索引在两个 project 下"会**返回 error**（`session.go:1891-1914`），设计没写这条错误怎么映射。请映射为 `ErrSessionUnavailable`（而不是 `ErrIO`），否则就成了一个"猜中 id 才触发"的可观测差异。

### 3.5 存在性 oracle

| 通道 | 结论 |
|---|---|
| 错误码 | 不存在 / 越权 / 索引不一致 / id 不可寻址 → 同一个 `ErrSessionUnavailable` → 同一句 `ERROR[SESSION_VIEW_NOT_FOUND]` ✅ |
| 错误文案 | 不含目标 id、不含 project 路径 ✅（§5.3 明确要求，测试 `TestSessionNotFoundStringsAreExact` 用 `==` 断言）✅ |
| 信息量差异 | `SESSION_VIEW_NO_WORKSPACE` 只依赖 caller 自身状态，不泄漏 target ✅ |
| 计数差异 | `session_list` / `search` 的 `Eligible`/`Scanned` 只统计 caller 自己 project 的会话 ✅ |
| **时序差异** | 越权目标会多一次"打开文件读首行"，未索引目标不会。本机文件操作，差 ~10µs 量级，模型无法观测 → **可接受**，建议在文档里一句话承认 |
| **`FindSessionMeta` 的重复索引 error** | 若映射成 `SESSION_VIEW_IO` 则构成差异 → 见 §3.4，必须归入 `NOT_FOUND` |
| 日志 | `deny ... observed_project=%s` 会写另一 project 的路径到 `debug.log`。本地日志、同用户可读，**可接受**；但注意别把 target 内容写进日志（设计 §5.11 已禁止）✅ |

### 3.6 self 排除

- 搜索：候选阶段剔除 + 扫描前再挡 + self 读取截断到最后一个 user entry 之前，**三道隔离成立**，且 `tool_call` 的 args 确实会落盘（`session.go:910-914`）所以"自己的 query 被自己搜到"结构上不可能 ✅。
- **但**：`session_list` 是否含 self 未定义（N1）。
- **R5 的边角**：`internal/web/chat.go:304-328` 在 client `session_id` 与当前 recorder 不一致时**换新 recorder**，而 `tenv.SessionCallerFn` 闭包的是旧 `trec`（§3.2 的写法）→ 那一刻 self 判定指向旧会话。影响面：（a）不会越权（新 recorder 同 project，`web/chat.go:308` 用 `eng.pwd`）；（b）只是 self 排除失效 → 理论上能搜到"当前会话"（旧 uuid 被排除、新 uuid 未被排除）。这是极窄的防御分支，**不是阻塞**，但 T6 的加固值得做：让闭包读 `eng.recorder` 而不是捕获 `trec`。

### 3.7 写入中/半行

- `writeEntryLocked` 一次 `WriteString(line+"\n")` + `Sync()`（`session.go:1585-1611`）→ 完整行必已落盘，尾部无 `\n` 残片丢弃（`Torn=true`）**成立** ✅。
- 额外好消息：`TruncateAtUserMessage` 用 **原子 rename**（`session.go:1357-1366` `durableReplacePrivateSessionFile`），已打开的读者继续读旧 inode，**不会看到撕裂的重写**；设计的 §5.10 第 2 行说得比实际更糟，可以不改。

### 3.8 符号链接 / 大小写 / 尾斜杠

- 授权键是 project **字符串**，与文件系统路径无关，所以 `~/.jcode` 是否为符号链接不影响授权 ✅。
- 大小写：macOS 默认大小写不敏感，`/Users/x/Work` 与 `/Users/x/work` 指向同一目录但字符串不等 → 误拒。这是 scope §3.2 的选择（fail closed），设计 R3 已承认 ✅。
- `SessionsDir()` 走 `os.UserHomeDir()`（`config/config.go:1342-1348`），E2E 用 `HOME` 覆盖可行 ✅。

### 3.9 会话文件本身能否被伪造

模型可用 `execute` 往 `~/.jcode/sessions/` 写一个"`project` 等于 caller project"的假会话（`session_start` 字段格式见 `session.go:1465-1478`），再用 `session_view` 读回来。但这与 B1 同源（它已经能直接 `cat`），**不构成新增权限**。建议在文档"残余风险"里记一行即可。

---

## 4. 正确性评审

### 4.1 `Entry` 无 seq、用 1-based 解析序数 —— 成立

- `Entry` 定义 `session.go:263-391`，确无 `Seq`/`Parent` 字段 ✅。
- `LoadSession` 用 `strings.Split` + `json.Unmarshal` 失败即 `skipped++`（`session.go:1926-1962`）→ **物理行号 ≠ 解析序数**，设计承认并据此选择"解析序数"，✅。
- **与 `LoadSession` 的语义一致性要求**：`sessionview/parse.go` 必须复刻 `LoadSession` 的"跳过坏行、忽略空行"规则，否则同一文件下 `session_search` 报的 entry 号与别处（回放/Web）不一致。建议单测直接 **对拍**：同一份含坏行的文件，`len(session.LoadSession(id))` 与 `parse.Total` 相等。
- **设计遗漏的一句**：`TruncateAtUserMessage` 会把 cut 之后出现的 append-only entry（`artifact`/`mode_change`/`generation_operation`/`provider_tool_operation`/`session_tool_override`，`session.go:1376-1390`）**上提到 `session_start` 之后**。所以重写后**不只是尾部 seq 漂移，而是整段重排**。设计 R1 只说"偏移"，强度不足，请把"重排"写进去（缓解措施不变）。

### 4.2 `TruncateAtUserMessage` 风险被显著高估

- 唯一调用点是 `internal/web/sessions.go:485`（Web 的"编辑/回退到某条 user 消息"接口），用户主动触发，且同一请求里会同时截断内存历史（`:493-510`）→ 当前 turn 基本被作废。
- `durableReplacePrivateSessionFile` 只被 `TruncateAtUserMessage` 使用（全仓 grep：仅 `session.go:1360`）；`PruneOldToolOutputs`（`session/history.go:64`）只改内存，不动文件。
- 因此正常对话里会话文件**只追加**，seq 稳定；"search 拿到 42 → read 42" 发生在同一 turn 内（秒级），漂移概率极低。
- **结论**：R1 被排成"最大残留风险"是**排序错误**。它应降级，把"B1 的护栏性质"和"B3 的读取保真度"排到前面。缓解措施（头部重述 `Entries a-b of N`）仍然值得保留 —— **建议再加一条极便宜的兜底**：`session_read` 接受 `start` 时，如果该 entry 的 `Timestamp` 与 `session_search` 报告的不一致，模型自己能看出来（把 `Updated:` 与 entry 时间都给出来即可，设计已有）。无需额外机制。

### 4.3 `Entry.Project` 是否只出现在 `session_start` —— **成立**

全文件 `Project:` 赋值只有两处：`session.go:1471`（`session_start`）与 `:1490`（写入索引的 `SessionMeta`）。所有其他 `Entry` 构造都不带 `Project`。设计 §2 与 §5.3 的"首行复检"逻辑因此自洽 ✅。`Entry.UUID` 同理只在 `session_start` 写入。

### 4.4 compact / summary 渲染 —— 成立

`EntryCompact` 写 `Summary`（`session.go:1250-1254`），设计把它作为可搜索文本与可渲染正文，正确 ✅。

### 4.5 渲染表完整性

21 个 `EntryType` 中，`generation_operation`/`provider_tool_operation` 未在 §5.6 的"其余"枚举中列出（N3）；`artifact`/`mode_change` 在"其余"里 ✅；`system_prompt` elided ✅；`session_start` 不单独渲染 ✅。**渲染表与 `session.go:31-54` 需要一一对账**，建议实现时以 `switch` + `default` 抛 `(unrecognized)` 兜底（N10）。

### 4.6 预算数字自洽性

- `max_entry_bytes=4096` × `limit=40` vs `max_output_bytes=16384`：默认组合必然截断（B3）。
- `snippet` 半窗 80 runes × 2 + `…` = 161，与断言一致 ✅。
- `tool_result` 首行 ≤200 runes、`tool_call` args 4000 **runes**（而其它预算都是 **bytes**）—— 单位混用，请在文档里统一标注单位，否则实现容易搞错（`internal/tools/read.go:204-213` 的既有 helper 是 bytes 语义）。

### 4.7 配置

- `MemoryConfig`（`config.go:256-273`）/`MemoryEnabled`（`:349-355`）/`ToolSearchConfig`（`:692-694`）/`ToolSearchEnabled`（`:745-748`）的模式确实可复用 ✅；`json.Unmarshal` 忽略未知字段 ✅（`config.go:1117`）。
- `SessionViewEnabled(nil) == false`（fail closed）与 `缺 section == true` 的组合是正确的产品语义，但**两个"默认"方向相反**容易写错，`TestConfigDefaults` 已覆盖 ✅。
- 配置只在 agent 构建时读取，切换需要重建 agent；`buildAllTools` 在模型/模式切换时会被重新调用（`interactive.go:160` 注释说明每轮重建），所以改配置后的行为取决于是否触发重建 —— 与 `MemoryEnabled` 一致，可接受 ✅。

---

## 5. 注册完整性评审

### 5.1 设计列的 6 个 append 点 —— 全部核对正确

| # | 设计引用 | 实际 | 结论 |
|---|---|---|---|
| 1 | `interactive.go:160` `buildAllTools` | ✅ 函数在 160，`all :=` 在 166 | 正确 |
| 2 | `interactive.go:231` `buildPlanTools` | ✅ | 正确 |
| 3 | `acp.go:597` `allTools` | ✅ | 正确 |
| 4 | `acp.go:685` `planTools` | ✅ | 正确 |
| 5 | `web.go:662` `all :=` | ✅ | 正确 |
| 6 | `web.go:740` `plan :=` | ✅ | 正确 |
| 7 | `tool_catalog.go:24` `commandToolPolicies` / `:161` `directPolicy` / `:78` `allModes` | ✅（`buildCommandToolPlan` 的 unknown-builtin 报错实际在 `:99-102`，设计写 `:102-105`，偏 3 行） | 基本正确 |
| 8 | `runner/approval.go:237-263` `noApprovalNeeded` | ✅ 实际 `:237-263`；`decide` 在 `:283` | 正确 |

### 5.2 设计**没有**遗漏的传输（我逐个查过）

- `internal/tools/subagent.go:561`、`internal/tools/flow_spawn.go:145`、`internal/command/interactive.go:118`（team）、`internal/review/investigate.go:51`、`internal/memory/pipeline/phase2.go:193` —— 都是**独立硬编码列表**，不接收 `buildAllTools` 的输出，因此**不会**出现"模型看到描述却调不到工具"。✅
- Web 的 headless / automation 路径全部经由 `command/web.go` 的 `buildAllTools` 闭包（`:871`、`:955`、`:1110` 三个调用点），一处 append 即全覆盖 ✅。
- `internal/web` 包里的 `Engine` 只是持有者（`web/engine.go:187`），不自己建工具；`internal/web/chat.go` 只换 recorder ✅。
- `agent.NewAgent` / `NewAgentWithToolPlan` 的调用点只有 `command/{interactive,web,acp}.go` + memory pipeline ✅（全仓 grep 验证）。
- `dropInteractiveTools`（`web.go:99-116`）只剔除 `ask_user/generate_image/automation_create|delete/switch_env`，session 工具会保留 —— 符合"只读、无人值守也安全" ✅。
- `ToolExposureHidden` 是**执行边界**而非仅可见性（`agent/tool_plan.go:75-86` 注释），所以 catalog 策略必须给 `allModes`，否则 plan 模式会真的调不到 —— 设计已写 `allModes` ✅。

### 5.3 未覆盖的注册面（低风险，非阻塞）

- `internal/command/acp_session_tools.go` 存在，但内容是 ACP 的 config-option 桩，与会话历史无关 ✅（namesake 陷阱，实现者别误改）。
- 工具名无冲突：`internal/tools` 里现有名字集合不含 `session_*` ✅。
- `catalog` 测试 `TestCommandToolPolicyCoversExpectedBuiltins`（`tool_catalog_test.go:323-334`）强校验"策略 map ⇔ `allCommandBuiltinNames`"，两处必须同时加 ✅（设计已列）。

---

## 6. 产品 / 设计取向评审

### 6.1 三个工具 vs 更少

- `session_list` 与 `session_search` 确有重叠，但覆盖的是两个不同入口（有词 / 无词）。DSH 只有 search 没有 list（`research/cross-session-dsh.md:9.1` 代码地图，五个工具里无 list），codex 有 `list_threads` 且有 search（`research/cross-session-codex.md:41`）。**3 个是两者的并集，可接受**。
- 拒绝 `session_history(action=…)` 有 DSH 先例支撑（`research/cross-session-dsh.md:491`）✅。
- 隐藏的好处：`session_read` 省略 `session_id` 时读自身，顺手解决了"压缩后忘记前文"，这是本设计最实用的一点，建议在 §1 目标里点明（现在只散落在 §5.4 描述里）。

### 6.2 direct vs deferred

- 结论正确。`tool_search` 默认关（`config.go:745-748`）时三条传输都走静态列表（`interactive.go:466-475`、`acp.go:825-838`、`web.go:889-900`），exposure 根本不参与 ✅。
- 但"3 条 schema ~350 tokens"**偏低**：三短两长共约 1 650 字符描述 + 6 个参数描述，实际约 **450–550 tokens**。默认配置下 TUI 已挂 ~23–26 个工具（browser/computer 开启后 ~36），加 3 个是 +8%~13%。结论（可接受）不变，数字要改（R7）。

### 6.3 "不加 system prompt" —— 决定正确，但**理由选错了**

设计给的理由是"改 prompts 要动两套模板与快照测试，风险大于收益"。实际上仓库里有一条**更强、更硬的既有不变量**：

- `internal/prompts/system.md:35-36`：「The function schemas attached to the current model request are the sole source of truth … Do not assume that a tool exists because it appeared in this prompt」；
- `internal/prompts/prompts_test.go:50-67` `TestBasePromptsUseRequestSchemasAsToolSourceOfTruth` 会**拒绝**提示里出现 `# Tools Available`、`- **read**:`、`tool_search` 等静态工具文本。

也就是说，"把工具语义写进 description、不写进 prompt"不是权衡，而是**本仓库已经固化的约定**（且 `automation_create` 那种"路由指引"是例外而非范式）。请把这条证据写进 §5.9 / D2，理由会强得多；同时**不要**加那句 167 字符的 hint —— 它会与 `system.md:35-36` 的精神冲突，而收益（让模型想起来有历史可查）在设计已选择 direct exposure 的前提下并不成立。

### 6.4 默认 `enabled: true`

- 与 scope §3.3 一致 ✅，但必须承认与 DSH 回滚教训（`research/cross-session-dsh.md:495-505`）的冲突（N9）。
- 关键区别确实存在：DSH 是 5 工具 + 一段常驻 prompt，jcode 是 3 工具 + 无 prompt + 一个开关全关。写清楚即可。

### 6.5 输出格式

- `[seq] role · ts` + 正文，可读性好于 JSON；`[n]` 连续无空洞与"坏行不占 seq"一致 ✅。
- `Entries 98-137 of 137 (corrupt lines skipped: 1, last line incomplete)` 头部信息量足，模型可自查 ✅。
- `session_list` 的编号（`1.`/`2.`）与 `session_read` 的 `[seq]` 使用不同括号体系，不会混淆 ✅。

---

## 7. 测试计划评审

### 7.1 做得好的部分

- HOME 铁律（`t.Setenv("HOME", t.TempDir())`）与仓库既有惯例一致（`session/agent_roundtrip_test.go:17`、`tools/browser_tools_test.go:14`）✅。
- `TestUnavailableErrorsIndistinguishable`（同 err、异 Reason）是本设计**最该有**的测试，设计想到了 ✅。
- 用 `go/parser` 做注册守卫：方向对（`acp.go:598-606` 的 400 次跑挂 4 次是有说服力的先例），但断言太弱（N14）。
- 表驱动覆盖：授权矩阵 12 case、搜索语义、UTF-8 片段、去重排序、截断 —— 覆盖面够 ✅。

### 7.2 测试缺口（按重要性）

| # | 缺口 | 建议用例 |
|---|---|---|
| T-1 | **索引来源 id 校验**（B2） | `TestSearchSkipsInvalidIndexUUID`：`../../x`、`a/b`、`""`、`..` 四条索引项 → 全部跳过，且 canary 文件未被读取 |
| T-2 | **长 entry 续读**（B3） | 10 KiB assistant 消息：断言"要么两次调用能读全，要么尾部明确给出续读参数" |
| T-3 | **单条 entry 类型全覆盖** | 对 `session.go:31-54` 的 21 个类型各造一条，断言渲染不 panic、不出现 `Content` 以外的字段（尤其 `Images[].Data`、`OpaqueResponseItems`） |
| T-4 | **与 `LoadSession` 对拍** | 同一含坏行/空行的文件：`len(LoadSession(id)) == parse.Total` |
| T-5 | **并发追加** | 读的同时由另一个 recoder 追加 100 条，断言不 panic、`Torn` 只在尾部、输出 UTF-8 合法 |
| T-6 | **配置关闭在三传输都生效** | 现在只有 `TestSessionViewToolsRegistration`（单元）+ 源码守卫；建议 E2E 的第三行（`enabled:false`）从"可选"升为**必做**，并在 ACP E2E 里断言 events 中无 `session_*` |
| T-7 | **`FindSessionMeta` 重复索引错误映射** | 构造同 uuid 两个 project，断言得到 `NOT_FOUND` 而非 `IO` |
| T-8 | **`ctx` 取消 vs 预算耗尽** | 取消 → 返回 `ctx.Err()`；预算 → 部分结果 + footer |
| T-9 | **self 读取在"当前轮 turn 未落盘"时** | 新会话第一条消息后立刻 self-read，断言不返回空/不 panic |

### 7.3 真模型 E2E 设计的问题（§6.3）

设计方向正确（ACP headless、临时 HOME、正反例、哨兵字符串），但有 4 个具体缺陷：

1. **只拷 `config.json` 不够**。OAuth/账号类 provider 的凭据在 `~/.jcode/provider-auth.json`（`internal/providerauth/store.go:16`；测试里也是这么种的，如 `internal/command/web_unavailable_model_test.go:56`）。E2E 步骤必须同时拷 `provider-auth.json`（若存在），否则在账号型 provider 上会以"未授权"失败，症状与被测功能无关。
2. **正例断言会被 B1 破坏**。模型可能用 `grep`/`execute cat` 拿到 `zebra-42`（甚至更容易），于是"必须出现一次 `session_search`"失败。修法：prompt 限定"只用 `session_*` 工具"，并且**主断言放在持久化会话 JSONL 上**（`tool_call` 行的 `name` 必须是 `session_search`/`session_read`，`session.go:910-914`），终端文本只作辅证。harness 的 `-out events.jsonl`（`agent-eval/harness/main.go:207`）也可作为辅证来源。
3. **`-mode full_access` 静默失败**。harness 对 `SetSessionMode` 失败只记 `setmode_error` 继续跑（`agent-eval/harness/main.go:288-291`），不会报错；好在本功能只读、`full_access` 与 `approval` 都能跑，但文档应说明"模式失败不影响判定"，避免把模式错误当成功能通过。
4. **`script/seed-session` 的契约没定义**。它必须同时写 `~/.jcode/sessions/<uuid>.json`（首行 `session_start`，`project` 与 `uuid` 必须**逐字**等于 `$WS`/uuid）**和** `~/.jcode/sessions/session.json`（`{"sessions":{"<WS>":[meta…]}}`），否则 `ListSessions` 找不到、整个 E2E 无效。建议在 §6.3 把种子文件的字段清单写死，并把"索引必须一起种"写成验收点。

---

## 8. 事实核查表

> 说明：`✅` = 与代码一致；`≈` = 结论正确但行号/措辞有偏差；`✗` = 与代码不符（仅 1 条，见 B1）。

### 8.1 架构与数据面

| # | 设计声称 | 实际代码证据 | 核实 |
|---|---|---|---|
| 1 | 会话是 JSONL、`<uuid>.json`、坏行跳过 | `session.go:263`（`Entry`）、`:1482`（写文件）、`:1926-1962`（`LoadSession` 跳过坏行） | ✅ |
| 2 | `Entry.Timestamp` 为 RFC3339、无 seq/parent | `session.go:277`；`:1585-1589`（`time.Now().Format(time.RFC3339)`）；全仓 `parent_uuid\|ParentSession\|forked_from` 0 命中 | ✅ |
| 3 | tool_call/tool_result 是独立行、大输出 head+tail 截断 | `session.go:34-35`（类型常量）、`:910-928`（`RecordToolCall`/`RecordToolResult` 注释） | ✅ |
| 4 | compact 摘要写在 `Entry.Summary` | `session.go:326-328`、`:1250-1254` | ✅ |
| 5 | `SessionMeta` 有 UpdatedAt/Archived/AutomationID、无父子 | `session.go:394-431` | ✅ |
| 6 | 索引按 project 分组、写有 `indexMu`+原子 rename、读无锁 | `session.go:452-454`、`:1623`、`:1626-1666` | ✅ |
| 7 | `ValidateSessionID` 拒绝 `/`、`\`、`..`、空、`.` | `session.go:761-775` | ✅ |
| 8 | `Recorder.Project()` 创建后不可变 | `session.go:686`；全文件无 `SetProject` | ✅ |
| 9 | 写侧单行追加 + fsync | `session.go:1585-1611` | ✅ |
| 10 | `TruncateAtUserMessage` 重写文件 | `session.go:1275-1367`；唯一调用 `internal/web/sessions.go:485` | ✅（补充：会把 cut 之后的 append-only entry **上提**，见 §4.1） |
| 11 | `session_start` 写 Project/Provider/Model/Timestamp | `session.go:1465-1478` | ✅ |
| 12 | teammate 转录路径 `{leader}/subagents/agent-{id}.jsonl`、uuid=`leader/agent`、project=`agentID`、不进索引 | `session.go:1406-1418`、`:1436-1445`、`:1486-1497`（`if r.agentID == ""` 才写索引） | ✅ |
| 13 | `Entry.Project` 只出现在 `session_start` | 全文件仅 `:1471`、`:1490` 两处 `Project:` 赋值 | ✅ |
| 14 | `SessionsDir` 在 `config.go:1352` | **实际 `:1342-1348`；`:1352` 是 `SessionsIndexPath`** | ≈（偏 10 行） |
| 15 | `SessionMeta.Project` 可能缺省、`FindSessionMeta` 从 key 补齐 | `session.go:1845-1859` 注释、`:1909` | ✅ |

### 8.2 身份与工具集

| # | 设计声称 | 实际代码证据 | 核实 |
|---|---|---|---|
| 16 | `SessionIDFn` 定义在三处接好 | `tools/env.go:46-48`；`interactive.go:1532-1534`；`acp.go:539-541`；`web.go:508-510` | ✅ |
| 17 | `SessionIDFn` 目前只被 automation_create / memory_note 使用 | `tools/automation_tool.go:88-89`、`tools/memory_note.go:93-94`（其余为 deps 注入） | ✅ |
| 18 | `CloneForSubagent` 不复制 `SessionIDFn` | `tools/env.go:156-170` 白名单式字面量 | ✅ |
| 19 | teammate Env 由 `EnvFactory` 新建 | `interactive.go:1676-1678` → `tools.NewEnv` | ✅ |
| 20 | subagent 工具集不含会话工具 | `tools/subagent.go:561-605` | ✅ |
| 21 | team 工具集不含会话工具 | `interactive.go:118-152` | ✅ |
| 22 | flow child = `CloneForSubagent` + 硬编码列表 | `tools/flow_spawn.go:70`、`:145-161` | ✅（设计未列，但行为正确） |
| 23 | review/approval-reviewer 不注册 | `internal/review/investigate.go:51-56` | ✅（设计未列，但行为正确） |

### 8.3 注册 / 审批 / 暴露

| # | 设计声称 | 实际代码证据 | 核实 |
|---|---|---|---|
| 24 | 四个（实为六个）注册点行号 | `interactive.go:160`/`:231`、`acp.go:597`/`:685`、`web.go:662`/`:740` | ✅ |
| 25 | 未知 builtin 会让 tool plan 构建失败 | `tool_catalog.go:99-102`（`unknown builtin tool %q`）；仅当 `config.ToolSearchEnabled` 时才走该路径（`interactive.go:481`、`acp.go:840`、`web.go:902/967`） | ≈（行号偏 3；结论正确） |
| 26 | `directPolicy`/`allModes` 位置 | `tool_catalog.go:161-166`、`:77-78` | ✅ |
| 27 | catalog 测试名单需同步 | `tool_catalog_test.go:24-35`（`allCommandBuiltinNames`）、`:37-84`（6 个矩阵 case）、`:323-334`（策略 map ⇔ 名单强校验） | ✅ |
| 28 | 只读免审批白名单 | `runner/approval.go:237-263`（`noApprovalNeeded`）、`:283-297`（`decide` 早退） | ✅ |
| 29 | tool 错误折叠为 `Tool execution failed: <err>` | `agent/middleware.go:130-146`（含"格式是 load-bearing"的注释） | ✅ |
| 30 | `ToolError`/`toolErrf` | `tools/errors.go:65-89`；注意 `Code` **渲染时不出现**（`:55-64` 注释），所以 `ERROR[...]` 必须写进 `Err` 文本 —— 设计 §5.3 已如此要求 | ✅ |
| 31 | `tool_search` 默认关、关闭时走静态列表 | `config.go:745-748`；`interactive.go:466-475`、`acp.go:825-838`、`web.go:889-900` | ✅ |
| 32 | deferred 需要 `select:` 发现轮 | `agent/toolsearch_agent.go:15-19`、`:52-63` | ✅ |
| 33 | Exposure/descriptor 语义 | `agent/tool_plan.go:23-48`、`:123-161`；`Hidden` 是执行边界见 `:75-86` | ✅ |
| 34 | 配置加载忽略未知字段 | `config.go:1117`（`json.Unmarshal`） | ✅ |
| 35 | `*bool` + helper 模式可复用 | `config.go:256-273`、`:349-355`、`:692-694`、`:745-748` | ✅ |
| 36 | 漏注册先例（ACP `load_skill`） | `acp.go:598-606` 注释（400 次里 4 次跑挂） | ✅ |
| 37 | schema 用 `NewParamsOneOfByParams`、按 rune 截断可复用 | `tools/read.go:44-60`、`:204-213`、`goal.go:374-379` | ✅ |
| 38 | Web 只有 list/get 会话 API | `web/server.go:467-468` | ✅ |
| 39 | ACP harness 驱动链 | `agent-eval/harness/main.go:240-300`；`-mode` 默认 `full_access`（`:209`） | ✅ |

### 8.4 安全叙事

| # | 设计声称 | 实际代码证据 | 核实 |
|---|---|---|---|
| 40 | **`~/.jcode/sessions` 不在任何工具可达范围内** | `grep.go:21-23` + `approval.go:240`（免审批、任意 path）；`read.go:84`+`env.go:141-143`（绝对路径不限制）；`execute.go:143`+`shell_policy.go:8`（`cat` 免审批） | **✗ 错误 —— B1** |
| 41 | R6「模型本就能 `read` 整个工作目录」 | 同上，实际可达范围**超出**工作目录 | ✗（B1 的一部分，结论可接受但理由错） |
| 42 | 索引是缓存、以文件为准复检 | `session.go:1845-1859`（索引可能无 Project 字段）、`:1891-1914` | ✅ |
| 43 | 子 agent/teammate 若误注册则 fail closed | 见 #18–#21 | ✅ |
| 44 | 无 `project`/`path`/`workspace` 入参 | 三份 schema 原文（§5.4）确无 | ✅ |

### 8.5 研究文档引用

| # | 设计引用的外部事实 | 核对 | 结论 |
|---|---|---|---|
| 45 | DSH 否掉"单工具 + action 选择器"（`research/cross-session-dsh.md:491`） | 引用行正是该表项 | ✅ |
| 46 | DSH 索引层是为 live+persisted 双源长的（`…:716-727`） | `:716-727` 讲的是"输出形态/默认不挂/时间戳"，"索引层"的论证在 `:720-726` 的"不要抄"表 | ≈（引错段） |
| 47 | DSH 是 5 条 schema + 整段 prompt，成本才是问题（`…:243`） | `:243` 是 prompt 原文，成本句在 `:246` | ≈（偏 3 行） |
| 48 | DSH 单闸门错误处理（`…:389-403`） | 正是 `serviceBoundary.call` 一节 | ✅ |
| 49 | codex 有模型侧 `list_threads`/`read_thread` | `research/cross-session-codex.md:41` | ✅（设计正文未直接引用，但取舍依据成立） |

---

## 9. 安全发现清单（按严重度）

| ID | 严重度 | 发现 | 处理 |
|---|---|---|---|
| S1 | **高（叙事层面）** | 授权边界不是安全边界：`grep`（免审批、任意 path）、`execute cat`（免审批）、`read`（绝对路径）今天就能读 `~/.jcode/sessions` 全部内容 | **B1**：改文档与 scope 口径；测试以 tool_call 审计为主证据 |
| S2 | **中** | 搜索/列表的候选 id 来自索引且未校验 → 索引污染可导致会话目录外读取/枚举 | **B2**：统一 `sessionPath()` + `ValidateSessionID` |
| S3 | 低 | self 分支早于 `ValidateSessionID`，依赖"三条传输写入的 UUID 都经 SetUUID 校验"这一外部不变量 | 并入 B2 修复；N7 之外的 1 行 |
| S4 | 低 | 授权检查与读取不是同一个文件句柄（TOCTOU） | N5 |
| S5 | 低 | `FindSessionMeta` 的"重复索引"错误若映射为 `IO` 会形成存在性 oracle | §3.4 |
| S6 | 低 | 远端/SSH 会话的授权键是本地 pwd，与当前 executor 不绑定 | §3.2，记录即可 |
| S7 | 信息 | `deny` 日志写另一 project 的绝对路径到本地 `debug.log` | 可接受（同用户可读） |
| S8 | 信息 | 模型可伪造 `session_start` 文件 | 与 S1 同源，不构成新增权限 |

**没有发现的**（明确记录，避免下一轮重复评审）：子 agent/teammate 身份继承漏洞（不存在）、模型可控 workspace 参数（不存在）、越权与不存在文案差异（不存在）、self 自激（结构上不可能）、跨 process 锁竞争（只读不引入）。

---

## 10. 实施者 checklist

**开写前（决定 / 文档）**

- [ ] B1：改写 §1 与 R6 的威胁模型表述；§6.3 反例加"禁止用 shell/文件工具"约束；向 Lead 提 scope §6 第 2 条措辞修订
- [ ] B3：选定方案 A（首条 entry 吃满调用预算）或 B（`entry_offset`），并把 `limit` 默认值调到 10；同步改 §5.4 描述与 §5.6 预算表
- [ ] N2：补齐 `ListResult` 定义
- [ ] N9：补一句"默认 true vs DSH 回滚教训"的取舍说明
- [ ] §5.9/D2 的理由改成引用 `internal/prompts/system.md:35-36` + `prompts_test.go:50-67`

**服务层（`internal/sessionview`）**

- [ ] B2：唯一路径构造函数 `sessionPath(id)`（内含 `ValidateSessionID`），search/list/lineage/self 全部走它
- [ ] N5：`ResolveTarget` 返回**已打开的 `*os.File`**（或 `fstat`+`SameFile` 校验），读取复用同一句柄
- [ ] §3.4：`FindSessionMeta` 的 error（含重复索引）**一律**映射 `ErrSessionUnavailable`
- [ ] N7：预算超时与 `ctx` 取消区分；超时返回部分结果
- [ ] §4.1：`parse.go` 的坏行/空行规则与 `LoadSession` **逐字一致**，并加对拍测试
- [ ] N3/N4/N10：渲染层显式枚举 21 个 `EntryType`，`default` 输出 `(unrecognized)`；禁止渲染 `Images`/`OpaqueResponseItems`/`Operation*` 字段
- [ ] B3：按选定方案实现单条 entry 预算；`… [entry truncated]` 与续读参数必须同时可用

**工具层（`internal/tools`）**

- [ ] N6：`SessionCallerFn` 闭包对 `rec == nil` 零值兜底（三条传输的既有 `if rec != nil` 保护要保留）
- [ ] 三个 `Info()` 的 `Desc` 与设计逐字一致；参数集合 ⇔ `required` 完全一致（`TestSessionToolsSchemas`）
- [ ] 错误文案三层：`ERROR[...]:`（在 `Err`）+ `Hint:`（`ToolError.Hint`）——注意 `Code` 字段不会渲染（`tools/errors.go:55-64`）
- [ ] 所有截断切在 rune 边界并断言 `utf8.ValidString`

**注册**

- [ ] `internal/command/session_view_tools.go` 统一构造 + `enabled` 门
- [ ] 6 个 append 点（interactive ×2、acp ×2、web ×2）
- [ ] `tool_catalog.go` 3 条 `directPolicy("session.history", allModes, "read")`
- [ ] `runner/approval.go` 3 个名字进 `noApprovalNeeded`
- [ ] `tool_catalog_test.go`：`allCommandBuiltinNames` + 6 个矩阵 case（策略 map 与名单由 `:323` 强校验，两处同改）
- [ ] **不要**改 `subagent.go:561`、`interactive.go:118`、`flow_spawn.go:145`、`review/investigate.go:51`

**测试**

- [ ] T-1 ~ T-9（§7.2）
- [ ] 注册守卫从"文本出现"升级为"构建后工具名集合包含"（N14）
- [ ] E2E：拷 `config.json` **和** `provider-auth.json`；种子同时写 JSONL 与索引；主断言看持久化 `tool_call` 行；正例 prompt 禁止 shell 绕过；`enabled:false` 用例升为必做

**收尾**

- [ ] `go build ./...`、`go test ./...`、`make lint`
- [ ] `internal-doc/cross-session-view-test-report.md` 逐条对 scope §6 记录通过/不通过（含 B1 的口径修订）

---

## 11. 对 Lead 的 scope 变更建议

**唯一一条（B1 的必然结果）**：`cross-session-view-scope.md` §6 验收第 2 条

> 现文：「跨 project 读取被拒绝，且与"不存在"同文案；日志里有真实原因。」
> 建议：「**新增的 session 工具**在 cross-project 读取上 fail closed，且与"不存在"同文案；日志里有真实原因。经既有 `grep`/`read`/`execute` 读取 `~/.jcode/sessions` 不属本特性范围（现已复核：`grep` 免审批且 path 不受限），如需真正隔离应单独立项给 `read`/`grep` 加 `~/.jcode` 拒读并收紧 shell allowlist。」

理由：不改这句，PR 描述与测试报告会承诺一个不存在的安全属性；改了这句，验收标准仍然可执行、可自动化。

---

## 12. Delta 复核（architect 追加，rev3 修订验证）

> 本节由架构师在 Lead 下发 delta 结论后追加，只记录「rev2 的新阻塞项与 nit 如何落地」，不代表评审者立场。
> 修订对象：`internal-doc/cross-session-view-design.md`（rev3，仍为 700 行，未改生产代码）。

| 项 | 落地位置（设计文档） | 验证方式 |
|---|---|---|
| NEW-B1 空 id = self | §5.3 步骤 (2)(3)：`effective := id`，空则取 `caller.SessionID`，随后走**完全相同**的 `sessionPath(effective)` 校验/读取路径；空 id 永不视为非法 | §6.1 授权矩阵新增「空 id（= self，必须允许读自己）」 |
| NEW-B2(i) content-only 全预算 + tool IO 无续读 | §5.6 规则 1–4、渲染表、§5.4 description：仅 `user`/`assistant`/`compact` 可吃满 16 KiB 并可续读；`tool_call`/`tool_result`/elided/unrecognized 保持类型上限且只有 `… [entry truncated: N more bytes]` | §6.1 `TestReadLongEntryContinuation` 双向断言 |
| NEW-B2(ii) `entry_offset>0 ⇒ start>0` | §5.5 校验（`entry_offset requires an explicit start`）、§5.4 schema description、§5.2 `ReadRequest` 注释 | 同上测试断言 `INVALID_RANGE` |
| NEW-B2(iii) 示例修正 | §5.6 模板：24 KiB user → 渲染 16384 body bytes、剩 8192、`entry_offset=16384`（ASCII bytes==runes）；tool_call/tool_result 示例改为 plain 截断 | 人工核对字节/rune 算术 |
| NEW-B2(iv) 类型化截断断言 | §6.1 测试行 | user/assistant/compact 有续读 footer，tool IO 无 |
| Nit 1 `subagent_async` | §5.6 渲染表（`session.go:42`/`:1205`） | 21 类型枚举测试覆盖 |
| Nit 2 `FindSessionMeta` 映射 | §5.3 步骤 (5)：索引读取/反序列化/IO → `SESSION_VIEW_IO`；重复索引（歧义）→ `NOT_FOUND`；§5.12 拆成两行 | 授权矩阵测试新增 case |
| Nit 3 关闭态 E2E | §6.3 关闭行：持久化 JSONL 无 `session_*` + 单元守卫断言 absent（`acp.go:467-471` 的 `NewSessionResponse` 不暴露工具清单） | 手动 E2E + 单测 |
| Nit 4 反例错误字段 | §6.3 反例行：在持久化 `tool_result.error` 中查 `ERROR[SESSION_VIEW_NOT_FOUND]`（`session.go:925-941` 分写 err/output） | 手动 E2E + JSONL 检查 |
| Nit 5 N14 回退 | §5.9 注释 + §6.2 测试行：优先提取；若 web 大闭包（`web.go:631-760`）不宜重构，允许源码级调用断言 + 每传输最小 builder smoke，append 顺序/条件分支必须行为等价 | 既有 web/acp 测试须全绿 |

**结论**：两项新阻塞项与 5 个 nit 已全部落到段落与测试断言，无遗留引用悬空；设计文档与「事实核查」表保持自洽。
