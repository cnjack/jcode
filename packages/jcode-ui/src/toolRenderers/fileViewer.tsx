/**
 * FileViewerRenderer — renders `read`/`write` tool calls.
 * Parses output lines of the form `   N│content` (jcode's read format) into a
 * line-numbered table. Falls back to a plain <pre> if the format doesn't match.
 */

import { memo, useMemo } from 'react'
import type { ToolRendererProps } from 'jcode-ui-core/adapters'
import { truncate } from './terminal.js'

interface FileLine {
  no: string
  content: string
}

export const FileViewerRenderer = memo(function FileViewerRenderer({ args, output }: ToolRendererProps) {
  let path = ''
  try {
    const parsed = JSON.parse(args)
    path = parsed.path ?? parsed.file_path ?? ''
  } catch {
    // ignore
  }
  const lines = useMemo(() => parseLines(output), [output])
  return (
    <div className="jcode-file-viewer jcode-selectable my-1 overflow-hidden rounded-[var(--radius-md)] border border-[var(--color-border)] bg-[var(--code-bg)]">
      {path && (
        <div className="truncate border-b border-[var(--color-border)] px-3 py-1 font-mono text-[0.72rem] text-[var(--color-muted-foreground)]">
          {path}
        </div>
      )}
      {lines ? (
        <table className="jcode-file-table max-h-[500px] overflow-auto">
          <tbody>
            {lines.map((l, i) => (
              <tr key={i}>
                <td className="jcode-file-lineno">{l.no}</td>
                <td className="text-[var(--color-foreground)]">{l.content}</td>
              </tr>
            ))}
          </tbody>
        </table>
      ) : (
        <pre className="max-h-[500px] overflow-auto px-3 py-2 font-mono text-[0.76rem] text-[var(--color-foreground)]">
          {truncate(output ?? '', 30000)}
        </pre>
      )}
    </div>
  )
})

/** Parse jcode's `   N│content` read format. Returns null if not matching. */
function parseLines(output?: string): FileLine[] | null {
  if (!output) return null
  const raw = output.split('\n')
  const out: FileLine[] = []
  let matched = 0
  for (const line of raw) {
    const m = line.match(/^\s*(\d+)│(.*)$/)
    if (m) {
      matched++
      out.push({ no: m[1] ?? '', content: m[2] ?? '' })
    } else {
      out.push({ no: '', content: line })
    }
  }
  // Only treat as line-numbered if a meaningful fraction matched the format.
  return matched >= Math.max(1, Math.floor(out.length / 3)) ? out : null
}
