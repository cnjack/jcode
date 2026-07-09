/**
 * ChatRuntime — the host-agnostic data source for jcode-ui.
 *
 * Components never touch the store directly. They talk to a `ChatRuntime`, which
 * is an `ExternalStore`-shaped interface (matching Redux's store signature) so
 * it can wrap RTK, Zustand, Pinia-via-snapshot, or a hand-rolled reducer with
 * zero adapter code.
 *
 * Why this abstraction exists: jcode-ui must render the same whether the data
 * comes from a live WebSocket-backed RTK store, a replayed JSONL session, or a
 * mock playground. The runtime is the single seam.
 */

import type { ThreadItem, TokenSnapshot, Goal, TodoItem, QueuedMessage } from '../types/index.js'

/**
 * The read-side state a Thread + Composer render from. Consumers provide an
 * object of this shape (or a subset — see `PartialRuntimeState`); the runtime
 * normalizes missing slices to safe defaults.
 */
export interface RuntimeState {
  /** The conversation timeline (messages, tool calls, approvals, interleaved). */
  items: ThreadItem[]
  /** True while the agent is producing output (drives the "Thinking…" row, the
   *  composer's send→stop button swap, and auto-follow behavior). */
  isRunning: boolean
  /** Live token/context snapshot, or null when no turn has reported usage yet. */
  tokenSnapshot: TokenSnapshot | null
  /** Active goal banner, or null. */
  goal: Goal | null
  /** Todo list (also rendered inside `todowrite` tool calls). */
  todos: TodoItem[]
  /** Type-ahead queue: messages composed mid-turn, drained on each turn end. */
  queued: QueuedMessage[]
}

/**
 * Actions the UI dispatches. The runtime forwards these to the host store
 * (which may in turn POST to a backend, resolve locally, etc.). Keeping these
 * as a flat bag of callbacks (rather than a discriminated union dispatched via
 * `runtime.dispatch`) means consumers wiring a Zustand store or plain React
 * state don't have to model their actions in our shape — they just hand us the
 * functions they already have.
 */
export interface RuntimeActions {
  /** Send a user-authored message. `images` are base64 payloads. */
  sendMessage: (text: string, images?: { data: string; media_type: string }[]) => void
  /** Enqueue a message while a turn is running (type-ahead). */
  enqueueMessage: (text: string, images?: { data: string; media_type: string }[]) => void
  /** Remove a queued message by id (before it is sent). */
  removeQueuedMessage: (id: string) => void
  /** Cancel the in-flight turn. */
  stop: () => void
  /** Resolve an approval gate. `approveAll` arms "allow all future" semantics. */
  resolveApproval: (id: string, approved: boolean, approveAll?: boolean) => void
  /** Answer an `ask_user` batch. */
  submitAskUser: (id: string, answers: { question_header: string; answer: string; selected?: string[] }[]) => void
  /** Edit a past user message and resend from that point. */
  editMessage: (id: string, newText: string) => void
}

/**
 * The contract every jcode-ui data source implements. `getState`/`subscribe`
 * deliberately match the Redux `Store` signature so a real RTK store can be
 * wrapped with a thin selector + the actions bound.
 */
export interface ChatRuntime {
  getState: () => RuntimeState
  subscribe: (listener: () => void) => () => void
  /** Action bag. Stable identity is recommended (consumers should memoize). */
  readonly actions: RuntimeActions
}

/** A `RuntimeState` where every slice is optional; useful for adapters that
 *  only implement part of the contract (e.g. a read-only replay runtime). */
export type PartialRuntimeState = Partial<RuntimeState>

/** Merge a partial state onto the default empty state. Missing slices get
 *  safe defaults so components never have to null-check. */
export function normalizeState(partial: PartialRuntimeState | undefined): RuntimeState {
  return {
    items: partial?.items ?? [],
    isRunning: partial?.isRunning ?? false,
    tokenSnapshot: partial?.tokenSnapshot ?? null,
    goal: partial?.goal ?? null,
    todos: partial?.todos ?? [],
    queued: partial?.queued ?? [],
  }
}

// Re-export the concrete runtime implementations so `jcode-ui-core/runtime`
// is the single import surface for everything runtime-related.
export * from './externalStore.js'
export * from './mockRuntime.js'
export * from './context.js'
