# DSH 跨会话查看工具（model-facing session query）实现剖析

> 状态：调研文档。参考仓库只读：`/Users/jack/workpath/opensource/deepseek-harness`（下称 DSH），所有 `file:line` 相对该仓库根。未运行 `pnpm install/build/test`，全部为静态阅读。目标：把 DSH 在「让模型看历史会话」上的取舍映射到 jcode（Go 单二进制 + JSONL 会话）。

---

## 1. 现状结论

DSH 让模型**只读地**看历史会话，只经五个扁平、无游标的工具：`session_search`（跨会话全文检索，按会话分组，每会话只回最强命中）、`session_event_search`（单会话内事件检索，目标是自己时自动截到本次 step 之前）、`session_trace`（祖先链 + 后代树）、`session_event_trace`（位置替换与被引用源事件关系）、`session_event_read`（一个事件的完整未删减 JSON + 可选邻居摘要）。核心赌注三条：(a) **权威来自调用者而非模型**——workspace 身份从 `ToolExecution.exec.agent` 推出，模型没有任何参数可以指定 workspace，跨会话访问要求目标会话 `cwd` 与调用者**字符串精确相等**，无 `cwd` 只能看自己（`packages/experimental/tool-session-query/src/workspace-access.ts:56-99`，`README.md:53`）；(b) **游标/偏移/页大小/模型可调上限一律不进模型**——单次调用内部把 provider 的 generation-bound 游标翻完，封顶到部署的 `maxSearchResults`（默认 100），封顶就提示「缩小查询」（`src/operations.ts:244-277`，`.agents/notes/archived/feature/2026-07-24-model-facing-session-query-tools.md:32`）；(c) **一个模型边界消毒器 + 不做第二套截断**——所有对可信服务 `ctx.sessionQuery` 的调用都过一次 `serviceBoundary`，取消原样透传、诊断只进内部日志、不确定的一律塌缩成 `SESSION_QUERY_TOOL_FAILED`；渲染结果保持完整，超长交给部署侧的通用 `tools/post-execute` spill policy，本包既不 import spill 也不自建截断（`src/service-boundary.ts:99-141`，`README.md:57,76`）。整包 **opt-in**：出厂 TUI/Web/headless 都不挂，默认请求里既没有这五条 schema 也没有那段指导语（`.agents/notes/archived/feature/2026-08-02-session-search-not-shipped-default.md:14-16`）。

---

## 2. 能力概览

五条工具全部只读，全部返回**一段纯文本**（`src/index.ts:46-49`：`output.schema = {type:'string'}`，`render` 把字符串装成一个 text block），没有任何结构化字段抵达模型。

