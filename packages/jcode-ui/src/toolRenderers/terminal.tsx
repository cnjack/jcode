/**
 * TerminalRenderer — renders `execute` tool calls.
 * Shows `$ <command>` then stdout/stderr, with error tinting on failure.
 */

import { memo } from 'react'
import type { ToolRendererProps } from 'jcode-ui-core/adapters'

export const TerminalRenderer = memo(function TerminalRenderer({ args, output, error, status }: ToolRendererProps) {
  let command = ''
  try {
    const parsed = JSON.parse(args)
    command = parsed.command ?? ''
  } catch {
    // ignore
  }
  const isError = status === 'error' || !!error
  return (
    <div className="jcode-terminal jcode-selectable my-1 overflow-hidden rounded-[var(--radius-md)] border border-[var(--color-border)] bg-[var(--code-bg)]">
      {command && (
        <div className="border-b border-[var(--color-border)] px-3 py-1.5 font-mono text-[0.78rem]">
          <span className="text-[var(--color-primary)]">$ </span>
          <span className="text-[var(--color-foreground)]">{command}</span>
        </div>
      )}
      {(output || error) && (
        <pre className="max-h-[400px] overflow-auto px-3 py-2 font-mono text-[0.76rem] leading-relaxed">
          {error && <span className="text-[var(--color-error-fg)]">{error}</span>}
          {error && output && '\n'}
          {output && <span className={isError ? 'text-[var(--color-error-fg)]' : 'text-[var(--color-foreground)]'}>{truncate(output, 20000)}</span>}
        </pre>
      )}
    </div>
  )
})

/** Count code points (not UTF-16 units) so CJK truncation is fair. */
export function truncate(text: string, max: number): string {
  const chars = [...text]
  if (chars.length <= max) return text
  return chars.slice(0, max).join('') + `\n… (${chars.length - max} more chars truncated)`
}
