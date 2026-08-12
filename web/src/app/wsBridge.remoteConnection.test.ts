import { describe, expect, it, vi } from 'vitest'
import type { AppDispatch, RootState } from './store'
import { createWSHandlers } from './wsBridge'

function baseState(status: 'waiting' | 'failed'): RootState {
  return {
    session: { currentSessionId: 'task-1', tasks: [], sessions: [] },
    chat: { queuedBySession: {}, timeline: [] },
    conversationLoad: { phase: 'idle' },
    remoteConnection: {
      byTaskId: {
        'task-1': {
          task_id: 'task-1',
          kind: 'ssh',
          status,
          attempt: 2,
          max_attempts: 8,
          revision: 1,
        },
      },
    },
  } as unknown as RootState
}

describe('remote connection WebSocket bridge', () => {
  it('dispatches task-scoped status into the dedicated slice', () => {
    const dispatch = vi.fn()
    const handlers = createWSHandlers(() => baseState('waiting'), dispatch as unknown as AppDispatch)

    handlers.onRemoteConnectionStatus?.({
      task_id: 'task-1', kind: 'ssh', status: 'waiting', attempt: 2, max_attempts: 8,
    })

    expect(dispatch).toHaveBeenCalledWith(expect.objectContaining({
      type: 'remoteConnection/statusReceived',
      payload: expect.objectContaining({ task_id: 'task-1', status: 'waiting' }),
    }))
  })

  it('clears only transient recovery when a foreground turn is stopped', () => {
    const dispatch = vi.fn()
    const handlers = createWSHandlers(() => baseState('waiting'), dispatch as unknown as AppDispatch)

    handlers.onAgentDone?.({ task_id: 'task-1', stopped: true })

    expect(dispatch).toHaveBeenCalledWith({ type: 'remoteConnection/clearTransient', payload: 'task-1' })
  })

  it('does not suppress an unstructured model error just because a failed notice remains', () => {
    const state = baseState('failed')
    const dispatch = vi.fn()
    const handlers = createWSHandlers(() => state, dispatch as unknown as AppDispatch)

    handlers.onAgentDone?.({ task_id: 'task-1', error: 'model request failed' })
    expect(dispatch).toHaveBeenCalledWith(expect.objectContaining({
      type: 'chat/agentDone', payload: expect.objectContaining({ error: 'model request failed' }),
    }))
  })

  it('uses structured agent_done metadata to suppress only remote transport errors', () => {
    const state = baseState('failed')
    const dispatch = vi.fn()
    const handlers = createWSHandlers(() => state, dispatch as unknown as AppDispatch)

    handlers.onAgentDone?.({
      task_id: 'task-1',
      error: 'connection exhausted',
      code: 'ssh_connection_failed',
      error_kind: 'remote_connection',
      kind: 'ssh',
      phase: 'before_dispatch',
      retryable: true,
    })

    expect(dispatch).toHaveBeenCalledWith(expect.objectContaining({
      type: 'chat/agentDone', payload: expect.objectContaining({ error: undefined }),
    }))
  })

  it('overrides a recovered status when the remote command outcome is unknown', () => {
    const state = baseState('failed')
    state.remoteConnection.byTaskId['task-1'].status = 'ready'
    const dispatch = vi.fn()
    const handlers = createWSHandlers(() => state, dispatch as unknown as AppDispatch)

    handlers.onAgentDone?.({
      task_id: 'task-1',
      error: 'remote command outcome is unknown',
      detail: 'connection dropped after exec request was dispatched',
      code: 'ssh_connection_failed',
      error_kind: 'remote_connection',
      kind: 'ssh',
      phase: 'outcome_unknown',
      retryable: true,
    })

    expect(dispatch).toHaveBeenCalledWith(expect.objectContaining({
      type: 'remoteConnection/statusReceived',
      payload: expect.objectContaining({
        task_id: 'task-1', status: 'action_required', code: 'remote_outcome_unknown', retryable: false,
      }),
    }))
    expect(dispatch).toHaveBeenCalledWith(expect.objectContaining({
      type: 'chat/agentDone', payload: expect.objectContaining({ error: undefined }),
    }))
  })
})
