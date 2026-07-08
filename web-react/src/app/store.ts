/**
 * Redux Toolkit store — the React app's state layer.
 *
 * The Vue app had one 1209-line Pinia chat store. Here we split it across
 * focused slices (per the migration assessment), each owning a clear concern:
 *   - chat      : timeline + streaming accumulation
 *   - session   : sessions/tasks list, current session, project
 *   - approval  : approval gates + ask-user interactive blocks
 *   - model     : provider/model/mode/favorites
 *
 * The whole store is exposed to jcode-ui via createExternalStoreRuntime +
 * a select() that projects to RuntimeState (see app/runtime.ts).
 */

import { configureStore, createSlice, createAsyncThunk } from '@reduxjs/toolkit'
import type { ThreadItem, Message, ToolCall, Approval, TokenSnapshot, Goal, TodoItem, QueuedMessage, AskUserQuestion } from 'jcode-ui-core'
import { api } from '../lib/api'
import type { AgentMode, ProviderInfo, SessionItem, TaskItem, SlashCommandInfo } from '../lib/types'

// ─── seq counter (stable DOM identity across streaming updates) ───
let _seq = 0
const nextSeq = () => ++_seq
function genId(prefix: string) {
  return `${prefix}_${Date.now()}_${Math.random().toString(36).slice(2, 6)}`
}

// ═══════════════════════════════════════════════════════════════════════════
// chat slice — timeline + streaming accumulation
// ═══════════════════════════════════════════════════════════════════════════

interface ChatState {
  timeline: ThreadItem[]
  isRunning: boolean
  tokenSnapshot: TokenSnapshot | null
  goal: Goal | null
  todos: TodoItem[]
  queued: QueuedMessage[]
  slashCommands: SlashCommandInfo[]
}

const initialChat: ChatState = {
  timeline: [],
  isRunning: false,
  tokenSnapshot: null,
  goal: null,
  todos: [],
  queued: [],
  slashCommands: [],
}

// Streaming accumulators — non-reactive module state (matches the Vue pattern).
let streamingText = ''
let streamingMsgId = ''

