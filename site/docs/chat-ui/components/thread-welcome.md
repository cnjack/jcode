---
title: ThreadWelcome
parent: Components
nav_order: 11
---

# ThreadWelcome

The empty-thread hero: a brand mark, a one-line pitch, and room for starter `Suggestions` below. Deliberately quiet — the composer is the call to action; the welcome only orients.

<div data-jcode-demo="welcome" data-height="300"></div>

## Usage

Drop it into `Thread`'s `emptyState`. `Suggestions` pills send their prompt through the runtime by default.

```tsx
import {
  RuntimeProvider,
  createMockRuntime,
  Thread,
  ThreadWelcome,
  Suggestions,
} from 'jcode-ui'
import 'jcode-ui/styles.css'

const welcome = (
  <ThreadWelcome
    title="What can I help you ship?"
    subtitle="Ask about your codebase, or pick a starter below."
  >
    <Suggestions
      items={[
        { id: 's1', label: 'Explain this repo', prompt: 'Give me a tour of this repository.' },
        { id: 's2', label: 'Find the race condition', prompt: 'Fix the race in server.go.' },
        { id: 's3', label: 'Write tests', prompt: 'Add table-driven tests for the parser.' },
      ]}
    />
  </ThreadWelcome>
)

<RuntimeProvider runtime={createMockRuntime()}>
  <Thread emptyState={welcome} />
</RuntimeProvider>
```

## Props

### `ThreadWelcome`

| Prop | Type | Default | Notes |
|------|------|---------|-------|
| `logo` | `ReactNode` | neutral chat glyph | Brand mark / logo slot. |
| `title` | `string` | `'Start a new conversation'` | Headline. |
| `subtitle` | `string` | — | Supporting line under the headline. |
| `children` | `ReactNode` | — | Extra content below (typically `<Suggestions>`). |

### `Suggestions`

| Prop | Type | Default | Notes |
|------|------|---------|-------|
| `items` | `SuggestionItem[]` | — | The pills to render. Empty → renders nothing. |
| `onPick` | `(item: SuggestionItem) => void` | — | Intercept picks (e.g. prefill the composer instead of sending). |
| `scroll` | `boolean` | `false` | Compact single-line variant with horizontal scroll. |
| `disabled` | `boolean` | `false` | Disable all pills (e.g. while running). |

```ts
interface SuggestionItem {
  id?: string      // stable key; defaults to label
  label: string    // pill text
  prompt?: string  // message to send; defaults to label
}
```

Without `onPick`, picking a pill calls `actions.sendMessage(item.prompt ?? item.label)`.

## Related

- [Thread](/chat-ui/docs/components/thread) — `emptyState` slot
- [ChatInput](/chat-ui/docs/components/chat-input)
