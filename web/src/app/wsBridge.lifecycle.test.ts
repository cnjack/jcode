import { describe, expect, it, vi } from 'vitest'
import type { ThreadItem, ToolCall } from 'jcode-ui-core'
import type { AppDispatch, RootState } from './store'
import { createWSHandlers } from './wsBridge'

function tool(data: Partial<ToolCall> & Pick<ToolCall, 'id' | 'name' | 'status'>, seq: number): ThreadItem {
  return {
    kind: 'tool',
    seq,
    data: {
      args: '{}',
      timestamp: 1,
      ...data,
    },
  }
}

function rootState(taskID: string, timeline: ThreadItem[]): RootState {
  return {
    session: { currentSessionId: taskID },
    chat: { timeline },
  } as RootState
}

describe('tool lifecycle WebSocket bridge', () => {
  it('refreshes a dropped initial progress frame instead of binding an old same-ID card', async () => {
    const old = tool({
      id: 'card-a', name: 'generate_image', status: 'done', toolCallID: 'tc-reused',
      operationID: 'op-a', phase: 'terminal', outcome: 'cancelled',
    }, 1)
    const next = tool({
      id: 'card-b', name: 'generate_image', status: 'running', toolCallID: 'tc-reused', phase: 'queued',
    }, 2)
    let state = rootState('task-progress', [old])
    const dispatchMock = vi.fn((action: unknown) => {
      if (typeof action === 'function') {
        // Simulate loadSession replaying the durable call whose WS tool_call
        // frame was missed during reconnect.
        state = rootState('task-progress', [old, next])
        return Promise.resolve({ type: 'session/loadOne/fulfilled' })
      }
      return action
    })
    const handlers = createWSHandlers(() => state, dispatchMock as unknown as AppDispatch)

    handlers.onToolProgress?.({
      name: 'generate_image', tool_call_id: 'tc-reused', operation_id: 'op-b', phase: 'generating',
    })

    await vi.waitFor(() => expect(dispatchMock).toHaveBeenCalledTimes(2))
    expect(typeof dispatchMock.mock.calls[0]?.[0]).toBe('function')
    expect(dispatchMock.mock.calls[1]?.[0]).toMatchObject({
      type: 'chat/progressToolCall',
      payload: { toolCallID: 'tc-reused', operationID: 'op-b', phase: 'generating' },
    })
  })

  it('refreshes and re-applies a terminal result when only a mismatched historical host exists', async () => {
    const old = tool({
      id: 'card-a', name: 'generate_image', status: 'error', toolCallID: 'tc-reused',
      operationID: 'op-a', phase: 'terminal', outcome: 'failed',
    }, 1)
    const next = tool({
      id: 'card-b', name: 'generate_image', status: 'running', toolCallID: 'tc-reused',
      operationID: 'op-b', phase: 'generating',
    }, 2)
    let state = rootState('task-result', [old])
    const dispatchMock = vi.fn((action: unknown) => {
      if (typeof action === 'function') {
        state = rootState('task-result', [old, next])
        return Promise.resolve({ type: 'session/loadOne/fulfilled' })
      }
      return action
    })
    const handlers = createWSHandlers(() => state, dispatchMock as unknown as AppDispatch)

    handlers.onToolResult?.({
      name: 'generate_image', output: 'artifact-b', tool_call_id: 'tc-reused',
      operation_id: 'op-b', phase: 'succeeded', outcome: 'succeeded',
    })

    await vi.waitFor(() => expect(dispatchMock).toHaveBeenCalledTimes(2))
    expect(typeof dispatchMock.mock.calls[0]?.[0]).toBe('function')
    expect(dispatchMock.mock.calls[1]?.[0]).toMatchObject({
      type: 'chat/resolveToolCall',
      payload: { toolCallID: 'tc-reused', operationID: 'op-b', outcome: 'succeeded' },
    })
  })
})
