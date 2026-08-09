/**
 * Unit tests for activity-group coalescing + category-count summaries.
 * Run: node --experimental-strip-types --test src/timeline/groupActivity.test.ts
 */

import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import {
  groupActivityTimeline,
  summarizeActivityCounts,
  countActivityFlags,
} from './groupActivity.ts'
import type { ThreadItem, ToolCall } from '../types/index.ts'

function tool(partial: Partial<ToolCall> & Pick<ToolCall, 'id' | 'name'>): ToolCall {
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

const read = (id: string, file = `${id}.go`) =>
  tool({ id, name: 'read', displayInfo: { title: 'Read', subtitle: file, category: 'context', kind: 'read', collapsible: true } })
const grep = (id: string) =>
  tool({ id, name: 'grep', displayInfo: { title: 'Search', subtitle: 'foo', category: 'context', kind: 'search', collapsible: true } })
const shell = (id: string, extra: Partial<ToolCall> = {}) =>
  tool({
    id,
    name: 'execute',
    displayInfo: { title: 'Shell', subtitle: `cmd ${id}`, kind: 'shell', collapsible: false },
    ...extra,
  })
const edit = (id: string, file = `${id}.ts`) =>
  tool({ id, name: 'edit', displayInfo: { title: 'Edit', subtitle: file, category: 'mutation', kind: 'edit' } })
const message = (id: string, seq: number, role: 'user' | 'assistant' = 'assistant'): ThreadItem => ({
  kind: 'message',
  data: { id, role, content: 'x', timestamp: 0 },
  seq,
})
const approval = (id: string, seq: number): ThreadItem => ({
  kind: 'approval',
  data: { id, tool_name: 'execute', tool_args: '{}', is_external: false },
  seq,
})
const image = (id: string, extra: Partial<ToolCall> = {}) =>
  tool({
    id,
    name: 'generate_image',
    surface: 'standalone',
    phase: 'queued',
    displayInfo: { title: 'Generate image', kind: 'other', collapsible: false },
    ...extra,
  })
const askUser = (id: string) =>
  tool({
    id,
    name: 'ask_user',
    displayInfo: { title: 'Ask user', kind: 'other', collapsible: false },
  })

describe('groupActivityTimeline', () => {
  it('merges ALL adjacent tools (read-only + mutating) into one activity group', () => {
    const items: ThreadItem[] = [
      toolItem(read('a'), 1),
      toolItem(shell('b'), 2),
      toolItem(edit('c'), 3),
    ]
    const out = groupActivityTimeline(items)
    assert.equal(out.length, 1)
    assert.equal(out[0].kind, 'activity')
    if (out[0].kind === 'activity') {
      assert.deepEqual(out[0].data.tools.map((t) => t.id), ['a', 'b', 'c'])
      assert.equal(out[0].data.status, 'done')
      assert.equal(out[0].data.explorative, false)
      assert.equal(out[0].seq, 1)
    }
  })

  it('never produces exploring or batch items', () => {
    const items: ThreadItem[] = [
      toolItem(read('a'), 1),
      toolItem(grep('b'), 2),
      toolItem(shell('c', { batchId: 'b1' }), 3),
      toolItem(shell('d', { batchId: 'b1' }), 4),
    ]
    const out = groupActivityTimeline(items)
    assert.ok(out.every((i) => i.kind !== 'exploring' && i.kind !== 'batch'))
    assert.equal(out.length, 1)
    assert.equal(out[0].kind, 'activity')
  })

  it('keeps an isolated single tool as a plain tool card', () => {
    const items: ThreadItem[] = [
      message('m1', 1),
      toolItem(shell('a'), 2),
      message('m2', 3),
    ]
    const out = groupActivityTimeline(items)
    assert.deepEqual(out.map((i) => i.kind), ['message', 'tool', 'message'])
  })

  it('breaks the group on messages', () => {
    const items: ThreadItem[] = [
      toolItem(read('a'), 1),
      toolItem(shell('b'), 2),
      message('m1', 3),
      toolItem(read('c'), 4),
      toolItem(edit('d'), 5),
    ]
    const out = groupActivityTimeline(items)
    assert.deepEqual(out.map((i) => i.kind), ['activity', 'message', 'activity'])
    if (out[0].kind === 'activity' && out[2].kind === 'activity') {
      assert.deepEqual(out[0].data.tools.map((t) => t.id), ['a', 'b'])
      assert.deepEqual(out[2].data.tools.map((t) => t.id), ['c', 'd'])
    }
  })

  it('does NOT break the group on approvals — they render in place after the anchor', () => {
    const items: ThreadItem[] = [
      toolItem(shell('a'), 1),
      approval('ap1', 2),
      toolItem(shell('b'), 3),
      toolItem(read('c'), 4),
    ]
    const out = groupActivityTimeline(items)
    assert.deepEqual(out.map((i) => i.kind), ['activity', 'approval'])
    if (out[0].kind === 'activity') {
      assert.deepEqual(out[0].data.tools.map((t) => t.id), ['a', 'b', 'c'])
    }
  })

  it('keeps a standalone image between adjacent tools as a hard boundary', () => {
    const out = groupActivityTimeline([
      toolItem(read('a'), 1),
      toolItem(image('image'), 2),
      toolItem(shell('b'), 3),
    ])
    assert.deepEqual(out.map((i) => i.kind), ['tool', 'tool', 'tool'])
    assert.deepEqual(
      out.filter((i) => i.kind === 'tool').map((i) => i.data.id),
      ['a', 'image', 'b'],
    )
  })

  it('keeps ask_user receipts as a hard boundary without requiring a surface hint', () => {
    const out = groupActivityTimeline([
      toolItem(read('a'), 1),
      toolItem(askUser('question'), 2),
      toolItem(shell('b'), 3),
    ])
    assert.deepEqual(out.map((i) => i.kind), ['tool', 'tool', 'tool'])
    assert.deepEqual(
      out.filter((i) => i.kind === 'tool').map((i) => i.data.id),
      ['a', 'question', 'b'],
    )
  })

  it('does not pull batch members backwards across a standalone image', () => {
    const out = groupActivityTimeline([
      toolItem(read('a'), 1),
      toolItem(image('image', { batchId: 'b1' }), 2),
      toolItem(shell('b', { batchId: 'b1' }), 3),
    ])
    assert.deepEqual(out.map((i) => i.kind), ['tool', 'tool', 'tool'])
    assert.deepEqual(
      out.filter((i) => i.kind === 'tool').map((i) => i.data.id),
      ['a', 'image', 'b'],
    )
  })

  it('preserves approvals around a standalone image without joining neighboring tools', () => {
    const out = groupActivityTimeline([
      toolItem(read('a'), 1),
      approval('before-image', 2),
      toolItem(image('image'), 3),
      approval('after-image', 4),
      toolItem(shell('b'), 5),
    ])
    assert.deepEqual(out.map((i) => i.kind), ['tool', 'approval', 'tool', 'approval', 'tool'])
  })

  it('anchors batch members at the first member even across approvals', () => {
    const items: ThreadItem[] = [
      toolItem(shell('a', { batchId: 'b1' }), 1),
      approval('ap1', 2),
      toolItem(shell('b', { batchId: 'b1' }), 3),
    ]
    const out = groupActivityTimeline(items)
    assert.deepEqual(out.map((i) => i.kind), ['activity', 'approval'])
    if (out[0].kind === 'activity') {
      assert.deepEqual(out[0].data.tools.map((t) => t.id), ['a', 'b'])
      assert.equal(out[0].seq, 1)
    }
  })

  it('pulls late batch members back to the batch even across a message boundary', () => {
    const items: ThreadItem[] = [
      toolItem(shell('a', { batchId: 'b1' }), 1),
      toolItem(shell('b', { batchId: 'b1' }), 2),
      message('m1', 3),
      toolItem(shell('c', { batchId: 'b1' }), 4),
      toolItem(read('d'), 5),
    ]
    const out = groupActivityTimeline(items)
    assert.deepEqual(out.map((i) => i.kind), ['activity', 'message', 'tool'])
    if (out[0].kind === 'activity') {
      assert.deepEqual(out[0].data.tools.map((t) => t.id), ['a', 'b', 'c'])
    }
    if (out[2].kind === 'tool') assert.equal(out[2].data.id, 'd')
  })

  it('merges an adjacent batch and single tools into ONE activity group', () => {
    const items: ThreadItem[] = [
      toolItem(read('a'), 1),
      toolItem(shell('b', { batchId: 'b1' }), 2),
      toolItem(shell('c', { batchId: 'b1' }), 3),
      toolItem(edit('d'), 4),
    ]
    const out = groupActivityTimeline(items)
    assert.equal(out.length, 1)
    assert.equal(out[0].kind, 'activity')
    if (out[0].kind === 'activity') {
      assert.deepEqual(out[0].data.tools.map((t) => t.id), ['a', 'b', 'c', 'd'])
    }
  })

  it('unwraps a single-member batch that stands alone', () => {
    const items: ThreadItem[] = [message('m1', 1), toolItem(shell('a', { batchId: 'b1' }), 2)]
    const out = groupActivityTimeline(items)
    assert.deepEqual(out.map((i) => i.kind), ['message', 'tool'])
  })

  it('marks explorative only when ALL tools are read-only', () => {
    const explorative = groupActivityTimeline([toolItem(read('a'), 1), toolItem(grep('b'), 2)])
    assert.equal(explorative[0].kind, 'activity')
    if (explorative[0].kind === 'activity') assert.equal(explorative[0].data.explorative, true)

    const mixed = groupActivityTimeline([toolItem(read('a'), 1), toolItem(shell('b'), 2)])
    assert.equal(mixed[0].kind, 'activity')
    if (mixed[0].kind === 'activity') assert.equal(mixed[0].data.explorative, false)
  })

  it('computes group status: running > error > done', () => {
    const running = groupActivityTimeline([
      toolItem(shell('a'), 1),
      toolItem(shell('b', { status: 'running' }), 2),
    ])
    if (running[0].kind === 'activity') assert.equal(running[0].data.status, 'running')

    const errored = groupActivityTimeline([
      toolItem(shell('a'), 1),
      toolItem(shell('b', { status: 'error' }), 2),
    ])
    if (errored[0].kind === 'activity') assert.equal(errored[0].data.status, 'error')
  })

  it('keeps a stable group id derived from the first tool', () => {
    const out = groupActivityTimeline([toolItem(read('a'), 1), toolItem(grep('b'), 2)])
    if (out[0].kind === 'activity') assert.equal(out[0].data.id, 'activity_a')
  })

  it('passes non-tool items through untouched', () => {
    const items: ThreadItem[] = [message('m1', 1, 'user'), approval('ap1', 2), message('m2', 3)]
    assert.deepEqual(groupActivityTimeline(items), items)
  })
})

describe('summarizeActivityCounts', () => {
  it('buckets a mixed group with verb phrasing, capitalized first segment', () => {
    const summary = summarizeActivityCounts([
      shell('c1'),
      shell('c2'),
      shell('c3'),
      read('r1', 'a.go'),
      read('r2', 'b.go'),
      tool({ id: 'ag1', name: 'subagent', displayInfo: { title: 'Agent', kind: 'agent' } }),
    ])
    assert.equal(summary, 'Ran 3 commands · read 2 files · ran 1 agent')
  })

  it('counts execute as a command even when kind is read/search/list', () => {
    const summary = summarizeActivityCounts([
      tool({
        id: 'g1',
        name: 'execute',
        displayInfo: { title: 'Shell', subtitle: 'git log', kind: 'search', collapsible: true },
      }),
      tool({
        id: 'g2',
        name: 'execute',
        displayInfo: { title: 'Shell', subtitle: 'git show', kind: 'read', collapsible: true },
      }),
    ])
    // All-read-only group → Explored phrasing, but git commands stay commands.
    assert.equal(summary, '2 commands')
  })

  it('uses the Explored phrasing for all-read-only groups', () => {
    const summary = summarizeActivityCounts([
      read('r1', 'a.go'),
      read('r2', 'b.go'),
      read('r3', 'c.go'),
      grep('s1'),
      grep('s2'),
    ])
    assert.equal(summary, '3 files read · 2 searches')
  })

  it('dedupes read files and edit files by subtitle', () => {
    const reads = summarizeActivityCounts([read('r1', 'a.go'), read('r2', 'a.go'), grep('s1')])
    assert.equal(reads, '1 file read · 1 search')

    const edits = summarizeActivityCounts([
      edit('e1', 'x.ts'),
      edit('e2', 'x.ts'),
      tool({ id: 'w1', name: 'write', displayInfo: { title: 'Write', subtitle: 'y.ts', kind: 'edit' } }),
      shell('c1'),
    ])
    assert.equal(edits, 'Ran 1 command · edited 2 files')
  })

  it('classifies glob/list_dir as lists even when kind says search', () => {
    const summary = summarizeActivityCounts([
      tool({ id: 'l1', name: 'glob', displayInfo: { title: 'Glob', subtitle: '**/*.ts', kind: 'search', collapsible: true } }),
      tool({ id: 'l2', name: 'list_dir', displayInfo: { title: 'List', subtitle: 'src/', kind: 'list', collapsible: true } }),
      read('r1', 'a.go'),
    ])
    assert.equal(summary, '1 file read · 2 lists')
  })

  it('uses singular forms', () => {
    const summary = summarizeActivityCounts([shell('c1'), edit('e1', 'x.ts'), read('r1', 'a.go')])
    assert.equal(summary, 'Ran 1 command · read 1 file · edited 1 file')
  })

  it('counts unclassified tools as other', () => {
    const summary = summarizeActivityCounts([
      tool({ id: 'o1', name: 'mystery_tool' }),
      shell('c1'),
    ])
    assert.equal(summary, 'Ran 1 command · 1 other')
  })
})

describe('countActivityFlags', () => {
  it('counts errored and nonzero-exit tools as failed', () => {
    const flags = countActivityFlags([
      shell('a', { status: 'error' }),
      shell('b', { meta: { exit_code: 1 } }),
      shell('c', { meta: { exit_code: 0 } }),
      shell('d'),
    ])
    assert.deepEqual(flags, { failed: 2, denied: 0 })
  })

  it('counts denied separately — a denied error-ish tool is denied, not failed', () => {
    const flags = countActivityFlags([
      shell('a', { denied: true }),
      shell('b', { denied: true, meta: { exit_code: 1 } }),
      shell('c', { status: 'error' }),
    ])
    assert.deepEqual(flags, { failed: 1, denied: 2 })
  })

  it('returns zeros for an all-green group', () => {
    assert.deepEqual(countActivityFlags([read('a'), shell('b')]), { failed: 0, denied: 0 })
  })
})
