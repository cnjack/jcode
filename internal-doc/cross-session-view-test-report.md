# 跨会话查看（cross-session view）— 测试报告

2026-10-19。对象：`session_list` / `session_search` / `session_read` 三个只读 model-facing 工具
（`internal/sessionview/`、`internal/tools/session_view*.go`、`internal/command/session_view_tools.go`、
`internal/config`、`internal/runner/approval.go` 的注册点）。
方法：全量单测 + `golangci-lint` + 真实模型 ACP 端到端（7 个用例，每例独立 seed 的 HOME）。

规格基准：[`cross-session-view-design.md`](cross-session-view-design.md) §6.3（E2E）与
[`cross-session-view-scope.md`](cross-session-view-scope.md) §6（验收标准）。
测试代码：`script/e2e/{seed_sessions.py, session_view_acp_probe.py, run_session_view_e2e.py}`（见其 README）。

**结论**：`go test ./...`、`golangci-lint`（0 issues）、`make lint-web` 全绿；真实模型 E2E **7/7 全通过**——
正例读出同 project 会话的 entry 内容与序号；跨 project 与"不存在"的错误文案**逐字节相同**；
`enabled:false` 时三个工具**未挂载**（非"模型恰好没调用"）；archived 默认排除、`include_archived:true`
可读；含坏行/尾行截断的会话仍可搜索并读出。本轮**未发现产品缺陷**；发现并修正 1 个测试脚本自身的断言缺陷。
唯一环境问题是 `golangci-lint` 需 `GOTOOLCHAIN=go1.26.5`（见 §2）。

## 1. 被测状态（最终 fingerprint）

所有结论都对应下列快照（`run_session_view_e2e.py` 在运行前后各取一次并自校验，`stableDuringRun=true`）：

| 项 | 值 |
|---|---|
| branch | `chore/notary-diagnostic` |
| HEAD | `2b8652ccc847ac5e69f0e86140846fffbdbfc492` |
| `git status --porcelain` sha256 | `e205c211210c72b4108802c3387f8ffd84f6f704250d9e66c6ef303bcbd43a1e` |
| `git diff` sha256 | `7415ee3af51eb71a9fe77d4b36d3f62a39a9274f2ee312621e435f1d88ab163a` |
| worktree 内容 sha256（42 个 modified/untracked 文件逐个哈希） | `e57f4771ece7055522d68365a4701f2127b1417e6ac0d614fc200be5309b7783` |
| 被测二进制 sha256 | `e70601aac0a417b8c58d663af5693d788ba7d778e734813e3ff84c6c00ce28db` |
| 运行期间 worktree 稳定性 | `true`（运行前后 status/diff/worktree sha 完全一致） |

两点说明：

1. `git status --porcelain` 只列路径，无法发现**未跟踪目录内部**的内容变化，因此另外记录 worktree 内容哈希。
2. 上表的 worktree 哈希由 E2E 运行时记录（与 `summary.json` 同值），**包含当时存在于工作区的
   `script/e2e/__pycache__/*.pyc`**（import harness 模块产生的缓存，现已删除，并已在 harness 中排除
   `__pycache__`/`*.pyc`）。清理该缓存并计入本报告后重算为
   `da0ac9b4bf4212dc2cf15b38ce0a19bd4ab7f9d8cc539ab71093f0f333fa84b4`（42 文件）。
   两者 status/diff sha 完全一致，且 E2E 之后对 harness 的改动均为 Python 脚本，**不参与被测二进制**。

因此判定被测代码的权威锚点是：**HEAD + status/diff sha + 二进制 sha**。为排除"报告与代码不同步"，
分别于 00:39、00:52、00:55 三次从当前工作区重建二进制，三次都与被测二进制 sha256 **逐字节相同**
（`e70601aa…`），即：被测代码 == 当前工作区代码。

**P2 修复轮已在快照内**：`TestParsePrefilterMatchesDecodeOrdinals`、
`TestReadMultibyteEntryContinuation`、`TestSessionViewRenderSearchZeroHitsKeepsPartialSearchFooter`
全部存在于本快照并通过。

## 2. 环境与工具链

