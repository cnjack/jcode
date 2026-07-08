/**
 * React binding for ChatRuntime: a Context provider + hooks that subscribe via
 * `useSyncExternalStore` (the React 18 idiom for external stores — handles
 * tearing and concurrent rendering). Selector memoization is layered on top
 * with a ref cache so granular reads don't re-render on unrelated changes.
 */

import { createContext, useContext, useMemo, useRef, useSyncExternalStore } from 'react'
import type { ReactNode } from 'react'
import type { ChatRuntime, RuntimeState } from './index.js'
import { normalizeState } from './index.js'

const RuntimeContext = createContext<ChatRuntime | null>(null)

export interface RuntimeProviderProps {
  runtime: ChatRuntime
  children: ReactNode
}

/** Provide a ChatRuntime to a subtree. Components under it read state/actions
 *  via `useRuntimeState` / `useRuntimeSelector` / `useRuntimeActions`. */
export function RuntimeProvider({ runtime, children }: RuntimeProviderProps): ReactNode {
  const value = useMemo(() => runtime, [runtime])
  return <RuntimeContext.Provider value={value}>{children}</RuntimeContext.Provider>
}

function useRuntime(): ChatRuntime {
  const runtime = useContext(RuntimeContext)
  if (!runtime) {
    throw new Error(
      'jcode-ui: no ChatRuntime provided. Wrap your UI in <RuntimeProvider runtime={...}>.',
    )
  }
  return runtime
}

/**
 * Subscribe to the full RuntimeState. Re-renders on any store change. Prefer
 * `useRuntimeSelector` for granular reads to avoid re-rendering on unrelated
 * state changes.
 *
 * Implementation note: we subscribe with `useSyncExternalStore` (tear-free) and
 * cache the selected value in a ref keyed on the store snapshot, so a stable
 * selector returning a primitive doesn't trigger spurious re-renders.
 */
export function useRuntimeState(): RuntimeState {
  const runtime = useRuntime()
  return useRuntimeSelectorInternal(
    runtime,
    (s) => s,
    Object.is,
  )
}

/**
 * Subscribe to a derived slice of RuntimeState. The selector MUST be stable
 * (memoize with useCallback) or return a primitive; otherwise React will
 * re-render on every store change. For object returns, also pass an `isEqual`
 * (e.g. shallow-equal) to avoid identity churn.
 */
export function useRuntimeSelector<T>(
  selector: (state: RuntimeState) => T,
  isEqual: (a: T, b: T) => boolean = Object.is,
): T {
  const runtime = useRuntime()
  return useRuntimeSelectorInternal(runtime, selector, isEqual)
}

/** Stable handle to the action bag. Identity is owned by the runtime. */
export function useRuntimeActions() {
  return useRuntime().actions
}

/** The raw runtime (rarely needed — prefer the state/actions hooks). */
export { useRuntime }

// --- internal selector+cache implementation --------------------------------
//
// useSyncExternalStore guarantees we re-read getSnapshot after every notify,
// but it does no selection. We layer a ref cache on top: on each store snapshot
// change we re-run the selector and only bust the cache when the selected value
// actually differs (per isEqual). This keeps re-renders proportional to real
// changes, not to notify count.
function useRuntimeSelectorInternal<T>(
  runtime: ChatRuntime,
  selector: (state: RuntimeState) => T,
  isEqual: (a: T, b: T) => boolean,
): T {
  const cacheRef = useRef<{ snapshot: RuntimeState; value: T } | null>(null)
  const subscribe = runtime.subscribe
  const getSnapshot = () => normalizeState(runtime.getState())
  // Prime the cache on first read / after a store change useSyncExternalStore detected.
  const snapshot = useSyncExternalStore(subscribe, getSnapshot, getSnapshot)
  const cache = cacheRef.current
  if (!cache || cache.snapshot !== snapshot) {
    const value = selector(snapshot)
    if (!cache || !isEqual(cache.value, value)) {
      cacheRef.current = { snapshot, value }
    } else {
      // keep old value identity, just refresh the snapshot stamp
      cacheRef.current = { snapshot, value: cache.value }
    }
  }
  return cacheRef.current!.value
}
