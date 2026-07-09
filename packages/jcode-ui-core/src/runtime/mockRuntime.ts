/**
 * MockRuntime — a self-contained, scriptable ChatRuntime for demos, docs, and
 * tests. No backend required: you feed it a "script" of items + a streaming
 * text fragment sequence, and it emits them on a timer.
 */

import type { ChatRuntime, PartialRuntimeState, RuntimeActions, RuntimeState } from './index.js'
import { normalizeState } from './index.js'
import type { ThreadItem } from '../types/index.js'

export interface MockRuntimeOptions {
  /** Initial items. */
  items?: ThreadItem[]
  /** Initial isRunning flag. */
  isRunning?: boolean
  /** Initial full/partial runtime state (overrides items/isRunning when set). */
  state?: PartialRuntimeState
  /** Captured-action handlers (defaults are no-ops that record to `.calls`). */
  actions?: Partial<RuntimeActions>
}

function maxSeq(items: ThreadItem[]): number {
  let m = 0
  for (const i of items) {
    if (i.seq > m) m = i.seq
  }
  return m
}

/**
 * Create a ChatRuntime backed by an in-memory store with pub/sub. Exposes
 * imperative mutators (`setItems`, `push`, `appendText`, `setRunning`, `patchState`)
 * so a script driver (or a test / docs demo) can evolve the state over time.
 */
export function createMockRuntime(opts: MockRuntimeOptions = {}): ChatRuntime & {
  setItems: (items: ThreadItem[]) => void
  push: (item: ThreadItem) => void
  setRunning: (running: boolean) => void
  patchState: (partial: PartialRuntimeState) => void
  appendText: (delta: string) => void
  /** Allocate a fresh seq higher than any current item (and any previously seen). */
  nextSeq: () => number
  calls: { action: string; args: unknown[] }[]
} {
  let state: RuntimeState = normalizeState({
    items: opts.items,
    isRunning: opts.isRunning,
    ...opts.state,
  })
  const listeners = new Set<() => void>()
  const calls: { action: string; args: unknown[] }[] = []

  function emit() {
    for (const l of listeners) l()
  }
  function replaceState(next: RuntimeState) {
    state = next
    // Keep the seq counter ahead of anything in the timeline so push/appendText
    // never collide with caller-assigned seq values (e.g. demo scripts).
    const m = maxSeq(next.items)
    if (m > seq) seq = m
    emit()
  }

  // Monotonic counter for auto-allocated seqs. Always stays >= max item.seq.
  let seq = maxSeq(state.items)
  const nextSeq = () => ++seq

  const noop = (action: string) => (...args: unknown[]) => {
    calls.push({ action, args })
  }

  const actions: RuntimeActions = {
    sendMessage: opts.actions?.sendMessage ?? noop('sendMessage'),
    enqueueMessage: opts.actions?.enqueueMessage ?? noop('enqueueMessage'),
    removeQueuedMessage: opts.actions?.removeQueuedMessage ?? noop('removeQueuedMessage'),
    stop: opts.actions?.stop ?? noop('stop'),
    resolveApproval:
      opts.actions?.resolveApproval ??
      ((id, approved, approveAll) => {
        calls.push({ action: 'resolveApproval', args: [id, approved, approveAll] })
        replaceState({
          ...state,
          items: state.items.map((i) =>
            i.kind === 'approval' && i.data.id === id
              ? { ...i, data: { ...i.data, resolved: true, approved } }
              : i,
          ),
        })
      }),
    submitAskUser:
      opts.actions?.submitAskUser ??
      ((id, answers) => {
        calls.push({ action: 'submitAskUser', args: [id, answers] })
        replaceState({
          ...state,
          items: state.items.map((i) =>
            i.kind === 'tool' && (i.data.askUserId === id || i.data.id === id)
              ? {
                  ...i,
                  data: {
                    ...i.data,
                    status: 'done',
                    askUserId: undefined,
                    askUserQuestions: undefined,
                    output: JSON.stringify(answers),
                  },
                }
              : i,
          ),
        })
      }),
    editMessage: opts.actions?.editMessage ?? noop('editMessage'),
  }

  return {
    getState: () => state,
    subscribe: (l) => {
      listeners.add(l)
      return () => listeners.delete(l)
    },
    actions,
    setItems: (items) => replaceState({ ...state, items }),
    push: (item) => {
      // If the caller omitted a meaningful seq (0 / negative), assign one.
      const withSeq =
        item.seq > 0
          ? item
          : { ...item, seq: nextSeq() }
      if (withSeq.seq > seq) seq = withSeq.seq
      replaceState({ ...state, items: [...state.items, withSeq] })
    },
    setRunning: (isRunning) => replaceState({ ...state, isRunning }),
    patchState: (partial) => {
      const next = { ...state, ...partial }
      if (partial.items) {
        replaceState(next)
      } else {
        state = next
        emit()
      }
    },
    nextSeq,
    appendText: (delta) => {
      const items = [...state.items]
      for (let i = items.length - 1; i >= 0; i--) {
        const it = items[i]
        if (it.kind === 'message' && it.data.role === 'assistant') {
          items[i] = { ...it, data: { ...it.data, content: it.data.content + delta } }
          replaceState({ ...state, items })
          return
        }
      }
      // No assistant message yet — create one with a non-colliding seq.
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
      replaceState({ ...state, items: [...state.items, msg] })
    },
    calls,
  }
}
