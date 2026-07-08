/**
 * ExternalStoreRuntime — wrap any Redux-shaped external store as a ChatRuntime.
 *
 * The host store holds the *full* app state (e.g. an RTK root state with many
 * slices). We select just the `RuntimeState` slice out of it via a provided
 * selector, and bind the action callbacks. The resulting `ChatRuntime` is what
 * jcode-ui components consume.
 *
 * Snapshot caching: the host store's state reference changes only when a reducer
 * dispatches. We cache the normalized RuntimeState keyed on that raw reference,
 * so `getState()` returns a stable identity between dispatches — a hard
 * requirement for React's `useSyncExternalStore` (which loops otherwise).
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
 * always returns a fully-populated RuntimeState (missing slices defaulted), and
 * returns the SAME object reference between dispatches (so it's safe to pass to
 * useSyncExternalStore's getSnapshot).
 */
export function createExternalStoreRuntime<THostState>(
  opts: ExternalStoreRuntimeOptions<THostState>,
): ChatRuntime {
  const { store, select, actions } = opts

  // Snapshot cache: re-normalize only when the host state reference changes.
  let lastRaw: THostState | undefined
  let lastNorm = normalizeState(undefined)
  function getStateCached() {
    const raw = store.getState()
    if (raw !== lastRaw) {
      lastRaw = raw
      lastNorm = normalizeState(select(raw))
    }
    return lastNorm
  }

  return {
    getState: getStateCached,
    subscribe: (listener) => store.subscribe(listener),
    actions,
  }
}