| 工具 | 目的 | 返回什么 | 刻意不返回什么 |
|---|---|---|---|
| `session_search` | 在调用者 workspace 的历史会话里做字面全文检索，按会话分组（`src/index.ts:67`，`docs/tool-catalog.md:1954`） | 每条 6 行（前置一空行）：`N. Session <id> — <title>` / `Created:` / `Parent:`（`root` / 父 id / `[outside workspace]`）/ `Availability:`（`live`、`persisted`、`unavailable`）/ `Best match: seq \| type \| surface \| ISO 时间` / `Snippet:`（`src/presentation.ts:48-79`） | 游标/偏移/页大小/模型可调 limit（`README.md:53`）；provider 数值 score（`session-query-sqlite/README.md:61`）；**调用者自己的会话**（`src/operations.ts:101`，`README.md:47`）；未授权父会话的真实 id（`presentation.ts:56-60`） |
| `session_event_search` | 在一条已授权会话内检索事件（`src/index.ts:77`） | 首行 `Session <id> — <title>`，再 `Event search results (N):` + 每行 `序号. seq \| type \| surface \| 时间` + `Snippet:`；空结果也带目标标题（`presentation.ts:85-106`） | 同上无游标；不返回命中总数（只给本页条数）；不返回事件正文（要配 `session_event_read`） |
| `session_trace` | 读一条会话的授权范围谱系：完整祖先链（近→远）+ 递归后代树（`src/index.ts:87`） | 头部 4 行（id/标题/Created/Availability）+ `Ancestors (nearest first):` 列表 + `Descendants:` 缩进树（`presentation.ts:108-147`） | **边界背后的任何隐藏 session id**：祖先断点 `- [outside workspace boundary]`，后代断点 `- [outside workspace subtree]`（`presentation.ts:126,141`）；`trace.unresolvedParentId` 从不渲染；无分页、无字符预算（note `2026-07-24:34`） |
| `session_event_trace` | 一个事件的直接位置替换关系 + 被引用源事件关系（`src/index.ts:97`） | 6 行：`Target:` / `Replaced by:` / `Replacement chain:` / `Events replaced by target:` / `Events cited directly as sources:` / `Direct derived events:`，空列表渲染 `none`（`presentation.ts:149-163,203-205`） | 邻居正文、谱系、任何分页；关系只给「直接链接」（仅 `replacementChain` 沿立即替换者走到底，`docs/subsystems/session-query.md:303`） |
| `session_event_read` | 精确读一个事件 + 可选前后原始事件摘要窗（`src/index.ts:110`） | 目标事件**完整未删减 JSON**（` ```json ` fenced，`JSON.stringify(target,null,2)`）+ `Before:`/`After:` 邻居清单（`- seq \| type \| ISO` + 语义文本，无文本时 `\| (no semantic text)`）（`presentation.ts:165-194`） | 邻居的完整 JSON（只给语义摘要）；整段日志；窗口大小受服务端 `readWindowMax`（默认 50）限制（`session-query/src/config.ts:7`），`before`/`after` 缺省即只看目标（`src/index.ts:113-115`） |

**五条共同的「刻意不返回」**：① 无 continuation 状态（cursor/offset/page size/limit 全不在 schema，`docs/tool-catalog.md:2045-2058` 里 `session_trace` 连 `required` 都没有）——理由不是省 token 而是**游标必然失效**：记录一次 tool result 或开始下一步都会推进 session/global generation（note `2026-07-24:44`）；② 无 workspace 路径；③ 无边界背后的 id；④ 无 provider 诊断；⑤ 无结果总数（`Result cap reached` 只说「还有更多」，`presentation.ts:75-78`）。

---

## 3. 工具 schema 原文

以下 JSON 来自 `docs/tool-catalog.md`，由 `scripts/gen-tool-catalog.ts` **真实启动插件后**读 `ctx.tools.schemas()` 生成（`docs/tool-catalog.md:8`），即模型实际收到的 `parameters`。逐字照抄。

**注册时声明的名字与描述**（`src/index.ts:65-121`；`timeoutMs`/`isConcurrencySafe`/`presentCall` 三个元数据**永不进模型**，`schemas()` 只白名单 `name`/`description`/`parameters`，`packages/core/tools/src/index.ts:260-267,1290-1302`）：

| 工具 | `description`（原文） | `timeoutMs` | `isConcurrencySafe` |
|---|---|---|---|
| `session_search` | `Search prior sessions in the caller workspace and return the strongest matching event from each session.` | `searchTimeoutMs`（默认 30000） | 未声明 → exclusive |
| `session_event_search` | `Search prior events in one authorized session; the current session excludes the step performing this call.` | `searchTimeoutMs` | 未声明 → exclusive |
| `session_trace` | `Read the authorized session lineage around one session, including complete visible ancestor and descendant relationships.` | 无 | `() => true` |
| `session_event_trace` | `Read every direct replacement and relationship to a cited source event for one event in an authorized session.` | 无 | `() => true` |
| `session_event_read` | `Read one full unabridged event and optional neighboring raw-event summaries from an authorized session.` | 无 | `() => true` |

### 3.1 `session_search`（`docs/tool-catalog.md:1956-2040`）

```json
{
  "type": "object",
  "properties": {
    "query": {
      "type": "string",
      "description": "Literal full-text query over prior session history."
    },
    "session_ids": {
      "type": "array",
      "description": "Optional session ids to include.",
      "items": {
        "type": "string"
      }
    },
    "created_at_from": {
      "type": "string",
      "description": "Inclusive timezone-qualified ISO 8601 creation-time lower bound."
    },
    "created_at_to": {
      "type": "string",
      "description": "Inclusive timezone-qualified ISO 8601 creation-time upper bound."
    },
    "parent_session_ids": {
      "type": "array",
      "description": "Optional direct parent session ids.",
      "items": {
        "type": "string"
      }
    },
    "include_root_sessions": {
      "type": "boolean",
      "description": "Include sessions with no parent in the parent filter."
    },
    "availability": {
      "type": "array",
      "description": "Require at least one selected source availability.",
      "items": {
        "type": "string",
        "enum": ["live", "persisted"]
      }
    },
    "event_seq_from": {
      "type": "integer",
      "description": "Inclusive event sequence lower bound."
    },
    "event_seq_to": {
      "type": "integer",
      "description": "Inclusive event sequence upper bound."
    },
    "event_time_from": {
      "type": "string",
      "description": "Inclusive timezone-qualified ISO 8601 event-time lower bound."
    },
    "event_time_to": {
      "type": "string",
      "description": "Inclusive timezone-qualified ISO 8601 event-time upper bound."
    },
    "event_types": {
      "type": "array",
      "description": "Event types to include.",
      "items": {
        "type": "string"
      }
    },
    "event_surfaces": {
      "type": "array",
      "description": "Event surfaces to include.",
      "items": {
        "type": "string",
        "enum": ["current", "shadowed", "log-only"]
      }
    }
  },
  "required": ["query"]
}
```

### 3.2 `session_event_search`（`docs/tool-catalog.md:1871-1922`）

```json
{
  "type": "object",
  "properties": {
    "session_id": {
      "type": "string",
      "description": "Target session id. Omit for the current session."
    },
    "query": {
      "type": "string",
      "description": "Literal full-text query over the target session."
    },
    "seq_from": {
      "type": "integer",
      "description": "Inclusive event sequence lower bound."
    },
    "seq_to": {
      "type": "integer",
      "description": "Inclusive event sequence upper bound."
    },
    "time_from": {
      "type": "string",
      "description": "Inclusive timezone-qualified ISO 8601 event-time lower bound."
    },
    "time_to": {
      "type": "string",
      "description": "Inclusive timezone-qualified ISO 8601 event-time upper bound."
    },
    "event_types": {
      "type": "array",
      "description": "Event types to include.",
      "items": {
        "type": "string"
      }
    },
    "surfaces": {
      "type": "array",
      "description": "Event surfaces to include.",
      "items": {
        "type": "string",
        "enum": ["current", "shadowed", "log-only"]
      }
    }
  },
  "required": ["query"]
}
```

### 3.3 `session_trace`（`docs/tool-catalog.md:2049-2058`）

```json
{
  "type": "object",
  "properties": {
    "session_id": {
      "type": "string",
      "description": "Target session id. Omit for the current session."
    }
  }
}
```

（无 `required`：唯一参数可省略，省略即「当前会话」。）

### 3.4 `session_event_trace`（`docs/tool-catalog.md:1931-1947`）

```json
{
  "type": "object",
  "properties": {
    "session_id": {
      "type": "string",
      "description": "Target session id. Omit for the current session."
    },
    "seq": {
      "type": "integer",
      "description": "Target event sequence number."
    }
  },
  "required": ["seq"]
}
```

### 3.5 `session_event_read`（`docs/tool-catalog.md:1838-1862`）

```json
{
  "type": "object",
  "properties": {
    "session_id": {
      "type": "string",
      "description": "Target session id. Omit for the current session."
    },
    "seq": {
      "type": "integer",
      "description": "Target event sequence number."
    },
    "before": {
      "type": "integer",
      "description": "Number of preceding raw events to summarize. Omit for none."
    },
    "after": {
      "type": "integer",
      "description": "Number of following raw events to summarize. Omit for none."
    }
  },
  "required": ["seq"]
}
```

### 3.6 每次请求都会注入的固定指导语

模型看到的原文（`snapshots/session/session-query-spill/system-prompt.expected.md:26`；源常量 `PROMPT_TEXT` 由三段字符串拼接，`src/index.ts:51-54`）：

```text
Use session_search to find relevant work from prior sessions, or session_event_search to search earlier events in one session. Search results are cursor-free and workspace-scoped. Follow a useful hit with session_trace, session_event_trace, or session_event_read when you need lineage, relationships, or exact data.
```

注入方式不是拼接进别的段落，而是 `systemPrompt.section()`（`src/index.ts:59-63`）：`{ name: 'tool:session-query', order: ctx.systemPrompt.getSectionOrder('TOOL_SESSION_QUERY'), text: PROMPT_TEXT }`；`TOOL_SESSION_QUERY: 2300` 定义在 `packages/core/system-prompt/src/index.ts:150`（工具指导区靠后）。README 写明成本：每请求固定多一个 section + 五条 schema，前缀稳定（`README.md:127-133,141-147`）。

---

## 4. 授权模型

### 4.1 调用者身份从哪来

`workspaceAccess.callerOf(exec, ctx)`（`src/workspace-access.ts:56-69`）取 `exec.agent`，没有就抛 `HarnessError('session query tools require an agent-bound caller', 'SESSION_QUERY_TOOL_MISSING_AGENT')`；返回 `{ id: agent.session.id, header: agent.session.header, boundary: ctx.sessionProjections.stateOf(agent.session,'turnBoundary') }`。`exec.agent` 的定义是 `/** The agent on whose behalf the call runs (set by the agent loop). */`（`packages/core/tools/src/index.ts:336-340`）。即：**身份、workspace、当前 step 边界三者同源，全部来自 agent loop 绑定的 Session 对象，模型零参与**（note `2026-07-24:24`）；`exec.signal` 是调用者取消信号（`index.ts:350-352`），后续每个 `serviceBoundary.call` 都拿它（`src/operations.ts:94-100`）。

### 4.2 workspace 相等性 = 字符串精确相等（`workspace-access.ts:96-99`）

```ts
function headerAuthorized(header: SessionHeader, caller: Caller): boolean {
  if (header.id === caller.id) return header.cwd === caller.header.cwd
  return caller.header.cwd !== undefined && header.cwd === caller.header.cwd
}
```

三条推论：① **无 `cwd` 只能看自己**（`:98`；`authorizeSessionIds` 在 `cwd === undefined` 时直接返回只可能含 self 的集合，`:119-122`；`session_search` 更早直接拒绝，`operations.ts:62-68`）；② **连自己也要校验**——self 分支仍要求观测到的 `cwd` 与调用者一致，同一 id 的 header 若丢了 `cwd`，self 也拒（fail closed）；③ **不做符号链接归一化**，README 把这条写成已知限制兼 Dev Note 未决方向（`README.md:171,182-185`）。

### 4.3 三种授权动作

**(a) 直接目标预检 `authorizeTarget`**（`workspace-access.ts:75-90`）：self 直接放行；否则 `cwd` 缺失 → `unauthorizedTarget()`；否则 `filterSessions([{kind:'id',values:[target]},{kind:'cwd',values:[cwd]}])`，**返回条数必须恰为 1**，否则 `unauthorizedTarget()`。**目标不存在与目标在别的 workspace 走同一条路径**（0 条 → 同一个错误），模型侧不可区分；单测 `tests/tool-session-query.spec.ts:594-608`。

**(b) 每个结果再校验一次（TOCTOU 防护）`assertObservedTargetAuthorized`**（`workspace-access.ts:101-109`）：`observed.id !== target || !headerAuthorized(observed, caller)` → 同一个 `unauthorizedTarget()`。覆盖每一种返回 payload 的观测：`traceSession` 的目标 header（`operations.ts:181`）、每一页 `searchEvents` 的 `page.session`（`:163`）、`traceEvent` 的 `trace.session`（`:215`）、`readEvent` 的 `window.session`（`:239`）、以及批量标题的每个成功项（`workspace-access.ts:151`）。意图写在 note：防止「预检通过之后、使用之前」发生的 live/persisted 目标替换跨越 workspace 边界（note `2026-07-24:24`）；测试 `spec.ts:1152-1248`。

**(c) 请求侧 id 集合去重 + 逐条授权 `authorizeSessionIds`**（`workspace-access.ts:111-135`）：`[...new Set(ids)]` 去重（入口 `materializeParentSessionIds` 也做一次，`src/input.ts:103-107`）→ 只对「非 self」发一次 `filterSessions([{id},{cwd}])` → 返回项**必须属于请求集合且逐条通过 `recordAuthorized`**，否则丢弃（`:128-133`，服务端多返回的记录不会凭空获得授权）→ self 无条件进集合（`:119`，仍受 §4.2 约束）。用途有三：① `parent_session_ids` 过滤——先授权父 id 再拼 `{kind:'parent', values:[...授权父, null?]}`，`include_root_sessions:true` 独立把 `null`（根标记）OR 进同一 clause；**全部未授权且不要 root 就直接返回空结果文案、连 search 都不发**（`operations.ts:79-89` + `presentation.ts:81-83`），这就是「缺失 vs 跨 workspace 猜测不可区分」的实现（测试 `spec.ts:621-648`、去重只发授权的 `spec.ts:650-679`）；② `session_search` 渲染 Parent 列前批量再授权，未授权显示 `[outside workspace]`（`operations.ts:104-107`）；③ 谱系——祖先一旦未授权就 `break` 并置 `ancestorBoundary`（`operations.ts:183-191`），后代用迭代式 `authorizeDescendants` 把**未授权子树整体**换成 `null`（`workspace-access.ts:175-205`，`:187-190` 直接 `continue` 不下钻）。

### 4.4 「调用者缺席 / 目标越界」的四种错误语义

| 场景 | 代码 | 文案（原文） | 出处 |
|---|---|---|---|
| 没有 agent 绑定 | `SESSION_QUERY_TOOL_MISSING_AGENT` | `session query tools require an agent-bound caller` | `workspace-access.ts:58-63` |
| 目标越界 / 不存在 / 观测漂移 | `SESSION_QUERY_TOOL_UNAUTHORIZED` | `session target is outside the caller workspace` | `service-boundary.ts:92-97` |
| 调用者无 workspace 还想跨会话搜 | `SESSION_QUERY_TOOL_UNAUTHORIZED` | `cross-session search is unavailable because the caller session has no workspace` | `operations.ts:62-68` |
| 当前会话检索但无 step 边界 | `SESSION_QUERY_TOOL_NO_CURRENT_STEP` | `current-session search requires an active step boundary` | `operations.ts:130-135` |

四条都是**工具自己造的错误**，故意放在 `serviceBoundary` 之外，因此不会被消毒器改写（note `2026-07-24:26` 末句）。

### 4.5 未授权边界如何渲染

祖先断点渲染成 `- [outside workspace boundary]`（`presentation.ts:126`），触发条件二选一：遇到未授权祖先（`operations.ts:186-190`），或祖先全授权但 `trace.complete === false`（父链离开可见语料，`:192`）。**两种情况渲染完全一样**，模型无法推断「父会话不存在」还是「在别的 workspace」。后代断点 `- [outside workspace subtree]` 保留缩进层级（`presentation.ts:138-142`，`visitDescendants` 用显式栈做前序，`workspace-access.ts:207-227`）。标题逐 id 降级为 `"<title> (title unavailable: <CODE>)"`（`workspace-access.ts:237-241`），但若消毒后的 code 是 UNAUTHORIZED 则重新抛出（`:171`）——「标题读不到」可降级，「标题源 header 越界」不可。

---

## 5. 存储与索引

### 5.1 物理形态：一个可丢弃的派生库

`packages/session-query/session-query-sqlite/src/schema.ts`：`SESSION_QUERY_SQLITE_SCHEMA_VERSION = 8`（`:8`）、`SESSION_QUERY_SQLITE_APPLICATION_ID = 0x44534851`（`:11`）；建库 `open(path,'wx',0o600)`、目录 `0700`（`:31-38,48-51`）。打开时三重拒绝（`:55-67`）：application_id 非 0 且非自家 → "belongs to another application"；id 为 0 但已有用户表 → "is not an empty or recognized derived index"；自家 id 但含**未知用户表** → `assertDerivedUserTables` 抛错（`:87-94`）。只有「认得出的、版本不兼容的」派生 schema 才原地 `DROP TABLE` 重置（`:96-101`）。**没有 SQLite trigger，没有 WAL 订阅，没有任何回灌会话存储的写路径。**

### 5.2 表结构（`schema.ts:103-169`）

```sql
CREATE TABLE IF NOT EXISTS search_state (
  singleton         INTEGER PRIMARY KEY CHECK (singleton = 1),
  global_generation INTEGER NOT NULL
) STRICT;

