/**
 * Activity-group coalescing for the chat timeline (Claude Code / Codex-style).
 *
 * All adjacent default-surface tool items — read-only or mutating, batched or
 * not — coalesce
 * into ONE synthetic `activity` ThreadItem. "Adjacent" means no assistant/user
 * message (or any other non-tool item) in between; approvals do NOT break a
 * group and keep rendering in place, independently. Tools sharing a `batchId`
 * are first pulled to the first member's slot (the batch-coalescing logic
 * absorbed from `groupToolTimeline`), then adjacent tools/batches merge.
 *
 * Grouping is UI-only — tool-call ids and model boundaries are unchanged.
 * Isolated single tools stay plain `tool` cards (no noisy group of 1).
 */

import type { ActivityGroup, ThreadItem, ToolCall, ToolStatus } from '../types/index.js'
// NOTE: runtime import uses the .ts extension so `node --experimental-strip-types
// --test` resolves it directly; tsc rewrites it to .js on emit
// (rewriteRelativeImportExtensions).
import { isCollapsibleTool } from './groupExploring.ts'

function toolStatus(tools: ToolCall[]): ToolStatus {
  if (tools.some((t) => t.status === 'running')) return 'running'
  if (tools.some((t) => t.status === 'error')) return 'error'
  return 'done'
}

/** Pass-1 unit: either a run of tools (a batch or a lone tool) or a pass-through item. */
type Unit = { cluster: { tools: ToolCall[]; seq: number } } | { item: ThreadItem }

/** Standalone tools own their complete timeline surface and are hard grouping
 * boundaries from the initial tool_call event onward. */
export function isStandaloneTool(tool: ToolCall): boolean {
  return tool.surface === 'standalone' || tool.name === 'ask_user'
}

/**
 * Coalesce adjacent tool items (and whole `batchId` batches) into `activity`
 * groups. Output contains only `'activity'` items (≥2 tools) and plain
 * `'tool'` items (isolated singles) — never `'exploring'` or `'batch'`.
 *
 * - Batch members are anchored at the FIRST member's position even when
 *   approvals (or anything else) sit between them.
 * - Approvals never break a group; they render right after the group anchor.
 * - Any other item (message, turnchanges, …) closes the open group.
 */
export function groupActivityTimeline(items: ThreadItem[]): ThreadItem[] {
  // Pass 1: pull same-batchId tools to the first member's slot.
  const units: Unit[] = []
  const batches = new Map<string, { tools: ToolCall[]; seq: number }>()
  for (const item of items) {
    if (item.kind === 'tool') {
      if (isStandaloneTool(item.data)) {
        // A batch must never pull a later member backwards across a standalone
        // media surface. Reset the batch index and preserve this item in place.
        batches.clear()
        units.push({ item })
        continue
      }
      const batchId = item.data.batchId
      if (batchId) {
        const existing = batches.get(batchId)
        if (existing) {
          existing.tools.push(item.data)
          continue
        }
        const cluster = { tools: [item.data], seq: item.seq }
        batches.set(batchId, cluster)
        units.push({ cluster })
        continue
      }
      units.push({ cluster: { tools: [item.data], seq: item.seq } })
      continue
    }
    units.push({ item })
  }

  // Pass 2: merge adjacent clusters into one activity group anchored at the
  // first cluster's position. Approvals pass through without closing it.
  const out: ThreadItem[] = []
  let current: ActivityGroup | null = null
  for (const unit of units) {
    if ('cluster' in unit) {
      if (current) {
        current.tools.push(...unit.cluster.tools)
        continue
      }
      current = {
        id: `activity_${unit.cluster.tools[0]?.id ?? unit.cluster.seq}`,
        tools: [...unit.cluster.tools],
        status: 'done',
        explorative: false,
      }
      out.push({ kind: 'activity', data: current, seq: unit.cluster.seq })
      continue
    }
    if (unit.item.kind === 'approval') {
      out.push(unit.item)
      continue
    }
    current = null
    out.push(unit.item)
  }

  // Finalize: unwrap single-tool groups, compute status + explorative.
  return out.map((item): ThreadItem => {
    if (item.kind !== 'activity') return item
    const g = item.data
    if (g.tools.length === 1) {
      return { kind: 'tool', data: g.tools[0] as ToolCall, seq: item.seq }
    }
    g.status = toolStatus(g.tools)
    g.explorative = g.tools.every(isCollapsibleTool)
    return item
  })
}

/**
 * Count the collapsed-header suffix flags of an activity group. `failed` =
 * errored or nonzero exit code (denied tools excluded — a user decision is
 * not a failure); `denied` = rejected at the approval prompt.
 */