| 项 | 值 |
|---|---|
| OS / CPU | macOS 27.0.1 / arm64 |
| Go（构建与 `go test`） | go1.27.2 darwin/arm64 |
| Go（`golangci-lint` 分析） | go1.26.5（`GOTOOLCHAIN=go1.26.5`） |
| golangci-lint | v2.13.1（built with go1.27.0） |
| Python / pnpm | 3.12.13 / 12.10.1 |
| 模型 | `zhipuai-coding-plan/glm-5.3`（`~/.jcode/config.json` 默认，reasoning model） |

**工具链事实（必须记录）**：本机 `golangci-lint` 无法读取 go1.27.2 的 export data，
因此 `make lint`（=`lint-go` + `lint-web`）中的 `golangci-lint run` 若以默认工具链运行会报
**纯 typecheck 假阳性**，与代码无关：

```
internal/sessionview/authorize.go:5:2: could not import encoding/json (…
  could not load export data: internal error in importing "cmp"
  (cannot decode "cmp", export data version 5 is greater than maximum supported version 4)) (typecheck)
6 issues: * typecheck: 6        # exit 1
```

以 `GOTOOLCHAIN=go1.26.5 golangci-lint run` 运行则为 **0 issues**。报告采用后者作为 Go lint 结论。
（这是环境/工具链问题，不是本特性的代码问题，也不影响 `go vet`/编译器；建议在 CI 或 Makefile 固定该变量。）

## 3. 命令与结果

```bash
cd /Users/jack/workpath/jjj/jcode
go build -o /tmp/jcode-e2e-final2 ./cmd/jcode                    # exit 0
go test ./...                                                     # exit 0
GOTOOLCHAIN=go1.26.5 golangci-lint run                            # 0 issues, exit 0
make lint-web                                                     # exit 0
./script/e2e/run_session_view_e2e.py --bin /tmp/jcode-e2e-final2 \
    --out-dir /tmp/e2e-final-run4 --timeout 300                   # 7/7 pass, exit 0
```

| 检查 | 结果 |
|---|---|
| `go test ./...` | **41 ok，0 FAIL**（另 10 个包无测试文件；共 51 个包），exit 0 |
| 特性相关包 | `internal/sessionview` 0.352s、`internal/tools` 4.390s、`internal/command` 1.550s、`internal/config`、`internal/runner` 全 ok |
| `GOTOOLCHAIN=go1.26.5 golangci-lint run`（全仓） | **0 issues** |
| `make lint-web`（tsc × 3 工程） | exit 0 |
| 真实模型 E2E | **7/7 pass**，`not-found vs unauthorized: byte-identical` |

## 4. 单元/集成测试覆盖

特性相关新增测试共 **74 个 Test 函数**（44 个在 `internal/sessionview/*_test.go`，
30 个在 `internal/tools/session_view_test.go`、`internal/config/session_view_test.go`、
`internal/runner/approval_session_view_test.go`、`internal/command/session_view_tools_test.go`），
多为表驱动。与 design §6.1/§6.2 的对应关系（节选）：

- **授权**：`TestResolveTargetAuthorizationMatrix`（self / 同 project / 跨 project / 索引有文件无 /
  索引≠文件 / 缺 project 回退 / 首行损坏 / id 含 `/`·`..` / 空 id / 无 caller / 重复索引）、
  `TestUnavailableErrorsIndistinguishable`、`TestResolveTargetRefusesUnvalidatedIndexPath`、
  `TestSessionToolsNoCallerFailsClosed`、`TestSessionToolsEmptyCallerIdentityFailsClosed`、
  `TestSessionViewIdentityNotInheritedBySubagent`。
- **坏行/容错**：`TestParseSessionTolerant`、`TestParseTornTailWithValidJSONFragmentIsDropped`、
  `TestParseMatchesLoadSession`（与 `session.LoadSession` 对拍）、`TestReadCorruptAndTorn`、
  `TestParsePrefilterKeepsOrdinals` + `TestParsePrefilterMatchesDecodeOrdinals`（P2 修复）。
- **截断/预算/续读**：`TestReadLongEntryContinuation`、`TestReadMultibyteEntryContinuation`（CJK）、
  `TestReadRangesAndBudgets`、`TestReadBudgetAfterFirstEntry`、`TestReadNoContinuationForStructuredTypes`、
  `TestReadShortToolCallIsNotMarkedTruncated`、`TestSessionReadInvalidRangeEntryOffset`、
  `TestSessionViewHonorsConfiguredBudgets`。
