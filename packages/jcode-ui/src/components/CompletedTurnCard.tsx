/**
 * CompletedTurnCard — elapsed-time disclosure for one settled user turn.
 *
 * Intermediate commentary, approvals and tool activity collapse behind a slim
 * duration row. The final assistant message remains visible at all times.
 */

import { Fragment, memo, useId, useState, type ReactNode } from 'react'
import { ChevronRightIcon } from '@heroicons/react/24/outline'
import type { CompletedTurn, ThreadItem } from 'jcode-ui-core'
import { Message } from './Message.js'

export interface CompletedTurnCardProps {
  turn: CompletedTurn
  renderActivity: (item: ThreadItem) => ReactNode
  durationLabel?: (durationMs: number) => string
  expandLabel?: string
  collapseLabel?: string
}

export const CompletedTurnCard = memo(function CompletedTurnCard({
  turn,
  renderActivity,
  durationLabel = defaultDurationLabel,
  expandLabel = 'Show work',
  collapseLabel = 'Hide work',
}: CompletedTurnCardProps) {
  const [expanded, setExpanded] = useState(false)
  const regionId = useId()
  const hasCollapsibleActivity = turn.activity.some((entry) => !entry.alwaysVisible)
  const visibleReceipts = turn.activity.filter((entry) => entry.alwaysVisible)
  const label = durationLabel(turn.durationMs)

  return (
    <section
      data-jcode-ui=""
      className="jcode-completed-turn"
      data-expanded={expanded ? 'true' : 'false'}
      data-testid="completed-turn"
    >
      <div className="jcode-chat-col">
        {hasCollapsibleActivity ? (
          <button
            type="button"
            className="jcode-turn-disclosure jcode-gutter"
            aria-expanded={expanded}
            aria-controls={regionId}
            aria-label={`${label} · ${expanded ? collapseLabel : expandLabel}`}
            onClick={() => setExpanded((value) => !value)}
          >
            <span className="jcode-turn-disclosure__label">{label}</span>
            <ChevronRightIcon
              className="jcode-turn-disclosure__chevron h-3.5 w-3.5 shrink-0"
              aria-hidden
            />
            <span className="jcode-turn-disclosure__rule" aria-hidden />
          </button>
        ) : (
          <div className="jcode-turn-disclosure jcode-gutter" aria-label={label}>
            <span className="jcode-turn-disclosure__label">{label}</span>
            <span className="jcode-turn-disclosure__rule" aria-hidden />
          </div>
        )}
      </div>

      {hasCollapsibleActivity && expanded ? (
        <div id={regionId} className="jcode-turn-activity" data-testid="turn-activity">
          {turn.activity.map((entry) => (
            <Fragment key={`${entry.item.kind}-${entry.item.seq}`}>
              {renderActivity(entry.item)}
            </Fragment>
          ))}
        </div>
      ) : (
        visibleReceipts.map((entry) => (
          <Fragment key={`${entry.item.kind}-${entry.item.seq}`}>
            {renderActivity(entry.item)}
          </Fragment>
        ))
      )}

      <Message message={turn.summary} showDuration={false} />
    </section>
  )
})

function defaultDurationLabel(durationMs: number): string {
  const totalSeconds = Math.max(0, Math.round(durationMs / 1000))
  const hours = Math.floor(totalSeconds / 3600)
  const minutes = Math.floor((totalSeconds % 3600) / 60)
  const seconds = totalSeconds % 60
  const parts: string[] = []
  if (hours > 0) parts.push(`${hours}h`)
  if (minutes > 0 || hours > 0) parts.push(`${minutes}m`)
  parts.push(`${seconds}s`)
  return `Worked for ${parts.join(' ')}`
}