export function countActivityFlags(tools: ToolCall[]): { failed: number; denied: number } {
  let failed = 0
  let denied = 0
  for (const t of tools) {
    if (t.denied) {
      denied += 1
      continue
    }
    if (t.status === 'error' || (t.meta?.exit_code !== undefined && t.meta.exit_code !== 0)) {
      failed += 1
    }
  }
  return { failed, denied }
}

type ActivityBucket = 'command' | 'read' | 'search' | 'list' | 'edit' | 'agent' | 'other'

/**
 * Classify one tool into a summary bucket. `execute` ALWAYS counts as a
 * command — even when the backend classifies it read/search/list (a
 * `git grep` is still a command, not "a search"). Explicit tool names win
 * over `displayInfo.kind` so e.g. `glob` with kind 'search' stays a list.
 */
function bucketOf(t: ToolCall): ActivityBucket {
  const kind = t.displayInfo?.kind ?? t.presentation?.kind ?? ''
  if (t.name === 'execute' || kind === 'shell') return 'command'
  if (t.name === 'subagent' || t.name === 'task' || t.name === 'team_spawn' || kind === 'agent') {
    return 'agent'
  }
  if (t.name === 'edit' || t.name === 'multi_edit' || t.name === 'write' || kind === 'edit') {
    return 'edit'
  }
  if (t.name === 'read') return 'read'
  if (t.name === 'grep') return 'search'
  if (t.name === 'glob' || t.name === 'list_dir') return 'list'
  if (kind === 'read') return 'read'
  if (kind === 'search') return 'search'
  if (kind === 'list') return 'list'
  return 'other'
}

/**
 * Bucket an activity group's tools into a compact category-count header.
 *
 * - Mixed groups: `Ran 3 commands · read 2 files · ran 1 agent` (verb
 *   phrases, first segment capitalized).
 * - All-read-only groups (every tool passes `isCollapsibleTool`): the
 *   Explored phrasing `3 files read · 2 searches · 1 list` — the card
 *   prefixes its own `Explored` label.
 *
 * Reads and edits dedupe by file (`displayInfo.subtitle`) so re-touching the
 * same file counts once.
 */
export function summarizeActivityCounts(tools: ToolCall[]): string {
  const counts: Record<ActivityBucket, number> = {
    command: 0,
    read: 0,
    search: 0,
    list: 0,
    edit: 0,
    agent: 0,
    other: 0,
  }
  const readFiles = new Set<string>()
  const editFiles = new Set<string>()

  for (const t of tools) {
    const bucket = bucketOf(t)
    counts[bucket] += 1
    const file = (t.displayInfo?.subtitle ?? '').trim()
    if (file && bucket === 'read') readFiles.add(file)
    if (file && bucket === 'edit') editFiles.add(file)
  }

  const readCount = readFiles.size || counts.read
  const editCount = editFiles.size || counts.edit
  const plural = (n: number, one: string, many: string) => (n === 1 ? one : many)
  const parts: string[] = []

  const explorative = tools.length > 0 && tools.every(isCollapsibleTool)
  if (explorative) {
    // Explored phrasing — noun-first, matches the classic Exploring card.
    if (counts.read > 0) parts.push(`${readCount} ${plural(readCount, 'file', 'files')} read`)
    if (counts.search > 0) parts.push(`${counts.search} ${plural(counts.search, 'search', 'searches')}`)
    if (counts.list > 0) parts.push(`${counts.list} ${plural(counts.list, 'list', 'lists')}`)
    if (counts.command > 0) parts.push(`${counts.command} ${plural(counts.command, 'command', 'commands')}`)
    if (counts.other > 0) parts.push(`${counts.other} other`)
    return parts.join(' · ')
  }

  if (counts.command > 0) parts.push(`ran ${counts.command} ${plural(counts.command, 'command', 'commands')}`)
  if (counts.read > 0) parts.push(`read ${readCount} ${plural(readCount, 'file', 'files')}`)
  if (counts.search > 0) parts.push(`${counts.search} ${plural(counts.search, 'search', 'searches')}`)
  if (counts.list > 0) parts.push(`${counts.list} ${plural(counts.list, 'list', 'lists')}`)
  if (counts.edit > 0) parts.push(`edited ${editCount} ${plural(editCount, 'file', 'files')}`)
  if (counts.agent > 0) parts.push(`ran ${counts.agent} ${plural(counts.agent, 'agent', 'agents')}`)
  if (counts.other > 0) parts.push(`${counts.other} other`)
  const joined = parts.join(' · ')
  return joined.charAt(0).toUpperCase() + joined.slice(1)
}
