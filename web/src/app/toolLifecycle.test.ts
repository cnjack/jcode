import { describe, expect, it } from 'vitest'
import type { ArtifactRef, ToolCall } from 'jcode-ui-core'
import { mergeToolLifecycle, normalizeWireLifecycle, settleIncompleteImageTool } from './toolLifecycle'

function image(overrides: Partial<ToolCall> = {}): ToolCall {
  return {
    id: 'image-1',
    toolCallID: 'tc-1',
    operationID: 'op-1',
    name: 'generate_image',
    args: '{}',
    status: 'running',
    surface: 'standalone',
    phase: 'queued',
    timestamp: 0,
    ...overrides,
  }
}

const artifact: ArtifactRef = {
  id: 'artifact-1',
  storage: 'managed',
  key: 'images/artifact-1.png',
  title: 'Generated image',
  kind: 'image',
  media_type: 'image/png',
  size: 10,
}

describe('mergeToolLifecycle', () => {
  it('normalizes wire terminal phases without losing a typed outcome', () => {
    expect(normalizeWireLifecycle('succeeded')).toEqual({ phase: 'terminal', outcome: 'succeeded' })
    expect(normalizeWireLifecycle('saving')).toEqual({ phase: 'saving', outcome: undefined })
  })

  it('is monotonic and keeps terminal outcomes immutable', () => {
    const tool = image()
    expect(mergeToolLifecycle(tool, { phase: 'generating' })).toBe('updated')
    expect(mergeToolLifecycle(tool, { phase: 'queued' })).toBe('stale')
    expect(tool.phase).toBe('generating')
    mergeToolLifecycle(tool, { phase: 'terminal', outcome: 'succeeded', artifacts: [artifact] })
    expect(tool.status).toBe('done')
    expect(mergeToolLifecycle(tool, { phase: 'saving' })).toBe('stale')
    mergeToolLifecycle(tool, { phase: 'terminal', outcome: 'failed', errorCode: 'late_error' })
    expect(tool.outcome).toBe('succeeded')
    expect(tool.errorCode).toBeUndefined()
  })

  it('rejects another operation and deduplicates artifact ids', () => {
    const tool = image({ artifacts: [artifact] })
    expect(mergeToolLifecycle(tool, { operationID: 'op-2', phase: 'saving' })).toBe('operation_mismatch')
    expect(tool.phase).toBe('queued')
    mergeToolLifecycle(tool, { operationID: 'op-1', artifacts: [{ ...artifact, size: 20 }] })
    expect(tool.artifacts).toHaveLength(1)
    expect(tool.artifacts?.[0]?.size).toBe(20)
  })

  it('settles pre-dispatch calls as cancelled and dispatched calls as uncertain', () => {
    const queued = image()
    settleIncompleteImageTool(queued)
    expect(queued.outcome).toBe('cancelled')
    expect(queued.status).toBe('done')

    const generating = image({ phase: 'generating' })
    settleIncompleteImageTool(generating)
    expect(generating.outcome).toBe('uncertain')
    expect(generating.status).toBe('error')
  })
})
