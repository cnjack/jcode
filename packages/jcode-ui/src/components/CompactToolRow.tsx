/**
 * CompactToolRow — slim one-line tool summary for subagent children.
 * Avoids nesting a full ToolCallCard for every inner step.
 */

import { memo } from 'react'
import type { ToolCall } from 'jcode-ui-core'
import { getApprovalOutcome } from 'jcode-ui-core'

export interface CompactToolRowProps {
  tool: ToolCall
}

export const CompactToolRow = memo(function CompactToolRow({ tool }: CompactToolRowProps) {
  const title = tool.displayInfo?.title ?? tool.name
  const subtitle = tool.displayInfo?.subtitle ?? ''
  // Denied ≠ error: muted strikethrough. Awaiting approval = warning color.
  const approvalOutcome = tool.approval ? getApprovalOutcome(tool.approval) : undefined
  const isDenied = !!tool.denied || approvalOutcome === 'denied'
  const isAwaiting = !isDenied && !!tool.awaitingApproval && tool.status === 'running'
  const statusColor = isAwaiting
    ? 'var(--jcode-color-warning-fg)'
    : !isDenied && tool.status === 'error'
      ? 'var(--jcode-color-error-fg)'
      : 'var(--jcode-color-muted-foreground)'

  return (
    <div
      data-jcode-ui="" className="jcode-compact-tool-row flex min-w-0 items-center gap-1.5 py-0.5"
      data-tool-name={tool.name}
      data-tool-status={tool.status}
      data-tool-denied={isDenied ? 'true' : undefined}
      data-tool-awaiting-approval={isAwaiting ? 'true' : undefined}
    >
      <span
        className={`shrink-0 text-[10px] ${tool.status === 'running' && !isAwaiting ? 'animate-pulse' : ''}`}
        style={{ color: statusColor }}
        aria-hidden
      >
        {isDenied ? '⊘' : tool.status === 'running' ? '●' : tool.status === 'error' ? '✗' : '·'}
      </span>
      <span
        className={`shrink-0 text-[11px] font-medium ${isDenied ? 'line-through' : ''}`}
        style={{
          color: isAwaiting ? 'var(--jcode-color-warning-fg)' : 'var(--jcode-color-muted-foreground)',
        }}
      >
        {title}
      </span>
      {subtitle && (
        <span
          className={`min-w-0 truncate font-mono text-[11px] ${isDenied ? 'line-through' : ''}`}
          style={{
            color: isDenied ? 'var(--jcode-color-muted-foreground)' : 'var(--jcode-color-foreground)',
            opacity: 0.85,
          }}
        >
          {subtitle}
        </span>
      )}
      {isDenied ? (
        <span className="ml-auto shrink-0 text-[10px]" style={{ color: 'var(--jcode-color-muted-foreground)' }}>
          Denied
        </span>
      ) : (
        tool.meta?.exit_code !== undefined && tool.meta.exit_code !== 0 && (
          <span className="ml-auto shrink-0 text-[10px]" style={{ color: 'var(--jcode-color-error-fg)' }}>
            exit {tool.meta.exit_code}
          </span>
        )
      )}
    </div>
  )
})
