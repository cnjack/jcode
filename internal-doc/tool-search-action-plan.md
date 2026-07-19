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
| TS-05 Prompt 与观测 | 进行中 | `dbe7ca4`、`69ba9ee`、`f905ae2`、`8675a5b`、`92c2f31`、`f101b18`、`503bef8`、`e3e1f7c` | 使用说明、skill→Deferred 路由桥接、schema/轨迹指标、凭证文件权限、Browser token 隔离、零替换发布门禁及 MCP structured result 投影 |
| TS-06 自动化测试 | 完成 | `77de5fc`、`92e0cb0`、`56fa6eb` | 单元、集成、fixture、routing oracle 与生命周期撤权矩阵 |
| TS-07 Kimi A/B | 进行中 | `28eae39`、`3a1ece8`、`7d713ef`、`f75041d`、`91bbc8b`、`fe81907`、`92d442e`、`8675a5b`、`d60000d`、`8c980b1`、`207b313`、`7530b38`、`92c2f31`、`ef66470`、`f101b18`、`503bef8`、`e3e1f7c`、`38bdd47`、`4ff93a8` | near-exact 修复后的 exact-Kimi Computer repeat canary 已 40/40 全绿；等待唯一一次完整 formal pass@10 |
| TS-08 30 分钟回归 | 进行中 | `9f2889e`、`8c980b1`、`b97734d`、`f101b18`、`503bef8`、`e3e1f7c` | Computer/Browser exact-Kimi repeat canary 已通过；下一步从记录本证据后的 clean commit 和全新目录完整重跑 320 jobs |
| TS-09 HTML 报告 | 进行中 | `ad55a3b`、`92d442e`、`9f2889e`、`8c980b1`、`f101b18` | 安全、fail-closed 的九门槛报告器已要求 canonical tool_names 与零替换产物；等待真实 campaign 产物 |

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
- 发布链安全子阶段：ACP harness 的 `tool_names` 是包含 path/command/pattern/URL 的 UI title，现不再进入 publish record；ACP/Web 均只发布私有 session extractor 生成的 canonical `calls_by_name`。每个 runner 接收 coordinator 的完整 forbidden scope，并额外覆盖 repo、runs/rundir、build/binary、HOME/config；成功产物必须是 `artifact_safe=false → 零 replacement/零 finding → true`，任何脱敏替换即 fail closed。Campaign 在解释不可信 record 前先扫描，并与 HTML report 双重校验 `tool_names == calls_by_name`、tool count 总量及 redaction 全零。
- 验证：`python3 -m unittest discover -s agent-eval/suite -p 'test_*.py'` 115/115；`python3 -m py_compile ...`；独立 harness module `go test ./...`；根 module 聚焦 Go 包与允许 loopback 的 `go test ./... -count=1` 全部通过；`git diff --check` 通过。首次从根 module 直接指定 `./agent-eval/harness` 因其为独立 module 被 Go 拒绝，已在正确 module 目录重跑通过；沙箱内全量 Go 仅有既有 `httptest` 端口权限失败，允许 loopback 后同命令通过。独立只读终审无 blocker。
- 已完成提交：`f101b18 fix(eval): publish canonical tool names safely`（TS-05.6 / TS-07～09 publication 子阶段；尚不勾选 TS-05.6，仍需完整 formal 全产物扫描）。
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
- 第三次真实 formal 尝试：从 clean commit `56b3bdf2085d9d57c876563fa2a121ee616990ce` 启动，exact model、`temperature=omitted`、`workers=1`、`jcode_eval` SHA-256 `ad30c169e12e92379ad8e9a54e130bd779e7d473df6856356bc08effd5b947d1`，7 项 supplementary 全部 PASS；计划 320 jobs，在 219.986s 完成 30 条后主动中止并以 `interrupted` fail closed，不计 TS-08 时长且目录不得复用。30 条中 27 PASS、3 routing FAIL；30 份 redaction report 全部 safe、0 finding。脱敏失败目录：`/private/tmp/jcode-toolsearch-formal-56b3bdf`。
- 第三次 formal 根因：两个 unrelated-keyword repeat 与一个 unknown-select repeat 都正确完成一次 `tool_search`，query mode、unknown exact query、调用顺序、final sentinel、contracts 与 artifact 均通过，endpoint/result status 也是 completed；但 verifier 把合法的空 `matches` 同时记为 `search_failed=1` 且 `empty_searches=0`，产生 `invalid_or_failed_search` + `empty_search_required`。因此 `empty_search=required` 合约在当前实现中不可满足，若继续会让 20 个 negative jobs 系统性失败并使 Deferred 95% 门槛不可能通过。下一步必须区分“成功返回空集合”和“调用/协议失败”，补回归测试后从新 clean commit、新目录完整重跑。
- Empty-search verifier 修复：Eino v0.9.9 的 `toolSearchResult.Matches` 在零命中时是 nil slice，真实 wire shape 为 `{"matches":null}`。两个 verifier 现在只在外层 result 成功、JSON 是 object 且显式存在 `matches` key 时，把 `null` 或 `list[string]` 规范化为合法结果；缺 key、顶层非 object、畸形 JSON、错类型/混合数组、error/denied/folded failure 继续 fail closed。成功空结果不会激活 Deferred 工具；正向目标场景仍因 `empty_search_forbidden` 与 `expected_search_match_missing` 失败。
- 验证：empty-search/verifier 定向 33/33、Agent Eval Python 全量 109/109、Go unknown-search/disclosure 两项集成测试、`py_compile`、`git diff --check` 全部通过；独立只读复审无 blocker。已完成提交 `4497bff fix(eval): accept successful empty tool searches`。TS-07.7/TS-08/TS-09 仍未勾选；下一步先从新 clean commit、新目录对两个 negative case 做 exact-Kimi 重复 canary，再启动全新 320-job formal。
- Empty-search 修复后的 exact-Kimi 定向 canary（非正式）：固定 clean commit `bf4beebde071632e5be62707f24429e19cac88ad`、`jcode_eval` SHA-256 `0fb99a9b1930ebb9ad154d9f7b9313384d1990ef4fd19f975b51cb4baf0e0558`、exact model、`temperature=omitted`、`workers=1`；unknown-select 与 unrelated-keyword 各 10 repeats，共 20/20 task/contracts/routing/artifact-safe PASS。20 次搜索全部 `search_success=1` + empty，`search_failed=0`；unknown exact query 10/10，bypass/same-batch/Deferred target call/failed result 均为 0。起止 `2026-07-18T21:26:12.803869Z`～`21:27:27.188911Z`，monotonic 74.384s、job wall 合计 72.7s；20 份 redaction 全部 safe、0 finding。脱敏目录：`/private/tmp/jcode-toolsearch-negative-canary-bf4beeb`。本结果只放行新 full formal，不计 TS-07.7 或 TS-08。
- 第四次真实 formal 尝试：从 clean commit `a4e5b318f13627fb8cb8bba60dd670b621c68323` 启动，exact model、`temperature=omitted`、`workers=1`、`jcode_eval` SHA-256 `ded8e8e6220b0448ebf285b825a99e621564f0d5235d2cd79e7d5ac556562add`，7/7 supplementary PASS；计划 320 jobs，起止 `2026-07-18T21:29:04.741216Z`～`22:01:20.696048Z`，1935.949s 时完成 250 条后以 `coordinator_artifact_scan_failed` fail closed。245 PASS/5 FAIL，其中 Deferred 3 条；虽然已超过 30 分钟，但 campaign 不完整且 artifact gate 失败，所以不能计入 TS-08，目录不得复用：`/private/tmp/jcode-toolsearch-formal-a4e5b31`。
- 第四次 formal 安全根因：250 个 matrix record 与 7 项 supplementary 的任务阶段完成后，strong coordinator scan 在 `ts_complex_automation_weekly` Deferred r10 的 `record.tool_names` 发现一处 sandbox host path；该 run 额外调用 `read`，ACP harness 把带参数的 UI title 聚合为 `tool_names` 并由 runner 原样发布，而同 run 的 trajectory/calls_by_name 仍是安全 canonical `read`。Coordinator 已将唯一一处替换为 `$REAL_HOME` 后终止；251 份既有 redaction report 均 safe/0 finding、未发现 credential，但它们的旧 per-run forbidden-path scope 没覆盖该路径。该结果属于真实 publication contract 缺陷，不作误报豁免。下一步须让 record/tool-name 聚合只发布 canonical 名称，并让 per-run 与 coordinator 使用同等 host-path scope，补泄露 canary 后从新 clean commit/new dir 重跑。
- 第四次 formal publication 修复：保留 ACP UI title，但 publish `record.tool_names` 的唯一来源改为 session-derived canonical `tool_counts.calls_by_name`；Web 采用同一字段合约。Coordinator 的完整 forbidden scope 现在显式传给 ACP/Web per-run，二者再覆盖 repo、runs/rundir 与 build/binary；任何 sanitizer replacement 均失败，只有零替换、零 finding 后才写 `artifact_safe=true`，post-scan 失败会撤回该标志。Campaign/report 同时拒绝 display title、合法格式但与 trajectory 漂移的工具名、总量不一致及声称 safe 却发生过 replacement 的产物。
- 修复验证：路径/display-title/canonical MCP 名称、scope parity、replacement failure、record/trajectory/report 一致性回归均通过；Python 全量 115/115、独立 harness module、根 module 聚焦 Go、允许 loopback 的全量 Go、`py_compile`、`git diff --check` 全部通过；独立终审无 blocker。代码提交 `f101b18`。该修复只恢复 formal 的发布前提，不计 TS-07.7、TS-08 或 TS-09；下一步必须从记录本证据后的 clean commit、全新 repo-external 目录先做 exact-Kimi 定向 canary，再完整重跑 320 jobs。
- Canonical publication exact-Kimi canary（非正式）：从 clean commit `2793be575b5f890cf761ed6b12d212ad97959d67`、全新目录启动；固定 `kimi-for-coding/kimi-for-coding`、`temperature=omitted`、`workers=1`、`jcode_eval`，JCode SHA-256 `ae469351424a7641379f745e6eef8ac7dae76a759966967e903e42b00e205860`。复杂 automation static/deferred 各 10 repeats，共 20/20 task/contracts/artifact-safe PASS；起止 `2026-07-18T22:28:04.687735Z`～`22:30:05.356244Z`，monotonic 120.669s。
- Canary 路由/发布细节：10 个 Deferred 均为 search=1、target=1、success=1，bypass=0、same-batch=0，首轮最多 11 tools。static r8 真实出现额外 `execute` 后再调用 `automation_create`，其 record/trajectory 均只发布 canonical `execute:1 + automation_create:1`，20 条 `tool_names == calls_by_name`，未发布 ACP command/title。20 份 redaction report 全部 safe、0 finding、0 redacted file、0 replacement；结果目录仅有 plan/campaign/index/all_records 与每 run 三件套，已验证不含 repo/run 绝对路径。脱敏目录：`/private/tmp/jcode-toolsearch-publication-canary-2793be5`。该结果只放行全新 formal，不计 TS-07.7、TS-08 或 TS-09。
- 第五次真实 formal 尝试：从 clean commit `7c65fd553b67845292a63520557df251dcf8697d`、全新目录启动，exact model、`temperature=omitted`、`workers=1`、`jcode_eval`，JCode SHA-256 `a4faa1a1ff17bb5dd656801052af4c8847ba72e8cb3e05fa9f7483abee096a79`；7/7 supplementary PASS。计划 320 jobs，起止 `2026-07-18T22:32:23.444911Z`～`22:52:33.914777Z`；在 1210.381s 完成 176 条后主动以 `interrupted` fail closed，174 PASS/2 FAIL、176/176 contracts/artifact-safe。两次失败均落在 critical `ts_mcp_catalog_10` Deferred（r8/r10），该 case 已无法达到 9/10，因此继续消耗剩余 jobs 不能恢复接受资格；不计 TS-07.7/TS-08，目录不得复用：`/private/tmp/jcode-toolsearch-formal-7c65fd5`。
- 第五次 formal 根因：两条失败都正确完成一次 `select:` search 并命中唯一 canonical target；bypass=0、same-batch=0、failed result=0、invalid args=0，且每次目标参数完全匹配、结果 status 全为 completed、最终用户可见 SKU oracle 通过。模型却在首个成功结果后以相同参数把同一 endpoint 总计调用 2 次（r8）/4 次（r10），分别触发 `required_call_count` 与外部 fixture `expected_calls_mismatch`。这不是 publication/path 回归：176 个 matrix record 与 4 个 Web supplementary 三件套共 180 份 redaction report 全部 safe、0 finding、0 redacted file、0 replacement；`tool_names` 均为 canonical。下一步需审计 MCP 成功结果传给模型的可判定完成语义与重复成功调用抑制边界，补产品级回归后再做 repeat canary/full formal。
- MCP 首次成功结果语义修复：链路审计证明 Eino MCP v0.0.8 把完整 `CallToolResult` envelope 作为普通字符串交给模型；fixture 虽有 `structuredContent`，标准 `content[0].text` 却只有 opaque marker，不满足 MCP“fallback 与 structured 内容功能等价”的兼容约定。对照 Codex 后，JCode canonical MCP wrapper 只在严格识别 `content` 数组且 endpoint 无错误时投影：非 null `structuredContent` 优先返回其 compact JSON，否则返回 compact `content` 数组；plain text、畸形/无关 JSON、非数组 content、Go error 与防御性的 `isError:true` 均保持原样。fixture 改用 `NewToolResultStructuredOnly`，文本 fallback 与 structured response JSON 语义等价，Python synthetic trajectory 同步为模型实际看到的投影形状。
- 重复抑制决策：Codex 与 Grok 均未使用通用 `name+args` 成功结果缓存；JCode trajectory 又在执行前记录模型发出的 tool call，middleware 即使阻止第二次副作用也无法让 exactly-once oracle PASS，并会掩盖模型问题。故不加入会破坏合法 mutating/polling/retry 语义的全局去重，继续要求 Kimi 在清晰的首次结果后不重试，并由 external fixture 严格计数。
- 修复验证：focused Go、MCP fixture、focused race 全部通过；Agent Eval Python 115/115、`py_compile`、`git diff --check`、`make lint-go`（0 issues）及允许 loopback 的根模块 `go test ./... -count=1` 全部通过。首次宽 Go 包测试仅因沙箱禁止既有 `httptest` bind loopback 失败，同命令在允许 loopback 的环境通过。独立只读审查无 blocker，并补入 `content:[]` structured-only 与异常 `isError:true` 防御用例。代码提交 `503bef8 fix(tools): project structured MCP results`。该修复只恢复 exact-Kimi repeat canary 的前提，不计 TS-07.7/TS-08/TS-09；下一步必须从记录本证据后的 clean commit、全新 repo-external 目录运行 MCP10/100 repeat canary。
- MCP projection exact-Kimi repeat canary（非正式）：从 clean commit `73829c06935fc12e93cb15b537f8f944f2abc04c`、全新外部目录运行 `ts_mcp_catalog_10` / `ts_mcp_catalog_100` × static/deferred × 10 repeats，共 40/40 task/contracts/routing/external-fixture/artifact-safe 严格 PASS。固定 `kimi-for-coding/kimi-for-coding`、`temperature=omitted`、`workers=1`、`jcode_eval`；JCode SHA-256 `dfd13e04104991685ba6e83e43ca641c7b889b91af7a145cc3f130110964bae3`。起止 `2026-07-18T23:15:23.275252Z`～`23:19:42.158334Z`，monotonic 258.879s，真实 job wall 合计 252.1s。
- Canary 调用/披露：20 个 Deferred 全部 search=1、fixture target=1、target success=1；20 个 static 全部 fixture target=1，没有任何相同成功调用重试。bypass=0、same-batch=0、failed result=0、invalid args=0、redundant search=0，routing/MCP violation 均为 0，`tool_names == calls_by_name` 40/40。Deferred 首轮最多 11 tools；MCP100 static/deferred 首轮 schema 为 19756/4906 estimated tokens（75.2% 降低），首轮可见 117/11；MCP10 为 7381/4906、27/11。40 份 redaction report 共扫描 120 个允许产物，全部 safe，0 finding、0 redacted file、0 replacement。脱敏目录：`/private/tmp/jcode-toolsearch-mcp-projection-canary-73829c0`。该 canary 只放行下一次 formal，不计 TS-07.7、TS-08 或 TS-09；下一步必须从记录该证据后的新 clean commit 和全新 repo-external 目录完整运行 canonical 320-job campaign。
- 第六次真实 formal 尝试：从 clean commit `4273b813774a801cfb202de4520c72d2bba717a5`、全新目录启动，exact model、`temperature=omitted`、`workers=1`、`jcode_eval`，JCode SHA-256 `bc399a617290d678a2e1e269e46e282ac5a000f9b8f532979ad51ff821950195`；7/7 supplementary PASS。计划 320 jobs，起止 `2026-07-18T23:22:56.055606Z`～`23:53:05.396874Z`，monotonic 1809.306s；在 212 条时主动以 `interrupted` fail closed，207 PASS/5 FAIL、211 contracts、209 routing、212 artifact-safe。Computer Deferred 已有 r6/r9 两次失败，即使剩余 repeats 全过也最多 8/10，故继续无法恢复 critical 9/10；虽然 wall 超过 30 分钟，但 campaign 不完整，不能计入 TS-08，目录不得复用：`/private/tmp/jcode-toolsearch-formal-4273b81`。
- 第六次 formal 失败分类：`negative_unknown_select` Deferred r5 与 `no_tool_irrelevant_zh` static r2 都仅 final sentinel 未命中，工具次数/routing/contracts/artifact 正常；Computer Deferred r6 完成真实 fixture 和正确参数/顺序，但把 `computer_act` 调用 2 次而严格 FAIL；MCP30 Deferred r4 在一次正确 search 后以相同正确参数把成功 target 调用 3 次，final SKU 正确但 routing/external fixture 严格 FAIL；Computer Deferred r9 则未搜索或调用 Computer，先 `load_skill` 1 次，再连续 `execute` 48 次，48 个结果成功、第 49 个 call 缺 result，300.090s TIMEOUT 且 contracts/routing FAIL。该 timeout 与 MCP 重试不是同一根因，需审计 Direct `execute`/skill 旁路的 no-progress loop 边界，不能用放宽 oracle 处理。
- 第六次 formal 安全：212 个 matrix record 与 4 个 Web supplementary 三件套共 216 份 redaction report 全部 safe，扫描 490 个允许产物，0 finding、0 redacted file、0 replacement；212/212 `tool_names == calls_by_name`。未读取 raw session、prompt、config、credentials 或 fixture logs。下一步先记录本失败，再对 Computer r9 的 metadata-only 调用序列、case prompt、prompt/loop-guard 现有实现做只读审计；任何修复都需代码提交、文档证据提交、Computer repeat canary，之后才允许全新 formal。
- Computer/Browser Deferred 路由修复：只读对照 Codex `56395bddaf26` 与 Grok CLI `fb97af83f06d` 后确认，两者均没有通用 `name+args` 成功调用缓存/吞并；Grok 的 400 轮上限与 Codex 的可选 token budget 也只是独立 fail-fast，不能修复路由。故本次不加入会误伤 polling、mutation、状态读取和审批/telemetry 的通用去重或 execute 熔断；`MaxIterations` 配置接线作为后续独立 hardening，不混入本轮 formal 变量。
- Skill→ToolSearch 桥接：Deferred 非空时的条件 instruction 现在明确“任务或适用 skill 需要但 schema 未附加”应先搜索，精确名称使用 `select:`，search miss 不重复同一搜索、不用 shell/UI 模拟；合法、已附加的替代工具仍可使用。Eino ToolSearch 之后新增低信任 JIT bridge：仅当 trailing tool batch 的 `load_skill` 结果精确提到当前有效但仍不可见的 Deferred 名时，在原 `Role=Tool` 结果中合并一次 routing note；不创建 System/User 消息、不枚举整个 Deferred catalog、不执行/授权/缓存/吞并任何调用，并覆盖并行多 skill batch。
- UI skills/oracle：Computer 与 Browser skill 都说明 schema 缺失但 `tool_search` 已附加时只搜索一次并等待，禁止以 `execute`/AppleScript/CLI UI automation 替代；Computer 与 Browser static/deferred oracle 均把 `execute` 列为 forbidden，Deferred search 收紧为恰好 1 次。首轮 Direct 工具层级未改变。
- 修复验证：`go test ./internal/agent ./internal/skills -count=1`、两包 `-race`、Agent Eval Python 115/115、JSON/`py_compile`、`git diff --check`、`make lint-go`（0 issues）以及允许 loopback 的 `go test ./... -count=1` 全部通过。独立终审最初发现 JIT System 提权 blocker，改为原 tool-result 同信任层并补全 negative/multi-tool batch 测试后复审确认关闭、无 blocker。代码提交 `e3e1f7c fix(agent): route deferred UI tools before shell`。该子阶段不计 TS-07.7/TS-08/TS-09；下一步必须从记录本证据后的 clean commit、全新 repo-external 目录运行 Computer/Browser 与 irrelevant no-tool repeat canary。
- UI routing exact-Kimi repeat canary（非正式）：从 clean commit `e4825242e38ddb7a2cc8178e46f8b102af1c7120`、全新外部目录运行 Computer、Browser、英文/中文 no-tool × static/deferred × 10，共 80/80 完整结束，campaign `canary_complete`。固定 `kimi-for-coding/kimi-for-coding`、`temperature=omitted`、`workers=1`、`jcode_eval`；JCode SHA-256 `53eba21ff7ba2c4a620f5d8e2d08ae3efd36f389265032d92fc8fe06362662d3`。起止 `2026-07-19T00:32:18.162930Z`～`00:43:22.497697Z`，monotonic 664.334s，真实 job wall 合计 657.823s。
- Canary UI 结果：Computer static/deferred 各 10/10 task/contracts/routing/artifact-safe，Browser static/deferred 各 10/10 task/contracts/routing/driver/artifact-safe。20 个 Deferred UI run 全部 `tool_search=1`，Computer open/act 各 20/20 exactly-once，Browser open/snapshot/read 各 20/20、act 40/40；80 条 `execute=0`。Computer/Browser Deferred 首轮最多 11/12 tools，schema 4906/5458 estimated tokens；对应 static 为 23/24、7456/7612。
- Canary negative/安全：英/中文 no-tool 共 40/40 routing/contracts/artifact-safe 且工具调用和 irrelevant search 都为 0。中文 final sentinel 有 static r1/r7、Deferred r3 共 3 条非路由 task FAIL，所以总 task 为 77/80；这三条均 `end_turn`、无 error、零工具，未误归因为 ToolSearch。全局 bypass=0、same-batch=0、failed result=0、invalid args=0、redundant search=0、routing violation=0，70/70 Deferred target calls 成功，20/20 参数检查匹配，80/80 canonical `tool_names == calls_by_name`。80 份 redaction report 扫描 160 个允许产物，全部 safe，0 finding、0 redacted file、0 replacement；公开目录只有 record/trajectory/redaction 三件套和 campaign manifests：`/private/tmp/jcode-toolsearch-ui-routing-canary-e482524`。该 canary 只放行全新 formal，不计 TS-07.7、TS-08 或 TS-09。
- 第七次真实 formal 尝试：从 clean commit `7befb60740c47febfd9bedd700ee861f0d2fcd48`、全新 repo-external 目录启动，固定 exact `kimi-for-coding/kimi-for-coding`、`temperature=omitted`、`workers=1`、`jcode_eval`；JCode/harness/MCP fixture SHA-256 分别为 `cd63519b3ddd4347a05c32f336ecbe39312fe8768fe2c1dd41c6e32c0b6ac8d3`、`626fbb9d5318d4701266d8b22b3b246b97256732e494d5b76efa9284d9c2eb69`、`77ab3f6a2142e4a0d23132e563361188a97f83ec82a0e3139e375322400c6062`。7/7 supplementary PASS；计划 320 jobs，起止 `2026-07-19T00:47:13.376515Z`～`01:04:18.354200Z`，monotonic 1025.022s，在 136 条时主动以 `interrupted` / exit 130 fail closed。133 task PASS、136 contracts、133 routing、136 artifact-safe；Computer static 已有两次失败，critical 最多 8/10，继续运行已不能恢复 9/10。campaign 不完整且不足 1800 秒，不计 TS-07.7/TS-08/TS-09，目录不得复用：`/private/tmp/jcode-toolsearch-formal-7befb60`。
- 第七次 formal 失败分类：`ts_computer_notes_click` static r8 完成 `load_skill1 → computer_open1 → computer_snapshot1 → computer_act2`，所有结果 completed、参数匹配，但因重复点击触发 `required_call_count`；同 case static r2 只有 `load_skill1 → computer_open1`，漏掉 inspect/click，触发 act 的 count/order。`ts_mcp_catalog_100` Deferred r10 则正确 search 一次并以正确参数调用 target 两次，结果均 completed，因单次成功后重复 target 严格 FAIL；该 MCP case 当时仍可恢复 9/10，不是中止原因。Computer Deferred 已出现的 r1/r2/r5/r6/r7/r8 全部 PASS 且 `execute=0`，证明本轮 skill→Deferred 路由修复有效；当前 blocker 收敛为 static Computer 对“点击一次”的模型方差，不能通过放宽 exactly-once oracle 处理。
- 第七次 formal 全局/安全：已完成记录中 bypass=0、same-batch=0、failed result=0、invalid args=0、redundant search=0、`execute=0`，136/136 `tool_names == calls_by_name`。136 个 matrix record 与 4 个 Web supplementary 三件套共 140 份 redaction report 全部 safe，扫描 318 个允许产物，0 finding、0 redacted file、0 replacement。未读取 raw session、config、credentials 或 fixture logs。下一步先审计 Computer case prompt 与 deterministic fixture 语义，再以不泄露 Deferred schema 名称的 exact-step/stop 条件消除重复和漏点，补测试、独立审查和 exact-Kimi repeat canary 后，才允许从新 clean commit/新目录再次完整 formal。
- Computer static 方差修复：三路只读审计确认 `computer_open` 已返回完整当前 UI，`computer_act` 成功结果也明确包含 `(1/1 actions completed)`；因此不改产品结果、不加会误杀合法 mutation/polling 的 runtime 去重。static/deferred 共用 prompt 现在明确顺序、open/click 各一次、不得成功前结束、成功后立即停止，且不出现 `tool_search`、`select:` 或任何 `computer_*` 函数名，不为 Deferred discovery 泄题。共享 deterministic base oracle 进一步要求 canonical journal 的唯一动作必须是 Notes 上以当前 `e1` 完成的 click，并禁止第二条 JSON action；single-action 与单元素 `steps` 等价通过，坐标 click、独立重复或 batched 双动作均失败。
- 修复验证：Computer matrix 定向 19/19、Agent Eval Python 全量 116/116、两份 JSON、`py_compile`、`git diff --check`、`go test -tags jcode_eval ./internal/command ./internal/computer -count=1` 全部通过；matrix summary 仍为 18 cases/15 critical/ACP17/Web1，exact-Kimi 配置 dry-run 为 320/320 unique jobs、Computer static/deferred 各 10、`workers=1`、`temperature=omitted`。独立终审确认 `e1`/journal 字段顺序/单行 regex 稳定、合法单步 `steps` 不会误杀且无 blocker。代码提交 `38bdd47 test(eval): stabilize Computer click contract`。该子阶段不计 TS-07.7/TS-08/TS-09；下一步必须从记录本证据后的 clean commit、全新外部目录运行 exact-Kimi Computer static/deferred repeat canary。
- Computer contract exact-Kimi repeat canary（非正式）：从 clean commit `ef83407067946060083e8baedbac2a05fcc0d303`、全新外部目录运行 `ts_computer_notes_click` static/deferred 各 20 次，40/40 完整结束、39 task/routing PASS、40/40 contracts/artifact-safe。固定 `kimi-for-coding/kimi-for-coding`、`temperature=omitted`、`workers=1`、`jcode_eval`；JCode/harness/MCP fixture SHA-256 分别为 `86f5934949db32ca79f45133a49ff425b0d78001aca33ec0a27970ebd193eaab`、`b0e6c9c67fe674d3958443ae06f99faa8c0cc071ea9af70cb05c8c394cbdb880`、`881ef811ffc9a31e103f1d678d9b776e8321b9f11cc7f3878bc80c172d0370be`。起止 `2026-07-19T01:24:20.647024Z`～`01:30:37.079031Z`，monotonic 376.425s、job wall 373.0s。
- Canary 动作/路由：static 20/20 严格 PASS，Deferred 19/20；两臂均 `computer_open=20`、`computer_act=20`、`execute=0`，160/160 个 action artifact oracles 全过，因此 40 条都只在 Notes 上以当前 `e1` 点击一次，没有漏点、重复、坐标旁路或 batched 多动作。Deferred target calls 40/40 completed，40/40 参数检查匹配；bypass=0、same-batch=0、failed result=0、invalid args=0，40/40 `tool_names == calls_by_name`。Deferred/static 首轮最多 11/23 tools、schema 4906/7456 estimated tokens。
- Canary 唯一失败：Deferred r19 先发出一次 successful-but-empty keyword search，下一模型请求再发出一次 `select:` search 并加载 Computer group，随后 `computer_open1 → computer_act1` 且结果、参数、顺序、唯一 UID 动作全部通过。严格 verifier 只因 `search_calls=2` 与第一条 empty search 触发 `search_call_count` / `empty_search_forbidden`；无 bypass/same-batch 或 target failure。全轮 21 次 search 均 endpoint success，其中 1 empty；40 份 redaction report 扫描 80 个允许产物，全部 safe，0 finding、0 redacted file、0 replacement。脱敏目录：`/private/tmp/jcode-toolsearch-computer-contract-canary-ef83407.3gAwhh`。该 canary 证明 click contract 修复有效，但不是全绿放行证据，不计 TS-07.7/TS-08/TS-09；下一步审计 semantic empty search 的索引/协议根因，不放宽 search exactly-once 合约。
- near-exact 根因：固定 Eino v0.9.9 的客户端 keyword search 不是 BM25，只以 `strings.Fields` 按空白分词；逗号 exact-name list 会被当成一个 substring term。r19 首次 46-byte/1-term keyword 与第二次 53-byte `select:` 恰差 7 bytes，第二次 metadata 为 2 个有效 Computer canonical name + 1 unknown；通过的 r2/r3/r13 又分别以 44/58/26-byte 单 term list 一次成功，证明索引本身没有漏 Computer，而是现有兼容器要求“列表全部 canonical”，遇到一个猜测名便把整串原样交给 Eino 并空命中。Eino v0.9.12 同路径源码与 v0.9.9 一致，升级不能解决。
- near-exact 修复：继续只处理严格 JSON envelope 中 2～8 个逗号分隔、无重复 identifier；至少 2 个必须是当前 effective Deferred canonical name，最多容忍 1 个 ASCII identifier unknown。改写只保留 known 且保序，unknown 丢弃、不作 alias 映射，再送入 `select:`；少于 2 known、2 unknown、空项、内部空白/危险标点、Unicode、越界、重复/未知 JSON field、异常 max_results、malformed/trailing JSON 全部原样 fail closed。cross-group/MCP 仅选择模型逐字点名的有效 anchor，后续 group expansion 仍受 plan gate，Hidden/Direct 不会披露，target approval 与 separate-batch 边界不变。系统提示同步说明 system-provided Deferred name block 是 exact candidate index 而非已附 schema，每个 discovery step 只用 known names 发一次 `select:`，禁止猜名或漏 prefix；没有修改 empty/redundant 语义或 exactly-once oracle。
- 修复验证：near-exact unit/wrapper/history/group/original-observation/repaired-approval/same-batch 边界、Agent 全包 normal 与 race、Agent/Command 集成、全量 `go test ./... -count=1`、Agent Eval Python 116/116、`py_compile`、`git diff --check` 与 `make lint-go`（0 issues）全部通过。两路独立审查及 adversarial 终审确认 general cross-group 边界不扩权、无需强制同 group、无 blocker；代码提交 `4ff93a8 fix(agent): recover near-exact tool searches`。该修复不计 TS-07.7/TS-08/TS-09；下一步从记录本证据后的 clean commit、全新外部目录复跑 exact-Kimi Computer static/deferred 20×2 canary。
- near-exact exact-Kimi repeat canary（非正式）已全绿：从 clean commit `54e18e49739cbba7c11d399000b68b5b8a500fd1`、全新外部目录运行 `ts_computer_notes_click` static/deferred 各 20 次，40/40 task、contracts、routing、artifact-safe 全部 PASS。固定 `kimi-for-coding/kimi-for-coding`、`temperature=omitted`、`workers=1`、`jcode_eval`；JCode/harness/MCP fixture SHA-256 分别为 `b0ada7c36b445ecd5a9185fb7d85067bda5c6ac9d442a7dcecd7f9c0b3c3e77b`、`84c8ae49dcfe7d850f5437d34bf313fb7dfe6eb5a876949e5e302414ac6835c6`、`94dbbb1022aa964d38b9d6d6977dd1b1b5b3a2395e122542d646fd6258bec1e5`；suite/matrix SHA-256 为 `f03290700bbe533cc4f94acbbc8d699ebb99747347d4c0c2bc6bc99f25e01de1` / `83a36c72ae1aa770f9ef1da10df7d43b132ecba7c771e3321f5882442dbd16b7`。起止 `2026-07-19T01:52:43.629170Z`～`01:59:29.318340Z`，monotonic 405.768s、job wall 399.1s。
- Canary 路由/动作/安全：static 与 Deferred 均 20/20；`computer_open=40`、`computer_act=40`、`execute=0`，120/120 tool result 成功。Deferred 20 次 search 全部一次成功且 0 empty，40/40 Deferred target call 与 40/40 参数检查匹配；bypass=0、observed bypass=0、same-batch=0、failed result=0、invalid args=0。Deferred/static 首轮工具数 11/23、schema 估算 4906/7456 tokens。40 份 redaction report 全部 safe，扫描 80 个允许产物，0 finding、0 redacted file、0 replacement；目录 `/private/tmp/jcode-toolsearch-near-exact-canary-54e18e4.MP991d`。这是 formal 前放行 canary，仍不计 TS-07.7/TS-08/TS-09；下一步立即从记录本证据后的 clean commit、全新 repo-external 目录启动唯一一次完整 formal。
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
- Canonical publication 提交：`f101b18 fix(eval): publish canonical tool names safely`；报告器要求 `record.tool_names` 是安全 canonical name→positive count map，且与 record/trajectory `calls_by_name` 精确相等；成功 redaction report 必须为零 replacement、零 redacted file、零 finding。真实完整 campaign 前仍不勾选 TS-09.1～09.5。

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
| 2026-07-18 | 第三次 formal 在 30/320 时主动 fail closed：negative empty-search oracle 把成功空集合误判为 search failure | 27 PASS/3 routing FAIL；7 supplementary PASS、30 redaction safe；219.986s 不计 30 分钟 | — |
| 2026-07-18 | 对齐 Eino `matches:null` 零命中 wire shape；null/[] 是成功空搜索，所有 malformed/outer failure 继续 fail closed且不激活工具 | 定向 33/33、Python 109/109、Go zero-match 集成、py_compile、diff check、独立 review 通过 | `4497bff` |
| 2026-07-18 | Empty-search 修复后两个 negative case exact-Kimi 各 10 次严格全通过，可重启 full formal | 20/20；success/empty=20、failed=0、query 10/10、bypass/same-batch/target=0、redaction safe | — |
| 2026-07-18 | 第四次 formal 在 250/320、1935.949s 时由 coordinator artifact gate fail closed：发布 `tool_names` 含一处 sandbox path | 245 PASS/5 task FAIL、7 supplementary PASS；路径已替换，未发现 credential；不计 TS-08 | — |
| 2026-07-18 | 修复 formal publication：仅发布 canonical tool_names，ACP/Web per-run 继承 coordinator 全 scope，零替换后才可标记 safe，campaign/report 双重校验 | Python 115/115、独立 harness、聚焦与全量 Go、py_compile、diff check、独立终审通过 | `f101b18` |
| 2026-07-18 | Canonical publication exact-Kimi 定向 canary 全通过，可进入全新 full formal | 20/20；Deferred search/target 10/10、bypass/same-batch=0；extra execute 仍仅发布 canonical 名；redaction 20/20 零替换 | — |
| 2026-07-18 | 第五次 formal 在 176/320 主动 fail closed：MCP10 Deferred 两条在成功结果后重复相同 target，critical 9/10 已数学上不可达 | 174 PASS/2 FAIL、7 supplementary PASS；bypass/same-batch/failed/invalid=0；180 redaction 全部零替换；1210.381s 不计 TS-08 | — |
| 2026-07-18 | 对齐 Codex 的 MCP result projection：structuredContent 优先，fixture fallback 与结构化结果等价；拒绝用全局成功调用缓存掩盖 Kimi 重试 | focused/race、Python 115/115、py_compile、全量 Go、lint 0 issues、独立审查无 blocker | `503bef8` |
| 2026-07-18 | MCP projection exact-Kimi repeat canary 严格全通过，放行全新 full formal | MCP10/100 × static/deferred ×10 = 40/40；Deferred search/target 20/20 exactly-once；bypass/same-batch/failed/invalid=0；redaction 40/40 safe | — |
| 2026-07-18 | 第六次 formal 在 212/320、1809.306s 时主动 fail closed：Computer Deferred r6/r9 两次失败使 critical 最多 8/10 | 207 PASS/5 FAIL、7 supplementary PASS；MCP30 一条成功 target 重试；Computer r9 为 load_skill1+execute48 后 timeout；216 redaction 全部零替换；不计 TS-08 | — |
| 2026-07-19 | 修复 skill→Deferred UI 路由：条件 ToolSearch 指引、同信任层 JIT bridge、Computer/Browser 禁止 execute 旁路且 search exactly-once；拒绝通用调用缓存/熔断 | agent/skills normal+race、Python 115/115、JSON/py_compile、全量 Go、lint 0 issues、两轮独立审查 | `e3e1f7c` |
| 2026-07-19 | UI routing exact-Kimi repeat canary 放行 full formal | Computer 20/20、Browser 20/20、no-tool routing 40/40；UI Deferred search exactly-once、execute/bypass/same-batch/failed/invalid/redundant=0；80 redaction safe | — |
| 2026-07-19 | 第七次 formal 在 136/320 主动 fail closed：Computer static 重复点击一次、漏点一次，使 critical 最多 8/10 | 133 task PASS、7/7 supplementary；Deferred Computer 已出现 6/6 PASS 且 execute=0；MCP100 一条成功 target 重试；140 redaction 全部零替换；1025.022s，不计 TS-07.7/TS-08/TS-09 | — |
| 2026-07-19 | 对齐 Computer exact-once 任务与 oracle：精确步骤/成功即停且不泄露 schema；journal 必须是唯一的 Notes e1 click，拒绝坐标和 batched 重复 | Python 116/116、tagged Computer/Command Go、JSON/py_compile、320/320 unique dry-run、独立终审无 blocker | `38bdd47` |
| 2026-07-19 | Computer contract exact-Kimi repeat canary 39/40；动作契约 40/40，唯一失败为 Deferred 首次 semantic empty 后第二次 select | static 20/20、Deferred 19/20；open/act exactly-once 40/40，execute/bypass/same-batch/failed/invalid=0；40 redaction safe | — |
| 2026-07-19 | 修复 near-exact search：2～8 项中至少 2 effective known、最多 1 identifier unknown；known-only select，unknown 不映射，协议提示对齐 Eino name block | Agent normal/race、全量 Go、Python 116/116、lint 0 issues、两路审查+adversarial 终审无 blocker | `4ff93a8` |
| 2026-07-19 | near-exact exact-Kimi repeat canary 40/40 全绿，放行最终 formal | static/deferred 各 20/20；20/20 search 一次命中、0 empty；open/act exactly-once 40/40，execute/bypass/same-batch/failed/invalid=0；40 redaction safe | — |
