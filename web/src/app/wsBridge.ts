/**
 * WS bridge — wires WebSocket events to Redux dispatches.
 *
 * This replaces the Vue App.vue's inline WS→store coupling (lines 156-206).
 * It's a module-level singleton: created once at boot, reads the active task id
 * from the store via a getter (so it stays current without re-subscribing), and
 * dispatches the matching action for each event type.
 */

import { dispatchWSHandler, type WSClient, type WSHandlers } from '../lib/ws'
import type { AppDispatch, RootState } from './store'
import {
  chatActions,
  sessionActions,
  remoteConnectionActions,
  modelRetryActions,
  modelActions,
  sendMessage,
  loadTasks,
  loadSessions,
  loadSession,
  hasToolLifecycleHost,
  bufferPendingConversationEvent,
} from './store'
import { api } from '../lib/api'
import type { Approval, Goal } from 'jcode-ui-core'
import { normalizeMode, type AgentDoneData } from '../lib/types'
import { i18n } from '../i18n'
import { normalizeWireLifecycle } from './toolLifecycle'

const pendingLifecycleRefreshes = new Map<string, Promise<unknown>>()

/** Create the handler set for a given store getter + dispatch. The handlers read
 *  fresh state (active task id) so they don't capture stale closures. */
export function createWSHandlers(
  getState: () => RootState,
  dispatch: AppDispatch,
): WSHandlers {
  const applyForegroundAgentDone = (data?: AgentDoneData, taskId?: string) => {
    const effectiveTaskId = taskId || getState().session.currentSessionId
    // A remote transport failure has its own task-scoped, actionable status
    // strip. Do not duplicate it as a permanent model-error card in the chat.
    const structuredRemoteError = data?.error_kind === 'remote_connection' || (!!data?.code && (
      data.code.startsWith('ssh_') || data.code.startsWith('docker_') || data.code === 'remote_connection_failed'
    ))
    const suppressRemoteError = !!data?.error && structuredRemoteError
    if (effectiveTaskId && suppressRemoteError) {
      const previous = getState().remoteConnection.byTaskId[effectiveTaskId]
      const outcomeUnknown = data?.phase === 'outcome_unknown'
      const actionRequired = outcomeUnknown || data?.code === 'ssh_auth_required' ||
        data?.code === 'ssh_host_key_unknown' || data?.code === 'ssh_host_key_changed' ||
        data?.code === 'ssh_host_key_confirmation_mismatch'
      dispatch(remoteConnectionActions.statusReceived({
        task_id: effectiveTaskId,
        kind: data?.kind || previous?.kind || 'ssh',
        status: actionRequired ? 'action_required' : 'failed',
        attempt: previous?.attempt || 0,
        max_attempts: previous?.max_attempts || 0,
        host: previous?.host,
        error: data?.detail || data?.error,
        code: outcomeUnknown ? 'remote_outcome_unknown' : data?.code,
        retryable: outcomeUnknown ? false : data?.retryable,
      }))
    }
    if (effectiveTaskId && data?.stopped) {
      dispatch(remoteConnectionActions.clearTransient(effectiveTaskId))
    }
    dispatch(chatActions.agentDone(data
      ? {
          error: suppressRemoteError ? undefined : data.error,
          detail: suppressRemoteError ? undefined : data.detail,
          stopped: data.stopped,
        }
      : undefined))
  }

  const applyAgentDoneBackgroundEffects = (taskId: string | undefined, background: boolean) => {
    // These effects belong to the completed task even when its foreground
    // transcript is still pending or the user later cancels navigation.
    if (taskId) dispatch(modelRetryActions.clearWaiting(taskId))
    void dispatch(loadTasks() as never)
    void dispatch(loadSessions() as never)
    const queued = taskId ? getState().chat.queuedBySession[taskId] : undefined
    if (!taskId || !queued || queued.length === 0) return
    const next = queued[0]
    dispatch(chatActions.shiftQueued(taskId))
    void dispatch(sendMessage({
      text: next.text,
      images: next.images,
      sessionId: taskId,
      background,
    }) as never)
  }

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
      refresh = Promise.resolve(dispatch(loadSession({ uuid: taskID, background: true }))).then(
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

  const handlers: WSHandlers = {
    activeTaskId: () => getState().session.currentSessionId || undefined,
    pendingTaskId: () => {
      const load = getState().conversationLoad
      return load.phase !== 'idle' && load.historyStatus === 'ready'
        ? load.target?.uuid
        : undefined
    },
    onPendingTaskEvent: (event) => {
      const load = getState().conversationLoad
      if (!load.target || load.target.uuid !== event.taskId || load.historyStatus !== 'ready') return
      if (event.type === 'agent_done') {
        const data = (event.data && typeof event.data === 'object'
          ? event.data
          : {}) as AgentDoneData
        // Drain metadata/type-ahead now so Cancel cannot discard task-owned
        // side effects. Buffer only the foreground timeline completion; using
        // dispatchWSHandler here would drain the queue a second time on commit.
        applyAgentDoneBackgroundEffects(event.taskId, true)
        bufferPendingConversationEvent(load.requestId, event.taskId, () => {
          applyForegroundAgentDone(data, event.taskId)
        })
        return
      }
      bufferPendingConversationEvent(load.requestId, event.taskId, () => {
        dispatchWSHandler(handlers, event.type, event.data)
      })
    },
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
        applyForegroundAgentDone(d, taskId || activeId)
      }
      const key = taskId || activeId
      applyAgentDoneBackgroundEffects(key, !isForeground)
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
      // Local Web/Desktop sends are inserted optimistically by sendMessage.
      // The backend still emits them for the durable Cloud relay, tagged as a
      // local echo; replaying that frame here would show the turn twice.
      if (d.local_echo) return
      dispatch(chatActions.addMessage({ role: 'user', content: d.content, source: d.source }))
      dispatch(chatActions.setRunning(true))
    },
    onRemoteConnectionStatus: (d) => {
      dispatch(remoteConnectionActions.statusReceived(d))
    },
    onModelRetryStatus: (d) => {
      dispatch(modelRetryActions.statusReceived(d))
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
  return handlers
}

/** Wire a WSClient to the store. Returns the client (already connecting). */
export function bridgeWS(client: WSClient, getState: () => RootState, dispatch: AppDispatch): WSClient {
  client.setHandlers(createWSHandlers(getState, dispatch))
  client.connect()
  return client
}
