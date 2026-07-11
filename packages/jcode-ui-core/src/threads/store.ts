/**
 * ThreadStore — the host-agnostic data source for a session/thread *list*.
 *
 * This is the sidebar analog of `ChatRuntime`: where `ChatRuntime` drives a
 * single conversation, `ThreadStore` drives the collection of conversations
 * (jcode calls them sessions; the cloud console calls them runs). Both hosts
 * implement the same `getState`/`subscribe`/`actions` shape so the styled
 * `ThreadList` renders identically over Redux, Zustand, or a hand-rolled store.
 *
 * Fail-visible actions: every action is optional. A host that can't (or won't)
 * support renaming simply omits `actions.rename`, and the UI renders no rename
 * control — mirroring how `Message` only shows the edit affordance when
 * `canEdit` is set. Never call an action without guarding its presence.
 */

/** A single row in the thread list — a lightweight summary, not the full convo. */
export interface ThreadSummary {
  /** Stable id (used for React keys, selection, and action targeting). */
  id: string
  /** Display title. Hosts may lazily fill this from the first user message. */
  title: string
  /** Last-activity timestamp (ms epoch). Drives relative-time + default sort. */
  updatedAt: number
  /** Live status. `running` renders a pulsing status dot; default is idle. */
  status?: 'idle' | 'running'
  /** Soft-hidden: rendered under the collapsible "Archived" group. */
  archived?: boolean
  /** Free-form host metadata (project id, trigger kind, avatar tint, …). */
  meta?: Record<string, unknown>
}

/** The read-side state the `ThreadList` renders from. */
export interface ThreadListState {
  /** All threads (active + archived). The UI splits/sorts them for display. */
  threads: ThreadSummary[]
  /** The currently-open thread id, or undefined when none is selected. */
  activeId?: string
  /** True while the initial list is loading (drives a skeleton/spinner). */
  loading?: boolean
}

/**
 * Actions the `ThreadList` dispatches. All optional — the host wires only what
 * it supports, and the UI hides controls for the rest (fail-visible). Keeping
 * these a flat bag of callbacks (rather than a dispatched union) means a host
 * can hand us the functions it already has with zero adapter code.
 */
export interface ThreadStoreActions {
  /** Open/select a thread by id. */
  select?: (id: string) => void
  /** Create a new thread (host decides id + initial title). */
  create?: () => void
  /** Rename a thread. */
  rename?: (id: string, title: string) => void
  /** Archive (soft-hide) a thread. */
  archive?: (id: string) => void
  /** Permanently remove a thread. */
  remove?: (id: string) => void
}

/**
 * The contract every thread-list data source implements. `getState`/`subscribe`
 * match the Redux `Store` signature so an RTK store wraps with a thin selector.
 *
 * Stability contract: `getState()` MUST return a stable reference between
 * changes (React's `useSyncExternalStore` loops otherwise). The mock honors
 * this by only replacing the state object on an actual mutation.
 */
export interface ThreadStore {
  getState: () => ThreadListState
  subscribe: (listener: () => void) => () => void
  /** Action bag. Stable identity recommended (consumers may memoize). */
  readonly actions: ThreadStoreActions
}

/** Seed for `createMockThreadStore` — an array of threads, or a partial state. */
export type MockThreadStoreSeed = ThreadSummary[] | Partial<ThreadListState>

/** The mock store plus imperative test/demo hooks. */
export interface MockThreadStore extends ThreadStore {
  /** Replace the whole thread array. */
  setThreads: (threads: ThreadSummary[]) => void
  /** Merge a partial state patch (threads/activeId/loading). */
  patchState: (partial: Partial<ThreadListState>) => void
  /** Recorded action invocations, for test assertions. */
  calls: { action: string; args: unknown[] }[]
}

function normalizeSeed(seed: MockThreadStoreSeed | undefined): ThreadListState {
  if (Array.isArray(seed)) return { threads: seed }
  return {
    threads: seed?.threads ?? [],
    activeId: seed?.activeId,
    loading: seed?.loading,
  }
}

/**
 * Create an in-memory `ThreadStore` with every action wired to real mutations.
 * Ideal for demos, docs, Storybook, and tests — no host store required.
 *
 * @example
 *   const store = createMockThreadStore([
 *     { id: 'a', title: 'Refactor auth', updatedAt: Date.now(), status: 'running' },
 *   ])
 *   <ThreadStoreProvider store={store}><ThreadList /></ThreadStoreProvider>
 */
export function createMockThreadStore(seed?: MockThreadStoreSeed): MockThreadStore {
  let state: ThreadListState = normalizeSeed(seed)
  const listeners = new Set<() => void>()
  const calls: { action: string; args: unknown[] }[] = []
  let idSeq = 0

  function emit() {
    for (const l of listeners) l()
  }
  function replace(next: ThreadListState) {
    state = next
    emit()
  }
  function record(action: string, args: unknown[]) {
    calls.push({ action, args })
  }
  function newId(): string {
    return `t_${Date.now().toString(36)}_${++idSeq}`
  }

  const actions: ThreadStoreActions = {
    select: (id) => {
      record('select', [id])
      if (id === state.activeId) return
      replace({ ...state, activeId: id })
    },
    create: () => {
      const id = newId()
      record('create', [])
      const thread: ThreadSummary = {
        id,
        title: 'New thread',
        updatedAt: Date.now(),
        status: 'idle',
      }
      replace({ ...state, threads: [thread, ...state.threads], activeId: id })
    },
    rename: (id, title) => {
      record('rename', [id, title])
      replace({
        ...state,
        threads: state.threads.map((t) => (t.id === id ? { ...t, title } : t)),
      })
    },
    archive: (id) => {
      record('archive', [id])
      replace({
        ...state,
        threads: state.threads.map((t) => (t.id === id ? { ...t, archived: true } : t)),
      })
    },
    remove: (id) => {
      record('remove', [id])
      const threads = state.threads.filter((t) => t.id !== id)
      const activeId = state.activeId === id ? undefined : state.activeId
      replace({ ...state, threads, activeId })
    },
  }

  return {
    getState: () => state,
    subscribe: (l) => {
      listeners.add(l)
      return () => listeners.delete(l)
    },
    actions,
    setThreads: (threads) => replace({ ...state, threads }),
    patchState: (partial) => replace({ ...state, ...partial }),
    calls,
  }
}
