// Main chat store using Pinia
import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import type {
  ChatMessage,
  ChatImage,
  ToolCall,
  PendingApproval,
  TodoItem,
  TokenUpdateData,
  SessionItem,
  AgentMode,
  ProviderInfo,
  ToolDisplayInfo,
  ModelRef,
} from '@/types/api'
import { api } from '@/composables/api'
import { extractToolDisplayInfo } from '@/composables/toolInfo'

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

  // Channel state
  const channelAvailable = ref(false)
  const channelEnabled = ref(false)

  // Image support for current model
  const imageSupport = ref(false)

  // Server version
  const serverVersion = ref('')

  // Model favorites & recent
  const favoriteModels = ref<Set<string>>(new Set())
  const recentModels = ref<ModelRef[]>([])

  // Current session tracking
  const currentSessionId = ref('')

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
  const activeTodos = computed(() => todos.value.filter((t) => t.status !== 'completed'))
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
  function addMessage(role: ChatMessage['role'], content: string, source?: string, images?: ChatImage[]): string {
    const id = genId('msg')
    const msg: ChatMessage = { id, role, content, timestamp: Date.now(), source, images }
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

  function addToolCall(name: string, args: string, toolCallID?: string, displayInfo?: ToolDisplayInfo) {
    // Flush current streaming — new text after tool will get a fresh message
    streamingText = ''
    streamingMsgId = ''

    const tc: ToolCall = {
      id: genId('tc'),
      toolCallID,
      name,
      args,
      status: 'running',
      timestamp: Date.now(),
      displayInfo,
    }
    timeline.value.push({ kind: 'tool', data: tc, seq: nextSeqId() })
  }

  function resolveToolCall(name: string, output: string, toolCallID?: string, error?: string, displayOutput?: string) {
    // Prefer matching by backend tool_call_id (exact, unambiguous).
    // Fall back to name-based scan for older events without an ID.
    for (let i = timeline.value.length - 1; i >= 0; i--) {
      const item = timeline.value[i]
      if (item && item.kind === 'tool') {
        const tc = item.data
        if (tc.status !== 'running') continue
        const idMatch = toolCallID && tc.toolCallID && tc.toolCallID === toolCallID
        const nameMatch = !toolCallID && tc.name === name
        if (idMatch || nameMatch) {
          tc.output = output
          tc.displayOutput = displayOutput || undefined
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
        const children = item.data.children

        if (event === 'tool_call') {
          // Create a new child ToolCall in running state
          children.push({
            id: genId('sub_tc'),
            name: toolName,
            args: detail,
            status: 'running',
            timestamp: Date.now(),
            displayInfo: extractToolDisplayInfo(toolName, detail),
          })
        } else if (event === 'tool_result') {
          // Resolve the most recent running child with this toolName
          for (let j = children.length - 1; j >= 0; j--) {
            const child = children[j]
            if (child && child.name === toolName && child.status === 'running') {
              child.output = detail
              child.status = 'done'
              break
            }
          }
        }
        break
      }
    }
  }

  function agentDone(error?: string) {
    isRunning.value = false
    streamingText = ''
    streamingMsgId = ''
    // Clean up any tool calls still in 'running' state — the agent has finished.
    for (const item of timeline.value) {
      if (item.kind === 'tool' && item.data.status === 'running') {
        item.data.status = 'done'
      }
    }
    if (error) {
      addMessage('system', `Error: ${error}`)
    }
    // Refresh session list & current session ID (the recorder may have been
    // created lazily during this run).
    fetchHealth()
    fetchSessions()
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

  async function sendMessage(text: string, images?: ChatImage[]) {
    addMessage('user', text, undefined, images)
    isRunning.value = true
    streamingText = ''
    streamingMsgId = ''
    try {
      const resp = await api.chat(
        text,
        mode.value === 'agent' ? ('build' as AgentMode) : mode.value,
        currentSessionId.value || undefined,
        images,
      )
      // Track the session_id returned by the backend so subsequent messages
      // continue the same session (prevents duplicate session creation).
      if (resp.session_id) {
        currentSessionId.value = resp.session_id
      }
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
      const list = await api.sessions()
      // Sort newest first by created_at
      list.sort((a, b) => {
        if (!a.created_at || !b.created_at) return 0
        return new Date(b.created_at).getTime() - new Date(a.created_at).getTime()
      })
      sessions.value = list
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
    // Already on the welcome screen — nothing to do.
    if (!currentSessionId.value && timeline.value.length === 0) return
    try {
      // Reset backend state (history, old recorder) but don't create a new
      // recorder yet — it will be created lazily on the first message.
      await api.newSession()
      currentSessionId.value = ''
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
      currentSessionId.value = h.session_id || ''
      isRunning.value = h.running || false
      imageSupport.value = h.image_support || false
      serverVersion.value = h.version || ''
      return h
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
      // Update image support based on the new model's capabilities.
      updateImageSupport(provider, model)
    } catch (err: unknown) {
      addMessage('system', `Failed to switch model: ${err instanceof Error ? err.message : String(err)}`)
    }
  }

  function updateImageSupport(provider: string, model: string) {
    const p = providers.value.find(pv => pv.id === provider)
    if (p) {
      const m = p.models.find(mv => mv.id === model)
      imageSupport.value = m?.image_support || false
    } else {
      imageSupport.value = false
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

  async function fetchChannelState() {
    try {
      const data = await api.channelStatus()
      channelAvailable.value = data.available
      channelEnabled.value = data.state === 'enabled'
    } catch {
      /* ignore */
    }
  }

  async function toggleChannel(enabled: boolean) {
    try {
      if (enabled) {
        await api.channelEnable()
        channelEnabled.value = true
      } else {
        await api.channelDisable()
        channelEnabled.value = false
      }
    } catch (err: unknown) {
      console.error('Failed to toggle channel:', err)
    }
  }

  async function fetchModelState() {
    try {
      const data = await api.modelState()
      recentModels.value = data.recent || []
      const favs = new Set<string>()
      for (const r of data.favorite || []) {
        favs.add(`${r.provider}/${r.model}`)
      }
      favoriteModels.value = favs
    } catch {
      /* ignore */
    }
  }

  async function toggleFavorite(provider: string, model: string) {
    try {
      const data = await api.toggleFavorite(provider, model)
      const key = `${provider}/${model}`
      const favs = new Set(favoriteModels.value)
      if (data.favorite) {
        favs.add(key)
      } else {
        favs.delete(key)
      }
      favoriteModels.value = favs
    } catch {
      /* ignore */
    }
  }

  async function toggleModelEnabled(provider: string, model: string, enabled: boolean) {
    try {
      await api.toggleModelEnabled(provider, model, enabled)
      // Update the local providers list to reflect the change
      for (const p of providers.value) {
        if (p.id === provider) {
          const m = p.models.find(m => m.id === model)
          if (m) {
            m.enabled = enabled
          }
        }
      }
    } catch {
      /* ignore */
    }
  }

  function isFavorite(provider: string, model: string): boolean {
    return favoriteModels.value.has(`${provider}/${model}`)
  }

  // Providers with only enabled models (for model picker)
  const enabledProviders = computed(() => {
    return providers.value
      .map(p => ({
        ...p,
        models: p.models.filter(m => m.enabled !== false),
      }))
      .filter(p => p.models.length > 0)
  })

  /** Restore the current session content if available (called on page load). */
  async function restoreCurrentSession() {
    if (!currentSessionId.value) return
    try {
      const entries = await api.session(currentSessionId.value)
      if (entries.length === 0) return

      clearChat()

      const pendingToolCalls = new Map<string, ToolCall>()
      for (const e of entries) {
        if (e.type === 'user' && e.content) {
          addMessage('user', e.content)
        } else if (e.type === 'assistant' && e.content) {
          addMessage('assistant', e.content)
        } else if (e.type === 'tool_call' && e.name) {
          const tc: ToolCall = {
            id: genId('tc'),
            toolCallID: e.tool_call_id,
            name: e.name,
            args: e.args || '',
            status: 'running',
            timestamp: e.timestamp ? new Date(e.timestamp).getTime() : Date.now(),
            displayInfo: extractToolDisplayInfo(e.name, e.args || ''),
          }
          timeline.value.push({ kind: 'tool', data: tc, seq: nextSeqId() })
          if (e.tool_call_id) {
            pendingToolCalls.set(e.tool_call_id, tc)
          }
        } else if (e.type === 'tool_result') {
          let resolved = false
          if (e.tool_call_id) {
            const tc = pendingToolCalls.get(e.tool_call_id)
            if (tc) {
              tc.output = e.output || ''
              tc.error = e.error || ''
              tc.status = e.error ? 'error' : 'done'
              pendingToolCalls.delete(e.tool_call_id)
              resolved = true
            }
          }
          if (!resolved && e.name) {
            for (let i = timeline.value.length - 1; i >= 0; i--) {
              const item = timeline.value[i]
              if (item && item.kind === 'tool' && item.data.name === e.name && item.data.status === 'running') {
                item.data.output = e.output || ''
                item.data.error = e.error || ''
                item.data.status = e.error ? 'error' : 'done'
                break
              }
            }
          }
        }
      }
      // Mark any tool calls that never got a result as done
      for (const tc of pendingToolCalls.values()) {
        tc.status = 'done'
      }
    } catch {
      // Session file may not exist yet (lazy creation), silently ignore
    }
  }

  async function retryFromMessage(messageId: string) {
    // Find the assistant/system message to retry
    const msgIdx = timeline.value.findIndex(i => i.kind === 'message' && i.data.id === messageId)
    if (msgIdx === -1) return

    // Find the last user message before this index
    let userMsgText = ''
    let userMsgIdx = -1
    for (let i = msgIdx - 1; i >= 0; i--) {
      const item = timeline.value[i]
      if (item && item.kind === 'message' && item.data.role === 'user') {
        userMsgText = item.data.content
        userMsgIdx = i
        break
      }
    }
    if (!userMsgText || userMsgIdx === -1) return

    // Count user messages BEFORE userMsgIdx in the timeline.
    // This matches the backend's user-message count (role === 'user' entries in s.history).
    const beforeUserMessage = timeline.value
      .slice(0, userMsgIdx)
      .filter(i => i.kind === 'message' && i.data.role === 'user')
      .length

    // Truncate backend history in-place and keep using the returned session id.
    // The backend preserves the same session UUID while removing the edited tail.
    try {
      const res = await api.truncateHistory(beforeUserMessage)
      if (res.session_id) {
        currentSessionId.value = res.session_id
      }
    } catch (err) {
      console.error('Failed to truncate history:', err)
      return
    }

    // Truncate frontend timeline from the user message (inclusive) and reset streaming state
    timeline.value.splice(userMsgIdx)
    streamingText = ''
    streamingMsgId = ''

    await sendMessage(userMsgText)
  }

  async function editAndResend(messageId: string, newText: string) {
    // Find the user message to edit
    const msgIdx = timeline.value.findIndex(
      i => i.kind === 'message' && i.data.id === messageId && i.data.role === 'user',
    )
    if (msgIdx === -1) return

    // Count user messages BEFORE this message in the timeline.
    const beforeUserMessage = timeline.value
      .slice(0, msgIdx)
      .filter(i => i.kind === 'message' && i.data.role === 'user')
      .length

    // Truncate backend history in-place and keep using the returned session id.
    try {
      const res = await api.truncateHistory(beforeUserMessage)
      if (res.session_id) {
        currentSessionId.value = res.session_id
      }
    } catch (err) {
      console.error('Failed to truncate history:', err)
      return
    }

    // Truncate frontend timeline from this message (inclusive) and reset streaming state
    timeline.value.splice(msgIdx)
    streamingText = ''
    streamingMsgId = ''

    await sendMessage(newText)
  }

  async function loadSession(uuid: string) {
    try {
      const entries = await api.session(uuid)

      // Notify backend to switch to this session (resume) before clearing UI.
      const resp = await api.newSession(uuid)
      currentSessionId.value = resp.session_id || uuid

      clearChat()

      // Track pending tool calls by tool_call_id so we can match results
      const pendingToolCalls = new Map<string, ToolCall>()
      for (const e of entries) {
        if (e.type === 'user' && e.content) {
          addMessage('user', e.content)
        } else if (e.type === 'assistant' && e.content) {
          addMessage('assistant', e.content)
        } else if (e.type === 'tool_call' && e.name) {
          const tc: ToolCall = {
            id: genId('tc'),
            toolCallID: e.tool_call_id,
            name: e.name,
            args: e.args || '',
            status: 'running',
            timestamp: e.timestamp ? new Date(e.timestamp).getTime() : Date.now(),
            displayInfo: extractToolDisplayInfo(e.name, e.args || ''),
          }
          timeline.value.push({ kind: 'tool', data: tc, seq: nextSeqId() })
          if (e.tool_call_id) {
            pendingToolCalls.set(e.tool_call_id, tc)
          }
        } else if (e.type === 'tool_result') {
          let resolved = false
          // First try matching by tool_call_id (exact, unambiguous)
          if (e.tool_call_id) {
            const tc = pendingToolCalls.get(e.tool_call_id)
            if (tc) {
              tc.output = e.output || ''
              tc.error = e.error || ''
              tc.status = e.error ? 'error' : 'done'
              pendingToolCalls.delete(e.tool_call_id)
              resolved = true
            }
          }
          // Fallback: match by name scanning timeline backwards
          if (!resolved && e.name) {
            for (let i = timeline.value.length - 1; i >= 0; i--) {
              const item = timeline.value[i]
              if (item && item.kind === 'tool' && item.data.name === e.name && item.data.status === 'running') {
                item.data.output = e.output || ''
                item.data.error = e.error || ''
                item.data.status = e.error ? 'error' : 'done'
                break
              }
            }
          }
        }
      }
      // Mark any tool calls that never got a result as done (session was interrupted).
      for (const tc of pendingToolCalls.values()) {
        tc.status = 'done'
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
    channelAvailable,
    channelEnabled,
    imageSupport,
    serverVersion,
    currentSessionId,
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
    fetchChannelState,
    toggleChannel,
    restoreCurrentSession,
    fetchModelState,
    toggleFavorite,
    toggleModelEnabled,
    isFavorite,
    recentModels,
    favoriteModels,
    enabledProviders,
    retryFromMessage,
    editAndResend,
  }
})
