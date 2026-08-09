/**
 * WebSocket client — ported from web/src/composables/ws.ts.
 *
 * Unlike the Vue version (a composable with onUnmounted), this is a module-level
 * singleton: created once at app boot, routes events to a handler set, and torn
 * down on app shutdown. The React app's WS event handler dispatches Redux
 * actions (replacing the Vue store-coupled wiring in App.vue).
 *
 * Dual-host contract: connects to wsBase()/api/ws, with the token (when present)
 * riding as a second WS subprotocol. Auto-reconnects on close (3s). 30s ping.
 * Drops events tagged with a task_id that isn't the active task.
 */

import { wsBase } from './apiBase'
import { getAuthToken } from './authToken'

export interface WSHandlers {
  onAgentStart?: () => void
  onAgentText?: (data: { text: string }) => void
  onToolCall?: (data: {
    name: string
    args: string
    tool_call_id?: string
    display_info?: { title: string; subtitle?: string; icon?: string; category?: string }
    surface?: import('jcode-ui-core').ToolSurface
    phase?: import('../app/toolLifecycle').WireToolPhase
    operation_id?: string
    /** Concurrent-batch grouping (tools issued together by one assistant message). */
    batch_id?: string
    /** 0-based position within the batch. */
    batch_index?: number
    batch_size?: number
    /** Wall-clock start (unix ms). */
    started_at?: number
  }) => void
  onToolProgress?: (data: {
    name?: string
    tool_call_id: string
    operation_id?: string
    phase: import('../app/toolLifecycle').WireToolPhase
    error_code?: string
    provider?: string
    model?: string
    artifacts?: import('jcode-ui-core').ArtifactRef[]
  }) => void
  onToolResult?: (data: {
    name: string
    output: string
    display_output?: string
    error?: string
    tool_call_id?: string
    /** Total tool duration (ms, approval wait subtracted) — merged into
     *  meta.duration_ms when absent there. */
    duration_ms?: number
    /** User rejected the call at the approval prompt (declined ≠ failed). */
    denied?: boolean
    streams?: import('./types').ToolResultStreams
    meta?: import('./types').ToolResultMeta
    presentation?: import('./types').ToolResultPresentation
    operation_id?: string
    phase?: import('../app/toolLifecycle').WireToolPhase
    outcome?: import('jcode-ui-core').ToolOutcome
    error_code?: string
    provider?: string
    model?: string
    artifacts?: import('jcode-ui-core').ArtifactRef[]
  }) => void
  onTokenUpdate?: (data: import('./types').TokenUpdateData) => void
  onAgentDone?: (data: { error?: string; detail?: string; stopped?: boolean; task_id?: string }) => void
  onTodoUpdate?: () => void
  onGoalUpdate?: (data: import('jcode-ui-core').Goal | null) => void
  onApprovalRequest?: (data: import('./types').ApprovalRequestData) => void
  onAskUserRequest?: (data: import('./types').AskUserRequestData) => void
  onSessionReset?: (data: { session_id: string }) => void
  onModelChanged?: (data: { provider: string; model: string }) => void
  onAgentChanged?: (data: { agent: string }) => void
  onModeChanged?: (data: { mode: string }) => void
  onApprovalModeChanged?: (data: { auto_approve: boolean }) => void
  onSubagentEvent?: (data: import('./types').SubagentEventData) => void
  onSubagentProgress?: (data: import('./types').SubagentProgressData) => void
  onUserMessage?: (data: { content: string; source: string }) => void
  onTaskStatus?: (taskId: string, running: boolean, project?: string, updatedAt?: string) => void
  onArtifactUpserted?: (data: {
    task_id: string
    id?: string
    artifact_id?: string
    focus?: boolean
  }) => void
  /** Fired when the socket opens/closes so the UI can show online/offline. */
  onConnectionChange?: (connected: boolean) => void
  /** Returns the task currently shown in the foreground. Events tagged with a
   *  DIFFERENT task id are dropped so they don't pollute the active view. */
  activeTaskId?: () => string | undefined
}

interface WSMessage {
  type: string
  task_id?: string
  data?: unknown
}

/** Event types whose data payload gets the envelope task_id merged in. */
const TASK_ID_DATA_TYPES = new Set(['approval_request', 'ask_user_request', 'agent_done', 'artifact_upserted'])
const BACKGROUND_EVENT_TYPES = new Set(['agent_done', 'artifact_upserted'])

export class WSClient {
  private ws: WebSocket | null = null
  private retryTimer: ReturnType<typeof setTimeout> | null = null
  private pingTimer: ReturnType<typeof setInterval> | null = null
  private connected = false
  private handlers: WSHandlers
  /** When true, onclose must not schedule a reconnect (intentional teardown /
   *  socket replacement). Without this, React StrictMode remount leaves a
   *  ghost client that also receives agent_text → doubled streaming text. */
  private closed = false
  /** Monotonic id so stale socket callbacks never touch a newer connection. */
  private gen = 0

  constructor(handlers: WSHandlers) {
    this.handlers = handlers
  }

  /** Update the handler set (e.g. when the active task changes). */
  setHandlers(handlers: WSHandlers): void {
    this.handlers = handlers
    this.handlerMap = null
  }

