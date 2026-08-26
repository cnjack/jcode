/**
 * Unit tests for approval/tool binding and completed-turn projection.
 * Run: node --experimental-strip-types --test src/timeline/turnGroups.test.ts
 */

import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { bindApprovalsToTools, groupCompletedTurns } from './turnGroups.ts'
import type { Approval, Message, ThreadItem, ToolCall } from '../types/index.ts'

function message(id: string, role: Message['role'], seq: number, timestamp = seq * 1000): ThreadItem {
  return {
    kind: 'message',
    seq,
    data: { id, role, content: id, timestamp },
  }
}

function tool(
  id: string,
  seq: number,
  partial: Partial<ToolCall> = {},
): ThreadItem {
  return {
    kind: 'tool',
    seq,
    data: {
      id,
      name: 'execute',
      args: '{}',
      toolCallID: id,
      status: 'done',
      timestamp: seq * 1000,
      ...partial,
    },
  }
}

function approval(
  id: string,
  seq: number,
  partial: Partial<Approval> = {},
): ThreadItem {
  return {
    kind: 'approval',
    seq,
    data: {
      id,
      tool_name: 'execute',
      tool_args: '{}',
      tool_call_id: id,
      is_external: false,
      ...partial,
    },
  }
}

describe('bindApprovalsToTools', () => {
  it('moves a matched pending approval onto the exact tool occurrence', () => {
    const out = bindApprovalsToTools([
      message('u', 'user', 1),
      tool('call-1', 2, { status: 'running' }),
      approval('approval-1', 3, { tool_call_id: 'call-1' }),
    ])

    assert.deepEqual(out.map((item) => item.kind), ['message', 'tool'])
    const bound = out[1]
    assert.equal(bound.kind, 'tool')
    if (bound.kind === 'tool') {
      assert.equal(bound.data.approval?.id, 'approval-1')
      assert.equal(bound.data.awaitingApproval, true)
    }
  })

  it('keeps missing-id and orphan approvals standalone', () => {
    const noID = approval('no-id', 2, { tool_call_id: undefined })
    const orphan = approval('orphan', 3, { tool_call_id: 'missing' })
    const input = [message('u', 'user', 1), noID, orphan]
    assert.deepEqual(bindApprovalsToTools(input), input)
  })

  it('does not bind a reused id to a settled prior occurrence when approval arrives before the new call', () => {
    const out = bindApprovalsToTools([
      message('u', 'user', 1),
      tool('old', 2, { toolCallID: 'reused', status: 'done' }),
      approval('gate', 3, { tool_call_id: 'reused' }),
      tool('new', 4, { toolCallID: 'reused', status: 'running' }),
    ])
    const tools = out.filter((item): item is Extract<ThreadItem, { kind: 'tool' }> => item.kind === 'tool')
    assert.equal(tools[0].data.approval, undefined)
    assert.equal(tools[1].data.approval?.id, 'gate')
  })

  it('requires tool name as well as call id', () => {
    const input = [
      tool('write-call', 1, { name: 'write', toolCallID: 'same', status: 'running' }),
      approval('execute-gate', 2, { tool_call_id: 'same', tool_name: 'execute' }),
    ]
    assert.deepEqual(bindApprovalsToTools(input), input)
  })

  it('uses approval occurrence id when concurrent call ids and names collide', () => {
    const out = bindApprovalsToTools([
      approval('gate-first', 1, { tool_call_id: 'reused', tool_args: '{"file":"first"}' }),
      approval('gate-second', 2, { tool_call_id: 'reused', tool_args: '{"file":"second"}' }),
      tool('tool-second', 3, {
        toolCallID: 'reused', approvalID: 'gate-second', status: 'running', args: '{"file":"second"}',
      }),
    ])
    assert.deepEqual(out.map((item) => item.kind), ['approval', 'tool'])
    assert.equal(out[0].kind, 'approval')
    if (out[0].kind === 'approval') assert.equal(out[0].data.id, 'gate-first')
    assert.equal(out[1].kind, 'tool')
    if (out[1].kind === 'tool') assert.equal(out[1].data.approval?.id, 'gate-second')
  })

  it('projects a resolved denial immediately without waiting for tool_result', () => {
    const out = bindApprovalsToTools([
      tool('call-1', 1, { status: 'running' }),
      approval('gate', 2, {
        tool_call_id: 'call-1',
        resolved: true,
        approved: false,
      }),
    ])
    assert.equal(out.length, 1)
    assert.equal(out[0].kind, 'tool')
    if (out[0].kind === 'tool') {
      assert.equal(out[0].data.denied, true)
      assert.equal(out[0].data.awaitingApproval, undefined)
    }
  })

  it('treats a terminal tool result as resolution when another client approved it', () => {
    const out = bindApprovalsToTools([
      tool('call-1', 1, { status: 'done', output: 'ok' }),
      approval('gate', 2, { tool_call_id: 'call-1' }),
    ])
    assert.equal(out.length, 1)
    assert.equal(out[0].kind, 'tool')
    if (out[0].kind === 'tool') {
      assert.equal(out[0].data.approval?.resolved, true)
      assert.equal(out[0].data.approval?.approved, true)
      assert.equal(out[0].data.awaitingApproval, undefined)
    }
  })

  it('keeps a replay-settled tool interactive when pending approval has no result evidence', () => {
    const out = bindApprovalsToTools([
      tool('call-1', 1, { status: 'done', output: undefined, error: undefined }),
      approval('gate', 2, { tool_call_id: 'call-1' }),
    ])
    assert.equal(out.length, 1)
    assert.equal(out[0].kind, 'tool')
    if (out[0].kind === 'tool') {
      assert.equal(out[0].data.approval?.resolved, undefined)
      assert.equal(out[0].data.status, 'running')
      assert.equal(out[0].data.awaitingApproval, true)
    }
  })
})

