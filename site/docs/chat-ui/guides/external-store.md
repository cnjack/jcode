---
title: External store
parent: Guides
nav_order: 1
---

# External store

jcode-ui is **bring-your-own-state**. The host owns the conversation; the library only needs a `ChatRuntime`.

## Pattern

1. Keep timeline + flags in your store (RTK, Zustand, Valtio, …).  
2. Project → `PartialRuntimeState` with `select`.  
3. Bind UI actions to dispatches / API calls.  
4. Wrap with `createExternalStoreRuntime` + `RuntimeProvider`.

```tsx
import { createExternalStoreRuntime, RuntimeProvider, Thread, ChatInput } from 'jcode-ui'
import 'jcode-ui/styles.css'

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
    sendMessage: (text, images) => {
      // 1) optimistic user message into timeline
      // 2) POST / stream agent events → append assistant / tools / approvals
    },
    enqueueMessage: (text, images) => { /* type-ahead queue */ },
    removeQueuedMessage: (id) => { /* … */ },
    stop: () => { /* abort controller / cancel run */ },
    resolveApproval: (id, approved, approveAll) => { /* POST decision */ },
    submitAskUser: (id, answers) => { /* POST answers */ },
    editMessage: (id, newText) => { /* truncate timeline + resend */ },
  },
})

export function Chat() {
  return (
    <RuntimeProvider runtime={runtime}>
      <Thread />
      <ChatInput />
    </RuntimeProvider>
  )
}
```

## Timeline shape

Your `items` array must be `ThreadItem[]` — interleaved messages, tools, and approvals with monotonic `seq` keys:

```ts
let seq = 0
function pushMessage(msg: Message): ThreadItem {
  return { kind: 'message', data: msg, seq: ++seq }
}
```

Streaming: mutate the last assistant message's `content` (or replace the item immutably) and let the runtime notify subscribers. Do **not** reassign `seq` on the same logical row.

## Zustand example

```ts
import { create } from 'zustand'
import { createExternalStoreRuntime } from 'jcode-ui'

const useChat = create((set, get) => ({
  items: [] as ThreadItem[],
  isRunning: false,
  // …
}))

// Zustand's API is getState/subscribe compatible:
const runtime = createExternalStoreRuntime({
  store: useChat,
  select: (s) => ({ items: s.items, isRunning: s.isRunning }),
  actions: { /* … */ },
})
```

## Checklist

- [ ] `actions` object identity is stable (module-level or `useMemo`)  
- [ ] `seq` never recycled for an in-place update  
- [ ] `isRunning` true for the whole agent turn (drives stop button + pending row)  
- [ ] Approvals and ask_user update the matching timeline item when resolved  
