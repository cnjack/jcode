/**
 * Completed-turn projection and approval/tool binding for the chat timeline.
 *
 * Both transforms are UI-only. They preserve the durable flat transcript and
 * stable tool-call identity while giving styled clients a coherent surface:
 * approvals live on the tool they gate, and completed turns keep only their
 * final assistant reply visible until the user expands the elapsed-time row.
 */

import type { Approval, ThreadItem, ToolCall } from '../types/index.js'

/**
 * Attach approval items to the concrete tool-call occurrence they gate.
 *
 * Tool-call ids are model supplied and may be reused across turns, so this does
 * not use a session-wide id map. Each approval binds to the nearest unmatched
 * occurrence inside the same user turn (normally the preceding tool; the
 * forward fallback covers transports that deliver approval before tool_call).
 * Matched approval items disappear from the projected list; unmatched legacy
 * approvals remain standalone so no decision UI is lost.
 */
export function bindApprovalsToTools(items: ThreadItem[]): ThreadItem[] {
  const bindings = new Map<number, Approval>()
  const matchedApprovals = new Set<number>()
  const occupiedTools = new Set<number>()

  for (let index = 0; index < items.length; index++) {
    const item = items[index]
    if (item.kind === 'tool' && item.data.approval) occupiedTools.add(index)
  }

  for (let approvalIndex = 0; approvalIndex < items.length; approvalIndex++) {
    const item = items[approvalIndex]
    if (item.kind !== 'approval' || !item.data.tool_call_id) continue

    const toolIndex = findApprovalTool(
      items,
      approvalIndex,
      item.data,
      occupiedTools,
    )
    if (toolIndex < 0) continue

    bindings.set(toolIndex, item.data)
    occupiedTools.add(toolIndex)
    matchedApprovals.add(approvalIndex)
  }

  if (bindings.size === 0) return items

  const out: ThreadItem[] = []
  for (let index = 0; index < items.length; index++) {
    if (matchedApprovals.has(index)) continue
    const item = items[index]
    const approval = bindings.get(index)
    if (item.kind !== 'tool' || !approval) {
      out.push(item)
      continue
    }

    // A terminal tool result is authoritative even when this browser missed
    // another client's approval-resolution event. Resolve the projected gate
    // so stale controls cannot survive beside a finished result.
    const hasTerminalEvidence =
      item.data.phase === 'terminal' ||
      item.data.output !== undefined ||
      item.data.error !== undefined ||
      item.data.denied !== undefined ||
      item.data.meta?.exit_code !== undefined
    const effectiveApproval: Approval =
      !approval.resolved && item.data.status !== 'running' && hasTerminalEvidence
        ? { ...approval, resolved: true, approved: !item.data.denied }
        : approval
    const pending = !effectiveApproval.resolved
    const denied = !!effectiveApproval.resolved && effectiveApproval.approved === false
    out.push({
      ...item,
      data: {
        ...item.data,
        approval: effectiveApproval,
        // Replay historically settles dangling generic tool_call entries to
        // `done`, even when /approval/pending proves the call is still gated.
        // The pending interaction is stronger UI evidence: restore running.
        status: pending ? 'running' : item.data.status,
        awaitingApproval: pending ? true : undefined,
        denied: denied || item.data.denied || undefined,
      },
    })
  }
  return out
}

function findApprovalTool(
  items: ThreadItem[],
  approvalIndex: number,
  approval: Approval,
  occupied: ReadonlySet<number>,
): number {
  let priorSettled = -1
  for (let index = approvalIndex - 1; index >= 0; index--) {
    const item = items[index]
    if (isUserMessage(item)) break
    if (
      item.kind === 'tool' &&
      toolMatchesApproval(item.data, approval) &&
      !occupied.has(index)
    ) {
      if (item.data.status === 'running') return index
      if (priorSettled < 0) priorSettled = index
    }
  }
  let followingSettled = -1
  for (let index = approvalIndex + 1; index < items.length; index++) {
    const item = items[index]
    if (isUserMessage(item)) break
    if (
      item.kind === 'tool' &&
      toolMatchesApproval(item.data, approval) &&
      !occupied.has(index)
    ) {
      if (item.data.status === 'running') return index
      if (followingSettled < 0) followingSettled = index
    }
  }
  return followingSettled >= 0 ? followingSettled : priorSettled
}