- **CJK/UTF-8 片段**：`TestSearchSnippetUTF8`、`TestReadMultibyteEntryContinuation`。
- **搜索语义/排序/自身排除**：`TestSearchLiteralSemantics`、`TestSearchRankingOrder`、`TestSearchCap`、
  `TestSearchSelfExclusion`、`TestSessionSearchExcludesSelf`、`TestSearchSkipsInvalidIndexUUID`、
  `TestSearchArchivedAutomation`、`TestSearchScannedEligibleNumbers`（footer 数字一致）。
- **渲染（21 类型）**：`TestSessionViewRenderEveryEntryType`、`TestSessionViewRenderUnknownTypeNeverLeaksText`、
  `TestReadForbiddenFieldsNeverRendered`、`TestSessionViewRenderSystemPromptElided`、
  `TestSessionViewRenderSearchZeroHitsKeepsPartialSearchFooter`（P2 修复）。
- **注册/审批/关闭开关**：`TestSessionViewToolsRegistration`、`TestSessionViewToolsWiredInAllTransports`
  （真实 builder：TUI `buildAllTools`/`buildPlanTools`、ACP 候选、Web 候选；enabled/disabled 两态）、
  `TestSessionViewToolsAreReadOnlyApproved`、`TestSessionViewToolsStayAutoApprovedOutsideWorkdir`、
  `TestSessionViewEnabled`、`TestSessionViewSettingsDefaults`、`TestSessionViewConfigJSON`。

## 5. 真实模型 E2E（`jcode acp`）

**方法**：`seed_sessions.py` 为每个用例生成独立 HOME（复制真实 `config.json`/`provider-auth.json`，
只读拷贝），写入 4 个格式化 fixture 会话（同 workspace / 另一 workspace / archived / 含坏行）并同步
`session.json` 索引；`session_view_acp_probe.py` 以 NDJSON JSON-RPC 驱动 `jcode acp`
（`initialize` → `session/new{cwd}` → `session/set_mode` → `session/prompt`），客户端不声明
fs/terminal capability，收集 `session/update` 文本与 tool_call 流，**并从落盘的调用者 JSONL 读回
`tool_call`/`tool_result` 作为主证据**。模型 `glm-5.3`，`full_access`，单轮 300s。7 例各自新 seed。

| # | 用例 | 判定 | 关键证据 |
|---|---|---|---|
| 1 | `positive` 同 project 搜索+读取 | **pass** | `session_search{query:zebra-unicorn-42}` → 命中 `1111…`（entry 6）；`session_read{session_id:1111…,start:6,limit:1}` → `Entries 6-6 of 7`；回答原文引用 CJK 行 |
| 2 | `negative` 跨 project 读取 | **pass** | `session_read{2222…}` → `ERROR[SESSION_VIEW_NOT_FOUND]…`；回答不含 `quokka-99`/另一 project 路径 |
| 3 | `missing` 不存在的 id | **pass** | 与 #2 **工具输出逐字节相同**（见下） |
| 4 | `disabled` `enabled:false` | **pass** | 挂载 12 个工具、`session_*` **0 个**；模型自述"没有 session_search 工具" |
| 5 | `archived_default` | **pass** | `session_search` 默认参数 → `No prior session matches found`，结果中无 archived uuid/title |
| 6 | `archived_included` | **pass** | 同查询 + `include_archived:true` → 命中 `3333…` "Archived investigation" + 片段 |
| 7 | `corrupt` 坏行/尾行截断 | **pass** | 命中 `4444…`；`session_read` 头部 `Entries 2-3 of 3 (corrupt lines skipped: 1, last line incomplete)` |

全部 7 例：`stopReason=end_turn`、`permissionRequests=0`（只读免审批的直接证据）。

### 5.1 正例（#1）摘录

```
CALL session_search {"query":"zebra-unicorn-42"}
 OUT Session search results (1) for "zebra-unicorn-42" in …/positive/ws:
     1. 11111111-1111-4111-8111-111111111111 — Set up the staging relay
        Best match: entry 6 (user) at 2026-01-05T10:00:00Z · 1 match in this entry, 3 more entries
        Snippet: 中文备注：生产环境的 relay 令牌是 zebra-unicorn-42，请务必不要提交到仓库。
CALL session_read {"session_id":"11111111-1111-4111-8111-111111111111","start":6,"limit":1}
 OUT Session 11111111-… — Set up the staging relay … Entries 6-6 of 7
     [6] user · 2026-01-05T10:00:00Z
     中文备注：生产环境的 relay 令牌是 zebra-unicorn-42，请务必不要提交到仓库。
FINAL Found it in session 1111… ("Set up the staging relay"), entry 6 (user message…)
      > 中文备注：生产环境的 relay 令牌是 zebra-unicorn-42，请务必不要提交到仓库。
```

