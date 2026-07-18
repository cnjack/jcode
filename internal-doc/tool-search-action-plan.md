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
| TS-05 Prompt 与观测 | 进行中 | `dbe7ca4`、`69ba9ee`、`f905ae2`、`8675a5b`、`92c2f31` | 使用说明、成功调用去重规则、schema/轨迹指标、凭证文件权限及 Browser 子进程 token 隔离 |
| TS-06 自动化测试 | 完成 | `77de5fc`、`92e0cb0`、`56fa6eb` | 单元、集成、fixture、routing oracle 与生命周期撤权矩阵 |
| TS-07 Kimi A/B | 进行中 | `28eae39`、`3a1ece8`、`7d713ef`、`f75041d`、`91bbc8b`、`fe81907`、`92d442e`、`8675a5b`、`d60000d`、`8c980b1`、`207b313`、`7530b38`、`92c2f31`、`ef66470` | MCP100 Deferred repeat 9/10、跨类小矩阵 12/12；等待 formal pass@10 |
| TS-08 30 分钟回归 | 进行中 | `9f2889e`、`8c980b1`、`b97734d` | formal 首次真实启动在 matrix 前因中文 Browser static 漏 snapshot 严格失败；五步 Prompt 已修复，等待 canary 与全新完整重跑 |
| TS-09 HTML 报告 | 进行中 | `ad55a3b`、`92d442e`、`9f2889e`、`8c980b1` | 安全、fail-closed 的九门槛报告器、canonical suite、build-tag 与 supplementary 合约已完成；等待真实 campaign 产物 |

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
- 运行前只读核验发现现有 `~/.jcode` / `config.json` 仍为历史 `0755/0644`；已仅收紧权限为 `0700/0600`，未重写或更改配置内容，随后再次 `stat` 验证生效。
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

- [x] TS-07.1 修正评测映射，精确使用 `kimi-for-coding/kimi-for-coding`，不使用 high-speed SKU。
- [x] TS-07.2 不发送 temperature；记录非敏感的实际 model ID、effort、variant 和工具数量。
- [x] TS-07.3 修复 isolated HOME 产物保留真实 `config.json` 的凭证泄露风险。
- [x] TS-07.4 增加 `static` / `deferred` variant、明确 repeats 和随机交错执行。
- [x] TS-07.5 从 session JSONL 提取真实工具调用序列、batch、args、结果和耗时。
- [x] TS-07.6 覆盖 Direct、精确搜索、英文/中文语义、多目标、复杂参数、Browser、Computer、MCP 和负面场景。
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

