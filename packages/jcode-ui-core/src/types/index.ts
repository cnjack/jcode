/**
 * Core message + tool types for jcode-ui.
 *
 * These mirror the jcode backend contract (see `web/src/types/api.ts`) but are
 * framework-agnostic and the single source of truth for both `jcode-ui-core`
 * (headless primitives) and `jcode-ui` (styled components).
 */

/** Who authored a message. */
export type Role = 'user' | 'assistant' | 'system'

/**
 * A base64-encoded image attached to a message (no `data:` prefix).
 *
 * Image-first (vision models). Generic file/PDF adapters are host concerns —
 * see docs/chat-ui attachments guide. Optional `name` is used for tooltips and
 * a11y labels (mirrors assistant-ui Attachment.name).
 */
export interface ChatImage {
  data: string
  media_type: string
  /** Original filename when known (file picker / drag-drop). */
  name?: string
}

/** Severity for `system` messages. Undefined → default neutral styling. */
export type SystemLevel = 'error' | 'notice'

/**
 * A single chat message. Streaming assistant text is represented as a message
 * whose `content` grows over time — the runtime owns the accumulation, not the
 * component, so `Message` re-renders idempotently on `content` changes.
 */
export interface Message {
  id: string
  role: Role
  content: string
  timestamp: number
  /** Origin channel for inbound messages (e.g. 'wechat'). Drives the compact
   *  source label and any host-provided identity chrome. */
  source?: string
  images?: ChatImage[]
  /** system-message severity. */
  level?: SystemLevel
  /** Optional raw detail (collapsed by default). */
  detail?: string
  /** Assistant turn elapsed (ms), stamped on the final message of a turn. */
  durationMs?: number
  /** Optional model reasoning / chain-of-thought text (rendered collapsible).
   *  Mirrors assistant-ui's Reasoning component + OpenAI/Anthropic thinking. */
  reasoning?: string
  /** Optional citation sources for the message (rendered as a Sources list).
   *  Mirrors assistant-ui's Sources component. */
  sources?: MessageSource[]
  /** Alternate versions (edit/regenerate branches). `content` mirrors the
   *  active version; absent for unbranched messages. */
  versions?: MessageVersion[]
  /** Which entry of `versions` is showing. */
  activeVersionId?: string
  /** Recorded 👍/👎 feedback, when the host persists it. */
  feedback?: 'up' | 'down'
}

/** Transport liveness surfaced by the runtime (drives ConnectionBanner). */
export type ConnectionState = 'connected' | 'reconnecting' | 'disconnected'

/** One alternate take of a message — produced by editing a user message or
 *  regenerating an assistant one. The parent `Message.content` always mirrors
 *  the active version so non-branching consumers keep working untouched. */
export interface MessageVersion {
  id: string
  content: string
  timestamp: number
  reasoning?: string
  sources?: MessageSource[]
  images?: ChatImage[]
}

/** A citation source attached to a message (e.g. a retrieved doc or URL). */
export interface MessageSource {
  /** Stable id for keying. */
  id: string
  /** Display title of the source. */
  title: string
  /** Optional URL or deep link. */
  url?: string
  /** Optional snippet/excerpt quoted from the source. */
  snippet?: string
}

/** Display metadata for a tool call, surfaced from the backend or extracted
 *  client-side from args. Lets renderers show a title/icon without parsing args. */
export interface ToolDisplayInfo {
  title: string
  subtitle?: string
  icon?: string
  /** 'context' | 'mutation' | 'execution' — informational grouping. */
  category?: string
  /** Presentation kind: read | search | list | shell | edit | agent | other. */
  kind?: string
  /** When true, adjacent tools may coalesce into an Exploring group. */
  collapsible?: boolean
}

export type ToolStatus = 'running' | 'done' | 'error'

/** Timeline surface requested by a tool from its initial tool_call event. */
export type ToolSurface = 'activity' | 'standalone'

/**
 * Durable image-tool lifecycle. `terminal` is intentionally separate from the
 * outcome so reducers can enforce monotonic ordering without guessing whether
 * a terminal call succeeded, failed, was cancelled, or became uncertain.
 */
export type ToolPhase = 'queued' | 'generating' | 'saving' | 'terminal'
export type ToolOutcome = 'succeeded' | 'failed' | 'cancelled' | 'uncertain'

export type ArtifactStorageKind = 'workspace' | 'managed'

