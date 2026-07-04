# Agent Memory 业界实践深度调研（2026-07）

> 方法：deep-research workflow —— 5 路搜索 → 15 来源抓取 → 每条 claim 3 票对抗验证（2/3 驳回即杀）→ 综合。
> 规模：104 个子代理、491 次工具调用。
> 用途：支撑 [[agent-memory-design]] v1.1 修订。eino 部分为调研空白，单独补查后追加在文末。

## 总结

2025-2026 业界对 coding agent 长期记忆已形成清晰共识:存储形态收敛到「本地文件/分层工件 + 索引 + 渐进披露」(Codex ~/.codex/memories/、Claude Code 项目级 markdown 目录、Anthropic memory tool 的 /memories 前缀),写入时机分两派——离线后台蒸馏(Codex 启动时两阶段管线、Claude Code 未发布的 auto-dream/dream-skill 四阶段整合)与在线工具写入(Anthropic memory tool 自动注入 MEMORY PROTOCOL);遗忘普遍不是纯时间衰减,而是使用反馈排名淘汰(Codex usage_count + max_unused_days)、矛盾驱动删除(Mem0 DELETE)或保留历史的时间性失效(Zep 双时间线边失效)。jcode 草案(文件+git+两阶段蒸馏+收件箱)与 Codex 管线高度同构并正确规避了其 SQLite 依赖,同时用收件箱吸收了在线写入的低延迟优点,方向与业界收敛点一致;主要事实性修正是 Claude Code 实为「MEMORY.md 索引 + 每主题一文件」而非草案所写的「每事实一文件」,且其写入并非纯在线(存在离线整合层)。值得吸收的改进:Mem0 的 ADD/UPDATE/DELETE/NOOP 四操作作为 Phase 2 可检验写入协议、dream-skill 的矛盾消解/相对日期绝对化/死链清理整合细则、memory tool 官方安全清单(路径穿越校验必须在实现层、文件大小上限+分页读取)。eino 相关问题(官方 memory 组件、Go 侧社区实践)没有任何 claim 通过验证,属于本次调研空白,需单独补查 cloudwego/eino 与 eino-ext 仓库。

## 经验证的结论（confirmed claims）

### 1. [high] Codex memories 是两阶段蒸馏管线:Phase 1 并行(固定并发上限)从每个近期 rollout 抽取结构化记忆(raw_memory / rollout_summary / 可选 slug),Phase 2 在全局锁下串行地把 stage-1 输出合并进文件系统工件并运行专门的 consolidation agent;两阶段模型可独立配置(memories.extract_model / memories.consolidation_model)。这直接印证 jcode 草案 §5 的两阶段设计与 memory.model 配置项。

**证据**：README 原文: "Phase 1 finds recent eligible rollouts and extracts a structured memory from each one... Phase 2 consolidates the latest stage-1 outputs into the filesystem memory artifacts and then runs a dedicated consolidation agent"; 官方文档确认 extract_model 用于 per-thread extraction、consolidation_model 用于 global consolidation。验证者逐句对照 main 分支核实。

**来源**：<https://github.com/openai/codex/blob/main/codex-rs/memories/README.md>、<https://developers.openai.com/codex/memories>

**验证投票**：merged [0]+[4], 3-0 + 3-0

### 2. [high] Codex 存储为 ~/.codex/memories/ 下的分层文件工件(raw_memories.md、rollout_summaries/、phase2_workspace_diff.md,以及留给 agent 维护的 MEMORY.md / memory_summary.md / skills/;内容分层为 summaries、durable entries、recent inputs、supporting evidence),且 memories 根本身是 git 基线仓库,每次成功整合后 commit、用 git 风格 diff 驱动下次整合。重要限定:整体是 state DB + 文件的混合(Phase 1 输出先入 DB,Phase 2 才同步 top-N 到文件工作区),并非纯文件。jcode 草案用 state.json + flock 替代 DB 是正确的无 SQLite 等价物,git-as-change-detector 设计与草案 §2.2 完全对应。

**证据**：README: "keeps the memories root itself as a git-baseline directory, initialized under ~/.codex/memories/.git... writes phase2_workspace_diff.md... with the git-style diff from the previous successful Phase 2 baseline"; 文档: "The main memory files live under ~/.codex/memories/ and include summaries, durable entries, recent inputs, and supporting evidence from prior threads." 验证者注明 DB+文件混合的限定。

