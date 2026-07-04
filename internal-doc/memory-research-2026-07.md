# Deep-Dive Survey of Industry Practice for Agent Memory (2026-07)

> Method: deep-research workflow — 5 search paths → 15 sources fetched → per-claim adversarial verification with 3 votes (rejected if 2/3 vote to kill) → synthesis.
> Scale: 104 subagents, 491 tool calls.
> Purpose: to support the [[agent-memory-design]] v1.1 revision. The eino portion was a gap in this survey; it was investigated separately afterward and appended at the end.

## Summary

Over 2025-2026 the industry has converged on a clear consensus for long-term memory in coding agents. Storage form has settled on "local files / layered artifacts + index + progressive disclosure" (Codex's ~/.codex/memories/, Claude Code's project-scoped markdown directory, the /memories prefix in Anthropic's memory tool). On write timing there are two camps — offline background distillation (Codex's two-phase pipeline at startup, Claude Code's unreleased four-phase consolidation via auto-dream/dream-skill) versus online tool writes (Anthropic's memory tool auto-injecting a MEMORY PROTOCOL). Forgetting is generally not pure time decay but usage-feedback ranked eviction (Codex's usage_count + max_unused_days), contradiction-driven deletion (Mem0 DELETE), or history-preserving temporal invalidation (Zep's bi-temporal edge invalidation). The jcode draft (files + git + two-phase distillation + inbox) is highly isomorphic to the Codex pipeline and correctly avoids its SQLite dependency, while using the inbox to absorb the low-latency advantage of online writes — a direction consistent with the industry's convergence point. The main factual correction is that Claude Code is actually "a MEMORY.md index + one file per topic" rather than the draft's "one file per fact", and its writes are not purely online (an offline consolidation layer exists). Improvements worth adopting: Mem0's four operations ADD/UPDATE/DELETE/NOOP as a checkable Phase 2 write protocol; dream-skill's consolidation rules for contradiction resolution / making relative dates absolute / dead-link cleanup; and the official memory tool security checklist (path-traversal validation must live in the implementation layer, plus a file-size cap + paginated reads). The eino-related questions (an official memory component, Go-side community practice) had no claim pass verification and constitute a gap in this survey; the cloudwego/eino and eino-ext repos need a separate follow-up investigation.

## Verified conclusions (confirmed claims)

### 1. [high] Codex memories is a two-phase distillation pipeline: Phase 1 runs in parallel (with a fixed concurrency cap) to extract a structured memory (raw_memory / rollout_summary / an optional slug) from each recent rollout; Phase 2 runs serially under a global lock, merging the stage-1 output into the filesystem artifacts and then running a dedicated consolidation agent. The two phases' models are independently configurable (memories.extract_model / memories.consolidation_model). This directly corroborates the two-phase design in jcode draft §5 and the memory.model config option.

**Evidence**: README source text: "Phase 1 finds recent eligible rollouts and extracts a structured memory from each one... Phase 2 consolidates the latest stage-1 outputs into the filesystem memory artifacts and then runs a dedicated consolidation agent"; the official docs confirm extract_model is used for per-thread extraction and consolidation_model for global consolidation. The verifier cross-checked the main branch line by line.

**Sources**: <https://github.com/openai/codex/blob/main/codex-rs/memories/README.md>, <https://developers.openai.com/codex/memories>

**Verification votes**: merged [0]+[4], 3-0 + 3-0

### 2. [high] Codex storage is layered file artifacts under ~/.codex/memories/ (raw_memories.md, rollout_summaries/, phase2_workspace_diff.md, plus MEMORY.md / memory_summary.md / skills/ left for the agent to maintain; content is layered into summaries, durable entries, recent inputs, and supporting evidence), and the memories root itself is a git-baseline repository, committed after each successful consolidation, with the git-style diff driving the next consolidation. An important qualifier: overall it is a hybrid of a state DB + files (Phase 1 output first lands in the DB, and only Phase 2 syncs the top-N to the file workspace), not pure files. The jcode draft's use of state.json + flock in place of a DB is a correct SQLite-free equivalent, and the git-as-change-detector design corresponds exactly to draft §2.2.

**Evidence**: README: "keeps the memories root itself as a git-baseline directory, initialized under ~/.codex/memories/.git... writes phase2_workspace_diff.md... with the git-style diff from the previous successful Phase 2 baseline"; docs: "The main memory files live under ~/.codex/memories/ and include summaries, durable entries, recent inputs, and supporting evidence from prior threads." The verifier noted the DB+file hybrid qualifier.

