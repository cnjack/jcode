import { afterEach, describe, expect, it } from 'vitest'
import { modelRetryActions, store } from './store'

afterEach(() => store.dispatch(modelRetryActions.reset()))

describe('model retry state', () => {
  it('isolates tasks, guards stale clears, and clears an exhausted waiting retry', () => {
    store.dispatch(modelRetryActions.statusReceived({
      task_id: 'task-a', status: 'waiting', attempt: 1, max_attempts: 5, retry_in_ms: 1_250,
    }))
    store.dispatch(modelRetryActions.statusReceived({
      task_id: 'task-b', status: 'ready', attempt: 2, max_attempts: 5,
    }))
    store.dispatch(modelRetryActions.statusReceived({
      task_id: 'task-a', status: 'waiting', attempt: 2, max_attempts: 5, retry_in_ms: 2_000,
    }))

    store.dispatch(modelRetryActions.clear({ taskId: 'task-a', revision: 1 }))
    expect(store.getState().modelRetry.byTaskId['task-a'].revision).toBe(2)
    expect(store.getState().modelRetry.byTaskId['task-b'].status).toBe('ready')

    store.dispatch(modelRetryActions.clearWaiting('task-a'))
    expect(store.getState().modelRetry.byTaskId['task-a']).toBeUndefined()
    store.dispatch(modelRetryActions.clearWaiting('task-b'))
    expect(store.getState().modelRetry.byTaskId['task-b'].status).toBe('ready')
  })
})
