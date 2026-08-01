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

  constructor(_url: string, _protocols?: string[]) {
    FakeWebSocket.instances.push(this)
  }

  send() {}
  close() {}
}

afterEach(() => {
  FakeWebSocket.instances = []
  vi.unstubAllGlobals()
})

describe('WSClient artifact routing', () => {
  it('delivers background artifact metadata with its task id', () => {
    vi.stubGlobal('WebSocket', FakeWebSocket)
    const onArtifactUpserted = vi.fn()
    const client = new WSClient({ activeTaskId: () => 'task-active', onArtifactUpserted })
    client.connect()

    const socket = FakeWebSocket.instances[0]
    socket.onmessage?.({ data: JSON.stringify({
      type: 'artifact_upserted',
      task_id: 'task-background',
      data: { artifact_id: 'artifact-2', focus: true },
    }) })

    expect(onArtifactUpserted).toHaveBeenCalledWith({
      task_id: 'task-background', artifact_id: 'artifact-2', focus: true,
    })
    client.disconnect()
  })
})
