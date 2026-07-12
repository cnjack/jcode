/**
 * Turn-level file-change aggregation (opencode SessionTurn-style).
 *
 * After a turn (one user message + everything the assistant produced before
 * the next user message) completes, the edit/write tools it ran are summarized
 * into one `turnchanges` ThreadItem: "Changed N files (+A −R)". Aggregation is
 * UI-only — tool-call ids and model boundaries are unchanged.
 *
 * ± line counts are derived client-side from tool args (the backend sends no
 * diff stats): edit/multi_edit count `old_string`/`new_string` lines, write
 * counts `content` lines as additions. Tools whose args carry no diff text
 * still list the file, just without counts.
 */

import type {
  ThreadItem,
  ToolCall,
  TurnChangesSummary,
  TurnFileChange,
} from '../types/index.js'

/** Tool names that mutate files and join the turn-changes summary. */
const CHANGE_TOOL_NAMES = new Set(['edit', 'multi_edit', 'write'])

/** Default display cap — files beyond it land in `overflow` ("… N more"). */
export const TURN_CHANGES_MAX_FILES = 10

/** True when a tool is an edit/write/patch-style file mutation. */
export function isFileChangeTool(tool: ToolCall): boolean {
  return CHANGE_TOOL_NAMES.has(tool.name)
}

/**
 * Derive ±line counts from edit/write args. Returns null when the args carry
 * no countable diff text (then the summary lists the file without counts).
 * Mirrors ToolCallCard's badge heuristic: line counts of old/new strings,
 * `write` counts content lines as additions (prior content is unknown).
 */
export function diffStatForTool(tool: ToolCall): { added: number; removed: number } | null {
  try {
    const parsed = JSON.parse(tool.args) as {
      old_string?: string
      new_string?: string
      content?: string
      edits?: Array<{ old_string?: string; new_string?: string }>
    }
    if (tool.name === 'write') {
      const content = parsed.content ?? ''
      if (!content) return null
      return { added: content.split('\n').length, removed: 0 }
    }
    // edit carries `edits` for multi-edit too (backend supports both shapes).
    if (Array.isArray(parsed.edits) && parsed.edits.length > 0) {
      let added = 0
      let removed = 0
      for (const e of parsed.edits) {
        if (e.old_string) removed += e.old_string.split('\n').length
        if (e.new_string) added += e.new_string.split('\n').length
      }
      return { added, removed }
    }
    const oldStr = parsed.old_string ?? ''
    const newStr = parsed.new_string ?? ''
    if (!oldStr && !newStr) return null
    // Creation (empty old_string): all added, nothing removed.
    return {
      added: newStr ? newStr.split('\n').length : 0,
      removed: oldStr ? oldStr.split('\n').length : 0,
    }
  } catch {
    return null
  }
}

function filePathOf(tool: ToolCall): string {
  try {
    const parsed = JSON.parse(tool.args) as { file_path?: string }
    if (typeof parsed.file_path === 'string' && parsed.file_path) return parsed.file_path
  } catch {
    /* fall through */
  }
  return ''
}

/** Flatten tool calls out of tool/activity/batch/exploring items (order preserved). */
function collectTools(items: ThreadItem[]): ToolCall[] {
  const tools: ToolCall[] = []
  for (const item of items) {
    if (item.kind === 'tool') tools.push(item.data)
    else if (item.kind === 'activity' || item.kind === 'batch' || item.kind === 'exploring') {
      tools.push(...item.data.tools)
    }
  }
  return tools
}

export interface SummarizeTurnChangesOptions {
  /** Display cap before files spill into `overflow`. Default 10. */
  maxFiles?: number
}

/**
 * Aggregate the file changes of one turn's items.
 *
 * Returns null when the turn has no completed file changes OR any tool in the
 * turn is still running (work in progress — the summary only appears once the
 * turn settles). Denied and errored change tools are skipped (they did not
 * touch the file). Files dedupe by path keeping the LAST change; totals sum
 * over the deduped set.
 */
export function summarizeTurnChanges(
  items: ThreadItem[],
  opts: SummarizeTurnChangesOptions = {},
): TurnChangesSummary | null {
  const maxFiles = opts.maxFiles ?? TURN_CHANGES_MAX_FILES
  const tools = collectTools(items)
  if (tools.some((t) => t.status === 'running')) return null

  // path → last change (insertion order preserved for stable display).
  const byPath = new Map<string, TurnFileChange>()
  for (const t of tools) {
    if (!isFileChangeTool(t)) continue
    if (t.status !== 'done' || t.denied) continue
    const path = filePathOf(t)
    if (!path) continue
    const stat = diffStatForTool(t)
    const change: TurnFileChange = {
      path,
      added: stat?.added,
      removed: stat?.removed,
      tool: t,
    }
    // Map.set on an existing key keeps first-seen order but the LAST data.
    byPath.set(path, change)
  }
  if (byPath.size === 0) return null

  const all = [...byPath.values()]
  let totalAdded = 0
  let totalRemoved = 0
  let hasLineCounts = false
  for (const c of all) {
    if (c.added !== undefined || c.removed !== undefined) hasLineCounts = true
    totalAdded += c.added ?? 0
    totalRemoved += c.removed ?? 0
  }
  const last = all[all.length - 1]
  return {
    id: `turnchanges_${last?.tool.id ?? 'turn'}`,
    fileCount: all.length,
    files: all.slice(0, maxFiles),
    overflow: all.slice(maxFiles),
    totalAdded,
    totalRemoved,
    hasLineCounts,
  }
}

export interface AppendTurnChangesOptions extends SummarizeTurnChangesOptions {
  /** While the runtime is streaming, the LAST turn is still open — suppress
   *  its summary even if no tool is currently running. */
  isRunning?: boolean
}

/**
 * Insert a `turnchanges` item at the end of every completed turn.
 *
 * Turn boundary = a user message up to (exclusive) the next user message.
 * Items before the first user message belong to no turn. The synthetic item's
 * seq is `last item seq + 0.5` — stable across re-renders and collision-free
 * against integer seqs. Intended as the LAST step of a `mapItems` pipeline
 * (after `groupToolTimeline`).
 */
export function appendTurnChangeSummaries(
  items: ThreadItem[],
  opts: AppendTurnChangesOptions = {},
): ThreadItem[] {
  // Find turn starts (user messages).
  const starts: number[] = []
  for (let i = 0; i < items.length; i++) {
    const item = items[i]
    if (item && item.kind === 'message' && item.data.role === 'user') starts.push(i)
  }
  if (starts.length === 0) return items

  const out: ThreadItem[] = []
  let cursor = 0
  for (let s = 0; s < starts.length; s++) {
    const start = starts[s] as number
    const end = s + 1 < starts.length ? (starts[s + 1] as number) : items.length
    // Copy anything before this turn (pre-first-turn items on s === 0).
    for (; cursor < start; cursor++) out.push(items[cursor] as ThreadItem)
    // Copy the turn itself.
    const turn = items.slice(start, end)
    for (; cursor < end; cursor++) out.push(items[cursor] as ThreadItem)
    // Last turn stays open while the runtime is running.
    const isLastTurn = s === starts.length - 1
    if (isLastTurn && opts.isRunning) continue
    const summary = summarizeTurnChanges(turn, opts)
    if (summary) {
      const lastSeq = turn[turn.length - 1]?.seq ?? 0
      out.push({ kind: 'turnchanges', data: summary, seq: lastSeq + 0.5 })
    }
  }
  return out
}
