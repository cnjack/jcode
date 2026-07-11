---
title: TaskList
parent: Components
nav_order: 15
---

# TaskList

A first-class task / todo list with a progress bar and per-status icons. Extracted from the `todowrite` renderer so hosts can drop it anywhere — run pages, automations, goal panels.

<div data-jcode-demo="tasklist" data-height="220"></div>

## Usage

`TaskList` is pure — pass it `items` directly. No runtime required.

```tsx
import { TaskList } from 'jcode-ui'
import 'jcode-ui/styles.css'

const items = [
  { id: 1, title: 'Read the current parser', status: 'completed' as const },
  { id: 2, title: 'Extract the tokenizer', status: 'in_progress' as const },
  { id: 3, title: 'Add table-driven tests', status: 'pending' as const },
  { id: 4, title: 'Delete the legacy shim', status: 'cancelled' as const },
]

<TaskList title="Ship the parser refactor" items={items} />
```

## Props

| Prop | Type | Default | Notes |
|------|------|---------|-------|
| `items` | `TodoItem[]` | — | Ordered task items. |
| `title` | `string` | — | Optional heading above the progress bar. |
| `compact` | `boolean` | `false` | Denser rows + smaller type for embedding in tool cards. |
| `hideProgress` | `boolean` | `false` | Hide the top progress bar. |
| `className` | `string` | — | Extra classes on the root. |

```ts
interface TodoItem {
  id: number
  title: string
  status: 'pending' | 'in_progress' | 'completed' | 'cancelled'
}
```

## Status icons

| Status | Icon |
|--------|------|
| `pending` | Hollow ring |
| `in_progress` | Half ring (spins; respects `prefers-reduced-motion`) |
| `completed` | Filled check (success) |
| `cancelled` | Strikethrough (muted) |

The top bar fills `completed / total` with `--jcode-accent-fill`.

## Compound + runtime variants

```tsx
// Single row (compound API)
<TaskList.Item item={item} compact />

// Bound to the runtime `todos` selector — renders nothing when the list is empty
import { RuntimeTaskList } from 'jcode-ui'
<RuntimeTaskList title="Plan" compact />
```

`RuntimeTaskList` accepts `title` / `compact` / `hideProgress` / `className` and reads items from `state.todos`.

## Related

- [ToolCallCard](/chat-ui/docs/components/tool-call-card) — the `todowrite` renderer uses `TaskList` internally
