/**
 * GenericRenderer — fallback renderer for tools without a specific renderer.
 * Shows args + output + error in labeled columns.
 */

import { memo } from 'react'
import type { ToolRendererProps } from 'jcode-ui-core/adapters'
import { truncate } from './terminal.js'

export const GenericRenderer = memo(function GenericRenderer({ args, output, error }: ToolRendererProps) {
  return (
    <div className="jcode-tool-generic jcode-selectable my-1 space-y-1 rounded-[var(--radius-md)] border border-[var(--color-border)] bg-[var(--code-bg)] px-3 py-2 font-mono text-[0.76rem]">
      {args && args !== '{}' && (
        <div>
          <div className="text-[0.68rem] uppercase text-[var(--color-muted-foreground)]">args</div>
          <pre className="whitespace-pre-wrap break-words text-[var(--color-foreground)]">{truncate(prettyArgs(args), 8000)}</pre>
        </div>
      )}
      {output && (
        <div>
          <div className="text-[0.68rem] uppercase text-[var(--color-muted-foreground)]">output</div>
          <pre className="whitespace-pre-wrap break-words text-[var(--color-foreground)]">{truncate(output, 20000)}</pre>
        </div>
      )}
      {error && (
        <div>
          <div className="text-[0.68rem] uppercase text-[var(--color-muted-foreground)]">error</div>
          <pre className="whitespace-pre-wrap break-words text-[var(--color-error-fg)]">{truncate(error, 8000)}</pre>
        </div>
      )}
    </div>
  )
})

function prettyArgs(args: string): string {
  try {
    return JSON.stringify(JSON.parse(args), null, 2)
  } catch {
    return args
  }
}
