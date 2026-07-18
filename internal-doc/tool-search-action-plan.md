# Tool Search 渐进式工具披露实施清单

> 状态：执行中
>
> 创建日期：2026-07-18
>
> 目标模型：`kimi-for-coding/kimi-for-coding`
>
> 参考实现：Codex runtime registry / model-visible spec 分离；Eino ToolSearch middleware
>
> 最终交付：实现代码、分阶段提交、至少 30 分钟全场景测试、HTML 测试报告

## 执行规则

1. 每个编号任务完成后，立即将复选框更新为 `[x]`。
2. 每个编号任务必须填写测试命令、测试结果、提交号；没有证据不得标记完成。
3. 每个实现阶段单独提交，不混入无关工作树修改。
4. 最终回归持续时间不得少于 30 分钟，必须覆盖 Direct、Deferred、MCP、Browser、Computer、权限、模式切换和失败路径。
5. 最终以 HTML 报告记录环境、模型参数、场景、调用轨迹、指标、失败项和结论。

## 关键设计约束

- 运行时工具全集与模型当前可见 schema 分离。
- `Direct` 首轮可见；`Deferred` 经 `tool_search` 后披露；`Hidden` 不注册或不可搜索。
- Deferred 仅控制披露，不构成授权；实际调用继续经过现有 approval middleware。
- Kimi 使用 Eino 客户端 ToolSearch：`UseModelToolSearch: false`。当前模型适配层不支持 Codex 风格原生 deferred wire protocol。
- 顶层 TUI、Web、ACP 首先接入；child agent 在审批和 plan 权限一致性修复前不接入。
- MCP 使用稳定的模型侧 canonical name，避免不同 server 及内建工具重名。

## 总体进度

| 阶段 | 状态 | 提交 | 说明 |
|---|---|---|---|
| TS-00 文档与基线 | 完成 | `a1d0b13` | 基线 `ca4c8ba6b709`，完整 Go 测试通过 |
| TS-01 ToolPlan 核心 | 完成 | `be5e701` | 分类、校验、稳定排序与 runtime 安全边界 |
| TS-02 Eino middleware | 完成 | `0a03ce6` | 客户端 ToolSearch、兼容开关与模型级测试 |
| TS-03 Transport 接入 | 完成 | `0f55179` | TUI/Web/ACP 共享 policy 与重建路径 |
| TS-04 MCP 与权限 | 完成 | `c9cb076`、`1aa128e`、`a316830`、`b1b687f`、`3b751db` | MCP、Plan、delegation 和 team mode 边界完成 |
| TS-05 Prompt 与观测 | 进行中 | `dbe7ca4`、`69ba9ee`、`f905ae2` | 使用说明、schema/轨迹指标与凭证文件权限 |
| TS-06 自动化测试 | 完成 | `77de5fc`、`92e0cb0`、`56fa6eb` | 单元、集成、fixture、routing oracle 与生命周期撤权矩阵 |
| TS-07 Kimi A/B | 未开始 | — | 精确模型、静态/渐进式对照 |
| TS-08 30 分钟回归 | 未开始 | — | 全场景长跑 |
| TS-09 HTML 报告 | 未开始 | — | 生成并审计最终报告 |

## TS-00 文档与基线

- [x] TS-00.1 建立本清单，记录执行和更新规则。
- [x] TS-00.2 保存基线证据：工作树状态、当前提交、Go/Eino 版本、基线测试结果。
- [x] TS-00.3 提交任务文档。

完成证据：

- 基线：`ca4c8ba6b709e5b3e9ccbd08f0e59929afff3b9d`，开始时工作树干净。
- 环境：`go1.26.4 darwin/arm64`；`github.com/cloudwego/eino v0.9.9`。
- 测试：`git diff --check`；`go test ./...`。
- 结果：通过。沙箱内运行因现有 localhost/Go cache 权限受限失败；允许 localhost 的沙箱外重跑完整通过。
- 提交：`a1d0b13 docs: track tool search rollout plan`。

## TS-01 ToolPlan 核心

- [x] TS-01.1 新增中心化 `ToolDescriptor` / `ToolPlan`。
- [x] TS-01.2 支持 `Direct`、`Deferred`、`Hidden`，并预留模型专用暴露等级。
- [x] TS-01.3 描述 transport、mode、source、bundle、aliases、enabled predicate、approval class 和真实 endpoint。
- [x] TS-01.4 校验 Direct/Deferred 不相交、名称唯一、ToolInfo 非空、`tool_search` 保留名和稳定排序。
- [x] TS-01.5 编写表驱动测试覆盖 transport × mode × capability 分类。

