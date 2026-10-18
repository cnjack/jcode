# MoonshotAI/kimi-code 跨会话查看能力调研

调研对象：`/Users/jack/workpath/opensource/kimi-code`（只读），HEAD `4a82a07f0`。本文所有结论都带 `file:line` 证据；行号对应上述 checkout。

**现状结论**：kimi-code **没有任何 model-facing 的跨会话查看能力**——模型可调用工具的入参里根本没有 `session_id`（见 §2 的工具清单证据），跨会话查看 100% 是 human-facing / HTTP-facing 的；最接近的类比是 `POST /api/v1/search`（跨会话全文检索，服务端持久索引 + live 双路由）+ `apps/kimi-inspect` 的 Search view：点中一条命中后，前端会 **resume 目标会话**并把聊天视图跳到命中的 `t<turn>.<step>`（`apps/kimi-inspect/src/App.tsx:88-100`）。

仓库里其实有**两条互不相干**的实现：一条是引擎内的搜索索引（`kap-server` + `kimi-inspect`，要 resume 才能看内容），另一条是 `kimi vis`（`apps/vis`）——**只读文件系统浏览器**，直接读 `~/.kimi-code/sessions/**` 起自己的 Hono 服务与 SPA，不经过引擎、不 resume、不写盘（`apps/vis/server/src/app.ts:97-112`、`apps/kimi-code/src/cli/sub/vis.ts:1-8`）。对 jcode 而言，后者是耦合最低、最值得照抄的类比（见 §8.0）。

---

## 1. 能力概览

### 1.1 今天实际能做的事

| 能力 | 载体 | 证据 |
| --- | --- | --- |
| 列出**所有工作区**的会话（游标分页、归档、busy 过滤） | `GET /api/v1/sessions` | `packages/kap-server/src/routes/sessions.ts:279-407` |
| 会话列表 v2：按 workspace / activity 过滤、排序、按 workspace 分组、`include=git`、`fields=id,archived` | `GET /api/v2/sessions` | `docs/en/reference/server-api.md:2236-2262` |
| 读**冷会话**（未加载）的另一条 agent 的 transcript，按 turn 分页 | `GET /api/v1/sessions/{id}/transcript?agent_id=` | `packages/kap-server/src/routes/transcript.ts:123-211` |
| 增量追平 transcript（op 批次，`seq > since_seq`） | `GET .../transcript/ops` | `packages/kap-server/src/routes/transcript.ts:213-260` |
| 用户消息锚点列表 / plan 快照 | `GET .../transcript/user-messages`、`.../transcript/plan` | `packages/kap-server/src/routes/transcript.ts:262-338` |
| 跨会话全文搜索（用户消息 / 助手文本 / 会话标题） | `POST /api/v1/search` | `packages/kap-server/src/routes/search.ts:77-115` |
| 导出会话（zip，含 manifest + wire 扫描 + 日志） | `POST /api/v1/sessions/{id}/export` | `packages/kap-server/src/routes/sessionExport.ts:46-62` |
| 独立只读浏览全部会话（列表 / 详情 / wire / 子 agent 树 / 任务 / cron / 日志 / context 投影），不经过引擎 | `kimi vis`（`apps/vis`） | `apps/kimi-code/src/cli/sub/vis.ts:60-96`、`apps/vis/server/src/app.ts:97-112` |
| 导入别人导出的 debug zip 并在同一 UI 里查看 | `POST /api/imports` + `<home>/imported/<id>/` | `apps/vis/server/src/routes/imports.ts:13-45`、`apps/vis/server/src/lib/import-store.ts:1-14` |
| 人肉浏览：多个会话的列表 + 逐条 transcript + 搜索视图 | `apps/kimi-inspect` | `apps/kimi-inspect/README.md:24-29` |
| TUI 内切换/恢复其它会话 | `/sessions`（`/resume`）、`kimi --session <id>` | `docs/en/guides/sessions.md:57-62`、`docs/en/reference/kimi-command.md:18` |

### 1.2 明确**做不到**的事

1. **模型不能读别的会话**。内置工具集合为 `Agent`（`packages/agent-core-v2/src/agent/tools/agent/agentTool.ts:678`）、`AskUserQuestion`（`.../ask-user-question/askUserQuestionTool.ts:256`）、`Edit`（`.../edit/editTool.ts:116`）、`Bash`（`.../os/bash/bashTool.ts:440`）、`Glob`（`.../os/glob/globTool.ts:330`）、`Grep`（`.../os/grep/grepTool.ts:349`）、`Read`（`.../os/read/readTool.ts:624`）、`Write`（`.../os/write/writeTool.ts:137`）、`FetchURL`（`.../fetch-url/fetchUrlTool.ts:79`）、`WebSearch`（`.../web-search/webSearchTool.ts:115`）、`TaskList/TaskOutput/TaskStop/WaitFor`（`.../task/task-*/`）、`SelectTools`、`ReadMediaFile`。`Agent` 只做**同会话**内的子 agent 派生与 `resume`（`agentTool.ts:741` 明确 `resume=<agentId>`，“NOT task_id”），跨不到 session。
2. **搜索没有 workspace 维度过滤**。查询契约里只有 `container{ session_id, agent_id }`，`workspaceId` 只是**输出字段**（`packages/kap-server/src/search/contract.ts:1-15`、`:33-44`、`packages/kap-server/src/search/docs.ts:7`）；索引枚举会话时不带 workspace 约束（`searchService.ts:371-380`）。因此一次搜索天然横跨所有工作区。
3. **不可搜的东西**：`think` 推理片段不落文档（`wireExtract.ts:203-204` 只推进 turn 计数）、工具调用与工具输出完全不进索引（`wireExtract.ts:141-224` 只认 `context.append_message` / `context.append_loop_event` / `context.undo` 三种记录）、被注入的“伪用户消息”也被排除（`wireExtract.ts:30-46`、`:61-67`）。
4. **没有语义/向量检索**，只有词项倒排 + n-gram 子串（`indexCore.ts:426-435`）。
5. **索引 building 期间搜不到东西**，只返回空页 + `index_state.state='building'`（`searchService.ts:664-682`）。
6. **没有跨会话的实时推送**：v2 WS 事件流已被移除，kimi-inspect 的列表/面板改为轮询与手动刷新（`apps/kimi-inspect/src/App.tsx:1-20`、`apps/kimi-inspect/README.md:55-57`）。
7. **没有跨会话读的审计/血缘**：搜索请求只写 `requestLog`（`routes/search.ts:103`），不会在被查询会话里留任何“被看过”的记录。

---

## 2. 面向对象与表层

### 2.1 Human-facing UI