**Sources**: <https://github.com/openai/codex/blob/main/codex-rs/memories/README.md>, <https://developers.openai.com/codex/memories>

**Verification votes**: merged [1]+[5], 3-0 + 3-0

### 3. [high] Codex's write timing is an async background task at session startup, not at session end: it is triggered when a root session starts, gated on being non-ephemeral, the feature being enabled, not being a sub-agent, and the state DB being available; it skips still-active or too-short sessions and waits until a thread has been idle long enough (default ~6h, configurable 1-48h) before distilling; Phase 1 has a startup load cap, and Phase 2 exits at zero cost when there is no change after the artifact sync; generated memory fields have secrets redacted. The gate conditions + cooldown in jcode draft §5.1 align with this, and the additional per-day token budget gate for the BYOM scenario is a necessary enhancement (GitHub issues confirm that Codex's background memory generation does consume the user's quota).

**Evidence**: docs source text: "Codex skips active or short-lived sessions, redacts secrets from generated memory fields, and updates memories in the background instead of immediately at the end of every thread... waits until a thread has been idle long enough"; the README lists all four gate conditions. openai/codex issues #19732/#19105 confirm that background memory generation consumes the rate limit.

**Sources**: <https://github.com/openai/codex/blob/main/codex-rs/memories/README.md>, <https://developers.openai.com/codex/memories>

**Verification votes**: merged [2]+[6], 3-0 + 3-0

### 4. [high] Codex forgetting is usage-feedback-driven ranked eviction, not pure time decay: Phase 2 selection prioritizes by usage_count, then sorts by last_usage/generated_at, and directly ignores memories whose last_usage falls outside max_unused_days; the rollout summaries that lose out and over-age extended resources are physically pruned and reflected in the workspace diff (from which the consolidation agent surgically cleans up MEMORY.md); the read-path crate (codex-memories-read) is responsible for memory injection, citation parsing, and read-usage telemetry, feeding data into the feedback loop. jcode draft §3.2 (command-parse accounting) + §5.3 (usage ranking) is a full benchmark against this closed loop, and it avoids the citation-compliance risk of BYOM models.

**Evidence**: README: "ranks eligible memories by usage_count first, then by the most recent last_usage / generated_at... ignores memories whose last_usage falls outside the configured max_unused_days window"; "prunes stale rollout summaries... so cleanup appears in the workspace diff"; the read crate "owns the read path: memory developer-instruction injection, memory citation parsing, and read-usage telemetry classification".

**Sources**: <https://github.com/openai/codex/blob/main/codex-rs/memories/README.md>

**Verification votes**: [3], 3-0

### 5. [high] Claude Code auto memory storage is a project-scoped pure-markdown directory ~/.claude/projects/<project>/memory/, keyed by git repository (all worktrees and subdirectories of the same repo share one memory directory; non-git repos fall back to the project root); the layout is a MEMORY.md index + optional topic files (e.g. debugging.md, api-conventions.md) — i.e. "one file per topic" rather than "one file per fact". This is a direct correction to the jcode draft: line 4 and the §1.2 table saying "one md file per fact" do not match the official docs; the draft's notes/ inbox (small per-fact <ts>-<slug>.md files) is fine as a staging area, but the refined layer should be organized by task family / topic (the "task-family chunking" in draft §5.3 is already topic-oriented — only the benchmark description needs fixing).

**Evidence**: official docs: "Each project gets its own memory directory at ~/.claude/projects/<project>/memory/. The <project> path is derived from the git repository, so all worktrees and subdirectories within the same repo share one auto memory directory"; "MEMORY.md acts as an index... using MEMORY.md to keep track of what's stored where"; "Claude keeps MEMORY.md concise by moving detailed notes into separate topic files". The verifier also confirmed the per-repo sharing behavior on the local disk.

**Sources**: <https://code.claude.com/docs/en/memory>

**Verification votes**: merged [7]+[8], 3-0 + 3-0

### 6. [high] Claude Code's retrieval injection is hard-bounded: each session startup loads only the first 200 lines or 25KB of MEMORY.md (whichever comes first), and does not load anything beyond that; topic files are never loaded at startup and are read on demand by the model during the session using the standard file tools. This validates the jcode draft's three-tier progressive disclosure — "summary resident (default ≤1200 tokens truncated) + MEMORY.md grep + on-demand deep read" — and shows no dedicated retrieval tool is needed (consistent with draft §3.3).

