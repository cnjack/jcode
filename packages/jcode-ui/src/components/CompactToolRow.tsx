/**
 * CompactToolRow — slim one-line tool summary for subagent children.
 * Avoids nesting a full ToolCallCard for every inner step.
 */

import { memo } from 'react'
import type { ToolCall } from 'jcode-ui-core'

export interface CompactToolRowProps {
  tool: ToolCall
}

export const CompactToolRow = memo(function CompactToolRow({ tool }: CompactToolRowProps) {
  const title = tool.displayInfo?.title ?? tool.name
  const subtitle = tool.displayInfo?.subtitle ?? ''
  const statusColor =
    tool.status === 'error'
      ? 'var(--color-error-fg)'
      : tool.status === 'running'
        ? 'var(--color-muted-foreground)'
        : 'var(--color-muted-foreground)'

  return (
    <div
      className="jcode-compact-tool-row flex min-w-0 items-center gap-1.5 py-0.5"
      data-tool-name={tool.name}
      data-tool-status={tool.status}
    >
      <span
        className={`shrink-0 text-[10px] ${tool.status === 'running' ? 'animate-pulse' : ''}`}
        style={{ color: statusColor }}
        aria-hidden
      >
        {tool.status === 'running' ? '●' : tool.status === 'error' ? '✗' : '·'}
      </span>
      <span
        className="shrink-0 text-[11px] font-medium"
        style={{ color: 'var(--color-muted-foreground)' }}
      >
        {title}
      </span>
      {subtitle && (
        <span
          className="min-w-0 truncate font-mono text-[11px]"
          style={{ color: 'var(--color-foreground)', opacity: 0.85 }}
        >
          {subtitle}
        </span>
      )}
      {tool.meta?.exit_code !== undefined && tool.meta.exit_code !== 0 && (
        <span className="ml-auto shrink-0 text-[10px]" style={{ color: 'var(--color-error-fg)' }}>
          exit {tool.meta.exit_code}
        </span>
      )}
    </div>
  )
})