- **`apps/kimi-inspect`**（开发/调试用 Inspector，需要 loopback + `--debug-endpoints`，`apps/kimi-inspect/README.md:9-13`）：
  - `SearchView`：全宽搜索页，`role` 过滤、`sort`、`exact`（映射到 `literal`）、Load more 分页，并用 badge 显示 `live`/`index`（`apps/kimi-inspect/src/components/SearchView.tsx:1-6`）。
  - 命中点击 → `openSearchHit`：`setSessionId / setAgentId('main' if '') / setView('chat') / setJump({turnId:'t'+turn, stepId})`（`App.tsx:88-100`）。
  - 打开会话会先 `ISessionManager.resume(sessionId)`（`App.tsx:58-78`）——**“看”会导致目标会话被物化到服务进程**（恢复盘上状态、可被后续写）。
  - `ChatView` 处理 jump：从窗口头部往前翻页直到目标 turn 进入窗口，再按 `[data-step-id]` / `[data-turn-id]` 滚动并闪一下（`ChatView.tsx:403-470`）。
  - `ChatSearchBar`：**会话内**搜索，固定 `mode:'literal'` + `container:{sessionId}`（`components/ChatSearchBar.tsx:1-8`、`:67-70`）。
- **`kimi vis`**（`kimi vis [sessionId]`，独立进程 + 独立端口，默认 loopback，`--host` / `--no-open`）：左侧会话栏带搜索框、排序（recent / oldest / most records / most subagents）、health 与 source 过滤以及拖拽导入 zip（`apps/vis/web/src/components/sessions/SessionFilter.tsx:14-57`）；中间是会话详情（wire 记录、context 投影、任务、cron、日志、blob），外加子 agent 树（`apps/vis/server/src/routes/subagents.ts:7-16`）。**全程只读盘，不 resume、不写 session**。wire 视图是“整份读进内存 + 前端子串过滤”（`apps/vis/server/src/routes/wire.ts:29-40`、`apps/vis/web/src/components/wire/WireTab.tsx:85-122`）。
- **`kimi web`**：文档声称有 “Global search … Web searches across sessions and workspaces”（`docs/en/guides/web.md:75`），但该前端源码不在本仓库，只有构建产物 `apps/kimi-code/dist-web/`，且产物里**找不到** `/api/v1/search` 字符串——所以“生产 Web UI 的全局搜索”无法从本仓库核实，可核实的消费者只有 kimi-inspect。

### 2.2 HTTP API 表层

- `POST /api/v1/search`：唯一跨会话检索入口（`routes/search.ts:81`、`:88`）。
- 读其它会话的状态/内容：`GET /api/v1/sessions`、`/sessions/{id}`、`/sessions/{id}/transcript[...]`、`/sessions/{id}/messages`、`POST /sessions/{id}/export`。
- `kimi vis` 是**另一套 HTTP 面**：自己的 Hono 服务（默认 `127.0.0.1:3001`，`PORT`/`VIS_HOST` 可覆盖）挂 `/api/sessions`、`/api/sessions/:id`、`/api/sessions/:id/wire|agents|tasks|cron|logs|blobs|context`、`/api/imports`（`apps/vis/server/src/app.ts:97-112`、`apps/vis/server/src/config.ts:14-37`），与 kap-server 的 `/api/v1/*` 完全无关。
- 注意：搜索**不在** `packages/klient`（daemon 客户端协议）里——`grep -rn "search" packages/klient/src` 无命中。也就是说 IDE/ACP/v2 daemon 客户端拿不到这个能力，它只是 v1 REST 的旁挂接口。

### 2.3 Model-facing

**不存在**。证据见 §1.2 第 1 条。跨会话能力对模型完全不可见；模型最多通过 `Read`/`Grep` 读文件系统，而 session 目录在 `~/.kimi-code/sessions/...`（默认 workspace cwd 之外），因此除非用户显式开启权限并手工给路径，模型也读不到。这是 kimi-code 与 “agent 主动巡查历史会话” 类产品最大的能力缺口。

---

## 3. 存储与索引

### 3.1 盘上布局

```
$KIMI_CODE_HOME/                     # 默认 ~/.kimi-code，可用 KIMI_CODE_HOME 改
├── session_index.jsonl              # 会话索引（每行 sessionId/sessionDir/workDir）
├── search-index/                    # 全局全文索引（MiniDb 目录）
└── sessions/<workspaceId>/<sessionId>/
    ├── state.json                   # 标题 / lastPrompt / 时间戳 / forkedFrom
    ├── session-meta/state.json      # 旧版布局，标题读取会两处都试
    ├── agents/<agentId>/wire.jsonl   # 每个 agent 一份 append-only 事件流
    └── ... (plans/ tasks/ cron/ logs/)
```

- 布局文档：`docs/en/configuration/data-locations.md:38`、`:73-79`；`docs/en/guides/sessions.md:9-25`。
- 目录名 = `workspaceId`（形如 `wd_<slug>_<sha256 前 12 位>`），由 `workspacePersistenceScope` + `sessionDirOf` 拼出（`packages/agent-core-v2/src/workspace/sessionLifecycle/internal/addressing.ts:3-13`，被 `searchService.ts:282-287` 复用）。
- 索引目录固定为 `join(bootstrap.homeDir, 'search-index')`（`searchService.ts:57`、`:252-254`），`homeDir = KIMI_CODE_HOME ?? ~/.kimi-code`（`packages/agent-core-v2/src/app/bootstrap/bootstrap.ts:169-173`）。

### 3.2 索引不是 SQLite，是自研 `@moonshot-ai/minidb`

- `minidb` 是零依赖纯 TS 的嵌入式 KV：Redis 式 WAL + group commit + 快照/压缩 + 跳表二级索引 + 文本倒排（`packages/minidb/README.md:1-25`）。
- 打开参数：`valueCodec:'json'`、`valueMode:'disk'`、`fsyncPolicy:'everysec'`、`onLockFail:'readonly'`（`indexCore.ts:470-481`）。落盘文件含 `db.wal` / `db.snapshot` / `db.textindexes.json` / `db.lock`（`indexCore.ts:518-529`、`:459-468`）。
- 文本倒排：每个 term 的 posting list 作为一条 append-only 记录落在 flat file，内存只留 `term -> {off,len,df}` 字典；payload 是 delta+varint 编码的 `(docID,freq)`，CRC32 校验（`packages/minidb/src/text-postings.ts:1-24`、`:66-88`）。
- 打分是 **TF-IDF**：`score += (f / docLen) * idf(df)`（`packages/minidb/src/text-index/index.ts:776-814`）。
- 分词：小写 Latin 词 + CJK **uni-gram 和 bi-gram**，零依赖、不依赖中文分词器（`packages/minidb/src/text-index/tokenize.ts:15-40`）。

### 3.3 两个文本索引：`body` 与 `tri`

`publishDb` 在可写打开时建两个文本索引（`indexCore.ts:422-436`）：

- `body`：默认分词器，给 `terms` 模式用；
- `tri`：`tokenizer:'ngram'`，给 `literal` 精确子串模式当候选集用。