const chatSlice = createSlice({
  name: 'chat',
  initialState: initialChat,
  reducers: {
    clearChat(s) {
      s.timeline = []
      s.isRunning = false
      s.tokenSnapshot = null
      s.goal = null
      s.todos = []
      s.queued = []
      streamingText = ''
      streamingMsgId = ''
    },
    setRunning(s, a: { payload: boolean }) {
      s.isRunning = a.payload
    },
    addMessage(
      s,
      a: { payload: { role: Message['role']; content: string; source?: string; images?: Message['images']; level?: Message['level']; detail?: string; durationMs?: number } },
    ) {
      const msg: Message = {
        id: genId('msg'),
        timestamp: Date.now(),
        ...a.payload,
      }
      s.timeline.push({ kind: 'message', data: msg, seq: nextSeq() })
    },
    appendAgentText(s, a: { payload: string }) {
      const delta = a.payload
      streamingText += delta
      if (!streamingMsgId) {
        const msg: Message = {
          id: genId('asst'),
          role: 'assistant',
          content: streamingText,
          timestamp: Date.now(),
        }
        streamingMsgId = msg.id
        s.timeline.push({ kind: 'message', data: msg, seq: nextSeq() })
      } else {
        const item = s.timeline.find((i) => i.kind === 'message' && i.data.id === streamingMsgId)
        if (item && item.kind === 'message') item.data.content = streamingText
      }
    },
    addToolCall(
      s,
      a: { payload: { name: string; args: string; toolCallID?: string; displayInfo?: ToolCall['displayInfo'] } },
    ) {
      // A tool call flushes the streaming text accumulator (text after a tool
      // starts a fresh assistant message).
      streamingText = ''
      streamingMsgId = ''
      const tc: ToolCall = {
        id: genId('tool'),
        name: a.payload.name,
        args: a.payload.args,
        toolCallID: a.payload.toolCallID,
        status: 'running',
        timestamp: Date.now(),
        displayInfo: a.payload.displayInfo,
      }
      s.timeline.push({ kind: 'tool', data: tc, seq: nextSeq() })
    },
    resolveToolCall(
      s,
      a: { payload: { name: string; toolCallID?: string; output?: string; displayOutput?: string; error?: string } },
    ) {
      const { toolCallID, name, output, displayOutput, error } = a.payload
      // Match by toolCallID (precise) or by the last running tool with this name.
      for (let i = s.timeline.length - 1; i >= 0; i--) {
        const item = s.timeline[i]
        if (item.kind !== 'tool') continue
        const match = toolCallID ? item.data.toolCallID === toolCallID : item.data.name === name && item.data.status === 'running'
        if (match) {
          item.data.status = error ? 'error' : 'done'
          item.data.output = output
          item.data.displayOutput = displayOutput
          item.data.error = error
          break
        }
      }
    },
    setTokenSnapshot(s, a: { payload: TokenSnapshot | null }) {
      s.tokenSnapshot = a.payload
    },
    setGoal(s, a: { payload: Goal | null }) {
      s.goal = a.payload
    },
    setTodos(s, a: { payload: TodoItem[] }) {
      s.todos = a.payload
    },
    enqueueMessage(s, a: { payload: QueuedMessage }) {
      s.queued.push(a.payload)
    },
    removeQueued(s, a: { payload: string }) {
      s.queued = s.queued.filter((q) => q.id !== a.payload)
    },
    drainQueue(s) {
      // Pops the first queued message — the App thunk resends it on agentDone.
      if (s.queued.length > 0) s.queued.shift()
    },
    agentDone(s, a: { payload: { error?: string } | undefined }) {
      // Stamp duration on the last assistant message.
      for (let i = s.timeline.length - 1; i >= 0; i--) {
        const item = s.timeline[i]
        if (item.kind === 'message' && item.data.role === 'assistant') {
          item.data.durationMs = Date.now() - (item.data.timestamp || Date.now())
          break
        }
      }
      // Mark any lingering running tools as done.
      for (const item of s.timeline) {
        if (item.kind === 'tool' && item.data.status === 'running') {
          item.data.status = 'done'
        }
      }
      if (a.payload?.error) {
        s.timeline.push({
          kind: 'message',
          data: { id: genId('sys'), role: 'system', content: a.payload.error, timestamp: Date.now(), level: 'error' },
          seq: nextSeq(),
        })
      }
      s.isRunning = false
      streamingText = ''
      streamingMsgId = ''
    },
    setSlashCommands(s, a: { payload: SlashCommandInfo[] }) {
      s.slashCommands = a.payload
    },
    addApprovalRequest(s, a: { payload: Approval }) {
      s.timeline.push({ kind: 'approval', data: a.payload, seq: nextSeq() })
    },
    attachAskUser(s, a: { payload: { toolName: string; askUserId: string; questions: AskUserQuestion[] } }) {
      // Arm the matching tool with ask_user state (the tool was added by tool_call).
      for (let i = s.timeline.length - 1; i >= 0; i--) {
        const item = s.timeline[i]
        if (item.kind === 'tool' && item.data.name === a.payload.toolName && item.data.status === 'running' && !item.data.askUserId) {
          item.data.askUserId = a.payload.askUserId
          item.data.askUserQuestions = a.payload.questions
          break
        }
      }
    },
    setApprovalResolving(s, a: { payload: { id: string; resolving: boolean } }) {
      const item = s.timeline.find((i) => i.kind === 'approval' && i.data.id === a.payload.id)
      if (item && item.kind === 'approval') item.data.resolving = a.payload.resolving
    },
    resolveApprovalItem(s, a: { payload: { id: string; approved: boolean } }) {
      const item = s.timeline.find((i) => i.kind === 'approval' && i.data.id === a.payload.id)
      if (item && item.kind === 'approval') {
        item.data.resolved = true
        item.data.approved = a.payload.approved
        item.data.resolving = false
      }
    },
  },
})

