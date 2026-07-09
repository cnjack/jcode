/**
 * ToolCallView — the headless expand/collapse shell for a tool invocation.
 *
 * Owns: expand/collapse state, renderer lookup via ToolRendererRegistry, and
 * subagent recursion. Does NOT own per-tool body chrome — the styled
 * `jcode-ui` `ToolCallCard` supplies header styling + CSS for `.toolcall-body`.
 *
 * Subagent: only `tool.name === 'subagent'` (NOT team_spawn — that has its own
 * renderer). Children recurse as nested ToolCallView instances. ask_user tools
 * route to the host's renderAskUser slot.
 */

import { createContext, useContext, useMemo, useState } from 'react'
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
  /** Render-prop for the header. Falls back to a default row. */
  renderHeader?: (tool: ToolCall, expanded: boolean, toggle: () => void) => ReactNode
  /**
   * Optional subagent body (output/error). Styled layer supplies markdown.
   * Receives only output-related fields — never args.
   */
  renderSubagentOutput?: (tool: ToolCall) => ReactNode
  /** className passthrough. */
  className?: string
}

export function ToolCallView({
  tool,
  depth = 0,
  maxDepth = 4,
  defaultExpanded,
  className,
  renderHeader,
  renderSubagentOutput,
}: ToolCallViewProps): ReactNode {
  const ctx = useToolCallContext()
  // Only the recursive subagent tool — team_spawn has its own renderer (Vue parity).
  const isSubagent = tool.name === 'subagent'
  const isAskUser = tool.name === 'ask_user' && (!!tool.askUserId || tool.status === 'running')

  const [expanded, setExpanded] = useState(defaultExpanded ?? isSubagent)
  const toggle = useMemo(() => () => setExpanded((e) => !e), [])

  // ask_user: delegate to the ask_user renderer.
  if (isAskUser && ctx?.renderAskUser) {
    return <>{ctx.renderAskUser(tool)}</>
  }

  // Look up a renderer for the body (not used for subagent shells — no args dump).
  const Renderer = ctx?.registry.get(tool.name) ?? null

  const header =
    renderHeader?.(tool, expanded, toggle) ?? (
      <DefaultToolHeader tool={tool} expanded={expanded} onToggle={toggle} />
    )

  const body = !isSubagent && Renderer ? <Renderer {...toRendererProps(tool)} /> : null

  const children =
    isSubagent && tool.children && tool.children.length > 0 && depth < maxDepth
      ? tool.children.map((c) =>
          ctx?.renderChild ? (
            <div key={c.id}>{ctx.renderChild(c, depth + 1)}</div>
          ) : (
            <ToolCallView
              key={c.id}
              tool={c}
              depth={depth + 1}
              maxDepth={maxDepth}
              renderHeader={renderHeader}
              renderSubagentOutput={renderSubagentOutput}
            />
          ),
        )
      : null

  // Prefer displayOutput (clean) over raw output; never surface args for subagents.
  const subagentText = tool.displayOutput || tool.output || ''

  return (
    <div
      className={className}
      data-tool-name={tool.name}
      data-tool-status={tool.status}
      data-expanded={expanded ? 'true' : 'false'}
    >
      {header}

      {/* Subagent: children + output only (no args). Output rendered by styled slot. */}
      {expanded && isSubagent && (
        <div className="toolcall-subagent-body">
          {children && children.length > 0 ? (
            <div className="toolcall-subagent-children">{children}</div>
          ) : tool.status === 'running' && !subagentText ? (
            <div className="toolcall-subagent-starting">Starting…</div>
          ) : null}
          {renderSubagentOutput
            ? renderSubagentOutput(tool)
            : subagentText
              ? <pre className="toolcall-subagent-output">{truncate(subagentText, 2000)}</pre>
              : null}
          {tool.error ? <pre className="toolcall-subagent-error">{tool.error}</pre> : null}
        </div>
      )}

      {/* Regular tool: single content box under the title (top edge = divider). */}
      {expanded && !isSubagent && (
        <div
          className="toolcall-body jcode-selectable"
          data-selectable
          data-tool-status={tool.status}
        >
          {body}
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

function truncate(text: string, max: number): string {
  const chars = [...text]
  return chars.length > max ? chars.slice(0, max).join('') + `… (${chars.length} chars)` : text
}

/** Minimal default header (headless fallback). */
function DefaultToolHeader({
  tool,
  expanded,
  onToggle,
}: {
  tool: ToolCall
  expanded: boolean
  onToggle: () => void
}): ReactNode {
  const title = tool.displayInfo?.title ?? tool.name
  const subtitle = tool.displayInfo?.subtitle ?? ''
  return (
    <button
      type="button"
      onClick={onToggle}
      style={{
        display: 'flex',
        gap: 6,
        alignItems: 'center',
        cursor: 'pointer',
        background: 'none',
        border: 'none',
        padding: 0,
        textAlign: 'left',
      }}
    >
      <span>{title}</span>
      {subtitle && <span style={{ opacity: 0.7 }}>{subtitle}</span>}
      <span aria-hidden>{expanded ? '▾' : '▸'}</span>
    </button>
  )
}
