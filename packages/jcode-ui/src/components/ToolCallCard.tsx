/**
 * ToolCallCard — styled tool-invocation shell (matches web ToolCallCard.vue).
 *
 * Design:
 *   - Header is a borderless plain-text row (title + subtitle + chevron).
 *     No permanent status glyph; running titles use `.shimmer-running`.
 *   - Content box is ONE shared frame (`.toolcall-body` in components.css);
 *     individual renderers must not add their own outer border.
 *   - Subagent gets a dedicated header + unboxed children list.
 */

import { memo, useMemo } from 'react'
import { ChevronDownIcon } from '@heroicons/react/24/outline'
import type { ToolCall } from 'jcode-ui-core'
import { ToolCallView, ToolCallProvider } from 'jcode-ui-core/primitives'
import type { ToolRendererRegistry } from 'jcode-ui-core/adapters'
import { AskUserCard } from './AskUserCard.js'
import { useToolRegistry } from './ToolRegistryContext.js'
import { renderMarkdown } from '../lib/markdown.js'

export interface ToolCallCardProps {
  tool: ToolCall
  /** Override the registry (defaults to the context-provided one). */
  registry?: ToolRendererRegistry
  /** Extra classes (e.g. pl-9 to indent under the message content column). */
  className?: string
  /** Nesting depth for subagent children. */
  depth?: number
}

export const ToolCallCard = memo(function ToolCallCard({
  tool,
  registry,
  className,
  depth = 0,
}: ToolCallCardProps) {
  const ctxRegistry = useToolRegistry()
  const reg = registry ?? ctxRegistry
  const providerValue = useMemo(
    () => ({
      registry: reg,
      renderAskUser: (t: ToolCall) => <AskUserCard tool={t} />,
      // Nested subagent children re-enter the styled card (same header chrome).
      renderChild: (child: ToolCall, childDepth: number) => (
        <ToolCallCard tool={child} registry={reg} depth={childDepth} />
      ),
    }),
    [reg],
  )
  return (
    <ToolCallProvider value={providerValue}>
      <ToolCallView
        tool={tool}
        depth={depth}
        className={`jcode-toolcall my-1 ${className ?? ''}`}
        renderHeader={(t, expanded, toggle) =>
          t.name === 'subagent' ? (
            <SubagentHeader tool={t} expanded={expanded} onToggle={toggle} />
          ) : (
            <ToolHeader tool={t} expanded={expanded} onToggle={toggle} />
          )
        }
        renderSubagentOutput={(t) => <SubagentOutput tool={t} />}
      />
    </ToolCallProvider>
  )
})

/**
 * Subagent body: output only (never args), rendered as markdown.
 * Matches Vue's subagent output box but with prose instead of a plain <pre>.
 */
function SubagentOutput({ tool }: { tool: ToolCall }) {
  const text = (tool.displayOutput || tool.output || '').trim()
  if (!text) return null
  // Cap very long outputs for DOM size; still enough for typical summaries.
  const capped = text.length > 12000 ? text.slice(0, 12000) + '\n\n…' : text
  const html = renderMarkdown(capped)
  return (
    <div
      className="toolcall-subagent-output jcode-prose jcode-selectable max-h-72 overflow-y-auto break-words"
      dangerouslySetInnerHTML={{ __html: html }}
    />
  )
}

// ─── Regular tool header (Vue: transparent row, no glyph) ───