**来源**：<https://github.com/openai/codex/blob/main/codex-rs/memories/README.md>、<https://developers.openai.com/codex/memories>

**验证投票**：merged [1]+[5], 3-0 + 3-0

### 3. [high] Codex 写入时机是会话启动时的异步后台任务而非会话结束时:root session 启动触发,门条件为非 ephemeral、feature 开启、非 sub-agent、state DB 可用;跳过仍活跃或过短的会话,等线程空闲足够久(默认约 6h,可配 1-48h)才蒸馏;Phase 1 有启动负载上限,Phase 2 在工件同步后无变更时零成本退出;生成的记忆字段会做 secrets 脱敏。jcode 草案 §5.1 的门条件+冷却期与此对齐,BYOM 场景额外加每日 token 预算闸门是必要增强(GitHub issues 证实 Codex 后台记忆生成确实消耗用户配额)。

**证据**：文档原文: "Codex skips active or short-lived sessions, redacts secrets from generated memory fields, and updates memories in the background instead of immediately at the end of every thread... waits until a thread has been idle long enough"; README 列出全部四个门条件。openai/codex issues #19732/#19105 证实后台记忆生成消耗 rate limit。

**来源**：<https://github.com/openai/codex/blob/main/codex-rs/memories/README.md>、<https://developers.openai.com/codex/memories>

**验证投票**：merged [2]+[6], 3-0 + 3-0

### 4. [high] Codex 遗忘是使用反馈驱动的排名淘汰而非纯时间衰减:Phase 2 选材按 usage_count 优先、再按 last_usage/generated_at 排序,直接忽略 last_usage 超出 max_unused_days 的记忆;落选的 rollout 摘要和超龄扩展资源被物理清理并体现在 workspace diff 中(由整合代理据此手术式清理 MEMORY.md);读路径 crate(codex-memories-read)负责记忆注入、citation 解析和 read-usage 遥测,为反馈回路供数。jcode 草案 §3.2(命令解析记账)+ §5.3(usage 排名)是对该闭环的完整对标,且避开了 BYOM 模型 citation 合规性风险。

**证据**：README: "ranks eligible memories by usage_count first, then by the most recent last_usage / generated_at... ignores memories whose last_usage falls outside the configured max_unused_days window"; "prunes stale rollout summaries... so cleanup appears in the workspace diff"; read crate "owns the read path: memory developer-instruction injection, memory citation parsing, and read-usage telemetry classification"。

**来源**：<https://github.com/openai/codex/blob/main/codex-rs/memories/README.md>

**验证投票**：[3], 3-0

### 5. [high] Claude Code auto memory 存储为项目级纯 markdown 目录 ~/.claude/projects/<project>/memory/,按 git 仓库为键(同 repo 的所有 worktree 与子目录共享一个记忆目录,非 git 仓库回退到项目根);布局是 MEMORY.md 索引 + 可选主题文件(如 debugging.md、api-conventions.md)——即「每主题一文件」而非「每事实一文件」。这是对 jcode 草案的直接修正:草案第 4 行与 §1.2 表格写的「每事实一个 md 文件」不符合官方文档;草案的 notes/ 收件箱(<ts>-<slug>.md 单事实小文件)作为暂存区没问题,但精编层应按任务族/主题组织(草案 §5.3 的「任务族分块」恰好已是主题式,只需改掉对标描述)。

**证据**：官方文档: "Each project gets its own memory directory at ~/.claude/projects/<project>/memory/. The <project> path is derived from the git repository, so all worktrees and subdirectories within the same repo share one auto memory directory"; "MEMORY.md acts as an index... using MEMORY.md to keep track of what's stored where"; "Claude keeps MEMORY.md concise by moving detailed notes into separate topic files"。验证者还在本机磁盘核实了 per-repo 共享行为。

**来源**：<https://code.claude.com/docs/en/memory>

**验证投票**：merged [7]+[8], 3-0 + 3-0

### 6. [high] Claude Code 的检索注入是硬性有界的:每次会话启动只加载 MEMORY.md 的前 200 行或 25KB(先到为准),超出部分不加载;主题文件从不在启动时加载,由模型在会话中用标准文件工具按需读取。这验证了 jcode 草案的「summary 常驻(默认 ≤1200 tokens 截断)+ MEMORY.md grep + 按需深读」三级渐进披露,且说明不需要专用检索工具(与草案 §3.3 一致)。

