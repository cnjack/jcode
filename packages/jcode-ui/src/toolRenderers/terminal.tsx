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
    <div className="jcode-terminal max-h-72 overflow-y-auto px-3 py-2.5 font-mono text-xs leading-relaxed">
      {command && (
        <div className="jcode-terminal__cmd">
          <span className="jcode-terminal__prompt select-none">$ </span>
          <span className="jcode-terminal__command">{command}</span>
        </div>
      )}
      {body && <div className="jcode-terminal__out mt-1.5 whitespace-pre-wrap break-all">{truncate(body, 2000)}</div>}
      {error && <div className="jcode-terminal__err mt-1 whitespace-pre-wrap">{error}</div>}
      {status === 'running' && <div className="jcode-terminal__run mt-1 animate-pulse">Running…</div>}
    </div>
  )
})

/** Count code points (not UTF-16 units) so CJK truncation is fair. */
export function truncate(text: string, max: number): string {
  const chars = [...text]
  if (chars.length <= max) return text
  return chars.slice(0, max).join('') + `… (${chars.length} chars)`
}