普通模式首轮目标不超过 12 个：

`tool_search`、`read`、`grep`、`edit`、`write`、`execute`、`check_background`、`todowrite`、`todoread`、`ask_user`、`load_skill`、`subagent`。

Plan 模式首轮目标：

`tool_search`、`read`、`grep`、`execute`、`todowrite`、`todoread`、`ask_user`；ACP 去掉 `ask_user`，active goal 可追加 `goal_get` / `goal_update`。

完成证据：

- 实现：`internal/agent/tool_plan.go`、`internal/agent/tool_plan_test.go`。
- 安全边界：transport/mode/capability gate 产生的 Hidden 工具不进入 runtime registry，防止模型猜名绕过。
- 测试：`go test ./internal/agent -count=1`；`go test -race ./internal/agent -run '^TestToolPlan' -count=1`；`go test ./...`；`make lint-go`。
- 结果：全部通过；race 无异常；lint `0 issues`。
- 提交：`be5e701 feat(agent): add progressive tool exposure plan`。

## TS-02 Eino ToolSearch middleware

- [x] TS-02.1 使用当前 Eino 版本提供的 `adk/middlewares/dynamictool/toolsearch`。
- [x] TS-02.2 初始 Agent 只传 Direct 工具；Deferred 只传 `DynamicTools`。
- [x] TS-02.3 固定 `UseModelToolSearch: false`，Deferred 为空时跳过 middleware。
- [x] TS-02.4 保持 approval middleware 最内层，确保 Deferred 工具执行不绕过审批。
- [x] TS-02.5 增加 `tool_search.enabled` 配置开关和 static 回退路径。
- [x] TS-02.6 测试首轮隐藏、搜索后披露、多次搜索累积和未知搜索。

完成证据：

- 实现：保留 legacy `NewAgent`；新增 `NewAgentWithToolPlan`，并将 Eino ToolSearch 放在 caller middleware 之后、history rewrite handlers 和 approval 之前。
- 配置：`tool_search.enabled` 当前为 opt-in；缺省、nil、显式 false 均保持原 eager/static 行为。
- 安全：Hidden 不注册；`DirectModelOnly` 在当前适配层明确 fail closed；手工构造的非法计划会被重新校验。
- 测试：Agent/config 定向测试；`go test -race ./internal/agent ./internal/config -run 'ToolSearch|NewAgentLegacy|LegacyConfigWithoutToolSearch' -count=1`；`go test ./...`；`make lint-go`。
- 结果：全部通过；模型级测试覆盖首轮 schema、search 激活、累积、runtime registry、history rewrite 顺序和原审批 name/args；lint `0 issues`。
- 提交：`0a03ce6 feat(agent): add deferred tool search middleware`。

## TS-03 TUI / Web / ACP 接入

- [x] TS-03.1 TUI normal/plan 使用统一 ToolPlan。
- [x] TS-03.2 Web normal/plan/automation 使用统一 ToolPlan。
- [x] TS-03.3 ACP normal/plan 使用统一 ToolPlan，并维持 ACP 不暴露 Browser 的约束。
- [x] TS-03.4 Browser、Computer、Memory、MCP、goal、team 状态变化后正确重建计划。
- [x] TS-03.5 验证三种 transport 对同一工具分类一致，且 transport 特例明确可测。

完成证据：

- 实现：新增唯一 `commandToolPolicies` 与 `buildCommandToolPlan`；三 transport 在开关关闭时保持 eager/static，在开启时使用同一分类器。
- 首轮：TUI/Web Normal 为 11 Direct + `tool_search` = 12；ACP Normal 为 11；TUI/Web Plan 为 7；ACP Plan 为 6。
- MCP：从 builtin candidates 分离并使用同一代快照；Normal 全部 Deferred，Plan Hidden；跨 builtin/server 重名 fail closed。
- 重建：mode、Browser、Computer、MCP、模型配置变化继续重建 Agent；测试验证新一代候选不会继承旧 Browser/MCP runtime。
- 测试：command policy/matrix 定向测试；`go test -race ./internal/command -run 'TestBuildCommandToolPlan|TestCommandToolPolicy' -count=1`；`go test ./...`；`make lint-go`。
- 结果：全部通过；TUI/Web/ACP × Normal/Plan、unattended filter、MCP、scope、collision、stable runtime 均覆盖；lint `0 issues`。
- 提交：`0f55179 feat(command): defer low-frequency tools across transports`。

