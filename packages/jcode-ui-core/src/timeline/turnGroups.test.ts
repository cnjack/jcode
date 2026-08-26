/**
 * Unit tests for approval/tool binding and completed-turn projection.
 * Run: node --experimental-strip-types --test src/timeline/turnGroups.test.ts
 */

import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { bindApprovalsToTools, groupCompletedTurns } from './turnGroups.ts'
import { getApprovalOutcome } from '../types/index.ts'
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

describe('getApprovalOutcome', () => {
  const base: Approval = {
    id: 'gate',
    tool_name: 'execute',
    tool_args: '{}',
    is_external: false,
  }
  const options: Approval['options'] = [
    { id: 'once', label: 'Allow once', kind: 'allow_once' },
    { id: 'always', label: 'Always allow', kind: 'allow_always' },
    { id: 'deny', label: 'Deny', kind: 'deny' },
    { id: 'custom', label: 'Continue', kind: 'custom' },
    { id: 'implicit-custom', label: 'Continue' },
  ]

  it('supports classic boolean decisions and keeps unresolved gates pending', () => {
    assert.equal(getApprovalOutcome(base), 'pending')
    assert.equal(getApprovalOutcome({ ...base, resolved: true, approved: true }), 'allowed')
    assert.equal(getApprovalOutcome({ ...base, resolved: true, approved: false }), 'denied')
  })

  it('uses the selected option when approved is absent or contradictory', () => {
    assert.equal(getApprovalOutcome({
      ...base, resolved: true, approved: false, options, resolvedOptionId: 'once',
    }), 'allowed')
    assert.equal(getApprovalOutcome({
      ...base, resolved: true, options, resolvedOptionId: 'always',
    }), 'allowed')
    assert.equal(getApprovalOutcome({
      ...base, resolved: true, approved: true, options, resolvedOptionId: 'deny',
    }), 'denied')
    assert.equal(getApprovalOutcome({
      ...base, resolved: true, options, resolvedOptionId: 'custom',
    }), 'allowed')
    assert.equal(getApprovalOutcome({
      ...base, resolved: true, options, resolvedOptionId: 'implicit-custom',
    }), 'allowed')
  })

  it('fails closed when a resolved decision has no usable outcome', () => {
    assert.equal(getApprovalOutcome({ ...base, resolved: true }), 'denied')
    assert.equal(getApprovalOutcome({
      ...base, resolved: true, options, resolvedOptionId: 'unknown',
    }), 'denied')
  })
})

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

  it('prefers a following exact approval occurrence over a prior running legacy id match', () => {
    const out = bindApprovalsToTools([
      message('u', 'user', 1),
      tool('safe', 2, {
        toolCallID: 'reused',
        status: 'running',
        args: '{"command":"git status"}',
      }),
      approval('danger-gate', 3, {
        tool_call_id: 'reused',
        tool_args: '{"command":"rm victim"}',
      }),
      tool('danger', 4, {
        toolCallID: 'reused',
        approvalID: 'danger-gate',
        status: 'running',
        args: '{"command":"rm victim"}',
      }),
    ])
    const tools = out.filter((item): item is Extract<ThreadItem, { kind: 'tool' }> => item.kind === 'tool')

    assert.equal(tools[0].data.approval, undefined)
    assert.equal(tools[1].data.approval?.id, 'danger-gate')
  })

  it('keeps a duplicate approval standalone when its exact occurrence is already occupied', () => {
    const existingGate: Approval = {
      id: 'danger-gate',
      tool_name: 'execute',
      tool_args: '{"command":"rm victim"}',
      tool_call_id: 'reused',
      is_external: false,
    }
    const input: ThreadItem[] = [
      message('u', 'user', 1),
      tool('safe', 2, { toolCallID: 'reused', status: 'running' }),
      tool('danger', 3, {
        toolCallID: 'reused',
        approvalID: 'danger-gate',
        approval: existingGate,
        status: 'running',
      }),
      approval('danger-gate', 4, { tool_call_id: 'reused' }),
    ]

    const out = bindApprovalsToTools(input)
    assert.deepEqual(out, input)
    assert.equal(out[1].kind, 'tool')
    if (out[1].kind === 'tool') assert.equal(out[1].data.approval, undefined)
    assert.equal(out[3].kind, 'approval')
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

  it('projects option-based allow and deny outcomes when approved is absent', () => {
    const options: Approval['options'] = [
      { id: 'allow', label: 'Allow once', kind: 'allow_once' },
      { id: 'deny', label: 'Deny', kind: 'deny' },
    ]
    const denied = bindApprovalsToTools([
      tool('denied-call', 1, { status: 'running' }),
      approval('denied-gate', 2, {
        tool_call_id: 'denied-call', resolved: true, options, resolvedOptionId: 'deny',
      }),
    ])
    const allowed = bindApprovalsToTools([
      tool('allowed-call', 1, { status: 'running' }),
      approval('allowed-gate', 2, {
        tool_call_id: 'allowed-call', resolved: true, options, resolvedOptionId: 'allow',
      }),
    ])

    assert.equal(denied[0].kind, 'tool')
    if (denied[0].kind === 'tool') {
      assert.equal(denied[0].data.denied, true)
      assert.equal(denied[0].data.awaitingApproval, undefined)
    }
    assert.equal(allowed[0].kind, 'tool')
    if (allowed[0].kind === 'tool') {
      assert.equal(allowed[0].data.denied, undefined)
      assert.equal(allowed[0].data.awaitingApproval, undefined)
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
      assert.deepEqual(completed.data.activity.map((entry) => entry.item.kind), ['tool'])
      assert.deepEqual(completed.data.activity.map((entry) => entry.alwaysVisible), [false])
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
      assert.deepEqual(turn.data.activity.map((entry) => entry.item.kind), ['message', 'tool'])
      assert.deepEqual(turn.data.activity.map((entry) => entry.alwaysVisible), [false, false])
    }
  })

  it('keeps standalone media and terminal errors ordered before the final summary', () => {
    const error = message('error', 'system', 3)
    if (error.kind === 'message') error.data.level = 'error'
    const out = groupCompletedTurns([
      message('u', 'user', 1),
      tool('image', 2, { name: 'generate_image', surface: 'standalone' }),
      error,
      message('final', 'assistant', 4),
    ])

    assert.deepEqual(out.map((item) => item.kind), ['message', 'turn'])
    const turn = out[1]
    if (turn.kind === 'turn') {
      assert.deepEqual(
        turn.data.activity.map((entry) => entry.item.kind),
        ['tool', 'message'],
      )
      assert.deepEqual(
        turn.data.activity.map((entry) => entry.alwaysVisible),
        [true, true],
      )
      assert.equal(turn.data.summary.id, 'final')
    }
  })

  it('retains exact intermediate order for mixed hidden work and visible receipts', () => {
    const error = message('visible-error', 'system', 4)
    if (error.kind === 'message') error.data.level = 'error'
    const out = groupCompletedTurns([
      message('u', 'user', 1),
      message('before', 'assistant', 2),
      tool('image', 3, { name: 'generate_image', surface: 'standalone' }),
      error,
      tool('after', 5),
      message('final', 'assistant', 6),
    ])

    const turn = out[1]
    assert.equal(turn.kind, 'turn')
    if (turn.kind === 'turn') {
      assert.deepEqual(
        turn.data.activity.map((entry) => entry.item.data.id),
        ['before', 'image', 'visible-error', 'after'],
      )
      assert.deepEqual(
        turn.data.activity.map((entry) => entry.alwaysVisible),
        [false, true, true, false],
      )
    }
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
