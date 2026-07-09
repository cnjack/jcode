/**
 * FileViewerRenderer — `read` / `write` (matches Vue file-viewer block).
 * read: parse `   N│content` lines. write: raw content from args with generated line numbers.
 * No outer border — lives inside `.toolcall-body`.
 */

import { memo, useMemo } from 'react'
import type { ToolRendererProps } from 'jcode-ui-core/adapters'

interface FileLine {
  num: number
  text: string
}

export const FileViewerRenderer = memo(function FileViewerRenderer({
  name,
  args,
  output,
  error,
  status,
}: ToolRendererProps) {
  const lines = useMemo(() => buildLines(name, args, output), [name, args, output])

  if (lines.length > 0) {
    return (
      <div className="jcode-file-viewer max-h-72 overflow-y-auto" style={{ background: 'var(--color-surface)' }}>
        <table className="jcode-file-table w-full border-collapse">
          <tbody>
            {lines.map((l) => (
              <tr key={l.num}>
                <td className="jcode-file-lineno">{l.num}</td>
                <td style={{ color: 'var(--color-foreground)' }}>{l.text}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    )
  }

  if (error) {
    return (
      <div
        className="px-3 py-2 font-mono text-xs whitespace-pre-wrap"
        style={{ color: 'var(--color-destructive, var(--color-error-fg))' }}
      >
        {error}
      </div>
    )
  }
  if (status === 'running') {
    return (
      <div className="animate-pulse px-3 py-3 text-xs" style={{ color: 'var(--color-muted-foreground)' }}>
        Loading…
      </div>
    )
  }
  return (
    <div className="px-3 py-2 text-xs italic" style={{ color: 'var(--color-muted-foreground)' }}>
      No content
    </div>
  )
})

function buildLines(name: string, args: string, output?: string): FileLine[] {
  // write: content lives in args
  if (name === 'write') {
    try {
      const parsed = JSON.parse(args)
      const content: string = parsed.content || ''
      if (!content) return []
      return content.split('\n').map((text, i) => ({ num: i + 1, text }))
    } catch {
      return []
    }
  }
  // read: output is `   N│content` (or raw)
  if (!output) return []
  const raw = output.split('\n')
  const numbered = raw
    .map((line) => {
      const m = line.match(/^\s*(\d+)│(.*)$/)
      if (m) return { num: parseInt(m[1] ?? '0', 10), text: m[2] ?? '' }
      return null
    })
    .filter((x): x is FileLine => x !== null)
  // Drop trailing empty line artifact
  if (numbered.length) {
    return numbered.filter((_, i, arr) => !(i === arr.length - 1 && _.text === ''))
  }
  // Fallback: raw content with generated numbers
  return raw
    .filter((line, i, arr) => !(i === arr.length - 1 && line === ''))
    .map((text, i) => ({ num: i + 1, text }))
}