## TS-04 MCP 命名、审批与权限

- [x] TS-04.1 MCP 模型侧名称改为 `mcp__<server>__<tool>`，保留 UI 展示名和真实 endpoint 映射。
- [x] TS-04.2 使用 registry metadata 判断 MCP 来源，不依赖字符串前缀。
- [x] TS-04.3 MCP 按 server/tool 稳定排序；跨 server 原名可共存，sanitize/长度冲突按 Codex 策略加稳定 hash，内建最终名冲突由 ToolPlan fail closed。
- [x] TS-04.4 `tool_search`、`load_skill`、`goal_get` 等只读工具免审批。
- [x] TS-04.5 验证 Deferred 写工具仍按原策略审批，搜索激活不等于授权。
- [x] TS-04.6 审计并修正 subagent、workflow、team child agent 的审批和 plan 硬边界，再决定是否接入 ToolSearch。

完成证据：

- MCP 命名：仅模型侧 `ToolInfo.Name` 改为 canonical；Invokable/Enhanced endpoint、原始 server/tool、参数、option 和结果保持透传。
- 来源/UI：canonical registry 同时保存 server、raw endpoint 和 `server.raw_tool`；Web 展示不泄漏 canonical 拼接名；manager 不再用 raw 名污染 provenance。
- 测试：MCP tools/command/handler 定向测试；三包定向 race；`go test ./...`；`make lint-go`。
- 结果：全部通过；race 无异常；lint `0 issues`。覆盖同名跨 server、非法字符、sanitize collision、长名、重复 identity、稳定排序、raw endpoint 透传与 UI display。
- 已完成提交：`c9cb076 feat(tools): canonicalize MCP tool identities`（TS-04.1～TS-04.3）。
- 审批：MCP registry provenance 在 builtin allowlist 前判定；`tool_search`、`load_skill`、`goal_get` 只读直通；`goal_set/update`、automation、memory、workflow 等仍 fail closed。
- 授权测试：模型先 search 激活 Deferred 写工具，再由 approval 拒绝；断言真实 endpoint 调用次数为 0，证明 disclosure 不授予权限。
- 测试：runner/agent 定向测试；两包定向 race；`go test ./...`；`make lint-go`；全部通过，lint `0 issues`。
- 已完成提交：`1aa128e fix(runner): preserve approval across tool disclosure`（TS-04.4～TS-04.5）。
- Plan endpoint：新增三 transport 共用的 `NewPlanExecuteTool`，在 executor 前拒绝 background、非 allowlist 命令、shell 注入和危险 git helper/output 参数；runner 复用同一 classifier。
- Browser Plan：schema 仅广告既有 tab 的 list/select，endpoint 在打开 session 前拒绝 `new_tab=true` 及 new/claim/close；Normal schema/endpoint 不变。
- 测试：tools/runner/command 定向测试；三包定向 race；`go test ./...`；`make lint-go`；全部通过，lint `0 issues`。拒绝用例断言 executor 调用数不增加。
- 已完成提交：`a316830 fix(tools): enforce Plan mode at execution boundary`（TS-04.6 / Plan 子阶段）。
- Subagent：explore 仅注册 read/grep/Plan execute；general/coordinator 的写/任意 execute 由父 `subagent` 调用一次性授权，background 不改变授权位置。
- Workflow：explore 使用 Plan execute；general/coordinator 继承 `workflow_run` 的一次性授权；所有 flow child 都挂 safe-error middleware，不在后台等待交互审批。
- 测试：subagent/flow tool matrix、schema grant 文案、runner grant/background/malformed matrix、flow error folding；定向、两包 race、`go test ./...`、`make lint-go` 全部通过。
- 已完成提交：`b1b687f fix(tools): bound delegated child permissions`（TS-04.6 / subagent+workflow 子阶段）。
- Team：`agent_type` 规范为 `explore/general/coder`，`mode` 规范为 `normal/plan/auto`；缺省为 `general+normal`，未知值在启动 goroutine 前 fail closed。Explore 或 Plan child 只有 read/grep/Plan execute；Normal 写调用共享 leader 的逐次审批；仅 general/coder+auto 在父 `team_spawn` 请求一次性授权。
- Team schema：`agent_type` / `mode` 提供 enum、default 和一次性授权说明；结果回显规范化 type/mode。Manager 的 ToolBuilder、PromptBuilder、HandlersFactory 和持久化状态均携带同一规范化 permission，缺少依赖或 nil 环境时不启动 child。
- Child ToolSearch 决策：暂不接入。Explore/Plan 只有 3 个、general/coder 只有 7 个 schema，不存在足以抵消 middleware/reminder 开销的 schema 压力；保留静态小集合可使权限 profile 更直接可审计。后续只有 child 工具集显著增长并通过同等权限矩阵后才重新评估。
- 测试：`go test ./internal/team ./internal/command ./internal/runner ./internal/tools`；`go test -race ./internal/team ./internal/command ./internal/runner ./internal/tools`；`go test ./...`；`make lint-go`；全部通过，race 无异常，lint `0 issues`。
- 已完成提交：`3b751db fix(team): enforce child permission profiles`（TS-04.6 / team 子阶段，TS-04 完成）。

