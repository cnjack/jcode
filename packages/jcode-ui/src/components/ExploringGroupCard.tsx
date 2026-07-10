/**
 * ExploringGroupCard — compact summary for coalesced read/search/list tools.
 * Renders "Exploring" / "Explored" with action lines (Read a, b · Search …).
 */

import { memo, useMemo, useState } from 'react'
import { ChevronDownIcon } from '@heroicons/react/24/outline'
import type { ExploringGroup } from 'jcode-ui-core'
import { summarizeExploringSteps } from 'jcode-ui-core'

export interface ExploringGroupCardProps {
  group: ExploringGroup
  className?: string
}

export const ExploringGroupCard = memo(function ExploringGroupCard({
  group,
  className,
}: ExploringGroupCardProps) {
  const [expanded, setExpanded] = useState(false)
  const steps = useMemo(() => summarizeExploringSteps(group.tools), [group.tools])
  const running = group.status === 'running'
  const errored = group.status === 'error'
  const label = running ? 'Exploring' : 'Explored'
  const count = group.tools.length

  return (
    <div
      className={`jcode-toolcall jcode-exploring my-1 ${className ?? ''}`}
      data-tool-name="exploring"
      data-tool-status={group.status}
      data-expanded={expanded ? 'true' : 'false'}
    >
      <button
        type="button"
        onClick={() => setExpanded((e) => !e)}
        className="flex w-full max-w-full cursor-pointer items-center gap-1.5 bg-transparent text-left"
      >
        <span
          className={`shrink-0 text-xs font-medium tracking-wide ${running ? 'shimmer-running' : ''}`}
          style={{
            color: errored
              ? 'var(--color-destructive, var(--color-error-fg))'
              : 'var(--color-muted-foreground)',
          }}
        >
          {label}
        </span>
        <span
          className="min-w-0 truncate font-mono text-[0.72rem]"
          style={{ color: 'var(--color-foreground)', opacity: 0.88 }}
        >
          {count} step{count === 1 ? '' : 's'}
        </span>
        <ChevronDownIcon
          className={`h-3 w-3 shrink-0 text-[var(--color-muted-foreground)] transition-transform duration-[var(--duration-normal)] ${
            expanded ? 'rotate-180' : ''
          }`}
        />
      </button>

      <div className="jcode-exploring__steps" role="list">
        {steps.map((step, i) => (
          <div key={i} className="jcode-exploring__step" role="listitem">
            <span className="jcode-exploring__action">{step.action}</span>
            {step.detail ? (
              <span className="jcode-exploring__detail">{step.detail}</span>
            ) : null}
          </div>
        ))}
      </div>

      {expanded && (
        <div className="toolcall-body jcode-selectable" data-selectable data-tool-status={group.status}>
          <ul className="jcode-exploring__detail-list m-0 list-none p-2">
            {group.tools.map((t) => (
              <li
                key={t.id}
                className="flex items-center gap-2 border-b border-[var(--color-border)] px-1 py-1.5 last:border-0"
              >
                <span
                  className="shrink-0 text-[10px] font-medium uppercase tracking-wide"
                  style={{ color: 'var(--color-muted-foreground)' }}
                >
                  {t.displayInfo?.title || t.name}
                </span>
                <span
                  className="min-w-0 truncate font-mono text-[11px]"
                  style={{ color: 'var(--color-foreground)' }}
                >
                  {t.displayInfo?.subtitle || ''}
                </span>
                <span
                  className="ml-auto shrink-0 text-[10px] tabular-nums"
                  style={{
                    color:
                      t.status === 'error'
                        ? 'var(--color-error-fg)'
                        : 'var(--color-muted-foreground)',
                  }}
                >
                  {t.status}
                </span>
              </li>
            ))}
          </ul>
        </div>
      )}
    </div>
  )
})
