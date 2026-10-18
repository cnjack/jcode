# 跨会话查看（cross-session view）——实现评审

> 评审对象：`main` @ `2b8652c` 上的**未提交工作树**（8 个修改文件 + 9 个新文件）。
> 依据：`internal-doc/cross-session-view-design.md`（rev3 + §12 delta）、`internal-doc/cross-session-view-design-review.md`、`internal-doc/cross-session-view-scope.md`、`AGENTS.md`。
> 评审者未参与实现；结论来自逐行读代码 + 自建对抗性 PoC，不采信测试注释与设计文档的自述。
> 未修改生产代码或测试；`.github/workflows/notary-check.yml` 按 Lead 指示忽略（非本特性）。

---

## 0. 结论

**VERDICT: APPROVE WITH MINOR CHANGES —— 无阻塞项（0 blocking）。**

| 项 | 结论 |
|---|---|
| 授权边界 | **成立**。身份只来自 `Env.SessionCallerFn`；三条 schema 无 `project`/`path`/`workspace`/`cwd`；所有路径（模型传入 / 索引来源 / self / 子转录目录）统一走 `sessionPath`/`sessionDir` + `ValidateSessionID`；目标 project 用**同一个 `*os.File`** 复检后原句柄读取；不存在与越权在所有路径上同一句文案 |
| fail-closed | 成立。nil/空身份、空 workspace、坏索引、坏首行、未知 entry 类型、torn 尾行、非法 id 全部收敛到固定文案或跳过 |
| 只读 | 成立。新增代码零写操作（只有 `Close`），无 goroutine、无 `time.Sleep`、无锁跨 IO |
| 注册/开关 | 6 处 append 全部到位；`enabled:false` 在三条传输（含 plan）都不注册；catalog/approval/测试一致 |
| 测试 | 强。授权矩阵、同文案、self 排除、B2 canary、预算、渲染枚举都有真断言（详见 §4） |
| 代码质量 | 结构清晰（服务层无渲染、工具层无 IO 路径拼接），注释与实现基本一致 |

**为何没有阻塞项**：本次唯一的实质缺陷（§2 N1/N2）都不改变授权结果、不产生越权读取、不破坏 fail-closed；N1 是"搜索超时且零命中时丢失 footer"的误导性文案，N2 是"JSON 合法但非对象的行"导致 search 与 read 的 entry 序号错位（jcode 写侧不可能产生该行）。二者修复成本均为 2–3 行。除此之外，我在 §3 的 20 项属性清单上逐项找到了代码强制点 + 钉住它的测试。

---

## 1. 已执行的命令与结果

```
$ go build ./...            # exit 0
$ go test ./internal/sessionview/ ./internal/tools/ ./internal/command/ ./internal/config/ ./internal/runner/ -count=1
ok  github.com/cnjack/jcode/internal/sessionview  0.266s
ok  github.com/cnjack/jcode/internal/tools        4.004s
ok  github.com/cnjack/jcode/internal/command      1.387s
ok  github.com/cnjack/jcode/internal/config       0.768s
ok  github.com/cnjack/jcode/internal/runner       0.564s
```

lint 按 Lead 说明未重复运行（已由实现方确认 0 issues）。

---

## 2. 发现（均为非阻塞，按优先级）

### N1（P2，spec 偏差｜误导性文案）搜索超时且零命中时丢失 `Stopped after …` footer

- 位置：`internal/tools/session_view_render.go:104-107` 的早退分支
  ```go
  if len(result.Hits) == 0 {
      return fmt.Sprintf("No prior session matches found for %q.\n", query)
  }
  ```
  而 `TimedOut` 的 footer 在 `:142-147`，永远走不到。
- 后果：`session_search` 因 3s 预算被截断但恰好没有命中时，模型收到"没有匹配"，而不是"我只看了一部分"。设计 §5.5/§5.12 明确"预算用尽 = 部分结果 + `Stopped after …`，不是错误"，实现违反了"部分结果必须自述"的一半。
- 最小修复：早退分支也带上 footer，例如把 `TimedOut` / `Capped` 的判断移到 `if len(Hits)==0` 之前，或让空结果文案在超时时变成
  `No prior session matches found for "<q>" in the sessions searched.\nStopped after 3.0s (scanned 12 of 48 sessions). Narrow the query to search the rest.`