function toolMatchesApproval(tool: ToolCall, approval: Approval): boolean {
  // New Web events carry the host-generated approval occurrence id. When it
  // exists it is authoritative; never fall back to model IDs and accidentally
  // bind a concurrent sibling gate. Legacy/replayed calls use the bounded
  // current-turn ID + name fallback above.
  if (tool.approvalID) return tool.approvalID === approval.id
  return tool.toolCallID === approval.tool_call_id && tool.name === approval.tool_name
}

/** Options for {@link groupCompletedTurns}. */
export interface GroupCompletedTurnsOptions {
  /** The final user turn stays expanded while the runtime is active. */
  isRunning?: boolean
}

/**
 * Replace each settled user turn with:
 *
 *   user message -> collapsed activity/duration row + final assistant summary
 *                -> turn-changes summary (when present)
 *
 * A turn is left flat when it has no final assistant reply, contains unresolved
 * approval/running work, or has tool activity after its last assistant message.
 */
export function groupCompletedTurns(
  items: ThreadItem[],
  options: GroupCompletedTurnsOptions = {},
): ThreadItem[] {
  const starts: number[] = []
  for (let index = 0; index < items.length; index++) {
    if (isUserMessage(items[index])) starts.push(index)
  }
  if (starts.length === 0) return items

  const out: ThreadItem[] = items.slice(0, starts[0])
  for (let turnIndex = 0; turnIndex < starts.length; turnIndex++) {
    const start = starts[turnIndex]
    const end = starts[turnIndex + 1] ?? items.length
    const turn = items.slice(start, end)
    const isLastTurn = turnIndex === starts.length - 1

    if ((isLastTurn && options.isRunning) || hasOpenInteraction(turn)) {
      out.push(...turn)
      continue
    }

    let finalAssistantIndex = -1
    for (let index = turn.length - 1; index >= 1; index--) {
      const item = turn[index]
      if (item.kind === 'message' && item.data.role === 'assistant') {
        finalAssistantIndex = index
        break
      }
    }
    if (finalAssistantIndex < 0 || hasWorkAfterSummary(turn, finalAssistantIndex)) {
      out.push(...turn)
      continue
    }

    const user = turn[0]
    const summaryItem = turn[finalAssistantIndex]
    if (user.kind !== 'message' || summaryItem.kind !== 'message') {
      out.push(...turn)
      continue
    }

    const durationMs = turnDuration(user.data.timestamp, summaryItem.data.timestamp, summaryItem.data.durationMs)
    const intermediate = turn.slice(1, finalAssistantIndex)
    const visibleReceipts = intermediate.filter(isAlwaysVisibleReceipt)
    out.push(user)
    out.push({
      kind: 'turn',
      seq: summaryItem.seq,
      data: {
        id: `turn_${user.data.id}`,
        activity: intermediate.filter((item) => !isAlwaysVisibleReceipt(item)),
        summary: summaryItem.data,
        durationMs,
      },
    })
    // Provider-backed media/artifact surfaces and terminal errors are outcomes,
    // not implementation detail. Keep them visible beside the final summary.
    out.push(...visibleReceipts)
    out.push(...turn.slice(finalAssistantIndex + 1))
  }
  return out
}

function isUserMessage(item: ThreadItem): boolean {
  return item.kind === 'message' && item.data.role === 'user'
}

function hasOpenInteraction(items: ThreadItem[]): boolean {
  return items.some((item) => {
    if (item.kind === 'approval') return !item.data.resolved
    if (item.kind === 'tool') return isOpenTool(item.data)
    if (item.kind === 'activity') return item.data.tools.some(isOpenTool)
    if (item.kind === 'batch' || item.kind === 'exploring') {
      return item.data.tools.some(isOpenTool)
    }
    return false
  })
}

function isOpenTool(tool: ToolCall): boolean {
  return tool.status === 'running' || (!!tool.approval && !tool.approval.resolved)
}

function hasWorkAfterSummary(turn: ThreadItem[], finalAssistantIndex: number): boolean {
  return turn.slice(finalAssistantIndex + 1).some((item) => item.kind !== 'turnchanges')
}

function isAlwaysVisibleReceipt(item: ThreadItem): boolean {
  if (item.kind === 'tool') return item.data.surface === 'standalone'
  return item.kind === 'message' && item.data.role === 'system' && item.data.level === 'error'
}

function turnDuration(startedAt: number, summaryAt: number, stamped?: number): number {
  if (typeof stamped === 'number' && Number.isFinite(stamped) && stamped >= 0) {
    return stamped
  }
  if (!Number.isFinite(startedAt) || !Number.isFinite(summaryAt)) return 0
  return Math.max(0, summaryAt - startedAt)
}
