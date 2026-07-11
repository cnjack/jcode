/**
 * SearchRenderer — `grep` (matches Vue search block).
 * Shows args summary + file:line hits + match count. No outer border.
 */

import { memo, useMemo } from 'react'
import type { ToolRendererProps } from 'jcode-ui-core/adapters'

interface GrepHit {
  file: string
  lineNum: number
  content: string
  isRef: boolean
}

export const SearchRenderer = memo(function SearchRenderer({
  args,
  output,
  error,
  status,
}: ToolRendererProps) {
  const argsDisplay = useMemo(() => formatArgs(args), [args])
  const { lines, count } = useMemo(() => parseResults(output), [output])

  return (
    <div
      className="jcode-search max-h-72 overflow-y-auto px-3 py-2"
      style={{ background: 'var(--jcode-color-surface)' }}
    >
      {argsDisplay && (
        <div
          className="mb-2 whitespace-pre-wrap font-mono text-[11px]"
          style={{ color: 'var(--jcode-color-muted-foreground)' }}
        >
          {argsDisplay}
        </div>
      )}
      {lines.length > 0 ? (
        lines.map((line, i) => (
          <div key={i} className="py-0.5">
            {line.isRef && (
              <div className="flex items-baseline gap-1.5 font-mono text-[10px]">
                <span style={{ color: 'var(--jcode-color-accent-neutral)' }}>{line.file}</span>
                <span style={{ color: 'var(--jcode-color-muted-foreground)' }}>:{line.lineNum}</span>
              </div>
            )}
            <div
              className="whitespace-pre-wrap font-mono text-xs"
              style={{ color: 'var(--jcode-color-foreground)' }}
            >
              {line.content}
            </div>
          </div>
        ))
      ) : status === 'running' ? (
        <div className="animate-pulse py-1 text-xs" style={{ color: 'var(--jcode-color-muted-foreground)' }}>
          Searching…
        </div>
      ) : error ? (
        <div
          className="whitespace-pre-wrap py-1 font-mono text-xs"
          style={{ color: 'var(--jcode-color-destructive, var(--jcode-color-error-fg))' }}
        >
          {error}
        </div>
      ) : (
        <div className="py-1 text-xs italic" style={{ color: 'var(--jcode-color-muted-foreground)' }}>
          No results
        </div>
      )}
      {count !== null && (
        <div className="mt-1.5 font-mono text-[10px]" style={{ color: 'var(--jcode-color-muted-foreground)' }}>
          {count} matches found
        </div>
      )}
    </div>
  )
})

function formatArgs(args: string): string {
  try {
    const parsed = JSON.parse(args)
    return Object.entries(parsed)
      .filter(([, v]) => v !== undefined && v !== null && v !== '')
      .map(([k, v]) => `${k}: ${typeof v === 'string' ? v : JSON.stringify(v)}`)
      .join('\n')
  } catch {
    return args
  }
}

function parseResults(output?: string): { lines: GrepHit[]; count: number | null } {
  if (!output) return { lines: [], count: null }
  const countMatch = output.match(/\((\d+) (?:matches found|results?)\)/)
  const count = countMatch ? parseInt(countMatch[1] ?? '', 10) : null
  const lines = output
    .split('\n')
    .filter((l) => {
      const t = l.trim()
      return t && !t.startsWith('(')
    })
    .map((line) => {
      const m = line.match(/^([^:]+):(\d+):(.*)$/)
      if (m) {
        return {
          file: m[1] ?? '',
          lineNum: parseInt(m[2] ?? '0', 10),
          content: m[3] ?? '',
          isRef: true,
        }
      }
      return { file: '', lineNum: 0, content: line, isRef: false }
    })
  return { lines, count }
}