- 复现成本：低（`TestSearchCancelVsBudget` 已能造预算耗尽；补一个"零命中 + 超时"的渲染断言即可）。

### N2（P2，正确性｜契约不一致）search 与 read 的 entry 序号在"JSON 合法但非对象"的行上错位

- 位置：`internal/sessionview/parse.go:140-146`
  ```go
  if opts.prefilter != nil && !opts.prefilter(line) {
      if json.Valid(line) {
          seq++          // 预筛路径：合法 JSON 就当成已解析 entry
      } else {
          stats.Corrupt++
      }
      continue
  }
  ```
  对比解码路径 `:148-153` 用 `json.Unmarshal(line, &session.Entry{})`，以及 `session.LoadSession`（`internal/session/session.go:1926-1962`）同样用 `Unmarshal`。
- 后果：文件里出现一行 `123`（或 `[...]`）时，search 把它算作一个 entry，read 把它算作坏行 → **`session_search` 报的 entry N 比 `session_read` 的 seq 大 1**，直接违反"search 的 hit entry 就是 read 的 start"这一本设计的核心坐标契约。
- 我的 PoC 已证实（见 §5，`TestPoC_SeqOrdinalsMatchBetweenSearchAndRead`）：`search says 3, read says 2`。
- 最小修复：把 `json.Valid(line)` 换成与解码路径逐字一致的判定，例如
  ```go
  var probe session.Entry
  if json.Unmarshal(line, &probe) == nil { seq++ } else { stats.Corrupt++ }
  ```
  （多一次 unmarshal，但只在预筛不命中时发生，量级可忽略）；同时补一条 `TestParsePrefilterMatchesDecodeOrdinals` 单测。
- 触发面：jcode 写侧只会写对象行，故线上不可达；但 `parse.go` 的正确性契约是"与 `LoadSession` 逐字一致"（设计 §4.1），当前实现不满足。

### N3（P3，设计 T6/R6 未兑现｜注释过度承诺）

- 位置：`internal/command/session_view_tools.go:43-51`。闭包捕获的是 `rec` **指针**，`SetUUID` 能被跟随（`TestWireSessionCallerKeepsIdentityFromOneSource:215-222` 验证了这一点），但 `internal/web/chat.go:304-328` 是**换一个新的 `*Recorder` 对象**赋给 `eng.recorder`，闭包看不到。
- 注释 `wireSessionCaller` 写"The closure reads the recorder live, so a resumed/rebound recorder reports the current UUID (R6)"——**对 `SetUUID` 成立，对"换 recorder"不成立**，属过度承诺。
- 影响：换 recorder 后 self-read / self-exclusion 指向旧会话（同 project，无越权）。设计 R6/T6 原本承诺加固，实现未做。
- 建议：要么按设计 T6 让 web 侧发布"读 `eng.recorder`（持 `emu`）"的闭包，要么把注释改成如实描述并把 R6 写进 impl-notes 的 residuals。

### N4（P3，设计 N14 的实现方式变更）

- 设计 T7/N14 要求把 ACP/Web 的内联列表提取成可测纯函数。实现改为：TUI 用**真实 `buildAllTools`/`buildPlanTools`** 断言工具名（`session_view_tools_test.go:96-124`），ACP/Web 用**源码断言**（`:126-163`，断言精确 append 语句 + 调用次数）。
- 评价：这是可接受的降级（两个闭包确实需要活跃 model/HTTP server），且比"裸名字 grep"强（断言了赋值目标与次数）。但设计文档应同步记录该偏差，避免后续 review 以为 N14 已按原文落地。

### N5（P3，nit）`provider_tool_operation` 渲染字段与设计不一致

- `internal/sessionview/read.go:299-301` 渲染 `provider=ProviderToolProviderProfileID`，设计 §5.6 写的是 `provider=<p>`。值是 opaque profile id，不含密钥，**无泄露**；只是文案与设计表不一致。

