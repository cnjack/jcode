/**
 * ToolCallCard — styled tool-invocation shell.
 *
 * - Header is a borderless plain-text row (title + subtitle + chevron).
 * - Content box is ONE shared frame (`.toolcall-body`).
 * - Subagent: compact header + compact children rows + result prominence.
 */

import { memo, useMemo } from 'react'
import type { ReactNode } from 'react'
import { ChevronDownIcon } from '@heroicons/react/24/outline'
import type { ToolCall } from 'jcode-ui-core'
import { groupExploringTimeline, summarizeExploringSteps } from 'jcode-ui-core'
import { ToolCallView, ToolCallProvider } from 'jcode-ui-core/primitives'
import type { ToolRendererRegistry } from 'jcode-ui-core/adapters'
import { AskUserCard } from './AskUserCard.js'
import { CompactToolRow } from './CompactToolRow.js'
import { useToolRegistry } from './ToolRegistryContext.js'
import { renderMarkdown } from '../lib/markdown.js'

export interface ToolCallCardSlots {
  /**
   * Replace the content of the title-row button (expand/collapse interaction
   * is preserved — clicking still toggles the card).
   */
  header?: (tool: ToolCall) => ReactNode
  /** Extra content appended below the card body. */
  footer?: (tool: ToolCall) => ReactNode
}

export interface ToolCallCardProps {
  tool: ToolCall
  /** Override the registry (defaults to the context-provided one). */
  registry?: ToolRendererRegistry
  /** Extra classes (e.g. pl-9 to indent under the message content column). */
  className?: string
  /** Nesting depth for subagent children. */
  depth?: number
  /** Optional header/footer overrides. Omit for the default card (unchanged). */
  slots?: ToolCallCardSlots
}

export const ToolCallCard = memo(function ToolCallCard({
  tool,
  registry,
  className,
  depth = 0,
  slots,
}: ToolCallCardProps) {
  const ctxRegistry = useToolRegistry()
  const reg = registry ?? ctxRegistry
  const providerValue = useMemo(
    () => ({
      registry: reg,
      renderAskUser: (t: ToolCall) => <AskUserCard tool={t} />,
      // Nested subagent children use compact rows (not full ToolCallCards).
      renderChild: (child: ToolCall) => <CompactToolRow tool={child} />,
    }),
    [reg],
  )
  return (
    <ToolCallProvider value={providerValue}>
      <ToolCallView
        tool={tool}
        depth={depth}
        data-jcode-ui="" className={`jcode-toolcall my-1 ${className ?? ''}`}
        renderHeader={(t, expanded, toggle) =>
          slots?.header ? (
            <button
              type="button"
              onClick={toggle}
              data-expanded={expanded ? 'true' : 'false'}
              className="jcode-toolcall__slot-header flex w-full max-w-full cursor-pointer items-center gap-1.5 bg-transparent text-left"
            >
              {slots.header(t)}
            </button>
          ) : t.name === 'subagent' ? (
            <SubagentHeader tool={t} expanded={expanded} onToggle={toggle} />
          ) : (
            <ToolHeader tool={t} expanded={expanded} onToggle={toggle} />
          )
        }
        renderSubagentOutput={(t) => <SubagentOutput tool={t} />}
        renderSubagentChildren={(children) => <SubagentChildren tools={children} />}
      />
      {slots?.footer && <div className="jcode-toolcall__footer">{slots.footer(tool)}</div>}
    </ToolCallProvider>
  )
})

/**
 * Subagent body: output only (never args), rendered as markdown.
 */
function SubagentOutput({ tool }: { tool: ToolCall }) {
  const text = (tool.displayOutput || tool.output || '').trim()
  if (!text) return null
  const capped = text.length > 12000 ? text.slice(0, 12000) + '\n\n…' : text
  const html = renderMarkdown(capped)
  return (
    <div
      className="toolcall-subagent-output jcode-prose jcode-selectable max-h-72 overflow-y-auto break-words"
      data-testid="subagent-result"
      dangerouslySetInnerHTML={{ __html: html }}
    />
  )
}

/** Compact children: exploring-group summary when possible, else compact rows. */
function SubagentChildren({ tools }: { tools: ToolCall[] }) {
  // Build a mini-timeline of tool items so we can reuse exploring grouping.
  const items = tools.map((t, i) => ({ kind: 'tool' as const, data: t, seq: i + 1 }))
  const grouped = groupExploringTimeline(items)

  return (
    <div className="toolcall-subagent-children" data-testid="subagent-children">
      {grouped.map((unit) => {
        if (unit.kind === 'exploring') {
          const steps = summarizeExploringSteps(unit.data.tools)
          const label = unit.data.status === 'running' ? 'Exploring' : 'Explored'
          return (
            <div key={unit.data.id} className="jcode-exploring jcode-exploring--nested py-1">
              <div
                className={`text-[11px] font-medium ${unit.data.status === 'running' ? 'shimmer-running' : ''}`}
                style={{ color: 'var(--jcode-color-muted-foreground)' }}
              >
                {label} · {unit.data.tools.length} steps
              </div>
              {steps.map((s, i) => (
                <div key={i} className="jcode-exploring__step">
                  <span className="jcode-exploring__action">{s.action}</span>
                  {s.detail ? <span className="jcode-exploring__detail">{s.detail}</span> : null}
                </div>
              ))}
            </div>
          )
        }
        if (unit.kind === 'tool') {
          return <CompactToolRow key={unit.data.id} tool={unit.data} />
        }
        return null
      })}
    </div>
  )
}

