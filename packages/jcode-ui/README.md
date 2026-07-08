# jcode-ui

React components for AI chat interfaces — streaming messages, tool calls, approvals, and ask-user interactions. Backend-agnostic, token-driven, and tree-shakeable. Powers [jcode](https://github.com/cnjack/jcode)'s Web + Desktop UIs.

[**Live demo**](https://www.j-code.net/chat-ui) · [**Docs**](https://www.j-code.net/docs/chat-ui)

## Why

Building an AI chat UI means re-implementing the same five pieces every time: a streaming message timeline, tool-call visualization, approval gates, an ask-user flow, and a composer. `jcode-ui` packages them as reusable, well-typed React components — with the hard parts (virtualization, auto-follow streaming, the "follow only when at bottom" contract, two-step approval arming) baked in.

## Two packages

| Package | What |
|---------|------|
| **`jcode-ui`** | Styled, token-driven components — the drop-in chat UI. |
| **`jcode-ui-core`** | Framework-agnostic core: types, the `ChatRuntime` abstraction, headless primitives. |

Use `jcode-ui` for the full styled experience, or `jcode-ui-core` if you want the behavior with your own styling layer.

## Install

```bash
pnpm add jcode-ui jcode-ui-core
# npm install jcode-ui jcode-ui-core
```

## Quick start

```tsx
import {
  RuntimeProvider,
  createExternalStoreRuntime,
  ToolRegistryProvider,
  createDefaultToolRegistry,
  Thread,
  ChatInput,
} from 'jcode-ui'
import 'jcode-ui/styles.css'

const registry = createDefaultToolRegistry()

// Wrap any Redux-shaped store. `select` projects your state → RuntimeState.
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
  actions: { sendMessage, stop, resolveApproval, submitAskUser, editMessage, /* … */ },
})

export function App() {
  return (
    <RuntimeProvider runtime={runtime}>
      <ToolRegistryProvider registry={registry}>
        <Thread />
        <ChatInput />
      </ToolRegistryProvider>
    </RuntimeProvider>
  )
}
```

That's the whole API surface for the common case. The runtime owns the data; the components own the rendering. Swap the runtime for a mock, a replayed session, or a different state manager without touching the UI.

## What renders

`<Thread>` renders a discriminated-union timeline of three item kinds:

- **`message`** — user / assistant / system bubbles, markdown-rendered with syntax highlighting (marked + highlight.js + DOMPurify).
- **`tool`** — expand/collapse cards dispatched to a registry of tool renderers (9 defaults: terminal, file-viewer, diff, search, todo, skill, team, browser-shot, generic).
- **`approval`** — interactive decision gates (allow once / allow all [armed] / deny).

`<ChatInput>` is the composer: autosizing textarea, send/queue/stop, slash commands, image attachments, context bar.

## Custom tool renderers

```tsx
import { createToolRendererRegistry } from 'jcode-ui-core/adapters'
import { GenericRenderer } from 'jcode-ui/tool-renderers'

const MyRenderer = ({ name, args, output }) => (
  <div><h4>{name}</h4><pre>{output}</pre></div>
)

const registry = createToolRendererRegistry()
registry.register('my_custom_tool', MyRenderer)
registry.setFallback(GenericRenderer)
```

See the [tool renderers docs](https://www.j-code.net/docs/chat-ui/tool-renderers).

## Theming

Every color/radius/shadow is a CSS custom property — no hardcoded hex. Re-theme by overriding tokens:

```css
:root { --color-primary: #6366f1; }
.dark { --color-primary: #818cf8; }
```

Light/dark via a `.dark` class on `<html>`. Generated themes (dracula, nord, …) work too. See the [theming docs](https://www.j-code.net/docs/chat-ui/theming).

## Packages

- [`jcode-ui-core`](../jcode-ui-core) — types, `ChatRuntime`, `ExternalStoreRuntime`, `MockRuntime`, `ToolRendererRegistry`, headless primitives (`Thread`, `MessageView`, `Composer`, `ToolCallView`, `ApprovalBlock`, `AskUserBlock`), behavioral hooks.
- `jcode-ui` — styled wrappers + 9 default tool renderers + markdown pipeline + token CSS.

## License

MIT