/** Safe, opaque reference to an Artifact. It never contains pixels or paths
 * outside the storage-kind-relative fields below. */
export interface ArtifactRef {
  id: string
  storage: ArtifactStorageKind
  /** Storage-kind-relative key. Never an absolute path or provider URL. */
  key: string
  title: string
  kind: string
  media_type: string
  size: number
  width?: number
  height?: number
  provider?: string
  model?: string
  operation_id?: string
  tool_call_id?: string
  shareable?: boolean
}

/** Structured stdout/stderr for execute-style tools (dual-channel UI path). */
export interface ToolStreams {
  stdout?: string
  stderr?: string
  aggregated?: string
}

/** Structured execution metadata for execute-style tools. */
export interface ToolMeta {
  exit_code?: number
  duration_ms?: number
  timed_out?: boolean
  truncated?: boolean
  spill_path?: string
}

/** Presentation hints attached to a tool result. */
export interface ToolPresentation {
  kind?: string
  title?: string
  subtitle?: string
  collapsible?: boolean
}

/**
 * A tool invocation. `args`/`output` are raw JSON strings; renderers parse
 * them. `children` carries subagent-nested calls (rendered recursively).
 */
export interface ToolCall {
  id: string
  /** Backend tool_call_id for precise result matching. */
  toolCallID?: string
  /** Host-generated id of the exact permission gate that released this call. */
  approvalID?: string
  /** Runner-owned generation-operation id. Never inferred from toolCallID. */
  operationID?: string
  name: string
  args: string
  output?: string
  /** Clean output for UI display (metadata stripped). */
  displayOutput?: string
  error?: string
  status: ToolStatus
  /** Initial timeline surface. Standalone tools are hard Activity boundaries. */
  surface?: ToolSurface
  /** Monotonic operation phase. Image tools start queued. */
  phase?: ToolPhase
  /** Required for terminal image operations. */
  outcome?: ToolOutcome
  /** Typed backend error classification; the UI never parses `error`. */
  errorCode?: string
  /** Immutable provider/model snapshot for this operation. These never derive
   * from the host's currently selected image model. */
  provider?: string
  model?: string
  /** Opaque, structured result references. Duplicate ids are ignored. */
  artifacts?: ArtifactRef[]
  /** User rejected this call at the approval prompt. Rendered struck-through
   *  and muted (declined ≠ failed) — status stays 'done', not 'error'. */
  denied?: boolean
  /** True while this call sits at an unresolved approval prompt. Rendered in
   *  the warning color; cleared when the approval resolves or a result lands. */
  awaitingApproval?: boolean
  /** Approval gate bound to this concrete tool-call occurrence for timeline
   *  rendering. Hosts may keep approvals as independent ThreadItems; the
   *  UI-only timeline projection attaches the matching item by tool_call_id. */
  approval?: Approval
  timestamp: number
  displayInfo?: ToolDisplayInfo
  /** Nested tool calls (subagent inner calls). */
  children?: ToolCall[]
  /** ask_user: request id while awaiting an answer (live runs only). */
  askUserId?: string
  /** ask_user: backend-normalized questions to render. */
  askUserQuestions?: AskUserQuestion[]
  /** Dual-channel streams (execute). */
  streams?: ToolStreams
  /** Dual-channel meta (execute). */
  meta?: ToolMeta
  /** Dual-channel presentation (execute). */
  presentation?: ToolPresentation
  /** Concurrent-batch id — tools issued together by one assistant message
   *  share it and coalesce into a `ToolBatchGroup` row stack. */
  batchId?: string
  /** 0-based position within the batch. */
  batchIndex?: number
  /** Total number of tools in the batch. */
  batchSize?: number
  /** Wall-clock start (unix ms) — drives the live elapsed badge while running. */
  startedAt?: number
}

/**
 * A UI-only group of ADJACENT tool calls in the timeline (adjacent = no
 * assistant/user message in between; approvals do NOT break adjacency and
 * render in place). Collapsed (all members settled) it shows one category-
 * count header line; expanded it is a bordered row-stack card whose rows
 * expand in place to each tool's registry-rendered body. Supersedes the
 * `exploring` and `batch` kinds. Does not change model-facing boundaries.
 */
export interface ActivityGroup {
  id: string
  tools: ToolCall[]
  status: ToolStatus
  /** True when ALL tools are read-only (per `isCollapsibleTool`). */
  explorative: boolean
}

