/**
 * Unit tests for exploring-group coalescing.
 * Run: node --experimental-strip-types --test src/timeline/groupExploring.test.ts
 */

import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import {
  groupExploringTimeline,
  isCollapsibleTool,
  summarizeExploringSteps,
} from './groupExploring.ts'
import type { ThreadItem, ToolCall } from '../types/index.ts'

function tool(
  partial: Partial<ToolCall> & Pick<ToolCall, 'id' | 'name'>,
): ToolCall {
  return {
    args: '{}',
    status: 'done',
    timestamp: 0,
    ...partial,
  }
}

function toolItem(t: ToolCall, seq: number): ThreadItem {
  return { kind: 'tool', data: t, seq }
}

describe('isCollapsibleTool', () => {
  it('treats read/grep/glob as collapsible', () => {
    assert.equal(isCollapsibleTool(tool({ id: '1', name: 'read' })), true)
    assert.equal(isCollapsibleTool(tool({ id: '2', name: 'grep' })), true)
    assert.equal(isCollapsibleTool(tool({ id: '3', name: 'glob' })), true)
  })
  it('treats edit/write as non-collapsible', () => {
    assert.equal(isCollapsibleTool(tool({ id: '1', name: 'edit' })), false)
    assert.equal(isCollapsibleTool(tool({ id: '2', name: 'write' })), false)
  })
  it('honors displayInfo.collapsible and kind for execute', () => {
    assert.equal(
      isCollapsibleTool(
        tool({
          id: '1',
          name: 'execute',
          displayInfo: { title: 'Shell', kind: 'list', collapsible: true },
        }),
      ),
      true,
    )
    assert.equal(
      isCollapsibleTool(
        tool({
          id: '2',
          name: 'execute',
          displayInfo: { title: 'Shell', kind: 'shell', collapsible: false },
        }),
      ),
      false,
    )
  })
})

describe('groupExploringTimeline', () => {
  it('merges adjacent collapsible tools into one exploring group', () => {
    const items: ThreadItem[] = [
      toolItem(tool({ id: 'a', name: 'read', displayInfo: { title: 'Read', subtitle: 'a.go', category: 'context' } }), 1),
      toolItem(tool({ id: 'b', name: 'grep', displayInfo: { title: 'Search', subtitle: 'foo', category: 'context' } }), 2),
      toolItem(tool({ id: 'c', name: 'glob', displayInfo: { title: 'Glob', subtitle: '**/*.ts', category: 'context' } }), 3),
    ]
    const out = groupExploringTimeline(items)
    assert.equal(out.length, 1)
    assert.equal(out[0].kind, 'exploring')
    if (out[0].kind === 'exploring') {
      assert.equal(out[0].data.tools.length, 3)
      assert.equal(out[0].data.status, 'done')
    }
  })

  it('keeps a single collapsible tool as a normal tool card', () => {
    const items: ThreadItem[] = [
      toolItem(tool({ id: 'a', name: 'read', displayInfo: { title: 'Read', category: 'context' } }), 1),
    ]
    const out = groupExploringTimeline(items)
    assert.equal(out.length, 1)
    assert.equal(out[0].kind, 'tool')
  })

  it('breaks group on mutation tool', () => {
    const items: ThreadItem[] = [
      toolItem(tool({ id: 'a', name: 'read', displayInfo: { title: 'Read', category: 'context' } }), 1),
      toolItem(tool({ id: 'b', name: 'grep', displayInfo: { title: 'Search', category: 'context' } }), 2),
      toolItem(tool({ id: 'c', name: 'edit', displayInfo: { title: 'Edit', category: 'mutation' } }), 3),
      toolItem(tool({ id: 'd', name: 'read', displayInfo: { title: 'Read', category: 'context' } }), 4),
      toolItem(tool({ id: 'e', name: 'glob', displayInfo: { title: 'Glob', category: 'context' } }), 5),
    ]
    const out = groupExploringTimeline(items)
    assert.equal(out.length, 3)
    assert.equal(out[0].kind, 'exploring')
    assert.equal(out[1].kind, 'tool')
    if (out[1].kind === 'tool') assert.equal(out[1].data.name, 'edit')
    assert.equal(out[2].kind, 'exploring')
  })

  it('breaks group on agent text (message)', () => {
    const items: ThreadItem[] = [
      toolItem(tool({ id: 'a', name: 'read', displayInfo: { title: 'Read', category: 'context' } }), 1),
      toolItem(tool({ id: 'b', name: 'grep', displayInfo: { title: 'Search', category: 'context' } }), 2),
      {
        kind: 'message',
        data: { id: 'm1', role: 'assistant', content: 'thinking…', timestamp: 0 },
        seq: 3,
      },
      toolItem(tool({ id: 'c', name: 'read', displayInfo: { title: 'Read', category: 'context' } }), 4),
      toolItem(tool({ id: 'd', name: 'glob', displayInfo: { title: 'Glob', category: 'context' } }), 5),
    ]
    const out = groupExploringTimeline(items)
    assert.equal(out.length, 3)
    assert.equal(out[0].kind, 'exploring')
    assert.equal(out[1].kind, 'message')
    assert.equal(out[2].kind, 'exploring')
  })

  it('marks exploring group running when any child is running', () => {
    const items: ThreadItem[] = [
      toolItem(tool({ id: 'a', name: 'read', status: 'done', displayInfo: { title: 'Read', category: 'context' } }), 1),
      toolItem(tool({ id: 'b', name: 'grep', status: 'running', displayInfo: { title: 'Search', category: 'context' } }), 2),
    ]
    const out = groupExploringTimeline(items)
    assert.equal(out[0].kind, 'exploring')
    if (out[0].kind === 'exploring') assert.equal(out[0].data.status, 'running')
  })
})

describe('summarizeExploringSteps', () => {
  it('merges consecutive reads into one line', () => {
    const steps = summarizeExploringSteps([
      tool({ id: 'a', name: 'read', displayInfo: { title: 'Read', subtitle: 'a.go' } }),
      tool({ id: 'b', name: 'read', displayInfo: { title: 'Read', subtitle: 'b.go' } }),
      tool({ id: 'c', name: 'grep', displayInfo: { title: 'Search', subtitle: 'foo' } }),
    ])
    assert.deepEqual(steps, [
      { action: 'Read', detail: 'a.go, b.go' },
      { action: 'Search', detail: 'foo' },
    ])
  })
})