**Evidence**: official docs: "The first 200 lines of MEMORY.md, or the first 25KB, whichever comes first, are loaded at the start of every conversation... Topic files like debugging.md or patterns.md are not loaded at startup. Claude reads them on demand using its standard file tools".

**Sources**: <https://code.claude.com/docs/en/memory>

**Verification votes**: [9], 3-0

### 7. [medium] Claude Code's writes are not purely online notes: the claim that "the model only writes selectively online during a session, with no post-hoc distillation pipeline" was rejected by 1-2 votes; on the contrary, an offline consolidation layer exists — the community dream-skill (104 stars) reproduces the unreleased Anthropic auto-dream feature (server-side flag tengu_onyx_plover), implementing a four-phase pipeline: Orient (scan the memory directory) → Gather Signal (use targeted grep to mine user corrections / preference changes / decisions / recurring patterns from recent session JSONL transcripts) → Consolidate (merge into existing memory, resolve contradictions, convert relative dates to absolute, deduplicate, clean up references pointing to nonexistent files) → Prune & Index (rebuild MEMORY.md into a lean index of <200 lines, demote verbose entries to topic files), triggered automatically via a Stop hook with 24-hour debouncing. Implication for jcode: both major vendors ultimately land on a two-layer "online write + offline consolidation", and jcode's hybrid architecture of inbox + Phase 2 sits right at the convergence point; dream-skill's consolidation rules (contradiction resolution, date absolutization, dead-link cleanup, index line-count cap) should be written into the Phase 2 consolidation agent prompt (draft §5.3 has some of this already; date absolutization and dead-link cleanup can be added).

