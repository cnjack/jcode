---
title: Runtime API
parent: API Reference
nav_order: 2
---

# Runtime API

```ts
import {
  createExternalStoreRuntime,
  createMockRuntime,
  RuntimeProvider,
  normalizeState,
} from 'jcode-ui'
// or jcode-ui-core/runtime
```

## ChatRuntime

```ts
interface ChatRuntime {
  getState: () => RuntimeState
  subscribe: (listener: () => void) => () => void
  readonly actions: RuntimeActions
}
```

## RuntimeState

| Field | Type | Default (normalize) |
|-------|------|---------------------|
| `items` | `ThreadItem[]` | `[]` |
| `isRunning` | `boolean` | `false` |
| `tokenSnapshot` | `TokenSnapshot \| null` | `null` |
| `goal` | `Goal \| null` | `null` |
| `todos` | `TodoItem[]` | `[]` |
| `queued` | `QueuedMessage[]` | `[]` |

## RuntimeActions

| Action | Signature |
|--------|-----------|
| `sendMessage` | `(text: string, images?: ChatImage[]) => void` |
| `enqueueMessage` | `(text: string, images?: ChatImage[]) => void` |
| `removeQueuedMessage` | `(id: string) => void` |
| `stop` | `() => void` |
| `resolveApproval` | `(id: string, approved: boolean, approveAll?: boolean) => void` |
| `submitAskUser` | `(id: string, answers: AskUserAnswer[]) => void` |
| `editMessage` | `(id: string, newText: string) => void` |

## createExternalStoreRuntime

```ts
function createExternalStoreRuntime<THostState>(
  opts: ExternalStoreRuntimeOptions<THostState>,
): ChatRuntime

interface ExternalStoreRuntimeOptions<THostState> {
  store: {
    getState: () => THostState
    subscribe: (listener: () => void) => () => void
  }
  select: (state: THostState) => PartialRuntimeState
  actions: RuntimeActions
}
```

Returns a runtime whose `getState()` is **referentially stable** between host dispatches.

## createMockRuntime

```ts
function createMockRuntime(opts?: MockRuntimeOptions): ChatRuntime & {
  setItems(items: ThreadItem[]): void
  push(item: ThreadItem): void
  setRunning(running: boolean): void
  patchState(partial: PartialRuntimeState): void
  appendText(delta: string): void
  calls: { action: string; args: unknown[] }[]
}

interface MockRuntimeOptions {
  items?: ThreadItem[]
  isRunning?: boolean
  state?: PartialRuntimeState
  actions?: Partial<RuntimeActions>
}
```

Default `resolveApproval` / `submitAskUser` update items so demos are interactive.

## RuntimeProvider

```tsx
<RuntimeProvider runtime={runtime}>{children}</RuntimeProvider>
```

## normalizeState

```ts
function normalizeState(partial: PartialRuntimeState | undefined): RuntimeState
```

Fills missing slices with safe defaults.
