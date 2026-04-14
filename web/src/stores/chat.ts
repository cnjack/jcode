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
  ModelsResponse,
  ProviderInfo,
} from '@/types/api'
import { api } from '@/composables/api'

let _nextId = 0
function nextId() {
  return `msg_${++_nextId}_${Date.now()}`
}

export const useChatStore = defineStore('chat', () => {
  // --- State ---
  const messages = ref<ChatMessage[]>([])
  const toolCalls = ref<ToolCall[]>([])
  const approvals = ref<PendingApproval[]>([])
  const todos = ref<TodoItem[]>([])
  const sessions = ref<SessionItem[]>([])
  const isRunning = ref(false)
  const tokenInfo = ref<TokenUpdateData | null>(null)
  const config = ref<{ provider: string; model: string } | null>(null)
  const pwd = ref('')
  const sseConnected = ref(false)

  // Mode & model
  const mode = ref<AgentMode>('build')
  const providerName = ref('')
  const modelName = ref('')
  const providers = ref<ProviderInfo[]>([])

  // Approval mode
  const autoApprove = ref(false)

  // Current streaming text accumulator
  let streamingText = ''
  let streamingMsgId = ''

  // --- Getters ---
  const hasMessages = computed(() => messages.value.length > 0)
  const activeTodos = computed(() => todos.value.filter((t) => t.Status !== 'completed'))
  const tokenPercentage = computed(() => {
    if (!tokenInfo.value || !tokenInfo.value.model_context_limit) return 0
    return Math.round(
      (tokenInfo.value.total_tokens / tokenInfo.value.model_context_limit) * 100,
    )
  })
  const projectName = computed(() => {
    const p = pwd.value
    if (!p) return ''
    const parts = p.split('/')
    return parts[parts.length - 1] || p
  })

  // --- Actions ---
  function addMessage(role: ChatMessage['role'], content: string): string {
    const id = nextId()
    messages.value.push({ id, role, content, timestamp: Date.now() })
    return id
  }

  function updateMessage(id: string, content: string) {
    const msg = messages.value.find((m) => m.id === id)
    if (msg) msg.content = content
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
    toolCalls.value.push({
      id: `tc_${Date.now()}`,
      name,
      args,
      status: 'running',
      timestamp: Date.now(),
    })
  }

  function resolveToolCall(name: string, output: string, error?: string) {
    for (let i = toolCalls.value.length - 1; i >= 0; i--) {
      const tc = toolCalls.value[i]
      if (tc.name === name && tc.status === 'running') {
        tc.output = output
        tc.error = error
        tc.status = error ? 'error' : 'done'
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
    approvals.value.push(data)
  }

  function resolveApprovalLocal(id: string, approved: boolean) {
    const a = approvals.value.find((x) => x.id === id)
    if (a) {
      a.resolved = true
      a.approved = approved
    }
  }

  async function sendMessage(text: string) {
    addMessage('user', text)
    isRunning.value = true
    streamingText = ''
    streamingMsgId = ''
    try {
      await api.chat(text, mode.value)
    } catch (err: any) {
      isRunning.value = false
      addMessage('system', err.message)
    }
  }

  async function resolveApproval(id: string, approved: boolean) {
    try {
      await api.approval(id, approved)
      resolveApprovalLocal(id, approved)
    } catch (err: any) {
      console.error('Approval error:', err)
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
    } catch (err: any) {
      console.error('Failed to delete session:', err)
    }
  }

  async function newSession() {
    try {
      await api.newSession()
      clearChat()
      await fetchSessions()
    } catch (err: any) {
      addMessage('system', err.message)
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
      mode.value = (h.mode as AgentMode) || 'build'
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
    } catch (err: any) {
      addMessage('system', `Failed to switch model: ${err.message}`)
    }
  }

  async function switchMode(newMode: AgentMode) {
    try {
      await api.switchMode(newMode)
      mode.value = newMode
    } catch (err: any) {
      console.error('Failed to switch mode:', err)
    }
  }

  function clearChat() {
    messages.value = []
    toolCalls.value = []
    approvals.value = []
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
    } catch (err: any) {
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
    } catch (err: any) {
      addMessage('system', `Failed to load session: ${err.message}`)
    }
  }

  return {
    // State
    messages,
    toolCalls,
    approvals,
    todos,
    sessions,
    isRunning,
    tokenInfo,
    config,
    pwd,
    sseConnected,
    mode,
    providerName,
    modelName,
    providers,
    autoApprove,
    // Getters
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
    agentDone,
    addApprovalRequest,
    resolveApproval,
    sendMessage,
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
