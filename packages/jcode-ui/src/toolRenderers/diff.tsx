/**
 * DiffRenderer — renders `edit`/`multi_edit` tool calls.
 * Builds a red/green diff table from old_string → new_string in the args.
 * Handles both single-edit and multi_edit (array) shapes.
 */

import { memo, useMemo } from 'react'
import type { ToolRendererProps } from 'jcode-ui-core/adapters'

interface EditSpec {
  old_string?: string
  new_string?: string
  path?: string
  file_path?: string
}

interface DiffRow {
  kind: 'add' | 'del' | 'ctx' | 'meta'
  text: string
}

export const DiffRenderer = memo(function DiffRenderer({ args }: ToolRendererProps) {
  const { path, rows } = useMemo(() => buildDiff(args), [args])
  if (rows.length === 0) {
    return <div className="px-3 py-2 text-[var(--color-muted-foreground)]">No changes</div>
  }
  return (
    <div className="jcode-diff jcode-selectable my-1 overflow-hidden rounded-[var(--radius-md)] border border-[var(--color-border)] bg-[var(--code-bg)]">
      {path && (
        <div className="truncate border-b border-[var(--color-border)] px-3 py-1 font-mono text-[0.72rem] text-[var(--color-muted-foreground)]">
          {path}
        </div>
      )}
      <table className="jcode-diff-table">
        <tbody>
          {rows.map((r, i) => (
            <tr key={i} className={rowClass(r.kind)}>
              <td className="w-4 select-none text-center">
                {r.kind === 'add' ? '+' : r.kind === 'del' ? '-' : ''}
              </td>
              <td>{r.text}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
})

function rowClass(kind: DiffRow['kind']): string {
  switch (kind) {
    case 'add':
      return 'jcode-diff-line-add'
    case 'del':
      return 'jcode-diff-line-del'
    case 'meta':
      return 'jcode-diff-line-meta'
    default:
      return ''
  }
}

function buildDiff(args: string): { path: string; rows: DiffRow[] } {
  let specs: EditSpec[]
  try {
    const parsed = JSON.parse(args)
    specs = Array.isArray(parsed) ? parsed : [parsed]
  } catch {
    return { path: '', rows: [] }
  }
  const path = specs[0]?.path ?? specs[0]?.file_path ?? ''
  const rows: DiffRow[] = []
  for (const s of specs) {
    const oldLines = (s.old_string ?? '').split('\n')
    const newLines = (s.new_string ?? '').split('\n')
    rows.push({ kind: 'del', text: '── old ──' })
    for (const l of oldLines) rows.push({ kind: 'del', text: l })
    rows.push({ kind: 'add', text: '── new ──' })
    for (const l of newLines) rows.push({ kind: 'add', text: l })
  }
  return { path, rows }
}