补充观察（验收标准第 3 条）：调用者自己的会话本次 prompt 里同样含 `zebra-unicorn-42`，
而搜索只返回了 1 条命中（fixture 会话），自身会话未被搜索命中（self-exclusion 生效）。

### 5.2 反例（#2/#3）与"同文案"证明

两例 `session_read` 的**落盘 tool_result 字段完全相同**（runner 自动比对）：

```
Tool execution failed: ERROR[SESSION_VIEW_NOT_FOUND]: session was not found or is not readable
from this session. Hint: Only sessions returned by session_list or session_search are readable;
ids look like <uuid> without a sess_ prefix; do not guess session ids.
```

两例的 `error` 字段均为空串、`denied=false`（见 §6）。真实原因只写入日志（设计 §5.3）：

```
[session_view] deny caller=04e7f503… target=2222… reason=project_mismatch
    caller_project=/tmp/…/negative/ws observed_project=/tmp/…/other-project
[session_view] deny caller=eba0ce4c… target=00000000-… reason=not_indexed caller_project=/tmp/…/missing/ws observed_project=
```

### 5.3 关闭开关（#4）

`config.json` 被改写为 `{"session_view":{"enabled":false}}` 后：
`persisted.visibleTools` 共 12 个工具，`session_list/search/read` **一个都不在**（该清单来自 recorder 的
`tool_observation.kind=model_request.visible_names`，是确定性事实，不依赖模型行为）；
模型回答："I don't have a `session_search` tool — it's not among my attached functions"。

### 5.4 archived 默认排除 / 显式包含（#5/#6）

```
# default
No prior session matches found for "narwhal-archived-7".
# include_archived:true
Session search results (1) … 1. 33333333-3333-4333-8333-333333333333 — Archived investigation
   … | archived | Best match: entry 2 (user) … Snippet: Old investigation that was archived. Marker: narwhal-archived-7.
```

### 5.5 坏行容错（#7）

```
Session 4444… — Session with corrupted lines … Entries 2-3 of 3 (corrupt lines skipped: 1, last line incomplete)
[2] user · Before the corruption. Marker: ocelot-corrupt-13.
[3] assistant · After the bad line: a tolerant reader must still see this entry…
```

坏行计数与"最后一行不完整"（torn tail）都被如实报告，且坏行之后的内容仍可读。

## 6. 验收标准逐条（scope §6）

| # | 标准 | 判定 | 证据 |
|---|---|---|---|
| 1 | 会话 A 能搜到同 project 会话 B 的内容，并读出 B 的 entry 内容与序号 | ✅ | E2E #1：命中 entry 6，`session_read` 显示 `Entries 6-6 of 7` 并引用原文 |
| 2 | 新增工具 cross-project fail closed，且与"不存在"同文案；日志有真实原因；tool_call 审计 | ✅ | E2E #2/#3 输出逐字节相同；`reason=project_mismatch` / `reason=not_indexed` 入日志；审计见 `persisted.tools` |
| 3 | 搜索永不返回调用者自身，不因自身查询自激 | ✅ | E2E #1（prompt 含同一 token，仍只命中 1 条 fixture）+ `TestSearchSelfExclusion`/`TestSessionSearchExcludesSelf` |
| 4 | 不存在可改变授权边界的 `project` 参数 | ✅ | schema 参数仅 `query / case_sensitive / include_archived / limit / session_id / start / entry_offset`（`internal/tools/session_view.go`）+ `TestSessionToolsSchemas` |
| 5 | 三传输均可用（至少 acp/web 有集成或 E2E） | ✅ | ACP：真实模型 E2E 全流程；TUI/Web：`TestSessionViewToolsWiredInAllTransports` 用真实 builder 断言 `buildAllTools`/`buildPlanTools`/ACP/Web 候选均含三工具 |
| 6 | `enabled:false` 时工具不出现 | ✅ | E2E #4：挂载清单中 0 个 `session_*`；`TestSessionViewToolsRegistration`（disabled/nil cfg → nil） |
| 7 | 只读、免审批；plan 模式可用 | ✅ | 7 例 `permissionRequests=0`；`TestSessionViewToolsAreReadOnlyApproved`、`...StayAutoApprovedOutsideWorkdir`；plan 列表在 `...WiredInAllTransports` 的 `buildPlanTools` 子测试 |
| 8 | `go test ./...` 与 `make lint` 通过；新代码有表驱动覆盖授权/截断/坏行/CJK | ✅（附工具链说明） | 41 ok/0 fail；`GOTOOLCHAIN=go1.26.5 golangci-lint run` 0 issues；`lint-web` 通过；见 §2 的默认工具链假阳性说明 |
| 9 | 真模型 E2E（`~/.jcode` 已配置模型）验证正例与反例 | ✅ | §5 七例，正/反/关闭/archived/corrupt 全通过 |

