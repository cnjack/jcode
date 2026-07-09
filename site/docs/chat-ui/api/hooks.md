---
title: Hooks
parent: API Reference
nav_order: 3
---

# Hooks

## Runtime hooks (`jcode-ui` / `jcode-ui-core/runtime`)

Require `<RuntimeProvider>`.

| Hook | Returns | Notes |
|------|---------|-------|
| `useRuntimeState()` | `RuntimeState` | Re-renders on any state change |
| `useRuntimeSelector(selector)` | `T` | Granular; identity-stable selectors preferred |
| `useRuntimeActions()` | `RuntimeActions` | Action bag (stable if host memoizes) |

```ts
const items = useRuntimeSelector((s) => s.items)
const isRunning = useRuntimeSelector((s) => s.isRunning)
const { sendMessage, stop } = useRuntimeActions()
```

## Behavioral hooks (`jcode-ui-core/hooks`)

| Hook | Signature | Purpose |
|------|-----------|---------|
| `useAutoScroll` | `(threshold?: number)` | `{ ref, onScroll, scrollToBottom, getIsAtBottom, isAtBottomRef }` |
| `useIsAtBottom` | `(threshold?: number)` | Re-render-friendly at-bottom tracking |
| `useStreamFollow` | `(autoScroll, dep)` | Scroll on `dep` change only if at bottom |
| `useFocusOnIdle` | `(isRunning: boolean)` | Returns ref; focuses when turn ends |
| `useQueuedMessages` | `()` | Current type-ahead queue |

```ts
import { useAutoScroll, useStreamFollow } from 'jcode-ui-core/hooks'

const auto = useAutoScroll<HTMLDivElement>(80)
useStreamFollow(auto, items.length)
// <div ref={auto.ref} onScroll={auto.onScroll}>…
```

## Tool registry hooks (`jcode-ui`)

| Hook | Returns |
|------|---------|
| `useToolRegistry()` | `ToolRendererRegistry` from nearest provider |
