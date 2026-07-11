/**
 * threads — the session/thread-*list* seam (sidebar), sibling to `runtime`
 * (which drives a single conversation).
 *
 *   - store   : ThreadSummary / ThreadListState / ThreadStore contract +
 *               createMockThreadStore
 *   - context : <ThreadStoreProvider> + useThreadListState / useThreadStoreActions
 */

export * from './store.js'
export * from './context.js'