// ─── Regular tool header ───

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
  const isError = tool.status === 'error' || (tool.meta?.exit_code !== undefined && tool.meta.exit_code !== 0)
  const diff = useMemo(() => parseDiffCount(tool), [tool])
  const exitBadge =
    tool.name === 'execute' && tool.meta?.exit_code !== undefined
      ? `exit ${tool.meta.exit_code}`
      : null
  const durationBadge =
    tool.meta?.duration_ms && tool.meta.duration_ms > 0
      ? tool.meta.duration_ms < 1000
        ? `${tool.meta.duration_ms}ms`
        : `${(tool.meta.duration_ms / 1000).toFixed(1)}s`
      : null

  return (
    <button
      type="button"
      onClick={onToggle}
      className="flex w-full max-w-full cursor-pointer items-center gap-1.5 bg-transparent text-left"
    >
      <span
        className={`shrink-0 text-xs font-medium tracking-wide ${isRunning ? 'shimmer-running' : ''}`}
        style={{
          color: isError ? 'var(--jcode-color-destructive, var(--jcode-color-error-fg))' : 'var(--jcode-color-muted-foreground)',
        }}
      >
        {title}
      </span>
      {subtitle && (
        <span
          className="jcode-toolcall__subtitle min-w-0 truncate font-mono text-[0.72rem]"
          style={{
            color: isContext
              ? 'var(--jcode-color-muted-foreground)'
              : 'var(--jcode-color-foreground)',
            opacity: 0.88,
          }}
          dangerouslySetInnerHTML={{ __html: subtitle }}
        />
      )}
      {(exitBadge || durationBadge) && (
        <span
          className="shrink-0 font-mono text-[10px] tabular-nums"
          style={{ color: isError ? 'var(--jcode-color-error-fg)' : 'var(--jcode-color-muted-foreground)' }}
        >
          {[exitBadge, durationBadge].filter(Boolean).join(' · ')}
        </span>
      )}
      <ChevronDownIcon
        className={`h-3 w-3 shrink-0 text-[var(--jcode-color-muted-foreground)] transition-transform duration-[var(--jcode-duration-normal)] ${
          expanded ? 'rotate-180' : ''
        }`}
      />
      {diff && (diff.added > 0 || diff.deleted > 0) && (
        <span className="jcode-toolcall__diff shrink-0 rounded-[var(--jcode-radius-sm)] bg-[var(--jcode-color-muted)] px-1.5 py-0.5 font-mono text-[10px] tabular-nums">
          {diff.added > 0 && (
            <span style={{ color: 'var(--jcode-color-success-fg)' }}>+{diff.added}</span>
          )}
          {diff.added > 0 && diff.deleted > 0 && (
            <span className="mx-0.5" style={{ color: 'var(--jcode-color-muted-foreground)' }}>
              /
            </span>
          )}
          {diff.deleted > 0 && (
            <span style={{ color: 'var(--jcode-color-error-fg)' }}>-{diff.deleted}</span>
          )}
        </span>
      )}
    </button>
  )
}

// ─── Subagent header ───

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
      return (parsed.description || parsed.name || 'subagent') as string
    } catch {
      return 'subagent'
    }
  }, [tool.args])

  const statusLabel =
    tool.status === 'done' ? 'Done' : tool.status === 'error' ? 'Error' : 'Running'
  const statusColor =
    tool.status === 'done'
      ? 'var(--jcode-color-muted-foreground)'
      : tool.status === 'error'
        ? 'var(--jcode-color-destructive, var(--jcode-color-error-fg))'
        : 'var(--jcode-color-primary)'
  const childCount = tool.children?.length ?? 0

  return (
    <button
      type="button"
      onClick={onToggle}
      className="flex w-full cursor-pointer items-center gap-1.5 bg-transparent py-1 pl-0 pr-1 text-left transition-opacity hover:opacity-70"
      data-testid="subagent-header"
    >
      <span
        className="shrink-0 rounded px-1.5 py-0.5 text-[10px] font-semibold uppercase tracking-wider"
        style={{
          color: 'var(--jcode-color-muted-foreground)',
          background: 'var(--jcode-color-muted)',
        }}
      >
        Agent
      </span>
      <span
        className={`min-w-0 truncate text-[12px] font-medium ${tool.status === 'running' ? 'shimmer-running' : ''}`}
        style={{ color: 'var(--jcode-color-foreground)' }}
      >
        {name}
      </span>
      <span
        className={`shrink-0 text-[10px] ${tool.status === 'running' ? 'animate-pulse' : ''}`}
        style={{ color: statusColor }}
      >
        {statusLabel}
      </span>
      {childCount > 0 && (
        <span
          className="ml-auto text-[10px] tabular-nums"
          style={{ color: 'var(--jcode-color-muted-foreground)' }}
        >
          {childCount} step{childCount === 1 ? '' : 's'}
        </span>
      )}
      <ChevronDownIcon
        className={`ml-1 h-3 w-3 shrink-0 text-[var(--jcode-color-muted-foreground)] transition-transform ${
          expanded ? 'rotate-180' : ''
        }`}
      />
    </button>
  )
}

/** Count +N/-M lines from edit/multi_edit args. */
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