### N6（P3，nit）输出预算的容差未在文档化

- `session_view_render.go:229-232` 用 `MaxOutputBytes - header` 并把下限夹到 512；首块无条件输出。因此实际输出可略超 `max_output_bytes`（测试也按 `+2048`/`+4096` 放宽）。属可接受工程容差，建议在 impl-notes 写明"预算是软上限，容差 ≈ header + 每 entry 前缀"。

### N7（P3，nit）两种"空结果"文案的小瑕疵

- `session_view_render.go:188-190`：非 self 的尾窗读返回空、但 `Total>0`（文件只有 `session_start`）时打印 "Session X has no recorded entries."，与事实（有 1 条 header）略有出入。
- `search.go` 的 `Eligible` 是**候选截断后**的数量（`candidates.go:71-75` 先 `out = out[:limit]` 再 `return len(out)`），会话数 >200 时 footer 的 "scanned X of Y" 会低估 Y。

---

## 3. 属性清单（property | where enforced | test pinning it | verdict）

| # | 属性 | 强制点 | 钉住它的测试 | 结论 |
|---|---|---|---|---|
| 1 | 身份只来自 `Env.SessionCallerFn`，模型无参数可影响 | `internal/tools/session_view.go:197-206`；schema 无 project/path/workspace/cwd | `TestSessionToolsSchemas`（逐参数计数 + 禁用参数名，`session_view_test.go:150-158`） | ✅ |
| 2 | 子 agent / teammate 拿不到身份（fail closed） | `env.go` `CloneForSubagent` 不复制（新增字段不在字面量里）；工具不注册于 `subagent.go:561`/`buildTeamChildTools` | `TestSessionViewIdentityNotInheritedBySubagent`（`tools:399-413`） | ✅ |
| 3 | 每条路径都过 `ValidateSessionID`（含索引与 self） | `paths.go:15-31` 唯一入口；`candidates.go:58`、`authorize.go:66`、`lineage.go:13` | `TestResolveTargetRefusesUnvalidatedIndexPath`、`TestSearchSkipsInvalidIndexUUID`（canary：`sessionview/search_test.go:258-294`）、`TestListSkipsInvalidIndexUUID`、`TestLineageInvalidIDDoesNotTouchFilesystem` | ✅ |
| 4 | 目标 project 从**同一个已打开文件**复检，读取同一句柄（无重开 TOCTOU） | `authorize.go:90-161`（`os.Open` → `readFirstEntry(f)` → `rewind(f)` → 返回 `Target.File`）；`read.go:102-106` 用它流式读 | `TestResolveTargetAuthorizationMatrix` 断言 `File.Seek(0,0)==0`（`authorize_test.go:204-210`） | ✅ |
| 5 | 搜索候选也复检文件 project | `candidates.go:192-221` `openCandidate`（uuid + project） | `TestSearchStaleIndexProjectMismatchSkipped`；我的 PoC `TestPoC_SearchRejectsFileProjectMismatch` 亦通过 | ✅ |
| 6 | 空 workspace 只读自己 | `authorize.go:61-64`；`candidates.go:39-41` | `TestSessionToolsNoWorkspace`、`TestListRequiresCallerAndWorkspace`、`TestSearchRequiresCallerAndWorkspace` | ✅ |
| 7 | 不存在 / 越权 / 索引不一致 / 非法 id 同一句文案 | `authorize.go` 全部 `unavailable(...)`；`session_view.go:215-235` 统一映射 | `TestUnavailableErrorsIndistinguishable`（对全部拒绝 case 做 `==` 文本比较）、`TestSessionNotFoundStringsAreExact`（含 `sess_` 前缀、不含目标 id/project/内容） | ✅ |
| 8 | 调用者不出现在搜索结果，`Eligible` 不计 self | `candidates.go:52` + `search.go:102` 双保险 | `TestSearchSelfExclusion`、`TestSessionSearchExcludesSelf` | ✅ |
| 9 | `session_list` 含 self 且标记 | `candidates.go:106-137` `IsCaller`；`session_view_render.go:75-79` `(this session)` | `TestListIncludesSelfMarkedAndOrdering`、`TestSessionListIncludesSelfMarked` | ✅ |
| 10 | self 读取排除当前轮次（含本次 tool_call 的 args） | `read.go:443-451` 在最后一个 user entry 处快照；`selected()` 返回快照 | `TestReadSelfCutsCurrentTurn`（精确断言只剩 seq 2、3，且 `session_id` 字样不出现） | ✅ |
| 11 | 空 `session_id` = self（不是 NOT_FOUND） | `authorize.go:54-60` 把 `id` 绑定为 `caller.SessionID` 再走同一校验 | `TestSessionReadOwnSessionWithoutID`；我的 PoC `TestPoC_EmptyIDSelfRead` 亦通过 | ✅ |
| 12 | 模型文本不含路径/project 泄漏 | 授权后 `ref.Project` 必等于 `caller.Project`（`refFor` + `authorize.go:140-149` 的不一致检查） | `TestSessionNotFoundStringsAreExact`（拒绝路径）；正面路径只打印 caller 自己的 workspace | ✅ |
| 13 | 未知 entry 类型 fail closed | `read.go:310-313` → `(unrecognized)`；`session_view_render.go:276-279` 二次拦截 | `TestSessionViewRenderUnknownTypeNeverLeaksText`、`TestRenderEveryEntryType`（21 类型） | ✅ |
| 14 | 禁渲染字段（Images/Opaque/Operation*/指纹） | `read.go:116-117` 读取即置 nil；`projectStructured` 显式投影 | `TestReadForbiddenFieldsNeverRendered`、`TestSessionViewRenderEveryEntryType`（禁用字段表）、`TestSessionViewRenderBookkeepingNeverPastesBody` | ✅ |
| 15 | 坏行/torn 尾行容错且计数 | `parse.go:125-147`；尾行无 `\n` 丢弃并置 `Torn` | `TestParseSessionTolerant`、`TestParseTornTailWithValidJSONFragmentIsDropped`、`TestReadCorruptAndTorn` | ✅ |
| 16 | 与 `LoadSession` 的坏行语义一致 | `parse.go`（解码路径一致；**预筛路径不一致 → N2**） | `TestParseMatchesLoadSession`（不含标量行场景） | ⚠️ 部分（N2） |
| 17 | `entry_offset` 仅在 `start>0` 且仅对 user/assistant/compact | `session_view.go:355-358` + `read.go:86-90` 双处校验；`read.go:196-198` 只对 `isContentType` 生效 | `TestSessionReadInvalidRangeEntryOffset`、`TestReadNoContinuationForStructuredTypes`、`TestReadLongEntryContinuation` | ✅ |
| 18 | 首条 16 KiB / 后续 4 KiB 预算 | `read.go:208-222`（首条 `capBytes=remaining`，后续 `min(MaxEntryBytes, remaining)`） | `TestReadBudgetAfterFirstEntry`、`TestReadLongEntryContinuation`、`TestSessionViewHonorsConfiguredBudgets` | ✅ |
| 19 | 超时与取消区分（预算→部分结果；取消→原样返回） | `search.go:78-81,127-137`；工具层 `sessionViewCallerAwareError`（`session_view.go:394-399`） | `TestSearchCancelVsBudget` | ✅ |
| 20 | 只读、无锁跨 IO、无 goroutine 泄漏 | grep 全包：仅 `Close`，无 `os.Create/Write/Remove/Rename`、无 `go func`、无 `sync.`；`Env.sessionView` 用 RWMutex 且只在读快照时持锁 | 代码检查（无专门测试，见 §6） | ✅ |
| 21 | 三传输 + plan 注册；`enabled:false` 全消失 | `interactive.go:229/246`、`acp.go:681/696`、`web.go:725/758`；`session_view_tools.go:21-31` | `TestSessionViewToolsWiredInAllTransports`（TUI 真实 builder + ACP/Web 调用点断言）、`TestSessionViewToolsRegistration`（nil/disabled/nil-env） | ✅ |
| 22 | catalog 策略 + 只读免审批 | `tool_catalog.go:33-35`（`allModes`/`read`）；`approval.go:252-254` | `TestBuildCommandToolPlanMatrix`（6 case）、`TestCommandToolPolicyCoversExpectedBuiltins`、`TestSessionViewToolsAreReadOnlyApproved`、`...StayAutoApprovedOutsideWorkdir` | ✅ |

