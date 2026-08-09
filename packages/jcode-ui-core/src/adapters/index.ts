/**
 * Tool renderer registry — the plugin seam for tool-call visualization.
 *
 * `ToolCallCard` doesn't know how to render any specific tool. Instead it looks
 * up a renderer by `tool.name` in a `ToolRendererRegistry`. jcode-ui ships
 * default renderers (terminal/file-viewer/diff/search/…) as a preset; consumers
 * override or extend with their own. This is what makes the component reusable
 * across agents with completely different tool surfaces.
 */

import type { ComponentType } from 'react'
import type {
  ArtifactRef,
  ToolCall,
  ToolDisplayInfo,
  ToolOutcome,
  ToolPhase,
  ToolStatus,
  ToolSurface,
} from '../types/index.js'

export type { ToolStatus }

/** Props every tool renderer receives. */
export interface ToolRendererProps {
  /** Logical tool name (e.g. 'execute', 'read', 'edit', 'grep', …). */
  name: string
  /** Raw args JSON string. Renderers parse what they need. */
  args: string
  /** Raw output string (may be omitted while running). */
  output?: string
  /** Clean display output (backend metadata stripped). */
  displayOutput?: string
  /** Error string if the tool failed. */
  error?: string
  status: ToolStatus
  surface?: ToolSurface
  phase?: ToolPhase
  outcome?: ToolOutcome
  errorCode?: string
  operationID?: string
  provider?: string
  model?: string
  artifacts?: ArtifactRef[]
  startedAt?: number
  /** Pre-extracted display metadata (title/subtitle/icon). May be absent. */
  displayInfo?: ToolDisplayInfo
  /** Nested subagent calls — renderers decide whether to recurse. */
  children?: ToolCall[]
  /** Dual-channel streams (execute). */
  streams?: ToolCall['streams']
  /** Dual-channel meta (execute). */
  meta?: ToolCall['meta']
  /** Dual-channel presentation (execute). */
  presentation?: ToolCall['presentation']
}

/** Map a ToolCall to the renderer contract. Shared by the collapsible shell
 * and standalone timeline surfaces so lifecycle fields cannot drift. */
export function toolCallToRendererProps(tool: ToolCall): ToolRendererProps {
  return {
    name: tool.name,
    args: tool.args,
    output: tool.output,
    displayOutput: tool.displayOutput,
    error: tool.error,
    status: tool.status,
    surface: tool.surface,
    phase: tool.phase,
    outcome: tool.outcome,
    errorCode: tool.errorCode,
    operationID: tool.operationID,
    provider: tool.provider,
    model: tool.model,
    artifacts: tool.artifacts,
    startedAt: tool.startedAt,
    displayInfo: tool.displayInfo,
    children: tool.children,
    streams: tool.streams,
    meta: tool.meta,
    presentation: tool.presentation,
  }
}

/** A tool renderer is just a React component. */
export type ToolRenderer = ComponentType<ToolRendererProps>

/**
 * Name-keyed registry of tool renderers, with a fallback. Lookups are
 * case-sensitive and exact (no globbing) — keep tool names stable.
 *
 * Register a single renderer, or a whole map at once. The registry is mutable
 * so consumers can register at app bootstrap and add more later.
 */
export class ToolRendererRegistry {
  private renderers = new Map<string, ToolRenderer>()
  private fallback: ToolRenderer | null = null

  /** Register a renderer for one or more tool names (later writes win). */
  register(name: string, renderer: ToolRenderer): this
  register(names: string[], renderer: ToolRenderer): this
  register(nameOrNames: string | string[], renderer: ToolRenderer): this {
    const names = Array.isArray(nameOrNames) ? nameOrNames : [nameOrNames]
    for (const n of names) this.renderers.set(n, renderer)
    return this
  }

  /** Register a batch of { name → renderer } entries. */
  registerAll(entries: Record<string, ToolRenderer>): this {
    for (const [name, renderer] of Object.entries(entries)) {
      this.renderers.set(name, renderer)
    }
    return this
  }

  /** Set the renderer used when no name-specific match exists. */
  setFallback(renderer: ToolRenderer): this {
    this.fallback = renderer
    return this
  }

  /** Look up a renderer by tool name, falling back if absent. Returns null
   *  only when nothing is registered AND no fallback is set. */
  get(name: string): ToolRenderer | null {
    return this.renderers.get(name) ?? this.fallback
  }

  /** True if a name-specific renderer is registered. */
  has(name: string): boolean {
    return this.renderers.has(name)
  }
}

/** Create a fresh registry. Convenience over `new` for chained registration. */
export function createToolRendererRegistry(): ToolRendererRegistry {
  return new ToolRendererRegistry()
}
