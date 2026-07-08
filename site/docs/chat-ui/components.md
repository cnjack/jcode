---
title: Components
parent: jcode-ui
nav_order: 5
---

# Component reference

Every styled component `jcode-ui` exports, with its props. This mirrors
assistant-ui's component catalog — if you're migrating from assistant-ui, the
table below maps their components to ours.

## assistant-ui → jcode-ui mapping

| assistant-ui | jcode-ui | Notes |
|--------------|----------|-------|
| `Thread` | `Thread` | Virtualized + auto-follow. Same concept. |
| `Composer` | `ChatInput` | Send/queue/stop, slash commands, attachments. |
| `Message` | `Message` (via `Thread`) | Markdown + role bubbles. |
| `Markdown` | (built into `Message`) | marked + highlight.js + DOMPurify. |
| `DiffViewer` | `DiffRenderer` (tool renderer) | Renders edit/multi_edit tool calls. |
| `ToolFallback` / `makeAssistantToolUI` | `ToolRendererRegistry` | Plugin registry pattern. |
| `ThreadConfig` `Context` | `RuntimeProvider` + `ChatRuntime` | The data-source seam. |
| `AssistantModal` / `AssistantSidebar` | `Sidebar` (product app) | In the product, not the library. |
| `ModelPicker` | `ProjectHeader` (product) | In the product app. |

---

## `<Thread />`

The conversation container. Renders the timeline with virtualization + the
"follow only when at bottom" streaming contract.

```tsx
import { Thread } from 'jcode-ui'

<Thread
  virtualize        // default true
  overscanBottom={96}  // px, clears a sticky composer
  emptyState={<EmptyState />}
/>
```

| Prop | Type | Default | Notes |
|------|------|---------|-------|
| `virtualize` | `boolean` | `true` | TanStack Virtual. Disable for short/replay timelines. |
| `overscanBottom` | `number` | — | Bottom padding (px) to clear a sticky composer. |
| `emptyState` | `ReactNode` | — | Shown when no items + not running. |
| `renderPending` | `() => ReactNode` | "Thinking…" | The trailing running indicator. |
| `className` | `string` | — | Passthrough. |

---

## `<ChatInput />`

The composer: autosizing textarea, send/queue/stop, slash-command palette,
image attachments, context bar.

```tsx
import { ChatInput } from 'jcode-ui'

<ChatInput
  slashCommands={[{ slash: '/goal', description: 'set the session goal' }]}
  allowImages={modelSupportsVision}
  showContextBar
  onSent={() => timelineSnapToBottom()}
/>
```

| Prop | Type | Default | Notes |
|------|------|---------|-------|
| `slashCommands` | `SlashCommand[]` | — | Drives the `/` menu. |
| `allowImages` | `boolean` | `false` | Gate by model vision support. |
| `placeholder` | `string` | `'Send a message…'` | |
| `showContextBar` | `boolean` | `true` | The token ring suffix. |
| `onSent` | `() => void` | — | Fires on send/queue (snap timeline to bottom). |

When the runtime reports `isRunning`, the send button becomes a stop button and
`send()` routes to `enqueueMessage` (type-ahead).

---

## `<Message />`

A single chat bubble. Usually rendered by `Thread`; use directly for custom layouts.

```tsx
import { Message } from 'jcode-ui'

<Message message={msg} canEdit={msg.role === 'user' && !isRunning} />
```

| Prop | Type | Notes |
|------|------|-------|
| `message` | `Message` | The message data. |
| `canEdit` | `boolean` | Shows the edit affordance (user messages, idle). |

Markdown is rendered via marked + highlight.js + DOMPurify. Role styling:
user = right-aligned primary fill, assistant = left surface, system = muted/error.

---

## `<ToolCallCard />`

The expand/collapse shell for a tool call. Dispatches the body to a registered
renderer. Recurses into `children` for subagent calls.

```tsx
import { ToolCallCard } from 'jcode-ui'

<ToolCallCard tool={toolCall} />
```

| Prop | Type | Notes |
|------|------|-------|
| `tool` | `ToolCall` | The tool call data. |
| `registry` | `ToolRendererRegistry` | Override (defaults to context). |

The header shows a status glyph (◈ running / ✓ done / ✗ error), title, subtitle,
and a diff counter (+N/-M) for edit/multi_edit. The shimmer animation plays while
running.

---

## `<ApprovalBanner />`

The approval gate. 3-tier decision (allow once / allow all [armed] / deny).

```tsx
import { ApprovalBanner } from 'jcode-ui'

<ApprovalBanner approval={ap} />
```

Pending: a warning-tinted card with the tool identity, primary target, an
external-path chip, collapsible full args, and the button ramp. Resolved:
collapses to a borderless inline note (✓ allowed / ✗ denied).

---

## `<AskUserCard />`

Interactive question block. Single + multi-select, free-text "Other", digit-key
shortcuts (1-9).

```tsx
import { AskUserCard } from 'jcode-ui'

<AskUserCard tool={toolWithAskUser} />
```

---

## `<ContextBar />`

The token/context indicator. SVG ring showing context-window occupancy (red at
≥90%) + a hover popover with the bucket breakdown.

```tsx
import { ContextBar } from 'jcode-ui'

<ContextBar size={20} breakdown={taskContextBreakdown} />
```

| Prop | Type | Notes |
|------|------|-------|
| `breakdown` | `TaskContextBreakdown` | Host-provided per-task stats. |
| `size` | `number` | Ring diameter (px). Default 20. |
| `dangerThreshold` | `number` | 0-1, turns red above. Default 0.9. |

---

## Providers

### `<RuntimeProvider>`

Provides the `ChatRuntime` to a subtree.

```tsx
<RuntimeProvider runtime={runtime}>
  <Thread />
  <ChatInput />
</RuntimeProvider>
```

### `<ToolRegistryProvider>`

Provides the `ToolRendererRegistry` (defaults to `createDefaultToolRegistry()`).

```tsx
const registry = createDefaultToolRegistry()
registry.register('my_tool', MyRenderer)

<ToolRegistryProvider registry={registry}>
  <Thread />
</ToolRegistryProvider>
```

### `<ApiBaseProvider>`

Provides the API base URL so `browser_screenshot` can resolve image refs.

```tsx
<ApiBaseProvider apiBase={apiBase}>
  <Thread />
</ApiBaseProvider>
```
