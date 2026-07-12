/**
 * Unit tests for turn-level file-change aggregation.
 * Run: node --experimental-strip-types --test src/timeline/turnChanges.test.ts
 */

import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import {
  diffStatForTool,
  summarizeTurnChanges,
  appendTurnChangeSummaries,
} from './turnChanges.ts'
import type { Message, ThreadItem, ToolCall } from '../types/index.ts'

function tool(partial: Partial<ToolCall> & Pick<ToolCall, 'id' | 'name'>): ToolCall {
  return {
    args: '{}',
    status: 'done',
    timestamp: 0,
    ...partial,
  }
}

function editTool(
  id: string,
  file: string,
  oldStr: string,
  newStr: string,
  extra: Partial<ToolCall> = {},
): ToolCall {
  return tool({
    id,
    name: 'edit',
    args: JSON.stringify({ file_path: file, old_string: oldStr, new_string: newStr }),
    ...extra,
  })
}

function toolItem(t: ToolCall, seq: number): ThreadItem {
  return { kind: 'tool', data: t, seq }
}

function msgItem(role: Message['role'], id: string, seq: number): ThreadItem {
  return {
    kind: 'message',
    data: { id, role, content: id, timestamp: 0 },
    seq,
  }
}

describe('diffStatForTool', () => {
  it('counts edit old/new lines', () => {
    const t = editTool('e1', 'a.go', 'x\ny', 'x\ny\nz')
    assert.deepEqual(diffStatForTool(t), { added: 3, removed: 2 })
  })
  it('counts creation (empty old_string) as pure addition', () => {
    const t = editTool('e1', 'a.go', '', 'one\ntwo')
    assert.deepEqual(diffStatForTool(t), { added: 2, removed: 0 })
  })
  it('sums multi-edit edits arrays (edit or multi_edit name)', () => {
    const args = JSON.stringify({
      file_path: 'a.go',
      edits: [
        { old_string: 'a', new_string: 'a\nb' },
        { old_string: 'c\nd', new_string: 'e' },
      ],
    })
    assert.deepEqual(diffStatForTool(tool({ id: 'm1', name: 'multi_edit', args })), {
      added: 3,
      removed: 3,
    })
    assert.deepEqual(diffStatForTool(tool({ id: 'm2', name: 'edit', args })), {
      added: 3,
      removed: 3,
    })
  })
  it('counts write content as additions only', () => {
    const t = tool({
      id: 'w1',
      name: 'write',
      args: JSON.stringify({ file_path: 'b.ts', content: '1\n2\n3' }),
    })
    assert.deepEqual(diffStatForTool(t), { added: 3, removed: 0 })
  })
  it('returns null on unparseable or empty args', () => {
    assert.equal(diffStatForTool(tool({ id: 'x', name: 'edit', args: 'not json' })), null)
    assert.equal(
      diffStatForTool(tool({ id: 'y', name: 'edit', args: JSON.stringify({ file_path: 'a' }) })),
      null,
    )
  })
})