**证据**：官方文档: "The first 200 lines of MEMORY.md, or the first 25KB, whichever comes first, are loaded at the start of every conversation... Topic files like debugging.md or patterns.md are not loaded at startup. Claude reads them on demand using its standard file tools"。

**来源**：<https://code.claude.com/docs/en/memory>

**验证投票**：[9], 3-0

### 7. [medium] Claude Code 的写入并非纯在线笔记:「模型只在会话中在线选择性写入、无事后蒸馏管线」的说法被 1-2 票驳回;相反,存在离线整合层——社区 dream-skill(104 stars)复刻了未发布的 Anthropic auto-dream 特性(服务端 flag tengu_onyx_plover),实现四阶段管线:Orient(扫描记忆目录)→ Gather Signal(用定向 grep 挖近期会话 JSONL 转录中的用户纠正/偏好变化/决策/复现模式)→ Consolidate(合并进现有记忆、消解矛盾、相对日期转绝对、去重、清理指向不存在文件的引用)→ Prune & Index(重建 MEMORY.md 为 <200 行的精简索引、把冗长条目降级为主题文件),经 Stop hook 24 小时去抖自动触发。对 jcode 的启示:两大厂最终都落在「在线写 + 离线整合」双层,jcode 收件箱+Phase 2 的混合架构正处收敛点;dream-skill 的整合细则(矛盾消解、日期绝对化、死链清理、索引行数上限)应写进 Phase 2 整合代理 prompt(草案 §5.3 已有部分,可补日期绝对化与死链清理)。

**证据**：dream-skill README: "Scans recent session transcripts (JSONL files) for user corrections, preference changes, important decisions, and recurring patterns"; "Rebuilds MEMORY.md as a lean index under 200 lines... Demotes verbose entries to topic files"。多个独立 2026 来源(Piebald-AI 提取的 Claude Code 内部 dream prompt、claudefa.st、VentureBeat 泄漏报道)佐证 auto-dream 真实存在但未正式发布。置 medium 因 auto-dream 归属为社区复刻+泄漏证据,非官方文档;且验证者指出去重/矛盾消解属 Consolidate 阶段而非 Prune & Index(阶段归属细节需按此表述)。

**来源**：<https://github.com/grandamenium/dream-skill>、<https://code.claude.com/docs/en/memory>

**验证投票**：merged [14]+[15]+[16], 3-0 + 3-0 + 3-0; 反向 claim 被 1-2 驳回

### 8. [high] Anthropic memory tool(API 层)是纯客户端文件操作模型:Claude 只发出对 /memories 前缀的六个命令(view/create/str_replace/insert/delete/rename),实际存储由宿主应用映射到磁盘/数据库/云端自行实现;启用后 API 自动注入 MEMORY PROTOCOL 系统提示(先 view 记忆目录再做事、边工作边写进度、假设上下文随时重置),即在线任务内写入而非会话后蒸馏。对 jcode 的借鉴:memory_note 工具描述可直接吸收 MEMORY PROTOCOL 的措辞纪律;「工具由实现层保证写入范围」的客户端模型与草案 §4 的免审批+路径锁死设计同构。

**证据**：官方文档: "The memory tool operates client-side: Claude requests file operations, and your application executes them... The /memories path is a prefix that your handler maps onto real storage"; "When the memory tool is present in your request's tools, the API automatically adds this instruction to the system prompt... ALWAYS VIEW YOUR MEMORY DIRECTORY BEFORE DOING ANYTHING ELSE... ASSUME INTERRUPTION"。

**来源**：<https://platform.claude.com/docs/en/agents-and-tools/tool-use/memory-tool>

**验证投票**：merged [10]+[11], 3-0 + 3-0

### 9. [high] memory tool 设计中遗忘与安全全部划归应用侧责任,官方给出可直接抄的清单:(1) 定期删除长期未访问的记忆文件(基于访问时间过期);(2) 限制单文件大小、cap view 返回字符数并支持 view_range 分页;(3) 模型「通常会拒绝」写敏感信息但应用必须在写盘前再做脱敏校验;(4) 必须对每个命令做路径校验防 /memories/../../ 目录穿越(canonical 化、拒绝 ../ 及 URL 编码变体)——相关攻击类真实存在(Anthropic Filesystem MCP Server 的 CVE-2025-53110/53109)。jcode 草案 §6 已覆盖脱敏与路径前缀校验,应补:memory 文件大小上限、read 工具对超大记忆文件的分页、基于 §3.2 usage 记账的访问时间过期(与 max_unused_days 淘汰天然合一)。