// ═══════════════════════════════════════════════════════════════════════════
// session slice — sessions/tasks list, current session, project
// ═══════════════════════════════════════════════════════════════════════════

interface SessionState {
  sessions: SessionItem[]
  tasks: TaskItem[]
  currentSessionId: string
  projectPath: string
  wsConnected: boolean
}

const initialSession: SessionState = {
  sessions: [],
  tasks: [],
  currentSessionId: '',
  projectPath: '',
  wsConnected: false,
}

const sessionSlice = createSlice({
  name: 'session',
  initialState: initialSession,
  reducers: {
    setSessions(s, a: { payload: SessionItem[] }) {
      s.sessions = a.payload
    },
    setTasks(s, a: { payload: TaskItem[] }) {
      s.tasks = a.payload
    },
    setCurrentSession(s, a: { payload: string }) {
      s.currentSessionId = a.payload
    },
    setProjectPath(s, a: { payload: string }) {
      s.projectPath = a.payload
    },
    setWsConnected(s, a: { payload: boolean }) {
      s.wsConnected = a.payload
    },
    setTaskRunning(s, a: { payload: { taskId: string; running: boolean } }) {
      const t = s.tasks.find((x) => x.uuid === a.payload.taskId)
      if (t) t.running = a.payload.running
    },
  },
})

// ═══════════════════════════════════════════════════════════════════════════
// model slice — provider/model/mode/favorites
// ═══════════════════════════════════════════════════════════════════════════

interface ModelState {
  providerName: string
  modelName: string
  mode: AgentMode
  providers: ProviderInfo[]
  favoriteModels: string[]
  recentModels: { provider: string; model: string }[]
  autoApprove: boolean
  imageSupport: boolean
  serverVersion: string
}

const initialModel: ModelState = {
  providerName: '',
  modelName: '',
  mode: 'approval',
  providers: [],
  favoriteModels: [],
  recentModels: [],
  autoApprove: false,
  imageSupport: false,
  serverVersion: '',
}

const modelSlice = createSlice({
  name: 'model',
  initialState: initialModel,
  reducers: {
    setProvider(s, a: { payload: string }) {
      s.providerName = a.payload
    },
    setModel(s, a: { payload: string }) {
      s.modelName = a.payload
    },
    setMode(s, a: { payload: AgentMode }) {
      s.mode = a.payload
    },
    setProviders(s, a: { payload: ProviderInfo[] }) {
      s.providers = a.payload
    },
    setAutoApprove(s, a: { payload: boolean }) {
      s.autoApprove = a.payload
    },
    setImageSupport(s, a: { payload: boolean }) {
      s.imageSupport = a.payload
    },
    setServerVersion(s, a: { payload: string }) {
      s.serverVersion = a.payload
    },
  },
})

// ═══════════════════════════════════════════════════════════════════════════
// App UI slice — view routing, dialog open states
// ═══════════════════════════════════════════════════════════════════════════

type View = 'chat' | 'automations' | 'channels' | 'automation-run'

interface UiState {
  activeView: View
  settingsOpen: boolean
  paletteOpen: boolean
  needsAuth: boolean
  needsSetup: boolean
  connectionError: string
  theme: string
}

const initialUi: UiState = {
  activeView: 'chat',
  settingsOpen: false,
  paletteOpen: false,
  needsAuth: false,
  needsSetup: false,
  connectionError: '',
  theme: 'system',
}

const uiSlice = createSlice({
  name: 'ui',
  initialState: initialUi,
  reducers: {
    setView(s, a: { payload: View }) {
      s.activeView = a.payload
    },
    setSettingsOpen(s, a: { payload: boolean }) {
      s.settingsOpen = a.payload
    },
    setPaletteOpen(s, a: { payload: boolean }) {
      s.paletteOpen = a.payload
    },
    setNeedsAuth(s, a: { payload: boolean }) {
      s.needsAuth = a.payload
    },
    setNeedsSetup(s, a: { payload: boolean }) {
      s.needsSetup = a.payload
    },
    setConnectionError(s, a: { payload: string }) {
      s.connectionError = a.payload
    },
    setTheme(s, a: { payload: string }) {
      s.theme = a.payload
    },
  },
})

