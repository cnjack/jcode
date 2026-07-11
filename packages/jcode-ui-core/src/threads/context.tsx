/**
 * React binding for `ThreadStore`: a Context provider + hooks that subscribe via
 * `useSyncExternalStore` (the React 18 idiom for external stores). This mirrors
 * the runtime binding in `../runtime/context.tsx` exactly — the thread list is
 * just a second external store living alongside the conversation runtime.
 *
 * Stability contract: the store's `getState()` MUST return a stable reference
 * between changes (the mock honors this; real hosts must too), otherwise
 * `useSyncExternalStore` infinite-loops.
 */

import { createContext, useContext, useMemo, useSyncExternalStore } from 'react'
import type { ReactNode } from 'react'
import type { ThreadStore, ThreadListState, ThreadStoreActions } from './store.js'

const ThreadStoreContext = createContext<ThreadStore | null>(null)

export interface ThreadStoreProviderProps {
  store: ThreadStore
  children: ReactNode
}

/** Provide a `ThreadStore` to a subtree. `ThreadList` reads it via the hooks. */
export function ThreadStoreProvider({ store, children }: ThreadStoreProviderProps): ReactNode {
  const value = useMemo(() => store, [store])
  return <ThreadStoreContext.Provider value={value}>{children}</ThreadStoreContext.Provider>
}

function useThreadStore(): ThreadStore {
  const store = useContext(ThreadStoreContext)
  if (!store) {
    throw new Error(
      'jcode-ui: no ThreadStore provided. Wrap your list in <ThreadStoreProvider store={...}>.',
    )
  }
  return store
}

/** Subscribe to the full `ThreadListState`. Re-renders on any list change. */
export function useThreadListState(): ThreadListState {
  const store = useThreadStore()
  return useSyncExternalStore(store.subscribe, store.getState, store.getState)
}

/** Stable handle to the thread-list action bag (identity owned by the store). */
export function useThreadStoreActions(): ThreadStoreActions {
  return useThreadStore().actions
}

/** The raw store (rarely needed — prefer the state/actions hooks). */
export { useThreadStore }
