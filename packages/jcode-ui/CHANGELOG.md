# Changelog

## Unreleased

### Added

- **Generated-image surface.** New public `GeneratedImageCard` component and
  `GeneratedImageState` / `GeneratedImageCardProps` /
  `GeneratedImageCardStrings` types render the complete generated-media
  lifecycle (`queued`, `generating`, `saving`, `succeeded`, `failed`,
  `cancelled`, and `uncertain`). The card supports managed Artifact metadata,
  provider/model context, typed error guidance, host-controlled open/download/
  reveal/settings actions, accessible live status, reduced motion, and the
  compact scan-weave generating/saving treatment.
- **Standalone tool renderers.** `Thread` now treats `surface: 'standalone'`
  tools as hard timeline boundaries and renders their registered renderer as
  the complete surface instead of wrapping it in the generic collapsible tool
  card. Hosts can use the new `hidePendingAskUser` prop when a pending question
  is presented in a separate interaction dock; resolved receipts remain in the
  transcript.
- **Billable approval presentation.** `ApprovalBanner` recognizes
  `approvalClass: 'billable_external'` and presents the bounded provider,
  model, size, count, and capability summary with explicit deny/allow-once
  actions. Image generation and provider web search receive distinct copy and
  icons without exposing the full prompt or raw tool arguments.
- Re-exported the new core media contracts — `ArtifactRef`, `ToolPhase`,
  `ToolOutcome`, and `ToolSurface` — from the main package entry.

### Changed

- **Ask User can own the composer dock.** `AskUserCard` now supports `timeline`
  and `dock` placements, shows one question at a time with previous/next
  navigation, preserves answers while paging, supports numeric shortcuts,
  multi-select and custom answers, marks recommended choices, and accepts
  host-provided strings. Pending dock cards can replace the composer, while a
  completed or skipped interaction collapses to a compact transcript receipt.
- **Image capabilities are explicit in the product composer.** `ModelInfo`
  gains `input_modalities`, `output_modalities`,
  `capability_availability`, and `image_sizes`; `ProductComposerStrings` gains
  `modelImageOutput`. The chat picker now limits the conversation list to
  enabled text-output models with tool calling, distinguishes image input from
  image output, and retains legacy text-output behavior when modality metadata
  is absent.

### Fixed

- Ask User submission now disables duplicate actions while pending, surfaces a
  localized inline error when the host rejects the request, and lets the user
  retry without losing answers. Option and custom-text answers are mutually
  exclusive, Enter advances the current page, and final submission returns to
  the first unanswered question instead of silently sending an incomplete
  batch.

## 0.4.1 — 2026-07-13

Republish of 0.4.0 with correct dependency metadata (0.4.0 was published
with npm and shipped a literal `workspace:*` dependency — BROKEN on npm,
use 0.4.1). No code changes.

Activity groups — Claude Code / Codex-style collapsed tool timeline:

- **ActivityGroupCard** — ALL adjacent tool calls now coalesce into one
  activity group. Once every member settles it collapses to a single muted
  line: chevron + status icon + category counts (`Ran 3 commands · read 2
  files · ran 1 agent`; all-read-only groups show `Explored` + `3 files read
  · 2 searches`). Failures stay visible collapsed (error icon + `N failed`);
  denied members append a muted `N denied`. Expanded it is a bordered card
  with one row per tool (icon + title + mono subtitle + elapsed/exit badge)
  where each row expands IN PLACE to the tool's registry-rendered body
  (diff/shell/subagent renderers) — no duplicate summary list anywhere.
  Expansion follows `userOverride ?? (status === 'running')`: auto-open while
  anything runs (live elapsed rows), auto-collapse when the group settles,
  manual toggles win from then on.
- **ToolRow / ToolRowHeader** — the batch row implementation extracted from
  ToolBatchGroupCard into a shared component; activity groups and batches
  render the same row.
- **Thread** now maps items through `groupActivityTimeline` (batches
  absorbed) and no longer produces `'exploring'`/`'batch'` items; both render
  branches remain for hosts that feed those kinds directly.
- **Deprecated** (kept + exported): `ExploringGroupCard`,
  `ToolBatchGroupCard`.
- New re-exports from core: `isActivityItem`, `groupActivityTimeline`,
  `summarizeActivityCounts`, `countActivityFlags`, and the `ActivityGroup`
  type.
- CSS: new `.jcode-activity` family (header hover, bordered
  `.jcode-activity__body` reusing the `.jcode-toolbatch` row styles), built
  entirely from existing `--jcode-*` tokens.

Turn-level change summary card (opencode SessionTurn-style):

- **TurnChangesCard** — after a turn completes, a slim `Changed N files`
  header (with a green/red `+A −R` badge when line counts are derivable)
  summarizes the turn's file changes. Expanded, it lists one row per file
  (deduped, last change wins) with per-file ± counts; clicking a row expands
  that change's registry-rendered body (diff/file renderers apply) via the
  same ToolCallCard slot-header path batch rows use. Files beyond 10 collapse
  behind an expandable `… N more`.
