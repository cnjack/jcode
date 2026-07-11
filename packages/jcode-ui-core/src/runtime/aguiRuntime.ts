/**
 * createAGUIRuntime — drive jcode-ui from any AG-UI protocol backend.
 *
 * An AG-UI server (LangGraph, CrewAI, Mastra, …) emits a normalized event stream;
 * this reducer folds that stream into `RuntimeState` so those backends need zero
 * glue to render in jcode-ui. It is the AG-UI sibling of `mockRuntime` /
 * `externalStore`: same `{ getState, subscribe, actions }` contract, same
 * stable-snapshot discipline that `context.tsx` requires from `getState`.
 *
 * Design notes (constraints not obvious from the code):
 * - Snapshot stability: `getState()` returns the SAME object reference until a
 *   real change, or `useSyncExternalStore` loops. We rebuild the snapshot only
 *   inside the batched flush.
 * - Notify batching: many events arrive per microtask (START/CONTENT/END for one
 *   token burst). We coalesce listener notifications with `queueMicrotask`, like
 *   externalStore leans on the host store's single post-dispatch notification.
 * - Item indices: `msgIndex`/`toolIndex` map ids → positions in `items`. Only
 *   append + full MESSAGES_SNAPSHOT rebuild ever run, so appended indices stay
 *   valid; immutable per-item replacement preserves positions.
 * - Agent state lives OUTSIDE RuntimeState (it is arbitrary backend JSON, not a
 *   chat concept) and is read via `getAgentState()`.
 */

import type { ChatRuntime, RuntimeActions, RuntimeState } from './index.js'
import type { ConnectionState, Message, Role, ThreadItem, ToolCall } from '../types/index.js'
import type {
  AGUIEvent,
  AGUIMessage,
  AGUIPatchOp,
  AGUIRunInput,
  AGUITransport,
} from './aguiEvents.js'
import { createFetchTransport } from './aguiEvents.js'

export interface AGUIRuntimeOptions {
  /** AG-UI run endpoint (POST, streams `text/event-stream`). */
  url: string
  /** Extra request headers (auth, etc.) for the default transport. */
  headers?: Record<string, string>
  /** Override the event source — inject a scripted stream in tests, or swap in
   *  WebSocket/other transports. Defaults to `createFetchTransport(url, headers)`. */
  transport?: AGUITransport
  /** Stable thread id for the whole session. Auto-generated when omitted. */
  threadId?: string
}

/** The AG-UI runtime adds read-only agent-state access to the base contract. */
export interface AGUIRuntime extends ChatRuntime {
  /** The latest STATE_SNAPSHOT with STATE_DELTA patches applied, or undefined. */
  getAgentState: () => unknown
}

let idSeed = 0
function genId(prefix: string): string {
  return `${prefix}_${Date.now().toString(36)}_${(idSeed++).toString(36)}`
}

/** AG-UI roles collapse onto jcode's three: unknown/tool/developer → system. */
function toRole(role: string | undefined): Role {
  if (role === 'user') return 'user'
  if (role === 'assistant') return 'assistant'
  return 'system'
}

/** Tool output is a raw JSON string in jcode; pass strings through, encode rest. */
function stringifyContent(content: unknown): string {
  if (typeof content === 'string') return content
  try {
    return JSON.stringify(content)
  } catch {
    return String(content)
  }
}

// --- RFC 6902 JSON Pointer (minimal: add / replace / remove) ----------------

function parsePointer(pointer: string): string[] {
  if (pointer === '' || pointer === '/') return []
  return pointer
    .split('/')
    .slice(1)
    .map((t) => t.replace(/~1/g, '/').replace(/~0/g, '~'))
}

