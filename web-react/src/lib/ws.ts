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
  onToolCall?: (data: { name: string; args: string; tool_call_id?: string; display_info?: { title: string; subtitle?: string; icon?: string; category?: string } }) => void
  onToolResult?: (data: { name: string; output: string; display_output?: string; error?: string; tool_call_id?: string }) => void
  onTokenUpdate?: (data: import('./types').TokenUpdateData) => void
  onAgentDone?: (data: { error?: string }) => void
  onTodoUpdate?: () => void
  onGoalUpdate?: (data: import('jcode-ui-core').Goal | null) => void
  onApprovalRequest?: (data: import('./types').ApprovalRequestData) => void
  onAskUserRequest?: (data: import('./types').AskUserRequestData) => void
  onSessionReset?: (data: { session_id: string }) => void
  onModelChanged?: (data: { provider: string; model: string }) => void
  onModeChanged?: (data: { mode: string }) => void
  onApprovalModeChanged?: (data: { auto_approve: boolean }) => void
  onSubagentEvent?: (data: import('./types').SubagentEventData) => void
  onSubagentProgress?: (data: import('./types').SubagentProgressData) => void
  onUserMessage?: (data: { content: string; source: string }) => void
  onTaskStatus?: (taskId: string, running: boolean) => void
  /** Returns the task currently shown in the foreground. Events tagged with a
   *  DIFFERENT task id are dropped so they don't pollute the active view. */
  activeTaskId?: () => string | undefined
}

interface WSMessage {
  type: string
  task_id?: string
  data?: unknown
}

export class WSClient {
  private ws: WebSocket | null = null
  private retryTimer: ReturnType<typeof setTimeout> | null = null
  private pingTimer: ReturnType<typeof setInterval> | null = null
  private connected = false
  private handlers: WSHandlers

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
    if (this.ws) {
      this.ws.close()
      this.ws = null
    }
    const token = getAuthToken()
    this.ws = token
      ? new WebSocket(`${wsBase()}/api/ws`, ['jcode-auth', token])
      : new WebSocket(`${wsBase()}/api/ws`)

    this.ws.onopen = () => {
      this.connected = true
      if (this.pingTimer) clearInterval(this.pingTimer)
      this.pingTimer = setInterval(() => this.send({ type: 'ping' }), 30000)
    }

    this.ws.onmessage = (event) => {
      try {
        const msg: WSMessage = JSON.parse(event.data)
        const active = this.handlers.activeTaskId?.()
        if (msg.task_id && active && msg.task_id !== active) return
        const handler = this.handlerFor(msg.type)
        if (handler) {
          let data = msg.data
          if (
            msg.task_id &&
            (msg.type === 'approval_request' || msg.type === 'ask_user_request') &&
            data &&
            typeof data === 'object'
          ) {
            data = { ...(data as Record<string, unknown>), task_id: msg.task_id }
          }
          handler(data)
        }
      } catch {
        // parse error — drop
      }
    }

    this.ws.onerror = () => {
      this.connected = false
    }

    this.ws.onclose = () => {
      this.connected = false
      if (this.pingTimer) {
        clearInterval(this.pingTimer)
        this.pingTimer = null
      }
      this.ws = null
      this.retryTimer = setTimeout(() => this.connect(), 3000)
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
    if (this.retryTimer) clearTimeout(this.retryTimer)
    if (this.pingTimer) clearInterval(this.pingTimer)
    this.ws?.close()
    this.ws = null
    this.connected = false
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
        tool_result: (d) => h.onToolResult?.(d),
        token_update: (d) => h.onTokenUpdate?.(d),
        agent_done: (d) => h.onAgentDone?.(d),
        todo_update: () => h.onTodoUpdate?.(),
        goal_update: (d) => h.onGoalUpdate?.(d),
        approval_request: (d) => h.onApprovalRequest?.(d),
        ask_user_request: (d) => h.onAskUserRequest?.(d),
        session_reset: (d) => h.onSessionReset?.(d),
        model_changed: (d) => h.onModelChanged?.(d),
        mode_changed: (d) => h.onModeChanged?.(d),
        approval_mode_changed: (d) => h.onApprovalModeChanged?.(d),
        subagent_event: (d) => h.onSubagentEvent?.(d),
        subagent_progress: (d) => h.onSubagentProgress?.(d),
        user_message: (d) => h.onUserMessage?.(d),
        task_status: (d) => h.onTaskStatus?.(d?.task_id, !!d?.running),
        pong: () => {},
      }
    }
    return this.handlerMap[type]
  }
}