**证据**：官方文档: "Memory expiration: Periodically delete memory files that haven't been accessed in a long time"; "Track memory file sizes and cap how large a file can grow... let Claude page through the rest with view_range"; "Your implementation must validate every path in every command to prevent directory traversal attacks"。验证者引 Cymulate 披露的 CVE 佐证攻击类真实性。

**来源**：<https://platform.claude.com/docs/en/agents-and-tools/tool-use/memory-tool>

**验证投票**：merged [12]+[13], 3-0 + 3-0

### 10. [high] Mem0 采用两阶段管线(与 jcode 两阶段蒸馏结构同形,但为在线逐消息对,非离线批量):extraction 阶段借助运行中会话摘要+近期消息从每个新消息对抽取候选事实,update 阶段把每个候选与既有记忆比对,由 LLM 通过 function-calling 在恰好四个操作中选择——ADD(新事实)/UPDATE(增补既有)/DELETE(删除被矛盾的旧记忆)/NOOP(跳过)。即遗忘在写入时由矛盾驱动而非时间衰减。对 jcode 的改进点:Phase 2 整合代理消化 notes/ 收件箱时,可要求其对每条候选显式输出 ADD/UPDATE/DELETE/NOOP 决策——这把自由文本整合变成可断言、可测试、可统计 no-op 率的协议(直接服务草案 M2 验收标准)。

**证据**：论文原文: "The extraction phase initiates upon ingestion of a new message pair... extracts a set of salient memories"; "determines which of four distinct operations to execute: ADD... UPDATE... DELETE for removal of memories contradicted by new information; and NOOP"。验证者确认操作经 tool call 接口由 LLM 直接选择;注意 Mem0 托管产品另有检索层 recency 重排与可选 expiration_date,属论文范围外。

**来源**：<https://arxiv.org/abs/2504.19413>

**验证投票**：merged [17]+[18], 3-0 + 3-0

### 11. [high] Zep 的核心是时间感知知识图谱引擎 Graphiti,三层结构(原始 episode 节点 → LLM 抽取的语义实体节点 → 强连通实体聚类的 community 节点);写入发生在摄取时:实体名嵌入 1024 维向量、余弦相似度召回候选、LLM 实体消解 prompt 合并重复后才入图(边去重同理);遗忘是双时间线边失效而非删除——追踪四个时间戳(t'created/t'expired 记录系统内摄取,t_valid/t_invalid 记录现实有效期),新事实矛盾旧事实时把旧边 t_invalid 设为新边 t_valid,历史全保留。对 jcode:图数据库形态不适用(违背零依赖),但「失效而非删除、历史可审计」的原则 jcode 靠 git 历史免费获得(草案 §2.2 的 git log 审计/回滚正是文件系统版的等价物);「摄取时去重消解」提示 Phase 1 输出落盘前可先做与既有 summary 的轻量查重。

**证据**：论文原文: "a temporally-aware knowledge graph engine... three hierarchical tiers"; "embeds each entity name into a 1024-dimensional vector space... processed through an LLM using our entity resolution prompt"; "invalidates the affected edges by setting their tinvalid to the tvalid of the invalidating edge"。验证者核实全文逐句匹配;唯一争议(与 MemGPT 的 benchmark 之争)不涉及架构描述。

**来源**：<https://arxiv.org/abs/2501.13956>

**验证投票**：merged [19]+[20]+[21], 3-0 ×3

### 12. [high] LangMem 提供两个对 jcode 接口设计直接有用的先例:(1) core API 与存储/框架解耦——无状态的 extract/consolidate 函数可配任意存储后端(bring-your-own persistence),证明「核心蒸馏逻辑 + 可插拔 store 接口」在纯 Go 文件后端上完全可行(jcode 可定义 MemoryStore 接口、v1 只给文件实现);(2) 官方划分三类检索注入条件——数据无关记忆永远在 prompt 里、数据相关记忆按语义相似度召回、其余按应用上下文+相似度+时间组合召回——即不是所有记忆都该走相似检索,核心层应无条件注入,这正是 jcode memory_summary.md 常驻 + MEMORY.md grep 分层的理论依据(且表明 jcode 无向量库、用 grep 做第二层召回是合理取舍而非缺陷)。

**证据**：博客原文: "You can use its core API with any storage system and within any Agent framework"; "(1) data-independent - they are always present in the prompt. (2) Data-dependent and may be recalled based on semantic similarity. (3) Others may be recalled based on a combination of application context, similarity, time, etc." 官方 conceptual guide 佐证核心函数不依赖特定数据库。

