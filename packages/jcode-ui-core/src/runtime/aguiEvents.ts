/**
 * AG-UI protocol event types + the default SSE transport.
 *
 * Event set: the AG-UI protocol `EventType` enum as documented at
 * https://docs.ag-ui.com/sdk/js/core/events (fetched 2026-07-11). The enum has
 * 26 members; this adapter maps the streaming-chat subset and safely ignores the
 * rest (see `aguiRuntime.ts`). The deprecated `THINKING_*` events are treated as
 * aliases of their `REASONING_*` replacements.
 *
 * Wire format is Server-Sent Events: each event is a JSON object delivered on one
 * or more `data:` lines, terminated by a blank line. We hand-roll the parser so
 * the package stays dependency-free (no `eventsource`, no `@ag-ui/*`).
 */

/** String tags of the AG-UI `EventType` enum (values equal the wire `type`). */
export const AGUIEventType = {
  RUN_STARTED: 'RUN_STARTED',
  RUN_FINISHED: 'RUN_FINISHED',
  RUN_ERROR: 'RUN_ERROR',
  STEP_STARTED: 'STEP_STARTED',
  STEP_FINISHED: 'STEP_FINISHED',
  TEXT_MESSAGE_START: 'TEXT_MESSAGE_START',
  TEXT_MESSAGE_CONTENT: 'TEXT_MESSAGE_CONTENT',
  TEXT_MESSAGE_END: 'TEXT_MESSAGE_END',
  TEXT_MESSAGE_CHUNK: 'TEXT_MESSAGE_CHUNK',
  TOOL_CALL_START: 'TOOL_CALL_START',
  TOOL_CALL_ARGS: 'TOOL_CALL_ARGS',
  TOOL_CALL_END: 'TOOL_CALL_END',
  TOOL_CALL_RESULT: 'TOOL_CALL_RESULT',
  TOOL_CALL_CHUNK: 'TOOL_CALL_CHUNK',
  STATE_SNAPSHOT: 'STATE_SNAPSHOT',
  STATE_DELTA: 'STATE_DELTA',
  MESSAGES_SNAPSHOT: 'MESSAGES_SNAPSHOT',
  REASONING_START: 'REASONING_START',
  REASONING_END: 'REASONING_END',
  REASONING_MESSAGE_START: 'REASONING_MESSAGE_START',
  REASONING_MESSAGE_CONTENT: 'REASONING_MESSAGE_CONTENT',
  REASONING_MESSAGE_END: 'REASONING_MESSAGE_END',
  REASONING_MESSAGE_CHUNK: 'REASONING_MESSAGE_CHUNK',
  RAW: 'RAW',
  CUSTOM: 'CUSTOM',
} as const

/** AG-UI role space. Not all map onto jcode's `Role` (see `toRole`). */
export type AGUIRole = 'developer' | 'system' | 'assistant' | 'user' | 'tool'

/** A tool call embedded in an AG-UI assistant message (OpenAI-shaped). */
export interface AGUIToolCall {
  id: string
  type?: string
  function: { name: string; arguments: string }
}

/** An AG-UI conversation message (as sent in run input / MESSAGES_SNAPSHOT). */
export interface AGUIMessage {
  id: string
  role: AGUIRole | string
  content?: string | null
  /** Tool name (present on `role: 'tool'` result messages). */
  name?: string
  /** Links a `role: 'tool'` message to the tool call it answers. */
  toolCallId?: string
  /** Present on assistant messages that invoke tools. */
  toolCalls?: AGUIToolCall[]
}

/** A single RFC 6902 JSON Patch operation (STATE_DELTA payload element). */
export interface AGUIPatchOp {
  op: 'add' | 'replace' | 'remove' | 'move' | 'copy' | 'test' | string
  path: string
  value?: unknown
  from?: string
}

/** Fields shared by every event. */
interface AGUIBaseEvent {
  type: string
  timestamp?: number
  rawEvent?: unknown
}

