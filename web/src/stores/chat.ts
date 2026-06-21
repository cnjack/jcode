// Main chat store using Pinia
import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import type {
  ChatMessage,
  ChatImage,
  ToolCall,
  PendingApproval,
  TodoItem,
  Goal,
  TokenUpdateData,
  SessionItem,
  AgentMode,
  ProviderInfo,
  ToolDisplayInfo,
  ModelRef,
  AskUserQuestion,
  AskUserAnswer,
} from '@/types/api'
import { normalizeMode } from '@/types/api'
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
  const goal = ref<Goal | null>(null)
  // When armed (toggled from the input toolbar), the next message typed in the
  // normal prompt box is submitted as the session goal instead of a chat message.
  const goalArmed = ref(false)
  const sessions = ref<SessionItem[]>([])
  const isRunning = ref(false)
  const tokenInfo = ref<TokenUpdateData | null>(null)
  const config = ref<{ provider: string; model: string } | null>(null)
  const pwd = ref('')
  const wsConnected = ref(false)

  // Mode & model
  const mode = ref<AgentMode>('ask')
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

  // Wall-clock start of the in-flight turn (set when a prompt is sent), used to
  // stamp the turn's elapsed time onto its final assistant message on completion.
  let turnStartedAt = 0

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
  function addMessage(
    role: ChatMessage['role'],
    content: string,
    source?: string,
    images?: ChatImage[],
    level?: ChatMessage['level'],
    detail?: string,
  ): string {
    const id = genId('msg')
    const msg: ChatMessage = { id, role, content, timestamp: Date.now(), source, images, level, detail }
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

  // Map raw backend/model error strings to a short, readable message. The raw
  // string is preserved separately (shown collapsed) when it differs.
  function friendlyError(raw: string): string {
    const e = raw.toLowerCase()
    // Phrase-based only: bare tokens like "eof" or a stray "401"/"500" in an
    // error body (e.g. "unexpected EOF", a line number) must not be misread.
    if (e.includes('deadline exceeded') || e.includes('timed out') || e.includes('timeout'))
      return 'The request timed out. Please try again.'
    if (
      e.includes('connection refused') ||
      e.includes('no such host') ||
      e.includes('connection reset') ||
      e.includes('dial tcp') ||
      e.includes('network is unreachable') ||
      e.includes('network error')
    )
      return 'Network error. Check your connection and try again.'
    if (e.includes('unauthorized') || e.includes('invalid api key') || e.includes('api key'))
      return 'Authentication failed. Check your API key in settings.'
    if (e.includes('rate limit') || e.includes('too many requests'))
      return 'Rate limited by the provider. Please wait and try again.'
    if (
      e.includes('internal server error') ||
      e.includes('service unavailable') ||
      e.includes('bad gateway') ||
      e.includes('overloaded')
    )
      return 'The model provider had a temporary error. Please try again.'
    // Otherwise strip internal framing noise (e.g. "[NodeRunError] … node path: […]").
    const cleaned = raw
      .replace(/^\[[A-Za-z]+Error\]\s*/, '')
      .replace(/\s*node path:\s*\[[^\]]*\]\s*$/i, '')
      .trim()
    return cleaned || raw
  }

  // Recursively mark a tool (and its subagent child tools) done if still
  // running. Subagent children live in tool.children and were previously never
  // cleaned, so interrupting a run mid-subagent left child tools spinning
  // ("running…/Searching…") forever under an already-✓ parent.
  function markToolTreeDone(tool: ToolCall) {
    if (tool.status === 'running') tool.status = 'done'
    if (tool.children) {
      for (const child of tool.children) markToolTreeDone(child)
    }
  }

  function agentDone(error?: string) {
    isRunning.value = false
    streamingText = ''
    streamingMsgId = ''
    // Stamp the turn's elapsed time onto its final assistant message (done before
    // any "Stopped"/error system message is appended, so we target the real
    // response). The live "Thinking…" timer is transient; this makes the duration
    // persist in the conversation history.
    if (turnStartedAt > 0) {
      for (let i = timeline.value.length - 1; i >= 0; i--) {
        const item = timeline.value[i]
        if (item && item.kind === 'message' && item.data.role === 'assistant') {
          item.data.durationMs = Date.now() - turnStartedAt
          break
        }
      }
      turnStartedAt = 0
    }
    // Clean up any tool calls still in 'running' state — the agent has finished.
    for (const item of timeline.value) {
      if (item.kind === 'tool') markToolTreeDone(item.data)
    }
    if (error) {
      // Match only the exact cancellation forms the backend emits, so a real
      // failure that merely mentions a canceled child context is still surfaced
      // as an error rather than swallowed as a benign "Stopped".
      const lower = error.trim().toLowerCase()
      const isCancel =
        lower === 'stopped by user' ||
        lower === 'context canceled' ||
        lower === 'context cancelled'
      if (isCancel) {
        // A user stop is expected, not an error. One stop can emit several
        // cancellation signals (stopped-by-user, the in-flight "context
        // canceled", node errors); collapse them into a single calm notice.
        const last = timeline.value[timeline.value.length - 1]
        const alreadyNoted =
          last?.kind === 'message' &&
          last.data.role === 'system' &&
          last.data.content === 'Stopped'
        if (!alreadyNoted) addMessage('system', 'Stopped', undefined, undefined, 'notice')
      } else {
        const friendly = friendlyError(error)
        addMessage('system', friendly, undefined, undefined, 'error', friendly !== error ? error : undefined)
      }
    }
    // Refresh session list & current session ID (the recorder may have been
    // created lazily during this run).
    fetchHealth()
    fetchSessions()
  }

  function addApprovalRequest(data: PendingApproval) {
    timeline.value.push({ kind: 'approval', data, seq: nextSeqId() })
  }

  /**
   * Arm a timeline ask_user tool item with a request id so its card switches to
   * interactive answer mode. Returns true if a matching item was armed (or was
   * already armed with this id — idempotent, so re-emits/pulls are safe).
   * An item already armed with a *different* id is skipped, so two concurrent
   * ask_user calls in one turn each bind to a distinct item.
   */
  function armAskItem(id: string, questions: AskUserQuestion[]): boolean {
    for (let i = timeline.value.length - 1; i >= 0; i--) {
      const item = timeline.value[i]
      if (!item || item.kind !== 'tool' || item.data.name !== 'ask_user') continue
      if (item.data.askUserId === id) return true // already armed (idempotent)
      // Match an unarmed item: live (running) or a replayed one (done, no output).
      if (!item.data.askUserId && (item.data.status === 'running' || (item.data.status === 'done' && !item.data.output))) {
        item.data.status = 'running'
        item.data.askUserId = id
        item.data.askUserQuestions = questions
        return true
      }
    }
    return false
  }

  /**
   * Handle an ask_user_request event. The tool_call event normally creates the
   * timeline item first, so we just arm it. If no item exists — the tool_call
   * was dropped (full event buffer), a rare but unrecoverable case — synthesize
   * one so the question still renders instead of silently hanging the run.
   */
  function attachAskUserRequest(id: string, questions: AskUserQuestion[]) {
    if (armAskItem(id, questions)) return
    const args = JSON.stringify({ questions })
    timeline.value.push({
      kind: 'tool',
      data: {
        id: genId('tc'),
        name: 'ask_user',
        args,
        status: 'running',
        timestamp: Date.now(),
        displayInfo: extractToolDisplayInfo('ask_user', args),
        askUserId: id,
        askUserQuestions: questions,
      },
      seq: nextSeqId(),
    })
  }

  /**
   * Re-attach any server-side pending ask_user questions after the timeline is
   * (re)built from a session — covers page reload / session resume mid-question,
   * where the live ask_user_request event was already consumed and lost.
   */
  async function reconcileAskUser() {
    try {
      const pending = await api.askPending()
      for (const req of pending) armAskItem(req.id, req.questions)
    } catch {
      /* ignore */
    }
  }

  /**
   * Re-attach any server-side pending approval requests after the timeline is
   * (re)built from a session — covers page reload / session resume / WS
   * reconnect mid-approval, where the live approval_request event was already
   * consumed and lost. Without this a pending approval card vanishes on switch
   * and the agent stays blocked forever. Dedupes by id so re-pulls are safe.
   */
  async function reconcileApprovals() {
    try {
      const pending = await api.approvalPending()
      for (const req of pending) {
        const exists = timeline.value.some((i) => i.kind === 'approval' && i.data.id === req.id)
        if (!exists) addApprovalRequest(req)
      }
    } catch {
      /* ignore */
    }
  }

  /** Submit answers for a pending ask_user request and collapse its card. */
  async function submitAskUser(id: string, answers: AskUserAnswer[]) {
    try {
      await api.askUser(id, answers)
      // Clear the pending state only after the answer is delivered, so a failed
      // POST leaves the card interactive (the agent is still blocked waiting).
      for (const item of timeline.value) {
        if (item.kind === 'tool' && item.data.askUserId === id) {
          item.data.askUserId = undefined
          break
        }
      }
    } catch (err: unknown) {
      addMessage(
        'system',
        `Failed to submit answer: ${err instanceof Error ? err.message : String(err)}`,
        undefined, undefined, 'error',
      )
    }
  }

  function resolveApprovalLocal(id: string, approved: boolean) {
    const item = timeline.value.find((i) => i.kind === 'approval' && i.data.id === id)
    if (item && item.kind === 'approval') {
      item.data.resolved = true
      item.data.approved = approved
    }
  }

  async function sendMessage(text: string, images?: ChatImage[]) {
    const trimmed = text.trim()

    // Goal-armed mode: the 🎯 toggle in the input toolbar arms the prompt box,
    // and the next normal message becomes the session goal (slash commands
    // still behave as commands).
    if (goalArmed.value && trimmed && !trimmed.startsWith('/')) {
      goalArmed.value = false
      addMessage('user', text)
      await setGoalObjective(trimmed)
      return
    }

    // Intercept the /goal slash command so it manages the session goal instead
    // of being sent to the model as a plain message. Grammar matches TUI/ACP:
    // "" | "status" reports, "clear" removes, anything else sets the objective.
    if (trimmed === '/goal' || trimmed.startsWith('/goal ')) {
      const arg = trimmed.slice('/goal'.length).trim()
      if (arg === '' || arg === 'status') {
        await fetchGoal()
        const g = goal.value
        addMessage(
          'system',
          g ? `🎯 ${g.status} — ${g.objective}` : '🎯 No goal set. Use /goal <objective> to set one.',
        )
        return
      }
      if (arg === 'clear') {
        await clearGoal()
        addMessage('system', '🎯 Goal cleared.')
        return
      }
      addMessage('user', text)
      await setGoalObjective(arg)
      return
    }

    addMessage('user', text, undefined, images)
    isRunning.value = true
    turnStartedAt = Date.now()
    streamingText = ''
    streamingMsgId = ''
    try {
      const resp = await api.chat(
        text,
        mode.value,
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

  async function resolveApproval(id: string, approved: boolean, approveAll = false) {
    const item = timeline.value.find((i) => i.kind === 'approval' && i.data.id === id)
    if (item && item.kind === 'approval') {
      // Block re-entry while a previous click is still in flight (or already
      // resolved) — otherwise repeated clicks fire multiple POSTs.
      if (item.data.resolving || item.data.resolved) return
      item.data.resolving = true
    }
    try {
      await api.approval(id, approved, approveAll)
      resolveApprovalLocal(id, approved)
    } catch (err: unknown) {
      // Re-enable the buttons and tell the user, instead of a silent
      // console.error that leaves the card looking unresponsive.
      if (item && item.kind === 'approval') item.data.resolving = false
      addMessage(
        'system',
        `Approval failed: ${err instanceof Error ? err.message : String(err)}`,
        undefined, undefined, 'error',
      )
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

  // resetToWelcomeAfterSwitch refreshes session-scoped state after the active
  // workspace changed (local switch or remote bind) and lands on a fresh welcome
  // screen so the next message starts a new task in the chosen workspace.
  async function resetToWelcomeAfterSwitch() {
    await fetchHealth()
    currentSessionId.value = ''
    clearChat()
    fetchTodos()
    fetchGoal()
    await fetchSessions()
  }

  async function fetchTodos() {
    try {
      todos.value = await api.todos()
    } catch (err) {
      console.error('Failed to fetch todos:', err)
    }
  }

  async function fetchGoal() {
    try {
      goal.value = await api.goal()
    } catch (err) {
      console.error('Failed to fetch goal:', err)
    }
  }

  // setGoalObjective sets a new session goal. The backend starts working toward
  // it immediately, so reflect the running state locally.
  async function setGoalObjective(objective: string) {
    try {
      goal.value = await api.setGoal(objective, true)
      isRunning.value = true
    } catch (err: unknown) {
      addMessage('system', err instanceof Error ? err.message : String(err))
    }
  }

  async function clearGoal() {
    try {
      await api.clearGoal()
      goal.value = null
    } catch (err) {
      console.error('Failed to clear goal:', err)
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
      mode.value = normalizeMode(h.mode)
      autoApprove.value = mode.value === 'autopilot'
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
    try {
      await api.switchMode(newMode)
      mode.value = newMode
      autoApprove.value = newMode === 'autopilot'
    } catch (err: unknown) {
      console.error('Failed to switch mode:', err)
    }
  }

  function clearChat() {
    timeline.value = []
    todos.value = []
    goal.value = null
    goalArmed.value = false
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

  // Set the default auto-approve preference. The backend persists it as the
  // startup mode and applies it now (auto-approve maps onto the unified mode:
  // Autopilot when on, Ask when off), so keep the local mode/flag in sync.
  async function setAutoApprove(enabled: boolean) {
    try {
      await api.setApprovalMode(enabled)
      autoApprove.value = enabled
      mode.value = enabled ? 'autopilot' : 'ask'
    } catch (err) {
      console.error('Failed to set auto-approve:', err)
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
      // Re-attach any question/approval still awaiting a response on the server.
      await reconcileAskUser()
      await reconcileApprovals()
    } catch {
      // Session file may not exist yet (lazy creation), silently ignore
    }
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
      // The backend restored the session's goal — refresh explicitly in case
      // the goal_update WS push is missed.
      fetchGoal()

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
      // Re-attach any question/approval still awaiting a response on the server.
      await reconcileAskUser()
      await reconcileApprovals()
    } catch (err: unknown) {
      addMessage('system', `Failed to load session: ${err instanceof Error ? err.message : String(err)}`)
    }
  }

  return {
    // State
    timeline,
    todos,
    goal,
    goalArmed,
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
    attachAskUserRequest,
    submitAskUser,
    reconcileAskUser,
    reconcileApprovals,
    resolveApproval,
    sendMessage,
    stopAgent,
    fetchSessions,
    deleteSession,
    newSession,
    resetToWelcomeAfterSwitch,
    fetchTodos,
    fetchGoal,
    setGoalObjective,
    clearGoal,
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
    editAndResend,
  }
})