describe('summarizeTurnChanges', () => {
  it('dedupes by file keeping the last change', () => {
    const first = editTool('e1', 'a.go', 'x', 'y\nz')
    const second = editTool('e2', 'a.go', 'y\nz\nw', 'k')
    const out = summarizeTurnChanges([toolItem(first, 1), toolItem(second, 2)])
    assert.ok(out)
    assert.equal(out.fileCount, 1)
    assert.equal(out.files[0]?.tool.id, 'e2')
    assert.equal(out.files[0]?.added, 1)
    assert.equal(out.files[0]?.removed, 3)
    assert.equal(out.totalAdded, 1)
    assert.equal(out.totalRemoved, 3)
  })

  it('keeps first-seen file order across dedupe', () => {
    const items = [
      toolItem(editTool('e1', 'a.go', 'x', 'y'), 1),
      toolItem(editTool('e2', 'b.go', 'x', 'y'), 2),
      toolItem(editTool('e3', 'a.go', 'y', 'z'), 3),
    ]
    const out = summarizeTurnChanges(items)
    assert.ok(out)
    assert.deepEqual(
      out.files.map((f) => f.path),
      ['a.go', 'b.go'],
    )
    assert.equal(out.files[0]?.tool.id, 'e3')
  })

  it('caps at maxFiles and spills the rest into overflow', () => {
    const items = Array.from({ length: 12 }, (_, i) =>
      toolItem(editTool(`e${i}`, `f${i}.go`, 'a', 'b'), i + 1),
    )
    const out = summarizeTurnChanges(items)
    assert.ok(out)
    assert.equal(out.fileCount, 12)
    assert.equal(out.files.length, 10)
    assert.equal(out.overflow.length, 2)
    // Totals cover ALL files, not just the displayed ones.
    assert.equal(out.totalAdded, 12)
    assert.equal(out.totalRemoved, 12)
  })

  it('returns null while any tool in the turn is running', () => {
    const items = [
      toolItem(editTool('e1', 'a.go', 'x', 'y'), 1),
      toolItem(tool({ id: 'r1', name: 'execute', status: 'running' }), 2),
    ]
    assert.equal(summarizeTurnChanges(items), null)
  })

  it('skips denied and errored change tools', () => {
    const items = [
      toolItem(editTool('e1', 'a.go', 'x', 'y', { denied: true }), 1),
      toolItem(editTool('e2', 'b.go', 'x', 'y', { status: 'error' }), 2),
    ]
    assert.equal(summarizeTurnChanges(items), null)
  })

  it('lists files without counts when args carry no diff text', () => {
    const items = [
      toolItem(tool({ id: 'w1', name: 'write', args: JSON.stringify({ file_path: 'a.md' }) }), 1),
    ]
    const out = summarizeTurnChanges(items)
    assert.ok(out)
    assert.equal(out.files[0]?.added, undefined)
    assert.equal(out.hasLineCounts, false)
  })

  it('collects change tools inside batch groups', () => {
    const batch: ThreadItem = {
      kind: 'batch',
      seq: 1,
      data: {
        id: 'batch_1',
        batchId: 'b1',
        tools: [editTool('e1', 'a.go', 'x', 'y'), editTool('e2', 'b.go', 'x', 'y\nz')],
        status: 'done',
        explorative: false,
      },
    }
    const out = summarizeTurnChanges([batch])
    assert.ok(out)
    assert.equal(out.fileCount, 2)
    assert.equal(out.totalAdded, 3)
  })

  it('collects change tools inside activity groups', () => {
    const activity: ThreadItem = {
      kind: 'activity',
      seq: 1,
      data: {
        id: 'activity_1',
        tools: [
          tool({ id: 'r1', name: 'read' }),
          editTool('e1', 'a.go', 'x', 'y'),
          editTool('e2', 'b.go', 'x', 'y\nz'),
        ],
        status: 'done',
        explorative: false,
      },
    }
    const out = summarizeTurnChanges([activity])
    assert.ok(out)
    assert.equal(out.fileCount, 2)
    assert.equal(out.totalAdded, 3)
  })

  it('returns null when the turn changed no files', () => {
    const items = [toolItem(tool({ id: 'r1', name: 'read' }), 1)]
    assert.equal(summarizeTurnChanges(items), null)
  })
})

describe('appendTurnChangeSummaries', () => {
  it('appends a summary at the end of each completed turn', () => {
    const items: ThreadItem[] = [
      msgItem('user', 'u1', 1),
      toolItem(editTool('e1', 'a.go', 'x', 'y'), 2),
      msgItem('assistant', 'a1', 3),
      msgItem('user', 'u2', 4),
      toolItem(editTool('e2', 'b.go', 'x', 'y'), 5),
      msgItem('assistant', 'a2', 6),
    ]
    const out = appendTurnChangeSummaries(items)
    assert.equal(out.length, 8)
    assert.equal(out[3]?.kind, 'turnchanges')
    assert.equal(out[3]?.seq, 3.5)
    assert.equal(out[7]?.kind, 'turnchanges')
    if (out[3]?.kind === 'turnchanges') {
      assert.equal(out[3].data.files[0]?.path, 'a.go')
    }
  })

  it('leaves pre-first-user-message items outside any turn', () => {
    const items: ThreadItem[] = [
      msgItem('assistant', 'a0', 1),
      toolItem(editTool('e0', 'pre.go', 'x', 'y'), 2),
      msgItem('user', 'u1', 3),
      msgItem('assistant', 'a1', 4),
    ]
    const out = appendTurnChangeSummaries(items)
    // pre.go was edited before any user message → no summary anywhere.
    assert.equal(out.length, 4)
    assert.ok(out.every((i) => i.kind !== 'turnchanges'))
  })

  it('suppresses the last turn while isRunning', () => {
    const items: ThreadItem[] = [
      msgItem('user', 'u1', 1),
      toolItem(editTool('e1', 'a.go', 'x', 'y'), 2),
      msgItem('user', 'u2', 3),
      toolItem(editTool('e2', 'b.go', 'x', 'y'), 4),
    ]
    const out = appendTurnChangeSummaries(items, { isRunning: true })
    const marks = out.filter((i) => i.kind === 'turnchanges')
    assert.equal(marks.length, 1)
    if (marks[0]?.kind === 'turnchanges') {
      assert.equal(marks[0].data.files[0]?.path, 'a.go')
    }
  })

  it('emits nothing for a turn with a running tool', () => {
    const items: ThreadItem[] = [
      msgItem('user', 'u1', 1),
      toolItem(editTool('e1', 'a.go', 'x', 'y'), 2),
      toolItem(tool({ id: 'r1', name: 'execute', status: 'running' }), 3),
    ]
    const out = appendTurnChangeSummaries(items)
    assert.ok(out.every((i) => i.kind !== 'turnchanges'))
  })

  it('returns items untouched when there is no user message', () => {
    const items: ThreadItem[] = [
      msgItem('assistant', 'a1', 1),
      toolItem(editTool('e1', 'a.go', 'x', 'y'), 2),
    ]
    assert.equal(appendTurnChangeSummaries(items), items)
  })
})
