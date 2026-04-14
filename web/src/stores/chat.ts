// Main chat store using Pinia
import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import type {
  ChatMessage,
  ToolCall,
  PendingApproval,
  TodoItem,
  TokenUpdateData,
  SessionItem,
  AgentMode,
  ProviderInfo,
  SubagentToolEvent,
} from '@/types/api'
import { api } from '@/composables/api'

let _seqId = 0
function nextSeqId() {
  return ++_seqId
}

function genId(prefix: string) {
  return `${prefix}_${Date.now()}_${Math.random().toString(36).slice(2, 6)}`
}

// Timeline item: a single entry in the conversation timeline.
export type TimelineItem =
  | { kind: 'message'; data: ChatMessage; seq: number }
  | { kind: 'tool'; data: ToolCall; seq: number }
  | { kind: 'approval'; data: PendingApproval; seq: number }

export const useChatStore = defineStore('chat', () => {
  // --- State ---
  const timeline = ref<TimelineItem[]>([])
  const todos = ref<TodoItem[]>([])
  const sessions = ref<SessionItem[]>([])
  const isRunning = ref(false)
  const tokenInfo = ref<TokenUpdateData | null>(null)
  const config = ref<{ provider: string; model: string } | null>(null)
  const pwd = ref('')
  const wsConnected = ref(false)

  // Mode & model
  const mode = ref<AgentMode>('agent')
  const providerName = ref('')
  const modelName = ref('')
  const providers = ref<ProviderInfo[]>([])

  // Approval mode
  const autoApprove = ref(false)

  // Current streaming text accumulator
  let streamingText = ''
  let streamingMsgId = ''

  // --- Getters ---
  const messages = computed(() =>
    timeline.value
      .filter((i): i is TimelineItem & { kind: 'message' } => i.kind === 'message')
      .map((i) => i.data),
  )
  const hasMessages = computed(() => messages.value.length > 0)
  const activeTodos = computed(() => todos.value.filter((t) => t.Status !== 'completed'))
  const tokenPercentage = computed(() => {
    if (!tokenInfo.value || !tokenInfo.value.model_context_limit) return 0
    return Math.round((tokenInfo.value.total_tokens / tokenInfo.value.model_context_limit) * 100)
  })
  const projectName = computed(() => {
    const p = pwd.value
    if (!p) return ''
    const parts = p.split('/')
    return parts[parts.length - 1] || p
  })

  // --- Actions ---
  function addMessage(role: ChatMessage['role'], content: string): string {
    const id = genId('msg')
    const msg: ChatMessage = { id, role, content, timestamp: Date.now() }
    timeline.value.push({ kind: 'message', data: msg, seq: nextSeqId() })
    return id
  }

  function updateMessage(id: string, content: string) {
    const item = timeline.value.find((i) => i.kind === 'message' && i.data.id === id)
    if (item && item.kind === 'message') {
      item.data.content = content
    }
  }

  function appendAgentText(text: string) {
    streamingText += text
    if (!streamingMsgId) {
      streamingMsgId = addMessage('assistant', streamingText)
    } else {
      updateMessage(streamingMsgId, streamingText)
    }
  }

  function addToolCall(name: string, args: string) {
    // Flush current streaming — new text after tool will get a fresh message
    streamingText = ''
    streamingMsgId = ''

    const tc: ToolCall = {
      id: genId('tc'),
      name,
      args,
      status: 'running',
      timestamp: Date.now(),
    }
    timeline.value.push({ kind: 'tool', data: tc, seq: nextSeqId() })
  }

  function resolveToolCall(name: string, output: string, error?: string) {
    for (let i = timeline.value.length - 1; i >= 0; i--) {
      const item = timeline.value[i]
      if (item && item.kind === 'tool') {
        const tc = item.data
        if (tc.name === name && tc.status === 'running') {
          tc.output = output
          tc.error = error
          tc.status = error ? 'error' : 'done'
          break
        }
      }
    }
  }

  /** Attach an intermediate tool event to the most recent running subagent tool call. */
  function addSubagentProgress(agentName: string, event: string, toolName: string, detail: string) {
    // Find the running subagent tool call (name === 'subagent') that matches the agentName
    for (let i = timeline.value.length - 1; i >= 0; i--) {
      const item = timeline.value[i]
      if (item && item.kind === 'tool' && item.data.name === 'subagent' && item.data.status === 'running') {
        if (!item.data.children) {
          item.data.children = []
        }
        const child: SubagentToolEvent = {
          event: event as 'tool_call' | 'tool_result',
          toolName,
          detail,
          timestamp: Date.now(),
        }
        item.data.children.push(child)
        break
      }
    }
  }

  function agentDone(error?: string) {
    isRunning.value = false
    streamingText = ''
    streamingMsgId = ''
    if (error) {
      addMessage('system', `Error: ${error}`)
    }
  }

  function addApprovalRequest(data: PendingApproval) {
    timeline.value.push({ kind: 'approval', data, seq: nextSeqId() })
  }

  function resolveApprovalLocal(id: string, approved: boolean) {
    const item = timeline.value.find((i) => i.kind === 'approval' && i.data.id === id)
    if (item && item.kind === 'approval') {
      item.data.resolved = true
      item.data.approved = approved
    }
  }

  async function sendMessage(text: string) {
    addMessage('user', text)
    isRunning.value = true
    streamingText = ''
    streamingMsgId = ''
    try {
      await api.chat(text, mode.value === 'agent' ? ('build' as AgentMode) : mode.value)
    } catch (err: unknown) {
      isRunning.value = false
      addMessage('system', err instanceof Error ? err.message : String(err))
    }
  }

  async function resolveApproval(id: string, approved: boolean) {
    try {
      await api.approval(id, approved)
      resolveApprovalLocal(id, approved)
    } catch (err: unknown) {
      console.error('Approval error:', err)
    }
  }

  async function stopAgent() {
    try {
      await api.stop()
    } catch (err: unknown) {
      console.error('Stop error:', err)
    }
  }

  async function fetchSessions() {
    try {
      sessions.value = await api.sessions()
    } catch (err) {
      console.error('Failed to fetch sessions:', err)
    }
  }

  async function deleteSession(uuid: string) {
    try {
      await api.deleteSession(uuid)
      sessions.value = sessions.value.filter((s) => s.uuid !== uuid)
    } catch (err: unknown) {
      console.error('Failed to delete session:', err)
    }
  }

  async function newSession() {
    try {
      await api.newSession()
      clearChat()
      await fetchSessions()
    } catch (err: unknown) {
      addMessage('system', err instanceof Error ? err.message : String(err))
    }
  }

  async function fetchTodos() {
    try {
      todos.value = await api.todos()
    } catch (err) {
      console.error('Failed to fetch todos:', err)
    }
  }

  async function fetchConfig() {
    try {
      const c = await api.config()
      config.value = c
    } catch (err) {
      console.error('Failed to fetch config:', err)
    }
  }

  async function fetchHealth() {
    try {
      const h = await api.health()
      pwd.value = h.pwd
      providerName.value = h.provider
      modelName.value = h.model
      const m = h.mode || 'build'
      mode.value = m === 'build' ? 'agent' : (m as AgentMode)
    } catch (err) {
      console.error('Failed to fetch health:', err)
    }
  }

  async function fetchModels() {
    try {
      const data = await api.models()
      providers.value = data.providers || []
      providerName.value = data.current.provider
      modelName.value = data.current.model
    } catch (err) {
      console.error('Failed to fetch models:', err)
    }
  }

  async function switchModel(provider: string, model: string) {
    try {
      await api.switchModel(provider, model)
      providerName.value = provider
      modelName.value = model
      clearChat()
    } catch (err: unknown) {
      addMessage('system', `Failed to switch model: ${err instanceof Error ? err.message : String(err)}`)
    }
  }

  async function switchMode(newMode: AgentMode) {
    const backendMode = newMode === 'agent' ? 'build' : newMode
    try {
      await api.switchMode(backendMode)
      mode.value = newMode
    } catch (err: unknown) {
      console.error('Failed to switch mode:', err)
    }
  }

  function clearChat() {
    timeline.value = []
    todos.value = []
    tokenInfo.value = null
    streamingText = ''
    streamingMsgId = ''
  }

  async function fetchApprovalMode() {
    try {
      const data = await api.approvalMode()
      autoApprove.value = data.auto_approve
    } catch (err) {
      console.error('Failed to fetch approval mode:', err)
    }
  }

  async function setAutoApprove(enabled: boolean) {
    try {
      const data = await api.setApprovalMode(enabled)
      autoApprove.value = data.auto_approve
    } catch (err: unknown) {
      console.error('Failed to set approval mode:', err)
    }
  }

  async function loadSession(uuid: string) {
    try {
      const entries = await api.session(uuid)
      clearChat()
      for (const e of entries) {
        if (e.type === 'user' && e.content) addMessage('user', e.content)
        else if (e.type === 'assistant' && e.content) addMessage('assistant', e.content)
      }
    } catch (err: unknown) {
      addMessage('system', `Failed to load session: ${err instanceof Error ? err.message : String(err)}`)
    }
  }

  return {
    // State
    timeline,
    todos,
    sessions,
    isRunning,
    tokenInfo,
    config,
    pwd,
    wsConnected,
    mode,
    providerName,
    modelName,
    providers,
    autoApprove,
    // Getters
    messages,
    hasMessages,
    activeTodos,
    tokenPercentage,
    projectName,
    // Actions
    addMessage,
    updateMessage,
    appendAgentText,
    addToolCall,
    resolveToolCall,
    addSubagentProgress,
    agentDone,
    addApprovalRequest,
    resolveApproval,
    sendMessage,
    stopAgent,
    fetchSessions,
    deleteSession,
    newSession,
    fetchTodos,
    fetchConfig,
    fetchHealth,
    fetchModels,
    switchModel,
    switchMode,
    clearChat,
    loadSession,
    fetchApprovalMode,
    setAutoApprove,
  }
})
