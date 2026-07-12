/**
 * TurnChangesCard — per-turn file-change summary (opencode SessionTurn-style).
 *
 * Header: `Changed N files` + a green/red `+A −R` badge when line counts are
 * available. Expanded: one row per file (path + ± counts); clicking a row
 * expands the LAST tool call that touched the file, rendered through the
 * regular registry body (diff/file renderers apply) via ToolCallCard slots —
 * the same reuse path as ToolBatchGroupCard rows.
 */

import { memo, useState } from 'react'
import { ChevronDownIcon } from '@heroicons/react/24/outline'
import type { TurnChangesSummary, TurnFileChange } from 'jcode-ui-core'
import { ToolCallCard } from './ToolCallCard.js'

export interface TurnChangesCardProps {
  summary: TurnChangesSummary
  className?: string
}

export const TurnChangesCard = memo(function TurnChangesCard({
  summary,
  className,
}: TurnChangesCardProps) {
  const [expanded, setExpanded] = useState(false)
  const [showOverflow, setShowOverflow] = useState(false)
  const { fileCount, files, overflow, totalAdded, totalRemoved, hasLineCounts } = summary

  return (
    <div
      data-jcode-ui=""
      className={`jcode-turnchanges my-1 ${className ?? ''}`}
      data-testid="turn-changes"
      data-expanded={expanded ? 'true' : 'false'}
    >
      <button
        type="button"
        onClick={() => setExpanded((e) => !e)}
        className="flex w-full max-w-full cursor-pointer items-center gap-1.5 bg-transparent py-0.5 text-left"
      >
        <span
          className="shrink-0 text-xs font-medium tracking-wide"
          style={{ color: 'var(--jcode-color-muted-foreground)' }}
        >
          Changed {fileCount} file{fileCount === 1 ? '' : 's'}
        </span>
        {hasLineCounts && (totalAdded > 0 || totalRemoved > 0) && (
          <span className="jcode-turnchanges__stat shrink-0 rounded-[var(--jcode-radius-sm)] bg-[var(--jcode-color-muted)] px-1.5 py-0.5 font-mono text-[10px] tabular-nums">
            {totalAdded > 0 && (
              <span style={{ color: 'var(--jcode-color-success-fg)' }}>+{totalAdded}</span>
            )}
            {totalAdded > 0 && totalRemoved > 0 && (
              <span className="mx-0.5" style={{ color: 'var(--jcode-color-muted-foreground)' }}>
                /
              </span>
            )}
            {totalRemoved > 0 && (
              <span style={{ color: 'var(--jcode-color-error-fg)' }}>−{totalRemoved}</span>
            )}
          </span>
        )}
        <ChevronDownIcon
          className={`h-3 w-3 shrink-0 text-[var(--jcode-color-muted-foreground)] transition-transform duration-[var(--jcode-duration-normal)] ${
            expanded ? 'rotate-180' : ''
          }`}
        />
      </button>

      {expanded && (
        <div className="jcode-turnchanges__files" data-testid="turn-changes-files">
          {files.map((change) => (
            <FileChangeRow key={change.path} change={change} />
          ))}
          {overflow.length > 0 &&
            (showOverflow ? (
              overflow.map((change) => <FileChangeRow key={change.path} change={change} />)
            ) : (
              <button
                type="button"
                onClick={() => setShowOverflow(true)}
                className="cursor-pointer bg-transparent py-0.5 text-left text-[11px]"
                style={{ color: 'var(--jcode-color-muted-foreground)' }}
                data-testid="turn-changes-more"
              >
                … {overflow.length} more
              </button>
            ))}
        </div>
      )}
    </div>
  )
})

/** One file row — a ToolCallCard whose slot header is the path + ± counts, so
 *  clicking the row expands that change's registry-rendered diff body. */
function FileChangeRow({ change }: { change: TurnFileChange }) {
  return (
    <ToolCallCard
      tool={change.tool}
      className="jcode-turnchanges__row"
      slots={{
        header: () => (
          <>
            <span
              className="min-w-0 truncate font-mono text-[0.72rem]"
              style={{ color: 'var(--jcode-color-foreground)', opacity: 0.88 }}
            >
              {change.path}
            </span>
            {(change.added !== undefined || change.removed !== undefined) && (
              <span className="ml-auto shrink-0 font-mono text-[10px] tabular-nums">
                {(change.added ?? 0) > 0 && (
                  <span style={{ color: 'var(--jcode-color-success-fg)' }}>+{change.added}</span>
                )}
                {(change.added ?? 0) > 0 && (change.removed ?? 0) > 0 && (
                  <span className="mx-0.5" style={{ color: 'var(--jcode-color-muted-foreground)' }}>
                    /
                  </span>
                )}
                {(change.removed ?? 0) > 0 && (
                  <span style={{ color: 'var(--jcode-color-error-fg)' }}>−{change.removed}</span>
                )}
              </span>
            )}
            <ChevronDownIcon
              className={`jcode-toolbatch__chevron h-3 w-3 shrink-0 text-[var(--jcode-color-muted-foreground)] transition-transform duration-[var(--jcode-duration-normal)] ${
                change.added === undefined && change.removed === undefined ? 'ml-auto' : ''
              }`}
            />
          </>
        ),
      }}
    />
  )
}