---

## 4. 测试质量评审（不是"有没有测"，而是"测的断言是否真能失败"）

**做得好的（抽样验证过断言强度）**
- `TestSessionNotFoundStringsAreExact`：用 `==` 比较越权与不存在的完整错误串，并显式检查不包含 foreignID / foreignProject / `quokka-99` / `sess_` 前缀。这是本特性最该有的一条测试，写得很到位。
- `TestUnavailableErrorsIndistinguishable`：遍历**全部**拒绝 case，断言 `err.Error()` 恒等于哨兵文本、`Reason` 各异（日志侧不丢信息）——同时锁住了"防存在性探测"和"日志可诊断"两个相反要求。
- `TestSearchSkipsInvalidIndexUUID` / `TestResolveTargetRefusesUnvalidatedIndexPath`：真实 canary 文件放在 sessions 目录**上方**，索引里塞 `../../canary`、`a/b`、`""`、`..`，断言 `Skipped==4`、`Scanned==0`、token 不出现。这是 B2 的回归网，写法正确。
- `TestReadSelfCutsCurrentTurn`：用真 `session.Recorder` 落盘（含 `RecordToolCall("session_read", …)`），断言 self 读只剩 `2 3` 且 `session_id` 字样不出现——直接钉住"自己的 query 不可能被自己搜到"。
- `TestSessionToolsSchemas`：不仅查描述，还断言**参数个数精确相等**与 `required` 集合，再加禁用参数名（project/path/workspace/cwd）。加参数不改文档会红。
- `TestReadNoContinuationForStructuredTypes`：显式断言 `tool_call`/`tool_result` 的 `NextEntryOffset == 0`——正好覆盖我在设计评审里担心的"续读死循环"。

