/**
 * TerminalRenderer — `execute` tool (matches Vue ToolCallCard terminal block).
 * Renders inside `.toolcall-body` — no outer border of its own.
 * Background is muted (not code-bg); `$ ` prefix is muted-foreground.
 */

import { memo } from 'react'
import type { ToolRendererProps } from 'jcode-ui-core/adapters'

export const TerminalRenderer = memo(function TerminalRenderer({
  args,
  output,
  displayOutput,
  error,
  status,
}: ToolRendererProps) {
  let command = ''
  try {
    const parsed = JSON.parse(args)
    command = parsed.command ?? ''
  } catch {
    // ignore
  }
  const body = displayOutput || output || ''
  return (
    <div
      className="jcode-terminal max-h-72 overflow-y-auto px-3 py-2 font-mono text-xs"
      style={{ background: 'var(--color-muted)' }}
    >
      {command && (
        <div>
          <span className="select-none" style={{ color: 'var(--color-muted-foreground)' }}>
            ${' '}
          </span>
          <span style={{ color: 'var(--color-foreground)' }}>{command}</span>
        </div>
      )}
      {body && (
        <div
          className="mt-1 whitespace-pre-wrap break-all"
          style={{ color: 'var(--color-muted-foreground)' }}
        >
          {truncate(body, 2000)}
        </div>
      )}
      {error && (
        <div className="mt-1 whitespace-pre-wrap" style={{ color: 'var(--color-error-fg)' }}>
          {error}
        </div>
      )}
      {status === 'running' && (
        <div className="mt-1 animate-pulse" style={{ color: 'var(--color-muted-foreground)' }}>
          Running…
        </div>
      )}
    </div>
  )
})

/** Count code points (not UTF-16 units) so CJK truncation is fair. */
export function truncate(text: string, max: number): string {
  const chars = [...text]
  if (chars.length <= max) return text
  return chars.slice(0, max).join('') + `… (${chars.length} chars)`
}