## TS-05 Prompt、schema 与观测

- [x] TS-05.1 System/Plan prompt 只将当前请求实际附带的 function schemas 作为可用工具与参数的唯一真相；Deferred 规则仅在启用时出现。
- [x] TS-05.2 教会模型使用 `select:name`、关键词、`+required`，并避免重复 search/select 和同 batch 调用新目标。
- [x] TS-05.3 删除 `load_skill` schema 与 system prompt 重复的技能目录文本。
- [x] TS-05.4 记录每次模型请求的可见工具名、schema bytes/token 估算和调用序号。
- [x] TS-05.5 以 metadata-only 方式记录 search 模式/长度、结果名、激活目标、重复搜索及 Deferred bypass；不持久化原 query。
- [ ] TS-05.6 日志和评测产物不得包含 API key、authorization header 或其他凭证。

完成证据：

- Prompt/schema 子阶段：System/Plan 删除静态内建工具库存和无条件 `tool_search` 假设；模型只依据当前请求 schemas 判断工具可用性与参数。Deferred 非空时沿用 Eino v0.9.9 内建的关键词、`select:` 多选、`+required`、命中即加载和禁止重复 search/select 说明，JCode 仅补充 Eino 缺失的“search 与新目标分开 batch”约束。
- Skill schema：`load_skill` 的 ToolInfo 使用稳定通用描述，不随 loader catalog 改变；缺失或未知名称的运行时结果仍列出当前可用技能。
- 测试：`go test ./internal/prompts ./internal/skills ./internal/agent`；`go test -race ./internal/prompts ./internal/skills ./internal/agent`；`go test ./...`；`make lint-go`。
- 结果：全部通过；race 无异常；lint `0 issues`。测试验证基础 prompt 无静态清单、Deferred 条件注入、Eino schema 覆盖全部搜索语法、legacy/no-Deferred 不出现 ToolSearch 规则，以及 skill schema 不随动态目录变化。
- 已完成提交：`dbe7ca4 feat(agent): align prompts with dynamic tool schemas`（TS-05.1～TS-05.3）。
- 观测子阶段：在 Eino ToolSearch rewrite 之后、状态重写/审批之前安装 metadata observer；`WrapModel` 针对每次真实 provider attempt 记录最终可见 canonical tool names、数量、schema bytes、`EstimateTokens` 估算、新披露 Deferred 和单调 request seq。
- Search/bypass：记录 query mode/bytes、term/required 数、max_results、已验证 select 名、未知 select 数、match/new-match、重复与冗余标志；不记录原 query、完整 args/schema/output/error。Deferred 调用若不在最后一次模型可见集合中，记录 bypass；模型级测试证明同 batch search+target 会被捕获。
- 持久化：新增 `tool_observation` JSONL entry，runner 每 turn 注入并发安全 sink；resume/replay 忽略新增元数据而不改变历史。Session/teammate/index/last-session 文件改为 owner-only `0600`，目录改为 `0700`，旧的宽松 session 在 append 前收紧。
- 测试：`go test ./internal/agent ./internal/session ./internal/runner`；`go test -race ./internal/agent ./internal/session ./internal/runner`；`go test ./...`；`make lint-go`。
- 结果：全部通过；race 无异常；lint `0 issues`。隐私测试向 query/args/output 注入 canary 并断言 observation 序列化不含 canary；权限测试验证新建、index、last-session 和 resume 文件模式。
- 已完成提交：`69ba9ee feat(agent): record tool disclosure metadata`（TS-05.4～TS-05.5）。
- Web 评测安全预审发现 `SaveConfig` 会用 `0755/0644` 保存包含 API key、custom headers、MCP 和 remote 凭证的配置。现已将新建及 legacy 路径在写入前后统一收紧为目录 `0700`、文件 `0600`，覆盖 Browser/Computer/MCP 等所有 Web 配置保存入口。
- 测试：`go test ./internal/config`；`go test -race ./internal/config`；`go test ./...`；`make lint-go`。全部通过，lint `0 issues`。
- 已完成提交：`f905ae2 fix(config): keep credential files owner-only`（TS-05.6 安全子阶段）。
- 待完成：TS-05.6 仍需由 TS-07 的隔离 HOME、脱敏 publish bundle 和全产物凭证扫描共同关闭；现阶段已证明 observer 不复制敏感 payload，且配置保存不会降级 owner-only 权限。