**偏弱的（非阻塞）**
- `TestSessionViewHonorsConfiguredBudgets` 用 `+2048` 容差；能抓住"完全没生效"，抓不住"超一点"（对应 N6）。
- ACP/Web 注册守卫是源码字符串断言而非真实 builder（N4）；能抓"漏 append"，抓不到"append 到错的列表"（例如把 session 工具塞进 web 的 plan 变量却带着 all 的 cfg 语义）。不过 `Count==2` 与精确语句断言把风险压得比较低。
- `TestSessionViewRenderEveryEntryType` 的 Text 是共享的 `"body text"`，只验证"没渲染禁字段"，不验证"该行的结构化摘要字段正确"（个别类型的具体字段错误不会被抓到，如 N5）。

---

## 5. 对抗性验证（PoC，写在 `/tmp/sv_poc/`，用 `go test -overlay` 注入，仓库零改动）

| # | 攻击/边界 | 预期 | 实际 |
|---|---|---|---|
| A | 索引把某会话登记在 caller 的 project 下，但文件 `session_start.project` 是另一个 project，内容含 `SECRET_TOKEN_B` | 搜索不得返回它 | **PASS**：`Scanned=1`（确实打开过）但 `Hits=0`，token 未出现（`authorize.go`/`candidates.go:210-214` 复检生效） |
| B | `<uuid>.json` 是**符号链接**，指向另一 project 的转录；索引按 caller project 登记 | 必须拒绝 | **PASS**：`ResolveTarget` 返回 error（按文件内容判定 project，不按路径） |
| C | `session_read` 省略 `session_id`（self 分支）读自己更早轮次，且当前轮 tool_call 已落盘 | 读到更早轮次、排除在飞轮次 | **PASS**：拿到 `first question`/`first answer`，`second question` 与 `session_read` 的 args 均未出现 |
| D | 会话文件中间插入一行 `123`（JSON 合法但非对象），其后是命中 entry；比较 search 报的 entry 号与 read 的 seq | 两者必须相等（坐标契约） | **FAIL — 证实 N2**：`search says 3, read says 2` |