CREATE TABLE IF NOT EXISTS persisted_sessions (
  id             TEXT PRIMARY KEY,
  version        INTEGER NOT NULL,
  created_at     INTEGER NOT NULL,
  cwd            TEXT,
  parent_session TEXT,
  seed_length    INTEGER,
  delegation_depth INTEGER,
  agent_preset  TEXT,
  revision       TEXT NOT NULL,
  generation     INTEGER NOT NULL
) STRICT
```

```sql
CREATE VIRTUAL TABLE IF NOT EXISTS persisted_docs USING fts5(
  text,
  session_id UNINDEXED,
  seq UNINDEXED,
  type UNINDEXED,
  time UNINDEXED,
  surface UNINDEXED,
  codepoint_length UNINDEXED,
  tokenize = 'unicode61'
)
```

连接级 TEMP 表同构：`temp.live_sessions`（多 `fingerprint TEXT NOT NULL` 与 `persisted INTEGER CHECK (persisted IN (0,1))`）与 `temp.live_docs`。**只有 `text` 建 FTS，其余全是 `UNINDEXED` 元数据列**，不另建 B-tree。

### 5.3 live / persisted 的遮蔽语义

查询把两张 FTS 表 `UNION ALL`，持久侧加反连接（`src/index.ts:797-850`）：`WHERE persisted_docs MATCH ? AND ? = 1 AND NOT EXISTS (SELECT 1 FROM temp.live_sessions AS ls WHERE ls.id = pd.session_id)`；那个 `? = 1` 绑定是「当前是否挂了 persistence 后端」（`selectedDocumentsParams`，`:854-869`）。同一 id 有 live owner 时持久行立刻被遮住，owner 消失后自动重新可见（`session-query-sqlite/README.md:104`）。

### 5.4 摄取：每次 search 前的串行 reconciliation

没有后台 daemon。`searchSessions`/`searchEvents` 的第一步都是 `_serialized(signal, async () => { await this._ensureReady(signal); await this._reconcile(signal); ... })`（`src/index.ts:268-294,296-325`）。`_reconcile`（`:407-493`）：① 读索引侧现状（`persisted_sessions` 的 `revision` + `temp.live_sessions` 的 `fingerprint`，`:410-417`）；② `_observeStable`（`:495-562`）里 `persistence.list()` 取快照，逐条与索引 `revision` 比对，**只对新增/变化的会话**走 `readColdSessionLog` 冷读（`:513-526`），并跳过已被 live 遮蔽的 id（`:520`），随后再 `list()` 比对前后快照（`:528-531`）；③ 稳定性上限 `STABLE_OBSERVATION_ATTEMPTS = 2`（`:86`），失败 → `SESSION_QUERY_PERSISTENCE_FAILED`（`session-search persistence observation did not stabilize after one retry`，`:558-561`）；④ 单事务 `BEGIN IMMEDIATE` → 删/插 `persisted_*` → 更新 `search_state.global_generation` → 删/插 `temp.live_*` → `COMMIT`，失败 `ROLLBACK` 并包成 `SESSION_QUERY_INDEX_FAILED`（`:451-486`）；⑤ live 指纹是 `sha256(JSON{header,inheritedEventCount,events})`（`:883-890`），任何事件变化都触发整段「删旧行 + 重插全部文档」（`:616-645`）。README 一句话概括收益：`Repeated queries and unchanged reopens read nothing`（`session-query-sqlite/README.md:104`）——代价是每次 search 都 list 一遍 persistence，收益是永远不需要一致性协议。

### 5.5 generation 与游标

游标 payload（`src/index.ts:193-200`）：`{ version: 1, instance: <本进程引擎的 randomUUID>, scope: 'sessions'|'events', fingerprint: <归一化请求的确定性 JSON>, generation: 'live:<n>'|'persisted:<epoch>:<n>', offset: <n> }`。

- 编码 `base64url(JSON)` 后打品牌类型（`:975-977`）；解码校验 `version/instance/scope/fingerprint` → `SESSION_QUERY_INVALID_CURSOR`，`generation` 不符 → `SESSION_QUERY_STALE_CURSOR`（文案 `session-search cursor is stale because its relevant corpus changed`）（`:979-1010`）。**绝不透明重启**（测试 `spec.ts:1549-1560`）。
- 指纹由 `requestFingerprint` 生成，过滤器做 `canonicalFilters`（值排序 + 过滤器排序）保证同义请求同指纹（`query.ts:244-261,444-455`）。
- 代际推进：持久侧有变化 `nextMainGeneration += 1`（`:441`）；live 替换 `nextLocalGeneration = max(local, main) + 1`（`:442-449`）；事务成功或有写 `this._globalGeneration += 1`（`:488`）；持久目标游标额外带 `_persistenceEpoch`（`:734`），换 persistence 后端也失效。跨会话游标受全局代际约束，会话内游标只受目标代际约束（`session-query-sqlite/README.md:61`）。

### 5.6 分页：模型看不到的翻页

provider 单页 `defaultLimit = 20`、`maxLimit = 100`（`src/index.ts:79-81`），工具**从不传 `limit`**（`operations.ts:95-100`），因此每页 20 条；provider 用 `LIMIT limit+1` 探测下一页（`src/index.ts:661,697,961-973`）。工具层 `collectPages`（`operations.ts:244-277`）每次循环前后都 `signal.throwIfAborted()`；命中数到 `maxResults` 才报封顶（`if (items.length === maxResults) return {items, capped:true}`，`:262-265`），所以「恰好 100 条且没有更多」不会误报（测试 `spec.ts:1295-1310`）；`seen` 集合命中重复游标 → `SESSION_QUERY_INVALID_CURSOR`（`:268-273`，测试 `spec.ts:1561-1570`）。故一次 `session_search` 最坏是「5 次 provider 往返 + 5 次 reconciliation」，`maxSearchResults` 控制的是**授权后命中条数**而非 SQL 行数。

### 5.7 查询构造与安全（`session-query-sqlite/src/query.ts`）

| 机制 | 实现 | 出处 |
|---|---|---|
| 查询文本当数据 | `quoteFtsData`：`"${query.replaceAll('"','""')}"`，整串包成一个 FTS5 phrase | `:223-225`；README `:59` |
| 保留标记防碰撞 | `highlight()` 用 `\uFDD0`/`\uFDD1`（`:19-21`），入库前 `sanitizeFtsText` 把它们与 `\0` 换成 `\uFFFD`（`:232-237`） | 同左 |
| 谓词预算 | FTS5 外层谓词 ≤ 14，超了 `SESSION_QUERY_INVALID_FILTER` | `:30,49-56` |
| 绑定预算 | 宿主参数 ≤ 32766 | `:27,36-43` |
| 空列表 | 编译成恒假 `0`（不是 `IN ()`） | `:391-393,404-406` |
| 时间戳 | 工具边界带时区 ISO 8601 → 闭区间毫秒；亚毫秒余数用 `nextUpFinite`/`nextDownFinite`，避免把 (`...0001`,`...0002`) 塌成一个点 | `tool-session-query/src/input.ts:158-246` |

