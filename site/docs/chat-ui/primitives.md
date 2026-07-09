---
title: Primitives
nav_order: 3
---

# Headless primitives

Under every styled `jcode-ui` component is a headless primitive in `jcode-ui-core` — behavior with
no styling. Use these directly when you want the logic (streaming, virtualization, keyboard
handling) but your own visual language.

```ts
import { Thread, MessageView, Composer, ToolCallView, ApprovalBlock, AskUserBlock } from 'jcode-ui-core/primitives'
```

Full prop tables: [API → Primitives](/chat-ui/docs/api/primitives).

## Thread

The conversation container. Owns virtualized rendering, the "follow only when at bottom" streaming
contract, and the pending ("Thinking…") row.

```tsx
import { Thread } from 'jcode-ui-core/primitives'

<Thread
  renderItem={(item) => {
    if (item.kind === 'message') return <MyMessage message={item.data} />
    if (item.kind === 'tool') return <MyTool tool={item.data} />
    if (item.kind === 'approval') return <MyApproval approval={item.data} />
  }}
  renderPending={() => <MyThinkingIndicator />}
  virtualize        // default true; disable for short/replay timelines
  estimateSize={80} // px, used before measure
  scrollThreshold={80}
/>
```

Key behaviors:

- **Virtualization** via TanStack Virtual — handles 10k-message threads. Set `virtualize={false}` for
  short replay timelines where DOM simplicity matters.
- **Auto-follow**: when the user is within `scrollThreshold` px of the bottom, new/streaming content
  scrolls into view. Scroll up to read and the view won't yank back down.
- **`seq` keying**: each `ThreadItem` carries a `seq` counter used as the React key.

## MessageView

A single chat bubble. Owns copy/edit interactions and image rendering; you provide the markdown
rendering via `renderContent`.

```tsx
import { MessageView } from 'jcode-ui-core/primitives'

<MessageView
  message={msg}
  canEdit={msg.role === 'user' && !isRunning}
  renderContent={(text) => <MyMarkdown html={text} />}
/>
```

## Composer

The message composer: autosizing textarea, send/queue/stop dispatch, slash-command palette
skeleton, image attachments.

```tsx
import { Composer } from 'jcode-ui-core/primitives'

<Composer
  slashCommands={[{ slash: '/goal', description: 'set the session goal' }]}
  allowImages={modelSupportsVision}
  onSent={() => timelineSnapToBottom()}
  renderSubmitButton={(mode, disabled, onActivate) => (
    <MyButton disabled={disabled} onClick={onActivate} />
  )}
/>
```

When the runtime reports `isRunning`, `send()` routes to `enqueueMessage` (type-ahead) and the
button slot receives `mode: 'stop'`. Always wire `onActivate` on the submit control.

## ToolCallView

The expand/collapse shell for a tool. Looks up a renderer via the `ToolRendererRegistry` (provided
through `ToolCallProvider`). Recurses into `children` for subagent calls.

```tsx
import { ToolCallView, ToolCallProvider } from 'jcode-ui-core/primitives'

<ToolCallProvider value={{ registry, renderAskUser: (t) => <MyAskUser tool={t} /> }}>
  <ToolCallView tool={toolCall} />
</ToolCallProvider>
```

## ApprovalBlock

The approval gate. Owns the pending/resolved split, the 3-tier decision, and the "arming" UX for
"allow all" (two-step confirm).

```tsx
import { ApprovalBlock } from 'jcode-ui-core/primitives'

<ApprovalBlock
  approval={ap}
  renderPending={(a, { allowOnce, allowAllArm, allowAllConfirm, deny, armed }) => (
    <MyDecisionCard ... />
  )}
  renderResolved={(a) => <MyResolvedNote approval={a} />}
/>
```

## AskUserBlock

Interactive question block. Owns per-question selection state (single + multi-select), free-text
"Other", and digit-key shortcuts (1-9).

```tsx
import { AskUserBlock } from 'jcode-ui-core/primitives'

<AskUserBlock
  tool={toolWithAskUser}
  renderPending={(questions, controls) => (
    <MyQuestionCard
      questions={questions}
      selected={controls.selected}
      onToggle={(q, label) => controls.toggleOption(q, label)}
      onSubmit={controls.submit}
    />
  )}
/>
```

The `controls` object exposes `toggleOption`, `setOther`, `submit`, `skip` — so a styled consumer
needs no local state.

## Behavioral hooks

```ts
import { useAutoScroll, useStreamFollow, useFocusOnIdle, useQueuedMessages } from 'jcode-ui-core/hooks'
```

| Hook | Purpose |
|------|---------|
| `useAutoScroll(threshold?)` | Track "at bottom" + `scrollToBottom` |
| `useStreamFollow(autoScroll, dep)` | Follow stream only when at bottom |
| `useFocusOnIdle(isRunning)` | Refocus composer when turn ends |
| `useQueuedMessages()` | Read type-ahead queue |

See [API → Hooks](/chat-ui/docs/api/hooks).

## Styled vs headless

| Need | Use |
|------|-----|
| Drop-in jcode look | `jcode-ui` (`Thread`, `ChatInput`, …) |
| Own design system | `jcode-ui-core/primitives` + your CSS |
| Mix | Styled `Thread` + custom `Composer` slots, etc. |