/**
 * A UI-only coalesced group of collapsible read/search/list tool calls.
 * Does not change model-facing tool boundaries.
 * @deprecated Superseded by {@link ActivityGroup} (`'activity'` items). Kept
 * for external consumers that still feed `'exploring'` items to `Thread`.
 */
export interface ExploringGroup {
  id: string
  tools: ToolCall[]
  status: ToolStatus
}

/**
 * A UI-only group of tool calls issued concurrently by one assistant message
 * (same `batchId`). Rendered as a stacked status-row list; when every member
 * is a collapsible read/search/list tool (`explorative`) it renders as an
 * upgraded Exploring card instead. Does not change model-facing boundaries.
 * @deprecated Superseded by {@link ActivityGroup} (`'activity'` items) — batch
 * members now coalesce into activity groups. Kept for external consumers that
 * still feed `'batch'` items to `Thread`.
 */
export interface ToolBatchGroup {
  id: string
  batchId: string
  tools: ToolCall[]
  status: ToolStatus
  /** True when ALL tools are collapsible (read/search/list). */
  explorative: boolean
}

/** An option in an `ask_user` question. */
export interface AskUserOption {
  label: string
  description?: string
}

/** A single interactive question posed by the agent mid-run. */
export interface AskUserQuestion {
  question: string
  header?: string
  options?: AskUserOption[]
  multi_select?: boolean
}

/** A resolved `ask_user` answer (mirrors backend AskUserAnswer). */
export interface AskUserAnswer {
  question_header: string
  /** single-select label or free text. */
  answer: string
  /** multi-select labels. */
  selected?: string[]
}

/** Button treatment for a host-defined approval option. `allow_always` keeps
 *  the two-step arming UX; `custom` renders as a neutral choice. */
export type ApprovalOptionKind = 'allow_once' | 'allow_always' | 'deny' | 'custom'

/** A host-defined approval decision (e.g. an ACP permission option). The `id`
 *  is echoed back verbatim via `resolveApprovalOption`. */
export interface ApprovalOption {
  id: string
  label: string
  kind?: ApprovalOptionKind
  description?: string
}

/** Safe, bounded summary for billable external approvals. Full prompt/tool
 * args deliberately stay out of the approval DOM. */
export interface BillableApprovalSummary {
  /** Stable provider capability key (for example image.generate or web.search). */
  capability?: string
  provider?: string
  model?: string
  size?: string
  aspect_ratio?: string
  resolution?: string
  count?: number
  billable?: boolean
  has_reference?: boolean
}

/**
 * A pending approval gate. While `resolved` is falsy the UI shows the decision
 * controls; once resolved it collapses to an inline note.
 *
 * Two shapes: the classic boolean contract (allow once / allow all / deny), or
 * host-defined `options` (arbitrary ids, e.g. ACP permission_request) — when
 * `options` is present the UI renders one control per option instead.
 */
export interface Approval {
  id: string
  tool_name: string
  tool_args: string
  /** Backend tool_call_id of the gated call — lets the host mark the exact
   *  pending tool row as awaiting approval (warning color). */
  tool_call_id?: string
  /** Target outside the workspace root — UI flags it prominently. */
  is_external: boolean
  /** Policy class supplied by the backend (e.g. billable_external). */
  approvalClass?: string
  /** Structured, non-secret summary for billable approval copy. */
  billableSummary?: BillableApprovalSummary
  resolved?: boolean
  approved?: boolean
  /** True while a resolve request is in flight (disables controls). */
  resolving?: boolean
  /** Host-defined decision options; absent → classic boolean controls. */
  options?: ApprovalOption[]
  /** The chosen option id once resolved (options mode). */
  resolvedOptionId?: string
}

/**
 * One changed file inside a turn-changes summary. `added`/`removed` are
 * client-derived line counts (absent when the tool args carry no diff text);
 * `tool` is the LAST call that touched the file, kept so the UI can expand
 * its registry-rendered diff body.
 */
export interface TurnFileChange {
  path: string
  added?: number
  removed?: number
  tool: ToolCall
}

/**
 * A UI-only per-turn summary of file changes (opencode SessionTurn-style):
 * "Changed N files (+A −R)" inserted at the end of a completed turn.
 * `files` holds up to the display cap; `overflow` the rest ("… N more").
 */
