---
title: BranchPicker
parent: Components
nav_order: 12
---

# BranchPicker

The `‹ 2/3 ›` version stepper for a branched message. Edit-a-user-message and regenerate-an-assistant-message both create alternate takes; this control steps between them.

<div data-jcode-demo="branching" data-height="240"></div>

The demo renders a single assistant `Message` with three versions. The mock runtime's default `switchVersion` and `submitFeedback` make the stepper, regenerate (↻), and 👍/👎 controls live.

## Data model

Branches live on the message, not on a separate store. `Message.content` **always mirrors the active version**, so non-branching consumers keep working untouched.

```ts
interface Message {
  // …
  versions?: MessageVersion[]   // alternate takes; absent for unbranched messages
  activeVersionId?: string      // which entry of `versions` is showing
}

interface MessageVersion {
  id: string
  content: string
  timestamp: number
  reasoning?: string
  sources?: MessageSource[]
  images?: ChatImage[]
}
```

Stepping calls `actions.switchVersion(messageId, versionId)`. The host (or the mock runtime) updates `activeVersionId` **and** copies that version's `content`/`reasoning`/`sources` up onto the message.

`BranchPicker` is rendered automatically inside [`Message`](/chat-ui/docs/components/message)'s action footer — you rarely mount it directly.

## Usage

```tsx
import { RuntimeProvider, createMockRuntime, Message } from 'jcode-ui'
import 'jcode-ui/styles.css'

const assistant = {
  id: 'a1',
  role: 'assistant' as const,
  content: 'Use sync.Map for the shared registry — lock-free reads.', // mirrors v2
  timestamp: Date.now(),
  activeVersionId: 'v2',
  versions: [
    { id: 'v1', content: 'Wrap map access in a sync.Mutex.', timestamp: Date.now() },
    { id: 'v2', content: 'Use sync.Map for the shared registry — lock-free reads.', timestamp: Date.now() },
    { id: 'v3', content: 'Shard the map by key hash.', timestamp: Date.now() },
  ],
}

<RuntimeProvider
  runtime={createMockRuntime({ items: [{ kind: 'message', seq: 1, data: assistant }] })}
>
  <Message message={assistant} />
</RuntimeProvider>
```

## Props

| Prop | Type | Notes |
|------|------|-------|
| `message` | `MessageData` | The (possibly branched) message. |

## Fail-visible

`BranchPicker` renders **nothing** unless there is more than one version **and** the host wired `actions.switchVersion` — never a dead stepper. This mirrors the whole library's convention: a control that can't act is not shown.

The related affordances follow the same rule inside `Message`:

| Control | Shown when |
|---------|-----------|
| ↻ Regenerate | `actions.regenerate` is wired |
| 👍 / 👎 | `actions.submitFeedback` is wired |
| ‹ N/M › | `versions.length > 1` **and** `actions.switchVersion` is wired |

## Related

- [Message](/chat-ui/docs/components/message)
- [Runtime actions](/chat-ui/docs/guides/runtime)
