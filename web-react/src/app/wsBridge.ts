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
    onAgentStart: () => dispatch(chatActions.setRunning(true)),
    onAgentText: (d) => dispatch(chatActions.appendAgentText(d.text)),
    onToolCall: (d) =>
      dispatch(
        chatActions.addToolCall({
          name: d.name,
          args: d.args,
          toolCallID: d.tool_call_id,
          displayInfo: d.display_info,
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
        }),
      ),
    onTokenUpdate: (d) => dispatch(chatActions.setTokenSnapshot(d)),
    onAgentDone: (d) => {
      dispatch(chatActions.agentDone(d ? { error: d.error } : undefined))
      // Drain one queued type-ahead message (terminal-style), if any.
      const queued = getState().chat.queued
      if (queued.length > 0) {
        const next = queued[0]
        dispatch(chatActions.drainQueue())
        void dispatch(sendMessage({ text: next.text, images: next.images }) as never)
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
    onTaskStatus: (taskId, running) => dispatch(sessionActions.setTaskRunning({ taskId, running })),
    onSessionReset: () => dispatch(chatActions.clearChat()),
  }
}

/** Wire a WSClient to the store. Returns the client (already connecting). */
export function bridgeWS(client: WSClient, getState: () => RootState, dispatch: AppDispatch): WSClient {
  client.setHandlers(createWSHandlers(getState, dispatch))
  client.connect()
  return client
}
