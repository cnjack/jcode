/**
 * Exploring-group coalescing for the chat timeline.
 *
 * Adjacent collapsible/read-only tool items collapse into one synthetic
 * `exploring` ThreadItem. Mutating tools, agent text, and approvals break the
 * group. Grouping is UI-only — tool-call ids and model boundaries are unchanged.
 */

import type { ExploringGroup, ThreadItem, ToolBatchGroup, ToolCall, ToolStatus } from '../types/index.js'

const COLLAPSIBLE_NAMES = new Set([
  'read',
  'grep',
  'glob',
  'todoread',
  'load_skill',
  'browser_snapshot',
  'browser_screenshot',
  'browser_read',
  'browser_tabs',
])

/** True when a tool should join an Exploring/Explored group. */
export function isCollapsibleTool(tool: ToolCall): boolean {
  if (tool.displayInfo?.collapsible === true) return true
  if (tool.displayInfo?.collapsible === false) return false
  if (tool.displayInfo?.category === 'context') return true
  if (tool.presentation?.collapsible === true) return true
  if (COLLAPSIBLE_NAMES.has(tool.name)) return true
  // execute classified as read/search/list by the backend
  const kind = tool.displayInfo?.kind ?? tool.presentation?.kind
  if (tool.name === 'execute' && (kind === 'read' || kind === 'search' || kind === 'list')) {
    return true
  }
  return false
}

function toolStatus(tools: ToolCall[]): ToolStatus {
  if (tools.some((t) => t.status === 'running')) return 'running'
  if (tools.some((t) => t.status === 'error')) return 'error'
  return 'done'
}

function makeExploring(tools: ToolCall[], seq: number): ThreadItem {
  const group: ExploringGroup = {
    id: `explore_${tools[0]?.id ?? seq}`,
    tools: [...tools],
    status: toolStatus(tools),
  }
  return { kind: 'exploring', data: group, seq }
}

/**
 * Collapse consecutive collapsible tools into exploring groups.
 * Non-tool items and non-collapsible tools always break a group.
 * @deprecated Superseded by `groupActivityTimeline` (activity groups coalesce
 * ALL adjacent tools, not just read-only ones). Kept for external consumers.
 */
export function groupExploringTimeline(items: ThreadItem[]): ThreadItem[] {
  const out: ThreadItem[] = []
  let pending: ToolCall[] = []
  let pendingSeq = 0

  const flush = () => {
    if (pending.length === 0) return
    if (pending.length === 1) {
      // Single collapsible tool stays as a normal tool card (no noisy group of 1).
      out.push({ kind: 'tool', data: pending[0], seq: pendingSeq })
    } else {
      out.push(makeExploring(pending, pendingSeq))
    }
    pending = []
  }

  for (const item of items) {
    if (item.kind === 'tool' && isCollapsibleTool(item.data)) {
      if (pending.length === 0) pendingSeq = item.seq
      pending.push(item.data)
      continue
    }
    flush()
    out.push(item)
  }
  flush()
  return out
}

/**
 * Coalesce tool calls that share a `batchId` (concurrent calls from one
 * assistant message) into `batch` items anchored at the first member's
 * position. Items in between (approvals, messages) stay in place and do NOT
 * break a batch. Single-member batches unwrap back to plain tool cards, and
 * tools without a batchId (old sessions / replay) keep the existing
 * exploring-adjacent coalescing behavior unchanged.
 * @deprecated Superseded by `groupActivityTimeline`, which absorbs the batch
 * coalescing and merges ALL adjacent tools/batches into `activity` groups.
 * Kept for external consumers (and nested subagent-children rendering).
 */