/** Apply one op onto `doc` in place, returning the (possibly new root) doc. */
function applyOp(doc: unknown, op: AGUIPatchOp): unknown {
  const tokens = parsePointer(op.path)
  const remove = op.op === 'remove'
  // Root replacement.
  if (tokens.length === 0) return remove ? undefined : op.value

  let parent = doc as Record<string, unknown>
  for (let i = 0; i < tokens.length - 1; i++) {
    const t = tokens[i]
    const next = (parent as Record<string, unknown>)[t]
    if (next === undefined || next === null || typeof next !== 'object') {
      ;(parent as Record<string, unknown>)[t] = {}
    }
    parent = (parent as Record<string, unknown>)[t] as Record<string, unknown>
  }

  const last = tokens[tokens.length - 1]
  if (remove) {
    if (Array.isArray(parent)) parent.splice(Number(last), 1)
    else delete parent[last]
  } else if (Array.isArray(parent)) {
    if (last === '-') parent.push(op.value)
    else parent[Number(last)] = op.value
  } else {
    parent[last] = op.value
  }
  return doc
}

/** add/replace/remove only; move/copy/test are ignored (documented limitation). */
function applyJsonPatch(base: unknown, ops: AGUIPatchOp[]): unknown {
  let doc: unknown =
    base === undefined || base === null ? {} : structuredClone(base)
  for (const op of ops) {
    if (op.op === 'add' || op.op === 'replace' || op.op === 'remove') {
      doc = applyOp(doc, op)
    }
  }
  return doc
}