### 5.8 排序确定性

跨会话 SQL（`src/index.ts:665-681`）：`ROW_NUMBER() OVER (PARTITION BY session_id ORDER BY match_count DESC, document_length ASC, time DESC, seq DESC) AS event_rank`，外层 `WHERE event_rank = 1 ORDER BY match_count DESC, document_length ASC, time DESC, session_id ASC, seq DESC LIMIT ? OFFSET ?`。`match_count` **不是分数**，是 `highlight()` 标记的实际出现次数：`(length(CAST(marked_text AS BLOB)) - length(CAST(replace(marked_text, ?, '') AS BLOB))) / ?`（`:843-849`）；文档长度用入库时按 code point 数好的 `codepoint_length`（`:611,816`）。README：`more actual FTS5 highlighted-match spans first, then shorter documents, ...`，且不返回数值 score（`session-query-sqlite/README.md:61`）。

### 5.9 并发规则：搜索独占、精确读并行

`isConcurrencySafe` 是纯同步分类器，**只有返回字面 `true` 才算并行**，缺省/抛错/非 true 一律 exclusive（`packages/core/tools/src/index.ts:1304-1320`；`exclusive` 会形成排序屏障，`:355-361`）。因此两个 search 未声明 → 不能与任何 sibling 并行，三个 trace/read 显式 `() => true`（`src/index.ts:90,103,118`）。理由：`because one search consumes generation-bound provider cursors, the two search tools execute exclusively with sibling tool calls, while the three exact trace/read tools opt into parallel execution`（`README.md:93`；note `2026-07-24:32`；测试 `spec.ts:308-328`）。索引侧还有第二道锁：`_serialized` 用 promise tail 队列把所有 search 串成单飞（`src/index.ts:383-405`），配 `exactly one service in one process owns a derived-index path`（`session-query-sqlite/README.md:108`）——这也是 trace/read 能并行的原因：它们根本不碰 FTS。

### 5.10 超时与上限

| 旋钮 | 默认 | 语义 | 出处 |
|---|---|---|---|
| `maxSearchResults` | `100` | 单次**授权后**命中条数上限 | `tool-session-query/src/index.ts:22,37,125-129` |
| `searchTimeoutMs` | `30000` | 只挂在两个 search 上的协作式 deadline | `src/index.ts:25,38,70,80` |
| `snippetChars` | `240` | 片段长度（code point） | `session-query-sqlite/src/index.ts:83,212` |
| `defaultLimit`/`maxLimit` | `20`/`100` | provider 单页 / 最大页 | `:79-81,210-211` |
| `readWindowMax` | `50` | `before`/`after` 上限 | `session-query/src/config.ts:7` |
| `persistedReadConcurrency` | `4` | 批量冷读并发 | `config.ts:10` |
| `preparedSessionCacheSize` | `5` | 冷 prepared-Session 复用 | `config.ts:13` |

`timeoutMs` 的真正执行者是另一个插件：它把 `exec.signal` 临时换成 `deadline(exec.signal, timeoutMs, TOOL_TIMEOUT)`，只在自己这层计时器先响时把结果替换成 `Error: tool call timed out after ${timeoutMs}ms` + `error.code = 'TOOL_TIMEOUT'`（`packages/guard/timeout-policy/src/index.ts:25,41-48,55-80`）。它是**协作式**的：依赖工具把 `exec.signal` 一路转发（所以 DSH 规定「声明 `timeoutMs` 就等于承诺可协作退出」，`packages/core/tools/src/index.ts:261-267`）。

---

## 6. 错误处理

### 6.1 唯一闸门 `serviceBoundary.call`（`src/service-boundary.ts:99-114`）

```ts
signal.throwIfAborted()
try {
  const value = await invoke()
  signal.throwIfAborted()
  return value
} catch (error: unknown) {
  signal.throwIfAborted()
  throw sanitizeError(ctx, operation, error)
}
```

三个细节：① **取消原样透传**——进入前、正常返回后、catch 里都先 `throwIfAborted()`，抛的是取消原因本身而非映射后的字符串（`session-query-sqlite` 的 `waitWithAbort` 也会用 `SESSION_QUERY_ABORTED` 拒绝，`session-query-sqlite/src/index.ts:1092-1111`；测试 `spec.ts:992-1050` 取消优先于 invalid lineage，`spec.ts:753-805` 父 id 预授权取消不写日志）；② **诊断只进日志**——`ctx.logger.warn('tool-session-query: <operation> failed: <diagnostic>')`（`:124`），`operation` 是调用点传的阶段名（`'session search'`/`'event search'`/`'session lineage trace'`/`'event trace'`/`'event read'`/`'target authorization'`/`'session-id authorization'`/`'title observation'`，`operations.ts:94,156,179,213,232`，`workspace-access.ts:84,123,144`）；③ **诊断渲染防御性**——`renderFullError` 沿 `Error.cause` 链收集 `stack ?? String(error)`，循环引用加 `[circular error cause]`，整体不可打印时用 `[unprintable session query failure]`（`:150-172`）。

### 6.2 映射表：17 个 `SessionQueryErrorCode` → 固定安全文案（`service-boundary.ts:21-90`，逐字）