export function groupToolTimeline(items: ThreadItem[]): ThreadItem[] {
  // Pass 1: gather same-batchId tools into a group at the first member's slot.
  const out: ThreadItem[] = []
  const groups = new Map<string, ToolBatchGroup>()
  for (const item of items) {
    if (item.kind === 'tool' && item.data.batchId) {
      const existing = groups.get(item.data.batchId)
      if (existing) {
        existing.tools.push(item.data)
        continue
      }
      const group: ToolBatchGroup = {
        id: `batch_${item.data.batchId}`,
        batchId: item.data.batchId,
        tools: [item.data],
        status: 'done',
        explorative: false,
      }
      groups.set(item.data.batchId, group)
      out.push({ kind: 'batch', data: group, seq: item.seq })
      continue
    }
    out.push(item)
  }
  // Finalize groups: unwrap singletons, compute status + explorative.
  const finalized = out.map((item): ThreadItem => {
    if (item.kind !== 'batch') return item
    const g = item.data
    if (g.tools.length === 1) {
      // No noisy batch of 1 — keep the normal tool card.
      return { kind: 'tool', data: g.tools[0], seq: item.seq }
    }
    g.status = toolStatus(g.tools)
    g.explorative = g.tools.every(isCollapsibleTool)
    return item
  })
  // Pass 2: legacy exploring coalescing for unbatched tools (batch items are
  // not tool items, so they naturally act as group boundaries).
  return groupExploringTimeline(finalized)
}

/** Summarize an exploring group into action lines (Read / Search / List …). */
export function summarizeExploringSteps(tools: ToolCall[]): { action: string; detail: string }[] {
  const lines: { action: string; detail: string }[] = []
  const reads: string[] = []

  const flushReads = () => {
    if (reads.length === 0) return
    // Adjacent reads merge into one line; repeated file names dedupe.
    lines.push({ action: 'Read', detail: [...new Set(reads)].join(', ') })
    reads.length = 0
  }

  for (const t of tools) {
    const title = (t.displayInfo?.title || t.name).trim()
    const subtitle = (t.displayInfo?.subtitle || '').trim()
    const kind = t.displayInfo?.kind || t.presentation?.kind || ''
    const isRead =
      t.name === 'read' || kind === 'read' || title.toLowerCase() === 'read'

    if (isRead) {
      reads.push(subtitle || title)
      continue
    }
    flushReads()

    let action = title
    if (t.name === 'grep' || kind === 'search') action = 'Search'
    else if (t.name === 'glob' || kind === 'list') action = t.name === 'glob' ? 'Glob' : 'List'
    else if (t.name === 'execute') {
      if (kind === 'list') action = 'List'
      else if (kind === 'search') action = 'Search'
      else if (kind === 'read') action = 'Read'
      else action = 'Ran'
    }
    lines.push({ action, detail: subtitle })
  }
  flushReads()
  return lines
}

/**
 * Bucket exploring steps into a compact category-count summary, e.g.
 * `3 files read · 2 searches · 1 list`. Read counts dedupe by file name
 * (subtitle) so re-reads of the same file count once.
 */
export function summarizeExploringCounts(tools: ToolCall[]): string {
  let reads = 0
  let searches = 0
  let lists = 0
  let other = 0
  const readFiles = new Set<string>()

  for (const t of tools) {
    const kind = t.displayInfo?.kind || t.presentation?.kind || ''
    const title = (t.displayInfo?.title || t.name).trim().toLowerCase()
    if (t.name === 'read' || kind === 'read' || title === 'read') {
      reads += 1
      const file = (t.displayInfo?.subtitle || '').trim()
      if (file) readFiles.add(file)
    } else if (t.name === 'grep' || kind === 'search') {
      searches += 1
    } else if (t.name === 'glob' || t.name === 'list_dir' || kind === 'list') {
      lists += 1
    } else {
      other += 1
    }
  }

  const parts: string[] = []
  const fileCount = readFiles.size || reads
  if (reads > 0) parts.push(`${fileCount} file${fileCount === 1 ? '' : 's'} read`)
  if (searches > 0) parts.push(`${searches} search${searches === 1 ? '' : 'es'}`)
  if (lists > 0) parts.push(`${lists} list${lists === 1 ? '' : 's'}`)
  if (other > 0) parts.push(`${other} other`)
  return parts.join(' · ')
}
