import { afterEach, describe, expect, it } from 'vitest'
import { remoteConnectionActions, store } from './store'

afterEach(() => store.dispatch(remoteConnectionActions.reset()))

describe('remote connection state', () => {
  it('isolates task records, normalizes the compatibility delay, and guards stale clears', () => {
    store.dispatch(remoteConnectionActions.statusReceived({
      task_id: 'task-a', kind: 'ssh', status: 'waiting', attempt: 1, max_attempts: 8, retry_after_ms: 1_250,
    }))
    store.dispatch(remoteConnectionActions.statusReceived({
      task_id: 'task-b', kind: 'docker', status: 'ready', attempt: 2, max_attempts: 3,
    }))

    const first = store.getState().remoteConnection
    expect(first.byTaskId['task-a'].retry_in_ms).toBe(1_250)
    expect(first.byTaskId['task-a'].revision).toBe(1)
    expect(first.byTaskId['task-b'].kind).toBe('docker')

    store.dispatch(remoteConnectionActions.statusReceived({
      task_id: 'task-a', kind: 'ssh', status: 'reconnecting', attempt: 2, max_attempts: 8,
    }))
    store.dispatch(remoteConnectionActions.clear({ taskId: 'task-a', revision: 1 }))
    expect(store.getState().remoteConnection.byTaskId['task-a'].revision).toBe(2)
  })
})