n-gram 侧：2/3-gram 各自哈希进 2^k 桶（crc32，不同宽度带不同前缀防别名），`normalizeLiteral = NFKC + lowercase`（`packages/minidb/src/trigram.ts:36-53`）。候选出来后再用 `normalizeLiteral(doc.text).indexOf(expected)` 逐条确认，所以“零假阳性”（`match.ts:159-164`）。

### 3.4 文档模型与哪些字段被索引

`SearchDoc = MessageDoc | TitleDoc | FileMetaDoc | SessionMetaDoc | StatsDoc`（`packages/kap-server/src/search/docs.ts:76`）：

| kind | key | 索引进 `text` 的内容 |
| --- | --- | --- |
| `message` | `${sessionId}/${agentId}/${source}:${lineOffset}:${i}` | 用户**手打**文本 / 助手 `text` frame |
| `title` | `${sessionId}/$title` | 会话标题（来自 `state.json`） |
| `fileMeta` | `\0meta\file\<sessionId>\<sha256(path)[:32]>` | 不索引，只存增量水位（`offset/size/mtimeMs/ino/turnState/stepState`） |
| `sessionMeta` | `\0meta\session\<sessionId>` | 不索引，存 `dir/identity/syncSource/sourceUpdatedAt` |
| `stats` | `\0meta\stats` | 不索引，存计数与 `degraded` |

key 生成见 `indexCore.ts:1061-1065`、`:1398-1400`、`:786-806`、`:42-56`；字段级索引只对 `text` 建（`indexCore.ts:427-428`），`sessionTitle` 只是冗余展示字段。

**截断**：单条文档 `text` 上限 `MAX_DOC_TEXT_CHARS = 20_000`（`docs.ts:1`），超长直接 `slice` 丢尾（`indexCore.ts:1053`，live 路由同样 `searchService.ts:496`、`:518`）。没有分块（chunking）/窗口。

**排除**：媒体路径占位文本（`wireExtract.ts:79-90` 的 `matchSingleMediaPathTag`）、`injection/system_trigger/retry/compaction_summary` 等非用户来源（`wireExtract.ts:30-46`、`:61-67`）、`system` 角色消息（`wireExtract.ts:116`）。

### 3.5 增量同步：byte-offset 水位

- 每个 `wire.jsonl` 的游标就是**字节偏移**，批大小 `WIRE_BATCH_OPS=1000`、读块 1MiB、单轮预算 64MiB / 30s、单轮会话上限 500、失败会话 5 次后跳过 300s（`indexCore.ts:62-69`、`:936-1003`、`:642-686`）。
- 失效判定：文件变小 / inode 变了 / 同尺寸但 mtime 变新（原地重写）/ 老格式无 `stepState` → 删掉该文件的全部文档后从 0 重扫（`indexCore.ts:901-911`、`:849-854`）。
- 去重：先 `sessionSourceUnchanged` 比 `dir/agents/state.json` 的 mtime + 每个 wire 的 `size/mtime/ino`，全一致就整会话跳（`indexCore.ts:813-832`、`:130-141`）。
- 删除：会话从 `ISessionIndex.listRecent` 消失 → 删该会话全部文档（`indexCore.ts:628-632`、`:720-728`）；文件消失 → 删该 agent+source 前缀的文档（`indexCore.ts:766-772`）。
- turn/step 编号：索引时用 `TurnCounterState` / `StepTrackerState` 在**跨轮次**间持久化（存在 fileMeta 里），`context.undo` 回退 anchor、`step.begin` 记账（`indexCore.ts:191-245`、`:1021-1068`），最终拼成 `stepId = \`t${turn}.${step}\``（`indexCore.ts:1056-1059`）。

### 3.6 snippet 与高亮

只返回纯文本 snippet，**不返回高亮偏移**：`makeSnippet(text, query, radius=80)` 取最早出现的 term，前后各 80 字符、空白折叠、加 `…`（`packages/kap-server/src/search/snippet.ts:15-47`）。`literal` 模式把候选确认时的位置当 anchor 传进去居中（`searchService.ts:635-640`）。UI 直接 `whitespace-pre-wrap` 渲染纯文本（`components/SearchView.tsx:245`）。

### 3.7 游标分页

- 分页是 **keyset**：边界宽度随排序变化（score 排序 3 元组 `(score,time,key)`，其余 2 元组 `(time,key)`）（`match.ts:41-49`、`:71-73`）；页内取 top-K 用二叉堆 `RowTopK`（`match.ts:90-133`）。
- token = base64url(JSON `{v:2, f:<查询指纹>, g:<索引 generation>, b:<boundary>}`)（`match.ts:210-221`）。
- 指纹 = sha256( query/mode/op/container/role/time/sort/source ) 的 base64url 前 16 字符（`match.ts:194-208`）；`generation` = `bootSalt:counter`，重建/重开索引会变（`indexCore.ts:418-420`）。
- 解码失败/条件变化/generation 变 → `invalid_page_token`（`match.ts:223-271`）；兼容 v1 的 offset token（`match.ts:246-251`）。

### 3.8 live vs index 双路由

```
// packages/kap-server/src/search/searchService.ts:402-410
const sessionId = q.container?.sessionId;
const liveStore = sessionId !== undefined ? this.liveSource?.forSessionLive(sessionId) : undefined;
if (liveStore !== undefined && sessionId !== undefined) {
  return this.searchLive(q, sessionId, liveStore, input.pageToken);
}
return this.searchIndex(q, input.pageToken);
```

- 只有**指明了 `container.session_id` 且该会话在本进程里活着**才走 live（直接扫内存 `TranscriptStore`，`searchService.ts:412-462`、`:464-544`）；其它情况一律走索引。
- 结论：**跨会话搜索永远是索引**，live 只是“会话内搜索”的快路径。响应里 `source:'live'|'index'` 明示走了哪条（`contract.ts:55`、`:65`）。
- transcript 侧有同构的双路由：内存 store 命中就直接 `paginateTurns`，否则 `readColdSnapshot` 从盘上 `wire.jsonl` 重建（`routes/transcript.ts:147-208`、`services/transcript/transcriptService.ts:543-572`）。

### 3.9 写入者唯一：worker 线程 + read-only 退化

- 默认把索引放在独立的 `worker_threads` 里跑（`SearchWorkerHost`），可配置退回 `InlineSearchBackend`（`searchService.ts:256-274`）。worker 有 ready/close/request/sync 超时、崩溃退避、`maxOldSpace=1024MB`（`worker/host.ts:32-38`、`:72-80`）。
- MiniDb 的写锁由 `db.lock`（pid+token）表达；拿不到锁就以 `readonly` 打开，并靠 `db.wal` 追平（`indexCore.ts:470-481`、`:550-575`），`reindex()` 在只读实例上直接抛 `readonly_index`（`indexCore.ts:1258-1285`）。

