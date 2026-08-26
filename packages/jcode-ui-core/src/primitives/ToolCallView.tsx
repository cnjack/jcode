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
import { getApprovalOutcome } from '../types/index.js'
import type { ToolCall } from '../types/index.js'
import { toolCallToRendererProps } from '../adapters/index.js'
import type { ToolRendererRegistry } from '../adapters/index.js'

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
  /**
   * Optional custom renderer for the full children list (e.g. compact rows +
   * exploring groups). When set, overrides per-child renderChild recursion.
   */
  renderSubagentChildren?: (children: ToolCall[]) => ReactNode
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
  renderSubagentChildren,
}: ToolCallViewProps): ReactNode {
  const ctx = useToolCallContext()
  const approvalOutcome = tool.approval ? getApprovalOutcome(tool.approval) : undefined
  const effectiveTool = approvalOutcome === 'denied' && !tool.denied
    ? { ...tool, denied: true }
    : tool
  const canRenderResult = !effectiveTool.denied
  // Only the recursive subagent tool — team_spawn has its own renderer (Vue parity).
  const isSubagent = effectiveTool.name === 'subagent'
  const isAskUser = effectiveTool.name === 'ask_user'

  const [expanded, setExpanded] = useState(defaultExpanded ?? (isSubagent && canRenderResult))
  const toggle = useMemo(() => () => setExpanded((e) => !e), [])

  // ask_user: delegate to the ask_user renderer.
  if (canRenderResult && isAskUser && ctx?.renderAskUser) {
    return <>{ctx.renderAskUser(effectiveTool)}</>
  }

  // Look up a renderer for the body (not used for subagent shells — no args dump).
  const Renderer = canRenderResult ? (ctx?.registry.get(effectiveTool.name) ?? null) : null

  const header =
    renderHeader?.(effectiveTool, expanded, toggle) ?? (
      <DefaultToolHeader tool={effectiveTool} expanded={expanded} onToggle={toggle} />
    )

  // A denied invocation is a receipt, not an executable/renderable result.
  // Keep its disclosure shell available for status context, but never mount a
  // host renderer even if the user expands the card.
  const body = canRenderResult && !isSubagent && Renderer
    ? <Renderer {...toolCallToRendererProps(effectiveTool)} />
    : null

  const childTools =
    canRenderResult &&
    isSubagent &&
    effectiveTool.children &&
    effectiveTool.children.length > 0 &&
    depth < maxDepth
      ? effectiveTool.children
      : null

  const children = childTools
    ? renderSubagentChildren
      ? renderSubagentChildren(childTools)
      : childTools.map((c) =>
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
              renderSubagentChildren={renderSubagentChildren}
            />
          ),
        )
    : null

  // Prefer displayOutput (clean) over raw output; never surface args for subagents.
  const subagentText = effectiveTool.displayOutput || effectiveTool.output || ''
  // When done with a result, show result first prominence (children still available).
  const showResultFirst = isSubagent && effectiveTool.status === 'done' && !!subagentText

  return (
    <div
      data-jcode-ui=""
      className={className}
      data-tool-name={effectiveTool.name}
      data-tool-status={effectiveTool.status}
      data-tool-denied={effectiveTool.denied ? 'true' : undefined}
      data-tool-awaiting-approval={effectiveTool.awaitingApproval ? 'true' : undefined}
      data-expanded={expanded ? 'true' : 'false'}
    >
      {header}

      {/* Subagent: children + output only (no args). Output rendered by styled slot. */}
      {expanded && canRenderResult && isSubagent && (
        <div className="toolcall-subagent-body">
          {showResultFirst &&
            (renderSubagentOutput
              ? renderSubagentOutput(effectiveTool)
              : subagentText
                ? <pre className="toolcall-subagent-output">{truncate(subagentText, 2000)}</pre>
                : null)}
          {children ? (
            <div className={renderSubagentChildren ? undefined : 'toolcall-subagent-children'}>
              {children}
            </div>
          ) : effectiveTool.status === 'running' && !subagentText ? (
            <div className="toolcall-subagent-starting">Starting…</div>
          ) : null}
          {!showResultFirst &&
            (renderSubagentOutput
              ? renderSubagentOutput(effectiveTool)
              : subagentText
                ? <pre className="toolcall-subagent-output">{truncate(subagentText, 2000)}</pre>
                : null)}
          {effectiveTool.error ? <pre className="toolcall-subagent-error">{effectiveTool.error}</pre> : null}
        </div>
      )}

      {/* Regular tool: single content box under the title (top edge = divider). */}
      {expanded && canRenderResult && !isSubagent && (
        <div
          className="toolcall-body jcode-selectable"
          data-selectable
          data-tool-status={effectiveTool.status}
        >
          {body}
        </div>
      )}
    </div>
  )
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
