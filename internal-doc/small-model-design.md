# 大小模型(small_model)设计与落地

2026-07-13。目标:token/成本优化 —— 主对话保质量,便宜的小模型接手不影响质量底线的副业调用。
本文记录调研结论、拍板决策与实现位置。(vision 模型角色另行立项,本期明确不做。)

## 竞品调研结论(opencode @2026-07-11 / codex @2026-07-02)

| 维度 | opencode | codex |
|---|---|---|
| 次级模型 | `small_model` 全局键 | 无 small;有 `review_model` + guardian 审查模型 |
| 小模型用途 | **仅标题生成、项目命名** | `/review`、auto-review 子会话 |
| 压缩/摘要 | 主模型(`agent.compaction.model` 可显式覆盖) | **刻意主模型**(sticky routing / prompt cache / `comp_hash`) |
| 未配置时 | family 启发式自动挑(gemini-flash→gpt-nano→claude-haiku,同 provider) | 无 |
| 更细的省钱层 | `small:true` 流标志:关 reasoning/thinking | per-model service_tier、切模型重校验 effort |
| subagent 模型 | 继承父模型,per-agent 配置可覆盖 | 受限子会话 + 独立模型键 + 回退主模型 |

核心启示:**两家都不用小模型做压缩/摘要**(质量伤不起),小模型只干"读得多、产出短、错了无所谓"的活。

## 拍板决策

1. **压缩、记忆蒸馏一律不用小模型**(用户拍板:质量太低且依旧废 token,没收益)。
   - 蒸馏原有的 `memory.model → SmallModel → Model` 链已收窄为 `memory.model → Model`。
   - `compaction.summary_model` 死字段(解析但从未消费)直接删除,不兑现。
   - `fallback_model` 死字段一并删除。旧配置文件含这些键仍可加载(JSON 忽略未知键,有兼容测试)。
2. **small_model 的两个消费场景**:
   - **subagent `"small"` 别名**:`subagent` 工具 `model` 参数接受 `"small"`,由 ModelFactory 统一展开;
     未配置时优雅降级到父模型(绝不报错)。工具描述动态生成——配置了才宣传别名,
     并引导主模型只把"机械、低风险"子任务(定点搜索/文件盘点/简单提取)派给 small,
     复杂推理和写代码留在父模型。**策略权在主模型**,不做按 agent_type 静默强制(v2 再议)。
   - **LLM 会话标题**:Recorder 首条用户消息后异步 refine(`SetTitleRefiner`/`SetTitle`),
     截断标题先落盘、LLM 标题后覆盖,失败保留截断版。仅 small_model 配置时启用,
     三个 surface(interactive/web/acp 新会话)统一挂 `attachTitleRefiner`。
3. **不做自动挑选启发式**(opencode 有):蒸馏已不走 small,但显式配置仍是唯一入口,
   避免 surprise;注册表有 Family/Cost/ReleaseDate,以后要做随时能加。
4. **未配置 small_model 时行为 100% 不变**(测试锁定)。

## 顺带修掉的存量 bug

- **subagent 的 `model` 覆盖参数在所有生产入口是静默 no-op**:TUI/web 的 `SubagentDeps`
  从未注入 `ModelFactory`(只有 WorkflowToolDeps 有)。现两处共享同一工厂实例。
- **subagent 用量归因**:此前一律记在会话主模型名下;现按实际服役模型记
  (`runSubagent` 接收 resolved usageModel)。
- 文档虚假宣传:site 文档把 `fallback_model`/`summary_model` 当可用功能描述,已更正。

## 实现位置

- 别名/解析:`internal/model/factory.go`(`SmallModelAlias`、`SmallModelRef`、`ResolveRef`、`GetModel` 展开)
- 标题:`internal/model/title.go`(生成+清洗)、`internal/session/session.go`(refiner 钩子)、
  `internal/command/title.go`(挂载+usage 归因)
- 工厂注入:`internal/command/interactive.go` `buildAllTools`、`internal/command/web.go` `buildAllTools`
- doctor 探针:`internal/command/commands.go` `doctorProbeModel`
- 蒸馏链收窄:`internal/memory/pipeline/phase1.go` `pipelineModel`

## 质量验证

- **线级路由 e2e**(`internal/tools/subagent_model_routing_test.go`):进程内 httptest 模拟
  OpenAI 兼容端点,捕获请求体 `model` 字段断言——`"small"` 命中 small id、未配置降级到父模型、
  默认继承不变、显式 `provider/model` 覆盖生效,且 subagent 均正常完成返回结果。
- factory 别名单测、标题生成/清洗单测、Recorder refiner 契约测试、旧配置兼容测试。

## 2026-07-14 多 agent 审查与修复

8 角度 finder + 6 组对抗验证,10 项确认全部修复:

- **别名覆盖面补齐**:team 闭包改走共享 ModelFactory(此前 `team_spawn model:"small"` 静默落回 leader 模型);automation 模型覆盖支持 `"small"`(展开自 config.small_model);畸形 small_model(无斜杠)从硬报错改为优雅降级到会话模型。
- **标题 refiner 生命周期**:改为触发时读盘上配置+懒建工厂(运行中增删 small_model 即时生效、resumed 会话零开销);UUID 绑定(`SetTitleFor`,/resume 竞态下丢弃陈旧标题而非写串);RecordUser 原子认领(并发首消息只触发一次)+ 触发后释放闭包;web 懒建/换会话 recorder 通过 `EngineConfig.RecorderInit` 补挂钩子。
- **doctor**:双探针统一走 factory 构建路径 + 60s 超时(静默端点不再挂死),消除三份构建逻辑复制。
- **用量一致性**:新增 `model.BareModelID`,runner/subagent/team/title 四个写入方统一 bare id + `CacheSeen`。
- **提示与措辞**:pipeline 在 small_model 已配而 memory.model 未配时打一行去向日志;CHANGELOG 注明死键会在下次保存时被清除。

未修(记录在案):`SaveConfig` 全量覆写不保留未知键(既有语义,死键无行为影响);`registry_generated.go` 的上游数据翻新(含可疑值 1048756)与本特性无关,应单独提交并复核;usage.Event 四处手写字面量可再抽 helper(本次只统一了字段值)。

## 已知边界 / 后续

- ~~ACP surface 没注册 subagent 工具~~——已由独立任务补齐(feat(acp): register
  subagent tool,随 `"small"` 别名与 ModelFactory 注入一并覆盖 ACP)。
- agent-eval harness 打真实端点、不捕获请求体,做不了确定性路由断言;本期用进程内
  httptest 覆盖(harness 若要支持需 mock provider base_url + 请求体记录,未做)。
- Web/TUI 设置 UI 尚未露出 small_model(config-file only),v2 做。
- 按 agent_type 的默认小模型策略、自动挑选启发式:观察别名实际使用效果后再议。
