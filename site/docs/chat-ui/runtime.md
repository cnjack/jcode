---
title: Runtime
parent: jcode-ui
nav_order: 1
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

`RuntimeState` is the read side:

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

`RuntimeActions` is the write side — callbacks the UI dispatches:

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

## Wrapping an external store

`createExternalStoreRuntime` adapts any Redux-shaped store. You provide a `select` function that
projects your full app state down to a (possibly partial) `RuntimeState` — missing slices default
to safe empties.

```ts
import { configureStore } from '@reduxjs/toolkit'
import { createExternalStoreRuntime } from 'jcode-ui'

const store = configureStore({ /* your reducers */ })

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

The action bag's identity should be stable (memoize it) — the components don't depend on it changing.

## Subscribing in components

Under a `<RuntimeProvider>`, three hooks read state:

```ts
const items      = useRuntimeSelector((s) => s.items)       // granular; re-renders only on items change
const state      = useRuntimeState()                        // full state; re-renders on any change
const actions    = useRuntimeActions()                      // stable action bag
```

`useRuntimeSelector` is the preferred read — it memoizes via a ref cache so a stable selector
returning a primitive won't trigger spurious re-renders.

## Mock runtime

For docs, tests, and demos, `createMockRuntime` gives you a scriptable runtime with no backend:

```ts
const rt = createMockRuntime()
rt.push({ kind: 'message', seq: 1, data: { id: 'm1', role: 'user', content: 'hi', timestamp: 0 } })
rt.appendText('Hello!')   // streams into the last assistant message
rt.setRunning(true)
```

This is exactly what powers the [live demo on the chat-ui page](/chat-ui).