| 输入 code | 输出 code | 输出 message |
|---|---|---|
| `SESSION_QUERY_ABORTED` | `SESSION_QUERY_ABORTED` | `session query was cancelled` |
| `SESSION_QUERY_CORRUPT_SESSION` | `SESSION_QUERY_CORRUPT_SESSION` | `session event history is corrupt` |
| `SESSION_QUERY_EVENT_NOT_FOUND` | `SESSION_QUERY_EVENT_NOT_FOUND` | `session event was not found` |
| `SESSION_QUERY_INDEX_FAILED` | `SESSION_QUERY_INDEX_FAILED` | `session search index is unavailable` |
| `SESSION_QUERY_INVALID_CONFIG` | `SESSION_QUERY_TOOL_FAILED` | `session query operation failed` |
| `SESSION_QUERY_INVALID_CURSOR` | `SESSION_QUERY_INVALID_CURSOR` | `session search continuation is invalid` |
| `SESSION_QUERY_INVALID_FILTER` | `SESSION_QUERY_INVALID_FILTER` | `session query filters were rejected` |
| `SESSION_QUERY_INVALID_LIMIT` | `SESSION_QUERY_INVALID_LIMIT` | `session query result limit was rejected` |
| `SESSION_QUERY_INVALID_QUERY` | `SESSION_QUERY_INVALID_QUERY` | `session query was rejected` |
| `SESSION_QUERY_INVALID_LINEAGE` | `SESSION_QUERY_INVALID_LINEAGE` | `session lineage is invalid` |
| `SESSION_QUERY_INVALID_SURFACE` | `SESSION_QUERY_INVALID_SURFACE` | `session event history is invalid` |
| `SESSION_QUERY_INVALID_WINDOW` | `SESSION_QUERY_INVALID_WINDOW` | `session event window is invalid` |
| `SESSION_QUERY_PERSISTENCE_FAILED` | `SESSION_QUERY_PERSISTENCE_FAILED` | `session history storage is unavailable` |
| `SESSION_QUERY_SEARCH_DISABLED` | `SESSION_QUERY_SEARCH_DISABLED` | `session search is disabled in this deployment` |
| `SESSION_QUERY_SESSION_NOT_FOUND` | `SESSION_QUERY_SESSION_NOT_FOUND` | `session was not found` |
| `SESSION_QUERY_STALE_CURSOR` | `SESSION_QUERY_STALE_CURSOR` | `session search changed while paging; retry the complete search call` |
| `SESSION_QUERY_SOURCE_CONFLICT` | `SESSION_QUERY_TOOL_FAILED` | `session query operation failed` |

两处刻意「降级为通用失败」：`INVALID_CONFIG`（部署配置错误不该被模型知道）、`SOURCE_CONFLICT`（源元数据自相矛盾，内部不变量破坏）。`sanitizeError`（`:116-141`）先建兜底 `genericFailure()` 再 `fullError(error)` 取诊断，`try` 内记录日志 + 分类，`catch { return generic }`，最后也 `return generic`；分类用 `Object.hasOwn(SAFE_SESSION_QUERY_FAILURES, code)`（防原型链注入）而非 `in`，`code` 先按 `unknown` 取再判类型（防 getter 抛错）。测试 `spec.ts:1477-1548`（不可打印的嵌套诊断仍保留外层安全分类；循环 cause 链只进日志；logger 抛错仍回 generic）。`genericFailure()` = `HarnessError('session query operation failed','SESSION_QUERY_TOOL_FAILED')`（`:143-148`）。

### 6.3 本地校验错误保持精确（`src/input.ts`，不经消毒器）

| 触发 | code | 文案（原文） |
|---|---|---|
| query 全空白 / 含 NUL | `SESSION_QUERY_INVALID_QUERY` | `session-search query must contain non-whitespace text` / `session-search query must not contain NUL` |
| seq 非负安全整数 | `SESSION_QUERY_INVALID_FILTER` | `sequence lower bound must be a non-negative safe integer`（`upper bound` 同理） |
| seq from > to | `SESSION_QUERY_INVALID_FILTER` | `session sequence range from must be less than or equal to to` |
| 时间戳无时区/非法 | `SESSION_QUERY_INVALID_FILTER` | `session <name> range must be an ISO 8601 timestamp with Z or a numeric offset` / `... must be a valid ISO 8601 timestamp` |
| 空数组 | `SESSION_QUERY_INVALID_FILTER` | `<name> must contain at least one value when supplied` |

（`input.ts:126-141,143-156,158-177,271-294`。）原则：`Tool-owned input-validation and authorization errors remain precise because they are created outside this service boundary`（note `2026-07-24:26`）。

### 6.4 模型实际看到什么（重要细节）

工具抛错后 registry 折成 `content: [{type:'text', text: \`Error: ${message}\`}]`，同时把 `{name, code}` 放进 `error.info`（`packages/core/tools/src/index.ts:1913-1925,660-668`）；agent loop 再把 `error.info` 写进 `tool/result` 事件字段（`packages/core/agent-loop/src/tool-calls.ts:264-286`）。**也就是 code 是持久元数据，模型在 content 里只看到 `Error: <message>`。** 所以全部安全语义必须落在 message 上——映射表的价值正在于此：message 既不含路径、不含 id、不含堆栈，又足够告诉模型下一步（`retry the complete search call` / `narrow the query`）。

---

## 7. 输出渲染

### 7.1 逐工具的文本形状

| 工具 | 首行 | 后续 | 出处 |
|---|---|---|---|
| `session_search` | `Session search results (N):` | 每条 6 行（前置空行）：`N. Session <id> — <title>` / `   Created: <ISO>` / `   Parent: root\|<id>\|[outside workspace]` / `   Availability: live, persisted\|unavailable` / `   Best match: seq <n> \| <type> \| <surface> \| <ISO>` / `   Snippet: <text>`，条目间空行 | `presentation.ts:48-79` |
| （空） | `No prior session matches found.` | — | `:81-83` |
| `session_event_search` | `Session <id> — <title>` | 空：`No prior event matches found.`；否则 `Event search results (N):` + `N. seq <n> \| <type> \| <surface> \| <ISO>` + `   Snippet: <text>` | `:85-106` |
| `session_trace` | `Session <id> — <title>` / `Created:` / `Availability:` / 空行 / `Ancestors (nearest first):` | `- <id> — <title> \| <ISO> \| <availability>`；无祖先时 `- none (target is a root session)`；断点 `- [outside workspace boundary]`；空行 + `Descendants:` + 缩进树 | `:108-147` |
| `session_event_trace` | `Session <id> — <title>` | 六行 `Target:` / `Replaced by:` / `Replacement chain:` / `Events replaced by target:` / `Events cited directly as sources:` / `Direct derived events:`；空列表 `none` | `:149-163,203-205` |
| `session_event_read` | `Session <id> — <title>` / `Target event seq <n>:` / ` ```json ` | 目标完整 JSON（2 空格缩进）+ 可选 `Before:`/`After:`；邻居 `- seq <n> \| <type> \| <ISO>`，有语义文本时换行缩进两格贴全文（`\n` 也缩进），无文本时 ` \| (no semantic text)` | `:165-194` |

封顶提示（两个 search 共用一句，原文）：`Result cap reached. Narrow the query or add filters to find additional matches.`（`presentation.ts:75-78,102-104`）。这是**唯一**告诉模型「还有更多」的方式——没有 token、没有 offset、没有「剩余 N 条」。`presentCall` 只影响 UI（`card:'generic'`，`kind:'search'|'read'`，title 如 `Trace current session` / `Read event 42`，`rawInput` 放 query 或 `{session_id, seq}`），模型侧不可见（`presentation.ts:211-241`；`packages/core/tools/src/index.ts:282-299`）。

### 7.2 为什么没有「第二套截断」

`session_event_read` 会贴**未删减**事件 JSON、`session_trace` 会贴**整棵树**（note `2026-07-24:34`：`Canonical results are plain text and remain complete within the service's existing event-window and search-count resource bounds`）。本包既不 import spill 也不做字符预算：`The package performs no byte or character truncation and does not import a spill backend`（`README.md:57`）。取舍（note `2026-07-24:45`）：**工具本地截断/offset/spill 文件**被否，因为 post-execute spill policy 已统一持有「完整结果留存 + 取回」。