## TS-06 自动化测试与 deterministic fixture

- [x] TS-06.1 单元测试：首轮仅 Direct + `tool_search`。
- [x] TS-06.2 单元测试：搜索后仅披露命中工具，多次搜索集合单调增长。
- [x] TS-06.3 单元测试：重名、保留名、空 ToolInfo 和错误配置启动失败。
- [x] TS-06.4 集成测试：真实 Deferred endpoint 的参数、结果和审批链保持不变。
- [x] TS-06.5 集成测试：检测未搜索直接调用、同 batch search+target、orphan tool call。
- [x] TS-06.6 增加本地 deterministic MCP fixture，覆盖 10/30/50/100 工具规模和相似名称干扰。
- [x] TS-06.7 增加声明式 routing oracle：工具序列、搜索命中、参数/result marker、禁止冗余搜索。
- [x] TS-06.8 覆盖 Browser、Computer、MCP reload、mode switch、compaction/resume 和禁用后撤权。

完成证据：

- Fixture：新增本地 stdio MCP server，稳定排序暴露 10/30/50/100 个工具；`catalog_lookup_precise` 是唯一目标，另有 9 个近似名干扰项与 filler。每次 endpoint 调用写 owner-only JSONL，记录 sequence、raw tool、合成参数和 deterministic marker，MCP stdout 只承载协议。
- Harness 注入：`orchestrate.py --mcp-fixture` 只接受 case 的安全 `server_name/tool_count`，command/args/log path 由 orchestrator 生成；拒绝 case 注入 command/env/headers，server 配置不含凭证字段。
- Routing oracle：以 session JSONL canonical name/call-id/batch 为权威，严格配对 call/result；只有成功 search 所在 batch 完整结束后才激活。检测 bypass、same-batch activation、冗余搜索、非 Deferred 命中、orphan/错名/失败结果，并将 fixture raw endpoint、参数、marker、session result、expected_calls 交叉验证。
- 真实格式兼容：Go 的 `batch_index=0` 因 `omitempty` 缺省，oracle 将“有 batch_id + 正数 batch_size + 缺 index”解释为首项，同时仍拒绝重复/越界 index；folded `Tool execution failed/panicked/approval error` 不计成功。Verdict 不复制原 search query 或完整 args。
- 测试：`go test ./agent-eval/fixture/mcp`；`go test -race ./agent-eval/fixture/mcp`；`python3 -m unittest agent-eval/suite/test_routing_verify.py`（9 tests）；`go test ./...`；`make lint-go`。
- 结果：全部通过；race 无异常；lint `0 issues`。覆盖正常路由、bypass、same batch、重复 search、fixture args/marker mismatch、call/result 错名、folded failure 和安全注入。
- 已完成提交：`77de5fc test(agent): add deterministic tool routing oracle`（TS-06.6～TS-06.7）。
- TS-06.1～06.3：`TestNewAgentWithToolPlanActivatesDeferredTool` 严格断言首轮只有 Direct + `tool_search`，搜索只披露命中项；accumulation/unknown-search 用例覆盖单调增长与未命中不披露；builder/manual-plan 用例覆盖重名、保留名/alias、nil/空/报错 ToolInfo、非法 exposure 和 partition 配置，全部 fail closed。
- TS-06.4：真实 Deferred endpoint 明确断言原始 JSON 参数、tool call ID/name、确定性原始结果到下一轮模型均不变；approval 用例证明披露不会授权写工具，变更工具仍须审批。
- TS-06.5：routing oracle 除 bypass 与 same-batch 外，新增“call 无 result”和“result 早于 call”两个 orphan 拒绝用例；Python suite 由 9 增至 11 个。
- 测试：`go test ./internal/agent ./internal/runner`；`go test -race ./internal/agent ./internal/runner`；`python3 -m unittest agent-eval/suite/test_routing_verify.py`（11 tests）；全部通过。
- 已完成提交：`92e0cb0 test(agent): harden deferred routing invariants`（TS-06.1～TS-06.5）。
- TS-06.8：generation 测试证明新 Agent 不继承旧的隐式激活；未压缩的精确 `tool_search` 结果可在 resume 时恢复，而 compaction 的自然语言摘要不作为 executable capability，必须重新搜索。Browser/Computer capability、Plan mode 和 ACP transport 撤权后，旧 endpoint 不进入 runtime registry，模型猜名执行失败。
- Static 兼容边界：`tool_search.enabled=false` 只切换为当前 catalog 的 eager/static 暴露，不等同 capability revoke；真正被当前 transport/mode/capability/catalog 移除的 endpoint 在 static 路径同样不可调用。
- MCP 修复：Web MCP reload 从“只重建 active task”改为重建所有 live task；复用 engine revision guard，慢 reload 不会覆盖并发的 Plan mode 切换。测试覆盖 active/background 去重重建与 stale generation 丢弃。
- 测试：`go test ./internal/agent ./internal/command ./internal/web ./internal/tools ./internal/session ./internal/runner`；`go test -race ./internal/agent ./internal/command ./internal/web`；`go test ./...`；`make lint-go`。全部通过，race 无异常，lint `0 issues`。
- 已完成提交：`56fa6eb fix(web): revoke stale tools across agent rebuilds`（TS-06.8，TS-06 完成）。