## 7. 对设计 §6.3 的经验性修正（重要）

设计 §6.3 的反例判定写的是"其 `tool_result` 含 `ERROR[SESSION_VIEW_NOT_FOUND]…`"，读者容易理解为
持久化行的 `error` 字段。**实测该字段恒为空串**：工具把策略失败当作**普通输出文本**返回给模型
（`internal/tools` 的 `toolErrf` 路径），持久化后形如：

```json
{"type":"tool_call","name":"session_read","args":"{\"session_id\":\"2222…\"}","tool_call_id":"call_…"}
{"type":"tool_result","name":"session_read","output":"Tool execution failed: ERROR[SESSION_VIEW_NOT_FOUND]: …","error":"","denied":false}
```

因此：

- 断言必须匹配 **`tool_result.output` 文本**（本报告的 `--expect-tool-result-contains`），
  而不是 `error` 非空；
- 同理，ACP 侧 `tool_call_update` 的 `status` 为 `failed`（基于 `isToolFailureOutput`），
  但 `rawOutput` 仍是该文本，`error` 语义不参与；
- 建议设计 §6.3 的措辞改为"持久化 `tool_result` 的 `output` 字段含 `ERROR[...]`"。

## 8. 发现的问题与限制

1. **产品缺陷：本轮 0 个。** 两次独立完整运行（含修复轮之后）均 7/7 通过；未观察到越权、
   文案不一致、截断破坏 UTF-8、archived 泄漏、坏行导致失败等问题。
2. **测试脚本自身缺陷（已修，非产品问题）**：`archived_default` 最初禁止 tool_result 含
   `narwhal-archived-7`；但"无命中"的回复会回显 query（`No prior session matches found for "<query>".`），
   该断言永不可能通过。已改为禁止 archived 会话的 **uuid 与 title**（只可能来自真实返回的内容），
   并保留 `archived_included` 作为正向对照。
3. **环境限制**：`golangci-lint` 必须 `GOTOOLCHAIN=go1.26.5`（§2）；默认工具链的 6 条 typecheck
   为假阳性，建议 Makefile/CI 固定。
4. **覆盖边界**：真实模型 E2E 只走 ACP（TUI/Web 由真实 builder 单测覆盖）；远端（SSH/Docker）
   与 teammate 会话按设计为不支持，本轮未验证。模型可能先调用 `tool_search`，属正常行为，不计失败。
5. 用例全部一次通过，无需重试；E2E 期间 worktree 稳定（`stableDuringRun=true`）。

## 9. 复现

```bash
cd /Users/jack/workpath/jjj/jcode
go build -o /tmp/jcode-e2e-bin ./cmd/jcode
./script/e2e/run_session_view_e2e.py --bin /tmp/jcode-e2e-bin \
    --out-dir /tmp/e2e-run --timeout 300        # 7 例；--only positive,negative 可跑子集
```

每个用例目录含 `probe.json`（完整结果、`persisted` 审计、`visibleTools`）、`events.jsonl`
（原始 ACP 通知流）、以及 seed 出来的 `home/`、`ws/`；运行级 `summary.json`/`summary.md`
记录 §1 的 fingerprint 与逐例判定。本报告的原始产物：`/tmp/e2e-final-run4/`（临时目录，非仓库内容）。
