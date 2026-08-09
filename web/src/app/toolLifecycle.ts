import type { ArtifactRef, ToolCall, ToolOutcome, ToolPhase } from 'jcode-ui-core'

export interface ToolLifecycleUpdate {
  operationID?: string
  phase?: ToolPhase
  outcome?: ToolOutcome
  errorCode?: string
  provider?: string
  model?: string
  artifacts?: ArtifactRef[]
}

export type WireToolPhase = ToolPhase | ToolOutcome

/** Normalize the transport contract, whose terminal phases use the concrete
 * outcome name, into the core's sortable `terminal` + `outcome` pair. */
export function normalizeWireLifecycle(
  phase?: WireToolPhase,
  outcome?: ToolOutcome,
): Pick<ToolLifecycleUpdate, 'phase' | 'outcome'> {
  if (phase === 'succeeded' || phase === 'failed' || phase === 'cancelled' || phase === 'uncertain') {
    return { phase: 'terminal', outcome: outcome ?? phase }
  }
  return { phase, outcome }
}

export type ToolLifecycleMergeResult = 'updated' | 'stale' | 'operation_mismatch'

const PHASE_RANK: Record<ToolPhase, number> = {
  queued: 0,
  generating: 1,
  saving: 2,
  terminal: 3,
}

/** Merge typed lifecycle data into a ToolCall. The function mutates `tool` so
 * it works inside an Immer reducer as well as replay reconstruction. */
export function mergeToolLifecycle(tool: ToolCall, update: ToolLifecycleUpdate): ToolLifecycleMergeResult {
  if (tool.operationID && update.operationID && tool.operationID !== update.operationID) {
    return 'operation_mismatch'
  }
  if (!tool.operationID && update.operationID) tool.operationID = update.operationID

  // Provider/model belong to the immutable operation snapshot. Duplicate
  // lifecycle events may fill a missing value, but never rewrite history.
  if (!tool.provider && update.provider) tool.provider = update.provider
  if (!tool.model && update.model) tool.model = update.model

  const incomingPhase = update.phase ?? (update.outcome ? 'terminal' : undefined)
  const currentPhase = tool.phase ?? (tool.outcome ? 'terminal' : undefined)
  const isStale = !!incomingPhase && !!currentPhase && PHASE_RANK[incomingPhase] < PHASE_RANK[currentPhase]

  // Artifact reconciliation remains useful for duplicate terminal events, but
  // never accept it from a mismatched operation (handled above).
  if (update.artifacts?.length) tool.artifacts = mergeArtifacts(tool.artifacts, update.artifacts)
  if (isStale) return 'stale'
  if (currentPhase === 'terminal' && tool.outcome && update.outcome && tool.outcome !== update.outcome) {
    return 'stale'
  }

  if (incomingPhase) tool.phase = incomingPhase
  if (incomingPhase === 'terminal') {
    // A terminal decision is immutable. Duplicate events may fill missing
    // metadata, but a conflicting terminal result cannot rewrite history.
    if (!tool.outcome && update.outcome) tool.outcome = update.outcome
    if (!tool.errorCode && update.errorCode) tool.errorCode = update.errorCode
    applyTerminalStatus(tool)
  }
  return 'updated'
}

/** Agent/session completion must not turn a missing image result into success. */
export function settleIncompleteImageTool(tool: ToolCall): void {
  if (tool.name !== 'generate_image' || tool.phase === 'terminal') return
  mergeToolLifecycle(tool, {
    phase: 'terminal',
    outcome: tool.phase === 'queued' || !tool.phase ? 'cancelled' : 'uncertain',
    errorCode: tool.phase === 'queued' || !tool.phase ? 'cancelled_before_dispatch' : 'operation_incomplete',
  })
}

export function mergeArtifacts(current: ArtifactRef[] | undefined, incoming: ArtifactRef[]): ArtifactRef[] {
  const merged = new Map<string, ArtifactRef>()
  for (const artifact of current ?? []) merged.set(artifact.id, artifact)
  for (const artifact of incoming) {
    const previous = merged.get(artifact.id)
    merged.set(artifact.id, previous ? { ...previous, ...artifact } : artifact)
  }
  return [...merged.values()]
}

function applyTerminalStatus(tool: ToolCall): void {
  if (tool.outcome === 'succeeded' || tool.outcome === 'cancelled') {
    tool.status = 'done'
    return
  }
  if (tool.outcome === 'failed' || tool.outcome === 'uncertain') tool.status = 'error'
}
