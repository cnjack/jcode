// WebSocket client composable for jcode web
import { ref, onUnmounted } from 'vue'
import type {
  AgentTextData,
  ToolCallData,
  ToolResultData,
  TokenUpdateData,
  AgentDoneData,
  ApprovalRequestData,
  AskUserRequestData,
  SubagentEventData,
  SubagentProgressData,
  Goal,
} from '@/types/api'

type WSHandler = {
  onAgentStart?: () => void
  onAgentText?: (data: AgentTextData) => void
  onToolCall?: (data: ToolCallData) => void
  onToolResult?: (data: ToolResultData) => void
  onTokenUpdate?: (data: TokenUpdateData) => void
  onAgentDone?: (data: AgentDoneData) => void
  onTodoUpdate?: () => void
  onGoalUpdate?: (data: Goal | null) => void
  onApprovalRequest?: (data: ApprovalRequestData) => void
  onAskUserRequest?: (data: AskUserRequestData) => void
  onSessionReset?: (data: { session_id: string }) => void
  onModelChanged?: (data: { provider: string; model: string }) => void
  onModeChanged?: (data: { mode: string }) => void
  onApprovalModeChanged?: (data: { auto_approve: boolean }) => void
  onSubagentEvent?: (data: SubagentEventData) => void
  onSubagentProgress?: (data: SubagentProgressData) => void
  onUserMessage?: (data: { content: string; source: string }) => void
}

interface WSMessage {
  type: string
  data?: unknown
}

export function useWebSocket(handlers: WSHandler) {
  const connected = ref(false)
  let ws: WebSocket | null = null
  let retryTimer: ReturnType<typeof setTimeout> | null = null
  let pingTimer: ReturnType<typeof setInterval> | null = null

  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  const handlerMap: Record<string, (data: any) => void> = {
    agent_start: () => handlers.onAgentStart?.(),
    agent_text: (d) => handlers.onAgentText?.(d),
    tool_call: (d) => handlers.onToolCall?.(d),
    tool_result: (d) => handlers.onToolResult?.(d),
    token_update: (d) => handlers.onTokenUpdate?.(d),
    agent_done: (d) => handlers.onAgentDone?.(d),
    todo_update: () => handlers.onTodoUpdate?.(),
    goal_update: (d) => handlers.onGoalUpdate?.(d),
    approval_request: (d) => handlers.onApprovalRequest?.(d),
    ask_user_request: (d) => handlers.onAskUserRequest?.(d),
    session_reset: (d) => handlers.onSessionReset?.(d),
    model_changed: (d) => handlers.onModelChanged?.(d),
    mode_changed: (d) => handlers.onModeChanged?.(d),
    approval_mode_changed: (d) => handlers.onApprovalModeChanged?.(d),
    subagent_event: (d) => handlers.onSubagentEvent?.(d),
    subagent_progress: (d) => handlers.onSubagentProgress?.(d),
    user_message: (d) => handlers.onUserMessage?.(d),
    pong: () => {}, // heartbeat response, no-op
  }

  function connect() {
    if (ws) {
      ws.close()
      ws = null
    }

    const proto = location.protocol === 'https:' ? 'wss:' : 'ws:'
    ws = new WebSocket(`${proto}//${location.host}/api/ws`)

    ws.onopen = () => {
      connected.value = true
      // Start heartbeat
      if (pingTimer) clearInterval(pingTimer)
      pingTimer = setInterval(() => {
        send({ type: 'ping' })
      }, 30000)
    }

    ws.onmessage = (event) => {
      try {
        const msg: WSMessage = JSON.parse(event.data)
        const handler = handlerMap[msg.type]
        if (handler) {
          handler(msg.data)
        }
      } catch (err) {
        console.error('WS parse error:', err)
      }
    }

    ws.onerror = () => {
      connected.value = false
    }

    ws.onclose = () => {
      connected.value = false
      if (pingTimer) {
        clearInterval(pingTimer)
        pingTimer = null
      }
      ws = null
      retryTimer = setTimeout(connect, 3000)
    }
  }

  function send(msg: WSMessage) {
    if (ws && ws.readyState === WebSocket.OPEN) {
      ws.send(JSON.stringify(msg))
    }
  }

  function sendApproval(id: string, approved: boolean, approveAll = false) {
    send({ type: 'approval', data: { id, approved, approve_all: approveAll } })
  }

  function disconnect() {
    if (retryTimer) clearTimeout(retryTimer)
    if (pingTimer) clearInterval(pingTimer)
    ws?.close()
    ws = null
    connected.value = false
  }

  connect()
  onUnmounted(disconnect)

  return { connected, send, sendApproval, disconnect }
}
