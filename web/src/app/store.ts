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
import { extractToolDisplayInfo } from '../lib/toolInfo'
import { normalizeMode, type AgentMode, type ProviderInfo, type SessionItem, type TaskItem, type SlashCommandInfo, type SessionEntry, type ModelRef } from '../lib/types'

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
  goalArmed: boolean
  todos: TodoItem[]
  /** Type-ahead queues keyed by session id — a message queued while an agent
   *  runs belongs to THAT conversation and must survive switching away and
   *  back (previously a single global list wiped by clearChat on switch). */
  queuedBySession: Record<string, QueuedMessage[]>
  slashCommands: SlashCommandInfo[]
}

const initialChat: ChatState = {
  timeline: [],
  isRunning: false,
  tokenSnapshot: null,
  goal: null,
  goalArmed: false,
  todos: [],
  queuedBySession: {},
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
      s.goalArmed = false
      s.todos = []
      // NOTE: queuedBySession is deliberately NOT cleared here — clearChat runs
      // on every session switch, and stashed type-ahead queues must survive.
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
      a: {
        payload: {
          name: string
          args: string
          toolCallID?: string
          displayInfo?: ToolCall['displayInfo']
          batchId?: string
          batchIndex?: number
          batchSize?: number
          startedAt?: number
        }
      },
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
        batchId: a.payload.batchId,
        batchIndex: a.payload.batchIndex,
        batchSize: a.payload.batchSize,
        // Fallback to the local clock so the live elapsed badge always works.
        startedAt: a.payload.startedAt ?? Date.now(),
      }
      s.timeline.push({ kind: 'tool', data: tc, seq: nextSeq() })
    },
    resolveToolCall(
      s,
      a: {
        payload: {
          name: string
          toolCallID?: string
          output?: string
          displayOutput?: string
          error?: string
          denied?: boolean
          durationMs?: number
          streams?: ToolCall['streams']
          meta?: ToolCall['meta']
          presentation?: ToolCall['presentation']
        }
      },
    ) {
      const { toolCallID, name, output, displayOutput, error, denied, durationMs, streams, meta, presentation } = a.payload
      // Match by toolCallID (precise) or by the last running tool with this name.
      for (let i = s.timeline.length - 1; i >= 0; i--) {
        const item = s.timeline[i]
        if (item.kind !== 'tool') continue
        const match = toolCallID ? item.data.toolCallID === toolCallID : item.data.name === name && item.data.status === 'running'
        if (match) {
          item.data.status = error ? 'error' : (meta?.exit_code !== undefined && meta.exit_code !== 0 ? 'error' : 'done')
          item.data.output = output
          item.data.displayOutput = displayOutput
          item.data.error = error
          // Denied (user rejection) is a distinct state from error; a result
          // always clears the awaiting-approval highlight.
          item.data.denied = denied || undefined
          item.data.awaitingApproval = undefined
          if (streams) item.data.streams = streams
          if (meta) item.data.meta = meta
          // Merge the event-level duration into meta when meta lacks one
          // (falling back to the local startedAt delta) so finished rows can
          // always show an accurate duration badge.
          const duration =
            item.data.meta?.duration_ms ??
            durationMs ??
            (item.data.startedAt ? Date.now() - item.data.startedAt : undefined)
          if (duration !== undefined) {
            item.data.meta = { ...(item.data.meta || {}), duration_ms: duration }
          }
          if (presentation) {
            item.data.presentation = presentation
            // Merge presentation collapsible/kind into displayInfo for grouping.
            item.data.displayInfo = {
              ...(item.data.displayInfo || { title: name }),
              kind: presentation.kind || item.data.displayInfo?.kind,
              collapsible: presentation.collapsible ?? item.data.displayInfo?.collapsible,
            }
          }
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
    setGoalArmed(s, a: { payload: boolean }) {
      s.goalArmed = a.payload
    },
    setTodos(s, a: { payload: TodoItem[] }) {
      s.todos = a.payload
    },
    enqueueMessage(s, a: { payload: { sessionId: string; message: QueuedMessage } }) {
      ;(s.queuedBySession[a.payload.sessionId] ??= []).push(a.payload.message)
    },
    removeQueued(s, a: { payload: { sessionId: string; id: string } }) {
      const q = s.queuedBySession[a.payload.sessionId]
      if (!q) return
      const next = q.filter((m) => m.id !== a.payload.id)
      if (next.length > 0) s.queuedBySession[a.payload.sessionId] = next
      else delete s.queuedBySession[a.payload.sessionId]
    },
    shiftQueued(s, a: { payload: string }) {
      // Pops the first queued message of the given session — the WS bridge
      // resends it on that session's agentDone.
      const q = s.queuedBySession[a.payload]
      if (!q) return
      q.shift()
      if (q.length === 0) delete s.queuedBySession[a.payload]
    },
    dropSessionQueue(s, a: { payload: string }) {
      // Session was deleted — its stash can never drain again.
      delete s.queuedBySession[a.payload]
    },
    agentDone(s, a: { payload: { error?: string; detail?: string; stopped?: boolean } | undefined }) {
      // Stamp duration on the last assistant message.
      for (let i = s.timeline.length - 1; i >= 0; i--) {
        const item = s.timeline[i]
        if (item.kind === 'message' && item.data.role === 'assistant') {
          item.data.durationMs = Date.now() - (item.data.timestamp || Date.now())
          break
        }
      }
      // Mark any lingering running tools as done (and drop any stale
      // awaiting-approval highlight — the run is over).
      for (const item of s.timeline) {
        if (item.kind === 'tool' && item.data.status === 'running') {
          item.data.status = 'done'
        }
        if (item.kind === 'tool' && item.data.awaitingApproval) {
          item.data.awaitingApproval = undefined
        }
      }
      if (a.payload?.stopped) {
        // Manual stop — a calm muted notice, not an error card.
        s.timeline.push({
          kind: 'message',
          data: {
            id: genId('sys'),
            role: 'system',
            content: 'Stopped by user',
            timestamp: Date.now(),
            level: 'notice',
          },
          seq: nextSeq(),
        })
      } else if (a.payload?.error) {
        s.timeline.push({
          kind: 'message',
          data: {
            id: genId('sys'),
            role: 'system',
            content: a.payload.error,
            detail: a.payload.detail || undefined,
            timestamp: Date.now(),
            level: 'error',
          },
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
    setTimeline(s, a: { payload: ThreadItem[] }) {
      s.timeline = a.payload
    },
    truncateTimelineFrom(s, a: { payload: string }) {
      const idx = s.timeline.findIndex((i) => i.kind === 'message' && i.data.id === a.payload)
      if (idx >= 0) s.timeline = s.timeline.slice(0, idx)
      streamingText = ''
      streamingMsgId = ''
    },
    addApprovalRequest(s, a: { payload: Approval }) {
      s.timeline.push({ kind: 'approval', data: a.payload, seq: nextSeq() })
      // Paint the gated tool row as awaiting approval (warning color) while
      // the prompt is unresolved.
      if (a.payload.tool_call_id) {
        for (let i = s.timeline.length - 1; i >= 0; i--) {
          const item = s.timeline[i]
          if (item.kind === 'tool' && item.data.toolCallID === a.payload.tool_call_id) {
            if (item.data.status === 'running') item.data.awaitingApproval = true
            break
          }
        }
      }
    },
    attachAskUser(s, a: { payload: { toolName: string; askUserId: string; questions: AskUserQuestion[]; taskId?: string } }) {
      // Arm the matching tool with ask_user state (the tool was added by tool_call).
      for (let i = s.timeline.length - 1; i >= 0; i--) {
        const item = s.timeline[i]
        if (item.kind !== 'tool' || item.data.name !== a.payload.toolName) continue
        if (item.data.askUserId === a.payload.askUserId) return
        if (!item.data.askUserId && (item.data.status === 'running' || (item.data.status === 'done' && !item.data.output))) {
          item.data.status = 'running'
          item.data.askUserId = a.payload.askUserId
          item.data.askUserQuestions = a.payload.questions
          ;(item.data as ToolCall & { askUserTaskId?: string }).askUserTaskId = a.payload.taskId
          return
        }
      }
      const args = JSON.stringify({ questions: a.payload.questions })
      const tc: ToolCall = {
        id: genId('ask'),
        name: a.payload.toolName,
        args,
        status: 'running',
        timestamp: Date.now(),
        displayInfo: extractToolDisplayInfo(a.payload.toolName, args),
        askUserId: a.payload.askUserId,
        askUserQuestions: a.payload.questions,
      }
      ;(tc as ToolCall & { askUserTaskId?: string }).askUserTaskId = a.payload.taskId
      s.timeline.push({ kind: 'tool', data: tc, seq: nextSeq() })
    },
    addSubagentProgress(s, a: { payload: { event: string; toolName: string; detail: string } }) {
      for (let i = s.timeline.length - 1; i >= 0; i--) {
        const item = s.timeline[i]
        if (item.kind !== 'tool' || item.data.name !== 'subagent' || item.data.status !== 'running') continue
        item.data.children ??= []
        if (a.payload.event === 'tool_call') {
          item.data.children.push({
            id: genId('sub_tc'),
            name: a.payload.toolName,
            args: a.payload.detail,
            status: 'running',
            timestamp: Date.now(),
            displayInfo: extractToolDisplayInfo(a.payload.toolName, a.payload.detail),
          })
        } else if (a.payload.event === 'tool_result') {
          for (let j = item.data.children.length - 1; j >= 0; j--) {
            const child = item.data.children[j]
            if (child.name === a.payload.toolName && child.status === 'running') {
              child.output = a.payload.detail
              child.status = 'done'
              break
            }
          }
        }
        break
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
        // Clear the awaiting-approval highlight on the gated tool row (the
        // denied strikethrough, if any, arrives with the tool_result).
        if (item.data.tool_call_id) {
          for (const t of s.timeline) {
            if (t.kind === 'tool' && t.data.toolCallID === item.data.tool_call_id) {
              t.data.awaitingApproval = undefined
              break
            }
          }
        }
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
      // Same lazy-index race as setTasks: keep the open session if the server
      // hasn't written it to the index yet.
      const next = a.payload
      const seen = new Set(next.map((x) => x.uuid))
      const localOnly = s.sessions.filter(
        (x) => !seen.has(x.uuid) && x.uuid === s.currentSessionId,
      )
      s.sessions = localOnly.length ? [...localOnly, ...next] : next
    },
    setTasks(s, a: { payload: TaskItem[] }) {
      // Preserve a just-created local task that isn't in the server index yet
      // (session files are created lazily on the first user message).
      const next = a.payload
      const seen = new Set(next.map((t) => t.uuid))
      const localOnly = s.tasks.filter(
        (t) => !seen.has(t.uuid) && t.uuid === s.currentSessionId,
      )
      s.tasks = localOnly.length ? [...localOnly, ...next] : next
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
    /** Insert or merge a task so the sidebar shows it immediately. */
    upsertTask(s, a: { payload: TaskItem }) {
      const i = s.tasks.findIndex((t) => t.uuid === a.payload.uuid)
      if (i >= 0) {
        s.tasks[i] = { ...s.tasks[i], ...a.payload }
      } else {
        s.tasks.unshift(a.payload)
      }
    },
    /** Patch fields on an existing task (no-op if missing). */
    patchTask(s, a: { payload: { uuid: string } & Partial<TaskItem> }) {
      const t = s.tasks.find((x) => x.uuid === a.payload.uuid)
      if (!t) return
      const { uuid: _uuid, ...rest } = a.payload
      Object.assign(t, rest)
    },
    /** Insert or merge a session for the active-project fallback list. */
    upsertSession(s, a: { payload: SessionItem }) {
      const i = s.sessions.findIndex((x) => x.uuid === a.payload.uuid)
      if (i >= 0) {
        s.sessions[i] = { ...s.sessions[i], ...a.payload }
      } else {
        s.sessions.unshift(a.payload)
      }
    },
  },
})

// ═══════════════════════════════════════════════════════════════════════════
// model slice — provider/model/mode/favorites
// ═══════════════════════════════════════════════════════════════════════════

interface ModelState {
  providerName: string
  modelName: string
  // config.small_model as "provider/model"; '' = unset (follow the main model).
  smallModel: string
  mode: AgentMode
  providers: ProviderInfo[]
  favoriteModels: string[]
  recentModels: ModelRef[]
  effortOverrides: Record<string, string>
  autoApprove: boolean
  imageSupport: boolean
  serverVersion: string
  maxIterations: number
}

const initialModel: ModelState = {
  providerName: '',
  modelName: '',
  smallModel: '',
  mode: 'approval',
  providers: [],
  favoriteModels: [],
  recentModels: [],
  effortOverrides: {},
  autoApprove: false,
  imageSupport: false,
  serverVersion: '',
  maxIterations: 0,
}

// Recompute imageSupport from the providers list for the currently selected
// model. Runs on every provider/model/providers change so the flag can never
// go stale when the user switches models (picker selectModel, WS
// model_changed). When the current model isn't in the list yet (providers not
// loaded), the last value — seeded from /health at startup — is kept.
function syncImageSupport(s: ModelState) {
  const cur = s.providers
    .find((p) => p.id === s.providerName)
    ?.models.find((m) => m.id === s.modelName)
  if (cur) {
    s.imageSupport = !!cur.image_support
  } else if (s.providers.length > 0) {
    // Providers are loaded but the current model isn't among them (e.g. set
    // via automation to an unlisted model) — don't carry the previous model's
    // capability forward.
    s.imageSupport = false
  }
}

const modelSlice = createSlice({
  name: 'model',
  initialState: initialModel,
  reducers: {
    setProvider(s, a: { payload: string }) {
      s.providerName = a.payload
      syncImageSupport(s)
    },
    setModel(s, a: { payload: string }) {
      s.modelName = a.payload
      syncImageSupport(s)
    },
    setSmallModel(s, a: { payload: string }) {
      s.smallModel = a.payload
    },
    setMode(s, a: { payload: AgentMode }) {
      s.mode = a.payload
    },
    setProviders(s, a: { payload: ProviderInfo[] }) {
      s.providers = a.payload
      syncImageSupport(s)
    },
    setModelState(s, a: { payload: { recent: ModelRef[]; favorite: ModelRef[]; effortOverrides?: Record<string, string> } }) {
      s.recentModels = a.payload.recent
      s.favoriteModels = a.payload.favorite.map((r) => `${r.provider}/${r.model}`)
      s.effortOverrides = a.payload.effortOverrides ?? {}
    },
    setFavorite(s, a: { payload: { provider: string; model: string; favorite: boolean } }) {
      const key = `${a.payload.provider}/${a.payload.model}`
      if (a.payload.favorite) {
        if (!s.favoriteModels.includes(key)) s.favoriteModels.push(key)
      } else {
        s.favoriteModels = s.favoriteModels.filter((x) => x !== key)
      }
    },
    setEffortOverride(s, a: { payload: { provider: string; model: string; effort: string } }) {
      const key = `${a.payload.provider}/${a.payload.model}`
      if (a.payload.effort) s.effortOverrides[key] = a.payload.effort
      else delete s.effortOverrides[key]
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
    setMaxIterations(s, a: { payload: number }) {
      s.maxIterations = a.payload
    },
  },
})

// ═══════════════════════════════════════════════════════════════════════════
// App UI slice — view routing, dialog open states
// ═══════════════════════════════════════════════════════════════════════════

// M18: settings is a first-class view (its own full-page surface), not an
// overlay dialog — the bottom-left gear / ⌘, / CloudBadge all route here.
type View = 'chat' | 'automations' | 'channels' | 'automation-run' | 'settings'

// Sections of the settings view. Deep links (e.g. CloudBadge → Cloud) set this
// together with setView('settings').
export type SettingsTab =
  | 'general'
  | 'cloud'
  | 'appearance'
  | 'providers'
  | 'mcp'
  | 'skills'
  | 'memory'
  | 'browser'
  | 'computer'
  | 'ssh'
  | 'channels'
  | 'shortcuts'
  | 'usage'
  | 'developer'

interface UiState {
  activeView: View
  settingsTab: SettingsTab
  paletteOpen: boolean
  needsAuth: boolean
  needsSetup: boolean
  connectionError: string
  theme: string
  channelAvailable: boolean
  channelEnabled: boolean
  bleAvailable: boolean
  bleEnabled: boolean
}

const initialUi: UiState = {
  activeView: 'chat',
  settingsTab: 'general',
  paletteOpen: false,
  needsAuth: false,
  needsSetup: false,
  connectionError: '',
  theme: 'system',
  channelAvailable: false,
  channelEnabled: false,
  bleAvailable: false,
  bleEnabled: false,
}

const uiSlice = createSlice({
  name: 'ui',
  initialState: initialUi,
  reducers: {
    setView(s, a: { payload: View }) {
      s.activeView = a.payload
    },
    setSettingsTab(s, a: { payload: SettingsTab }) {
      s.settingsTab = a.payload
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
    setChannelState(s, a: { payload: { available: boolean; enabled: boolean } }) {
      s.channelAvailable = a.payload.available
      s.channelEnabled = a.payload.enabled
    },
    setBLEState(s, a: { payload: { available: boolean; enabled: boolean } }) {
      s.bleAvailable = a.payload.available
      s.bleEnabled = a.payload.enabled
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
  async (payload: { text: string; images?: import('jcode-ui-core').ChatImage[]; mode?: AgentMode; sessionId?: string; background?: boolean }, { dispatch, getState }) => {
    const state = getState() as RootState
    // sessionId override targets a specific (possibly background) session —
    // used when draining a stashed queue after that session's agentDone.
    const sessionId = payload.sessionId ?? (state.session.currentSessionId || undefined)
    const trimmed = payload.text.trim()
    // Goal flows are foreground-only: the goal API always targets the active
    // engine, so a background queue drain sends its text as a plain message.
    if (!payload.background && state.chat.goalArmed && trimmed && !trimmed.startsWith('/')) {
      dispatch(chatActions.setGoalArmed(false))
      dispatch(chatActions.addMessage({ role: 'user', content: payload.text }))
      const goal = await api.setGoal(trimmed, true)
      dispatch(chatActions.setGoal(goal))
      dispatch(chatActions.setRunning(true))
      return
    }
    // /goal slash interception (matches Vue store.sendMessage).
    if (!payload.background && (trimmed === '/goal' || trimmed.startsWith('/goal '))) {
      const objective = trimmed.slice('/goal'.length).trim()
      if (objective === '' || objective === 'status') {
        const goal = await api.goal()
        dispatch(chatActions.setGoal(goal))
        dispatch(chatActions.addMessage({
          role: 'system',
          content: goal ? `Goal is ${goal.status}: ${goal.objective}` : 'No active goal.',
        }))
        return
      }
      if (objective === 'clear') {
        await api.clearGoal()
        dispatch(chatActions.setGoal(null))
        dispatch(chatActions.addMessage({ role: 'system', content: 'Goal cleared.' }))
        return
      }
      dispatch(chatActions.addMessage({ role: 'user', content: payload.text }))
      const goal = await api.setGoal(objective, true)
      dispatch(chatActions.setGoal(goal))
      dispatch(chatActions.setRunning(true))
      return
    }
    // Foreground sends echo into the visible timeline; a background drain must
    // not touch the conversation the user is currently viewing.
    if (!payload.background) {
      dispatch(chatActions.addMessage({ role: 'user', content: payload.text, images: payload.images }))
      dispatch(chatActions.setRunning(true))
    }
    // First user turn materializes the session on disk — surface it in the
    // sidebar immediately (title + running) before the chat HTTP round-trip.
    if (sessionId) {
      revealSessionInSidebar(dispatch as AppDispatch, () => getState() as RootState, {
        uuid: sessionId,
        title: sessionTitleFromMessage(payload.text),
        running: true,
      })
    }
    const resp = await api.chat(payload.text, payload.mode, sessionId, payload.images)
    const sid = resp.session_id || sessionId
    if (sid) {
      if (!state.session.currentSessionId) dispatch(sessionActions.setCurrentSession(sid))
      revealSessionInSidebar(dispatch as AppDispatch, () => getState() as RootState, {
        uuid: sid,
        title: sessionTitleFromMessage(payload.text),
        running: true,
      })
      // Reconcile with the server index (now written by RecordUser).
      void dispatch(loadSessions())
      void dispatch(loadTasks())
    }
  },
)

/** Match backend generateTitle so the sidebar title doesn't flicker after refresh. */
function sessionTitleFromMessage(content: string): string {
  const first = content.split(/\r?\n/, 1)[0]?.trim() ?? ''
  if (!first) return 'New session'
  const chars = Array.from(first)
  return chars.length > 80 ? chars.slice(0, 80).join('') + '…' : first
}

/**
 * Ensure a session/task appears in the left sidebar immediately.
 * Backend only indexes a session after the first recorded message; empty
 * "new chat" UUIDs are otherwise invisible until the next full reload.
 *
 * Takes dispatch/getState directly (not an RTK thunk) so it can be called
 * from createAsyncThunk without AppDispatch vs ThunkDispatch type friction.
 */
export function revealSessionInSidebar(
  dispatch: AppDispatch,
  getState: () => RootState,
  opts: {
    uuid: string
    title?: string
    running?: boolean
    project?: string
    provider?: string
    model?: string
  },
) {
  if (!opts.uuid) return
  const state = getState()
  const now = new Date().toISOString()
  const project = opts.project || state.session.projectPath || ''
  const existing = state.session.tasks.find((t) => t.uuid === opts.uuid)
  // First non-empty title wins (matches backend generateTitle on first user msg).
  const title = existing?.title || opts.title || ''
  dispatch(sessionActions.upsertTask({
    uuid: opts.uuid,
    project: existing?.project || project,
    created_at: existing?.created_at || now,
    updated_at: now,
    provider: opts.provider || existing?.provider || state.model.providerName || '',
    model: opts.model || existing?.model || state.model.modelName || '',
    title,
    pinned: existing?.pinned ?? false,
    archived: existing?.archived ?? false,
    unread: existing?.unread ?? false,
    status: existing?.status,
    running: opts.running ?? existing?.running ?? false,
  }))
  dispatch(sessionActions.upsertSession({
    uuid: opts.uuid,
    created_at: existing?.created_at || now,
    provider: opts.provider || existing?.provider || state.model.providerName || '',
    model: opts.model || existing?.model || state.model.modelName || '',
    title: title || undefined,
  }))
}

export const stopAgent = createAsyncThunk('chat/stop', async (_, { getState }) => {
  const state = getState() as RootState
  await api.stop(state.session.currentSessionId || undefined)
})

export const resolveApproval = createAsyncThunk(
  'approval/resolve',
  async (payload: { id: string; approved: boolean; approveAll?: boolean }, { dispatch, getState }) => {
    dispatch(chatActions.setApprovalResolving({ id: payload.id, resolving: true }))
    const state = getState() as RootState
    const item = state.chat.timeline.find((i) => i.kind === 'approval' && i.data.id === payload.id)
    const taskId = item?.kind === 'approval' ? (item.data as Approval & { task_id?: string }).task_id : undefined
    try {
      await api.approval(payload.id, payload.approved, payload.approveAll ?? false, taskId)
      dispatch(chatActions.resolveApprovalItem({ id: payload.id, approved: payload.approved }))
    } catch {
      dispatch(chatActions.setApprovalResolving({ id: payload.id, resolving: false }))
    }
  },
)

export const submitAskUser = createAsyncThunk(
  'askUser/submit',
  async (payload: { id: string; answers: import('jcode-ui-core').AskUserAnswer[] }, { dispatch, getState }) => {
    const state = getState() as RootState
    let taskId: string | undefined
    for (const item of state.chat.timeline) {
      if (item.kind === 'tool' && item.data.askUserId === payload.id) {
        taskId = (item.data as ToolCall & { askUserTaskId?: string }).askUserTaskId
        break
      }
    }
    try {
      await api.askUser(payload.id, payload.answers, taskId)
    } catch {
      // surface in timeline as a system message
      dispatch(chatActions.addMessage({ role: 'system', content: 'Failed to submit answer', level: 'error' }))
    }
  },
)

export const editMessage = createAsyncThunk(
  'chat/edit',
  async (payload: { id: string; text: string }, { dispatch, getState }) => {
    const state = getState() as RootState
    const msgIdx = state.chat.timeline.findIndex((i) => i.kind === 'message' && i.data.id === payload.id && i.data.role === 'user')
    if (msgIdx < 0) return
    const beforeUserMessage = state.chat.timeline
      .slice(0, msgIdx)
      .filter((i) => i.kind === 'message' && i.data.role === 'user')
      .length
    try {
      const res = await api.truncateHistory(beforeUserMessage)
      if (res.session_id) dispatch(sessionActions.setCurrentSession(res.session_id))
    } catch (e) {
      dispatch(chatActions.addMessage({
        role: 'system',
        content: e instanceof Error ? e.message : 'Failed to truncate history',
        level: 'error',
      }))
      return
    }
    dispatch(chatActions.truncateTimelineFrom(payload.id))
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

export const loadModels = createAsyncThunk('model/loadModels', async (_, { dispatch }) => {
  const data = await api.models()
  dispatch(modelActions.setProviders(data.providers || []))
  dispatch(modelActions.setProvider(data.current.provider))
  dispatch(modelActions.setModel(data.current.model))
  const provider = data.providers.find((p) => p.id === data.current.provider)
  const model = provider?.models.find((m) => m.id === data.current.model)
  dispatch(modelActions.setImageSupport(!!model?.image_support))
})

export const loadModelState = createAsyncThunk('model/loadState', async (_, { dispatch }) => {
  const data = await api.modelState()
  dispatch(modelActions.setModelState({
    recent: data.recent || [],
    favorite: data.favorite || [],
    effortOverrides: data.effort_overrides || {},
  }))
})

export const loadApprovalMode = createAsyncThunk('model/loadApprovalMode', async (_, { dispatch, getState }) => {
  const data = await api.approvalMode()
  dispatch(modelActions.setAutoApprove(data.auto_approve))
  const state = getState() as RootState
  if (data.auto_approve) dispatch(modelActions.setMode('full_access'))
  else if (state.model.mode === 'full_access') dispatch(modelActions.setMode('approval'))
})

export const loadChannelState = createAsyncThunk('ui/loadChannelState', async (_, { dispatch }) => {
  const data = await api.channelStatus()
  dispatch(uiActions.setChannelState({ available: data.available, enabled: data.state === 'enabled' }))
})

export const loadBLEState = createAsyncThunk('ui/loadBLEState', async (_, { dispatch }) => {
  const data = await api.channelBLEStatus()
  dispatch(uiActions.setBLEState({ available: data.available, enabled: data.enabled }))
})

export const loadConfig = createAsyncThunk('model/loadConfig', async (_, { dispatch }) => {
  const cfg = await api.config()
  dispatch(modelActions.setMaxIterations(cfg.max_iterations))
  dispatch(modelActions.setSmallModel(cfg.small_model ?? ''))
})

export const loadStatus = createAsyncThunk('app/loadStatus', async (_, { dispatch }) => {
  const status = await api.status()
  dispatch(chatActions.setRunning(!!status.running))
  dispatch(sessionActions.setProjectPath(status.pwd))
  dispatch(modelActions.setProvider(status.provider))
  dispatch(modelActions.setModel(status.model))
  dispatch(modelActions.setMode(normalizeMode(status.mode)))
  if (status.token) dispatch(chatActions.setTokenSnapshot(status.token))
})

export const loadGoal = createAsyncThunk('chat/loadGoal', async (_, { dispatch }) => {
  const goal = await api.goal()
  dispatch(chatActions.setGoal(goal))
})

export const loadTodos = createAsyncThunk('chat/loadTodos', async (_, { dispatch }) => {
  const todos = await api.todos()
  dispatch(chatActions.setTodos(todos))
})

export const reconcilePendingInteractions = createAsyncThunk('chat/reconcilePending', async (_, { dispatch, getState }) => {
  const [askResult, approvalResult] = await Promise.allSettled([
    api.askPending(),
    api.approvalPending(),
  ])
  if (askResult.status === 'fulfilled') {
    for (const req of askResult.value) {
      dispatch(chatActions.attachAskUser({
        toolName: 'ask_user',
        askUserId: req.id,
        questions: req.questions,
        taskId: req.task_id,
      }))
    }
  }
  if (approvalResult.status === 'fulfilled') {
    const state = getState() as RootState
    const existing = new Set(
      state.chat.timeline
        .filter((i) => i.kind === 'approval')
        .map((i) => i.kind === 'approval' ? i.data.id : ''),
    )
    for (const req of approvalResult.value) {
      if (existing.has(req.id)) continue
      const approval: Approval = {
        id: req.id,
        tool_name: req.tool_name,
        tool_args: req.tool_args,
        tool_call_id: req.tool_call_id,
        is_external: req.is_external,
      }
      ;(approval as Approval & { task_id?: string }).task_id = req.task_id
      dispatch(chatActions.addApprovalRequest(approval))
      existing.add(req.id)
    }
  }
})

export const loadWorkspaceState = createAsyncThunk('app/loadWorkspaceState', async (_, { dispatch }) => {
  await Promise.allSettled([
    dispatch(loadStatus()),
    dispatch(loadConfig()),
    dispatch(loadModels()),
    dispatch(loadModelState()),
    dispatch(loadSessions()),
    dispatch(loadTasks()),
    dispatch(loadSlashCommands()),
    dispatch(loadApprovalMode()),
    dispatch(loadChannelState()),
    dispatch(loadBLEState()),
    dispatch(loadGoal()),
    dispatch(loadTodos()),
    dispatch(reconcilePendingInteractions()),
  ])
})

/**
 * Load (replay) a session's history into the timeline. Ported from the Vue
 * store's loadSession: fetches the JSONL entries, tells the backend to resume
 * that session, clears the UI, then rebuilds the timeline by walking the entries
 * (user/assistant → messages; tool_call+tool_result → resolved tool calls).
 */
export const loadSession = createAsyncThunk(
  'session/loadOne',
  async (uuid: string, { dispatch, getState }) => {
    // Fetch the session's history. A 404 means the session has no JSONL yet
    // (fresh, never-used session) — return without rebuilding so the caller can
    // fall back to a different session.
    let entries: SessionEntry[]
    try {
      entries = await api.session(uuid)
    } catch {
      return
    }
    // Tell the backend to switch to (resume) this session.
    const resp = await api.newSession(uuid)
    dispatch(sessionActions.setCurrentSession(resp.session_id || uuid))

    // Clear the UI before rebuilding.
    dispatch(chatActions.clearChat())

    // Rebuild the timeline from entries.
    const timeline: ThreadItem[] = []
    const pendingToolCalls = new Map<string, ToolCall>()
    for (const e of entries) {
      if (e.type === 'user' && (e.content || (e.images && e.images.length > 0))) {
        timeline.push({ kind: 'message', seq: nextSeq(), data: { id: genId('msg'), role: 'user', content: e.content || '', timestamp: ts(e.timestamp), images: e.images } })
      } else if (e.type === 'assistant' && e.content) {
        timeline.push({ kind: 'message', seq: nextSeq(), data: { id: genId('asst'), role: 'assistant', content: e.content, timestamp: ts(e.timestamp) } })
      } else if (e.type === 'tool_call' && e.name) {
        const tc: ToolCall = {
          id: genId('tc'),
          toolCallID: e.tool_call_id,
          name: e.name,
          args: e.args || '',
          status: 'running',
          timestamp: ts(e.timestamp),
          displayInfo: extractToolDisplayInfo(e.name, e.args || ''),
          batchId: e.batch_id,
          batchIndex: e.batch_index,
          batchSize: e.batch_size,
          startedAt: e.started_at,
        }
        timeline.push({ kind: 'tool', seq: nextSeq(), data: tc })
        if (e.tool_call_id) pendingToolCalls.set(e.tool_call_id, tc)
      } else if (e.type === 'tool_result') {
        let resolved = false
        if (e.tool_call_id) {
          const tc = pendingToolCalls.get(e.tool_call_id)
          if (tc) {
            tc.output = e.output || ''
            tc.error = e.error || ''
            tc.status = e.error ? 'error' : 'done'
            tc.denied = e.denied || undefined
            if (e.duration_ms !== undefined && tc.meta?.duration_ms === undefined) {
              tc.meta = { ...(tc.meta || {}), duration_ms: e.duration_ms }
            }
            pendingToolCalls.delete(e.tool_call_id)
            resolved = true
          }
        }
        if (!resolved && e.name) {
          for (let i = timeline.length - 1; i >= 0; i--) {
            const item = timeline[i]
            if (item.kind === 'tool' && item.data.name === e.name && item.data.status === 'running') {
              item.data.output = e.output || ''
              item.data.error = e.error || ''
              item.data.status = e.error ? 'error' : 'done'
              item.data.denied = e.denied || undefined
              if (e.duration_ms !== undefined && item.data.meta?.duration_ms === undefined) {
                item.data.meta = { ...(item.data.meta || {}), duration_ms: e.duration_ms }
              }
              break
            }
          }
        }
      }
    }
    // Any tool calls that never got a result (interrupted session) → done.
    for (const tc of pendingToolCalls.values()) tc.status = 'done'

    dispatch(chatActions.setTimeline(timeline))

    // Seed isRunning from the task list (a resumed task may still be running).
    const state = getState() as RootState
    const resumedId = resp.session_id || uuid
    const running = !!state.session.tasks.find((t) => t.uuid === resumedId)?.running
    dispatch(chatActions.setRunning(running))

    // Rehydrate server-truth state for the resumed session (token snapshot,
    // provider/model, mode). clearChat nulled tokenSnapshot, and no
    // token_update arrives until the session's next LLM call — without this
    // the context ring stays hidden after switching conversations.
    await dispatch(loadStatus())
    await dispatch(reconcilePendingInteractions())

    // Refresh goal + todos (the backend restored them; no WS push on switch).
    try {
      const goal = await api.goal()
      dispatch(chatActions.setGoal(goal))
    } catch {
      // ignore
    }
    try {
      const todos = await api.todos()
      dispatch(chatActions.setTodos(todos))
    } catch {
      // ignore
    }
  },
)

/**
 * Start a fresh chat: clear the timeline, switch to the chat view, and ask the
 * backend for a new session id. Shared by the Sidebar "new task" button and the
 * ⌘N / ⇧⌘O keyboard shortcuts. The empty session stays out of the sidebar until
 * the first user message (backend only indexes then).
 */
export const startNewChat = createAsyncThunk('session/startNew', async (_, { dispatch }) => {
  dispatch(chatActions.clearChat())
  dispatch(sessionActions.setCurrentSession(''))
  dispatch(uiActions.setView('chat'))
  try {
    const resp = await api.newSession()
    dispatch(sessionActions.setCurrentSession(resp.session_id))
  } catch {
    // surfaced via health/gate
  }
})

export const replaySession = createAsyncThunk(
  'session/replay',
  async (uuid: string, { dispatch }) => {
    let entries: SessionEntry[]
    try {
      entries = await api.session(uuid)
    } catch (e) {
      dispatch(chatActions.clearChat())
      dispatch(chatActions.addMessage({
        role: 'system',
        content: e instanceof Error ? e.message : 'Failed to load session replay',
        level: 'error',
      }))
      return
    }

    dispatch(chatActions.clearChat())
    const timeline: ThreadItem[] = []
    const pendingToolCalls = new Map<string, ToolCall>()
    for (const e of entries) {
      if (e.type === 'user' && (e.content || (e.images && e.images.length > 0))) {
        timeline.push({ kind: 'message', seq: nextSeq(), data: { id: genId('msg'), role: 'user', content: e.content || '', timestamp: ts(e.timestamp), images: e.images } })
      } else if (e.type === 'assistant' && e.content) {
        timeline.push({ kind: 'message', seq: nextSeq(), data: { id: genId('asst'), role: 'assistant', content: e.content, timestamp: ts(e.timestamp) } })
      } else if (e.type === 'tool_call' && e.name) {
        const tc: ToolCall = {
          id: genId('tc'),
          toolCallID: e.tool_call_id,
          name: e.name,
          args: e.args || '',
          status: 'running',
          timestamp: ts(e.timestamp),
          displayInfo: extractToolDisplayInfo(e.name, e.args || ''),
          batchId: e.batch_id,
          batchIndex: e.batch_index,
          batchSize: e.batch_size,
          startedAt: e.started_at,
        }
        timeline.push({ kind: 'tool', seq: nextSeq(), data: tc })
        if (e.tool_call_id) pendingToolCalls.set(e.tool_call_id, tc)
      } else if (e.type === 'tool_result') {
        let resolved = false
        if (e.tool_call_id) {
          const tc = pendingToolCalls.get(e.tool_call_id)
          if (tc) {
            tc.output = e.output || ''
            tc.error = e.error || ''
            tc.status = e.error ? 'error' : 'done'
            tc.denied = e.denied || undefined
            if (e.duration_ms !== undefined && tc.meta?.duration_ms === undefined) {
              tc.meta = { ...(tc.meta || {}), duration_ms: e.duration_ms }
            }
            pendingToolCalls.delete(e.tool_call_id)
            resolved = true
          }
        }
        if (!resolved && e.name) {
          for (let i = timeline.length - 1; i >= 0; i--) {
            const item = timeline[i]
            if (item.kind === 'tool' && item.data.name === e.name && item.data.status === 'running') {
              item.data.output = e.output || ''
              item.data.error = e.error || ''
              item.data.status = e.error ? 'error' : 'done'
              item.data.denied = e.denied || undefined
              if (e.duration_ms !== undefined && item.data.meta?.duration_ms === undefined) {
                item.data.meta = { ...(item.data.meta || {}), duration_ms: e.duration_ms }
              }
              break
            }
          }
        }
      }
    }
    for (const tc of pendingToolCalls.values()) tc.status = 'done'
    dispatch(chatActions.setTimeline(timeline))
    dispatch(chatActions.setRunning(false))
  },
)

function ts(t?: string): number {
  return t ? new Date(t).getTime() : Date.now()
}

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
