/**
 * ToolRow — one compact status row inside a grouped tool container
 * (ActivityGroupCard, ToolBatchGroupCard).
 *
 * A ToolCallCard whose slot header is the row line — status icon (● running /
 * ✓ done / ✗ error / ⊘ denied) + title + mono subtitle + right-side elapsed
 * or exit badge + chevron — so clicking anywhere on the row expands the
 * tool's full registry-rendered body in place (diff/shell/subagent renderers
 * apply). Extracted from ToolBatchGroupCard so activity groups and batches
 * share ONE row implementation.
 */

import { useRef } from 'react'
import { ChevronDownIcon, ShieldCheckIcon } from '@heroicons/react/24/outline'
import type { ToolCall } from 'jcode-ui-core'
import { useElapsed, formatElapsed } from 'jcode-ui-core'
import { ToolCallCard } from './ToolCallCard.js'

/** Hide sub-2s durations on finished rows (only slow steps earn a badge). */
const MIN_DURATION_MS = 2000

export interface ToolRowProps {
  tool: ToolCall
  /** Row class — defaults to the shared `.jcode-toolbatch__row` styling. */
  className?: string
}

/** One expandable row: slot-headered ToolCallCard with the compact row line. */
export function ToolRow({ tool, className }: ToolRowProps) {
  return (
    <ToolCallCard
      tool={tool}
      className={className ?? 'jcode-toolbatch__row'}
      slots={{ header: (t) => <ToolRowHeader tool={t} /> }}
    />
  )
}

/**
 * Slot header for a grouped tool row. Rendered inside ToolCallCard's
 * slot-header button, so clicking anywhere on the row toggles the tool body.
 */
export function ToolRowHeader({ tool }: { tool: ToolCall }) {
  const running = tool.status === 'running'
  // Denied (user rejected at the approval prompt) ≠ error: struck-through and
  // muted, never red. Awaiting approval paints the row in the warning color
  // and pauses the live elapsed badge (the backend excludes the wait anyway).
  const isDenied = !!tool.denied
  const isAwaiting = !isDenied && !!tool.awaitingApproval && running
  const isAllowed = !isDenied && !!tool.approval?.resolved && tool.approval.approved === true
  const isError =
    !isDenied &&
    (tool.status === 'error' || (tool.meta?.exit_code !== undefined && tool.meta.exit_code !== 0))
  const title = tool.displayInfo?.title ?? tool.name
  const subtitle = tool.displayInfo?.subtitle ?? ''

  // Frozen fallback duration: when a row we observed running completes without
  // meta.duration_ms, capture `now - startedAt` once at the transition.
  const sawRunning = useRef(running)
  const frozenMs = useRef<number | undefined>(undefined)
  if (running) {
    sawRunning.current = true
  } else if (sawRunning.current && frozenMs.current === undefined && tool.startedAt) {
    frozenMs.current = Date.now() - tool.startedAt
  }
  const durationMs = tool.meta?.duration_ms ?? frozenMs.current

  const exitBadge =
    isError && tool.meta?.exit_code !== undefined ? `exit ${tool.meta.exit_code}` : null
  const durationBadge =
    !running && !isDenied && durationMs !== undefined && durationMs > MIN_DURATION_MS
      ? formatElapsed(durationMs)
      : null

  return (
    <>
      <span
        className={`jcode-toolbatch__status shrink-0 text-[10px] ${running && !isAwaiting ? 'animate-pulse' : ''}`}
        style={{
          color: isAwaiting
            ? 'var(--jcode-color-warning-fg)'
            : running
              ? 'var(--jcode-color-primary)'
              : isDenied
                ? 'var(--jcode-color-muted-foreground)'
                : isError
                  ? 'var(--jcode-color-error-fg)'
                  : 'var(--jcode-color-success-fg)',
        }}
        aria-hidden
      >
        {isDenied ? '⊘' : running ? '●' : isError ? '✗' : '✓'}
      </span>
      <span
        className={`shrink-0 text-xs font-medium tracking-wide ${running && !isAwaiting ? 'shimmer-running' : ''} ${isDenied ? 'line-through' : ''}`}
        style={{
          color: isError
            ? 'var(--jcode-color-destructive, var(--jcode-color-error-fg))'
            : isAwaiting
              ? 'var(--jcode-color-warning-fg)'
              : 'var(--jcode-color-muted-foreground)',
        }}
      >
        {title}
      </span>
      {subtitle && (
        <span
          className={`jcode-toolcall__subtitle min-w-0 truncate font-mono text-[0.72rem] ${isDenied ? 'line-through' : ''}`}
          style={{
            color: isDenied ? 'var(--jcode-color-muted-foreground)' : 'var(--jcode-color-foreground)',
            opacity: 0.88,
          }}
          dangerouslySetInnerHTML={{ __html: subtitle }}
        />
      )}
      {isAllowed && (
        <span
          className="jcode-toolcall__allowed inline-flex shrink-0 items-center gap-1 text-[10px] font-medium"
          style={{ color: 'var(--jcode-color-success-fg)' }}
        >
          <ShieldCheckIcon className="h-3 w-3" aria-hidden />
          Allowed
        </span>
      )}
      <span
        className="ml-auto flex shrink-0 items-center gap-1.5 font-mono text-[10px] tabular-nums"
        style={{
          color: isError
            ? 'var(--jcode-color-error-fg)'
            : isAwaiting
              ? 'var(--jcode-color-warning-fg)'
              : 'var(--jcode-color-muted-foreground)',
        }}
      >
        {exitBadge}
        {isDenied ? (
          <span className="jcode-toolcall__denied">Denied</span>
        ) : isAwaiting ? (
          <span>approval…</span>
        ) : running ? (
          <RunningElapsed startedAt={tool.startedAt} />
        ) : (
          durationBadge
        )}
      </span>
      <ChevronDownIcon className="jcode-toolbatch__chevron h-3 w-3 shrink-0 text-[var(--jcode-color-muted-foreground)] transition-transform duration-[var(--jcode-duration-normal)]" />
    </>
  )
}

/** Live elapsed badge — mounted only on running rows so the 1s interval only
 *  exists while something is actually running. */
function RunningElapsed({ startedAt }: { startedAt?: number }) {
  const elapsed = useElapsed(startedAt, true)
  if (!startedAt) return null
  return <span>{formatElapsed(elapsed)}</span>
}
