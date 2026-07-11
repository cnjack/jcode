/**
 * ContextBar — token/context usage indicator.
 *
 * Reads the TokenSnapshot from the runtime and renders an SVG ring showing
 * context-window occupancy (turns red at ≥90%) plus a small popover with the
 * bucket breakdown. The full bucket breakdown (system/tools/mcp/skills/messages)
 * requires host-provided per-task stats; when absent we show the simple ring.
 */

import { memo, useMemo } from 'react'
import { useRuntimeSelector } from 'jcode-ui-core/runtime'
import type { TaskContextBreakdown } from 'jcode-ui-core'

export interface ContextBarProps {
  /** Optional host-provided context breakdown for the popover. */
  breakdown?: TaskContextBreakdown | null
  /** Diameter of the ring in px. Default 20. */
  size?: number
  /** Threshold (0-1) above which the ring turns red. Default 0.9. */
  dangerThreshold?: number
  /** Show the popover on hover. Default true. */
  showPopover?: boolean
}

export const ContextBar = memo(function ContextBar({
  breakdown,
  size = 20,
  dangerThreshold = 0.9,
  showPopover = true,
}: ContextBarProps) {
  const snapshot = useRuntimeSelector((s) => s.tokenSnapshot)
  const pct = useMemo(() => {
    if (!snapshot || !snapshot.model_context_limit) return 0
    return Math.min(1, snapshot.total_tokens / snapshot.model_context_limit)
  }, [snapshot])

  const danger = pct >= dangerThreshold
  const r = (size - 4) / 2
  const circ = 2 * Math.PI * r

  return (
    <div data-jcode-ui="" className="jcode-context-bar group relative inline-flex">
      <svg width={size} height={size} className="-rotate-90">
        <circle
          cx={size / 2}
          cy={size / 2}
          r={r}
          fill="none"
          stroke="var(--jcode-color-border)"
          strokeWidth={2}
        />
        <circle
          cx={size / 2}
          cy={size / 2}
          r={r}
          fill="none"
          stroke={danger ? 'var(--jcode-color-error-fg)' : 'var(--jcode-color-primary)'}
          strokeWidth={2}
          strokeDasharray={circ}
          strokeDashoffset={circ * (1 - pct)}
          strokeLinecap="round"
          className="transition-all"
        />
      </svg>
      {showPopover && (
        <div className="pointer-events-none absolute bottom-full right-0 mb-2 w-56 rounded-[var(--jcode-radius-lg)] border border-[var(--jcode-color-border)] bg-[var(--jcode-color-surface)] p-3 opacity-0 shadow-[var(--jcode-shadow-md)] transition-opacity group-hover:opacity-100">
          <div className="mb-2 text-[0.72rem] font-medium text-[var(--jcode-color-foreground)]">Context</div>
          {snapshot ? (
            <div className="space-y-0.5 text-[0.72rem]">
              <Row label="tokens" value={formatNum(snapshot.total_tokens)} />
              <Row label="limit" value={formatNum(snapshot.model_context_limit)} />
              <Row label="occupancy" value={`${Math.round(pct * 100)}%`} />
              {snapshot.cache_supported && snapshot.cache_hit_rate != null && (
                <Row label="cache hit" value={`${Math.round(snapshot.cache_hit_rate * 100)}%`} />
              )}
            </div>
          ) : (
            <div className="text-[0.72rem] text-[var(--jcode-color-muted-foreground)]">No usage yet</div>
          )}
          {breakdown && (
            <div className="mt-2 space-y-0.5 border-t border-[var(--jcode-color-border)] pt-2 text-[0.72rem]">
              <Row label="messages" value={formatNum(breakdown.messages_tokens)} />
              <Row label="system prompt" value={formatNum(breakdown.system_prompt_tokens)} />
              <Row label="tools" value={formatNum(breakdown.system_tools_tokens)} />
              {breakdown.mcp_tools_tokens > 0 && <Row label="mcp" value={formatNum(breakdown.mcp_tools_tokens)} />}
              {breakdown.skills_tokens > 0 && <Row label="skills" value={formatNum(breakdown.skills_tokens)} />}
            </div>
          )}
        </div>
      )}
    </div>
  )
})

function Row({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex justify-between gap-2">
      <span className="text-[var(--jcode-color-muted-foreground)]">{label}</span>
      <span className="font-mono text-[var(--jcode-color-foreground)]">{value}</span>
    </div>
  )
}

function formatNum(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`
  if (n >= 1_000) return `${Math.round(n / 1_000)}K`
  return String(n)
}

// Re-export the breakdown type for hosts building custom bars.
export type { TaskContextBreakdown }