**来源**：<https://www.langchain.com/blog/langmem-sdk-launch>

**验证投票**：merged [22]+[23], 3-0 + 3-0

### 13. [medium] jcode 草案改进点清单(按优先级,均由上述 confirmed claims 导出):1) 【文档修正】把草案中对 Claude Code 的「每事实一文件」表述改为「MEMORY.md 索引 + 每主题一文件」,并将精编层组织原则明确为按任务族/主题(收件箱保持单事实小文件);2) 【协议化】Phase 2 整合代理对每条收件箱/summary 输入显式输出 ADD/UPDATE/DELETE/NOOP 决策(Mem0),使 M2/M3 验收可量化;3) 【prompt 增强】整合 prompt 补入 dream-skill 的三条细则:相对日期转绝对日期、矛盾消解、清理指向不存在文件的引用;MEMORY.md 索引加行数上限(Claude Code 200 行/25KB 注入界佐证草案 1200-token 截断的合理性);4) 【安全补齐】按 memory tool 官方清单补:memory 单文件大小上限、超大文件分页读取、路径校验覆盖 URL 编码穿越变体;5) 【已验证无需改】文件+git 形态、启动时异步+闲置门条件、usage 排名淘汰、无 diff 零 token 退出、summary 常驻+grep 分层、state.json 替代 SQLite——全部与至少一个 primary source 的机制一一对应。

**证据**：综合性发现:各条改进点分别锚定于 findings 1-12 的 confirmed 机制,与 /Users/jack/workpath/jjj/jcode/internal-doc/agent-memory-design.md 逐节比对得出(§1.2 表格与第 4 行需要修正、§5.3 可协议化、§6 可补齐)。置 medium 因清单本身是解释性综合,非单一来源直接陈述。

**来源**：<https://github.com/openai/codex/blob/main/codex-rs/memories/README.md>、<https://code.claude.com/docs/en/memory>、<https://platform.claude.com/docs/en/agents-and-tools/tool-use/memory-tool>、<https://arxiv.org/abs/2504.19413>、<https://github.com/grandamenium/dream-skill>

**验证投票**：synthesis over all confirmed claims


## 附录 A:eino 框架 memory 实践补查（单独代理,本地源码 + 官方文档双重核实）

**核心结论:eino 官方没有 memory 组件（业务层概念）,jcode 自建文件存储是正统做法。**

- eino v0.9.9（jcode 实际依赖）`components/` 无 memory;eino-ext code search 零结果;官方文档《Memory 与 Session》明确"不是框架核心组件";issue #203 被以"callback 自建"关闭。官方无长期记忆抽象,文档不分短期/长期。
- 官方示例三个:`react/memory_example/memory` 的 `MemoryStore{Write/Read/Query(sessionID, text, limit)}` 接口（Redis/内存实现）;`eino_assistant/pkg/mem/simple.go` JSONL 每会话一文件（与 jcode 最接近）;`chatwitheino/mem/store.go` 泛型 JSONL + pendingInterruptID 与历史同存。
- 社区:hildam/eino-history（MySQL/Redis,低活跃,无文件后端）;无"eino 长期记忆"成熟专文。
- adk 可挂钩子（本地核实 v0.9.9）:SessionValues(run 内 KV,非持久)、ChatModelAgentMiddleware 的 BeforeModelRewriteState（jcode compaction 已用）、GenModelInput、CheckPointStore(Get/Set 字节)、summarization 中间件(TranscriptFilePath 原文指针)、reduction 中间件(超长输出 offload 文件+ClearAtLeastTokens 保 cache)、agentsmd 中间件(**瞬时前插不入 state,免疫 compaction——memory 注入应同构**)。
- 对 jcode 的采纳:①三方法接口形态;②瞬时注入不入 history;③不等官方 SDK。顺带发现(另开任务):transcript 指针、reduction offload、CheckPointStore 文件实现。

来源:cloudwego.io/zh/docs/eino/quick_start/chapter_03_memory_and_session/ | github.com/cloudwego/eino/issues/203 | pkg.go.dev/github.com/cloudwego/eino-examples/flow/agent/react/memory_example/memory | ~/go/pkg/mod/github.com/cloudwego/eino@v0.9.9/adk/{runctx,handler,chatmodel}.go、middlewares/{summarization,reduction,agentsmd}
