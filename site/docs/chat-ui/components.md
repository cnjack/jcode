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

Complete coverage of the assistant-ui component catalog. Every component has a
jcode-ui equivalent (either a same-named component, a field-driven render inside
an existing component, or a documented product-app equivalent).

| assistant-ui | jcode-ui equivalent | Status | Notes |
|--------------|---------------------|--------|-------|
| `Thread` | `Thread` | ✅ same-named | Virtualized + auto-follow. |
| `ThreadList` (conversation list) | `Sidebar` (product app) | 🟡 product | The conversation list is app chrome, not a library primitive — shipped in the web-react product app. |
| `Composer` | `ChatInput` | ✅ same concept | Send/queue/stop, slash commands, attachments, context bar. |
| `Attachment` | `Attachment` + `AttachmentList` | ✅ same-named | Standalone + embedded in ChatInput. |
| `Markdown` | (built into `Message`) | ✅ field-driven | marked + highlight.js + DOMPurify. Render any markdown via `renderMarkdown()`. |
| `Diff Viewer` | `DiffRenderer` | ✅ tool renderer | Renders edit/multi_edit tool calls (red/green line table). |
| `Image` | (built into `Message` + `Attachment`) | ✅ field-driven | `message.images[]` renders inline; `Attachment` for the composer. |
| `Context Display` | `ContextBar` | ✅ same concept | SVG ring + breakdown popover. |
| `Message Timing` | (built into `Message`) | ✅ field-driven | `message.durationMs` renders a turn-elapsed label on the final assistant message. |
| `Reasoning` | `Reasoning` | ✅ same-named | Collapsible model thinking. Driven by `message.reasoning`. |
| `Sources` | `Sources` | ✅ same-named | Citation list. Driven by `message.sources`. |
| `Tool Fallback` | `GenericRenderer` | ✅ same concept | The registry fallback for unknown tools. |
| `Tool Group` | `ToolCallCard` (children recursion) | ✅ field-driven | Subagent nested calls render recursively via `tool.children`. |
| `Assistant Modal` | (product app) | 🟡 product | A floating-chat pattern is app-level; jcode ships a full-screen product UI instead. Buildable on top of the primitives. |
| `Assistant Sidebar` | `Sidebar` (product app) | 🟡 product | Same as ThreadList — app chrome. |
| `Model Selector` | `ProjectHeader` (product) | 🟡 product | Model/mode switching is app-level; the library is model-agnostic. |
| `makeAssistantToolUI` | `ToolRendererRegistry` | ✅ same concept | Plugin registry — register any tool renderer by name. |

**Legend:** ✅ = same-named / equivalent component in the library · 🟡 = app-level
(lives in the product app, not the library, because it's host-specific chrome) ·
field-driven = rendered automatically from a `Message`/`ToolCall` field.

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

## `<Reasoning />`

Collapsible model thinking / chain-of-thought. Mirrors assistant-ui's Reasoning.
Renders an assistant message's `reasoning` field in a collapsed disclosure
("Thought for Ns") with markdown support.

```tsx
import { Reasoning } from 'jcode-ui'

// Usually rendered automatically by <Message> when message.reasoning is set,
// but usable standalone:
<Reasoning reasoning={msg.reasoning} durationMs={msg.durationMs} defaultExpanded={false} />
```

| Prop | Type | Notes |
|------|------|-------|
| `reasoning` | `string` | Markdown text of the model's thinking. |
| `defaultExpanded` | `boolean` | Default `false` (collapsed). |
| `durationMs` | `number?` | Shows "Thought for Ns" label. |

---

## `<Sources />`

Citation list for a message. Mirrors assistant-ui's Sources. Renders a row of
clickable source chips; clicking opens a snippet popover.

```tsx
import { Sources } from 'jcode-ui'

// Usually rendered automatically by <Message> when message.sources is set:
<Sources sources={msg.sources} />
```

| Prop | Type | Notes |
|------|------|-------|
| `sources` | `MessageSource[]` | `{ id, title, url?, snippet? }`. |

---

## `<Attachment />` + `<AttachmentList />`

Image-attachment thumbnails. Mirrors assistant-ui's Attachment. Embedded in
`ChatInput`; also exported standalone for composing custom attachment UIs
(drag-drop zones, file pickers).

```tsx
import { Attachment, AttachmentList } from 'jcode-ui'

<Attachment image={img} size={64} onRemove={() => removeImage(i)} />
<AttachmentList images={images} onRemove={removeImage} />
```

| Prop | Type | Notes |
|------|------|-------|
| `image` | `ChatImage` | `{ data: base64, media_type }`. |
| `onRemove` | `() => void` | Renders the × button when provided. |
| `size` | `number` | Thumbnail px. Default 64. |

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
