import { describe, expect, it } from 'vitest'
import type { SessionEntry } from '../lib/types'
import { chatActions, hasToolLifecycleHost, modelActions, replayTimeline, store } from './store'

function imageTool(entries: SessionEntry[], running = false) {
  const item = replayTimeline(entries, running).find((entry) => entry.kind === 'tool')
  if (!item || item.kind !== 'tool') throw new Error('image tool was not replayed')
  return item.data
}

function imageTools(entries: SessionEntry[], running = false) {
  return replayTimeline(entries, running)
    .filter((entry) => entry.kind === 'tool')
    .map((entry) => entry.data)
    .filter((tool) => tool.name === 'generate_image')
}

describe('image-generation replay reconciliation', () => {
  it('prefers a terminal operation and attaches its managed Artifact', () => {
    const tool = imageTool([
      { type: 'tool_call', name: 'generate_image', args: '{"prompt":"desk"}', tool_call_id: 'tc-1' },
      { type: 'generation_operation', tool_call_id: 'tc-1', operation_id: 'op-1', operation_state: 'dispatch_attempted' },
      {
        type: 'artifact', tool_call_id: 'tc-1', operation_id: 'op-1', artifact_id: 'artifact-1',
        artifact_storage_kind: 'managed', artifact_key: 'images/artifact-1.png', artifact_title: 'Desk',
        artifact_kind: 'image', artifact_media_type: 'image/png', artifact_size: 2048, artifact_revision: 1,
      },
      {
        type: 'generation_operation', tool_call_id: 'tc-1', operation_id: 'op-1', operation_state: 'succeeded',
        artifact_ids: ['artifact-1'],
      },
      { type: 'tool_result', name: 'generate_image', tool_call_id: 'tc-1', output: 'artifact-1' },
    ])

    expect(tool.surface).toBe('standalone')
    expect(tool.phase).toBe('terminal')
    expect(tool.outcome).toBe('succeeded')
    expect(tool.artifacts?.[0]).toMatchObject({ id: 'artifact-1', storage: 'managed', key: 'images/artifact-1.png' })
  })

  it('marks a dispatched non-terminal historical operation uncertain', () => {
    const tool = imageTool([
      { type: 'tool_call', name: 'generate_image', args: '{}', tool_call_id: 'tc-1' },
      { type: 'generation_operation', tool_call_id: 'tc-1', operation_id: 'op-1', operation_state: 'accepted' },
    ])
    expect(tool.phase).toBe('terminal')
    expect(tool.outcome).toBe('uncertain')
  })

  it('lets a terminal tool result outrank a non-terminal operation journal', () => {
    const tool = imageTool([
      { type: 'tool_call', name: 'generate_image', args: '{}', tool_call_id: 'tc-1' },
      { type: 'generation_operation', tool_call_id: 'tc-1', operation_id: 'op-1', operation_state: 'accepted' },
      {
        type: 'tool_result', name: 'generate_image', tool_call_id: 'tc-1', operation_id: 'op-1',
        outcome: 'failed', error_code: 'rate_limited', error: 'provider request failed',
      },
    ])
    expect(tool.phase).toBe('terminal')
    expect(tool.outcome).toBe('failed')
    expect(tool.errorCode).toBe('rate_limited')
  })

  it('keeps a failed card bound to its operation model after the current image model changes', () => {
    store.dispatch(modelActions.setImageModel('new-provider/new-model'))
    const tool = imageTool([
      { type: 'tool_call', name: 'generate_image', args: '{}', tool_call_id: 'tc-old' },
      {
        type: 'generation_operation', tool_call_id: 'tc-old', operation_id: 'op-old',
        operation_state: 'failed', error_code: 'authentication_failed',
        operation_capability_key: {
          provider_profile_id: 'old-provider', endpoint_profile: 'image:old', model_id: 'old-model',
        },
      },
    ])
    expect(tool.provider).toBe('old-provider')
    expect(tool.model).toBe('old-model')

    store.dispatch(modelActions.setImageModel('another-provider/another-model'))
    expect(tool.provider).toBe('old-provider')
    expect(tool.model).toBe('old-model')
  })

  it('preserves current live phases but cancels a never-dispatched historical call', () => {
    const entries: SessionEntry[] = [
      { type: 'tool_call', name: 'generate_image', args: '{}', tool_call_id: 'tc-1' },
    ]
    expect(imageTool(entries, true).phase).toBe('queued')
    expect(imageTool(entries, false).outcome).toBe('cancelled')
  })

  it('keeps reused tool-call IDs bound to their own turn occurrence', () => {
    const tools = imageTools([
      { type: 'tool_call', name: 'generate_image', args: '{"prompt":"A"}', tool_call_id: 'tc-reused' },
      {
        type: 'tool_result', name: 'generate_image', tool_call_id: 'tc-reused',
        operation_id: 'tc-reused', outcome: 'cancelled', denied: true, error_code: 'approval_denied',
      },
      { type: 'user', content: 'try another image' },
      { type: 'tool_call', name: 'generate_image', args: '{"prompt":"B"}', tool_call_id: 'tc-reused' },
      // A duplicate result from the rejected occurrence arrives after B has
      // opened. Its operation identity must still route it back to A.
      {
        type: 'tool_result', name: 'generate_image', tool_call_id: 'tc-reused',
        operation_id: 'tc-reused', outcome: 'cancelled', denied: true, error_code: 'approval_denied',
      },
      {
        type: 'generation_operation', tool_call_id: 'tc-reused', operation_id: 'op-b',
        operation_state: 'dispatch_attempted',
      },
      {
        type: 'artifact', tool_call_id: 'tc-reused', operation_id: 'op-b', artifact_id: 'artifact-b',
        artifact_storage_kind: 'managed', artifact_key: 'images/artifact-b.png', artifact_title: 'B',
        artifact_kind: 'image', artifact_media_type: 'image/png', artifact_size: 1024, artifact_revision: 1,
      },
      {
        type: 'generation_operation', tool_call_id: 'tc-reused', operation_id: 'op-b',
        operation_state: 'succeeded', artifact_ids: ['artifact-b'],
      },
      {
        type: 'tool_result', name: 'generate_image', tool_call_id: 'tc-reused', operation_id: 'op-b',
        outcome: 'succeeded', artifact_ids: ['artifact-b'],
      },
    ])

    expect(tools).toHaveLength(2)
    expect(tools[0]).toMatchObject({
      toolCallID: 'tc-reused', operationID: 'tc-reused', phase: 'terminal',
      outcome: 'cancelled', denied: true,
    })
    expect(tools[0]?.artifacts).toBeUndefined()
    expect(tools[1]).toMatchObject({
      toolCallID: 'tc-reused', operationID: 'op-b', phase: 'terminal', outcome: 'succeeded',
    })
    expect(tools[1]?.artifacts?.map((artifact) => artifact.id)).toEqual(['artifact-b'])
  })

  it('does not close a newer occurrence with a late result for the old operation', () => {
    const tools = imageTools([
      { type: 'tool_call', name: 'generate_image', args: '{}', tool_call_id: 'tc-reused' },
      {
        type: 'generation_operation', tool_call_id: 'tc-reused', operation_id: 'op-a',
        operation_state: 'dispatch_attempted',
      },
      { type: 'tool_call', name: 'generate_image', args: '{}', tool_call_id: 'tc-reused' },
      {
        type: 'tool_result', name: 'generate_image', tool_call_id: 'tc-reused', operation_id: 'op-a',
        outcome: 'failed', error_code: 'late-a',
      },
      {
        type: 'generation_operation', tool_call_id: 'tc-reused', operation_id: 'op-b',
        operation_state: 'failed', error_code: 'failed-b',
      },
      {
        type: 'tool_result', name: 'generate_image', tool_call_id: 'tc-reused', operation_id: 'op-b',
        outcome: 'failed', error_code: 'failed-b',
      },
    ])

    expect(tools).toHaveLength(2)
    expect(tools[0]?.operationID).toBe('op-a')
    expect(tools[1]).toMatchObject({ operationID: 'op-b', outcome: 'failed', errorCode: 'failed-b' })
  })

  it('binds live progress to the latest unbound occurrence, never a mismatched terminal card', () => {
    const replayed = replayTimeline([
      { type: 'tool_call', name: 'generate_image', args: '{}', tool_call_id: 'tc-reused' },
      {
        type: 'generation_operation', tool_call_id: 'tc-reused', operation_id: 'op-a',
        operation_state: 'failed', error_code: 'failed-a',
      },
      {
        type: 'tool_result', name: 'generate_image', tool_call_id: 'tc-reused', operation_id: 'op-a',
        outcome: 'failed', error_code: 'failed-a',
      },
      { type: 'tool_call', name: 'generate_image', args: '{}', tool_call_id: 'tc-reused' },
    ], true)
    store.dispatch(chatActions.setTimeline(replayed))

    expect(hasToolLifecycleHost(store.getState().chat.timeline, 'tc-reused', 'op-b', 'generate_image')).toBe(true)
    store.dispatch(chatActions.progressToolCall({
      name: 'generate_image', toolCallID: 'tc-reused', operationID: 'op-b', phase: 'generating',
    }))

    const tools = store.getState().chat.timeline.filter((item) => item.kind === 'tool')
    expect(tools[0]?.data).toMatchObject({ operationID: 'op-a', outcome: 'failed', errorCode: 'failed-a' })
    expect(tools[1]?.data).toMatchObject({ operationID: 'op-b', phase: 'generating', status: 'running' })
    expect(hasToolLifecycleHost(store.getState().chat.timeline, 'tc-reused', 'op-c', 'generate_image')).toBe(false)
  })
})
