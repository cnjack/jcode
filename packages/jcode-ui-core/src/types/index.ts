/**
 * Core message + tool types for jcode-ui.
 *
 * These mirror the jcode backend contract (see `web/src/types/api.ts`) but are
 * framework-agnostic and the single source of truth for both `jcode-ui-core`
 * (headless primitives) and `jcode-ui` (styled components).
 */

/** Who authored a message. */
export type Role = 'user' | 'assistant' | 'system'

/** A base64-encoded image attached to a message (no `data:` prefix). */
export interface ChatImage {
  data: string
  media_type: string
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
  /** Origin channel for inbound messages (e.g. 'wechat'). Drives avatar tint. */
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
}

export type ToolStatus = 'running' | 'done' | 'error'

/**
 * A tool invocation. `args`/`output` are raw JSON strings; renderers parse
 * them. `children` carries subagent-nested calls (rendered recursively).
 */
export interface ToolCall {
  id: string
  /** Backend tool_call_id for precise result matching. */
  toolCallID?: string
  name: string
  args: string
  output?: string
  /** Clean output for UI display (metadata stripped). */
  displayOutput?: string
  error?: string
  status: ToolStatus
  timestamp: number
  displayInfo?: ToolDisplayInfo
  /** Nested tool calls (subagent inner calls). */
  children?: ToolCall[]
  /** ask_user: request id while awaiting an answer (live runs only). */
  askUserId?: string
  /** ask_user: backend-normalized questions to render. */
  askUserQuestions?: AskUserQuestion[]
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

/**
 * A pending approval gate. While `resolved` is falsy the UI shows the decision
 * controls; once resolved it collapses to an inline note.
 */
export interface Approval {
  id: string
  tool_name: string
  tool_args: string
  /** Target outside the workspace root — UI flags it prominently. */
  is_external: boolean
  resolved?: boolean
  approved?: boolean
  /** True while a resolve request is in flight (disables controls). */
  resolving?: boolean
}

/** The three built-in thread-item kinds. */
export type ThreadItemKind = 'message' | 'tool' | 'approval'

/**
 * The discriminated union rendered by `Thread`. A `seq` counter keeps DOM
 * identity stable across streaming updates and is used as the virtualizer key.
 */
export type ThreadItem =
  | { kind: 'message'; data: Message; seq: number }
  | { kind: 'tool'; data: ToolCall; seq: number }
  | { kind: 'approval'; data: Approval; seq: number }

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