### 3.10 第二条路线：`kimi vis` 的只读文件系统旁路

- 独立 Hono 服务 + 构建期内嵌的单文件 SPA；`kimi vis` 按需动态 `import('@moonshot-ai/vis-server/start')`，正常 `kimi` 启动不背这个包（`apps/kimi-code/src/cli/sub/vis.ts:60-80`、`:130-136`）。
- **会话发现不依赖任何索引**：直接枚举 `sessions/<bucket>/<sessionId>/`（`apps/vis/server/src/lib/session-store.ts:42-70`）。`state.json` 读不出来也照样返回 `SessionDetail`（`health:'broken_state'`），因为 wire 探测与 state 无关，坏 state 的会话仍能看 wire（`session-store.ts:79-89`、`:195-225`）。
- 路径安全：`SESSION_ID_RE` / `isSafeAgentId` 双重校验（`session-store.ts:10-19`）；`findSessionDir` 只信 `session_index.jsonl` 里**落在 `<home>/sessions/` 下且 basename 等于请求 id** 的条目，脏索引无法把读操作重定向到任意目录（`session-store.ts:340-357`）。
- 导入（hydrate）：把 `/export-debug-zip` 的产物解到 `<home>/imported/imp_<12hex>/`，**目录形状与真实 session 完全一致**，于是 wire / context / tasks / cron / blobs / logs 全部复用同一条读路径（`apps/vis/server/src/lib/import-store.ts:1-14`、`:30-60`）；上传 zip 上限 500 MiB（`apps/vis/server/src/routes/imports.ts:9-16`）。
- context 投影：用 `agent-core-v2` 的 contextOps / compaction 纯函数把 wire 折叠成“模型当时真正看到的上下文”（含 undo、compaction 气泡与 token 计数，`apps/vis/server/src/lib/context-projector.ts:1-30`）。这一点很关键——**wire 重放与 agent 共用同一套纯函数**，不是另写一个解释器。
- 缺点：**没有任何全文搜索**，只有会话列表/日志/wire 的客户端子串过滤；wire 一次性 JSON 返回（`routes/wire.ts:29-40`）。

---

## 4. 授权与边界

### 4.1 认证：一把 bearer token，全服务器通行

- `onRequest` 钩子顺序：Host 校验 → Origin/CORS 校验 → Bearer 校验（`packages/kap-server/src/start.ts:302-317`）。
- Bearer 只比对 `authorization: Bearer <token>`，失败 401 + 计数，超限 429（`middleware/auth.ts:48-89`）；带 `Authorization` 头在进入日志前被替换为 `[redacted]`（`auth.ts:76-78`）。
- 绕过白名单只有 `OPTIONS`、`GET /api/v1/healthz` 和非 `/api/*`、非 openapi 路径（`auth.ts:32-46`）。
- **没有** session/workspace 级别的 ACL、没有 per-token 作用域——一个 token 就能搜全部会话、导出任意会话。

### 4.2 暴露面：回环即边界

- `classify(host)` 分 `loopback/lan/public`（`security/bindClassify.ts:56-80`）；非回环默认**拒绝启动**（除非 `--insecure-no-tls`），并把 `terminals`、`shutdown`、`debug-endpoints` 关掉（`start.ts:160-169`）；非回环还会 warning“只有 banner 里的 bearer token 保护你”（`start.ts:257-268`）。
- **`/api/v1/search` 不在这些限制里**——非回环 + 有 token 就能跨会话搜全机器内容（`start.ts:169` 只 gate debug/terminals/shutdown）。
- Host 头白名单防 DNS rebinding（`middleware/hostnames.ts:61-95`），Origin 允许同 host/loopback/.localhost，其余靠 `KIMI_CODE_CORS_ORIGINS`（`middleware/origin.ts:34-50`）。

### 4.3 工作区作用域：会话列表有，搜索没有

- 列表接口支持 `workspace_id` 过滤并解析 alias（`routes/sessions.ts:299-315`）。
- 搜索查询结构里没有 workspace 字段（`search/contract.ts:1-15`），索引 `listAllSessions()` 直接取全部（`searchService.ts:371-380`）。**因此“跨会话”实际上是“跨工作区”**，唯一的信息屏障是进程 home 目录。
- kimi-inspect 的搜索 UI 也不提供 workspace 选择（`SearchView.tsx` 只有 role/sort/exact）。

### 4.4 其它边界手段

- 冷读 transcript 前会校验 `agent_id` 是 plain id（不含路径分隔符），避免路径穿越（`routes/transcript.ts:49-60`、`transcriptService.ts:549-551`）。
- 索引读取前会用 `state.json` 的 `dir/identity` 校验来源目录身份（`dev:ino:birthtimeNs`，`indexCore.ts:85-95`、`:1221-1242`），防“会话被删/替换后仍展示旧命中”。
- 删除/重建只有文件级粒度（`deleteFileDocs`/`deleteSessionDocs`），没有内容级 redaction 钩子。

### 4.5 Redaction 现状（重要缺口）

- 只有两处 redaction：HTTP `authorization` 头（`auth.ts:76-78`）和日志格式化器（`packages/agent-core-v2/src/_base/log/formatter.ts:54-108`）。
- **搜索文档没有任何脱敏**：用户 prompt 里的密钥会原样进索引、原样出现在 `snippet` 里。kimi-code 自己的 `sessionExport` 也不脱敏（`app/sessionExport/sessionExportService.ts` 收集会话日志/全局日志/wire），文档只提示“默认打包会话日志，不想共享全局日志用 `--no-include-global-log`”（`docs/en/configuration/data-locations.md:95`）。
- 可对比：agent 侧 `promptMetadataText.ts:40-48` 对 `sk-…`/长 base64 有 redaction，但那是 prompt 元数据路径，不在索引路径上。

### 4.6 `kimi vis` 的边界

- 默认绑 `127.0.0.1:3001`（`apps/vis/server/src/config.ts:14-37`）；**非 loopback 绑定时必须提供 `VIS_AUTH_TOKEN`，否则启动直接抛错**（`config.ts:51-60`）。
- 有 token 时 `/api/*` 全量要求 `Authorization: Bearer`，用 `timingSafeEqual` 比较（`apps/vis/server/src/app.ts:67-99`）；SPA 侧会把 token 从 URL 里剥掉（`apps/vis/web/src/api.ts:34-53`）。
- 同样**没有 workspace / 会话级 ACL**——token 即全量读权限；但只读 + 默认 loopback，风险面显著小于带写能力的 kap-server。

---

## 5. 查询语义

### 5.1 两种模式