// Lifecycle -----------------------------------------------------------------
export interface RunStartedEvent extends AGUIBaseEvent {
  type: 'RUN_STARTED'
  threadId?: string
  runId?: string
}
export interface RunFinishedEvent extends AGUIBaseEvent {
  type: 'RUN_FINISHED'
  result?: unknown
}
export interface RunErrorEvent extends AGUIBaseEvent {
  type: 'RUN_ERROR'
  message: string
  code?: string
}
export interface StepStartedEvent extends AGUIBaseEvent {
  type: 'STEP_STARTED'
  stepName: string
}
export interface StepFinishedEvent extends AGUIBaseEvent {
  type: 'STEP_FINISHED'
  stepName: string
}

// Text messages -------------------------------------------------------------
export interface TextMessageStartEvent extends AGUIBaseEvent {
  type: 'TEXT_MESSAGE_START'
  messageId: string
  role?: string
}
export interface TextMessageContentEvent extends AGUIBaseEvent {
  type: 'TEXT_MESSAGE_CONTENT'
  messageId: string
  delta: string
}
export interface TextMessageEndEvent extends AGUIBaseEvent {
  type: 'TEXT_MESSAGE_END'
  messageId: string
}
export interface TextMessageChunkEvent extends AGUIBaseEvent {
  type: 'TEXT_MESSAGE_CHUNK'
  messageId?: string
  role?: string
  delta?: string
}

// Tool calls ----------------------------------------------------------------
export interface ToolCallStartEvent extends AGUIBaseEvent {
  type: 'TOOL_CALL_START'
  toolCallId: string
  toolCallName: string
  parentMessageId?: string
}
export interface ToolCallArgsEvent extends AGUIBaseEvent {
  type: 'TOOL_CALL_ARGS'
  toolCallId: string
  delta: string
}
export interface ToolCallEndEvent extends AGUIBaseEvent {
  type: 'TOOL_CALL_END'
  toolCallId: string
}
export interface ToolCallResultEvent extends AGUIBaseEvent {
  type: 'TOOL_CALL_RESULT'
  messageId?: string
  toolCallId: string
  content: unknown
  role?: string
}
export interface ToolCallChunkEvent extends AGUIBaseEvent {
  type: 'TOOL_CALL_CHUNK'
  toolCallId?: string
  toolCallName?: string
  parentMessageId?: string
  delta?: string
}

// State ---------------------------------------------------------------------
export interface StateSnapshotEvent extends AGUIBaseEvent {
  type: 'STATE_SNAPSHOT'
  snapshot: unknown
}
export interface StateDeltaEvent extends AGUIBaseEvent {
  type: 'STATE_DELTA'
  delta: AGUIPatchOp[]
}
export interface MessagesSnapshotEvent extends AGUIBaseEvent {
  type: 'MESSAGES_SNAPSHOT'
  messages: AGUIMessage[]
}

// Reasoning (supersedes THINKING_*) -----------------------------------------
export interface ReasoningMessageContentEvent extends AGUIBaseEvent {
  type: 'REASONING_MESSAGE_CONTENT'
  messageId?: string
  delta: string
}
export interface ReasoningMessageChunkEvent extends AGUIBaseEvent {
  type: 'REASONING_MESSAGE_CHUNK'
  messageId?: string
  delta?: string
}

// Passthrough ---------------------------------------------------------------
export interface CustomEvent extends AGUIBaseEvent {
  type: 'CUSTOM'
  name: string
  value: unknown
}
export interface RawEvent extends AGUIBaseEvent {
  type: 'RAW'
  event: unknown
  source?: string
}

/**
 * The discriminated union the reducer switches on. Unmodelled `type`s (STEP_*,
 * REASONING lifecycle markers, ACTIVITY_*, deprecated THINKING_*) still arrive
 * as objects at runtime and fall through to the reducer's default branch.
 */
