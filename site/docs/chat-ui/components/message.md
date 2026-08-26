---
title: Message
parent: Components
nav_order: 3
---

# Message

A single chat turn in the default split layout: user content is a compact,
right-aligned muted bubble; assistant content stays flat and left-aligned.
Default user/assistant avatars and visible role labels are omitted, while the
message keeps an accessible role label. Usually rendered by `Thread`; use
directly for custom layouts.

<div data-jcode-demo="message"></div>

## Usage

```tsx
import { Message } from 'jcode-ui'

<Message message={msg} canEdit={msg.role === 'user' && !isRunning} />
```

## Props

| Prop | Type | Notes |
|------|------|-------|
| `message` | `Message` | See [types](/chat-ui/docs/api/types). |
| `canEdit` | `boolean` | Shows edit affordance (typically user + idle). |
| `showDuration` | `boolean` | Hides the legacy assistant duration when a completed-turn disclosure owns it. |
| `slots` | `MessageSlots` | Opts into a custom header/avatar or appends footer content. |

## Message fields that drive UI

| Field | Renders |
|-------|---------|
| `role` | User bubble vs. flat assistant/system layout; System and WeChat retain a compact visible label. |
| `content` | Markdown via `renderMarkdown()` (marked + highlight.js + DOMPurify) |
| `images[]` | Inline previews |
| `reasoning` | Collapsible [Reasoning](/chat-ui/docs/components/reasoning) |
| `sources[]` | [Sources](/chat-ui/docs/components/sources) chips |
| `durationMs` | Turn elapsed label on assistant messages |
| `level` / `detail` | System severity + expandable detail |
| `source` | e.g. `'wechat'` tint |

## Action bar (built-in)

On hover / focus:

- **Copy** — clipboard write of `content`
- **Edit** — when `canEdit`; Enter saves via `actions.editMessage`, Esc cancels

This is the jcode-ui equivalent of assistant-ui's ActionBar (copy/edit). Reload / speak / feedback are host concerns.

## Layout customization

The conversation stage is shared by messages, tools, approvals and the
composer. Override the scoped layout tokens after `jcode-ui/styles.css`:

```css
[data-jcode-ui] {
  --jcode-col-max: min(64rem, 100%);
  --jcode-col-pad-x: clamp(1rem, 2.2vw, 2rem);
  --jcode-gutter: 0rem;
}
```

Pass `slots.header` or `slots.avatar` when a host needs visible identity chrome;
the default layout intentionally communicates authorship through position and
surface instead.

## Markdown

```ts
import { renderMarkdown } from 'jcode-ui'

const html = renderMarkdown('# Hello')
```

Same pipeline the component uses. No separate `<Markdown />` component is required.

## Related

- [Reasoning](/chat-ui/docs/components/reasoning)  
- [Sources](/chat-ui/docs/components/sources)  
- [Thread](/chat-ui/docs/components/thread)  
