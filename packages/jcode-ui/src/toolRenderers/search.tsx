/**
 * SearchRenderer — renders `grep` tool calls.
 * Parses `file:line:content` matches from the output.
 */

import { memo, useMemo } from 'react'
import type { ToolRendererProps } from 'jcode-ui-core/adapters'

interface GrepHit {
  file: string
  line?: string
  content: string
}

export const SearchRenderer = memo(function SearchRenderer({ output }: ToolRendererProps) {
  const hits = useMemo(() => parseGrep(output), [output])
  if (hits.length === 0) {
    return <div className="px-3 py-2 text-[var(--color-muted-foreground)]">No matches</div>
  }
  return (
    <div className="jcode-search jcode-selectable my-1 max-h-[500px] overflow-auto rounded-[var(--radius-md)] border border-[var(--color-border)] bg-[var(--code-bg)] px-3 py-2 font-mono text-[0.76rem]">
      <div className="mb-1 text-[0.7rem] text-[var(--color-muted-foreground)]">{hits.length} matches</div>
      {hits.map((h, i) => (
        <div key={i} className="py-0.5">
          <span className="text-[var(--color-primary)]">{h.file}</span>
          {h.line && <span className="text-[var(--color-muted-foreground)]">:{h.line}</span>}
          <span className="text-[var(--color-muted-foreground)]">:</span>
          <span className="text-[var(--color-foreground)]">{h.content}</span>
        </div>
      ))}
    </div>
  )
})

function parseGrep(output?: string): GrepHit[] {
  if (!output) return []
  const hits: GrepHit[] = []
  for (const line of output.split('\n')) {
    if (!line.trim()) continue
    // ripgrep default: file:line:content
    const m = line.match(/^([^:]+):(\d+):(.*)$/)
    if (m) {
      hits.push({ file: m[1] ?? '', line: m[2], content: m[3] ?? '' })
      continue
    }
    // file:content (no line number)
    const m2 = line.match(/^([^:]+):(.*)$/)
    if (m2) {
      hits.push({ file: m2[1] ?? '', content: m2[2] ?? '' })
      continue
    }
    hits.push({ file: '', content: line })
  }
  return hits
}
