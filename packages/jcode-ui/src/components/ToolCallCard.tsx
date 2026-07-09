/**
 * ToolCallCard — styled tool-invocation card (wraps headless ToolCallView).
 *
 * Renders the expand/collapse header (status glyph, title/subtitle, diff counter,
 * chevron) and dispatches the body to a registered ToolRenderer. Sets up the
 * ToolCallProvider so the headless primitive can find the registry + route
 * ask_user tools to the styled AskUserCard.
 *
 * Subagent tools get a borderless recursive treatment; the shimmer-running class
 * animates the header while a tool is in flight.
 */

import { memo, useMemo } from 'react'
import { ChevronDownIcon } from '@heroicons/react/24/outline'
import type { ToolCall } from 'jcode-ui-core'
import { ToolCallView, ToolCallProvider } from 'jcode-ui-core/primitives'
import type { ToolRendererRegistry } from 'jcode-ui-core/adapters'
import { AskUserCard } from './AskUserCard.js'
import { useToolRegistry } from './ToolRegistryContext.js'

export interface ToolCallCardProps {
  tool: ToolCall
  /** Override the registry (defaults to the context-provided one). */
  registry?: ToolRendererRegistry
}

export const ToolCallCard = memo(function ToolCallCard({ tool, registry }: ToolCallCardProps) {
  const ctxRegistry = useToolRegistry()
  const reg = registry ?? ctxRegistry
  const providerValue = useMemo(
    () => ({ registry: reg, renderAskUser: (t: ToolCall) => <AskUserCard tool={t} /> }),
    [reg],
  )
  return (
    <ToolCallProvider value={providerValue}>
      <ToolCallView
        tool={tool}
        className="jcode-toolcall px-4 py-1"
        renderHeader={(t, expanded, toggle) => <Header tool={t} expanded={expanded} onToggle={toggle} />}
      />
    </ToolCallProvider>
  )
})

function Header({ tool, expanded, onToggle }: { tool: ToolCall; expanded: boolean; onToggle: () => void }) {
  const isSubagent = tool.name === 'subagent' || tool.name === 'team_spawn'
  const isRunning = tool.status === 'running'
  const isError = tool.status === 'error'

  const title = tool.displayInfo?.title ?? tool.name
  const subtitle = tool.displayInfo?.subtitle ?? ''
  const glyph = isRunning ? '◈' : isError ? '✗' : '✓'
  const glyphColor = isError
    ? 'text-[var(--color-error-fg)]'
    : isRunning
      ? 'text-[var(--color-primary)]'
      : 'text-[var(--color-muted-foreground)]'

  // Diff line counter for edit/multi_edit.
  const diffCount = useMemo(() => {
    if (tool.name !== 'edit' && tool.name !== 'multi_edit') return null
    return parseDiffCount(tool)
  }, [tool])

  return (
    <button
      type="button"
      onClick={onToggle}
      className={`group flex w-full items-center gap-2 rounded-[var(--radius-md)] px-2 py-1 text-left text-[0.82rem] transition-colors hover:bg-[var(--neutral-wash-soft)] ${
        isRunning ? 'shimmer-running' : ''
      } ${isSubagent ? 'font-medium' : ''}`}
    >
      <span className={`shrink-0 text-xs ${glyphColor}`} aria-hidden>
        {glyph}
      </span>
      <span className="truncate text-[var(--color-foreground)]">{title}</span>
      {subtitle && (
        <span className="truncate text-[var(--color-muted-foreground)]" dangerouslySetInnerHTML={{ __html: subtitle }} />
      )}
      {diffCount && (
        <span className="ml-1 shrink-0 rounded-[var(--radius-xs)] bg-[var(--color-muted)] px-1 text-[0.65rem] text-[var(--color-muted-foreground)]">
          {diffCount}
        </span>
      )}
      <ChevronDownIcon
        className={`ml-auto h-3.5 w-3.5 shrink-0 text-[var(--color-muted-foreground)] transition-transform ${expanded ? 'rotate-180' : ''}`}
      />
    </button>
  )
}

/** Compute "+N/-M" from an edit/multi_edit tool's args. */
function parseDiffCount(tool: ToolCall): string | null {
  try {
    const parsed = JSON.parse(tool.args)
    const edits = Array.isArray(parsed) ? parsed : [parsed]
    let add = 0
    let del = 0
    for (const e of edits) {
      if (typeof e.old_string === 'string') del += e.old_string.split('\n').length
      if (typeof e.new_string === 'string') add += e.new_string.split('\n').length
    }
    return `+${add}/-${del}`
  } catch {
    return null
  }
}
