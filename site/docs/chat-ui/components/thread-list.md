---
title: ThreadList
parent: Components
nav_order: 16
---

# ThreadList

The session/thread-list sidebar — the collection analog of [`Thread`](/chat-ui/docs/components/thread). Renders grouped rows (Active + a collapsible Archived group) with title, relative time, a pulsing dot while running, and a hover ⋯ menu.

<div data-jcode-demo="thread-list" data-height="320"></div>

## Data source

`ThreadList` reads a **`ThreadStore`** — the sidebar's own external store, sibling to the conversation `ChatRuntime`. Provide it via `ThreadStoreProvider` (from `jcode-ui-core`); `createMockThreadStore` wires every action to real mutations for demos and tests.

```tsx
import { createMockThreadStore, ThreadStoreProvider } from 'jcode-ui-core'
import { ThreadList } from 'jcode-ui'
import 'jcode-ui/styles.css'

const store = createMockThreadStore({
  activeId: 'th1',
  threads: [
    { id: 'th1', title: 'Refactor auth middleware', updatedAt: Date.now(), status: 'running' },
    { id: 'th2', title: 'Fix flaky payment test', updatedAt: Date.now() - 6e5 },
    { id: 'th3', title: 'Draft the release notes', updatedAt: Date.now() - 5e6 },
    { id: 'th4', title: 'Investigate memory leak', updatedAt: Date.now() - 3e8, archived: true },
  ],
})

<ThreadStoreProvider store={store}>
  <ThreadList title="Sessions" />
</ThreadStoreProvider>
```

## Props

| Prop | Type | Notes |
|------|------|-------|
| `title` | `string` | Optional small header label above the list. |
| `className` | `string` | Extra class on the root. |

## Store contract

```ts
interface ThreadSummary {
  id: string
  title: string
  updatedAt: number             // ms epoch; drives relative time + default sort
  status?: 'idle' | 'running'   // 'running' shows a pulsing dot
  archived?: boolean            // rendered under the collapsible "Archived" group
  meta?: Record<string, unknown>
}

interface ThreadStoreActions {
  select?: (id: string) => void
  create?: () => void
  rename?: (id: string, title: string) => void
  archive?: (id: string) => void
  remove?: (id: string) => void
}
```

## Fail-visible

Every action is optional. Controls appear **only** for the actions you wire:

| Control | Shown when |
|---------|-----------|
| New thread | `actions.create` exists |
| Rename | `actions.rename` exists |
| Archive | `actions.archive` exists (and the row isn't already archived) |
| Delete | `actions.remove` exists |

A host that wires only `select` gets a clean read-only list with no dangling controls. The store's `getState()` must return a **stable reference** between changes (`useSyncExternalStore` loops otherwise) — the mock honors this.

## Related

- [Thread](/chat-ui/docs/components/thread) — the single-conversation view
