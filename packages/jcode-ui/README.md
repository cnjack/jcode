# jcode-ui

React components for AI chat interfaces — streaming messages, tool calls, approvals, and ask-user interactions. Backend-agnostic, token-driven, and tree-shakeable. Powers [jcode](https://github.com/cnjack/jcode)'s Web + Desktop UIs.

[**Live demo**](https://www.j-code.net/chat-ui) · [**Docs**](https://www.j-code.net/chat-ui/docs) · [**llms.txt**](https://www.j-code.net/llms.txt)

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

`<Thread>` renders a discriminated-union timeline:

- **`message`** — user / assistant / system, streaming-stable markdown (unclosed fences render cleanly, finished blocks are cached), code-block chrome with copy, branch picker (`‹ 2/3 ›`), regenerate, 👍👎 feedback, failed-turn retry.
- **`tool`** — expand/collapse cards dispatched to a registry of renderers (terminal with dual-channel stdout/stderr, diff, file-viewer, file-tree, test-results, stack-trace, search, todo, skill, team/subagent trees, browser-shot, generic).
- **`approval`** — decision gates: classic allow once / allow all (two-step armed) / deny, or arbitrary host-defined options (ACP-compatible).

Plus `ThreadWelcome` + `Suggestions` (empty state & follow-ups), `ConnectionBanner`, `ThreadList` (sidebar contract + UI), `ExportButton` (markdown download), `QuoteSelection`, `TaskList`, `Artifact`, `ModelSelector`.

`<ChatInput>` is the composer: autosizing textarea, send/queue/stop, slash commands, attachments (pluggable `AttachmentAdapter` with progress, drag & drop, paste-screenshot), optional dictation, `leadingControls`/`trailingControls`/`footer` slots, and a `ComposerHandle` ref (`insertText`/`focus`).

### Optional subentries

| Import | What |
|--------|------|
| `jcode-ui/canvas` (+`canvas.css`) | Agent workflow canvas on `@xyflow/react` — status-aware nodes, animated edges, `toolTreeToGraph` |
| `jcode-ui/voice` (+`voice.css`) | SpeechInput, Transcription, AudioPlayer, VoiceVisualizer — browser APIs only |
| `jcode-ui/plugins/mermaid` / `plugins/katex` | Diagram/math rendering via dynamic-import peers (zero cost unused) |
| `jcode-ui/product` | The jcode **product composer** (`ChatInput`, `WorkspacePicker`, `BranchPicker`, `GoalBanner`) — the exact desktop input experience, driven by a `ProductComposerHost` prop (state + actions + i18n strings + provider icons). Uses the product theme tokens (`--color-*`), not the scoped `--jcode-*` library tokens |
| `createAGUIRuntime` (core) | Drive everything from any AG-UI backend — LangGraph, CrewAI, Mastra, … |

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

See the [tool renderers docs](https://www.j-code.net/chat-ui/docs/tool-renderers).

## Theming

Every color/radius/shadow is a **scoped** CSS custom property (`--jcode-*` under `[data-jcode-ui]`) — no hardcoded hex, zero leakage into your page:

```css
[data-jcode-ui] { --jcode-color-primary: #6366f1; }
.dark [data-jcode-ui] { --jcode-color-primary: #818cf8; }
```

Light/dark via a `.dark` class on any ancestor. Three ways in:

- `jcode-ui/styles.css` — self-contained defaults, override on `[data-jcode-ui]`.
- `+ jcode-ui/compat.css` — keep theming via legacy unprefixed names (`--color-primary`, generated themes, 0.1 hosts).
- `+ jcode-ui/shadcn.css` — inherit your shadcn theme automatically.

See the [theming docs](https://www.j-code.net/chat-ui/docs/theming) and the [0.2 migration guide](https://www.j-code.net/chat-ui/docs/guides/migration-0.2).

## Packages

- [`jcode-ui-core`](../jcode-ui-core) — types, `ChatRuntime`, `ExternalStoreRuntime`, `MockRuntime`, `ToolRendererRegistry`, headless primitives (`Thread`, `MessageView`, `Composer`, `ToolCallView`, `ApprovalBlock`, `AskUserBlock`), behavioral hooks.
- `jcode-ui` — styled wrappers + 9 default tool renderers + markdown pipeline + token CSS.

## Examples

| App | Pattern |
|-----|---------|
| [`examples/jcode-ui-minimal`](../../examples/jcode-ui-minimal) | Mock runtime, no backend |
| [`examples/jcode-ui-zustand`](../../examples/jcode-ui-zustand) | External store + Zustand |

```bash
cd packages/jcode-ui-core && pnpm build && cd ../jcode-ui && pnpm build
cd ../../examples/jcode-ui-minimal && pnpm install && pnpm dev
```

## License

MIT
