/**
 * SkillRenderer — renders `load_skill` tool calls.
 * Parses the skill name from args and description from output (format:
 * `description="..."`).
 */

import { memo, useMemo } from 'react'
import { SparklesIcon } from '@heroicons/react/24/outline'
import type { ToolRendererProps } from 'jcode-ui-core/adapters'

export const SkillRenderer = memo(function SkillRenderer({ args, output }: ToolRendererProps) {
  const { name, description } = useMemo(() => {
    let n = ''
    try {
      n = JSON.parse(args).name ?? ''
    } catch {
      // ignore
    }
    const descMatch = (output ?? '').match(/description="([^"]*)"/)
    return { name: n, description: descMatch ? descMatch[1] ?? '' : '' }
  }, [args, output])
  return (
    <div className="jcode-skill my-1 flex items-start gap-2 rounded-[var(--radius-md)] border border-[var(--color-border)] bg-[var(--code-bg)] px-3 py-2">
      <SparklesIcon className="mt-0.5 h-4 w-4 shrink-0 text-[var(--color-primary)]" />
      <div className="min-w-0">
        <div className="font-medium text-[var(--color-foreground)]">{name}</div>
        {description && <div className="text-[0.78rem] text-[var(--color-muted-foreground)]">{description}</div>}
      </div>
    </div>
  )
})
