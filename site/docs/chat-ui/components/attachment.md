---
title: Attachment
parent: Components
nav_order: 9
---

# Attachment

Image attachment tiles for the composer and user messages — thumbnails, remove control, and click-to-preview. Inspired by [assistant-ui Attachments](https://www.assistant-ui.com/docs/guides/attachments), scoped to **vision images** (`ChatImage`) that jcode agents already send to the model.

<div data-jcode-demo="attachment"></div>

## Overview

| Piece | Role |
|-------|------|
| `Attachment` | One image tile (+ optional × + lightbox) |
| `AttachmentList` | Flex row of tiles |
| `ChatInput` + `allowImages` | Paste + paperclip picker + strip above the input |
| `Message` | Renders `message.images` with the same tiles |

**Not included (by design):** PDF / arbitrary file adapters, upload progress, cloud storage. Those stay host concerns — attach your own chips next to the composer if needed. Most agent models only accept images today.

## Usage

```tsx
import { Attachment, AttachmentList, ChatInput } from 'jcode-ui'
import type { ChatImage } from 'jcode-ui-core'

// Standalone tiles
<Attachment image={img} size={56} onRemove={() => remove(i)} />
<AttachmentList images={images} onRemove={remove} />

// In the composer (preferred — handles paste + file picker)
<ChatInput allowImages placeholder="Send a message…" />
```

`ChatImage` shape (shared with the jcode product):

```ts
interface ChatImage {
  data: string        // base64, no data: prefix
  media_type: string  // e.g. image/png
  name?: string       // filename for tooltip / a11y
}
```

## Props

### Attachment

| Prop | Type | Notes |
|------|------|-------|
| `image` | `ChatImage` | Required. |
| `onRemove` | `() => void` | Shows the × button when set (composer). |
| `size` | `number` | Thumbnail px. Default `56`. |
| `preview` | `boolean` | Click-to-lightbox. Default `true`. |

### AttachmentList

| Prop | Type | Notes |
|------|------|-------|
| `images` | `ChatImage[]` | |
| `onRemove` | `(index: number) => void` | |
| `size` | `number` | Passthrough. |
| `preview` | `boolean` | Passthrough. |
| `className` | `string` | |

### ChatInput attachment-related

| Prop | Type | Notes |
|------|------|-------|
| `allowImages` | `boolean` | Enables paste, paperclip, and the attachment strip. Gate on model vision. |
| `acceptImages` | `string` | File input `accept`. Default `image/*`. |

## How it wires (runtime)

1. **Composer** (`jcode-ui-core`) owns pending images, paste handling, hidden file input, and size limits (`maxImageBytes`, default 10MB).
2. **ChatInput** supplies the styled paperclip (`ComposerAddAttachment` equivalent) and `AttachmentList` strip (`ComposerAttachments`).
3. On send, images go out via `actions.sendMessage(text, images)` / `enqueueMessage`.
4. **Message** renders `message.images` with the same tiles (no remove).

This keeps **library demos**, **docs**, and the **jcode product** (`web/src/components/ChatInput.tsx` uses `AttachmentList`) on one visual path.

## Product (jcode web / desktop)

- Attach entry: **+ menu → Image** (and paste into the textarea).
- Capability gate: model `image_support` / settings “Allow image attachments”.
- Thumbnails: shared `AttachmentList` (preview + remove).
- Backend contract remains base64 `ChatImage[]` on the user message.

## Compared to assistant-ui

| assistant-ui | jcode-ui |
|--------------|----------|
| `AttachmentAdapter` (image / text / PDF / composite) | Image-only `ChatImage` pipeline |
| Pending / uploading / error status | Immediate base64 decode (sync UX) |
| `ComposerAddAttachment` primitive | `renderAddAttachment` + paperclip in `ChatInput` |
| `UserMessageAttachments` | `Message` → `AttachmentList` |
| External URL attachments | Host-owned if needed |

If you need PDF-as-text or remote upload later, extend the host (product) and keep rendering custom chips beside `ChatInput` — don’t fork the image tile path.

## Related

- [ChatInput](/chat-ui/docs/components/chat-input)
- [Message](/chat-ui/docs/components/message)
- [assistant-ui Attachments guide](https://www.assistant-ui.com/docs/guides/attachments)
