// SSE event source composable for jcode web
import { ref, onUnmounted } from 'vue'
import type {
  AgentTextData,
  ToolCallData,
  ToolResultData,
  TokenUpdateData,
  AgentDoneData,
  ApprovalRequestData,
} from '@/types/api'

type SSEHandler = {
  onAgentText?: (data: AgentTextData) => void
  onToolCall?: (data: ToolCallData) => void
  onToolResult?: (data: ToolResultData) => void
  onTokenUpdate?: (data: TokenUpdateData) => void
  onAgentDone?: (data: AgentDoneData) => void
  onTodoUpdate?: () => void
  onApprovalRequest?: (data: ApprovalRequestData) => void
  onSessionReset?: (data: { session_id: string }) => void
  onModelChanged?: (data: { provider: string; model: string }) => void
  onModeChanged?: (data: { mode: string }) => void
}

export function useSSE(handlers: SSEHandler) {
  const connected = ref(false)
  let source: EventSource | null = null
  let retryTimer: ReturnType<typeof setTimeout> | null = null

  function connect() {
    if (source) source.close()
    source = new EventSource('/api/events')

    source.onopen = () => {
      connected.value = true
    }

    source.onerror = () => {
      connected.value = false
      source?.close()
      source = null
      retryTimer = setTimeout(connect, 3000)
    }

    const events: [string, (data: any) => void][] = [
      ['agent_text', (d) => handlers.onAgentText?.(d)],
      ['tool_call', (d) => handlers.onToolCall?.(d)],
      ['tool_result', (d) => handlers.onToolResult?.(d)],
      ['token_update', (d) => handlers.onTokenUpdate?.(d)],
      ['agent_done', (d) => handlers.onAgentDone?.(d)],
      ['todo_update', () => handlers.onTodoUpdate?.()],
      ['approval_request', (d) => handlers.onApprovalRequest?.(d)],
      ['session_reset', (d) => handlers.onSessionReset?.(d)],
      ['model_changed', (d) => handlers.onModelChanged?.(d)],
      ['mode_changed', (d) => handlers.onModeChanged?.(d)],
    ]

    for (const [event, handler] of events) {
      source.addEventListener(event, (e: MessageEvent) => {
        try {
          const data = e.data ? JSON.parse(e.data) : undefined
          handler(data)
        } catch (err) {
          console.error(`SSE parse error for ${event}:`, err)
        }
      })
    }
  }

  function disconnect() {
    if (retryTimer) clearTimeout(retryTimer)
    source?.close()
    source = null
    connected.value = false
  }

  connect()
  onUnmounted(disconnect)

  return { connected, disconnect }
}
