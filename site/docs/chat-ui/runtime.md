---
title: Runtime
nav_order: 2
---

# ChatRuntime

Components never touch your store directly. They talk to a `ChatRuntime` — a host-agnostic data
source with the same shape as a Redux store (`getState` / `subscribe`) plus an action bag.

## The contract

```ts
interface ChatRuntime {
  getState: () => RuntimeState
  subscribe: (listener: () => void) => () => void
  readonly actions: RuntimeActions
}
```

### RuntimeState (read side)

```ts
interface RuntimeState {
  items: ThreadItem[]          // message | tool | approval, interleaved
  isRunning: boolean           // agent producing output?
  tokenSnapshot: TokenSnapshot | null
  goal: Goal | null
  todos: TodoItem[]
  queued: QueuedMessage[]      // type-ahead messages
}
```

### RuntimeActions (write side)

```ts
interface RuntimeActions {
  sendMessage(text, images?)
  enqueueMessage(text, images?)   // type-ahead while running
  removeQueuedMessage(id)
  stop()                          // cancel the in-flight turn
  resolveApproval(id, approved, approveAll?)
  submitAskUser(id, answers[])
  editMessage(id, newText)        // edit-and-resend a past user message
}
```

Full field reference: [API → Runtime](/chat-ui/docs/api/runtime) · [API → Types](/chat-ui/docs/api/types).

## Wrapping an external store

`createExternalStoreRuntime` adapts any Redux-shaped store. You provide a `select` function that
projects your full app state down to a (possibly partial) `RuntimeState` — missing slices default
to safe empties.

```ts
import { createExternalStoreRuntime } from 'jcode-ui'

const runtime = createExternalStoreRuntime({
  store,
  select: (s) => ({
    items: s.chat.timeline,
    isRunning: s.chat.isRunning,
    tokenSnapshot: s.chat.tokenInfo,
    goal: s.chat.goal,
    todos: s.chat.todos,
    queued: s.chat.queued,
  }),
  actions: {
    sendMessage: (text, images) => store.dispatch(sendMessage({ text, images })),
    stop: () => store.dispatch(stopAgent()),
    resolveApproval: (id, approved, approveAll) =>
      store.dispatch(resolveApproval({ id, approved, approveAll })),
    submitAskUser: (id, answers) => store.dispatch(submitAskUser({ id, answers })),
    editMessage: (id, text) => store.dispatch(editAndResend({ id, text })),
    enqueueMessage: (text, images) => store.dispatch(enqueueMessage({ text, images })),
    removeQueuedMessage: (id) => store.dispatch(removeQueued({ id })),
  },
})
```

**Options**

| Option | Type | Notes |
|--------|------|-------|
| `store` | `{ getState, subscribe }` | Redux / Zustand / custom |
| `select` | `(state) => PartialRuntimeState` | Project host → runtime |
| `actions` | `RuntimeActions` | Keep identity stable (memoize) |

Snapshot caching: `getState()` returns a **stable reference** between host dispatches so
`useSyncExternalStore` does not loop.

## Subscribing in components

Under a `<RuntimeProvider>`, three hooks read state:

```ts
const items   = useRuntimeSelector((s) => s.items)  // granular re-renders
const state   = useRuntimeState()                     // full state
const actions = useRuntimeActions()                   // stable action bag
```

Prefer `useRuntimeSelector` for hot paths (timeline length, `isRunning`).

## Mock runtime

For docs, tests, and demos, `createMockRuntime` gives you a scriptable runtime with no backend:

```ts
const rt = createMockRuntime({
  items: [],
  state: { tokenSnapshot: { total_tokens: 1000, prompt_tokens: 800, completion_tokens: 200, model_context_limit: 128000 } },
})

rt.push({ kind: 'message', seq: 1, data: { id: 'm1', role: 'user', content: 'hi', timestamp: 0 } })
rt.appendText('Hello!')   // streams into the last assistant message
rt.setRunning(true)
rt.patchState({ goal: { objective: 'Ship the UI', status: 'active' } })
```

### Mutators

| Method | Purpose |
|--------|---------|
| `setItems(items)` | Replace timeline |
| `push(item)` | Append one item |
| `setRunning(bool)` | Toggle agent-running |
| `appendText(delta)` | Stream into last assistant message |
| `patchState(partial)` | Merge token/goal/todos/queued/… |
| `calls` | Recorded action invocations (tests) |

Default `resolveApproval` / `submitAskUser` handlers update items in-place so interactive demos work without a backend.

This powers the [live demo](/chat-ui) and every docs preview on this site.

## Providers

```tsx
<RuntimeProvider runtime={runtime}>
  <ToolRegistryProvider registry={createDefaultToolRegistry()}>
    <Thread />
    <ChatInput />
  </ToolRegistryProvider>
</RuntimeProvider>
```

Optional: `<ApiBaseProvider apiBase={…}>` so `browser_screenshot` can resolve image URLs.

## Related

- [External store guide](/chat-ui/docs/guides/external-store)  
- [API → Runtime](/chat-ui/docs/api/runtime)  
- [API → Hooks](/chat-ui/docs/api/hooks)  
