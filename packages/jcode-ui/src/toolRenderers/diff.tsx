/**
 * DiffRenderer — `edit` / `multi_edit` (matches Vue diff block).
 * Sections of del/add lines with line numbers + sign column. No outer border.
 */

import { memo, useMemo } from 'react'
import type { ToolRendererProps } from 'jcode-ui-core/adapters'
import { truncate } from './terminal.js'

interface Section {
  type: 'add' | 'del'
  lines: string[]
}

export const DiffRenderer = memo(function DiffRenderer({
  name,
  args,
  output,
  error,
  status,
}: ToolRendererProps) {
  const { sections } = useMemo(() => buildDiff(name, args), [name, args])

  if (sections.length > 0) {
    return (
      <div className="jcode-diff max-h-72 overflow-y-auto">
        <table className="jcode-diff-table w-full border-collapse">
          <tbody>
            {sections.map((section, si) =>
              section.lines.map((line, li) => (
                <tr
                  key={`${si}-${li}`}
                  className={section.type === 'del' ? 'jcode-diff-line-del' : 'jcode-diff-line-add'}
                >
                  <td className="jcode-diff-ln">{li + 1}</td>
                  <td className="jcode-diff-sign">{section.type === 'del' ? '−' : '+'}</td>
                  <td>{line}</td>
                </tr>
              )),
            )}
          </tbody>
        </table>
        {output && !error && (
          <div
            className="px-3 py-1 font-mono text-[10px]"
            style={{ color: 'var(--color-muted-foreground)' }}
          >
            {truncate(output, 200)}
          </div>
        )}
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
        Applying…
      </div>
    )
  }
  return (
    <div className="px-3 py-2 text-xs italic" style={{ color: 'var(--color-muted-foreground)' }}>
      No changes
    </div>
  )
})

function buildDiff(name: string, args: string): { sections: Section[] } {
  try {
    const parsed = JSON.parse(args)
    if (name === 'multi_edit' && Array.isArray(parsed.edits)) {
      const sections: Section[] = []
      for (const edit of parsed.edits as Array<{ old_string?: string; new_string?: string }>) {
        if (edit.old_string) sections.push({ type: 'del', lines: edit.old_string.split('\n') })
        if (edit.new_string) sections.push({ type: 'add', lines: edit.new_string.split('\n') })
      }
      return { sections }
    }
    const oldStr: string = parsed.old_string || ''
    const newStr: string = parsed.new_string || ''
    const isCreate = !oldStr && !!newStr
    const sections: Section[] = []
    if (oldStr && !isCreate) sections.push({ type: 'del', lines: oldStr.split('\n') })
    if (newStr) sections.push({ type: 'add', lines: newStr.split('\n') })
    return { sections }
  } catch {
    return { sections: [] }
  }
}
