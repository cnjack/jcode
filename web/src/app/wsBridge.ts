/**
 * WS bridge — wires WebSocket events to Redux dispatches.
 *
 * This replaces the Vue App.vue's inline WS→store coupling (lines 156-206).
 * It's a module-level singleton: created once at boot, reads the active task id
 * from the store via a getter (so it stays current without re-subscribing), and
 * dispatches the matching action for each event type.
 */

import type { WSClient, WSHandlers } from '../lib/ws'
import type { AppDispatch, RootState } from './store'
import {
  chatActions,
  sessionActions,
  modelActions,
  sendMessage,
  loadTasks,
  loadSessions,
} from './store'
import { api } from '../lib/api'
import type { Approval, Goal } from 'jcode-ui-core'
import { normalizeMode } from '../lib/types'

/** Create the handler set for a given store getter + dispatch. The handlers read
 *  fresh state (active task id) so they don't capture stale closures. */
export function createWSHandlers(
  getState: () => RootState,
  dispatch: AppDispatch,
): WSHandlers {
  return {
    activeTaskId: () => getState().session.currentSessionId || undefined,
    onConnectionChange: (connected) => dispatch(sessionActions.setWsConnected(connected)),
    onAgentStart: () => dispatch(chatActions.setRunning(true)),
    onAgentText: (d) => dispatch(chatActions.appendAgentText(d.text)),
    onToolCall: (d) =>
      dispatch(
        chatActions.addToolCall({
          name: d.name,
          args: d.args,
          toolCallID: d.tool_call_id,
          displayInfo: d.display_info,
          batchId: d.batch_id,
          batchIndex: d.batch_index,
          batchSize: d.batch_size,
          startedAt: d.started_at,
        }),
      ),
    onToolResult: (d) =>
      dispatch(
        chatActions.resolveToolCall({
          name: d.name,
          toolCallID: d.tool_call_id,
          output: d.output,
          displayOutput: d.display_output,
          error: d.error,
          denied: d.denied,
          durationMs: d.duration_ms,
          streams: d.streams,
          meta: d.meta,
          presentation: d.presentation,
        }),
      ),
    onTokenUpdate: (d) => dispatch(chatActions.setTokenSnapshot(d)),
    onAgentDone: (d) => {
      // agent_done arrives for EVERY session (the ws client lets it through the
      // foreground filter) so a background session's type-ahead queue can drain
      // while the user is viewing another conversation. Foreground-only state
      // (timeline, isRunning) is touched only when the done matches the view.
      const taskId = d?.task_id
      const activeId = getState().session.currentSessionId
      const isForeground = !taskId || taskId === activeId
      if (isForeground) {
        dispatch(chatActions.agentDone(d ? { error: d.error, detail: d.detail, stopped: d.stopped } : undefined))
      }
      // Refresh sidebar metadata (title / updated_at / running) after a turn.
      void dispatch(loadTasks() as never)
      void dispatch(loadSessions() as never)
      // Drain one queued type-ahead message (terminal-style) from the session
      // that just finished — wherever the user is currently looking.
      const key = taskId || activeId
      const queued = key ? getState().chat.queuedBySession[key] : undefined
      if (key && queued && queued.length > 0) {
        const next = queued[0]
        dispatch(chatActions.shiftQueued(key))
        void dispatch(sendMessage({ text: next.text, images: next.images, sessionId: key, background: !isForeground }) as never)
      }
    },
    onTodoUpdate: () => {
      void api.todos().then((todos) => dispatch(chatActions.setTodos(todos)))
    },
    onGoalUpdate: (d) => dispatch(chatActions.setGoal(d as Goal | null)),
    onApprovalRequest: (d) =>
      dispatch(
        chatActions.addApprovalRequest({
          id: d.id,
          tool_name: d.tool_name,
          tool_args: d.tool_args,
          tool_call_id: d.tool_call_id,
          is_external: d.is_external,
          task_id: d.task_id,
        } as Approval & { task_id?: string }),
      ),
    onAskUserRequest: (d) =>
      dispatch(
        chatActions.attachAskUser({
          toolName: 'ask_user',
          askUserId: d.id,
          questions: d.questions,
          taskId: d.task_id,
        }),
      ),
    onModelChanged: (d) => {
      dispatch(modelActions.setProvider(d.provider))
      dispatch(modelActions.setModel(d.model))
    },
    onModeChanged: (d) => {
      const mode = normalizeMode(d.mode)
      dispatch(modelActions.setMode(mode))
      dispatch(modelActions.setAutoApprove(mode === 'full_access'))
    },
    onApprovalModeChanged: (d) => {
      dispatch(modelActions.setAutoApprove(d.auto_approve))
      if (d.auto_approve) dispatch(modelActions.setMode('full_access'))
      else if (getState().model.mode === 'full_access') dispatch(modelActions.setMode('approval'))
    },
    onSubagentProgress: (d) =>
      dispatch(chatActions.addSubagentProgress({
        event: d.event,
        toolName: d.tool_name,
        detail: d.detail,
      })),
    onUserMessage: (d) => {
      dispatch(chatActions.addMessage({ role: 'user', content: d.content, source: d.source }))
      dispatch(chatActions.setRunning(true))
    },
    onTaskStatus: (taskId, running, project, updatedAt) => {
      dispatch(sessionActions.setTaskRunning({ taskId, running }))
      // A status flip means real activity (a turn started/ended) — the server
      // bumps the project-level timestamp in the same write and sends both the
      // project path and its exact timestamp, so mirror them with the SERVER's
      // values (never the browser clock, which may be skewed). Fall back to the
      // local task list only for older servers that omit the fields.
      const path = project || getState().session.tasks.find((t) => t.uuid === taskId)?.project
      if (path) {
        dispatch(sessionActions.touchProjectTime({ path, ts: updatedAt || new Date().toISOString() }))
      }
    },
    onSessionReset: () => dispatch(chatActions.clearChat()),
  }
}

/** Wire a WSClient to the store. Returns the client (already connecting). */
export function bridgeWS(client: WSClient, getState: () => RootState, dispatch: AppDispatch): WSClient {
  client.setHandlers(createWSHandlers(getState, dispatch))
  client.connect()
  return client
}
