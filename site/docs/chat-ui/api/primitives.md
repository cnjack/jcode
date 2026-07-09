---
title: Primitives API
parent: API Reference
nav_order: 4
---

# Primitives API

```ts
import {
  Thread,
  MessageView,
  Composer,
  ToolCallView,
  ToolCallProvider,
  ApprovalBlock,
  AskUserBlock,
} from 'jcode-ui-core/primitives'
```

## Thread

| Prop | Type | Default |
|------|------|---------|
| `renderItem` | `(item: ThreadItem) => ReactNode` | required |
| `renderPending` | `() => ReactNode` | — |
| `renderEmpty` | `() => ReactNode` | — |
| `virtualize` | `boolean` | `true` |
| `estimateSize` | `number` | `80` |
| `scrollThreshold` | `number` | `80` |
| `overscanBottom` | `number` | — |
| `className` | `string` | — |

## MessageView

| Prop | Type |
|------|------|
| `message` | `Message` |
| `canEdit` | `boolean` |
| `renderContent` | `(text: string) => ReactNode` |
| `className` | `string` |

Plus optional action slots (see source `MessageViewRenderSlots`).

## Composer

| Prop | Type | Default |
|------|------|---------|
| `placeholder` | `string` | `'Send a message…'` |
| `maxRows` | `number` | `160` (px) |
| `slashCommands` | `SlashCommand[]` | — |
| `allowImages` | `boolean` | `false` |
| `maxImageBytes` | `number` | `10MB` |
| `defaultValue` | `string` | `''` |
| `onSent` | `() => void` | — |
| `className` | `string` | — |

**Slots:** `renderSlashMenu`, `renderQueue`, `renderSubmitButton`, `renderAttachments`, `renderPrefix`, `renderSuffix`.

## ToolCallView

| Prop | Type |
|------|------|
| `tool` | `ToolCall` |
| `registry` | `ToolRendererRegistry` (optional if context) |
| `className` | `string` |

Wrap tree with `ToolCallProvider` for registry + `renderAskUser`.

## ApprovalBlock

| Prop | Type |
|------|------|
| `approval` | `Approval` |
| `renderPending` | `(approval, actions) => ReactNode` |
| `renderResolved` | `(approval) => ReactNode` |
| `className` | `string` |

Pending actions: `allowOnce`, `allowAllArm`, `allowAllConfirm`, `deny`, `armed`.

## AskUserBlock

| Prop | Type |
|------|------|
| `tool` | `ToolCall` |
| `renderPending` | `(questions, controls) => ReactNode` |
| `renderResolved` | `(answers) => ReactNode` |
| `className` | `string` |

Controls: `toggleOption`, `setOther`, `submit`, `skip`, `selected`, …