| | `terms`（默认） | `literal` |
| --- | --- | --- |
| 规范化 | `tokenize()`：Latin 词 + CJK uni/bi-gram，去重（`searchService.ts:113-120`） | `normalizeLiteral` = NFKC + lowercase（`match.ts:119`） |
| 匹配 | 倒排 + `op:AND/OR`（默认 AND）（`indexCore.ts:1133-1137`） | n-gram 候选 + 逐条 `indexOf` 确认（`indexCore.ts:1126-1132`、`match.ts:159-164`） |
| 排序 | `score`（默认）/`time_desc`/`time_asc` | **永远 time desc**，忽略 `sort`（`docs/en/reference/server-api.md:2226`、`match.ts:61-69`） |
| 长度约束 | ≤ 32 个 term（`searchService.ts:121-126`） | 索引路由 2..1024 字符（`searchService.ts:560-574`）；**live 路由允许 1 字符**（`test/search/searchService.test.ts:2141`） |
| 打分 | TF-IDF（`minidb/src/text-index/index.ts:800-813`） | score 恒为 0（`match.ts:164`） |

### 5.2 过滤与排序的落点

- `container{session_id,agent_id}`、`role`、`start_time`、`end_time` 都在 `matchDocs` 里做**后置过滤**（`match.ts:150-158`），不在倒排里做——索引本身没有这些维度。
- 排序 tie-break：score 排序时 `score desc → time desc → key asc`（`match.ts:62-64`）；keyset 边界与比较函数严格对应（`match.ts:71-88`），保证不重不漏（测试 `test/search/searchService.test.ts:1605-1646`）。

### 5.3 预算与“不完整”语义

服务层硬编码预算（`searchService.ts:57-66`、`:546-554`）：

| 预算 | 值 | 触发后果 |
| --- | --- | --- |
| `pageSize` | 1..50，默认 20 | 越界 → `invalid_query`（`searchService.ts:127-130`） |
| `MAX_POSTINGS_VISITS` | 250_000 | `incomplete:'postings_budget'`（`indexCore.ts:1157-1171`） |
| `MAX_TEXT_HITS` | 100_000 | `incomplete:'candidate_cap'` |
| `LITERAL_CANDIDATE_CAP` | 10_000 | 同上 |
| `QUERY_DEADLINE_MS` | 500 | `incomplete:'deadline'`，**返回部分结果**（`match.ts:147-149`、`indexCore.ts:1216-1220`） |
| `QUERY_TEXT_BUDGET_CHARS` | 16_000_000 | 超了也报 `deadline`（`match.ts:160-161`） |

即：**慢查询不会 5xx，而是返回一个被截断的页 + `incomplete` 标记**。这是最容易抄、也最容易被忽略的语义。

### 5.4 错误码

| 情况 | `reason` | 线格式 |
| --- | --- | --- |
| 空 query / >32 terms / pageSize 越界 / literal 太短或太长 | `invalid_query` | `40001 VALIDATION_FAILED`（`routes/search.ts:96-101`） |
| token 畸形 / 指纹不匹配 / generation 过期 | `invalid_page_token` | `40001`（同上；`match.ts:234-260`） |
| 索引只读时调 `reindex` | `readonly_index` | 走 catch-all → `50001 INTERNAL_ERROR` |
| 索引打不开 / 服务已 dispose | `index_unavailable` | `50001`（`indexCore.ts:1070-1090`） |
| 任何其它异常 | — | `50001` + `requestLog().error`（`routes/search.ts:103-111`） |

注意：`readonly_index` / `index_unavailable` **没有专门的错误码**，被兜底成 `INTERNAL_ERROR`；索引坏掉时只有搜索失败，列表/创建/读取仍然工作（`test/search/searchRoute.test.ts:212-290` 断言 50001，并且 `/sessions`、`/sessions/{id}`、`/messages` 仍 200）。

### 5.5 会话列表语义（跨会话浏览的另一半）

- `GET /api/v1/sessions`：`before_id` 与 `after_id` 互斥（`routes/sessions.ts:151-160`）；**不带 `page_size` 且不是 `archived_only` 时返回单窗口、`has_more` 恒 false**（`routes/sessions.ts:366-389`，文档同款提醒 `docs/en/reference/server-api.md:678`）。
- 会话索引自己也带扫描缓存：`session-meta/state.json` 的 mtime/size 作为缓存键，存在 `.index-cache/scan.json`（`app/sessionIndex/sessionIndexSource.ts:9-34`）。

---

## 6. 关键代码地图

