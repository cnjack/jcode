/**
 * MockRuntime — a self-contained, scriptable ChatRuntime for demos, docs, and
 * tests. No backend required: you feed it a "script" of items + a streaming
 * text fragment sequence, and it emits them on a timer. This is what powers the
 * website playground and visual-regression fixtures.
 *
 * It's read-only-ish: action callbacks are captured (so you can assert on them)
 * but don't drive scripted playback — the script is the source of truth.
 */

import type { ChatRuntime, RuntimeActions, RuntimeState } from './index.js'
import { normalizeState } from './index.js'
import type { ThreadItem } from '../types/index.js'

export interface MockRuntimeOptions {
  /** Initial items. */
  items?: ThreadItem[]
  /** Initial isRunning flag. */
  isRunning?: boolean
  /** Captured-action handlers (defaults are no-ops that record to `.calls`). */
  actions?: Partial<RuntimeActions>
}

/**
 * Create a ChatRuntime backed by an in-memory store with pub/sub. Exposes
 * imperative mutators (`setItems`, `push`, `appendText`, `setRunning`) so a
 * script driver (or a test) can evolve the state over time.
 */
export function createMockRuntime(opts: MockRuntimeOptions = {}): ChatRuntime & {
  /** Mutators for scripts/tests. */
  setItems: (items: ThreadItem[]) => void
  push: (item: ThreadItem) => void
  setRunning: (running: boolean) => void
  /** Append to the content of the last message item. */
  appendText: (delta: string) => void
  /** Recorded action calls (most-recent-last). */
  calls: { action: string; args: unknown[] }[]
} {
  let state: RuntimeState = normalizeState({
    items: opts.items,
    isRunning: opts.isRunning,
  })
  const listeners = new Set<() => void>()
  const calls: { action: string; args: unknown[] }[] = []

  function emit() {
    for (const l of listeners) l()
  }
  function setState(next: RuntimeState) {
    state = next
    emit()
  }

  let seq = state.items.reduce((m, i) => Math.max(m, i.seq), 0)
  const nextSeq = () => ++seq

  const noop = (action: string) => (...args: unknown[]) => {
    calls.push({ action, args })
  }

  const actions: RuntimeActions = {
    sendMessage: opts.actions?.sendMessage ?? noop('sendMessage'),
    enqueueMessage: opts.actions?.enqueueMessage ?? noop('enqueueMessage'),
    removeQueuedMessage: opts.actions?.removeQueuedMessage ?? noop('removeQueuedMessage'),
    stop: opts.actions?.stop ?? noop('stop'),
    resolveApproval: opts.actions?.resolveApproval ?? noop('resolveApproval'),
    submitAskUser: opts.actions?.submitAskUser ?? noop('submitAskUser'),
    editMessage: opts.actions?.editMessage ?? noop('editMessage'),
  }

  return {
    getState: () => state,
    subscribe: (l) => {
      listeners.add(l)
      return () => listeners.delete(l)
    },
    actions,
    setItems: (items) => setState({ ...state, items }),
    push: (item) => setState({ ...state, items: [...state.items, item] }),
    setRunning: (isRunning) => setState({ ...state, isRunning }),
    appendText: (delta) => {
      const items = [...state.items]
      for (let i = items.length - 1; i >= 0; i--) {
        const it = items[i]
        if (it.kind === 'message' && it.data.role === 'assistant') {
          items[i] = { ...it, data: { ...it.data, content: it.data.content + delta } }
          setState({ ...state, items })
          return
        }
      }
      // No assistant message yet — create one.
      const msg: ThreadItem = {
        kind: 'message',
        seq: nextSeq(),
        data: {
          id: `mock_${Date.now()}`,
          role: 'assistant',
          content: delta,
          timestamp: Date.now(),
        },
      }
      setState({ ...state, items: [...state.items, msg] })
    },
    calls,
  }
}
