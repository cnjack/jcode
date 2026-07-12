/**
 * Unit tests for exploring-group coalescing.
 * Run: node --experimental-strip-types --test src/timeline/groupExploring.test.ts
 */

import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import {
  groupExploringTimeline,
  groupToolTimeline,
  isCollapsibleTool,
  summarizeExploringSteps,
  summarizeExploringCounts,
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

  it('dedupes repeated file names in a merged read line', () => {
    const steps = summarizeExploringSteps([
      tool({ id: 'a', name: 'read', displayInfo: { title: 'Read', subtitle: 'a.go' } }),
      tool({ id: 'b', name: 'read', displayInfo: { title: 'Read', subtitle: 'b.go' } }),
      tool({ id: 'c', name: 'read', displayInfo: { title: 'Read', subtitle: 'a.go' } }),
    ])
    assert.deepEqual(steps, [{ action: 'Read', detail: 'a.go, b.go' }])
  })
})

describe('summarizeExploringCounts', () => {
  it('buckets steps into read/search/list counts', () => {
    const summary = summarizeExploringCounts([
      tool({ id: 'a', name: 'read', displayInfo: { title: 'Read', subtitle: 'a.go' } }),
      tool({ id: 'b', name: 'read', displayInfo: { title: 'Read', subtitle: 'b.go' } }),
      tool({ id: 'c', name: 'read', displayInfo: { title: 'Read', subtitle: 'c.go' } }),
      tool({ id: 'd', name: 'grep', displayInfo: { title: 'Search', subtitle: 'foo' } }),
      tool({ id: 'e', name: 'grep', displayInfo: { title: 'Search', subtitle: 'bar' } }),
      tool({ id: 'f', name: 'list_dir', displayInfo: { title: 'List', subtitle: 'src/' } }),
    ])
    assert.equal(summary, '3 files read · 2 searches · 1 list')
  })

  it('dedupes read file names and uses singular forms', () => {
    const summary = summarizeExploringCounts([
      tool({ id: 'a', name: 'read', displayInfo: { title: 'Read', subtitle: 'a.go' } }),
      tool({ id: 'b', name: 'read', displayInfo: { title: 'Read', subtitle: 'a.go' } }),
      tool({ id: 'c', name: 'grep', displayInfo: { title: 'Search', subtitle: 'foo' } }),
    ])
    assert.equal(summary, '1 file read · 1 search')
  })
})

describe('groupToolTimeline', () => {
  const shell = (id: string, batchId?: string, status: ToolCall['status'] = 'done') =>
    tool({
      id,
      name: 'execute',
      status,
      batchId,
      displayInfo: { title: 'Shell', subtitle: `cmd ${id}`, kind: 'shell', collapsible: false },
    })

  it('coalesces same-batchId tools into one batch item', () => {
    const items: ThreadItem[] = [
      toolItem(shell('a', 'b1'), 1),
      toolItem(shell('b', 'b1'), 2),
      toolItem(shell('c', 'b1'), 3),
    ]
    const out = groupToolTimeline(items)
    assert.equal(out.length, 1)
    assert.equal(out[0].kind, 'batch')
    if (out[0].kind === 'batch') {
      assert.equal(out[0].data.batchId, 'b1')
      assert.deepEqual(out[0].data.tools.map((t) => t.id), ['a', 'b', 'c'])
      assert.equal(out[0].data.status, 'done')
      assert.equal(out[0].seq, 1)
    }
  })

  it('keeps a single-member batch as a normal tool card', () => {
    const items: ThreadItem[] = [toolItem(shell('a', 'b1'), 1)]
    const out = groupToolTimeline(items)
    assert.equal(out.length, 1)
    assert.equal(out[0].kind, 'tool')
    if (out[0].kind === 'tool') assert.equal(out[0].data.id, 'a')
  })

  it('keeps an approval in place without breaking the batch', () => {
    const items: ThreadItem[] = [
      toolItem(shell('a', 'b1'), 1),
      {
        kind: 'approval',
        data: { id: 'ap1', tool_name: 'execute', tool_args: '{}', is_external: false },
        seq: 2,
      },
      toolItem(shell('b', 'b1'), 3),
    ]
    const out = groupToolTimeline(items)
    assert.equal(out.length, 2)
    assert.equal(out[0].kind, 'batch')
    if (out[0].kind === 'batch') assert.equal(out[0].data.tools.length, 2)
    assert.equal(out[1].kind, 'approval')
  })

  it('falls back to exploring coalescing for tools without a batchId', () => {
    const items: ThreadItem[] = [
      toolItem(tool({ id: 'a', name: 'read', displayInfo: { title: 'Read', category: 'context' } }), 1),
      toolItem(tool({ id: 'b', name: 'grep', displayInfo: { title: 'Search', category: 'context' } }), 2),
    ]
    const out = groupToolTimeline(items)
    assert.deepEqual(out, groupExploringTimeline(items))
    assert.equal(out[0].kind, 'exploring')
  })

  it('marks a batch explorative only when ALL tools are collapsible', () => {
    const explorative = groupToolTimeline([
      toolItem(tool({ id: 'a', name: 'read', batchId: 'b1', displayInfo: { title: 'Read', category: 'context' } }), 1),
      toolItem(tool({ id: 'b', name: 'grep', batchId: 'b1', displayInfo: { title: 'Search', category: 'context' } }), 2),
    ])
    assert.equal(explorative[0].kind, 'batch')
    if (explorative[0].kind === 'batch') assert.equal(explorative[0].data.explorative, true)

    const mixed = groupToolTimeline([
      toolItem(tool({ id: 'a', name: 'read', batchId: 'b2', displayInfo: { title: 'Read', category: 'context' } }), 1),
      toolItem(shell('b', 'b2'), 2),
    ])
    assert.equal(mixed[0].kind, 'batch')
    if (mixed[0].kind === 'batch') assert.equal(mixed[0].data.explorative, false)
  })

  it('marks a batch running when any member is running', () => {
    const out = groupToolTimeline([
      toolItem(shell('a', 'b1', 'done'), 1),
      toolItem(shell('b', 'b1', 'running'), 2),
    ])
    assert.equal(out[0].kind, 'batch')
    if (out[0].kind === 'batch') assert.equal(out[0].data.status, 'running')
  })
})
