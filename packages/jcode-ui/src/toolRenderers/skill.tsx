/**
 * SkillRenderer — `load_skill` (matches Vue skill block). No outer border / icon.
 */

import { memo, useMemo } from 'react'
import type { ToolRendererProps } from 'jcode-ui-core/adapters'

function skillDescription(output?: string): string {
  const match = output?.match(/\bdescription=("(?:\\.|[^"\\])*")/)
  if (!match) return ''
  try {
    const value: unknown = JSON.parse(match[1])
    return typeof value === 'string' ? value : ''
  } catch {
    return match[1].slice(1, -1)
  }
}

export const SkillRenderer = memo(function SkillRenderer({
  args,
  output,
  error,
  status,
}: ToolRendererProps) {
  const { name, description } = useMemo(() => {
    let n = ''
    try {
      n = JSON.parse(args).name ?? ''
    } catch {
      // ignore
    }
    return { name: n, description: skillDescription(output) }
  }, [args, output])

  return (
    <div className="jcode-skill px-3 py-2.5" style={{ background: 'var(--jcode-color-surface)' }}>
      <div className="flex items-center gap-2">
        <span className="font-mono text-[11px] font-semibold" style={{ color: 'var(--jcode-color-foreground)' }}>
          {name}
        </span>
        {status === 'running' && (
          <span className="animate-pulse text-[10px]" style={{ color: 'var(--jcode-color-muted-foreground)' }}>
            loading
          </span>
        )}
      </div>
      {description && (
        <div className="mt-1 line-clamp-2 text-[11px] leading-snug" title={description} style={{ color: 'var(--jcode-color-muted-foreground)' }}>
          {description}
        </div>
      )}
      {error && (
        <div
          className="mt-1 font-mono text-[11px]"
          style={{ color: 'var(--jcode-color-destructive, var(--jcode-color-error-fg))' }}
        >
          {error}
        </div>
      )}
    </div>
  )
})
