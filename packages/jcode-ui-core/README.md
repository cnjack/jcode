# jcode-ui-core

The framework-agnostic core of [`jcode-ui`](../jcode-ui) — types, the `ChatRuntime` abstraction, and headless React primitives for AI chat interfaces.

Use this directly when you want the behavior (streaming, virtualization, tool dispatch, auto-follow) with your own styling layer. For the full styled experience, use `jcode-ui`.

## What's inside

- **`types`** — `Message`, `ToolCall`, `Approval`, `ThreadItem` (discriminated union), `TokenSnapshot`, `Goal`, `TodoItem`, `AskUserQuestion`/`Answer`.
- **`runtime`** — the `ChatRuntime` contract (`getState` / `subscribe` / `actions`), `createExternalStoreRuntime` (adapts any Redux-shaped store), `createMockRuntime` (scriptable, for demos/tests), `<RuntimeProvider>` + `useRuntimeState`/`useRuntimeSelector`/`useRuntimeActions` hooks.
- **`adapters`** — `ToolRendererRegistry`, the plugin seam for tool-call visualization.
- **`primitives`** — headless components: `Thread` (virtualized + auto-follow), `MessageView`, `Composer`, `ToolCallView`, `ApprovalBlock`, `AskUserBlock`.
- **`hooks`** — `useAutoScroll`, `useStreamFollow`, `useFocusOnIdle`.

## Install

```bash
pnpm add jcode-ui-core
```

React is an optional peer dependency — the non-React entries (`types`, `runtime` core, `adapters`) work in any TS project.

## Quick start (headless)

```tsx
import { RuntimeProvider, createExternalStoreRuntime } from 'jcode-ui-core/runtime'
import { Thread, MessageView, Composer } from 'jcode-ui-core/primitives'

const runtime = createExternalStoreRuntime({ store, select, actions })

;<RuntimeProvider runtime={runtime}>
  <Thread
    renderItem={(item) => {
      if (item.kind === 'message') return <MessageView message={item.data} />
      // …your tool/approval renderers
    }}
  />
  <Composer />
</RuntimeProvider>
```

## Runtime contract

```ts
interface ChatRuntime {
  getState: () => RuntimeState
  subscribe: (listener: () => void) => () => void
  readonly actions: RuntimeActions
}
```

`RuntimeState` carries `items` / `isRunning` / `tokenSnapshot` / `goal` / `todos` / `queued`. `RuntimeActions` exposes `sendMessage`, `enqueueMessage`, `stop`, `resolveApproval`, `submitAskUser`, `editMessage`, `removeQueuedMessage`. See the [runtime docs](https://www.j-code.net/chat-ui/docs/runtime).

## License

MIT