export interface TurnChangesSummary {
  id: string
  /** Total distinct files changed this turn (files + overflow). */
  fileCount: number
  files: TurnFileChange[]
  overflow: TurnFileChange[]
  totalAdded: number
  totalRemoved: number
  /** True when at least one file has derived ± line counts. */
  hasLineCounts: boolean
}

/**
 * A completed user turn projected into one collapsible timeline row. The user
 * message remains outside this item; `activity` contains the intermediate
 * assistant/tool/approval work, while `summary` is the final assistant reply
 * that always stays visible. This is UI-only and does not alter transcript or
 * model-facing message boundaries.
 */
export interface CompletedTurn {
  id: string
  activity: ThreadItem[]
  summary: Message
  durationMs: number
}

/** Built-in thread-item kinds (activity/turn/exploring/batch/turnchanges are UI-only coalescing). */
export type ThreadItemKind =
  | 'message'
  | 'tool'
  | 'approval'
  | 'activity'
  | 'turn'
  | 'exploring'
  | 'batch'
  | 'turnchanges'

/**
 * The discriminated union rendered by `Thread`. A `seq` counter keeps DOM
 * identity stable across streaming updates and is used as the virtualizer key.
 */
export type ThreadItem =
  | { kind: 'message'; data: Message; seq: number }
  | { kind: 'tool'; data: ToolCall; seq: number }
  | { kind: 'approval'; data: Approval; seq: number }
  | { kind: 'activity'; data: ActivityGroup; seq: number }
  | { kind: 'turn'; data: CompletedTurn; seq: number }
  | { kind: 'exploring'; data: ExploringGroup; seq: number }
  | { kind: 'batch'; data: ToolBatchGroup; seq: number }
  | { kind: 'turnchanges'; data: TurnChangesSummary; seq: number }

/** Type guard helpers (kept generic so consumers can narrow item arrays). */
export function isMessageItem(i: ThreadItem): i is Extract<ThreadItem, { kind: 'message' }> {
  return i.kind === 'message'
}
export function isToolItem(i: ThreadItem): i is Extract<ThreadItem, { kind: 'tool' }> {
  return i.kind === 'tool'
}
export function isApprovalItem(i: ThreadItem): i is Extract<ThreadItem, { kind: 'approval' }> {
  return i.kind === 'approval'
}
export function isActivityItem(i: ThreadItem): i is Extract<ThreadItem, { kind: 'activity' }> {
  return i.kind === 'activity'
}
export function isTurnItem(i: ThreadItem): i is Extract<ThreadItem, { kind: 'turn' }> {
  return i.kind === 'turn'
}
export function isExploringItem(i: ThreadItem): i is Extract<ThreadItem, { kind: 'exploring' }> {
  return i.kind === 'exploring'
}
export function isBatchItem(i: ThreadItem): i is Extract<ThreadItem, { kind: 'batch' }> {
  return i.kind === 'batch'
}
export function isTurnChangesItem(i: ThreadItem): i is Extract<ThreadItem, { kind: 'turnchanges' }> {
  return i.kind === 'turnchanges'
}

/** A message composed while the agent is running; drained turn-by-turn. */
export interface QueuedMessage {
  id: string
  text: string
  images?: ChatImage[]
}

/** Live token/context usage snapshot for a turn. */
export interface TokenSnapshot {
  total_tokens: number
  prompt_tokens: number
  completion_tokens: number
  cached_tokens?: number
  reasoning_tokens?: number
  cache_write_tokens?: number
  call_count?: number
  cache_hit_rate?: number
  cache_supported?: boolean
  model_context_limit: number
}

/** Per-task context-window breakdown (host-provided; powers the ContextBar popover). */
export interface TaskContextBreakdown {
  context_limit: number
  system_prompt_tokens: number
  system_tools_tokens: number
  mcp_tools_tokens: number
  skills_tokens: number
  messages_tokens: number
}

/** A todo/goal tracking item. (id is a number — matches the Go backend.) */
export interface TodoItem {
  id: number
  title: string
  status: 'pending' | 'in_progress' | 'completed' | 'cancelled'
}

export type GoalStatus = 'active' | 'complete' | 'blocked'

/** An active agent goal (set via /goal or a dedicated control). */
export interface Goal {
  objective: string
  status: GoalStatus
  tokens_used?: number
  created_at?: number
  updated_at?: number
}