接管者是通用插件 `packages/spill/spill-policy/src/index.ts`：唯一配置 `maxInlineTokens`（`:25-28`），未配置则整个插件 `return`（`:49-50`）；挂在 `tools/post-execute`，先 `await next()`，只在结果 `accept` 且无 policy 已替换的 `value` 时介入，`exec.name === 'read'` 特殊放行（`:133-150`）；超预算时 `spillStore.saveText({owner:{sessionId}, source:{kind:'tool',toolName,callId,label}, suggestedName:'<tool>.txt', content:完整文本})` 得 `SpillRef`，再 `retainContent(content, maxTokens - reserved, price)` 保留头尾、中间用 `\n\n[...]\n\n` 断开、尾部追加带 locator 与字节数的 notice（`:91-131`）；任何失败（无 backend、无 pricing、无 owner）只 warn 并**保留原文**（`:127-130`）。于是「截断」在全仓只有一个 owner（`docs/tool-catalog.md:40` 的 deployment note 也写：`compositions that need enforced deadlines or bounded inline output also mount the generic timeout or spill policies`）；装配样例见 `snapshots/session/session-query-spill/cordis.yml`。

---

## 8. 设计史与踩坑

### 8.1 `2026-07-24-model-facing-session-query-tools.md`（核心决策）

**问题**（`:10`）：`ctx.sessionQuery` 已能精确读/过滤/追踪/检索，但把 provider 请求类型直接给模型会暴露「不稳定的分页游标、可信语料范围、存储形态的时间值、以及更方便程序消费而非推理的结果记录」；而「大 trace / 大 payload 的输出尺寸」若在本消费者里自解，会重复全仓 spill 机制并让会话工具与其他工具不一致。

**决策**（`:14-20`）：五个窄工具 + 一条固定指导语；包**只 import 接口不 import SQLite 实现**；入口只做「配置 / prompt 注册 / 工具注册」，其余按执行边界切模块（`input.ts` schema+归一化+过滤器；`service-boundary.ts` provider 调用+模型安全翻译；`workspace-access.ts` 身份+授权+标题+谱系投影；`operations.ts` 五工作流编排；`presentation.ts` 渲染+call card）。过滤语义：扁平 snake_case；时间戳工具边界带时区 ISO 8601 → 服务闭区间毫秒 → 渲染回 UTC ISO 8601；一个列表内 OR、过滤器间 AND；父 id 去重+授权后才进 FTS，缺失与越权猜测表现一致，root 标记独立 OR；事件类型字符串保持开放（`SessionEventMap` 可合并扩展），availability/surface 用闭集。

**权威**（`:24`）：`Every executor derives its caller from immutable ToolExecution.exec.agent identity and never accepts a model-supplied workspace`；每个 payload 的 header 必须与「同一次服务观测」一起校验；谱系在未授权边界停住且不泄漏 id；无 `cwd` 只能看自己；无 agent 身份 fail closed。

**游标与并行**（`:30-32`）：不暴露 cursor/offset/page size/limit；一次执行内部跟 provider 游标翻页到 `maxSearchResults`（默认 100）；封顶让模型缩窄；代际变化让整次重试；`searchTimeoutMs` 默认 30s 同时接成工具 deadline 与 service abort signal；**因为内部页共享 generation-bound 游标，两个 search 在调度器里独占**，三个精确工具因「观测能容忍中间提交」而并行。

**Host 装配**（`:38-40`）：消费者 opt-in；出厂 TUI/Web/headless 都不挂，只保留 `ctx.sessionQuery` 索引（服务 `/resume` 与 Web 内容搜索）；automation 的 ACP composition 也不挂；只有 `snapshots/session/session-query-spill` 参照 composition 显式挂载。

**被否掉的替代**（`:44-48`）：

| 方案 | 否决理由 |
|---|---|
| 把 provider 游标暴露给模型 | 记录一次 tool result 或开始下一步都会改变 session/global generation，模型还没来得及复用游标就已失效 |
| 工具本地截断 / offset / spill 文件 | post-execute spill policy 已统一持有完整结果留存与取回 |
| 允许任意 persisted session 或模型自带 workspace 过滤 | `ctx.sessionQuery` 是可信服务，模型侧消费者必须自己执行调用者权限边界 |
| 把 search/trace/read 合成一个带 operation 选择器的工具 | 窄名字给模型的 schema、默认值、presentation 意图与后续动作都更清楚 |
| 谱系只返回一跳 | spill 已把「内联尺寸」这个动机拿掉，而一跳输出会丢掉没有 continuation 路径的关系 |

### 8.2 `2026-08-02-session-search-not-shipped-default.md`（回归教训）

**它曾经被默认发布**：一个 roster 决策把 `tool-session-query` 写进共享 `cordis.patch.yml` 的默认行，于是出厂 TUI/Web 把五个工具塞给模型，既与 07-24 的 opt-in 立场（README 记为 "shipped host compositions do not mount it by default"）矛盾，还额外发布了一段没人要求的 prior-work 检索工作流提示（`:10`）。**决策**（`:14-16`）：TUI/Web/headless 都不挂，任何出厂 preset 也不带；消费者保持 opt-in；`session-query` 参照 composition 仍挂载。**但 `ctx.sessionQuery` 服务保留**：`session-query-sqlite` 仍是 base 行（TUI 的 `session-reference` 用它做 `/resume`），只是全文索引默认关（`openAt: never`）；被移除的只有面向模型的消费者。**否掉的替代**（`:20-22`）：连 sqlite provider 一起移除（不行，`/resume` 与 Web 内容搜索直接用 `ctx.sessionQuery`，那是宿主功能）；保留行但在 overlay 里 disable（不行，disabled 的 base 行仍发布依赖并诱导一行 re-enable）；只在 TUI 挂（不行，共享 base 是单一 roster）。**后果**（`:24`）：两个 surface 回到同样的 20 个无条件工具（ripgrep 时另加 `glob`/`grep`），默认请求里五条 schema 与 prompt section 一起消失，shipped-composition 测试钉住这个更小的目录。

### 8.3 衍生决策：内容搜索也 opt-in（`2026-08-13-session-content-search-opt-in.md:10-18`）

把 provider 整个 unmount 不可行——`ApiProxyService` 把 `sessionQuery` 声明为必需注入，缺了它整个 host API gateway 不加载、Web GUI 起不来；会话日志导出与 subagent fork 也靠 `ctx.sessionQuery.traceSession` 找后代。于是加了第三个开启阶段 `openAt: 'never'`：`searchSessions`/`searchEvents` 在**任何请求归一化之前**抛 `SESSION_QUERY_SEARCH_DISABLED`，`node:sqlite` 从不 import/打开/观测，继承来的精确读/过滤/追踪照常工作（实现 `session-query-sqlite/src/index.ts:338-344`）。对 jcode 的意义：**「索引导航」与「把索引喂给模型」必须是两个开关。**

### 8.4 尚未完成 / 明确延期

| 项 | 状态与出处 |
|---|---|
| workspace 只做字符串相等，符号链接等价路径不共享权威 | `tool-session-query/README.md:171,184-185`（Dev Note 写作 undecided 而非 backlog） |
| 搜索封顶后没有 continuation | `README.md:170` |
| 未装 spill policy 的自定义 composition 会内联完整 trace/read | `README.md:172` |
| trigram tokenizer 被测量并否决 | `session-query-sqlite/README.md:158` |
| 精简 `readSession`/`readTitle`/`readTitleSnapshot`/`listEvents` 四个无第一方产品调用者的方法 | `.agents/notes/proposed/simplification/2026-09-19-trim-session-query-convenience-api.md:9-19` |
| 实验性 bundle `@deepseek-ai/dsh-experimental-session-search`：Web/Desktop 的 Plugins 开关（默认关），开启时注册**隔离的** `sessionQuery` 群组（`:memory:` + `openAt: first-search`），关掉即释放 | `packages/experimental/session-search/cordis.patch.yml`；同包 `README.md:12,30-36` |

---

## 9. 关键代码地图

### 9.1 表格