- **Thread** appends the summaries automatically
  (`appendTurnChangeSummaries` after `groupToolTimeline`); nothing is shown
  while the turn still has running tools or the agent is working.

Subagent inline progress:

- **ToolCallCard** subagent header now shows `↳ <current tool> <subtitle>`
  (last running child, shimmer animation) while running, and
  `N toolcalls · <duration>` once finished (duration from `meta.duration_ms`
  when the host provides it — the jcode web store merges the runner-measured
  event duration for every tool — else frozen from `startedAt` at the
  running→done transition; omitted when unknown).
- Re-exports from core: `isTurnChangesItem`, `summarizeTurnChanges`,
  `appendTurnChangeSummaries`, `diffStatForTool`, and the
  `TurnChangesSummary` / `TurnFileChange` types.

## 0.3.0 — 2026-07 (with jcode-ui-core 0.3.0)

Concurrent tool-call batch groups (Claude-Code-style):

- **ToolBatchGroupCard** — new component for tools issued concurrently by one
  assistant message (same `batchId`). Mixed batches render as a flat row
  stack with no group header: each row shows a status icon (● running with
  pulse / ✓ done / ✗ error), the tool title, a mono subtitle, a right-side
  elapsed badge, and expands independently to the tool's registry-rendered
  body (diff/shell renderers apply). Running rows tick a live elapsed badge
  (`2s`, `1m 05s`); finished rows show a duration only beyond 2s
  (`meta.duration_ms` preferred); error rows add `exit N` in the error color.
- **Thread** now groups via `groupToolTimeline` (batch + exploring). Tools
  without a `batchId` (old sessions/replay) behave exactly as before.
- **ExploringGroupCard** header upgraded to a category-count summary
  (`3 files read · 2 searches · 1 list`); merged Read lines dedupe file names.
  All-read/search/list batches render as this upgraded Exploring card.
- Subagent children lists handle batch groups (compact rows); markdown export
  expands batches into per-tool entries.
- Re-exports from core: `isBatchItem`, `groupToolTimeline`,
  `summarizeExploringCounts`, `useElapsed`, `formatElapsed`, and the
  `ToolBatchGroup` / `ExploringGroup` types.

Approval semantics (opencode/codex-inspired):

- **Denied ≠ error** — a tool call the user rejected at the approval prompt
  (`tool.denied`) renders with a struck-through, muted title/subtitle, a
  neutral `Denied` badge, and a `⊘` status glyph. It never uses the
  destructive/error red. Applied consistently in `ToolCallCard`,
  `ToolBatchGroupCard` rows, and `CompactToolRow`.
- **Awaiting approval = warning yellow** — while a call sits at an unresolved
  approval prompt (`tool.awaitingApproval`), its title/status glyph switch to
  `--jcode-color-warning-fg`, the shimmer/pulse pauses, and the live elapsed
  badge is replaced with `approval…` (the backend excludes approval wait from
  the reported duration, so timers effectively pause during approval).
- `data-tool-denied` / `data-tool-awaiting-approval` attributes (from core's
  `ToolCallView`, mirrored on `CompactToolRow`) for external customization.

## 0.2.3 — 2026-07

Republish of 0.2.2, which was again published with npm and shipped the literal
`workspace:*` specifier (uninstallable — same failure as 0.2.0, now
deprecated). No code changes. Reminder: **always `pnpm publish`.**

## 0.2.2 — 2026-07 (with jcode-ui-core 0.2.1) — BROKEN on npm, use 0.2.3

