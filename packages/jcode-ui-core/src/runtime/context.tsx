/**
 * React binding for ChatRuntime: a Context provider + hooks that subscribe via
 * `useSyncExternalStore` (the React 18 idiom for external stores — handles
 * tearing and concurrent rendering). Selector memoization is layered on top
 * with a ref cache so granular reads don't re-render on unrelated changes.
 *
 * Stability contract: the ChatRuntime's getState() MUST return a stable
 * reference between dispatches (the ExternalStoreRuntime + MockRuntime both
 * honor this by caching). Without it, useSyncExternalStore infinite-loops.
 */

import { createContext, useContext, useMemo, useRef, useSyncExternalStore } from 'react'
import type { ReactNode } from 'react'
import type { ChatRuntime, RuntimeState } from './index.js'

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
 */
export function useRuntimeState(): RuntimeState {
  const runtime = useRuntime()
  // getSnapshot returns the runtime's (stable) snapshot directly. subscribe is
  // a stable method reference.
  return useSyncExternalStore(runtime.subscribe, runtime.getState, runtime.getState)
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
  // Subscribe to the raw snapshot (stable identity), then memoize the selected
  // value in a ref so a stable selector returning a primitive doesn't trigger
  // spurious re-renders.
  const snapshot = useSyncExternalStore(runtime.subscribe, runtime.getState, runtime.getState)
  const cacheRef = useRef<{ snapshot: RuntimeState; value: T } | null>(null)
  const cache = cacheRef.current
  if (!cache || cache.snapshot !== snapshot) {
    const value = selector(snapshot)
    if (!cache || !isEqual(cache.value, value)) {
      cacheRef.current = { snapshot, value }
    } else {
      cacheRef.current = { snapshot, value: cache.value }
    }
  }
  return cacheRef.current!.value
}

/** Stable handle to the action bag. Identity is owned by the runtime. */
export function useRuntimeActions() {
  return useRuntime().actions
}

/** The raw runtime (rarely needed — prefer the state/actions hooks). */
export { useRuntime }
