import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { ThreadItem } from 'jcode-ui-core'
import { chatActions, store } from './store'

beforeEach(() => {
  store.dispatch(chatActions.clearChat())
})
afterEach(() => {
  vi.restoreAllMocks()
  store.dispatch(chatActions.clearChat())
})

describe('chat agentDone turn duration', () => {
  it('measures from the latest user message instead of the final assistant first token', () => {
    const timeline: ThreadItem[] = [
      { kind: 'message', seq: 1, data: { id: 'user', role: 'user', content: 'do work', timestamp: 1_000 } },
      { kind: 'tool', seq: 2, data: { id: 'tool', name: 'execute', args: '{}', status: 'done', timestamp: 2_000 } },
      { kind: 'message', seq: 3, data: { id: 'final', role: 'assistant', content: 'done', timestamp: 9_000 } },
    ]
    store.dispatch(chatActions.setTimeline(timeline))
    vi.spyOn(Date, 'now').mockReturnValue(11_000)

    store.dispatch(chatActions.agentDone(undefined))

    const final = store.getState().chat.timeline.find(
      (item) => item.kind === 'message' && item.data.id === 'final',
    )
    expect(final?.kind).toBe('message')
    if (final?.kind === 'message') expect(final.data.durationMs).toBe(10_000)
  })

  it('does not rewrite the previous turn when the latest failed turn has no assistant reply', () => {
    const timeline: ThreadItem[] = [
      { kind: 'message', seq: 1, data: { id: 'old-user', role: 'user', content: 'first', timestamp: 1_000 } },
      { kind: 'message', seq: 2, data: { id: 'old-final', role: 'assistant', content: 'first done', timestamp: 2_000, durationMs: 1_000 } },
      { kind: 'message', seq: 3, data: { id: 'new-user', role: 'user', content: 'second', timestamp: 5_000 } },
      { kind: 'tool', seq: 4, data: { id: 'tool', name: 'execute', args: '{}', status: 'running', timestamp: 6_000 } },
    ]
    store.dispatch(chatActions.setTimeline(timeline))
    vi.spyOn(Date, 'now').mockReturnValue(12_000)

    store.dispatch(chatActions.agentDone({ error: 'failed' }))

    const oldFinal = store.getState().chat.timeline.find(
      (item) => item.kind === 'message' && item.data.id === 'old-final',
    )
    expect(oldFinal?.kind).toBe('message')
    if (oldFinal?.kind === 'message') expect(oldFinal.data.durationMs).toBe(1_000)
  })
})