- Model/request：`kimi-for-coding` label 硬映射为精确 `kimi-for-coding/kimi-for-coding`，显式拒绝 highspeed 漂移；selected provider config 不复制 temperature，record 明确标注 `temperature=omitted`、model ID、effort、variant、seed 与工具/schema 数量。
- Pairing：新增 `static/deferred`、显式 repeats、固定 seed 的成对随机交错；formal 模式强制 exact Kimi、两个 variant、显式 repeats、`workers=1`，保持 legacy 默认单 static 兼容。
- 隔离：HOME 只复制目标 provider 运行字段，不复制其他 provider、telemetry 或 model state；目录/文件 `0700/0600`。正常、异常路径都删除 config、raw session/event/result/work。子进程改用固定无用户路径的安全 PATH、隔离 TMPDIR 和最小 env，不继承无关 API token、proxy credential、SSH agent 或 `JCODE_WEB_TOKEN`。
- Trajectory：以真实 session JSONL 提取 call/result 顺序、call ID、batch、status、duration、model-visible names、schema bytes/token 和 ToolSearch metadata；默认不复制 query/args/output，仅 testcase 明确声明的 deterministic fixture args 可发布，result 始终 metadata-only。
- Artifact safety：新增 exact secret/host path + 高置信 credential redaction、metadata-only findings 和 post-redaction scan；canary 测试证明扫描报告自身不回显凭证或宿主路径。
- 测试：`python3 -m unittest discover -s agent-eval/suite -p 'test_*.py'`（20 tests）；legacy static dry-run；formal exact-Kimi paired dry-run（4 jobs）；`git diff --check`。全部通过。
- 已完成提交：`28eae39 test(eval): secure paired ToolSearch campaigns`（TS-07.1～TS-07.5）。
- TS-07.6 矩阵子阶段：新增独立 16-case/13-critical 声明式矩阵，覆盖 Direct、英/中文 no-tool、精确/英/中文语义、多目标、复杂参数、MCP 10/30/50/100+相似名干扰、deterministic Computer、Web-only loopback Browser、unknown/unrelated negative search。ACP 明确 15 cases 且不冒充 Browser，Browser 单独 1 个 Web case。
- Matrix validator：展开完整 MCP catalog/distractors，复用既有 Computer fixture，校验唯一 ID、variant routing、参数 matcher、separate-batch、metric tags、安全字段和固定 9 项硬门槛；拒绝 credential、command/env 注入与外部 URL。
- 测试：matrix + routing verifier 19 tests、JSON parse、py_compile、16/13/ACP15/Web1 summary 全部通过。
- 已完成提交：`3a1ece8 test(eval): define ToolSearch acceptance matrix`（TS-07.6 matrix 子阶段）。
- Web Browser driver 子阶段：新增独立 authenticated Web driver，固定 exact Kimi，支持 static/deferred、英/中文、success/approval-deny/browser-disabled。真实 loopback HTML form 用 open/fill/click callback 与 session `browser_read` 双证据，且正确提交必须恰好一次。
- Web 安全/生命周期：Bearer token 只进最小 child env，stdout 丢弃、stderr 私有后删除；验证 health/model/auth 401+200/Browser status，轮询 running→两次 idle 与 pending approval/ask；超时 stop，最终 SIGTERM/SIGKILL 整个进程组。发布 record 为 allowlist metadata，raw session path 只作进程内 handoff。
- 测试：Web driver 12 tests + py_compile + diff check 全部通过；覆盖 success、中文 Deferred、static 错搜、缺 search、审批拒绝、Browser disabled、超时、highspeed 拒绝、session canary 与凭证/路径不回显。
- 已完成提交：`7d713ef test(eval): add authenticated Web Browser driver`（TS-07.6 Web driver 子阶段）。
- ACP routing dispatch 子阶段：实际 orchestrator 现在显式加载 ToolSearch matrix，只运行 `surface=acp`，默认把 Web-only Browser case 记录为 handoff，显式把 Browser 交给 ACP 时 fail closed。case 自身 variant 会限制 job；两个负面案例只生成 Deferred job，formal paired block 保持相邻且 `workers=1`。
- 路由判定：新增 metadata-only expected-routing verifier，逐次校验搜索次数/模式/命中、空结果、必需/可选/禁止调用、exact/contains 参数、调用顺序、call/result 配对、独立 batch 激活、bypass 与 same-batch。MCP static/deferred 两臂均保留 fixture endpoint/args/result marker 交叉验证，只有 Deferred 要求 ToolSearch 激活。
- 测试：Python suite 51 tests；legacy dry-run 39 jobs；formal ToolSearch dry-run 28 jobs，固定 `kimi-for-coding/kimi-for-coding`、`workers=1`、ACP 15 cases、Browser handoff 1 case；diff check 全部通过。
- 已完成提交：`f75041d test(eval): enforce ToolSearch routing expectations`（TS-07.6 ACP routing dispatch 子阶段）。
- 首次真实 canary 暴露评测器路径缺陷：ACP 协议 ID 为 `sess_<uuid>`，私有 recorder 文件实际是 `<uuid>.json`；旧 runner 因把 `sess_` 拼进文件名而将六条成功 session 全误报为 missing。现以严格 UUID 格式映射并拒绝 prefix 缺失、路径穿越和非法 ID。
- exact-Kimi 修复后 canary（非正式、不能计入 TS-07.7）：3 cases × static/deferred = 6 runs；6/6 routing verdict 通过、2/2 MCP fixture 交叉验证通过、5/6 task oracle 通过。唯一 task fail 的 ToolSearch→`goal_get` 调用、参数、独立 batch、结果均通过，失败来自 final-text 固定措辞 oracle，需在正式 canary 前消除该评测噪声。
- 测试：相关 Python 32 tests、全 Python suite 67 tests、`py_compile`、diff check 通过；真实重跑结果如上。
- 已完成提交：`91bbc8b fix(eval): resolve ACP recorder session paths`（TS-07 canary 基础设施修复；未勾选 TS-07.7）。
- Web dispatch 子阶段：新增独立 Web-only orchestrator，以 authenticated Web driver 运行真实 Browser surface；formal 固定 exact Kimi、单语言、`workers=1` 和相邻 static/deferred pair，ACP case 显式拒绝。标准 record/trajectory/redaction 三件套直接通过报告器 validator，raw session 必须位于 isolated HOME，否则 fail closed。
- Browser matrix 与真实 driver 对齐为 runner-owned loopback proof form，明示 success 的 navigate/interact preapproval；required/order/Deferred 激活覆盖 `open → snapshot → act(fill) → act(click) → read`，不再用只读静态 body 冒充完整 Browser-use。approval-deny/browser-disabled 为独立 supplementary 场景，使用 driver 真值且不误套 success routing oracle。
- 安全：只复制 selected provider，最小 HOME、无 temperature、owner-only 文件；driver publication allowlist、session scope、usage、routing、artifact scan 全部在清理 HOME 前完成；异常也不保留 config/session/work。deny/disabled 使用合法 `default_mode=approval`。
- 测试：Python suite 69 tests、report record/trajectory validators、formal Web dry-run 4 jobs、`py_compile`、diff check 全部通过。
- 已完成提交：`fe81907 test(eval): dispatch Browser ToolSearch through Web`（TS-07.6 Web dispatch 子阶段；TS-07.6 完成）。
- Canary 指标校准：3 个 `goal_get` case 改用稳定 `NO_GOAL_SET_OK` sentinel，仅消除最终自然语言措辞噪声；工具搜索、调用次数、空参数、call/result 和独立 batch oracle 保持严格。首轮 schema 降低门槛仍为 ≥50%，但 scope 固定为唯一的完整 100-tool catalog 配对 case；普通工具集约 18% 的实际降幅继续逐 case 展示，不能参与稀释或抬高该门槛。validator 会拒绝缺失/重复/unpaired full-schema tag 及 scope 漂移。
- 测试：ToolSearch discovery 48 tests、`py_compile`、JSON parse、`git diff --check` 全部通过；synthetic 证明普通 case 18% 不影响 full-catalog 60% PASS，full-catalog 回退到 40% 必然 FAIL。
- 已完成提交：`92d442e test(eval): calibrate ToolSearch acceptance scopes`（正式 canary 前校准；未勾选 TS-07.7 或 TS-09）。
- 首次 Web Browser exact-Kimi canary（非正式、不能计入 TS-07.7）：static/deferred 均正确进入 Browser 路由，Deferred 首轮 12 tools、无 bypass、无 same-batch 激活，但两臂都在首个 `browser_open` 阻塞至 360 秒后被 driver 安全停止，结果 0/2；因此未把路由部分成功误报为 task pass。
- 根因 A/B：同一 managed Chrome 代码在正常 HOME 下 `Page.navigate` 约 24ms，在评测隔离 HOME 下稳定超时；macOS Chrome 子进程单独恢复经 `os/user` 校验的系统 HOME 后，JCode 自身隔离 HOME、config/session 和 `--user-data-dir` 均保持不变，完整 open→snapshot→click→screenshot smoke 在相同隔离环境 0.96s 通过。
- Browser 韧性/安全：Chrome 子进程不再继承 `JCODE_WEB_TOKEN`；`browser_open` 在创建 session 前仅接受无 userinfo 且 host 有效的 HTTP(S) URL；每个 Browser tool 有 30 秒总 operation timeout，内部 deadline 转为不含 URL/path 的稳定错误；domain enable 继承父 context。Web driver 只接受裸 UUID 或 `sess_<uuid>`，统一映射 recorder `<uuid>.json` 并拒绝 traversal。
- 测试：`go test ./internal/browser ./internal/tools`、聚焦 race、Python Web driver 13/13、`py_compile`、gofmt/diff check 及上述真实隔离 Chrome smoke 全部通过。
- 已完成提交：`8675a5b fix(browser): harden managed Chrome operations`（Web canary 产品/driver 修复；未勾选 TS-07.7）。
- 修复后 Web Browser exact-Kimi canary（仍为非正式）：固定提交 `e559c41a49cc`、jcode SHA-256 `47799786722dd8933fa16701e115efdb0a361096a760ba3b164859381f675473`；static 14.665s 全链路 PASS，Deferred 24.698s 完成真实 open/fill/click/read proof，但严格结果为 1/2。
- Deferred 诊断：首轮 12 tools、5458 estimated schema tokens（static 24/7612），3 次 `tool_search` 最终匹配全部 4 个所需 Browser tools，bypass=0、same-batch=0；模型先只披露 open/snapshot，因后续 act/read 尚隐藏而重复 `browser_open` 3 次，其中 1 次失败，故 routing/task gate 正确 FAIL，不能用 proof 最终成功掩盖多余/失败调用。
- Capability-family 修复：`ToolDescriptor` 新增独立 `DisclosureGroup`；Eino 客户端 `tool_search` 成功命中显式组成员后，把当前 transport/mode/capability gate 后仍为 Deferred 的同组成员稳定追加到 `matches`。扩展结果写入历史，并且只能在下一次模型 generation 生效，因此不会把同 batch 调用合法化。
- 显式小组严格限定为 Browser `open/snapshot/act/read` 和 Computer `open/snapshot/act/apps`；`browser_eval`、tabs、截图、`computer_read`、截图保持精确披露。MCP 不设置 group，即使同一 canonical server 有 32 个工具也不会命中一个后整 server 展开；首轮 Direct/tool_search 数量和 prompt 均未改变。
- 权限/生命周期：group map 只从最终有效 Deferred 描述符生成，Hidden、transport/mode/capability-revoked peer 不会被披露或注册；自动披露的 `browser_act` 仍逐次经过 approval，拒绝后 endpoint 0 次；grouped search+target 同 batch 仍记录 bypass。新 Agent 可从已持久化 expanded result 恢复整组，而旧版未扩展历史只恢复原 match。
- 独立 review：确认 Eino handler 顺序为 ToolSearch → Observation → Disclosure → caller/approval，返回路径由 Disclosure 先扩展、Observation 再记录；未发现阻断、权限泄漏或同批次激活问题。review 提出的历史恢复、grouped same-batch、自动披露写工具审批和同 server MCP 四个测试盲点均已补齐。
- 测试：`go test ./internal/agent ./internal/command -count=1`；`go test -race ./internal/agent ./internal/command -count=1`；`go test ./... -count=1`（允许 localhost 的环境）；`make lint-go`；`git diff --check`。全部通过，lint `0 issues`。
- 已完成提交：`d60000d feat(agent): disclose deferred capability groups`（exact-Kimi canary 产品修复；TS-07.7 仍未勾选）。
- capability-group 后首轮 4-job exact-Kimi canary：Browser static/deferred 均 PASS；Deferred 首轮 12 tools，仅 1 次 `tool_search`，一次即匹配 `open/snapshot/act/read` 四个成员，随后 open 1、snapshot 1、act 2、read 1 全部成功，bypass=0、same-batch=0。static/deferred 分别 11.649s/14.243s，证明重复 open 缺陷已修复。
- 同轮 Computer static/deferred 均在 `computer_open` 失败，故总体严格结果仅 2/4，未误报 canary 通过。根因是统一 coordinator 构建 JCode 时遗漏 `-tags jcode_eval`，deterministic fake Computer backend 没有编入二进制；两臂同时失败且 Browser 正常，和 ToolSearch/group 路由无关。
- Campaign 构建修复：JCode 固定使用 `go build -tags jcode_eval -trimpath`；plan/campaign 写入非敏感 `build.jcode_tags=["jcode_eval"]`，报告器精确校验并在 HTML reproducibility 中展示。缺失/错误 tag fail closed；binary hash、canonical suite hash 和 clean-Git 合约不变。
- 测试：Agent Eval Python 全量 91/91、`py_compile`、实际 `jcode_eval` binary build、`git diff --check` 全部通过。
- 已完成提交：`8c980b1 fix(eval): build deterministic Computer campaign`（canary infrastructure 修复；TS-07.7 仍未勾选）。
- `jcode_eval` 修复后第二轮 4-job canary 仍严格为 2/4：Computer static PASS，证明 fake backend 已生效；Computer Deferred 的 open/act 和落盘 oracle 全部成功、bypass=0、same-batch=0，但第一次 search 把五个精确 `computer_*` 名以逗号连接却漏掉 `select:`，Eino 因此空命中，Kimi 第二次补正后才完成，routing 对冗余/空搜索正确判 FAIL。Browser Deferred PASS；Browser static 完成 open/fill/click/read proof，但随机漏调 prompt 明确要求的 snapshot，routing 正确判 FAIL。
- Exact-list 兼容：只在 query 是 2～8 个逗号分隔、无重复、全部属于当前 effective Deferred canonical names 时，原位补 `select:`；1 个/超过 8 个、未知名、alias、普通语义、畸形/重复/未知 JSON 字段均原样交给 Eino。该规则对 MCP 只加载模型明确点名的工具，不按 server 扩展；`max_results` 字段原字节保留，但遵循 Eino direct-select 语义不再限流，八名上限是硬边界。
- Middleware/安全：Observation、caller handler 和真实 PreToolUse hook 仍看到模型原始 keyword 参数；approval 与 Eino endpoint 看到 repaired 只读 query。下一代 history/schema 正常激活，DisclosureGroup 可继续扩展有效 peer；Hidden/Direct 不进入候选，same-batch 仍被记录并阻断 endpoint，授权边界不变。
- 独立 review：两轮只读审查均未发现阻断；补齐真实 hook 顺序、exact-list+group 联动、MCP 精确边界、UTF-8 JSON escape/escaped duplicate key、异常 `max_results`、malformed/trailing 等回归。
- 测试：focused normal/race、`go test ./internal/agent -count=1`、`go test -race ./internal/agent -count=1`、`go test ./... -count=1`（允许 localhost）、`make lint-go`、`git diff --check` 全部通过，lint `0 issues`。
- 已完成提交：`207b313 fix(agent): accept exact deferred tool lists`（exact-Kimi routing 兼容；TS-07.7 仍未勾选）。
- 第三轮 Browser+Computer exact-Kimi canary：4/4 task、contracts、routing、artifact safety 全部 PASS。Browser Deferred 首轮 12 tools、一次 keyword search 即匹配完整 `open/snapshot/act/read` 组，随后 5 个 Deferred endpoint 全部成功；Computer Deferred 首轮 11 tools、一次 keyword search 匹配完整有效组，open/act 及 fixture oracle 全部成功。两者 bypass=0、same-batch=0、failed result=0。
- 时延：Browser static/deferred 分别 356.562s/17.706s，Computer static/deferred 分别 11.4s/15.8s。Browser static 虽严格通过，但接近 360 秒上限，作为 provider/Web 延迟风险保留观察，不能因最终 PASS 隐去；正式统计仍按真实 interval 计时。该轮 Kimi 使用了 13-byte 有效 Computer keyword，未再次触发逗号 exact-list 兼容路径；兼容路径由模型级、真实 hook 和 endpoint 集成测试证明，后续 formal 继续观测。
- Canary 结论：产品与 campaign 基础设施已具备进入跨类小矩阵的条件；本结果只是 repeat=1 的非正式 canary，不计入 TS-07.7 pass@10 或 TS-08 的 30 分钟门槛。
- 首轮 12-job 跨类小矩阵：no-tool、exact、MCP10、MCP100、Browser 的 static/deferred 与 Computer static 共 11 条全部 PASS；no-tool 两臂均 0 tool，MCP10/100 两臂均命中唯一 fixture endpoint 且未整 server 扩展。唯一失败是 Computer Deferred：任务、open/act、fixture oracle 和全部 endpoint 都成功，bypass=0、same-batch=0，但 Kimi 第一次把完整 6 个 Computer 名称逗号连接（92 bytes）却漏 `select:`，原五名上限拒绝兼容；第二次补 `select:`（99 bytes）后成功，routing 因首个空搜索严格 FAIL，故总计 11/12。
- 上限修复：兼容 ceiling 从 5 调整为 8，覆盖最大内建 Browser/Computer capability family，并继续拒绝 9+ catalog-wide list；新增 6-name Computer、8-name boundary 与 9-name fail-closed 测试。focused/race、全量 Go、lint `0 issues`、diff check 全部通过。
- 已完成提交：`7530b38 fix(agent): cover full capability tool lists`（12-job canary routing 修复；TS-07.7 仍未勾选）。
- 第二轮相同 12-job 小矩阵仍严格为 11/12：Computer Deferred 以 4 次调用、11.2s PASS，证明完整 6-tool family 的首次 query 已被兼容；no-tool 两臂继续保持 0 tool，exact、MCP10、Browser、Computer static 及 MCP100 static 均 PASS。唯一失败转为 MCP100 Deferred：一次 `select:` 正确命中唯一目标，bypass=0、same-batch=0、所有 endpoint result 均成功，但 Kimi 随后用完全相同参数连续调用目标 MCP 142 次，360.1s 超时；routing 因 required max=1、外部 fixture calls=142 和非正常终止严格 FAIL。相同提交的上一轮 MCP100 Deferred 曾以 search 1 + endpoint 1、7.942s PASS，说明披露/schema/参数路径一致，失败来自模型随机重复成功调用，而非 server 扩展或 Deferred 绕过。
- MCP completion 修复：目标 fixture 保留原 `content[0].text == marker` 与严格 external oracle，同时新增 `structuredContent` 的 `status=found`、`complete=true`、`authoritative=true`、请求回显和 synthetic record；distractor 继续只返回原 marker。这样评测仍严格衡量唯一工具/参数/次数，但不再要求模型从 opaque hash 猜测“权威记录已取得”。System 与 Plan prompt 同时加入成功调用复用规则，只禁止相同工具+相同参数的无进展重调，并明确豁免 incomplete、poll/retry 与外部状态变化。
- 安全/兼容：Eino MCP adapter 会把完整 `CallToolResult` 传给模型；routing verifier 继续递归匹配 marker，required max=1 未放宽。新增真实 structured wire-shape 与嵌套 secret payload 的 sanitizer 回归；trajectory 仍只保存 status/duration/output bytes，不保存 result payload。独立审查无 blocker。
- 测试：Prompt/fixture focused 与 race、Agent Eval Python 91/91、`py_compile`、全量 `go test ./... -count=1`、`make lint-go`、`git diff --check` 全部通过，lint `0 issues`。
- 已完成提交：`92c2f31 fix(eval): make MCP completion unambiguous`（MCP100 loop 诱因修复；TS-07.7 仍未勾选）。
- MCP100 Deferred 独立 10-repeat：10 条均在 6.153～10.224s 正常 `end_turn`，每条严格为一次 `tool_search` + 一次目标 MCP；routing、MCP external verifier、contracts、artifact safety 全部 10/10，bypass=0、same-batch=0、重复 target=0，证明 142-call loop 已消失。旧 task oracle 只有 7/10，三条失败唯一未过项均为 `final_text_contains` 内部 marker；三条仍有 252～328 字符自然答复，且工具真值全部通过。
- Oracle 修复：四个 MCP case 的 final oracle 改为用户可见的对应外部 SKU，不再要求泄露内部 `JCODE_MCP_FIXTURE_OK`。这不放宽工具准确性：普通 oracle、raw-session ToolSearch expectation 和独立 fixture/session marker verdict 仍按 AND 聚合；canonical endpoint、完整参数、结果 marker 与 0600 fixture log 逐条匹配，expected call multiset 必须精确相等。新增矩阵回归固定 static/deferred 两臂目标 spec 唯一且 `min=max=1`，全部 distractor 仍 forbidden。
- 独立审查无 blocker；确认错误 endpoint、参数、结果、重复调用、同批激活或绕过搜索均不能因 SKU oracle 通过而被掩盖。取舍是 SKU 已在 prompt 中，final oracle 本身不再单独证明输出 grounding；grounding 由上述双 raw verifier 负责，避免把内部测试 token 当用户答案契约。
- 测试：Agent Eval Python 91/91、JSON parse、`py_compile`、相关 oracle/routing 45 tests、`git diff --check` 全部通过。
- 已完成提交：`ef66470 test(eval): use user-facing MCP result oracles`（task oracle 校准；硬 routing gate 未放宽）。
- 新 suite hash 的 MCP100 Deferred 独立 10-repeat 严格 9/10 PASS，达到 critical ≥9/10 门槛。九条 PASS 均为 search 1 + target 1，6.5～10.5s 正常结束；唯一 r9 在 13.876s 内先 `select:` 并调用 forbidden `catalog_lookup_metadata`，再搜索/调用正确 precise，最终答复与 contracts 均正常，但 routing 与 external fixture verifier 因错误 distractor/额外 call 严格 FAIL。整轮无 bypass、same-batch 或成功调用循环。
- LoopGuard 决策：20 次结构化 completion 后未再出现相同成功调用循环；当前唯一失败是模型工具选择准确率，而非无进展重复。暂不加入会改变正常 polling/重复操作语义的通用 guard；formal 继续以原始 tool calls 严格计分和 360s timeout 控损。
- 最终 12-job 跨类小矩阵严格 12/12 PASS：no-tool static/deferred 均 0 call；exact 两臂、MCP10/100 两臂、Browser 两臂和 Computer 两臂的 task/contracts/routing/artifact 全部通过，MCP external verifier 4/4。所有 Deferred 总计 bypass=0、same-batch=0、failed result=0；MCP10/100 均 search 1 + precise 1；Browser Deferred 一次 keyword search 匹配完整 `open/snapshot/act/read` 组，随后 open1/snapshot1/act2/read1，15.695s；static 相同 proof 为 14.471s。
- Computer：Deferred 一次 keyword search 匹配所需 open/act，open1/act1 与 fixture oracle 通过；额外 load_skill/todo/execute/apps/snapshot 均为允许且成功的辅助步骤，未出现重复 search 或 forbidden call，总计 11 calls/22.912s。static 为 6 calls/15.054s。首轮 schema：完整 MCP100 static/deferred 19756/4906 estimated tokens（75.2% 降低），Browser 7612/5458，Computer 7456/4906；Deferred 首轮可见为 MCP 11、Browser 12、Computer 11，均不超过 12。
- 小矩阵结论：已消除前两轮分别暴露的 Computer exact-list 和 MCP completion/marker 噪声，没有 task pass 掩盖 routing 异常；可以从干净 Git 快照启动 canonical formal pass@10。
- 待完成：TS-07.7 需用 exact-Kimi canary 验证上述 Deferred 多步披露修复，并运行关键用例重复 A/B；TS-05.6 待真实 publish bundle 的最终扫描关闭。

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
- Coordinator 基础设施：新增统一 ACP/Web formal campaign，固定 exact Kimi、`workers=1`、每 case 至少 10 repeats、static/deferred 相邻随机 block；一次构建 jcode/ACP harness/MCP fixture 并记录 Git、Go、OS/arch、Eino 与 SHA-256，结束前复核 suite 和三份二进制未漂移。canary/dry-run 明确非 formal，不能作为验收证据。
- 覆盖/时长：正式 plan 必须是 canonical matrix/base suite 的内容 hash，不能换成缩小矩阵；只把逐个真实 matrix job 的无重叠 interval 交给 1800 秒 gate，固定 TUI/Web/ACP mode/reload/failure Go 覆盖及 Browser deny/disabled/中文 supplementary 明确不计时，禁止 sleep/filler 凑时长。
- 安全/失败关闭：每 run 必须有 record/metadata-only trajectory/redaction 三件套，私有目录 owner-only，凭证/host path 扫描；中断、部分失败、Git/suite/binary 漂移均写 failed manifest。supplementary 必须与真实 record 的 language/scenario/routing/real_execution 身份一致，报告要求固定 3 command + 4 Web 记录全部通过。
- 独立 review 曾发现 report 未强制 complete/formal、binary hash 未复核、formal 可替换小矩阵、supplementary 可身份漂移四类缺口；均已增加 fail-closed 校验和回归测试后才提交。
- Computer fixture 预检补强：统一 coordinator 现在固定以 `jcode_eval` tag 构建 JCode，plan/campaign 显式记录该 tag；报告验收精确要求同一值，防止普通 release binary 冒充 deterministic Computer campaign。提交 `8c980b1`，Python 全量 91/91。
- 首次真实 formal 尝试：固定 clean commit `1e9d172`、exact model、`temperature=omitted`、`workers=1`、`jcode_eval` build，计划 300 matrix jobs；三项 deterministic command、Web approval-deny 与 browser-disabled supplementary 均 PASS。中文 success static 完成真实 open/fill/click/read confirmation，但漏掉明确要求的 `browser_snapshot`，external routing 因 required count/order 严格 FAIL；coordinator 在 46.053s、matrix 0/300 时以 `supplementary_web_failed` fail closed，未把该次运行计入 30 分钟门槛，也不复用失败目录。三份 supplementary publish bundle 均 artifact-safe、redaction 0 finding。
- Browser Prompt 修复：中英文 success prompt 都改为显式 1～5 编号调用契约：open → snapshot → act fill → act click → read；要求五步逐项实际调用，中文两次强调 snapshot 即使页面已可见也不可省略。Deferred 的 `tool_search` 仍被明确说明为额外披露步骤，并要求与新工具分轮，不计入五个 Browser proof calls。routing、approval、ToolSearch activation 和 external verifier 均未改变。
- 安全/测试：proof/URL 仍只进入私有 prompt/session，published record/trajectory/report 仅含白名单元数据；Python eval 91/91、`py_compile`、`git diff --check` 与独立 review 全部通过，无 blocker。已完成提交 `b97734d fix(eval): pin Browser proof call order`。
- Browser Prompt 修复后的中文重复 canary（非正式，不计入 TS-07.7/TS-08）：固定 clean commit `ccbee30`，`jcode_eval` SHA-256 `7cda91f411580b3e50cf7a885d108c5751b86400dd6dfb707e6f6b01bd82c1d4`，exact `kimi-for-coding/kimi-for-coding`、`temperature=omitted`；static/deferred 各 3 次，共 6/6 task/contracts/routing/driver/artifact-safe 严格 PASS。六条均按 `open1 → snapshot1 → act2 → read1` 完成真实 loopback proof，调用顺序 5/5（static）或含独立 ToolSearch 的 6/6（Deferred）；三条 Deferred 均 search=1、首轮可见 12、bypass=0、same-batch=0、failed result=0。总 job wall 113.805s，单条 13.300～35.095s；6 份 redaction report 全部 safe、0 finding。脱敏结果目录：`/private/tmp/jcode-toolsearch-zh-browser-canary-ccbee30`。下一步必须从记录该证据后的新 clean commit 与全新目录重新完整 formal。
- 第二次真实 formal 尝试：从 clean commit `452cb407b95ee72ac73fac15b662f95ce05b4ff5` 启动，exact model、`temperature=omitted`、`workers=1`、`jcode_eval` SHA-256 `31cd4c8036414a2e26e2a661606d715133168079c54a460c7e0244f769a2936b`，7 项 supplementary 全部 PASS；在 261.595s 完成 37/300 个 matrix record 后主动 fail closed，中止码 `interrupted`，不计 TS-08 的 30 分钟门槛且目录不复用。已完成记录中 27 PASS、10 FAIL；10 个失败全部是严格 routing oracle：4 个 Direct read 的 endpoint/result 成功但 fixture path 参数未与字面相对路径 matcher 等值，另 6 个 Deferred（semantic en/zh 及 automation）均成功 search、匹配并调用目标，却因 Kimi 使用合法 `select:` 而 testcase 只允许 `keyword` 被判 `search_query_mode`。Browser/Computer、MCP10/30/50/100、no-tool 和 exact-select 已出现的两臂结果均 PASS；41 份 redaction report 全部 safe、0 finding。脱敏失败目录：`/private/tmp/jcode-toolsearch-formal-452cb40`。下一步先修正等价路径 matcher 与语义场景合法 search mode 合约，保持目标工具、参数、结果、次数、独立 batch、bypass/same-batch 门槛不变，再从新 clean commit/新目录完整重跑。
- Formal oracle 等价性修复：Direct read 改用 runner-owned `fixture_path` matcher，只接受声明的相对路径、`./` 写法或同一 canonical absolute path；fixture scope 在模型运行前捕获 root/file 的设备号、inode、类型、大小与 SHA-256，并在判定前复核。缺失 scope、未声明目标、`..`、外部/碰撞路径、symlink/alias、同路径替换或 root 替换全部 fail closed，published verdict 不包含宿主路径。
- Search 合约拆分：英文/中文 semantic discovery 与 automation 保持未辅导自然任务，Deferred 接受 Eino 合法的 `select` 或 `keyword`；另增两个 Deferred-only critical keyword 场景，以英文固定 query `+current +usage` 分别验证英文指令和中文指令下的 keyword 协议。exact-select、unknown-select 与两个 keyword case 均在私有 verifier 中逐字校验 query，published artifact 只保留计数和 violation type。中文场景不得表述为“中文词法 query”。
- Canonical matrix 从历史 16 cases/13 critical 扩为 18 cases/15 critical（ACP 17、Web 1）：14 个 static/deferred paired case 产生 280 jobs，两个 keyword 与两个 negative Deferred-only case 产生 40 jobs；10 repeats 的统一计划为 320 个唯一任务（ACP 300、Web 20）。九项硬门槛的名称、scope、聚合、比较符和阈值未改，MCP100 仍是唯一 full-schema representative，报告器继续要求全部 15 个 critical case 各自至少 9/10。
- 安全/验证：Agent Eval Python 全量 104/104、`py_compile`、`git diff --check`、matrix summary `18/15/ACP17/Web1`、统一 dry-run `320/320 unique jobs` 全部通过；独立只读复审无 blocker。已完成提交 `5c26838 test(eval): harden equivalent ToolSearch oracles`。TS-07.7、TS-08 与 TS-09 仍未勾选；下一步必须从记录本证据后的新 clean commit、全新 repo-external 目录运行 exact-Kimi canary，失败目录不得复用。
- Oracle 修复后的 exact-Kimi 放行 canary（非正式，不计入 TS-07.7/TS-08）：固定 clean commit `45b69915fa3ebe7c1eba9934200bf7f4b75c711d`、`jcode_eval` SHA-256 `9240844b4407f40c62cce8e40842d9c9c42e8e45955bad8ac33ee13d6abf59b5`、`kimi-for-coding/kimi-for-coding`、`temperature=omitted`、`workers=1`。Direct read、exact select、未辅导英文/中文 discovery、英文指令/中文指令 keyword 协议、复杂 automation 共 7 cases，repeat=3 形成 36 jobs，task/contracts/routing/artifact-safe 严格 36/36 PASS；起止 `2026-07-18T21:08:50.093582Z`～`21:11:34.438961Z`，monotonic 164.346s，真实 job wall 合计 161.1s。
- Canary 路由细节：21 个 Deferred runs 中 Direct read 3 条按 Direct 设计无需搜索，其余 18 条各 search 一次；select/keyword 为 12/6，9 个 exact-query 私有检查全部匹配。bypass=0、same-batch=0、failed result=0、invalid args=0、首轮可见最大 11；Direct path 6/6、automation args 6/6 精确匹配。36 份 redaction report 全部 safe、0 finding。脱敏结果目录：`/private/tmp/jcode-toolsearch-oracle-canary-45b6991`。Canary 放行 formal，但仍必须从记录本证据后的新 clean commit 和全新目录完整运行 320-job canonical campaign。
- 测试：campaign 定向 10/10、修复后定向 23/23、agent-eval Python 全量 90/90、fake formal 可被真实报告器解析且仅 1800 秒 gate 按预期失败；`py_compile`、diff check 通过。
- 已完成提交：`9f2889e test(eval): coordinate formal ToolSearch campaign`（仅基础设施；TS-08.1～08.6 均未勾选）。

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
- 报告器基础设施子阶段：新增独立、无外部 CSS/JS 的 HTML generator；只读取 allowlisted `record.json`、metadata-only `trajectory.json`、`redaction_report.json`、统一 plan/campaign manifest，不读取 raw session、prompt、config、debug log、完整参数或输出。
- 完整性门槛：精确校验 `kimi-for-coding/kimi-for-coding`、`temperature=omitted`、一致的实际 effort、clean Git、固定二进制 SHA-256、每个 plan job 的三件套及 `all_records` 一致性；任何缺失、highspeed 漂移、脏树、raw payload、凭证/宿主路径或 redaction finding 均 fail closed。
- 时长证明：同时要求 wall clock、monotonic 和成功 real-run interval 有效并集均 ≥1800 秒，`workers=1` 且区间不重叠；每段可计时长再由对应 `record.wall_s` 封顶，因此 sleep、setup 或 filler 不能凑满 30 分钟。
- 指标/展示：固定九项硬门槛、逐 case/variant/transport 结果、关键场景逐项 pass@10、Direct/Deferred schema/token/时延对比、metadata-only 调用轨迹、失败分类和相对 artifact links。
- 测试：Python suite 58 tests、`py_compile`、`git diff --check` 全部通过；synthetic 覆盖 PASS、单 gate 失败、缺失产物、时长/重叠伪造、credential/path canary、highspeed、dirty tree 和 unsafe redaction。
- 已完成提交：`ad55a3b test(eval): add ToolSearch acceptance report`（TS-09 报告器基础设施子阶段；TS-09.1～09.5 均须等待真实 campaign 后才能勾选）。
- 指标 scope 校准提交：`92d442e test(eval): calibrate ToolSearch acceptance scopes`；报告器从 hard-gate 声明读取唯一 `full_schema_disclosure` 代表 case，50% 阈值不变，缺失或不完整配对 fail closed。真实 campaign 前不勾选 TS-09.1～09.5。
- Formal 合约硬化提交：`9f2889e test(eval): coordinate formal ToolSearch campaign`；报告强制 canonical suite hash、formal/complete/no-failure manifest、固定 binary hash 与完整 supplementary 身份/产物，不接受部分或缩小 campaign。真实 campaign 前仍不勾选 TS-09.1～09.5。
- Deterministic build provenance 提交：`8c980b1 fix(eval): build deterministic Computer campaign`；HTML 展示 `jcode_eval` build tag，plan/campaign 缺失或漂移均拒绝生成 PASS。真实 campaign 前仍不勾选 TS-09.1～09.5。

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
| 2026-07-18 | TS-05.6 运行前安全：将现有 `~/.jcode` / `config.json` 从历史 `0755/0644` 收紧为 `0700/0600` | `stat` 复核通过，配置内容未改 | — |
| 2026-07-18 | 完成 TS-07.1～07.5：exact Kimi、无 temperature、paired variants、最小 HOME/env、安全 trajectory 与凭证扫描 | Python 20 tests、legacy/formal dry-run 通过 | `28eae39` |
| 2026-07-18 | TS-07.6 matrix 子阶段：16 cases/13 critical、ACP15/Web1、固定九项硬门槛与安全 validator | matrix+routing 19 tests、JSON/py_compile 通过 | `3a1ece8` |
| 2026-07-18 | TS-07.6 Web 子阶段：authenticated Web driver + loopback open/fill/click/read 真值 + approval/disabled/cleanup | Web driver 12 tests、py_compile 通过 | `7d713ef` |
| 2026-07-18 | TS-07.6 ACP dispatch 子阶段：matrix 实际调度、metadata-only expected-routing、MCP static/deferred fixture 交叉验证 | Python 51 tests、legacy 39/formal 28 dry-run、diff check 通过 | `f75041d` |
| 2026-07-18 | TS-09 报告器基础设施：固定九门槛、pass@10、三重 1800 秒证明、安全 HTML 与凭证扫描 | Python 58 tests、py_compile、diff check 通过 | `ad55a3b` |
| 2026-07-18 | TS-07 canary 修复：ACP `sess_<uuid>` 正确映射 recorder `<uuid>.json`，拒绝非法/穿越 ID | Python 67 tests；exact-Kimi 6/6 routing、2/2 MCP、5/6 task | `91bbc8b` |
| 2026-07-18 | 完成 TS-07.6：Web Browser authenticated dispatch、完整 proof-form 激活边界、supplementary deny/disabled | Python 69 tests、report validators、formal Web dry-run 通过 | `fe81907` |
| 2026-07-18 | TS-07/09 canary 指标校准：稳定 goal sentinel；50% schema 门槛固定到完整 100-tool catalog，拒绝 scope 漂移 | ToolSearch discovery 48 tests、py_compile、JSON、diff check 通过 | `92d442e` |
| 2026-07-18 | Web Browser canary 修复：macOS Chrome system HOME、HTTP(S) 边界、30s timeout、token 隔离及 recorder ID 映射 | Browser/tools Go、race、Python 13/13；隔离 Chrome smoke 0.96s | `8675a5b` |
| 2026-07-18 | TS-08/09 formal coordinator：canonical full matrix、固定 binary/suite hash、真实 interval、完整 supplementary 与 partial fail-closed | 独立 review；Python 90/90、fake formal/report 合约通过 | `9f2889e` |
| 2026-07-18 | Browser 修复后 exact-Kimi canary：Chrome 故障解除；static PASS，Deferred proof 成功但重复 open 严格 FAIL | 1/2；Deferred first-visible=12、bypass=0、same-batch=0 | — |
| 2026-07-18 | TS-07 capability-family 修复：Browser/Computer 显式组下一轮整体披露；gated peer、审批、same-batch、历史恢复及 MCP 不分组边界 | 两包 normal/race、全量 Go、lint `0 issues`、独立 review 通过 | `d60000d` |
| 2026-07-18 | capability-group 4-job exact-Kimi canary：Browser 2/2 PASS；Computer 两臂因 coordinator 未编入 eval backend 失败，严格总计 2/4 | Browser search=1、open=1、完整 proof、bypass/same-batch=0；Computer 根因已定位 | — |
| 2026-07-18 | 修复 formal/canary JCode 构建：固定 `jcode_eval` tag，plan/campaign/HTML 记录并 fail-closed 校验 | Python 91/91、py_compile、实际 eval build、diff check 通过 | `8c980b1` |
| 2026-07-18 | 第二轮 4-job exact-Kimi canary：Computer static 与 Browser Deferred PASS；Computer Deferred 漏 `select:` 后补救、Browser static 漏 snapshot，严格 2/4 | Computer 真值完成且无 bypass/same-batch；两项 routing 非合规均未误报 | — |
| 2026-07-18 | 修复 Kimi 逗号精确名称 query：严格 2～5 个 effective Deferred 名时有界补 `select:`，普通/MCP/权限边界不扩大 | agent normal/race、全量 Go、lint `0 issues`、两轮独立 review 通过 | `207b313` |
| 2026-07-18 | 第三轮 Browser+Computer exact-Kimi canary 4/4 严格 PASS；两个 Deferred 均一次搜索命中整组 | task/contracts/routing/artifact-safe 全通过；bypass=0、same-batch=0；Browser static 356.562s 延迟风险保留 | — |
| 2026-07-18 | 首轮 12-job 小矩阵严格 11/12；唯一失败为 Computer Deferred 逗号列出完整 6-tool family，超过五名兼容上限后再补正 | 其余 no-tool/exact/MCP10/MCP100/Browser/Computer static 全通过；失败未被任务真值掩盖 | — |
| 2026-07-18 | exact-list ceiling 调整到 8，覆盖最大内建 capability family，9+ 仍 fail closed | agent normal/race、全量 Go、lint `0 issues`、6/8/9-name 边界通过 | `7530b38` |
| 2026-07-18 | 第二轮相同 12-job 小矩阵严格 11/12；Computer Deferred 修复通过，唯一失败转为 MCP100 Deferred 相同成功调用 142 次后 360.1s 超时 | search/select、披露和参数均正确，bypass/same-batch/failed result=0；routing 与外部 fixture oracle 严格拒绝重复调用 | — |
| 2026-07-18 | MCP target 返回明确 structured completion/authoritative record 且保留 marker；System/Plan 加入成功结果去重规则 | focused/race、Python 91/91、全量 Go、lint `0 issues`、独立 review 通过 | `92c2f31` |
| 2026-07-18 | MCP100 Deferred 独立 10-repeat：工具 routing/MCP/contracts/artifact 10/10，旧 final marker task oracle 7/10 | 每条 search=1、target=1、6.153～10.224s、无 loop/bypass/same-batch；三条仅未复述内部 marker | — |
| 2026-07-18 | 四个 MCP final oracle 改为用户可见 SKU；raw marker/fixture/参数/次数/激活严格 gate 不变 | Python 91/91、相关 45 tests、独立 review 无 blocker | `ef66470` |
| 2026-07-18 | 新 oracle MCP100 Deferred 独立 10-repeat 严格 9/10，达到 critical 门槛 | 9 条 search=1/target=1；唯一失败先误选 metadata 后纠正，严格 forbidden/external verifier FAIL；无成功循环 | — |
| 2026-07-18 | 最终 12-job 跨类小矩阵严格 12/12 PASS，可进入 formal | no-tool/exact/MCP10/100/Browser/Computer 两臂全通过；Deferred bypass/same-batch/failed=0；MCP100 首轮 schema 降低 75.2% | — |
| 2026-07-18 | 首次真实 formal 在 matrix 前 fail closed：中文 Browser static 漏 snapshot | 3 command + deny + disabled PASS；success 完成页面 proof 但 required count/order FAIL；46.053s、0/300，不计 30 分钟 | — |
| 2026-07-18 | Browser success Prompt 改为中英文五步编号强顺序，snapshot 明确不可省略 | Python 91/91、py_compile、diff check、独立 review 无 blocker；routing/approval/activation 不变 | `b97734d` |
| 2026-07-18 | Prompt 修复后中文 Browser static/deferred 各 3 次真实 canary 严格 6/6 PASS，可重新启动 formal | 每条 open1/snapshot1/act2/read1；Deferred search=1、首轮 12、bypass/same-batch/failed=0；redaction 6/6 safe、0 finding | — |
| 2026-07-18 | 第二次 formal 完成 37/300 后主动 fail closed：暴露 path 字面等值与 semantic-only-keyword 两类 oracle 误判 | 27 PASS/10 routing FAIL；所有失败 endpoint 成功，4× argument mismatch、6× 合法 select 被拒；7 supplementary PASS、41 redaction safe | — |
| 2026-07-18 | 修复 formal 等价 oracle：可信 fixture identity、私有 exact-query 合约、未辅导 discovery 与独立 EN/ZH 指令 keyword 场景；canonical matrix 扩为 18/15 | Python 104/104、py_compile、diff check、summary 18/15/ACP17/Web1、dry-run 320 unique、独立 review 无 blocker | `5c26838` |
| 2026-07-18 | Oracle 修复后 exact-Kimi 36-job 放行 canary 严格全通过，可进入全新 formal | 36/36；Deferred bypass/same-batch/failed/invalid=0，query 9/9，Direct path 6/6，redaction 36/36 safe | — |