export type AGUIEvent =
  | RunStartedEvent
  | RunFinishedEvent
  | RunErrorEvent
  | StepStartedEvent
  | StepFinishedEvent
  | TextMessageStartEvent
  | TextMessageContentEvent
  | TextMessageEndEvent
  | TextMessageChunkEvent
  | ToolCallStartEvent
  | ToolCallArgsEvent
  | ToolCallEndEvent
  | ToolCallResultEvent
  | ToolCallChunkEvent
  | StateSnapshotEvent
  | StateDeltaEvent
  | MessagesSnapshotEvent
  | ReasoningMessageContentEvent
  | ReasoningMessageChunkEvent
  | CustomEvent
  | RawEvent

/** The POST body sent to open a run (AG-UI `RunAgentInput`, trimmed). */
export interface AGUIRunInput {
  threadId: string
  runId: string
  messages: AGUIMessage[]
  state?: unknown
  tools?: unknown[]
  context?: unknown[]
  forwardedProps?: unknown
}

/**
 * A pluggable event source. The default (`createFetchTransport`) POSTs the run
 * input and streams the SSE response; tests inject a scripted async iterable.
 */
export type AGUITransport = (
  input: AGUIRunInput,
  signal: AbortSignal,
) => AsyncIterable<AGUIEvent>

/**
 * Parse one SSE event block into an AG-UI event. Joins multiple `data:` lines,
 * skips comments/other fields, and returns null for keep-alives, `[DONE]`
 * sentinels, or unparseable JSON (so a single bad frame can't kill the stream).
 */
function parseSSEBlock(block: string): AGUIEvent | null {
  const dataLines: string[] = []
  for (const line of block.split('\n')) {
    if (line === '' || line.startsWith(':')) continue
    if (line.startsWith('data:')) {
      // A single leading space after the colon is part of the SSE framing.
      dataLines.push(line.slice(5).replace(/^ /, ''))
    }
  }
  if (dataLines.length === 0) return null
  const payload = dataLines.join('\n')
  if (payload === '[DONE]') return null
  try {
    return JSON.parse(payload) as AGUIEvent
  } catch {
    return null
  }
}

/**
 * Turn a byte stream of SSE frames into a stream of AG-UI events. Frame
 * boundaries are blank lines; CRLF is normalized to LF first so the same
 * splitter handles both line endings.
 */
export async function* parseSSEStream(
  body: ReadableStream<Uint8Array>,
): AsyncGenerator<AGUIEvent> {
  const reader = body.getReader()
  const decoder = new TextDecoder()
  let buf = ''
  try {
    for (;;) {
      const { done, value } = await reader.read()
      if (done) break
      buf += decoder.decode(value, { stream: true }).replace(/\r\n/g, '\n')
      let boundary = buf.indexOf('\n\n')
      while (boundary !== -1) {
        const ev = parseSSEBlock(buf.slice(0, boundary))
        if (ev) yield ev
        buf = buf.slice(boundary + 2)
        boundary = buf.indexOf('\n\n')
      }
    }
    const tail = (buf + decoder.decode()).trim()
    if (tail) {
      const ev = parseSSEBlock(tail)
      if (ev) yield ev
    }
  } finally {
    reader.releaseLock()
  }
}

/**
 * The default transport: HTTP POST + `text/event-stream`. Built from the
 * runtime's `url`/`headers` and closed over so the `AGUITransport` it returns
 * matches the `(input, signal)` shape tests use.
 */
export function createFetchTransport(
  url: string,
  headers?: Record<string, string>,
): AGUITransport {
  return async function* fetchTransport(input, signal) {
    const res = await fetch(url, {
      method: 'POST',
      headers: {
        'content-type': 'application/json',
        accept: 'text/event-stream',
        ...(headers ?? {}),
      },
      body: JSON.stringify(input),
      signal,
    })
    if (!res.ok) {
      throw new Error(`AG-UI run request failed: ${res.status} ${res.statusText}`)
    }
    if (!res.body) {
      throw new Error('AG-UI run response has no readable body')
    }
    yield* parseSSEStream(res.body)
  }
}
