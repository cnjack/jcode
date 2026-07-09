/**
 * SkillRenderer — `load_skill` (matches Vue skill block). No outer border / icon.
 */

import { memo, useMemo } from 'react'
import type { ToolRendererProps } from 'jcode-ui-core/adapters'

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
    const descMatch = (output ?? '').match(/description="([^"]*)"/)
    return { name: n, description: descMatch ? (descMatch[1] ?? '') : '' }
  }, [args, output])

  return (
    <div className="jcode-skill px-3 py-2.5" style={{ background: 'var(--color-surface)' }}>
      <div className="flex items-center gap-2">
        <span className="font-mono text-[11px] font-semibold" style={{ color: 'var(--color-foreground)' }}>
          {name}
        </span>
        {status === 'running' && (
          <span className="animate-pulse text-[10px]" style={{ color: 'var(--color-muted-foreground)' }}>
            loading
          </span>
        )}
      </div>
      {description && (
        <div className="mt-1 text-[11px] leading-snug" style={{ color: 'var(--color-muted-foreground)' }}>
          {description}
        </div>
      )}
      {error && (
        <div
          className="mt-1 font-mono text-[11px]"
          style={{ color: 'var(--color-destructive, var(--color-error-fg))' }}
        >
          {error}
        </div>
      )}
    </div>
  )
})
