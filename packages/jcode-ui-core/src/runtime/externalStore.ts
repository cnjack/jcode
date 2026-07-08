/**
 * ExternalStoreRuntime — wrap any Redux-shaped external store as a ChatRuntime.
 *
 * The host store holds the *full* app state (e.g. an RTK root state with many
 * slices). We select just the `RuntimeState` slice out of it via a provided
 * selector, and bind the action callbacks. The resulting `ChatRuntime` is what
 * jcode-ui components consume.
 *
 * Example (RTK):
 *   const runtime = createExternalStoreRuntime({
 *     store,
 *     select: (s) => ({
 *       items: s.chat.timeline,
 *       isRunning: s.chat.isRunning,
 *       tokenSnapshot: s.chat.tokenInfo,
 *       goal: s.chat.goal,
 *       todos: s.chat.todos,
 *       queued: s.chat.queued,
 *     }),
 *     actions: {
 *       sendMessage: (t, imgs) => store.dispatch(sendMessage({ text: t, images: imgs })),
 *       stop: () => store.dispatch(stopAgent()),
 *       resolveApproval: (id, ok, all) => store.dispatch(resolveApproval({ id, approved: ok, approveAll: all })),
 *       // ...
 *     },
 *   })
 */

import type { ChatRuntime, PartialRuntimeState, RuntimeActions } from './index.js'
import { normalizeState } from './index.js'

export interface ExternalStoreRuntimeOptions<THostState> {
  /** The external store. Must expose Redux-compatible getState/subscribe. */
  store: {
    getState: () => THostState
    subscribe: (listener: () => void) => () => void
  }
  /** Project the host state down to a (possibly partial) RuntimeState. */
  select: (state: THostState) => PartialRuntimeState
  /** Action bag. Identity should be stable across renders. */
  actions: RuntimeActions
}

/**
 * Wrap an external store as a ChatRuntime. The returned object's `getState`
 * always returns a fully-populated RuntimeState (missing slices defaulted),
 * so downstream selectors never see undefined.
 */
export function createExternalStoreRuntime<THostState>(
  opts: ExternalStoreRuntimeOptions<THostState>,
): ChatRuntime {
  const { store, select, actions } = opts
  return {
    getState: () => normalizeState(select(store.getState())),
    subscribe: (listener) => store.subscribe(listener),
    actions,
  }
}
