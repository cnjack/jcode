---
title: Message
parent: Components
nav_order: 3
---

# Message

A single chat turn — flat layout (avatar + role label + markdown body), not a chat bubble card. Usually rendered by `Thread`; use directly for custom layouts.

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

## Message fields that drive UI

| Field | Renders |
|-------|---------|
| `role` | Avatar glyph + label (You / JCODE / System / WeChat) |
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
