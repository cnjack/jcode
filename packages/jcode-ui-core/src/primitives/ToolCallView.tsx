/**
 * ToolCallView — the headless expand/collapse shell for a tool invocation.
 *
 * Owns: expand/collapse state, status glyph dispatch, and renderer lookup via a
 * ToolRendererRegistry (provided through context by the host). Does NOT own the
 * per-tool rendering logic — that lives in registered renderers. The styled
 * `jcode-ui` `ToolCallCard` wraps this with the jcode visual language.
 *
 * Subagent recursion: when `tool.name === 'subagent'`, children are rendered as
 * nested ToolCallView instances (capped at a max-depth). ask_user tools are
 * routed to the AskUserBlock renderer.
 */

import { useContext, useMemo, useState } from 'react'
import type { ReactNode } from 'react'
import type { ToolCall } from '../types/index.js'
import type { ToolRendererRegistry, ToolRendererProps } from '../adapters/index.js'

/** Context the host provides to wire the registry + the subagent/askuser slots. */
export interface ToolCallContextValue {
  registry: ToolRendererRegistry
  /** Render nested subagent children. Default: recurses into ToolCallView. */
  renderChild?: (child: ToolCall, depth: number) => ReactNode
  /** Render an ask_user tool (interactive question block). */
  renderAskUser?: (tool: ToolCall) => ReactNode
}

import { createContext } from 'react'
const ToolCallCtx = createContext<ToolCallContextValue | null>(null)

export function ToolCallProvider({ value, children }: { value: ToolCallContextValue; children: ReactNode }) {
  return <ToolCallCtx.Provider value={value}>{children}</ToolCallCtx.Provider>
}

export function useToolCallContext(): ToolCallContextValue | null {
  return useContext(ToolCallCtx)
}

export interface ToolCallViewProps {
  tool: ToolCall
  /** Nesting depth (subagent children). 0 = top-level. */
  depth?: number
  /** Max subagent recursion depth. Default 4. */
  maxDepth?: number
  /** Default expanded state. Default false (subagents default true). */
  defaultExpanded?: boolean
  /** Render-prop for the collapsed header. Falls back to a default row. */
  renderHeader?: (tool: ToolCall, expanded: boolean, toggle: () => void) => ReactNode
  /** className passthrough. */
  className?: string
}

const SUBAGENT_NAMES = new Set(['subagent', 'team_spawn'])

export function ToolCallView({
  tool,
  depth = 0,
  maxDepth = 4,
  defaultExpanded,
  className,
  renderHeader,
}: ToolCallViewProps): ReactNode {
  const ctx = useToolCallContext()
  const isSubagent = SUBAGENT_NAMES.has(tool.name)
  const isAskUser = tool.name === 'ask_user' && (!!tool.askUserId || tool.status === 'running')

  const [expanded, setExpanded] = useState(defaultExpanded ?? isSubagent)
  const toggle = useMemo(() => () => setExpanded((e) => !e), [])

  // ask_user: delegate to the ask_user renderer.
  if (isAskUser && ctx?.renderAskUser) {
    return <>{ctx.renderAskUser(tool)}</>
  }

  // Look up a renderer for the body.
  const Renderer = ctx?.registry.get(tool.name) ?? null

  const header =
    renderHeader?.(tool, expanded, toggle) ?? (
      <DefaultToolHeader tool={tool} expanded={expanded} onToggle={toggle} />
    )

  const body = Renderer ? <Renderer {...toRendererProps(tool)} /> : null
  const children =
    isSubagent && tool.children && tool.children.length > 0 && depth < maxDepth
      ? tool.children.map((c) => (
          <div key={c.id} style={{ marginLeft: 12 }}>
            {ctx?.renderChild ? ctx.renderChild(c, depth + 1) : <ToolCallView tool={c} depth={depth + 1} maxDepth={maxDepth} />}
          </div>
        ))
      : null

  return (
    <div className={className} data-tool-name={tool.name} data-tool-status={tool.status}>
      {header}
      {expanded && (
        <div className="toolcall-body">
          {body}
          {children}
        </div>
      )}
    </div>
  )
}

/** Map a ToolCall to the ToolRendererProps contract. */
function toRendererProps(tool: ToolCall): ToolRendererProps {
  return {
    name: tool.name,
    args: tool.args,
    output: tool.output,
    displayOutput: tool.displayOutput,
    error: tool.error,
    status: tool.status,
    displayInfo: tool.displayInfo,
    children: tool.children,
  }
}

/** Minimal default header: status glyph + title + subtitle + chevron. */
function DefaultToolHeader({
  tool,
  expanded,
  onToggle,
}: {
  tool: ToolCall
  expanded: boolean
  onToggle: () => void
}): ReactNode {
  const glyph = tool.status === 'running' ? '◈' : tool.status === 'error' ? '✗' : '✓'
  const title = tool.displayInfo?.title ?? tool.name
  const subtitle = tool.displayInfo?.subtitle ?? ''
  return (
    <button type="button" onClick={onToggle} style={{ display: 'flex', gap: 8, alignItems: 'center', cursor: 'pointer', background: 'none', border: 'none', padding: 0, textAlign: 'left' }}>
      <span aria-hidden>{glyph}</span>
      <span>{title}</span>
      {subtitle && <span style={{ opacity: 0.7 }}>{subtitle}</span>}
      <span aria-hidden>{expanded ? '▾' : '▸'}</span>
    </button>
  )
}