  /** True when the WS is open. */
  isConnected(): boolean {
    return this.connected
  }

  connect(): void {
    this.closed = false
    this.clearRetry()
    this.detachSocket() // drop any existing socket without auto-reconnect

    const token = getAuthToken()
    const gen = ++this.gen
    const ws = token
      ? new WebSocket(`${wsBase()}/api/ws`, ['jcode-auth', token])
      : new WebSocket(`${wsBase()}/api/ws`)
    this.ws = ws

    ws.onopen = () => {
      if (this.gen !== gen || this.ws !== ws) return
      this.connected = true
      this.handlers.onConnectionChange?.(true)
      if (this.pingTimer) clearInterval(this.pingTimer)
      this.pingTimer = setInterval(() => this.send({ type: 'ping' }), 30000)
    }

    ws.onmessage = (event) => {
      if (this.gen !== gen || this.ws !== ws) return
      try {
        const msg: WSMessage = JSON.parse(event.data)
        const active = this.handlers.activeTaskId?.()
        // Events tagged with a different task id are dropped so they don't
        // pollute the active view — EXCEPT agent_done, which the bridge needs
        // for every session to drain that session's type-ahead queue.
        if (msg.task_id && active && msg.task_id !== active && !BACKGROUND_EVENT_TYPES.has(msg.type)) return
        const handler = this.handlerFor(msg.type)
        if (handler) {
          let data = msg.data
          if (msg.task_id && TASK_ID_DATA_TYPES.has(msg.type)) {
            data = { ...((data && typeof data === 'object' ? data : {}) as Record<string, unknown>), task_id: msg.task_id }
          }
          handler(data)
        }
      } catch {
        // parse error — drop
      }
    }

    ws.onerror = () => {
      if (this.gen !== gen || this.ws !== ws) return
      this.connected = false
      this.handlers.onConnectionChange?.(false)
    }

    ws.onclose = () => {
      if (this.gen !== gen) return
      if (this.ws === ws) this.ws = null
      this.connected = false
      this.handlers.onConnectionChange?.(false)
      if (this.pingTimer) {
        clearInterval(this.pingTimer)
        this.pingTimer = null
      }
      // Only auto-reconnect unexpected drops — never after disconnect() or
      // when connect() replaced this socket.
      if (this.closed) return
      this.clearRetry()
      this.retryTimer = setTimeout(() => {
        if (!this.closed) this.connect()
      }, 3000)
    }
  }

  send(msg: WSMessage): void {
    if (this.ws && this.ws.readyState === WebSocket.OPEN) {
      this.ws.send(JSON.stringify(msg))
    }
  }

  sendApproval(id: string, approved: boolean, approveAll = false, taskId?: string): void {
    this.send({ type: 'approval', data: { id, approved, approve_all: approveAll, task_id: taskId } })
  }

  disconnect(): void {
    this.closed = true
    this.clearRetry()
    if (this.pingTimer) {
      clearInterval(this.pingTimer)
      this.pingTimer = null
    }
    this.detachSocket()
    this.connected = false
  }

  private clearRetry(): void {
    if (this.retryTimer) {
      clearTimeout(this.retryTimer)
      this.retryTimer = null
    }
  }

  /** Close the current socket without scheduling reconnect. */
  private detachSocket(): void {
    const ws = this.ws
    this.ws = null
    if (!ws) return
    // Null handlers first so the close event cannot re-enter connect().
    ws.onopen = null
    ws.onmessage = null
    ws.onerror = null
    ws.onclose = null
    try {
      ws.close()
    } catch {
      // ignore
    }
  }

  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  private handlerMap: Record<string, (data: any) => void> | null = null
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  private handlerFor(type: string): ((data: any) => void) | undefined {
    if (!this.handlerMap) {
      const h = this.handlers
      this.handlerMap = {
        agent_start: () => h.onAgentStart?.(),
        agent_text: (d) => h.onAgentText?.(d),
        tool_call: (d) => h.onToolCall?.(d),
        tool_progress: (d) => h.onToolProgress?.(d),
        tool_result: (d) => h.onToolResult?.(d),
        token_update: (d) => h.onTokenUpdate?.(d),
        agent_done: (d) => h.onAgentDone?.(d),
        todo_update: () => h.onTodoUpdate?.(),
        goal_update: (d) => h.onGoalUpdate?.(d),
        approval_request: (d) => h.onApprovalRequest?.(d),
        ask_user_request: (d) => h.onAskUserRequest?.(d),
        session_reset: (d) => h.onSessionReset?.(d),
        model_changed: (d) => h.onModelChanged?.(d),
        agent_changed: (d) => h.onAgentChanged?.(d),
        mode_changed: (d) => h.onModeChanged?.(d),
        approval_mode_changed: (d) => h.onApprovalModeChanged?.(d),
        subagent_event: (d) => h.onSubagentEvent?.(d),
        subagent_progress: (d) => h.onSubagentProgress?.(d),
        user_message: (d) => h.onUserMessage?.(d),
        task_status: (d) => h.onTaskStatus?.(d?.task_id, !!d?.running, d?.project, d?.updated_at),
        artifact_upserted: (d) => h.onArtifactUpserted?.(d),
        pong: () => {},
      }
    }
    return this.handlerMap[type]
  }
}