export function createAGUIRuntime(options: AGUIRuntimeOptions): AGUIRuntime {
  const transport = options.transport ?? createFetchTransport(options.url, options.headers)
  const threadId = options.threadId ?? genId('thread')

  // --- mutable model ---
  let items: ThreadItem[] = []
  let isRunning = false
  let agentState: unknown
  /** Reflects transport health: flips to 'disconnected' on a non-abort failure. */
  let connection: ConnectionState = 'connected'
  let seq = 0
  /** Reasoning deltas accumulated before the assistant text message exists. */
  let pendingReasoning = ''
  const msgIndex = new Map<string, number>()
  const toolIndex = new Map<string, number>()

  // --- subscription + batched snapshot ---
  const listeners = new Set<() => void>()
  let snapshot: RuntimeState = buildSnapshot()
  let scheduled = false

  function buildSnapshot(): RuntimeState {
    // AG-UI has no token/goal/todo/queue channel; those slices stay defaulted.
    return { items, isRunning, tokenSnapshot: null, goal: null, todos: [], queued: [], connection }
  }
  function markDirty(): void {
    if (scheduled) return
    scheduled = true
    queueMicrotask(() => {
      scheduled = false
      snapshot = buildSnapshot()
      for (const l of listeners) l()
    })
  }

  function pushItem(item: ThreadItem): number {
    const idx = items.length
    items = [...items, item]
    markDirty()
    return idx
  }
  function updateAt(idx: number, next: ThreadItem): void {
    const copy = items.slice()
    copy[idx] = next
    items = copy
    markDirty()
  }
  function setRunning(running: boolean): void {
    if (isRunning === running) return
    isRunning = running
    markDirty()
  }

  function ensureAssistantMessage(messageId: string, role?: string): number {
    const existing = msgIndex.get(messageId)
    if (existing !== undefined) return existing
    const reasoning = pendingReasoning || undefined
    pendingReasoning = ''
    const msg: Message = {
      id: messageId,
      role: toRole(role ?? 'assistant'),
      content: '',
      timestamp: Date.now(),
      reasoning,
    }
    const idx = pushItem({ kind: 'message', seq: ++seq, data: msg })
    msgIndex.set(messageId, idx)
    return idx
  }

  function appendMessageText(messageId: string, delta: string): void {
    const idx = ensureAssistantMessage(messageId)
    const it = items[idx]
    if (it.kind !== 'message') return
    updateAt(idx, { ...it, data: { ...it.data, content: it.data.content + delta } })
  }

  function ensureToolCall(toolCallId: string, name: string): number {
    const existing = toolIndex.get(toolCallId)
    if (existing !== undefined) return existing
    const tc: ToolCall = {
      id: toolCallId,
      toolCallID: toolCallId,
      name,
      args: '',
      status: 'running',
      timestamp: Date.now(),
    }
    const idx = pushItem({ kind: 'tool', seq: ++seq, data: tc })
    toolIndex.set(toolCallId, idx)
    return idx
  }

  function patchTool(toolCallId: string, patch: Partial<ToolCall>): void {
    const idx = toolIndex.get(toolCallId)
    if (idx === undefined) return
    const it = items[idx]
    if (it.kind !== 'tool') return
    updateAt(idx, { ...it, data: { ...it.data, ...patch } })
  }

  function pushSystemError(message: string, detail?: string): void {
    const data: Message = {
      id: genId('err'),
      role: 'system',
      content: message,
      timestamp: Date.now(),
      level: 'error',
      detail,
    }
    pushItem({ kind: 'message', seq: ++seq, data })
  }

  /** Rebuild the whole timeline from an authoritative message list. */
  function rebuildFromMessages(messages: AGUIMessage[]): void {
    const next: ThreadItem[] = []
    msgIndex.clear()
    toolIndex.clear()

    for (const m of messages) {
      if (m.role === 'tool') {
        const out = stringifyContent(m.content ?? '')
        let attached = false
        if (m.toolCallId) {
          for (let i = 0; i < next.length; i++) {
            const it = next[i]
            if (it.kind === 'tool' && it.data.id === m.toolCallId) {
              next[i] = { ...it, data: { ...it.data, status: 'done', output: out, displayOutput: out } }
              attached = true
              break
            }
          }
        }
        if (!attached) {
          const tc: ToolCall = {
            id: m.toolCallId ?? genId('tool'),
            toolCallID: m.toolCallId,
            name: m.name ?? 'tool',
            args: '',
            status: 'done',
            output: out,
            displayOutput: out,
            timestamp: Date.now(),
          }
          next.push({ kind: 'tool', seq: ++seq, data: tc })
          toolIndex.set(tc.id, next.length - 1)
        }
        continue
      }

      const content = typeof m.content === 'string' ? m.content : ''
      // Skip empty assistant shells that only exist to carry tool calls.
      if (content.length > 0 || m.role !== 'assistant') {
        const msg: Message = { id: m.id, role: toRole(m.role), content, timestamp: Date.now() }
        next.push({ kind: 'message', seq: ++seq, data: msg })
        msgIndex.set(m.id, next.length - 1)
      }
      if (m.role === 'assistant' && Array.isArray(m.toolCalls)) {
        for (const call of m.toolCalls) {
          const tc: ToolCall = {
            id: call.id,
            toolCallID: call.id,
            name: call.function?.name ?? 'tool',
            args: call.function?.arguments ?? '',
            status: 'done',
            timestamp: Date.now(),
          }
          next.push({ kind: 'tool', seq: ++seq, data: tc })
          toolIndex.set(tc.id, next.length - 1)
        }
      }
    }

    items = next
    markDirty()
  }

  // --- the reducer ---
  function handleEvent(ev: AGUIEvent): void {
    switch (ev.type) {
      case 'RUN_STARTED':
        setRunning(true)
        break
      case 'RUN_FINISHED':
        setRunning(false)
        break
      case 'RUN_ERROR':
        pushSystemError(ev.message, ev.code)
        setRunning(false)
        break

      case 'TEXT_MESSAGE_START':
        ensureAssistantMessage(ev.messageId, ev.role)
        break
      case 'TEXT_MESSAGE_CONTENT':
        appendMessageText(ev.messageId, ev.delta)
        break
      case 'TEXT_MESSAGE_CHUNK':
        if (ev.messageId && ev.delta) appendMessageText(ev.messageId, ev.delta)
        break
      case 'TEXT_MESSAGE_END':
        // Streaming boundary marker; content already applied.
        break

      case 'TOOL_CALL_START':
        ensureToolCall(ev.toolCallId, ev.toolCallName)
        break
      case 'TOOL_CALL_ARGS': {
        const idx = ensureToolCall(ev.toolCallId, 'tool')
        const it = items[idx]
        if (it.kind === 'tool') patchTool(ev.toolCallId, { args: it.data.args + ev.delta })
        break
      }
      case 'TOOL_CALL_CHUNK': {
        if (!ev.toolCallId) break
        ensureToolCall(ev.toolCallId, ev.toolCallName ?? 'tool')
        if (ev.delta) {
          const it = items[toolIndex.get(ev.toolCallId)!]
          if (it.kind === 'tool') patchTool(ev.toolCallId, { args: it.data.args + ev.delta })
        }
        break
      }
      case 'TOOL_CALL_END':
        patchTool(ev.toolCallId, { status: 'done' })
        break
      case 'TOOL_CALL_RESULT': {
        const out = stringifyContent(ev.content)
        const idx = toolIndex.get(ev.toolCallId)
        if (idx === undefined) {
          ensureToolCall(ev.toolCallId, 'tool')
        }
        patchTool(ev.toolCallId, { status: 'done', output: out, displayOutput: out })
        break
      }

      case 'STATE_SNAPSHOT':
        agentState = ev.snapshot
        markDirty()
        break
      case 'STATE_DELTA':
        agentState = applyJsonPatch(agentState, ev.delta)
        markDirty()
        break
      case 'MESSAGES_SNAPSHOT':
        rebuildFromMessages(ev.messages)
        break

      case 'REASONING_MESSAGE_CONTENT':
      case 'REASONING_MESSAGE_CHUNK':
        // Buffer reasoning until the assistant text message it belongs to opens.
        if (ev.delta) pendingReasoning += ev.delta
        break

      // STEP_*, REASONING lifecycle markers, RAW, CUSTOM, and any future/
      // unmodelled types are intentionally not mapped onto RuntimeState.
      default:
        break
    }
  }

  // --- run driving ---
  let controller: AbortController | null = null

  function outgoingMessages(): AGUIMessage[] {
    // Reconstruct the text-message history for stateless backends. Tool-call
    // history is left to the backend / MESSAGES_SNAPSHOT (see report).
    const out: AGUIMessage[] = []
    for (const it of items) {
      if (it.kind === 'message') {
        out.push({ id: it.data.id, role: it.data.role, content: it.data.content })
      }
    }
    return out
  }

  async function runLoop(input: AGUIRunInput): Promise<void> {
    const ac = new AbortController()
    controller = ac
    setRunning(true)
    try {
      for await (const ev of transport(input, ac.signal)) {
        handleEvent(ev)
      }
    } catch (err) {
      if (!ac.signal.aborted) {
        connection = 'disconnected'
        pushSystemError(err instanceof Error ? err.message : String(err))
      }
    } finally {
      if (controller === ac) controller = null
      setRunning(false)
    }
  }

  const actions: RuntimeActions = {
    sendMessage: (text, images) => {
      const data: Message = {
        id: genId('user'),
        role: 'user',
        content: text,
        timestamp: Date.now(),
        images: images?.map((i) => ({ data: i.data, media_type: i.media_type })),
      }
      pushItem({ kind: 'message', seq: ++seq, data })
      // A fresh send is our best signal the endpoint is reachable again.
      connection = 'connected'
      const input: AGUIRunInput = {
        threadId,
        runId: genId('run'),
        messages: outgoingMessages(),
        state: agentState ?? {},
        tools: [],
        context: [],
        forwardedProps: {},
      }
      void runLoop(input).catch(() => {})
    },
    stop: () => {
      controller?.abort()
      setRunning(false)
    },
    // AG-UI has no client-side queue/approval/ask_user/edit channel in this
    // adapter; kept present with full types so the UI never crashes calling them.
    enqueueMessage: () => {},
    removeQueuedMessage: () => {},
    resolveApproval: () => {},
    submitAskUser: () => {},
    editMessage: () => {},
  }

  return {
    getState: () => snapshot,
    subscribe: (l) => {
      listeners.add(l)
      return () => listeners.delete(l)
    },
    actions,
    getAgentState: () => agentState,
  }
}
