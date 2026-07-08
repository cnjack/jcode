---
title: jcode-ui
parent: Docs
nav_order: 10
has_children: true
---

# jcode-ui — React AI Chat Components

`jcode-ui` is an open-source React component library for building AI chat interfaces — streaming
messages, tool calls, approvals, and ask-user interactions. It powers jcode's own Web + Desktop UIs
and is published as a standalone package so you can drop the same components into any agent or
copilot.

## Two packages

| Package | What it is |
|---------|------------|
| `jcode-ui` | Styled, token-driven components — the drop-in chat UI. |
| `jcode-ui-core` | Framework-agnostic core: types, the `ChatRuntime` abstraction, and headless primitives. |

Use `jcode-ui` for the full styled experience, or reach for `jcode-ui-core` if you want the behavior
(streaming, virtualization, tool dispatch) with your own styling layer.

## Install

```bash
pnpm add jcode-ui jcode-ui-core
# or: npm install jcode-ui jcode-ui-core
```

## Quick start

```tsx
import { RuntimeProvider, createExternalStoreRuntime, Thread, ChatInput } from 'jcode-ui'
import 'jcode-ui/styles.css'

const runtime = createExternalStoreRuntime({
  store,                  // your Redux / Zustand store
  select: (s) => ({       // project host state → RuntimeState
    items: s.chat.timeline,
    isRunning: s.chat.isRunning,
    tokenSnapshot: s.chat.tokenInfo,
    // goal, todos, queued …
  }),
  actions: { sendMessage, stop, resolveApproval, /* … */ },
})

export function App() {
  return (
    <RuntimeProvider runtime={runtime}>
      <Thread />
      <ChatInput />
    </RuntimeProvider>
  )
}
```

That's it — the runtime owns the data, the components own the rendering. Swap the runtime for a
mock, a replayed session, or a different state manager without touching the UI.

## What renders

`Thread` renders a discriminated-union timeline of three item kinds:

- **`message`** — user / assistant / system bubbles, markdown-rendered with syntax highlighting.
- **`tool`** — expand/collapse cards dispatched to a registry of tool renderers.
- **`approval`** — interactive decision gates (allow once / allow all / deny).

On top of that, `ChatInput` is the composer (autosizing textarea, send/queue/stop, slash commands,
image attachments, context bar).

## Next

- [Runtime](./runtime) — the `ChatRuntime` contract and `ExternalStoreRuntime`.
- [Primitives](./primitives) — the headless components under the styled layer.
- [Tool renderers](./tool-renderers) — the registry + writing your own.
- [Theming](./theming) — CSS custom properties and dark mode.