D 的复现命令（已删仓库外的临时文件后重跑可复现）：
```
$ go test -overlay=/tmp/sv_poc/overlay.json ./internal/sessionview/ -run TestPoC_SeqOrdinals -count=1 -v
--- FAIL: TestPoC_SeqOrdinalsMatchBetweenSearchAndRead
    ORDINAL DIVERGENCE: search says 3, read says 2
```

其余尝试但**未发现漏洞**的角度：JSON 转义预筛的假阳性（`<` 被写成 `\u003c`；预筛命中后在解码文本上复核，`matchableText` 只认 user/assistant/compact，工具 IO 内容不会成为 hit）；大小写折叠（ASCII-only 折叠，非 ASCII 字节精确，CJK/emoji 不受影响）；`..`/`/`/`\`/空 id 全部在 `paths.go` 被拒；self 与 `caller.Project==""` 的组合按矩阵放行/拒绝；并发 append 只可能造成 torn 尾行（丢弃并置 `Torn`，不 panic）。

---

## 6. 缺失/建议补的测试

1. **搜索超时 + 零命中** 的渲染（N1）：断言输出含 `Stopped after`，而不是只有 "No prior session matches"。
2. **预筛与解码的序号一致性**（N2）：标量行 / 数组行场景下 `search.Seq == read.seq`。
3. **CJK/emoji 的 entry 截断与 `entry_offset` 续读**：`read_test.go` 里目前**没有任何多字节内容**（只有 `…` 分隔符）。`truncateRunesBytes` 与 `entry_offset` 都是 rune 语义，值得一条 `中文…🚀` 跨 16 KiB 边界的续读断言。
4. **并发追加**：一个 goroutine 持续 `RecordAssistant` 的同时 `Read` 尾窗，断言不 panic、结果 UTF-8 合法、不返回半行。
5. **self 会话不在索引里**（index 写失败/被删）：断言 fail closed 成 NOT_FOUND 而不是 panic 或空结果——这是 self 读唯一会"莫名其妙失败"的场景。
6. **索引条目 >200** 时的 `Eligible`/footer 数字（N7 的一半）。

---

## 7. Owner 相关卫生检查

| 项 | 结论 |
|---|---|
| 无无关改动 | `git diff` 只涉及 8 个文件，全部围绕本特性；`git status` 的新文件全部属于 `internal/sessionview`、`session_view*`、`session_view_tools*`、新测试与 `script/e2e/`（后者属 tester，task-8） |
| 无生成文件 / 无新依赖 | `go.mod`/`go.sum` 未变；无 `registry_generated.go` 之类改动；禁止 SQLite/FTS 的约束遵守 |
| AGENTS.md 约定 | 工具返回 `(string, error)`；错误用 `toolErrf` + `Hint`；诊断只用 `config.Logger()`（grep 无 `fmt.Print`/`os.Stdout`）；新增字段无 panic 路径；无中间件顺序改动 |
| 只读 | 新增包零写操作（只有 `Close`），不 resume、不改索引、不改 lastsession |
| 锁/IO | 无锁跨 IO；`Env.sessionView` 的 RWMutex 只在取快照时持有，不做 IO |
| 资源 | `ResolveTarget` 的 `keep` 标志保证所有错误路径关闭句柄；`openCandidate`/`scanCandidate` 同样 |

---

## 8. 给实现方的行动清单

**建议本 PR 内修（各 2–3 行）**
- [ ] N1：零命中 + 超时时保留 `Stopped after …` footer（`session_view_render.go:104-107`）
- [ ] N2：预筛关行判定改为与 `json.Unmarshal` 一致，并补序号一致性测试（`parse.go:140-146`）
- [ ] N3：要么兑现 T6（web 闭包读当前 recorder），要么把 `wireSessionCaller` 注释改为如实描述并在 impl-notes 记 residuals

**可留到后续**
- [ ] N4：设计文档同步"N14 降级为源码断言"的偏差
- [ ] N5–N7：文案/容差的小瑕疵
- [ ] §6 的 6 条补充测试（其中 1、2、3 建议本次补上，成本低）