// ═══════════════════════════════════════════════════════════════════════════
// Store wiring
// ═══════════════════════════════════════════════════════════════════════════

export const chatActions = chatSlice.actions
export const sessionActions = sessionSlice.actions
export const modelActions = modelSlice.actions
export const uiActions = uiSlice.actions

// Async thunks — wrap API calls + dispatch the right reducers.
export const sendMessage = createAsyncThunk(
  'chat/send',
  async (payload: { text: string; images?: import('jcode-ui-core').ChatImage[]; mode?: AgentMode }, { dispatch, getState }) => {
    const state = getState() as RootState
    const sessionId = state.session.currentSessionId || undefined
    // /goal slash interception (matches Vue store.sendMessage).
    if (payload.text.startsWith('/goal ')) {
      const objective = payload.text.slice(6).trim()
      dispatch(chatActions.addMessage({ role: 'user', content: payload.text }))
      const goal = await api.setGoal(objective)
      dispatch(chatActions.setGoal(goal))
      return
    }
    dispatch(chatActions.addMessage({ role: 'user', content: payload.text, images: payload.images }))
    dispatch(chatActions.setRunning(true))
    const resp = await api.chat(payload.text, payload.mode, sessionId, payload.images)
    if (!state.session.currentSessionId) dispatch(sessionActions.setCurrentSession(resp.session_id))
  },
)

export const stopAgent = createAsyncThunk('chat/stop', async (_, { getState }) => {
  const state = getState() as RootState
  await api.stop(state.session.currentSessionId || undefined)
})

export const resolveApproval = createAsyncThunk(
  'approval/resolve',
  async (payload: { id: string; approved: boolean; approveAll?: boolean }, { dispatch }) => {
    dispatch(chatActions.setApprovalResolving({ id: payload.id, resolving: true }))
    try {
      await api.approval(payload.id, payload.approved, payload.approveAll ?? false)
      dispatch(chatActions.resolveApprovalItem({ id: payload.id, approved: payload.approved }))
    } catch {
      dispatch(chatActions.setApprovalResolving({ id: payload.id, resolving: false }))
    }
  },
)

export const submitAskUser = createAsyncThunk(
  'askUser/submit',
  async (payload: { id: string; answers: import('jcode-ui-core').AskUserAnswer[] }, { dispatch }) => {
    try {
      await api.askUser(payload.id, payload.answers)
    } catch {
      // surface in timeline as a system message
      dispatch(chatActions.addMessage({ role: 'system', content: 'Failed to submit answer', level: 'error' }))
    }
  },
)

export const editMessage = createAsyncThunk(
  'chat/edit',
  async (payload: { id: string; text: string }, { dispatch }) => {
    // Trim the timeline up to (and including) the edited message, then resend.
    dispatch(chatActions.clearChat())
    await dispatch(sendMessage({ text: payload.text }))
  },
)

export const loadSessions = createAsyncThunk('session/load', async (_, { dispatch }) => {
  const sessions = await api.sessions()
  dispatch(sessionActions.setSessions(sessions))
})

export const loadTasks = createAsyncThunk('tasks/load', async (_, { dispatch }) => {
  const tasks = await api.tasks()
  dispatch(sessionActions.setTasks(tasks))
})

export const loadSlashCommands = createAsyncThunk('chat/loadSlash', async (_, { dispatch }) => {
  const cmds = await api.slashCommands()
  dispatch(chatActions.setSlashCommands(cmds))
})

export const store = configureStore({
  reducer: {
    chat: chatSlice.reducer,
    session: sessionSlice.reducer,
    model: modelSlice.reducer,
    ui: uiSlice.reducer,
  },
})

export type RootState = ReturnType<typeof store.getState>
export type AppDispatch = typeof store.dispatch
