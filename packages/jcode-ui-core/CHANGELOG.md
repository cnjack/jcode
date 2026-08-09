# Changelog

## Unreleased

### Added

- **Durable tool and Artifact contracts.** New `ToolSurface`, `ToolPhase`,
  `ToolOutcome`, `ArtifactStorageKind`, and `ArtifactRef` types describe
  standalone timeline surfaces, monotonic generated-media lifecycles, and
  opaque workspace/managed Artifact references. `ToolCall` now carries the
  operation id, surface, phase, terminal outcome, typed error code, immutable
  provider/model snapshot, and structured Artifact references needed to replay
  these tools without parsing display strings.
- **Lifecycle-aware renderer contract.** `ToolRendererProps` now receives the
  lifecycle, provider, model, Artifact, and operation fields, and the exported
  `toolCallToRendererProps()` helper provides the single mapping used by both
  generic and standalone tool surfaces.
- **Structured billable approvals.** New `BillableApprovalSummary` plus
  `Approval.approvalClass` / `Approval.billableSummary` carry a bounded,
  non-secret summary for external image/search decisions. Pending renderers can
  inspect `ApprovalDecisionActions.canResolveOptions` before returning opaque
  option ids.
- **Paged Ask User controls.** `AskUserControls` gains `activeIndex`,
  `setActiveIndex`, `isSubmitting`, and the stable `submitError` code;
  `submit()` and `skip()` are now asynchronous controls. The public
  `AskUserSubmitError` type currently exposes `submit_failed`.
- Exported `isStandaloneTool()`. Tools that declare
  `surface: 'standalone'`, together with `ask_user`, are explicit hard
  boundaries for activity grouping and same-batch reordering.

### Changed

- `RuntimeActions.submitAskUser` may now return a Promise so headless
  primitives can keep the interaction pending until the host confirms the
  answer. The mock runtime writes resolved output as `{ answers: [...] }`,
  matching the persisted runtime payload consumed by receipt rendering.
- Ask User state resets for a new `askUserId`; option selection clears custom
  text and non-empty custom text clears selected options. Numeric shortcuts act
  on the visible question, incomplete submission focuses the first unanswered
  page, and failed host submissions restore the controls with answers intact.
- `groupActivityTimeline()` no longer absorbs or reorders standalone tools;
  they retain their original position and split the surrounding activity
  groups.

### Security

- `ApprovalBlock` now fails closed for `billable_external` approvals when the
  host does not implement `resolveApprovalOption`; opaque allow-once decisions
  are never coerced into the legacy boolean approval contract. Non-billable
  option approvals retain the compatibility fallback.

## 0.4.0 — 2026-07-13

Activity groups — Claude Code / Codex-style timeline coalescing:

- **Types**: new `ActivityGroup { id, tools, status, explorative }`;
  `ThreadItem` gains the UI-only `'activity'` kind with an `isActivityItem`
  guard.
- **`timeline/groupActivity.ts`** — `groupActivityTimeline(items)` coalesces
  ALL adjacent tool items (read-only or mutating, batched or not) into one
  `activity` group: same-`batchId` tools are first pulled to the first
  member's slot (batch logic absorbed from `groupToolTimeline`, approvals in
  between included), then adjacent tools/batches merge. Messages break a
  group; approvals do NOT and keep rendering in place. Isolated single tools
  stay plain `tool` cards. Output never contains `'exploring'`/`'batch'`.
- **`summarizeActivityCounts(tools)`** — category-count header text: mixed
  groups get verb phrasing (`Ran 3 commands · read 2 files · ran 1 agent`),
  all-read-only groups (per `isCollapsibleTool`) get the Explored phrasing
  (`3 files read · 2 searches · 1 list`). `execute` ALWAYS counts as a
  command — even when the backend classifies it read/search/list — and
  explicit names win over `displayInfo.kind` (`glob` stays a list). Reads and
  edits dedupe by file (subtitle).
- **`countActivityFlags(tools)`** — collapsed-header suffix counts: `failed`
  (errored or nonzero exit; denied excluded) and `denied`.
- **turnChanges**: `summarizeTurnChanges`/`appendTurnChangeSummaries` now
  also collect change tools inside `'activity'` groups.
- **Deprecated** (kept + exported for external consumers):
  `groupExploringTimeline`, `groupToolTimeline`, `ExploringGroup`,
  `ToolBatchGroup` — superseded by activity groups.
- **tsconfig**: `rewriteRelativeImportExtensions` + `allowImportingTsExtensions`
  enabled so cross-module runtime imports can use `.ts` specifiers (needed by
  the `node --experimental-strip-types --test` runner; emit is unchanged).

Turn-level change summaries (opencode SessionTurn-style):

- **Types**: new `TurnFileChange` / `TurnChangesSummary`; `ThreadItem` gains
  the UI-only `'turnchanges'` kind with an `isTurnChangesItem` guard.
- **`timeline/turnChanges.ts`** — `summarizeTurnChanges(items)` aggregates the
  edit/multi_edit/write calls of one turn into a per-file change list: dedupes
  by `file_path` keeping the LAST change, skips denied/errored calls, returns
  null while any tool in the turn is still running, caps display files at 10
  (`overflow` carries the rest), and derives ± line counts client-side from
  tool args via the exported `diffStatForTool` (edit old/new strings,
  multi-edit `edits` sums, write content lines as additions — the backend
  sends no diff stats). `appendTurnChangeSummaries(items, { isRunning })`
  inserts a `turnchanges` item at the end of every completed turn (turn
  boundary = user message → next user message; the last turn is suppressed
  while running; synthetic seq = last item seq + 0.5).

## 0.3.0 — 2026-07

Concurrent tool-call batch groups:

- **Types**: `ToolCall` gains optional `batchId` / `batchIndex` / `batchSize` /
  `startedAt` (unix ms); new `ToolBatchGroup` type; `ThreadItem` gains the
  `'batch'` kind with an `isBatchItem` guard.
- **`groupToolTimeline(items)`** — coalesces same-`batchId` tools into a batch
  item anchored at the first member's position (approvals in between stay in
  place and don't break the group; single-member batches stay plain tool
  cards). Tools without a `batchId` keep the existing exploring-adjacent
  coalescing unchanged. `explorative` is true when every member passes
  `isCollapsibleTool`.
- **Exploring summaries**: `summarizeExploringSteps` dedupes repeated file
  names in merged Read lines; new `summarizeExploringCounts` builds a compact
  category-count summary (`3 files read · 2 searches · 1 list`).
- **Hooks**: new `useElapsed(startedAt, active)` (1s tick while active) and
  `formatElapsed(ms)` (`2s`, `1m 05s`) for live duration badges.
- **exportThreadMarkdown**: `'batch'` items export as their member tools.

Approval semantics:

- **Types**: `ToolCall` gains optional `denied` (user rejected the call at the
  approval prompt — a distinct state from `status: 'error'`) and
  `awaitingApproval` (call is sitting at an unresolved approval prompt);
  `Approval` gains optional `tool_call_id` so hosts can tie a prompt to the
  exact pending tool row.
- **ToolCallView** stamps `data-tool-denied="true"` /
  `data-tool-awaiting-approval="true"` on the root element for external
  styling hooks.

All existing exports unchanged (`groupExploringTimeline` kept).

## 0.2.1 and earlier

See the jcode-ui package changelog — the two packages version together.
