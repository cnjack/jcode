---
title: Thread
parent: Components
nav_order: 1
---

# Thread

The conversation container. Renders the timeline with virtualization and the "follow only when at bottom" streaming contract.

<div data-jcode-demo="thread" data-height="420"></div>

## Usage

```tsx
import { Thread } from 'jcode-ui'

<Thread
  virtualize
  overscanBottom={96}
  emptyState={<EmptyState />}
/>
```

Requires a parent `<RuntimeProvider>`. Tool cards need `<ToolRegistryProvider>` (or the default registry if you pass one higher up).

## Props

| Prop | Type | Default | Notes |
|------|------|---------|-------|
| `virtualize` | `boolean` | `true` | TanStack Virtual. Disable for short/replay timelines. |
| `overscanBottom` | `number` | `24` | Bottom padding (px) to clear a sticky composer. |
| `emptyState` | `ReactNode` | — | Shown when no items + not running. |
| `renderPending` | `() => ReactNode` | "Thinking…" | Trailing running indicator. |
| `className` | `string` | — | Passthrough on the scroll container. |

## What it renders

Timeline items are a discriminated union:

| `item.kind` | Component |
|-------------|-----------|
| `message` | `Message` (edit enabled for user messages while idle) |
| `tool` | `ToolCallCard` (indented) |
| `approval` | `ApprovalBanner` (indented) |

While `isRunning`, a pending row appears at the bottom.

## Empty state + suggestions

Pass your own welcome UI via `emptyState`. Suggestion chips are host-owned — wire them to `actions.sendMessage`:

```tsx
const empty = (
  <div className="p-8 text-center">
    <h2>How can I help?</h2>
    <button type="button" onClick={() => actions.sendMessage('Summarize this repo')}>
      Summarize this repo
    </button>
  </div>
)

<Thread emptyState={empty} />
```

## Headless alternative

For full control over item chrome, use `Thread` from `jcode-ui-core/primitives` with `renderItem`. See [Primitives](/chat-ui/docs/primitives).

## Related

- [ChatInput](/chat-ui/docs/components/chat-input)  
- [Message](/chat-ui/docs/components/message)  
- [Virtualization guide](/chat-ui/docs/guides/virtualization)  