| 文件:行 | 角色 |
|---|---|
| `tool-session-query/src/index.ts:19-121` | 插件入口：`inject`、config、prompt section、五个 `defineTool` |
| `.../index.ts:46-54` | `TEXT_OUTPUT`（`schema:{type:'string'}`）与 `PROMPT_TEXT` |
| `.../src/input.ts:45-86` | 三套模型参数 schema（session search / event search / target） |
| `.../src/input.ts:88-124` | 模型参数 → 服务过滤器（session/event 两套） |
| `.../src/input.ts:126-246` | query 归一化、seq/时间区间校验、亚毫秒边界 |
| `.../src/workspace-access.ts:56-73` | 调用者身份与 target 缺省（self） |
| `.../src/workspace-access.ts:75-135` | 三种授权动作 |
| `.../src/workspace-access.ts:137-173` | 标题批量观测与逐 id 降级 |
| `.../src/workspace-access.ts:175-235` | 谱系剪枝/遍历（迭代式，无递归爆栈） |
| `.../src/operations.ts:55-115` | `session_search` 工作流（parent 预授权 + cwd 过滤） |
| `.../src/operations.ts:117-169` | `session_event_search`（当前 step 排除） |
| `.../src/operations.ts:171-201` | `session_trace`（祖先断点/后代剪枝） |
| `.../src/operations.ts:203-242` | `session_event_trace` / `session_event_read` |
| `.../src/operations.ts:244-277` | `collectPages`：内部分页、封顶、重复游标防护 |
| `.../src/service-boundary.ts:21-90` | 17 条错误码 → 安全文案映射表 |
| `.../src/service-boundary.ts:99-141` | 唯一闸门 `call` + 分类 `sanitizeError` |
| `session-query/session-query-sqlite/src/schema.ts:8-169` | 应用 id、schema 版本、三张持久表 + 两张 TEMP 表 |
| `.../session-query-sqlite/src/index.ts:268-405` | 两个 search 入口 + 单飞队列 |
| `.../session-query-sqlite/src/index.ts:407-562` | reconciliation 与「稳定观测」重试 |
| `.../session-query-sqlite/src/index.ts:797-869` | live/persisted `UNION ALL` + `NOT EXISTS` 遮蔽 |
| `.../session-query-sqlite/src/index.ts:961-1010` | 分页 lookahead、游标编解码、stale 判定 |
| `.../session-query-sqlite/src/query.ts:223-294` | FTS 转义、标记清洗、请求指纹、片段生成 |
| `session-query/session-query/src/extraction.ts:15-44` | 哪些事件贡献「可检索语义文本」 |
| `core/tools/src/index.ts:1304-1320` | `executionMode`（只有字面 `true` 才并行） |
| `core/tools/src/index.ts:1913-1925` | `toolErrorResult`：`Error: <message>` + `error.info` |
| `guard/timeout-policy/src/index.ts:55-80` | `timeoutMs` 的真正执行者 |
| `spill/spill-policy/src/index.ts:133-155` | `tools/post-execute` 拦截与替换 |

### 9.2 八处最重要的原文摘录

**① 唯一注入的指导语**（`tool-session-query/src/index.ts:51-54`）

```ts
const PROMPT_TEXT =
  'Use session_search to find relevant work from prior sessions, or session_event_search to search earlier '
  + 'events in one session. Search results are cursor-free and workspace-scoped. Follow a useful hit with '
  + 'session_trace, session_event_trace, or session_event_read when you need lineage, relationships, or exact data.'
```

**② workspace 相等性的全部实现**（`workspace-access.ts:96-99`）

```ts
function headerAuthorized(header: SessionHeader, caller: Caller): boolean {
  if (header.id === caller.id) return header.cwd === caller.header.cwd
  return caller.header.cwd !== undefined && header.cwd === caller.header.cwd
}
```

**③ 目标预检 + oracle 抵抗**（`workspace-access.ts:75-90`）

```ts
if (target === caller.id) return
const cwd = caller.header.cwd
if (cwd === undefined) throw serviceBoundary.unauthorizedTarget()
const records = await serviceBoundary.call(ctx, signal, 'target authorization', () =>
  ctx.sessionQuery.filterSessions([
    { kind: 'id', values: [target] },
    { kind: 'cwd', values: [cwd] },
  ], signal))
if (records.length !== 1) throw serviceBoundary.unauthorizedTarget()
```

**④ 用「同一次观测」堵 TOCTOU**（`workspace-access.ts:101-109`）

```ts
function assertObservedTargetAuthorized(
  caller: Caller,
  target: SessionIdValue,
  observed: SessionHeader,
): void {
  if (observed.id !== target || !headerAuthorized(observed, caller)) {
    throw serviceBoundary.unauthorizedTarget()
  }
}
```

**⑤ 当前会话的 step 排除**（`operations.ts:128-139`）

```ts
if (sessionId === caller.id) {
  const stepStartSeq = caller.boundary?.lastStepStartSeq
  if (stepStartSeq === undefined) {
    throw new HarnessError(
      'current-session search requires an active step boundary',
      'SESSION_QUERY_TOOL_NO_CURRENT_STEP',
    )
  }
  // `null` (no step started yet) caps the range to a degenerate `to` the
  // filter validation rejects — the loop never runs tools outside a step.
  range.to = Math.min(range.to ?? Number.MAX_SAFE_INTEGER, (stepStartSeq ?? 0) - 1)
}
```

**⑥ 内部翻页 + 封顶 + 重复游标防护**（`operations.ts:256-276`）

```ts
while (true) {
  signal.throwIfAborted()
  const page = await request(cursor)
  signal.throwIfAborted()
  for (const item of page.items) {
    if (!accept(item)) continue
    if (items.length === maxResults) {
      return { items, capped: true }
    }
    items.push(item)
  }
  if (page.nextCursor === undefined) return { items, capped: false }
  if (seen.has(page.nextCursor)) {
    throw new SessionQueryError(
      'session-search provider repeated a continuation cursor',
      'SESSION_QUERY_INVALID_CURSOR',
    )
  }
  seen.add(page.nextCursor)
  cursor = page.nextCursor
}
```

**⑦ 单一消毒器**（`service-boundary.ts:121-140`）

```ts
const generic = genericFailure()
const diagnostic = fullError(error)
try {
  ctx.logger.warn(`tool-session-query: ${operation} failed: ${diagnostic}`)
  if (error instanceof SessionQueryError) {
    const code: unknown = error.code
    const failure = typeof code === 'string' && Object.hasOwn(SAFE_SESSION_QUERY_FAILURES, code)
      ? SAFE_SESSION_QUERY_FAILURES[code as SessionQueryErrorCode]
      : undefined
    if (failure !== undefined && failure.code !== 'SESSION_QUERY_TOOL_FAILED') {
      return new SessionQueryError(failure.message, failure.code)
    }
  }
  if (error instanceof HarnessError && error.code === 'SESSION_QUERY_TOOL_UNAUTHORIZED') {
    return unauthorizedTarget()
  }
} catch {
  return generic
}
return generic
```

**⑧ 游标失效而不是「悄悄纠正」**（`session-query-sqlite/src/index.ts:992-1009`）

```ts
if (
  decoded.version !== 1
  || decoded.instance !== instance
  || decoded.scope !== scope
  || decoded.fingerprint !== fingerprint
  || !Number.isSafeInteger(decoded.offset)
  || decoded.offset === undefined
  || decoded.offset < 0
) {
  throw invalidCursor(new Error('cursor does not belong to this normalized request'))
}
if (decoded.generation !== generation) {
  throw new SessionQueryError(
    'session-search cursor is stale because its relevant corpus changed',
    'SESSION_QUERY_STALE_CURSOR',
  )
}
return decoded.offset
```

---

## 10. 对 jcode 的启示

先说结论：**值得抄的是「权限与呈现的纪律」，不值得抄的是为「live+persisted 双源、多进程、SQLite 派生索引」长出来的整套机器**。jcode 是 Go 单二进制；会话是 `~/.jcode/sessions/{uuid}.json`（JSONL 内容，逐行 `Entry`，容错跳过坏行，`internal/session/session.go:1926-1963`）；索引是 `~/.jcode/sessions/session.json`（`internal/config/config.go:1352-1358`，`ListSessions(project)` 见 `session.go:1788`）。

**1) 身份从 `*Env` / 会话上下文取，schema 里绝不放 workspace 参数。** DSH 五条 schema 里没有任何路径或 workspace 字段（§3），身份来自 `exec.agent.session.header`。jcode 的工具是 `*Env` 上的方法，所以 `NewSessionSearchTool()` 应从 `Env` 读「当前会话 id + 当前 project」，而不是新增 `project` 入参。注意 `Entry.Project` 只在 `session_start` 写过（`session.go:1465-1478`），运行期目录可变（`working_directory` 工具），所以授权键应取**会话启动时记录的 project**（等价于 DSH 用 header 而非 cwd 变更事件）。

