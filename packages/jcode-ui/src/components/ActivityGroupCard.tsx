/**
 * ActivityGroupCard — Claude Code / Codex-style collapsed activity group for
 * ALL adjacent tool calls in the timeline.
 *
 * Collapsed (every member settled): ONE muted line — chevron + status icon +
 * category counts (`Ran 3 commands · read 2 files · ran 1 agent`; all-read-
 * only groups say `Explored` + `3 files read · 2 searches`). Failures stay
 * visible collapsed: error-colored icon + `N failed` suffix; denied members
 * append a muted `N denied`.
 *
 * Expanded: a bordered card with one ToolRow per tool; each row expands IN
 * PLACE to the tool's registry-rendered body (diff/shell/subagent renderers
 * apply) — never a second duplicate list.
 *
 * Expansion state machine: `expanded = userOverride ?? (status === 'running')`
 * — auto-open while anything runs (live rows + elapsed), auto-collapse when
 * the group settles, and manual toggles win from then on.
 */

import { memo, useMemo, useState } from 'react'
import { ChevronDownIcon } from '@heroicons/react/24/outline'
import type { ActivityGroup } from 'jcode-ui-core'
import { summarizeActivityCounts, countActivityFlags } from 'jcode-ui-core'
import { ToolRow } from './ToolRow.js'

export interface ActivityGroupCardProps {
  group: ActivityGroup
  className?: string
}

export const ActivityGroupCard = memo(function ActivityGroupCard({
  group,
  className,
}: ActivityGroupCardProps) {
  // undefined → follow the running state; a manual toggle always wins after.
  const [userOverride, setUserOverride] = useState<boolean | undefined>(undefined)
  const running = group.status === 'running'
  const expanded = userOverride ?? running

  const counts = useMemo(() => summarizeActivityCounts(group.tools), [group.tools])
  const { failed, denied } = useMemo(() => countActivityFlags(group.tools), [group.tools])
  const hasError = failed > 0

  return (
    <div
      data-jcode-ui=""
      className={`jcode-activity my-1 ${className ?? ''}`}
      data-tool-name="activity"
      data-tool-status={group.status}
      data-expanded={expanded ? 'true' : 'false'}
      data-testid="activity-group"
    >
      <button
        type="button"
        onClick={() => setUserOverride(!expanded)}
        className="jcode-activity__header flex w-full max-w-full cursor-pointer items-center gap-1.5 bg-transparent text-left"
      >
        <span
          className={`jcode-toolbatch__status shrink-0 text-[10px] ${running ? 'animate-pulse' : ''}`}
          style={{
            color: running
              ? 'var(--jcode-color-primary)'
              : hasError
                ? 'var(--jcode-color-error-fg)'
                : 'var(--jcode-color-success-fg)',
          }}
          aria-hidden
        >
          {running ? '●' : hasError ? '✗' : '✓'}
        </span>
        {running ? (
          <span
            className="shimmer-running shrink-0 text-xs font-medium tracking-wide"
            style={{ color: 'var(--jcode-color-muted-foreground)' }}
          >
            Running…
          </span>
        ) : group.explorative ? (
          <>
            <span
              className="shrink-0 text-xs font-medium tracking-wide"
              style={{ color: 'var(--jcode-color-muted-foreground)' }}
            >
              Explored
            </span>
            {counts && (
              <span
                className="min-w-0 truncate font-mono text-[0.72rem]"
                style={{ color: 'var(--jcode-color-foreground)', opacity: 0.88 }}
              >
                {counts}
              </span>
            )}
          </>
        ) : (
          <span
            className="min-w-0 truncate text-xs font-medium tracking-wide"
            style={{ color: 'var(--jcode-color-muted-foreground)' }}
          >
            {counts || `${group.tools.length} tool calls`}
          </span>
        )}
        {failed > 0 && (
          <span
            className="shrink-0 text-xs font-medium"
            style={{ color: 'var(--jcode-color-destructive, var(--jcode-color-error-fg))' }}
            data-testid="activity-failed"
          >
            · {failed} failed
          </span>
        )}
        {denied > 0 && (
          <span
            className="shrink-0 text-xs font-medium"
            style={{ color: 'var(--jcode-color-muted-foreground)' }}
            data-testid="activity-denied"
          >
            · {denied} denied
          </span>
        )}
        <ChevronDownIcon
          className={`h-3 w-3 shrink-0 text-[var(--jcode-color-muted-foreground)] transition-transform duration-[var(--jcode-duration-normal)] ${
            expanded ? 'rotate-180' : ''
          }`}
        />
      </button>

      {expanded && (
        <div
          className="jcode-activity__body jcode-toolbatch"
          data-tool-status={group.status}
          data-testid="activity-rows"
        >
          {group.tools.map((t) => (
            <ToolRow key={t.id} tool={t} />
          ))}
        </div>
      )}
    </div>
  )
})