| 位置 | 角色 |
| --- | --- |
| `packages/kap-server/src/routes/search.ts:77-115` | 注册 `POST /search`，把 domain `GlobalSearchPage` 投影成 snake_case 线格式并映射错误码 |
| `packages/kap-server/src/protocol/rest-search.ts:3-19` | 请求体 zod：`query/mode/op/container/role/start_time/end_time/sort/page_size(1..50)/page_token` |
| `packages/kap-server/src/protocol/rest-search.ts:35-49` | 响应 zod：`items[]/has_more/page_token?/incomplete?/index_state/source` |
| `packages/kap-server/src/search/contract.ts:1-66` | domain 层契约：Query/Hit/IndexState/Incomplete/Page + `GlobalSearchError` |
| `packages/kap-server/src/search/searchService.ts:113-144` | `normalizeQuery`：模式、term 去重、32 term 上限、pageSize 校验、默认 sort=score |
| `packages/kap-server/src/search/searchService.ts:256-274` | 选择 backend：worker 线程 vs inline；读取 `databaseSearchEnabled` 与 tuning |
| `packages/kap-server/src/search/searchService.ts:371-380` | `listAllSessions()`：500/页翻完 `ISessionIndex`，无 workspace 约束 |
| `packages/kap-server/src/search/searchService.ts:402-410` | live/index 路由分叉 |
| `packages/kap-server/src/search/searchService.ts:627-646` | `projectHit`：snippet 生成（literal 用 anchor 居中） |
| `packages/kap-server/src/search/searchService.ts:648-682` | `composeIndexState` / `buildingPage`：building/stale/degraded 语义 |
| `packages/kap-server/src/search/indexCore.ts:390-457` | 打开索引、建两个文本索引、计算 fingerprint、发布 generation、读 lock token |
| `packages/kap-server/src/search/indexCore.ts:470-494` | `MiniDb.open` 参数；`classifyStorageError==='rebuild'` 时 `wipeStoreDir` 重建 |
| `packages/kap-server/src/search/indexCore.ts:577-703` | 一轮 sync：删消失会话 → 逐会话增量 → 写 `stats` → `fullSyncDone` |
| `packages/kap-server/src/search/indexCore.ts:730-811` | `syncSession`：目录身份/title/文件集合差异/标题文档 |
| `packages/kap-server/src/search/indexCore.ts:856-1019` | `syncWireFile`：字节水位、分块读、批提交、失效重扫 |
| `packages/kap-server/src/search/indexCore.ts:1021-1068` | `collectWireLine`：wire 行 → `MessageDoc`（turn/step 归属） |
| `packages/kap-server/src/search/indexCore.ts:1070-1256` | `search`：page token 校验、bounded 检索、后置过滤、来源身份校验、分页 |
| `packages/kap-server/src/search/indexCore.ts:1378-1396` | `collectWireFiles`：根 `wire.jsonl`(legacy, agent='main') + `agents/**/wire.jsonl` |
| `packages/kap-server/src/search/match.ts:137-170` | `matchDocs`：container/role/time/boundary 过滤 + literal 确认 + deadline/text 预算 |
| `packages/kap-server/src/search/match.ts:194-271` | 查询指纹、token 编解码、v1→v2 兼容 |
| `apps/kimi-code/src/cli/sub/vis.ts:60-96` | `kimi vis`：起独立只读服务、打印 URL、支持 `sessions/<id>` 深链 |
| `apps/vis/server/src/app.ts:67-112` | vis 路由表（sessions/detail/wire/subagents/blobs/tasks/cron/logs/imports/context）+ Bearer 中间件 |
| `apps/vis/server/src/lib/session-store.ts:42-70`、`:340-357` | 扫盘枚举会话 + 安全的 session 目录解析 |
| `apps/vis/server/src/lib/import-store.ts:1-60` | debug zip 导入为 `imported/<id>/`，目录形状对齐真实 session |
| `apps/vis/server/src/lib/context-projector.ts:1-30` | 用引擎同源纯函数把 wire 折叠成“模型当时看到的上下文” |
| `packages/kap-server/src/search/wireExtract.ts:30-46` | 哪些 origin 算“用户手打”/隐藏/marker |
| `packages/kap-server/src/search/wireExtract.ts:113-139` | turn 开合语义（user 开新 turn，assistant `ensure`） |
| `packages/kap-server/src/search/snippet.ts:15-47` | snippet 窗口（radius 80、空白折叠、`…`） |
| `packages/kap-server/src/search/docs.ts:1-76` | 文档模型与 20k 截断常量 |
| `packages/kap-server/src/search/worker/host.ts:32-95` | worker 错误码、锁 token 登记、超时/内存默认值 |
| `packages/kap-server/src/routes/sessions.ts:279-407` | `GET /sessions` 列表（游标/归档/busy/单窗口特例） |
| `packages/kap-server/src/routes/transcript.ts:123-211` | transcript 页：live 走内存 store，cold 走 `readColdSnapshot` |
| `packages/kap-server/src/services/transcript/transcriptService.ts:543-572` | 冷读：拼 `<home>/sessions/<workspaceId>/<sessionId>/agents/<agentId>/wire.jsonl` 并重建 |
| `packages/transcript/src/pagination/paginate.ts:15-28` | `paginateTurns`：按 turn 切段，`before_turn`/`after_turn` 两个方向 |
| `packages/minidb/src/text-index/index.ts:776-814` | TF-IDF 打分 + AND/OR 候选合并 |
| `packages/minidb/src/trigram.ts:36-53` | `normalizeLiteral` + n-gram 桶编码 |
| `apps/kimi-inspect/src/App.tsx:58-100` | 选中会话先 `resume`，搜索命中转成 `ChatJump{turnId,stepId}` |
| `apps/kimi-inspect/src/components/ChatView.tsx:403-470` | 命中跳转：往前翻页直到目标 turn 进窗口，再滚动/闪烁 |
| `apps/kimi-inspect/src/search/api.ts:44-52`、`:152` | 客户端只识别 `incomplete='candidate_cap'`，丢掉 `postings_budget`/`deadline` |

### 6.1 五处最关键代码

**（1）live/index 双路由——跨会话搜索永远走索引**

```ts
// packages/kap-server/src/search/searchService.ts:404-409
const sessionId = q.container?.sessionId;
const liveStore = sessionId !== undefined ? this.liveSource?.forSessionLive(sessionId) : undefined;
if (liveStore !== undefined && sessionId !== undefined) {
  return this.searchLive(q, sessionId, liveStore, input.pageToken);
}
return this.searchIndex(q, input.pageToken);
```

**（2）双文本索引：一份词项、一份 n-gram**

```ts
// packages/kap-server/src/search/indexCore.ts:426-435
for (const [name, options] of [
  [TEXT_INDEX_NAME, { fields: ['text'] }],
  [TRI_INDEX_NAME, { fields: ['text'], tokenizer: 'ngram' }],
] as const) {
  try {
    await next.createTextIndex(name, options);
  } catch (error) {
    if (!(error instanceof Error && error.message.includes('already exists'))) throw error;
  }
}
```

**（3）分页 token 绑定“查询条件指纹 + 索引 generation”**

```ts
// packages/kap-server/src/search/match.ts:218-220
return Buffer.from(
  JSON.stringify({ v: PAGE_TOKEN_VERSION, f: tokenFingerprint(q, source), g: generation, b: boundary }),
).toString('base64url');
```

**（4）turn 归属规则（决定命中能不能跳回去）**

```ts
// packages/kap-server/src/search/wireExtract.ts:133-138
if (typeof kind === 'string' && MARKER_USER_ORIGINS.has(kind)) {
  if (origin !== undefined && isUserSlashPrompt(origin)) return { kind: 'open', anchor: true };
  return NONE;
}
const anchor = kind === undefined || kind === 'user';
return { kind: 'open', anchor };
```

**（5）查询期按目录身份过滤“已删除/已替换”的会话**

```ts
// packages/kap-server/src/search/indexCore.ts:1238-1242
if (identities.get(meta.dir) === row.value.sessionIdentity) {
  visible.push({ ...row, value: { ...row.value, sessionTitle: meta.title ?? '' } });
} else {
  freshnessStale = true;
}
```

---

## 7. 失败模式与限制

