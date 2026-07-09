---
title: ChatInput
parent: Components
nav_order: 2
---

# ChatInput

The composer: autosizing textarea, send / queue / stop, slash-command palette, image attachments, and optional context ring.

<div data-jcode-demo="chat-input" data-height="140"></div>

## Usage

```tsx
import { ChatInput } from 'jcode-ui'

<ChatInput
  slashCommands={[{ slash: '/goal', description: 'set the session goal' }]}
  allowImages={modelSupportsVision}
  showContextBar
  onSent={() => timelineSnapToBottom()}
/>
```

## Props

| Prop | Type | Default | Notes |
|------|------|---------|-------|
| `slashCommands` | `SlashCommand[]` | — | Drives the `/` menu. |
| `allowImages` | `boolean` | `false` | Gate by model vision support. Enables paste + paperclip + attachment strip. |
| `acceptImages` | `string` | `'image/*'` | File picker `accept`. |
| `placeholder` | `string` | `'Send a message…'` | |
| `showContextBar` | `boolean` | `true` | Token ring suffix (`ContextBar`). |
| `onSent` | `() => void` | — | Fires on send/queue (snap timeline). |

```ts
interface SlashCommand {
  slash: string        // e.g. '/goal'
  description?: string
}
```

## Behavior

| Runtime state | Composer behavior |
|---------------|-------------------|
| `isRunning === false` | Send → `actions.sendMessage` |
| `isRunning === true` | Send → `actions.enqueueMessage` (type-ahead); button becomes **Stop** → `actions.stop` |
| Queued messages | Chips rendered above the textarea |
| Slash `/` | Filters `slashCommands` and inserts on select |
| `allowImages` | Paperclip opens file picker; paste images into the textarea; strip uses `AttachmentList` |

See [Attachment](/chat-ui/docs/components/attachment) for the shared tile API (library + product).

Keyboard: Enter sends (Shift+Enter newline). IME composition is respected.

## App chrome vs library

`ChatInput` intentionally **excludes** model/mode/workspace pickers — those stay in the host product (see jcode web `ProjectHeader`). Compose them above or beside `ChatInput`.

## Headless alternative

`Composer` from `jcode-ui-core/primitives` exposes render slots (`renderSubmitButton`, `renderSlashMenu`, `renderAttachments`, …).

## Related

- [ContextBar](/chat-ui/docs/components/context-bar)  
- [Attachment](/chat-ui/docs/components/attachment)  
- [Slash commands guide](/chat-ui/docs/guides/slash-commands)  
