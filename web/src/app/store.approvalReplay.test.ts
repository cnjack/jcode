import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import type { Approval, ThreadItem } from 'jcode-ui-core'
import { chatActions, store } from './store'

beforeEach(() => {
  store.dispatch(chatActions.clearChat())
})

afterEach(() => {
  store.dispatch(chatActions.clearChat())
})

describe('approval replay tool release', () => {
  it('updates the replayed occurrence instead of appending a duplicate after Allow', () => {
    const args = JSON.stringify({ file_path: 'out.txt', content: 'approved' })
    const gate: Approval = {
      id: 'approval-1',
      tool_name: 'write',
      tool_args: args,
      tool_call_id: 'call-1',
      is_external: false,
    }
    const timeline: ThreadItem[] = [
      { kind: 'message', seq: 1, data: { id: 'user', role: 'user', content: 'write it', timestamp: 1_000 } },
      {
        kind: 'tool',
        seq: 2,
        data: {
          id: 'replayed-tool',
          toolCallID: 'call-1',
          name: 'write',
          args,
          status: 'done', // legacy replay fallback for a call with no result
          timestamp: 2_000,
        },
      },
      { kind: 'approval', seq: 3, data: gate },
    ]
    store.dispatch(chatActions.setTimeline(timeline))

    store.dispatch(chatActions.addToolCall({
      name: 'write',
      args,
      toolCallID: 'call-1',
      startedAt: 5_000,
      approvalID: 'approval-1',
      approvalGranted: true,
    }))

    const tools = store.getState().chat.timeline.filter((item) => item.kind === 'tool')
    expect(tools).toHaveLength(1)
    expect(tools[0]).toMatchObject({
      data: {
        id: 'replayed-tool',
        status: 'running',
        startedAt: 5_000,
      },
    })
    const approvalItem = store.getState().chat.timeline.find((item) => item.kind === 'approval')
    expect(approvalItem).toMatchObject({ data: { resolved: true, approved: true } })
  })

  it('does not reuse an unresolved occurrence from an earlier user turn', () => {
    const timeline: ThreadItem[] = [
      { kind: 'message', seq: 1, data: { id: 'user-1', role: 'user', content: 'first', timestamp: 1_000 } },
      {
        kind: 'tool',
        seq: 2,
        data: {
          id: 'old-tool', toolCallID: 'reused', name: 'write', args: '{}', status: 'done', timestamp: 2_000,
        },
      },
      { kind: 'message', seq: 3, data: { id: 'user-2', role: 'user', content: 'second', timestamp: 3_000 } },
    ]
    store.dispatch(chatActions.setTimeline(timeline))

    store.dispatch(chatActions.addToolCall({ name: 'write', args: '{}', toolCallID: 'reused' }))

    const tools = store.getState().chat.timeline.filter((item) => item.kind === 'tool')
    expect(tools).toHaveLength(2)
    expect(tools[0]).toMatchObject({ data: { id: 'old-tool', status: 'done' } })
    expect(tools[1]).toMatchObject({ data: { status: 'running' } })
  })

  it('resolves only the current-turn approval when a model call id is reused', () => {
    const oldGate: Approval = {
      id: 'approval-old',
      tool_name: 'write',
      tool_args: '{}',
      tool_call_id: 'reused',
      is_external: false,
      resolved: true,
      approved: false,
    }
    const currentGate: Approval = {
      id: 'approval-current',
      tool_name: 'write',
      tool_args: '{}',
      tool_call_id: 'reused',
      is_external: false,
    }
    const timeline: ThreadItem[] = [
      { kind: 'message', seq: 1, data: { id: 'user-1', role: 'user', content: 'first', timestamp: 1_000 } },
      { kind: 'approval', seq: 2, data: oldGate },
      { kind: 'message', seq: 3, data: { id: 'user-2', role: 'user', content: 'second', timestamp: 3_000 } },
      { kind: 'approval', seq: 4, data: currentGate },
    ]
    store.dispatch(chatActions.setTimeline(timeline))

    store.dispatch(chatActions.addToolCall({
      name: 'write', args: '{}', toolCallID: 'reused', approvalID: 'approval-current', approvalGranted: true,
    }))

    const approvals = store.getState().chat.timeline.filter((item) => item.kind === 'approval')
    expect(approvals).toHaveLength(2)
    expect(approvals[0]).toMatchObject({ data: { id: 'approval-old', resolved: true, approved: false } })
    expect(approvals[1]).toMatchObject({ data: { id: 'approval-current', resolved: true, approved: true } })
  })

  it('settles the exact same-turn gate when call id and name collide', () => {
    const first: Approval = {
      id: 'approval-first', tool_name: 'write', tool_args: '{"file_path":"first.txt"}',
      tool_call_id: 'reused', is_external: false,
    }
    const second: Approval = {
      id: 'approval-second', tool_name: 'write', tool_args: '{"file_path":"second.txt"}',
      tool_call_id: 'reused', is_external: false,
    }
    store.dispatch(chatActions.setTimeline([
      { kind: 'message', seq: 1, data: { id: 'user', role: 'user', content: 'write both', timestamp: 1_000 } },
      { kind: 'approval', seq: 2, data: first },
      { kind: 'approval', seq: 3, data: second },
    ]))

    store.dispatch(chatActions.addToolCall({
      name: 'write', args: first.tool_args, toolCallID: 'reused',
      approvalID: 'approval-first', approvalGranted: true,
    }))

    const state = store.getState().chat.timeline
    const approvals = state.filter((item) => item.kind === 'approval')
    expect(approvals[0]).toMatchObject({ data: { id: 'approval-first', resolved: true, approved: true } })
    expect(approvals[1]).toMatchObject({ data: { id: 'approval-second' } })
    if (approvals[1].kind === 'approval') {
      expect(approvals[1].data.resolved).toBeUndefined()
      expect(approvals[1].data.approved).toBeUndefined()
    }
    expect(state.find((item) => item.kind === 'tool')).toMatchObject({
      data: { approvalID: 'approval-first', args: first.tool_args },
    })
  })

  it('routes interleaved deny and success results by approval occurrence', () => {
    store.dispatch(chatActions.setTimeline([
      { kind: 'message', seq: 1, data: { id: 'user', role: 'user', content: 'run both', timestamp: 1_000 } },
      {
        kind: 'tool', seq: 2,
        data: {
          id: 'first', name: 'execute', args: '{"command":"slow"}', toolCallID: 'reused',
          approvalID: 'approval-first', status: 'running', timestamp: 2_000,
        },
      },
      {
        kind: 'tool', seq: 3,
        data: {
          id: 'second', name: 'execute', args: '{"command":"deny"}', toolCallID: 'reused',
          approvalID: 'approval-second', status: 'running', timestamp: 3_000,
        },
      },
    ]))

    store.dispatch(chatActions.resolveToolCall({
      name: 'execute', toolCallID: 'reused', approvalID: 'approval-second',
      output: 'Tool execution was rejected by user.', denied: true,
    }))
    store.dispatch(chatActions.resolveToolCall({
      name: 'execute', toolCallID: 'reused', approvalID: 'approval-first', output: 'slow done',
    }))

    const tools = store.getState().chat.timeline.filter((item) => item.kind === 'tool')
    expect(tools[0]).toMatchObject({ data: { id: 'first', status: 'done', output: 'slow done' } })
    expect(tools[1]).toMatchObject({
      data: {
        id: 'second', status: 'done', output: 'Tool execution was rejected by user.', denied: true,
      },
    })
  })

  it('fails closed for an ambiguous result without an approval occurrence', () => {
    store.dispatch(chatActions.setTimeline([
      { kind: 'message', seq: 1, data: { id: 'user', role: 'user', content: 'run both', timestamp: 1_000 } },
      {
        kind: 'tool', seq: 2,
        data: { id: 'first', name: 'execute', args: '{}', toolCallID: 'reused', status: 'running', timestamp: 2_000 },
      },
      {
        kind: 'tool', seq: 3,
        data: { id: 'second', name: 'execute', args: '{}', toolCallID: 'reused', status: 'running', timestamp: 3_000 },
      },
    ]))

    store.dispatch(chatActions.resolveToolCall({
      name: 'execute', toolCallID: 'reused', output: 'ambiguous',
    }))

    const tools = store.getState().chat.timeline.filter((item) => item.kind === 'tool')
    expect(tools.map((item) => item.data.status)).toEqual(['running', 'running'])
    expect(tools.every((item) => item.data.output === undefined)).toBe(true)
  })

  it('falls back to the unique replayed host when approval id arrived during refresh', () => {
    store.dispatch(chatActions.setTimeline([
      { kind: 'message', seq: 1, data: { id: 'user', role: 'user', content: 'run it', timestamp: 1_000 } },
      {
        kind: 'tool', seq: 2,
        data: {
          id: 'replayed', name: 'execute', args: '{"command":"slow"}', toolCallID: 'call-1',
          status: 'done', timestamp: 2_000,
        },
      },
    ]))

    store.dispatch(chatActions.resolveToolCall({
      name: 'execute', toolCallID: 'call-1', approvalID: 'approval-missed-during-refresh',
      output: 'finished after reload',
    }))

    const tool = store.getState().chat.timeline.find((item) => item.kind === 'tool')
    expect(tool).toMatchObject({
      data: { id: 'replayed', status: 'done', output: 'finished after reload' },
    })
  })
})