1. **长消息丢尾**：>20k 字符直接截断，没有分块（`docs.ts:1`、`indexCore.ts:1053`）。长报告/大代码块后半段永远搜不到。
2. **snippet 锚点可能偏**：`literal` 模式下 `at` 是在 `normalizeLiteral(doc.text)`（NFKC+lowercase，可能改变长度）上算的，却被拿去切**原文**（`match.ts:162-164` + `snippet.ts:23-35`）。全角/兼容字符在命中点之前出现时，窗口会偏移。
3. **索引滞后**：同步是 2s debounce + 分轮预算（`searchService.ts:211`、`indexCore.ts:634-637`），刚写完的会话内容搜不到；`index_state.stale` 只是弱提示。
4. **静默截断**：`candidate_cap`/`postings_budget`/`deadline` 只体现在响应字段（`contract.ts:57`），而参考客户端只认 `candidate_cap`（`search/api.ts:152`），UI 上看不出 `deadline` 截断 → 用户会以为“真的只有这些结果”。
5. **undo 不删索引**：`collectWireLine` 对 undo 只回退 turn 计数、从不 `del` 已写入文档（`indexCore.ts:1021-1068`、`:191-206`），被 undo 的内容仍可被搜到，且其 `turn` 号会被后续 turn 复用 → 命中跳转可能落到错误 turn。
6. **`context.clear` / compaction 不进索引**：`analyzeWireLine` 只识别三种记录（`wireExtract.ts:141-224`），而冷读 transcript 是识别 `context.clear` 的（`transcriptService.ts:596-599`）→ 索引与真实 transcript 在 `/clear`、压缩之后会分叉。
7. **building 期空结果**：索引首次构建或文本索引重建时整页为空（`searchService.ts:664-682`、`indexCore.ts:1113-1115`），客户端没有“稍后自动重试”的约定，只靠人再点一次。
8. **单写者**：第二个服务实例只能只读并靠 WAL 追平（`indexCore.ts:470-481`、`:550-575`），`reindex` 在只读实例上直接报 `readonly_index`（`:1258-1285`）——多实例/多用户部署下体验很差。
9. **损坏即全量重建**：`classifyStorageError==='rebuild'` 时 `wipeStoreDir` 后重扫全部会话（`indexCore.ts:482-493`、`:602-608`），并让所有旧 page token 失效。
10. **每会话每轮 stat 成本**：`sessionSourceUnchanged` 对每个会话 stat 目录 + `agents/` + `state.json` + 每个 wire 文件（`indexCore.ts:813-832`、`:130-141`），数千会话时是持续的元数据风暴；`syncSessionCap=500` 只是限制“真正解析”的会话数。
11. **无 workspace/ACL 边界**（§4.3）：多项目 home 下，A 项目会话能搜出 B 项目内容与 `workspace_id`；非回环部署时只靠一把 bearer。
12. **无内容级删除**：只有文件/会话级 doc 删除（`indexCore.ts:720-728`、`:849-854`），没有“某个 prompt 不许被搜”的能力；`export` 同理。
13. **live 路由能力窄**：只有 `container.session_id` 且本进程活着才可用（`searchService.ts:404-408`），所以“跨会话实时流式查看”在这个设计里不存在。
14. **inspector 的“看”= resume**：打开会话触发 `ISessionManager.resume`（`App.tsx:58-78`），会做恢复、可能触发副作用；只读浏览没有独立的 read-only 路径。
15. **vis 把整份 wire 一次性返回**（`apps/vis/server/src/routes/wire.ts:29-40`），record 数上万的会话会把整份 JSON 打进浏览器再在前端过滤（`WireTab.tsx:85-122`），没有分页/流式。
16. **vis 完全没有跨会话全文检索**（只有列表过滤），kap-server 有检索但没有只读浏览——两条路线各自缺一半，这也是它们同时存在的原因（`apps/kimi-inspect` 需要引擎活着，`kimi vis` 只需要磁盘）。

---

## 8. 对 jcode 的启示

### 8.0 最重要的结论：优先照抄 `kimi vis` 的“只读旁路”，而不是 kap-server 的搜索索引

kimi 的两条路线里，`kimi vis` 与 jcode 的现状最同构：jcode 已经有完整的读盘函数（`session.LoadSession`、`session.ListSessions`、`subagents/agent-<id>.jsonl` 路径，`internal/session/session.go:970-980`、`:1788-1805`），缺的只是一个**只读 HTTP 面 + 前端**。对应关系几乎是 1:1：

| kimi | jcode 对应物 | 备注 |
| --- | --- | --- |
| `kimi vis` 起独立只读服务（`cli/sub/vis.ts:60-96`） | 新增 `jcode vis`（或 `jcode web --readonly`） | 独立进程，不加载 provider/MCP/automation |
| 扫 `sessions/<bucket>/<id>/` 枚举（`session-store.ts:42-70`） | `~/.jcode/sessions/*.json` + `session.json` 索引 | jcode 的索引已经按 project 分好（`session.go:451-454`） |
| 读单个 agent 的 `wire.jsonl` 并做 context 投影 | 读 `Entry[]` + `ReconstructState`（`internal/session/history.go:149`） | jcode 已有 `ReconstructState`/`PruneOldToolOutputs` |
| 导入 debug zip 到 `imported/<id>/`（`import-store.ts:1-14`） | 导入别人导出的 session JSON | 目录/文件形状对齐后复用全部读路径 |
| 默认 loopback，非 loopback 强制 token（`config.ts:51-60`） | 复用现有 `POST /api/auth/verify` | 再加 Host/Origin 校验 |
| 子 agent 树（`routes/subagents.ts:7-16`） | `subagents/` 目录扫描 | team/automation 已有子会话概念 |

所以建议的落地顺序是：**先做 `jcode vis`（只读旁路，1 个 handler 文件 + 1 个页面）→ 再做搜索索引（§8.1）→ 最后才考虑把搜索能力交给模型（§8.3）**。

### 8.1 抄什么

