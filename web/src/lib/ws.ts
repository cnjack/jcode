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
  onAgentDone?: (data: import('./types').AgentDoneData) => void
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
  onUserMessage?: (data: { content: string; source: string; local_echo?: boolean }) => void
  onRemoteConnectionStatus?: (data: import('./types').RemoteConnectionStatusData) => void
  onModelRetryStatus?: (data: import('./types').ModelRetryStatusData) => void
  onTaskStatus?: (taskId: string, running: boolean, project?: string, updatedAt?: string) => void
  onArtifactUpserted?: (data: {
    task_id: string
    id?: string
    artifact_id?: string
    focus?: boolean
  }) => void
  /** Fired when the socket opens/closes so the UI can show online/offline. */
  onConnectionChange?: (connected: boolean) => void
  /** Returns the task currently shown in the foreground. Foreground-only events
   * tagged with any other task — including while no task is active — are dropped. */
  activeTaskId?: () => string | undefined
  /** Existing conversation whose history snapshot is ready but has not yet
   * become foreground. Its task-scoped events are buffered by the bridge. */
  pendingTaskId?: () => string | undefined
  onPendingTaskEvent?: (event: PendingTaskEvent) => void
}

export interface PendingTaskEvent {
  type: string
  taskId: string
  data: unknown
}

interface WSMessage {
  type: string
  task_id?: string
  data?: unknown
}

/** Event types whose data payload gets the envelope task_id merged in. */
const TASK_ID_DATA_TYPES = new Set([
  'approval_request',
  'ask_user_request',
  'agent_done',
  'artifact_upserted',
  'remote_connection_status',
  'model_retry_status',
])
const BACKGROUND_EVENT_TYPES = new Set([
  'agent_done',
  'artifact_upserted',
  'remote_connection_status',
  'model_retry_status',
])
const PENDING_FOREGROUND_EVENT_TYPES = new Set([
  'agent_start',
  'agent_text',
  'tool_call',
  'tool_progress',
  'tool_result',
  'token_update',
  'agent_done',
  'todo_update',
  'goal_update',
  'approval_request',
  'ask_user_request',
  'session_reset',
  'model_changed',
  'agent_changed',
  'mode_changed',
  'approval_mode_changed',
  'subagent_event',
  'subagent_progress',
  'user_message',
  'remote_connection_status',
  'model_retry_status',
])

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
        const taskRoutingEnabled = this.handlers.activeTaskId !== undefined
        const active = this.handlers.activeTaskId?.()
        let data = msg.data
        if (msg.task_id && TASK_ID_DATA_TYPES.has(msg.type)) {
          data = { ...((data && typeof data === 'object' ? data : {}) as Record<string, unknown>), task_id: msg.task_id }
        }
        const pending = this.handlers.pendingTaskId?.()
        if (
          msg.task_id &&
          pending === msg.task_id &&
          msg.task_id !== active &&
          PENDING_FOREGROUND_EVENT_TYPES.has(msg.type)
        ) {
          this.handlers.onPendingTaskEvent?.({ type: msg.type, taskId: msg.task_id, data })
          return
        }
        // Foreground mutations must match the active task. This also covers the
        // short new-chat window where currentSessionId is empty: a running
        // background task must not repopulate the freshly cleared timeline.
        // Background metadata still passes so queues/status/sidebar can settle.
        if (
          taskRoutingEnabled && msg.task_id && msg.task_id !== active &&
          !BACKGROUND_EVENT_TYPES.has(msg.type)
        ) return
        dispatchWSHandler(this.handlers, msg.type, data)
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

}

/** Apply one already-routed event to a handler set. Pending-conversation
 * buffers use this after the target becomes foreground. */
export function dispatchWSHandler(handlers: WSHandlers, type: string, data: unknown): void {
  // The wire contract is heterogeneous by event type; narrowing happens at
  // the strongly typed handler boundary below.
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  const d = data as any
  switch (type) {
    case 'agent_start': handlers.onAgentStart?.(); break
    case 'agent_text': handlers.onAgentText?.(d); break
    case 'tool_call': handlers.onToolCall?.(d); break
    case 'tool_progress': handlers.onToolProgress?.(d); break
    case 'tool_result': handlers.onToolResult?.(d); break
    case 'token_update': handlers.onTokenUpdate?.(d); break
    case 'agent_done': handlers.onAgentDone?.(d); break
    case 'todo_update': handlers.onTodoUpdate?.(); break
    case 'goal_update': handlers.onGoalUpdate?.(d); break
    case 'approval_request': handlers.onApprovalRequest?.(d); break
    case 'ask_user_request': handlers.onAskUserRequest?.(d); break
    case 'session_reset': handlers.onSessionReset?.(d); break
    case 'model_changed': handlers.onModelChanged?.(d); break
    case 'agent_changed': handlers.onAgentChanged?.(d); break
    case 'mode_changed': handlers.onModeChanged?.(d); break
    case 'approval_mode_changed': handlers.onApprovalModeChanged?.(d); break
    case 'subagent_event': handlers.onSubagentEvent?.(d); break
    case 'subagent_progress': handlers.onSubagentProgress?.(d); break
    case 'user_message': handlers.onUserMessage?.(d); break
    case 'remote_connection_status': handlers.onRemoteConnectionStatus?.(d); break
    case 'model_retry_status': handlers.onModelRetryStatus?.(d); break
    case 'task_status': handlers.onTaskStatus?.(d?.task_id, !!d?.running, d?.project, d?.updated_at); break
    case 'artifact_upserted': handlers.onArtifactUpserted?.(d); break
  }
}