**2) 授权键 = project 精确相等；空 project 只能看自己，且必须复检。** 直接抄 `headerAuthorized`：`caller.Project == target.Project`，空值只允许 self。好消息是 jcode 的索引本来就按 project 分组（`ListSessions(project)`），授权天然是**结构性**的；坏消息是 `session.json` 只是缓存（`UpdateSessionMeta` 会重写它，`session.go:1808-1868`），所以渲染前要打开目标文件读第一行 `session_start` 确认 `Project` 与索引一致（等价 `assertObservedTargetAuthorized`，`workspace-access.ts:101-109`）。目标不存在与 project 不匹配必须返回**同一句**错误文案。

**3) 保留「无游标」纪律。** 一次调用扫完，封顶返回 `结果上限已达（N 条），请缩小关键词或加过滤条件。`（对应 `presentation.ts:75-78`）。jcode 没有 generation，理论上能做 continuation，但 DSH 的理由在 jcode 里同样成立：一次 tool result 就写进 JSONL，会话已经前进。**不做 continuation**。默认上限建议 20（DSH 是 100，因为它每页 20 最多翻 5 页）。

**4) 分两层：`internal/sessionquery` 服务 + `internal/tools/` 消费者。** 前者纯 Go，暴露 `Search(ctx, req)`、`TraceSession(id)`、`TraceEvent(id,seq)`、`ReadEvent(id,seq,before,after)`，无 token 预算、无错误消毒；后者只管 schema、参数校验、授权、渲染、错误文案。这样 Web/TUI 的「历史会话搜索」可直接复用同一服务（对应 DSH 的 `/resume` 与 Web 内容搜索共用 `ctx.sessionQuery`，note `2026-08-02:16`）。

**5) 父会话过滤：先去重再授权，猜不中 = 空结果而非报错。** 照抄 `materializeParentSessionIds`（`input.ts:103-107`）+ `authorizeSessionIds`（`workspace-access.ts:111-135`）：只把授权过的 id 拼进查询，root 标记独立 OR；**全部未授权且没要 root 就直接返回空结果文案、连扫描都不做**（`operations.ts:87`）。模型便无法用「空 vs 报错 vs 数量」做存在性探测（测试 `spec.ts:621-648`）。

**6) 谱系只在断点放占位符，永不泄漏隐藏 id。** jcode 的后代关系**已在文件系统里**：teammate 会话写在 `~/.jcode/sessions/{leaderUUID}/subagents/agent-{agentID}.jsonl`（`session.go:1400-1444`），祖先由目录名推出 leader，后代扫 `{id}/subagents/` + 索引里的父子关系。渲染照抄 `- [outside workspace boundary]` / `- [outside workspace subtree]`（`presentation.ts:126,141`），并且**不要**说明「父 id 解析不到」的真实原因（`operations.ts:192` 把「不存在」与「越权」合成同一个 `ancestorBoundary` 就是为此）。剪枝要保证「剪掉一个节点就不再下钻」，否则能通过孙节点反推父节点存在。

**7) 自身会话检索必须排除「进行中的这一轮」。** DSH 用 `turnBoundary.lastStepStartSeq - 1`（`core/agent-loop/src/index.ts:41-90` + `operations.ts:136-139`）。jcode 的 JSONL 没有 step 事件，等价物是「截到本轮 assistant 的第一条 `Entry` 之前」（或更保守：截到本次工具调用所在 assistant 消息之前）。必须实现，否则模型会检索到自己的查询参数并自激（note `2026-07-24:28`：`including ... the query arguments indexed from that call`）。

**8) 错误：一个包内统一封装，文案里带 code。** DSH 的 code 不进模型正文（§6.4），但 jcode 工具**只能返回字符串**（AGENTS.md），所以反过来做：把稳定 code 嵌进文案，如 `ERROR[SESSION_QUERY_UNAUTHORIZED]: session target is outside the caller workspace`；内部细节（路径、堆栈、provider 错误）只进 `config.Logger()`（对应 `service-boundary.ts:124`）。同时保留两档纪律：**参数校验要具体可自纠**（`input.ts:126-294` 那批文案），**服务/IO 错误收敛成固定句子**。

**9) 输出形态：第一期只做 3 个工具。** 价值最高的是 `session_search`（找到哪个会话）与 `session_event_read`（拿到确切事件）；`session_event_search` 便宜且有用。`session_event_trace` 依赖 jcode **没有**的 surface 模型（`current`/`shadowed`/`log-only` 来自 `foldSurface`，`session-query/src/documents.ts:57-74`；jcode 的 `Entry` 无 replacement/sourceEventSeqs，`session.go:263-288`）。`session_trace` 放第二期（依赖先加 `ParentUUID`）。

**10) 默认不挂；一个显式开关 + 一条固定指导语。** DSH 把「默认发布」当成被回滚过的错误（note `2026-08-02`），代价是 5 schema + 1 段 prompt 常驻每请求（`README.md:127-133`）。jcode 对应：`~/.jcode/config.json` 加 `session_query: {enabled: bool, max_hits: int}`（默认 `false`），开启时在 `internal/prompts` 追加**一段**（不是五段）指导语，文案可借用 DSH 那句并换工具名。注册按 AGENTS.md 同步四处：`internal/command/interactive.go:160` 的 `buildAllTools`、`:231` 的 `buildPlanTools`、`internal/command/acp.go` 与 `internal/command/web.go` 的内联列表；只读工具天然免审批（`internal/runner/approval.go` 的只读分支）。

**11) 时间戳别过度设计。** DSH 为「带时区 ISO 8601 + 闭区间 + 亚毫秒」写了 60 行（`input.ts:158-246`）。jcode 的 `Entry.Timestamp` 就是 `time.RFC3339`（`session.go:277`），`time.Parse` 后 `!t.Before(from) && !t.After(to)` 即可。真正要抄的只有一句纪律：**边界一律闭区间，且错误文案要说清「必须带时区」**，否则模型会传 `2026-10-18` 然后把失败归因到工具坏了。

### 明确**不要**抄的部分（单二进制 + JSONL 不成立的前提）

| DSH 机制 | 为什么 jcode 不需要 |
|---|---|
| SQLite FTS5 派生索引 + `application_id` + schema 版本原地重建 + TEMP 表遮蔽（`schema.ts:8-169`） | jcode 只有一个进程写会话、索引已是 `session.json`。第一版 `LoadSession` + `strings.Contains`/`regexp` 扫描即可；撑不住时再加一个**可选、默认关**的派生索引（学 `openAt: never`）。 |
| live / persisted 双源 + 每次 search 前 `persistence.list()` 对账 + revision 比对（`index.ts:407-562`） | jcode 没有两套真相：`Recorder` 直接追加同一个文件（`session.go:1450-1495`），读到的就是最新。 |
| generation（global/local/epoch）+ 游标 instance/fingerprint 校验（`index.ts:193-200,975-1010`） | 前提是游标要给模型/跨调用复用；jcode 不做 continuation，这套全部消失。 |
| 17 码 `SessionQueryErrorCode` 闭集 + 17 条映射表（`config.ts:36-51`，`service-boundary.ts:21-90`） | 面向 Go 内部调用者，5 个类别足够：`unauthorized`/`not_found`/`invalid_query`/`io`/`corrupt`。 |
| `tools/post-execute` 通用 spill 策略（`spill-policy/src/index.ts:133-155`） | jcode 现有截断 owner 是**展示层**（`internal-doc/tool-output-redesign.md:78-79` 的 head/tail + ctrl+t transcript），模型侧没有等价物。所以 `session_event_read` 若会贴整段事件 JSON，必须在**工具层**加一次模型侧预算（例如单事件 8–16KB，超了给 `… +K bytes truncated`）。这才是「只有一个截断 owner」在 jcode 的正确译法：owner 是工具层（模型看到什么），不是 TUI。 |
| `isConcurrencySafe` 工具级并发分类（`core/tools/src/index.ts:1304-1320`） | jcode 没有 per-tool 并发元数据（`internal/agent/agent.go:168-171` 只配 `ToolsNodeConfig`）。若将来上索引，用 `sync.Mutex` 在 `sessionquery` 里串行化写即可。 |
| `turnBoundary` 投影 + `sessionProjections.stateOf` | jcode 没有投影子系统；用第 7 条的「截到当前轮之前」在工具里算。 |
| `availability: live\|persisted`、`surfaces: current\|shadowed\|log-only` 两个枚举 | jcode 的 `Entry` 没有这些维度（`session.go:263-288`）。不要为对齐 DSH 硬造模型看不懂的枚举——那是纯粹的 schema 税。 |