1. **“索引落盘 wire，而不是内存 transcript”**——kimi 的索引直接扫 `sessions/<ws>/<sid>/agents/<agent>/wire.jsonl`，按字节水位增量推进（`indexCore.ts:856-1019`、`:1378-1396`），所以**冷会话、别的 agent、甚至服务刚启动**都能立刻搜到，不需要 resume。jcode 现状是 `GET /api/sessions` 只列**当前项目**（`internal/web/sessions.go:242-277` → `internal/session/session.go:1788-1805` 的 `idx.Sessions[project]`），`GET /api/sessions/{id}` 一次性返回整份 entries（`internal/web/sessions.go:278-287`），既没有跨项目视图也没有分页。**第一步应当是：为 `~/.jcode/sessions/*.json` + `subagents/agent-*.jsonl` 建一个旁挂的增量索引（扫描器 + offset/ino/mtime 水位），而不是改动写入路径。**
2. **“词项倒排 + n-gram 精确子串”双索引**。这是同一份文本上用两个文本索引解决“相关度排序”和“Ctrl+F 式零假阳性”两种需求（`indexCore.ts:426-435`、`trigram.ts:36-53`）。Go 侧对应物就是 SQLite **FTS5**（`unicode61`/`trigram` tokenizer 各建一张表，或一个 `fts5(..., tokenize='trigram')` 表做 literal）。中文用 uni/bi-gram 免分词（`tokenize.ts:15-40`），对 jcode 的 CJK 用户直接生效；不要引入分词器依赖。
3. **live/index 双路由 + 响应里写明 `source`**（`searchService.ts:402-410`、`contract.ts:55-65`）。jcode 的会话内容在内存里就是 `[]Entry`，做“会话内搜索”可以纯内存扫（对应 live 路由），跨会话再走索引。UI/agent 都能据此判断“我看到的是不是实时的”。
4. **keyset 分页 + 查询指纹 + 索引 generation**（`match.ts:194-271`）。三件事一次解决：翻页不重不漏、中途改条件立刻报错、重建索引后旧 cursor 自动失效。jcode 目前 `GET /api/sessions` 连 `limit/offset` 都没有（`internal/web/sessions.go:242-277` 全量数组），建议直接照这个形状设计：`page_token = base64({f: fingerprint, g: generation, b: [sortKey…]})`，并明确区分 `invalid_query(40001)` 与 `index_unavailable(50001)`。
5. **给结果一个稳定的深链坐标 `t<turn>` / `t<turn>.<step>`**（`indexCore.ts:1056-1059` + `App.tsx:96-97` + `ChatView.tsx:403-470` 的“往前翻页直到命中进窗口”）。jcode 的 `Entry` 只有 `uuid`/`tool_call_id`/`batch_id`，没有 turn/step 序号（`internal/session/session.go:263-291`），跨会话跳转没有坐标可用。建议在写 session 时（或索引时）派生 `turn`/`step` 序号并随命中返回 `turn`/`step_id`，前端沿用“反向按页加载 + scrollIntoView”的跳转算法——这比要求服务端支持随机访问简单得多。
6. **预算化查询 + `incomplete` 标记，而不是超时 5xx**（`searchService.ts:57-66`、`match.ts:137-170`）。`deadline/postings_budget/candidate_cap` 三个 flag 让调用方知道“这页不全”。jcode 的 agent 场景尤其需要：搜索工具宁可返回“部分结果 + truncated 原因”，也不要卡死或报错。
7. **冷/热双读 + agent 维度 + 路径校验**：transcript 路由按 `agent_id` 取单个 agent 的页（`routes/transcript.ts:123-211`），cold 时从 wire 重建（`transcriptService.ts:543-572`），并对 `agent_id` 做 plain-id 校验（`:549-551`）。jcode 已有 `subagents/agent-<id>.jsonl`（`internal/session/session.go:970-980`），可以平移到 `GET /api/sessions/{id}/transcript?agent_id=&before_turn=`。
8. **索引状态作为一等公民**：`index_state{state,indexed_sessions,total_sessions,documents,stale,degraded}`（`contract.ts:46-53`）+ 独立的 `/status` + `reindex`（`searchService.ts:684-734`）。前端能显示“正在建索引 12/340”，而不是静默空列表。jcode 的 `session.json` 索引已经存在（`internal/session/session.go:451-454`），可以在同一处挂 `search_state`。
9. **安全基线照抄一半**：单一 bearer + Host 白名单 + Origin 白名单 + 失败限流（`middleware/auth.ts:32-89`、`hostnames.ts:61-95`、`start.ts:302-317`）是低成本高收益的；jcode 已有 `POST /api/auth/verify` 与 CORS（`internal/web/server.go:463`、`internal/web/cors_test.go`），可以对齐这套“Host → Origin → Bearer”的钩子顺序。

### 8.2 不抄什么（以及为什么）

1. **不要自研存储引擎**。kimi 为了“零 native 依赖 + 大索引”自己写了 `minidb`（WAL/跳表/倒排/postings 文件/CRC/varint，`packages/minidb/README.md:1-25`），代价是 20+ 源文件、worker 线程、只读退化、损坏重建、generation/fingerprint 一整套。jcode 是 Go，直接 SQLite FTS5（单文件、事务、`bm25()` 排序、`trigram` tokenizer 精确子串）即可覆盖 90% 语义，且天然多读者单写者。
2. **不要 worker_thread + inline 双实现**（`searchService.ts:256-274`、`worker/host.ts`）。那是 JS 单线程 + `readSync` 阻塞的产物；Go 里用 `database/sql` + 只读连接池 + `context` deadline 就够了，双实现只会让 §7 第 8 条（单写者）变成你的负担。
3. **不要做“无 workspace 边界的全局搜索”**（§4.3）。jcode 的会话本来就是按项目组织的（`session.json` 以 project path 为 key，`internal/session/session.go:451-454`），搜索默认应当**项目内**，跨项目要显式 `scope=all` 并在 UI 上说明；否则把 `workspace_id` 一样的多租户场景（cloud/remote-control）直接暴露出去。
4. **不要让“查看”变成“resume”**（`App.tsx:58-78`）。jcode 有 `/api/remote/*`、cloud relay 与 session file lock 语义（`internal/session/security_filelock.go`），物化别人的会话有真实副作用。跨会话查看应当只读盘（或只读 DB），不触发恢复、不写 session 文件、不进入 `lastsession`。
5. **不要索引 think / 工具输出**（kimi 只索引 user+assistant 文本与标题，`wireExtract.ts:141-224`）。这是正确的成本与隐私取舍；但要在 UI/工具描述里**明说**“只搜用户与助手文本”，否则用户会以为搜的是全部历史。若要更高召回，可后续加“工具输出”二级开关，而不是默认打开。
6. **不要照抄 vis 的“整份 wire 一次性返回 + 前端过滤”**（`routes/wire.ts:29-40`、`WireTab.tsx:85-122`）。jcode 的会话 JSON 可能很大；应直接上 `before_turn`/`limit` 的服务端分页（kimi 的 transcript 路由就是这么做的，`transcript.ts:123-211`）。
7. **不要放过期命中**。kimi 只用目录身份（`dev:ino:birthtimeNs`）过滤已删除会话（`indexCore.ts:1221-1242`），但对 undo / clear / compaction 之后的内容仍然返回（§7 条 5、6）。jcode 若做索引，应在写入侧（截断/undo）或索引侧显式删除过期 doc——这是 kimi 的已知坑，不是可抄的设计。

### 8.3 可以做得比 kimi 更好的一点：把能力给模型

kimi 完全没有 model-facing 的跨会话读（§2.3），而 jcode 已经有很强的工具注册链（`internal/tools/` + `internal/command/interactive.go` 的三处注册）。在索引落地之后，加一个**只读**的 `session_search` / `session_read` 工具（入参 `query` / `session_id` + `agent_id` + `turn` 区间，输出 snippet + `t<turn>.<step>` 坐标）成本很低，而且是差异化能力。前提：默认只在当前项目 scope 内可搜、跨项目要显式开关、工具调用写审计日志（kimi 连“谁查了谁”都没有，`routes/search.ts:103` 只有 request log）。

---

### 附：一句话总结这套设计的骨架

> **wire 是唯一真相源 → 后台增量扫描（字节水位）→ 两个文本索引（词项 + n-gram）→ 带预算的查询（deadline/postings/candidate cap）→ keyset 游标（查询指纹 + generation）→ 冷读 transcript 做深链跳转 → 单一 bearer 做全服务器边界（无 workspace ACL）。**

这是一套“能上线、能解释、能降级”的工程折中；可迁移性最高的是**语义与边界**（双路由、双索引、预算、游标、深链坐标），可迁移性最低的是**实现手段**（自研 minidb + worker 线程）。
