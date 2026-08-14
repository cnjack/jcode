import { afterEach, describe, expect, it, vi } from 'vitest'
import { WSClient } from './ws'

class FakeWebSocket {
  static OPEN = 1
  static instances: FakeWebSocket[] = []
  readyState = FakeWebSocket.OPEN
  onopen: (() => void) | null = null
  onmessage: ((event: { data: string }) => void) | null = null
  onerror: (() => void) | null = null
  onclose: (() => void) | null = null
  constructor(_url: string, _protocols?: string[]) { FakeWebSocket.instances.push(this) }
  send() {}
  close() {}
}

afterEach(() => {
  FakeWebSocket.instances = []
  vi.unstubAllGlobals()
})

describe('WSClient remote connection routing', () => {
  it('merges and preserves a background task id for model retry status', () => {
    vi.stubGlobal('WebSocket', FakeWebSocket)
    const onModelRetryStatus = vi.fn()
    const client = new WSClient({ activeTaskId: () => 'task-active', onModelRetryStatus })
    client.connect()

    FakeWebSocket.instances[0].onmessage?.({ data: JSON.stringify({
      type: 'model_retry_status',
      task_id: 'task-background',
      data: { status: 'waiting', attempt: 1, max_attempts: 5, retry_in_ms: 500 },
    }) })

    expect(onModelRetryStatus).toHaveBeenCalledWith({
      task_id: 'task-background', status: 'waiting', attempt: 1, max_attempts: 5, retry_in_ms: 500,
    })
    client.disconnect()
  })

  it('merges the envelope task id into an active-task status', () => {
    vi.stubGlobal('WebSocket', FakeWebSocket)
    const onRemoteConnectionStatus = vi.fn()
    const client = new WSClient({ activeTaskId: () => 'task-active', onRemoteConnectionStatus })
    client.connect()

    FakeWebSocket.instances[0].onmessage?.({ data: JSON.stringify({
      type: 'remote_connection_status',
      task_id: 'task-active',
      data: { kind: 'ssh', status: 'waiting', attempt: 2, max_attempts: 8, retry_in_ms: 500 },
    }) })

    expect(onRemoteConnectionStatus).toHaveBeenCalledWith({
      task_id: 'task-active', kind: 'ssh', status: 'waiting', attempt: 2, max_attempts: 8, retry_in_ms: 500,
    })
    client.disconnect()
  })

  it('buffers a pending target status and preserves unrelated task-scoped status', () => {
    vi.stubGlobal('WebSocket', FakeWebSocket)
    const onPendingTaskEvent = vi.fn()
    const onRemoteConnectionStatus = vi.fn()
    const client = new WSClient({
      activeTaskId: () => 'task-active',
      pendingTaskId: () => 'task-pending',
      onPendingTaskEvent,
      onRemoteConnectionStatus,
    })
    client.connect()

    const socket = FakeWebSocket.instances[0]
    socket.onmessage?.({ data: JSON.stringify({
      type: 'remote_connection_status',
      task_id: 'task-pending',
      data: { kind: 'ssh', status: 'reconnecting', attempt: 3, max_attempts: 8 },
    }) })
    socket.onmessage?.({ data: JSON.stringify({
      type: 'remote_connection_status',
      task_id: 'task-background',
      data: { kind: 'ssh', status: 'failed', attempt: 8, max_attempts: 8 },
    }) })

    expect(onPendingTaskEvent).toHaveBeenCalledWith({
      type: 'remote_connection_status',
      taskId: 'task-pending',
      data: { task_id: 'task-pending', kind: 'ssh', status: 'reconnecting', attempt: 3, max_attempts: 8 },
    })
    expect(onRemoteConnectionStatus).toHaveBeenCalledTimes(1)
    expect(onRemoteConnectionStatus).toHaveBeenCalledWith({
      task_id: 'task-background', kind: 'ssh', status: 'failed', attempt: 8, max_attempts: 8,
    })
    client.disconnect()
  })
})