function ToolHeader({
  tool,
  expanded,
  onToggle,
}: {
  tool: ToolCall
  expanded: boolean
  onToggle: () => void
}) {
  const title = tool.displayInfo?.title ?? tool.name
  const subtitle = tool.displayInfo?.subtitle ?? ''
  const isContext = tool.displayInfo?.category === 'context'
  const isRunning = tool.status === 'running'
  const isError = tool.status === 'error'
  const diff = useMemo(() => parseDiffCount(tool), [tool])

  return (
    <button
      type="button"
      onClick={onToggle}
      className="flex w-full cursor-pointer items-center gap-1.5 bg-transparent py-1 pl-0 pr-1 text-left transition-opacity hover:opacity-70"
    >
      <span
        className={`shrink-0 text-xs font-medium ${isRunning ? 'shimmer-running' : ''}`}
        style={{
          color: isError ? 'var(--color-destructive, var(--color-error-fg))' : 'var(--color-muted-foreground)',
        }}
      >
        {title}
      </span>
      {subtitle && (
        <span
          className="truncate font-mono text-xs"
          style={{
            color: isContext ? 'var(--color-muted-foreground)' : 'var(--color-accent-neutral)',
          }}
          // Subtitle may contain backend-escaped HTML (path highlights).
          dangerouslySetInnerHTML={{ __html: subtitle }}
        />
      )}
      <ChevronDownIcon
        className={`h-3 w-3 shrink-0 text-[var(--color-muted-foreground)] transition-transform ${
          expanded ? 'rotate-180' : ''
        }`}
      />
      {diff && (diff.added > 0 || diff.deleted > 0) && (
        <span className="ml-auto shrink-0 font-mono text-[10px] tabular-nums">
          {diff.added > 0 && (
            <span style={{ color: 'var(--color-success-fg)' }}>+{diff.added}</span>
          )}
          {diff.added > 0 && diff.deleted > 0 && (
            <span className="mx-0.5" style={{ color: 'var(--color-muted-foreground)' }}>
              /
            </span>
          )}
          {diff.deleted > 0 && (
            <span style={{ color: 'var(--color-error-fg)' }}>-{diff.deleted}</span>
          )}
        </span>
      )}
    </button>
  )
}

// ─── Subagent header (Vue: SUBAGENT label + name + working + call count) ───

function SubagentHeader({
  tool,
  expanded,
  onToggle,
}: {
  tool: ToolCall
  expanded: boolean
  onToggle: () => void
}) {
  const name = useMemo(() => {
    try {
      const parsed = JSON.parse(tool.args)
      return (parsed.name || parsed.description || 'subagent') as string
    } catch {
      return 'subagent'
    }
  }, [tool.args])

  const statusColor =
    tool.status === 'done'
      ? 'var(--color-accent-neutral)'
      : tool.status === 'error'
        ? 'var(--color-destructive, var(--color-error-fg))'
        : 'var(--color-muted-foreground)'

  const glyph = tool.status === 'running' ? '◈' : tool.status === 'done' ? '✓' : '✗'
  const childCount = tool.children?.length ?? 0

  return (
    <button
      type="button"
      onClick={onToggle}
      className="flex w-full cursor-pointer items-center gap-1.5 bg-transparent py-1 pl-0 pr-1 text-left transition-opacity hover:opacity-70"
    >
      <span
        className={`shrink-0 text-[10px] ${tool.status === 'running' ? 'animate-pulse' : ''}`}
        style={{ color: statusColor }}
        aria-hidden
      >
        {glyph}
      </span>
      <span
        className="shrink-0 text-[10px] font-semibold uppercase tracking-wider"
        style={{ color: 'var(--color-muted-foreground)' }}
      >
        Subagent
      </span>
      <span className="font-mono text-[11px]" style={{ color: 'var(--color-foreground)' }}>
        {name}
      </span>
      {tool.status === 'running' && (
        <span
          className="animate-pulse text-[10px]"
          style={{ color: 'var(--color-muted-foreground)' }}
        >
          working
        </span>
      )}
      {childCount > 0 && (
        <span
          className="ml-auto text-[10px] tabular-nums"
          style={{ color: 'var(--color-muted-foreground)' }}
        >
          {childCount} call{childCount === 1 ? '' : 's'}
        </span>
      )}
      <ChevronDownIcon
        className={`ml-1 h-3 w-3 shrink-0 text-[var(--color-muted-foreground)] transition-transform ${
          expanded ? 'rotate-180' : ''
        }`}
      />
    </button>
  )
}

/** Count +N/-M lines from edit/multi_edit args (matches Vue diffData). */
function parseDiffCount(tool: ToolCall): { added: number; deleted: number } | null {
  if (tool.name !== 'edit' && tool.name !== 'multi_edit') return null
  try {
    const parsed = JSON.parse(tool.args)
    if (tool.name === 'multi_edit' && Array.isArray(parsed.edits)) {
      let added = 0
      let deleted = 0
      for (const e of parsed.edits as Array<{ old_string?: string; new_string?: string }>) {
        if (e.old_string) deleted += e.old_string.split('\n').length
        if (e.new_string) added += e.new_string.split('\n').length
      }
      return { added, deleted }
    }
    const oldStr: string = parsed.old_string || ''
    const newStr: string = parsed.new_string || ''
    const isCreate = !oldStr && !!newStr
    return {
      added: newStr ? newStr.split('\n').length : 0,
      deleted: isCreate ? 0 : oldStr ? oldStr.split('\n').length : 0,
    }
  } catch {
    return null
  }
}
