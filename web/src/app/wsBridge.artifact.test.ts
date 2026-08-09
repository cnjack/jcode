import { afterEach, describe, expect, it, vi } from 'vitest'
import type { AppDispatch, RootState } from './store'
import { createWSHandlers } from './wsBridge'

afterEach(() => {
  vi.restoreAllMocks()
})

function handlers(activeTask = 'task-active') {
  const dispatchMock = vi.fn()
  const dispatch = dispatchMock as unknown as AppDispatch
  const getState = () => ({
    session: { currentSessionId: activeTask, tasks: [] },
  }) as unknown as RootState
  return { dispatch: dispatchMock, handlers: createWSHandlers(getState, dispatch) }
}

describe('artifact WebSocket bridge', () => {
  it('focuses an artifact only when it belongs to the active task', () => {
    const { handlers: wsHandlers } = handlers()
    const received: unknown[] = []
    const listener = (event: Event) => received.push((event as CustomEvent).detail)
    window.addEventListener('jcode:artifact-upserted', listener)

    wsHandlers.onArtifactUpserted?.({
      task_id: 'task-active', artifact_id: 'artifact-1', focus: true,
    })
    wsHandlers.onArtifactUpserted?.({
      task_id: 'task-background', artifact_id: 'artifact-2', focus: true,
    })

    window.removeEventListener('jcode:artifact-upserted', listener)
    expect(received).toEqual([{ task_id: 'task-active', artifact_id: 'artifact-1', focus: true }])
  })

  it('refreshes task metadata for a background artifact without stealing focus', () => {
    const { dispatch, handlers: wsHandlers } = handlers()
    wsHandlers.onArtifactUpserted?.({
      task_id: 'task-background', artifact_id: 'artifact-2', focus: true,
    })
    expect(dispatch).toHaveBeenCalledTimes(1)
    expect(typeof dispatch.mock.calls[0][0]).toBe('function')
  })

  it('keeps a generated managed artifact unseen without auto-opening the active panel', () => {
    const { dispatch, handlers: wsHandlers } = handlers()
    const listener = vi.fn()
    window.addEventListener('jcode:artifact-upserted', listener)
    wsHandlers.onArtifactUpserted?.({
      task_id: 'task-active', artifact_id: 'generated-managed', focus: false,
    })
    window.removeEventListener('jcode:artifact-upserted', listener)

    expect(listener).not.toHaveBeenCalled()
    expect(dispatch).toHaveBeenCalledTimes(1)
    expect(typeof dispatch.mock.calls[0][0]).toBe('function')
  })
})
