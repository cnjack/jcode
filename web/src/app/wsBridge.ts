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
  loadSession,
  hasToolLifecycleHost,
} from './store'
import { api } from '../lib/api'
import type { Approval, Goal } from 'jcode-ui-core'
import { normalizeMode } from '../lib/types'
import { i18n } from '../i18n'
import { normalizeWireLifecycle } from './toolLifecycle'

const pendingLifecycleRefreshes = new Map<string, Promise<unknown>>()

/** Create the handler set for a given store getter + dispatch. The handlers read
 *  fresh state (active task id) so they don't capture stale closures. */
export function createWSHandlers(
  getState: () => RootState,
  dispatch: AppDispatch,
): WSHandlers {
  const refreshMissingLifecycleHost = (
    toolCallID: string,
    operationID: string | undefined,
    name: string | undefined,
    apply: () => void,
  ) => {
    const taskID = getState().session.currentSessionId
    if (!taskID) return
    const key = `${taskID}\u0000${toolCallID}\u0000${operationID || ''}`
    let refresh = pendingLifecycleRefreshes.get(key)
    if (!refresh) {
      refresh = Promise.resolve(dispatch(loadSession(taskID))).then(
        () => undefined,
        () => undefined,
      )
      pendingLifecycleRefreshes.set(key, refresh)
      void refresh.finally(() => {
        if (pendingLifecycleRefreshes.get(key) === refresh) pendingLifecycleRefreshes.delete(key)
      })
    }
    // Multiple progress/result frames may arrive while the one refresh is in
    // flight. Give each frame a chance to attach after replay rebuilt the
    // missing occurrence; do not let the first frame suppress later terminal
    // evidence for the same operation.
    void refresh.then(() => {
      const state = getState()
      if (
        state.session.currentSessionId === taskID &&
        hasToolLifecycleHost(state.chat.timeline, toolCallID, operationID, name)
      ) apply()
    })
  }

  return {
    activeTaskId: () => getState().session.currentSessionId || undefined,
    onConnectionChange: (connected) => dispatch(sessionActions.setWsConnected(connected)),
    onAgentStart: () => dispatch(chatActions.setRunning(true)),
    onAgentText: (d) => dispatch(chatActions.appendAgentText(d.text)),
    onToolCall: (d) => {
      const lifecycle = normalizeWireLifecycle(d.phase)
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
          surface: d.surface,
          phase: lifecycle.phase,
          operationID: d.operation_id,
        }),
      )
    },
    onToolProgress: (d) => {
      const lifecycle = normalizeWireLifecycle(d.phase)
      const action = chatActions.progressToolCall({
        name: d.name,
        toolCallID: d.tool_call_id,
        operationID: d.operation_id,
        phase: lifecycle.phase,
        outcome: lifecycle.outcome,
        errorCode: d.error_code,
        provider: d.provider,
        model: d.model,
        artifacts: d.artifacts,
      })
      if (hasToolLifecycleHost(getState().chat.timeline, d.tool_call_id, d.operation_id, d.name)) {
        dispatch(action)
        return
      }
      // A progress event without its initial tool_call must not bind to an old
      // terminal occurrence that happens to reuse the same model-supplied ID.
      // Refresh once per operation and re-apply the frame only if replay finds
      // its concrete occurrence.
      refreshMissingLifecycleHost(d.tool_call_id, d.operation_id, d.name, () => dispatch(action))
    },
    onToolResult: (d) => {
      const lifecycle = normalizeWireLifecycle(d.phase, d.outcome)
      const action = chatActions.resolveToolCall({
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
        operationID: d.operation_id,
        phase: lifecycle.phase,
        outcome: lifecycle.outcome,
        errorCode: d.error_code,
        provider: d.provider,
        model: d.model,
        artifacts: d.artifacts,
      })
      const typedLifecycle = d.name === 'generate_image' || !!d.operation_id || !!d.outcome || !!d.artifacts?.length
      if (
        d.tool_call_id &&
        typedLifecycle &&
        !hasToolLifecycleHost(getState().chat.timeline, d.tool_call_id, d.operation_id, d.name)
      ) {
        refreshMissingLifecycleHost(d.tool_call_id, d.operation_id, d.name, () => dispatch(action))
        return
      }
      dispatch(action)
    },
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
          approvalClass: d.approval_class,
          options: d.options,
          billableSummary: d.billable_summary,
          resolvedOptionId: d.resolved_option_id,
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
    onAgentChanged: (d) => {
      dispatch(modelActions.setAgent(d.agent || ''))
      // Only show the notice when a conversation is active (timeline non-empty).
      // On the welcome screen the user hasn't started yet — adding a message
      // would replace the welcome hero with a near-empty conversation.
      if (getState().chat.timeline.length > 0) {
        dispatch(chatActions.addMessage({
          role: 'system',
          content: d.agent
            ? i18n.t('chat.agent.changedTo', { name: d.agent })
            : i18n.t('chat.agent.changedToDefault'),
          level: 'notice',
        }))
      }
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
    onArtifactUpserted: (d) => {
      // Artifact metadata updates the sidebar for every task, but only an
      // explicitly focused artifact from the foreground task may open UI.
      void dispatch(loadTasks() as never)
      const artifactID = d.artifact_id || d.id
      if (d.task_id === getState().session.currentSessionId && d.focus !== false && artifactID) {
        window.dispatchEvent(new CustomEvent('jcode:artifact-upserted', {
          detail: { ...d, artifact_id: artifactID },
        }))
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