## TS-07 Kimi A/B 评测

- [ ] TS-07.1 修正评测映射，精确使用 `kimi-for-coding/kimi-for-coding`，不使用 high-speed SKU。
- [ ] TS-07.2 不发送 temperature；记录非敏感的实际 model ID、effort、variant 和工具数量。
- [ ] TS-07.3 修复 isolated HOME 产物保留真实 `config.json` 的凭证泄露风险。
- [ ] TS-07.4 增加 `static` / `deferred` variant、明确 repeats 和随机交错执行。
- [ ] TS-07.5 从 session JSONL 提取真实工具调用序列、batch、args、结果和耗时。
- [ ] TS-07.6 覆盖 Direct、精确搜索、英文/中文语义、多目标、复杂参数、Browser、Computer、MCP 和负面场景。
- [ ] TS-07.7 Canary 通过后运行关键用例至少 10 次，不能用 aggregate 掩盖单项失败。

Kimi 硬门槛：

- Deferred bypass = 0。
- Search 与目标工具同 batch = 0。
- Deferred 参数有效和调用成功率 ≥ 98%。
- Direct/no-tool 无关搜索率 ≤ 2%。
- Deferred 任务通过率 ≥ 95%，关键场景至少 9/10。
- Direct 相对 static 非劣于 -3pp。
- 首轮普通模式可见工具 ≤ 12，全功能场景 schema token 至少下降 50%。

完成证据：

- 测试：待填写
- 结果：待填写
- 提交：待填写

## TS-08 至少 30 分钟全场景回归

- [ ] TS-08.1 构建固定二进制和 fixture，记录 Git SHA、Go 版本、OS、模型和非敏感参数。
- [ ] TS-08.2 连续测试累计实际运行时间 ≥ 30 分钟。
- [ ] TS-08.3 覆盖 static/deferred、TUI/Web/ACP 可自动化路径、normal/plan、能力开关及失败恢复。
- [ ] TS-08.4 覆盖 Direct、Deferred、Browser、Computer、MCP、审批、模式切换和负面场景。
- [ ] TS-08.5 保存结构化原始结果，确认无凭证残留。
- [ ] TS-08.6 对所有失败进行分类；硬门槛失败必须修复并重新完整运行。