describe('groupCompletedTurns', () => {
  it('collapses completed turns but leaves the active final turn flat', () => {
    const out = groupCompletedTurns([
      message('u1', 'user', 1, 1_000),
      tool('t1', 2),
      message('a1', 'assistant', 3, 4_000),
      message('u2', 'user', 4, 5_000),
      tool('t2', 5, { status: 'running' }),
    ], { isRunning: true })

    assert.deepEqual(out.map((item) => item.kind), ['message', 'turn', 'message', 'tool'])
    const completed = out[1]
    assert.equal(completed.kind, 'turn')
    if (completed.kind === 'turn') {
      assert.equal(completed.data.summary.id, 'a1')
      assert.deepEqual(completed.data.activity.map((item) => item.kind), ['tool'])
      assert.equal(completed.data.durationMs, 3_000)
    }
  })

  it('uses a stamped live duration and keeps the final summary out of hidden activity', () => {
    const summary = message('final', 'assistant', 4, 20_000)
    if (summary.kind === 'message') summary.data.durationMs = 12_345
    const out = groupCompletedTurns([
      message('u', 'user', 1, 1_000),
      message('commentary', 'assistant', 2, 2_000),
      tool('t', 3),
      summary,
    ])

    assert.deepEqual(out.map((item) => item.kind), ['message', 'turn'])
    const turn = out[1]
    assert.equal(turn.kind, 'turn')
    if (turn.kind === 'turn') {
      assert.equal(turn.data.durationMs, 12_345)
      assert.equal(turn.data.summary.id, 'final')
      assert.deepEqual(turn.data.activity.map((item) => item.kind), ['message', 'tool'])
    }
  })

  it('keeps standalone media and terminal errors visible beside the summary', () => {
    const error = message('error', 'system', 3)
    if (error.kind === 'message') error.data.level = 'error'
    const out = groupCompletedTurns([
      message('u', 'user', 1),
      tool('image', 2, { name: 'generate_image', surface: 'standalone' }),
      error,
      message('final', 'assistant', 4),
    ])

    assert.deepEqual(out.map((item) => item.kind), ['message', 'turn', 'tool', 'message'])
    const turn = out[1]
    if (turn.kind === 'turn') assert.equal(turn.data.activity.length, 0)
  })

  it('does not collapse a tool-only/error turn with no final assistant', () => {
    const error = message('error', 'system', 3)
    if (error.kind === 'message') error.data.level = 'error'
    const input = [message('u', 'user', 1), tool('t', 2), error]
    assert.deepEqual(groupCompletedTurns(input), input)
  })

  it('does not treat assistant commentary before a later tool as the final summary', () => {
    const input = [
      message('u', 'user', 1),
      message('commentary', 'assistant', 2),
      tool('t', 3),
    ]
    assert.deepEqual(groupCompletedTurns(input), input)
  })

  it('never hides an unresolved approval', () => {
    const input = [
      message('u', 'user', 1),
      tool('t', 2, {
        status: 'running',
        approval: {
          id: 'gate',
          tool_name: 'execute',
          tool_args: '{}',
          tool_call_id: 't',
          is_external: false,
        },
      }),
      message('final', 'assistant', 3),
    ]
    assert.deepEqual(groupCompletedTurns(input), input)
  })
})
