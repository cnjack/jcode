/**
 * GenericRenderer — fallback (matches Vue left-border block).
 * No full card chrome — a left accent border inside the shared toolcall-body.
 */

import { memo } from 'react'
import type { ToolRendererProps } from 'jcode-ui-core/adapters'
import { truncate } from './terminal.js'

export const GenericRenderer = memo(function GenericRenderer({
  args,
  output,
  error,
  status,
}: ToolRendererProps) {
  const borderColor =
    status === 'error' || error
      ? 'var(--color-destructive, var(--color-error-fg))'
      : 'var(--color-border)'

  return (
    <div
      className="jcode-tool-generic max-h-64 overflow-y-auto border-l-2 py-2 pl-3 ml-3 font-mono text-xs"
      style={{ borderColor }}
    >
      {args && args !== '{}' && (
        <div className="mb-1.5">
          <span
            className="text-[10px] uppercase tracking-wider"
            style={{ color: 'var(--color-muted-foreground)' }}
          >
            args
          </span>
          <div className="mt-0.5" style={{ color: 'var(--color-muted-foreground)' }}>
            {formatArgs(args)}
          </div>
        </div>
      )}
      {output && (
        <div className="mt-2">
          <span
            className="text-[10px] uppercase tracking-wider"
            style={{ color: 'var(--color-muted-foreground)' }}
          >
            output
          </span>
          <div className="mt-0.5 whitespace-pre-wrap" style={{ color: 'var(--color-muted-foreground)' }}>
            {truncate(output, 500)}
          </div>
        </div>
      )}
      {error && (
        <div className="mt-2">
          <span
            className="text-[10px] uppercase tracking-wider"
            style={{ color: 'var(--color-destructive, var(--color-error-fg))' }}
          >
            error
          </span>
          <div
            className="mt-0.5 whitespace-pre-wrap"
            style={{ color: 'var(--color-destructive, var(--color-error-fg))' }}
          >
            {truncate(error, 500)}
          </div>
        </div>
      )}
    </div>
  )
})

function formatArgs(args: string): string {
  try {
    const parsed = JSON.parse(args)
    return Object.entries(parsed)
      .map(
        ([k, v]) =>
          `${k}: ${typeof v === 'string' ? v.slice(0, 80) : JSON.stringify(v).slice(0, 80)}`,
      )
      .join(', ')
  } catch {
    return args.slice(0, 120)
  }
}
