/**
 * Exploring-group coalescing for the chat timeline.
 *
 * Adjacent collapsible/read-only tool items collapse into one synthetic
 * `exploring` ThreadItem. Mutating tools, agent text, and approvals break the
 * group. Grouping is UI-only — tool-call ids and model boundaries are unchanged.
 */

import type { ExploringGroup, ThreadItem, ToolCall, ToolStatus } from '../types/index.js'

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

/** Summarize an exploring group into action lines (Read / Search / List …). */
export function summarizeExploringSteps(tools: ToolCall[]): { action: string; detail: string }[] {
  const lines: { action: string; detail: string }[] = []
  const reads: string[] = []

  const flushReads = () => {
    if (reads.length === 0) return
    lines.push({ action: 'Read', detail: reads.join(', ') })
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