Post-release review fixes (PR #133 findings):

- **AG-UI runtime**: `enqueueMessage` is now a real type-ahead queue — drafts
  composed mid-run are kept in `RuntimeState.queued` and drained one per
  natural turn end (never after a user `stop()`); `removeQueuedMessage` works.
  Previously queued drafts were silently dropped.
- **Composer**: `send()` keeps uploading/error attachment slots (only `done`
  ones are consumed) and resets dictation buffers so recognized text can't
  resurface after sending.
- **ConnectionBanner**: the "Reconnected" flash now auto-hides — the effect no
  longer cancels its own timeout via a self-triggering dependency.
- **WorkflowCanvas**: `interactive` can no longer be silently overridden by
  props spread; per-flag overrides still supported.
- **Transcription**: active-segment ref survives backward seeks.
- **FileTree renderer**: trailing annotations ("(dir)", size columns) are now
  actually stripped as documented.
- Packaging: `./package.json` added to both packages' exports; core selftest
  artifacts excluded from the published tarball; deprecated CSS
  (`word-break: break-word`, `clip`) replaced.

## 0.2.1 — 2026-07

Republish of 0.2.0 with correct dependency metadata: the 0.2.0 tarball was
published with npm, which leaked the literal `workspace:*` specifier for
`jcode-ui-core` and made the package uninstallable. No code changes.
`jcode-ui@0.2.0` is deprecated on the registry. Always publish with
`pnpm publish` (it rewrites workspace specifiers at pack time).

## 0.2.0 — 2026-07

The "complete the loop" release: scoped theming, the full conversation
lifecycle, a second-generation composer, streaming-stable markdown, and three
new optional subentries. See the
[migration guide](https://www.j-code.net/chat-ui/docs/guides/migration-0.2).

### Breaking

- **Scoped tokens.** All design tokens moved from `:root` to `[data-jcode-ui]`
  and gained a `--jcode-` prefix (`--color-primary` → `--jcode-color-primary`).
  Components stamp the attribute on their own roots; nothing leaks into host
  pages. Legacy names keep working via `jcode-ui/compat.css`; shadcn apps can
  inherit their theme via `jcode-ui/shadcn.css`.
- Animation classes/keyframes prefixed: `.animate-fade-in` →
  `.jcode-animate-fade-in`, `@keyframes fade-in` → `jcode-fade-in`, etc.
- `RuntimeState` gains a required `connection` slice (defaulted to
  `'connected'` by `normalizeState` — external-store/mock users unaffected).

### Conversation loop

- **BranchPicker** — `‹ 2/3 ›` version navigation on branched messages
  (`Message.versions` + `switchVersion`), plus **regenerate** and **👍/👎
  feedback** actions in the message footer and **retry** on failed turns. All
  fail-visible: controls render only when the host implements the action.
- **ConnectionBanner** — sticky `reconnecting / disconnected / reconnected`
  transport status driven by `RuntimeState.connection`.
- **ThreadWelcome + Suggestions** — empty-state hero and prompt pills
  (starters and follow-ups; `<Thread suggestions={…}>` renders under the last
  turn when idle).
- **ExportButton / exportThreadMarkdown** — download the timeline as portable
  markdown (tools fold into `<details>`, fences escape safely).
- **QuoteSelection + formatQuote** — select thread text → floating Quote
  button → blockquote lands in the composer via the new `ComposerHandle`.

### Approvals

- Host-defined **approval options** (`Approval.options`, arbitrary ids —
  ACP-compatible) render as one control per option; `allow_always` kinds keep
  the two-step arming UX. New optional action `resolveApprovalOption`.

### Composer 2

- **AttachmentAdapter** — pluggable uploads with progress + retry; generic
  file chips alongside the base64 image fast path. Drag & drop and paste
  (screenshots) built in.
- **Slots** — `leadingControls` / `trailingControls` / `footer`; new styled
  **ModelSelector** drops into `leadingControls`.
- **Dictation** — optional Web Speech input (`enableDictation`).
- `ChatInput`/`Composer` expose `ComposerHandle { insertText, focus }` via ref.

### Rendering

- **Streaming-stable markdown** — unclosed fences/emphasis/links/tables are
  completed before parse; finished blocks are cached (`useStreamingMarkdown`)
  so long streams stop re-rendering the whole document.
- **Code-block chrome** — language + filename bar (```` ```ts title=a.ts ````
  or `ts:a.ts`) with a copy button (`bindCodeBlockCopy`).
- **Optional plugins** — `jcode-ui/plugins/mermaid` and
  `jcode-ui/plugins/katex` (dynamic-import peers; zero cost when unused).

### Runtime-wired renderers & components

- **TaskList** (todo lists as a first-class component + `RuntimeTaskList`),
  **FileTree**, **TestResults** (go test / vitest parsing), **StackTrace**
  (Go/JS, `jcode-ui:open-file` events), **Artifact** container, and
  `slots` on `Message`/`ToolCallCard`.

### New subentries

- **`jcode-ui/canvas`** — agent workflow canvas on `@xyflow/react` (optional
  peer): status-aware nodes, animated edges, `toolTreeToGraph`.
- **`jcode-ui/voice`** — SpeechInput, Transcription, AudioPlayer,
  VoiceVisualizer (browser APIs only).
- **ThreadList** — `ThreadStore` contract + styled sidebar list
  (`createMockThreadStore` included).
- **`createAGUIRuntime`** — drive the components from any AG-UI backend
  (LangGraph, CrewAI, Mastra, …); SSE transport, JSON-Patch shared state,
  injectable transport for tests.

### Fixes

- Message → tool-card vertical rhythm tightened (hover actions row no longer
  reserves 30px).
- Approval primitive stamps its own root (theming works without a wrapper).

## 0.1.1 — 2026-06

Initial public release: Thread, ChatInput, Message, ToolCallCard (+9
renderers), ApprovalBanner, AskUserCard, ContextBar, Reasoning, Sources,
Attachment; ExternalStore/Mock runtimes; token-driven theming.
