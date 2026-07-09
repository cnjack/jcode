---
title: Installation
nav_order: 1
---

# Installation

Get `jcode-ui` rendering a live chat thread in about five minutes. No backend required for the first pass — use `createMockRuntime` for local demos, then swap in your store.

## Requirements

| Dependency | Version |
|------------|---------|
| React | `^18` or `^19` |
| React DOM | `^18` or `^19` |
| Bundler | Vite, Next.js, or any ESM-capable bundler |

`jcode-ui` ships prebuilt CSS (Tailwind utilities are compiled in). You do **not** need to configure Tailwind in the host app unless you want to extend tokens.

## 1. Install packages

```bash
pnpm add jcode-ui jcode-ui-core
# npm install jcode-ui jcode-ui-core
# yarn add jcode-ui jcode-ui-core
```

Both packages are required for the styled surface (`jcode-ui` depends on `jcode-ui-core`). Tree-shakeable subpaths:

| Import | Purpose |
|--------|---------|
| `jcode-ui` | Styled components + runtime re-exports |
| `jcode-ui/styles.css` | Required stylesheet |
| `jcode-ui/tool-renderers` | Individual tool renderers |
| `jcode-ui-core` | Types + everything |
| `jcode-ui-core/runtime` | Runtime only |
| `jcode-ui-core/primitives` | Headless primitives |
| `jcode-ui-core/adapters` | Tool registry |
| `jcode-ui-core/hooks` | Behavioral hooks |

## 2. Import styles

```tsx
import 'jcode-ui/styles.css'
```

Put this once at your app root (or the route that mounts the chat UI). The file includes:

1. Compiled Tailwind utilities used by components  
2. Base design tokens (`:root` light + `.dark` dark)  
3. Animations (tool shimmer)  
4. Component-local styles (code blocks, diff tables)

### Dark mode

Add the `.dark` class on `<html>` (or a wrapping ancestor):

```ts
document.documentElement.classList.toggle('dark')
```

## 3. Minimal app (mock runtime)

```tsx
import {
  RuntimeProvider,
  ToolRegistryProvider,
  createDefaultToolRegistry,
  createMockRuntime,
  Thread,
  ChatInput,
} from 'jcode-ui'
import 'jcode-ui/styles.css'

const runtime = createMockRuntime()
const registry = createDefaultToolRegistry()

// Seed a couple of items so the thread isn't empty:
runtime.push({
  kind: 'message',
  seq: 1,
  data: {
    id: 'm1',
    role: 'user',
    content: 'Hello jcode-ui',
    timestamp: Date.now(),
  },
})
runtime.push({
  kind: 'message',
  seq: 2,
  data: {
    id: 'm2',
    role: 'assistant',
    content: 'Hi — components are live.',
    timestamp: Date.now(),
  },
})

export function App() {
  return (
    <div style={{ height: '100vh', display: 'flex', flexDirection: 'column' }}>
      <RuntimeProvider runtime={runtime}>
        <ToolRegistryProvider registry={registry}>
          <div style={{ flex: 1, minHeight: 0 }}>
            <Thread />
          </div>
          <ChatInput />
        </ToolRegistryProvider>
      </RuntimeProvider>
    </div>
  )
}
```

<div data-jcode-demo="thread" data-height="360"></div>

## 4. Wire your own store (production path)

jcode-ui never owns the conversation. Wrap any Redux-shaped store:

```tsx
import { createExternalStoreRuntime, RuntimeProvider, Thread, ChatInput } from 'jcode-ui'

const runtime = createExternalStoreRuntime({
  store, // { getState, subscribe }
  select: (s) => ({
    items: s.chat.timeline,
    isRunning: s.chat.isRunning,
    tokenSnapshot: s.chat.tokenInfo,
    goal: s.chat.goal,
    todos: s.chat.todos,
    queued: s.chat.queued,
  }),
  actions: {
    sendMessage: (text, images) => dispatch(sendMessage({ text, images })),
    enqueueMessage: (text, images) => dispatch(enqueue({ text, images })),
    removeQueuedMessage: (id) => dispatch(removeQueued(id)),
    stop: () => dispatch(stopAgent()),
    resolveApproval: (id, approved, approveAll) =>
      dispatch(resolveApproval({ id, approved, approveAll })),
    submitAskUser: (id, answers) => dispatch(submitAskUser({ id, answers })),
    editMessage: (id, text) => dispatch(editAndResend({ id, text })),
  },
})
```

Full recipe: [External store guide](/chat-ui/docs/guides/external-store).

## Framework notes

### Vite

```tsx
// main.tsx
import 'jcode-ui/styles.css'
import { createRoot } from 'react-dom/client'
import { App } from './App'

createRoot(document.getElementById('root')!).render(<App />)
```

No extra Vite plugins required.

### Next.js (App Router)

Import styles in the root layout and render chat UI in a **client** component:

```tsx
// app/layout.tsx
import 'jcode-ui/styles.css'

// app/chat/page.tsx
'use client'
import { Chat } from './Chat'
export default function Page() {
  return <Chat />
}
```

If you see hydration warnings around virtualization, disable it for SSR-first paints (`<Thread virtualize={false} />`) or only mount the thread after `useEffect`.

## TypeScript

Types ship with the packages (`"types"` export). No `@types/*` package is needed beyond React.

## Next steps

- [Runtime](/chat-ui/docs/runtime) — state contract in depth  
- [Components](/chat-ui/docs/components) — every styled component with live previews  
- [Custom tool renderers](/chat-ui/docs/guides/custom-tool-renderer) — render your agent tools  
- [API Reference](/chat-ui/docs/api) — full type surface  
