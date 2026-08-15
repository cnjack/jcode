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
  it('keeps legacy all-task delivery when no active-task resolver is configured', () => {
    vi.stubGlobal('WebSocket', FakeWebSocket)
    const onToolCall = vi.fn()
    const client = new WSClient({ onToolCall })
    client.connect()

    FakeWebSocket.instances[0].onmessage?.({ data: JSON.stringify({
      type: 'tool_call',
      task_id: 'task-running',
      data: { name: 'execute', args: '{}', tool_call_id: 'call-legacy' },
    }) })

    expect(onToolCall).toHaveBeenCalledTimes(1)
    client.disconnect()
  })

  it('does not leak a running task tool event into the new-chat gap', () => {
    vi.stubGlobal('WebSocket', FakeWebSocket)
    const onToolCall = vi.fn()
    const onAgentDone = vi.fn()
    const client = new WSClient({
      activeTaskId: () => undefined,
      onToolCall,
      onAgentDone,
    })
    client.connect()

    const socket = FakeWebSocket.instances[0]
    socket.onmessage?.({ data: JSON.stringify({
      type: 'tool_call',
      task_id: 'task-running-over-ssh',
      data: { name: 'execute', args: '{}', tool_call_id: 'call-background' },
    }) })
    socket.onmessage?.({ data: JSON.stringify({
      type: 'agent_done',
      task_id: 'task-running-over-ssh',
      data: {},
    }) })

    expect(onToolCall).not.toHaveBeenCalled()
    expect(onAgentDone).toHaveBeenCalledWith({ task_id: 'task-running-over-ssh' })
    client.disconnect()
  })

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

  it('buffers foreground mutations for a pending conversation instead of dropping them', () => {
    vi.stubGlobal('WebSocket', FakeWebSocket)
    const onAgentText = vi.fn()
    const onPendingTaskEvent = vi.fn()
    const client = new WSClient({
      activeTaskId: () => 'task-active',
      pendingTaskId: () => 'task-pending',
      onPendingTaskEvent,
      onAgentText,
    })
    client.connect()

    FakeWebSocket.instances[0].onmessage?.({ data: JSON.stringify({
      type: 'agent_text',
      task_id: 'task-pending',
      data: { text: 'arrived during navigation' },
    }) })

    expect(onAgentText).not.toHaveBeenCalled()
    expect(onPendingTaskEvent).toHaveBeenCalledWith({
      type: 'agent_text',
      taskId: 'task-pending',
      data: { text: 'arrived during navigation' },
    })
    client.disconnect()
  })
})