完成证据：

- 开始/结束：待填写
- 总耗时：待填写
- 场景数/运行数：待填写
- 结果目录：待填写
- 提交：待填写

## TS-09 HTML 测试报告与完成审计

- [ ] TS-09.1 生成独立 HTML 报告，包含环境、设计、用例矩阵、调用轨迹、指标和失败详情。
- [ ] TS-09.2 报告明确列出 Direct/Deferred 正确率、bypass、参数有效率、token 和时延对比。
- [ ] TS-09.3 报告包含不少于 30 分钟运行的起止时间与原始结果链接。
- [ ] TS-09.4 对 TS-00 至 TS-09 逐项核验证据，确认没有以窄测试代替全范围要求。
- [ ] TS-09.5 提交报告及最终文档状态。

完成证据：

- HTML：待填写
- 校验：待填写
- 提交：待填写

## 更新日志

| 时间 | 更新 | 测试 | 提交 |
|---|---|---|---|
| 2026-07-18 | 完成 TS-00：创建清单并采集基线 | `git diff --check`、`go test ./...` 通过 | `a1d0b13` |
| 2026-07-18 | 完成 TS-01：ToolPlan 分类、校验、稳定排序和 Hidden runtime 边界 | 包测试、race、全量 Go、lint 通过 | `be5e701` |
| 2026-07-18 | 完成 TS-02：接入 Eino 客户端 ToolSearch 与 opt-in 配置 | 模型级、race、全量 Go、lint 通过 | `0a03ce6` |
| 2026-07-18 | 完成 TS-03：TUI/Web/ACP 共享 tool policy 和重建路径 | 矩阵、race、全量 Go、lint 通过 | `0f55179` |
| 2026-07-18 | 完成 TS-04.1～04.3：MCP canonical identity、稳定消歧、raw endpoint/展示映射 | 定向、race、全量 Go、lint 通过 | `c9cb076` |
| 2026-07-18 | 完成 TS-04.4～04.5：只读免审、MCP provenance 优先、Deferred 激活不授权 | 定向、race、全量 Go、lint 通过 | `1aa128e` |
| 2026-07-18 | 完成 TS-04.6 子阶段：Plan execute/Browser 在 endpoint 层硬拒绝越权 | 定向、race、全量 Go、lint 通过 | `a316830` |
| 2026-07-18 | 完成 TS-04.6 子阶段：subagent/workflow 一次性授权与 observe profile | 定向、race、全量 Go、lint 通过 | `b1b687f` |
| 2026-07-18 | 完成 TS-04.6 与 TS-04：team child 权限矩阵、fail-closed mode、父级一次性授权；小工具集暂不接 ToolSearch | 聚焦、四包 race、全量 Go、lint 通过 | `3b751db` |
| 2026-07-18 | 完成 TS-05.1～05.3：prompt 以请求 schemas 为真相、条件 ToolSearch 指引、skill schema 去重 | 聚焦、三包 race、全量 Go、lint 通过 | `dbe7ca4` |
| 2026-07-18 | 完成 TS-05.4～05.5：provider-attempt schema 指标、metadata-only search/bypass、session owner-only 权限 | 聚焦、三包 race、全量 Go、lint 通过 | `69ba9ee` |
| 2026-07-18 | 完成 TS-06.6～06.7：10/30/50/100-tool MCP fixture、session/fixture routing oracle | fixture normal/race、Python 9 tests、全量 Go、lint 通过 | `77de5fc` |
| 2026-07-18 | 完成 TS-06.1～06.5：首轮/命中/单调增长/fail-closed/参数结果审批/orphan 路由不变量 | agent/runner normal+race、Python 11 tests 通过 | `92e0cb0` |
| 2026-07-18 | 完成 TS-06.8 与 TS-06：generation/resume/compaction/capability 矩阵，修复 Web MCP 后台任务残留 endpoint | 相关包、三包 race、全量 Go、lint 通过 | `56fa6eb` |
| 2026-07-18 | TS-05.6 安全子阶段：修复 Web 保存配置把 API key 文件降为 `0644` | config normal/race、全量 Go、lint 通过 | `f905ae2` |