**Evidence**: dream-skill README: "Scans recent session transcripts (JSONL files) for user corrections, preference changes, important decisions, and recurring patterns"; "Rebuilds MEMORY.md as a lean index under 200 lines... Demotes verbose entries to topic files". Multiple independent 2026 sources (the Claude Code internal dream prompt extracted by Piebald-AI, claudefa.st, VentureBeat's leak reporting) corroborate that auto-dream genuinely exists but is not officially released. Set to medium because auto-dream is attributed via community reproduction + leak evidence, not official docs; and the verifier notes that deduplication / contradiction resolution belong to the Consolidate phase, not Prune & Index (the phase-attribution detail must follow this wording).

**Sources**: <https://github.com/grandamenium/dream-skill>, <https://code.claude.com/docs/en/memory>

**Verification votes**: merged [14]+[15]+[16], 3-0 + 3-0 + 3-0; the reverse claim was rejected 1-2

### 8. [high] Anthropic's memory tool (API layer) is a pure client-side file-operation model: Claude only issues the six commands against the /memories prefix (view/create/str_replace/insert/delete/rename), and the actual storage is implemented by the host application itself, mapping to disk / database / cloud; once enabled, the API automatically injects a MEMORY PROTOCOL system prompt (view the memory directory before doing anything, write progress as you work, assume the context can reset at any time) — i.e. writing within an online task rather than post-session distillation. Lesson for jcode: the memory_note tool description can directly absorb the phrasing discipline of the MEMORY PROTOCOL; the client-side model of "write scope guaranteed by the implementation layer" is isomorphic to the approval-free + path-locked design in draft §4.

**Evidence**: official docs: "The memory tool operates client-side: Claude requests file operations, and your application executes them... The /memories path is a prefix that your handler maps onto real storage"; "When the memory tool is present in your request's tools, the API automatically adds this instruction to the system prompt... ALWAYS VIEW YOUR MEMORY DIRECTORY BEFORE DOING ANYTHING ELSE... ASSUME INTERRUPTION".

**Sources**: <https://platform.claude.com/docs/en/agents-and-tools/tool-use/memory-tool>

**Verification votes**: merged [10]+[11], 3-0 + 3-0

### 9. [high] In the memory tool design, forgetting and security are both assigned to the application side, and the official docs give a directly copyable checklist: (1) periodically delete memory files not accessed for a long time (expiration based on access time); (2) cap single-file size, cap the character count returned by view, and support view_range pagination; (3) the model "will usually refuse" to write sensitive information but the application must run another redaction check before writing to disk; (4) every command must be path-validated to prevent /memories/../../ directory traversal (canonicalize, reject ../ and URL-encoded variants) — this class of attack is real (Anthropic Filesystem MCP Server's CVE-2025-53110/53109). jcode draft §6 already covers redaction and path-prefix validation, and should add: a memory file-size cap, paginated reads for oversized memory files, and access-time expiration based on the §3.2 usage accounting (which naturally coincides with max_unused_days eviction).

**Evidence**: official docs: "Memory expiration: Periodically delete memory files that haven't been accessed in a long time"; "Track memory file sizes and cap how large a file can grow... let Claude page through the rest with view_range"; "Your implementation must validate every path in every command to prevent directory traversal attacks". The verifier cites the CVEs disclosed by Cymulate to corroborate that the attack class is real.

**Sources**: <https://platform.claude.com/docs/en/agents-and-tools/tool-use/memory-tool>

**Verification votes**: merged [12]+[13], 3-0 + 3-0

### 10. [high] Mem0 uses a two-phase pipeline (isomorphic in structure to jcode's two-phase distillation, but for online per-message-pairs, not offline batch): the extraction phase draws on a running session summary + recent messages to extract candidate facts from each new message pair, and the update phase compares each candidate against existing memory, with the LLM selecting via function-calling from exactly four operations — ADD (new fact) / UPDATE (augment existing) / DELETE (remove memory contradicted by new info) / NOOP (skip). That is, forgetting is contradiction-driven at write time rather than time decay. Improvement for jcode: when the Phase 2 consolidation agent digests the notes/ inbox, it can be required to explicitly emit an ADD/UPDATE/DELETE/NOOP decision for each candidate — this turns free-text consolidation into an assertable, testable protocol with a measurable no-op rate (directly serving the draft's M2 acceptance criteria).

**Evidence**: paper source text: "The extraction phase initiates upon ingestion of a new message pair... extracts a set of salient memories"; "determines which of four distinct operations to execute: ADD... UPDATE... DELETE for removal of memories contradicted by new information; and NOOP". The verifier confirmed the operations are selected directly by the LLM via the tool-call interface; note that Mem0's managed product has an additional retrieval-layer recency re-ranking and optional expiration_date, which are outside the paper's scope.

**Sources**: <https://arxiv.org/abs/2504.19413>

**Verification votes**: merged [17]+[18], 3-0 + 3-0

### 11. [high] Zep's core is Graphiti, a temporally-aware knowledge-graph engine with a three-tier structure (raw episode nodes → LLM-extracted semantic entity nodes → community nodes clustering strongly-connected entities); writing happens at ingestion: entity names are embedded into 1024-dim vectors, candidates are recalled by cosine similarity, and an LLM entity-resolution prompt merges duplicates before entry into the graph (edge deduplication works the same way); forgetting is bi-temporal edge invalidation rather than deletion — it tracks four timestamps (t'created/t'expired record in-system ingestion, t_valid/t_invalid record real-world validity), and when a new fact contradicts an old one, the old edge's t_invalid is set to the new edge's t_valid, with the full history retained. For jcode: the graph-database form is not applicable (it violates zero-dependency), but the "invalidate rather than delete, history auditable" principle jcode gets for free via git history (the git log audit/rollback in draft §2.2 is precisely the filesystem-version equivalent); "dedup + resolve at ingestion" suggests Phase 1 output can undergo a lightweight duplicate check against the existing summary before landing on disk.

**Evidence**: paper source text: "a temporally-aware knowledge graph engine... three hierarchical tiers"; "embeds each entity name into a 1024-dimensional vector space... processed through an LLM using our entity resolution prompt"; "invalidates the affected edges by setting their tinvalid to the tvalid of the invalidating edge". The verifier checked that the full text matches sentence by sentence; the only dispute (the benchmark dispute with MemGPT) does not touch the architecture description.

**Sources**: <https://arxiv.org/abs/2501.13956>

**Verification votes**: merged [19]+[20]+[21], 3-0 ×3

### 12. [high] LangMem provides two precedents directly useful to jcode's interface design: (1) the core API is decoupled from storage/framework — the stateless extract/consolidate functions can be configured with any storage backend (bring-your-own persistence), proving that "core distillation logic + pluggable store interface" is entirely feasible on a pure-Go file backend (jcode can define a MemoryStore interface and ship only a file implementation in v1); (2) the official division of three classes of retrieval injection conditions — data-independent memory always in the prompt, data-dependent memory recalled by semantic similarity, the rest recalled by a combination of application context + similarity + time — i.e. not all memory should go through similarity retrieval, and the core layer should be injected unconditionally, which is exactly the theoretical basis for jcode's layering of a resident memory_summary.md + MEMORY.md grep (and shows that jcode having no vector store and using grep for the second-tier recall is a reasonable trade-off, not a defect).

**Evidence**: blog source text: "You can use its core API with any storage system and within any Agent framework"; "(1) data-independent - they are always present in the prompt. (2) Data-dependent and may be recalled based on semantic similarity. (3) Others may be recalled based on a combination of application context, similarity, time, etc." The official conceptual guide corroborates that the core functions do not depend on a specific database.

**Sources**: <https://www.langchain.com/blog/langmem-sdk-launch>

**Verification votes**: merged [22]+[23], 3-0 + 3-0

### 13. [medium] jcode draft improvement checklist (by priority, all derived from the confirmed claims above): 1) [doc correction] change the draft's "one file per fact" description of Claude Code to "MEMORY.md index + one file per topic", and make the refined-layer organization principle explicitly by-task-family/by-topic (the inbox stays as small per-fact files); 2) [protocolize] have the Phase 2 consolidation agent explicitly emit an ADD/UPDATE/DELETE/NOOP decision for each inbox/summary input (Mem0), making M2/M3 acceptance quantifiable; 3) [prompt enhancement] add dream-skill's three rules to the consolidation prompt: convert relative dates to absolute dates, resolve contradictions, clean up references pointing to nonexistent files; add a line-count cap to the MEMORY.md index (Claude Code's 200-line/25KB injection bound corroborates the reasonableness of the draft's 1200-token truncation); 4) [security fill-in] per the official memory tool checklist add: a memory single-file size cap, paginated reads for oversized files, path validation covering URL-encoded traversal variants; 5) [verified, no change needed] the file+git form, async-at-startup + idle gate conditions, usage-ranked eviction, zero-token exit on no diff, resident summary + grep layering, state.json in place of SQLite — all map one-to-one to a mechanism in at least one primary source.

**Evidence**: synthesis finding: each improvement point is anchored respectively in the confirmed mechanisms of findings 1-12, derived by a section-by-section comparison with /Users/jack/workpath/jjj/jcode/internal-doc/agent-memory-design.md (the §1.2 table and line 4 need correction, §5.3 can be protocolized, §6 can be filled in). Set to medium because the checklist itself is an interpretive synthesis, not a direct statement from a single source.

**Sources**: <https://github.com/openai/codex/blob/main/codex-rs/memories/README.md>, <https://code.claude.com/docs/en/memory>, <https://platform.claude.com/docs/en/agents-and-tools/tool-use/memory-tool>, <https://arxiv.org/abs/2504.19413>, <https://github.com/grandamenium/dream-skill>

**Verification votes**: synthesis over all confirmed claims


## Appendix A: eino framework memory practice follow-up (separate agent, dual verification via local source + official docs)

**Core conclusion: eino officially has no memory component (a business-layer concept); jcode building its own file storage is the orthodox approach.**

- eino v0.9.9 (jcode's actual dependency) has no memory in `components/`; the eino-ext code search returns zero results; the official doc "Memory and Session" explicitly states it "is not a core framework component"; issue #203 was closed as "build your own via callback". There is no official long-term memory abstraction, and the docs do not distinguish short-term from long-term.
- Three official examples: the `MemoryStore{Write/Read/Query(sessionID, text, limit)}` interface (Redis/in-memory implementation) in `react/memory_example/memory`; `eino_assistant/pkg/mem/simple.go`, JSONL with one file per session (the closest to jcode); `chatwitheino/mem/store.go`, generic JSONL + pendingInterruptID stored alongside history.
- Community: hildam/eino-history (MySQL/Redis, low activity, no file backend); no mature dedicated write-up on "eino long-term memory".
- adk provides hooks (verified locally on v0.9.9): SessionValues (in-run KV, non-persistent), ChatModelAgentMiddleware's BeforeModelRewriteState (already used by jcode compaction), GenModelInput, CheckPointStore (Get/Set bytes), the summarization middleware (TranscriptFilePath source-text pointer), the reduction middleware (offload oversized output to a file + ClearAtLeastTokens to preserve cache), the agentsmd middleware (**transient prepend not entered into state, immune to compaction — memory injection should be isomorphic**).
- Adoption for jcode: (1) the three-method interface shape; (2) transient injection not entered into history; (3) do not wait for an official SDK. Incidental findings (spin off as separate tasks): the transcript pointer, reduction offload, and a CheckPointStore file implementation.

Sources: cloudwego.io/zh/docs/eino/quick_start/chapter_03_memory_and_session/ | github.com/cloudwego/eino/issues/203 | pkg.go.dev/github.com/cloudwego/eino-examples/flow/agent/react/memory_example/memory | ~/go/pkg/mod/github.com/cloudwego/eino@v0.9.9/adk/{runctx,handler,chatmodel}.go, middlewares/{summarization,reduction,agentsmd}
