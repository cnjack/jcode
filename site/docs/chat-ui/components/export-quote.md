---
title: Export & Quote
parent: Components
nav_order: 19
---

# Export & Quote

Two small conversation utilities: `ExportButton` downloads the thread as Markdown, and `QuoteSelection` floats a "Quote" affordance over selected message text and hands it to your composer.

## ExportButton

Reads the timeline from the runtime and serializes it with `exportThreadMarkdown` (pure, from `jcode-ui-core`). Renders **nothing** while the thread is empty — an export of nothing is noise.

```tsx
import { RuntimeProvider, ExportButton } from 'jcode-ui'
import 'jcode-ui/styles.css'

<RuntimeProvider runtime={runtime}>
  <ExportButton filename="session.md" title="Debug session" />
</RuntimeProvider>
```

| Prop | Type | Default | Notes |
|------|------|---------|-------|
| `filename` | `string` | `'conversation.md'` | Download filename. |
| `title` | `string` | — | Document title inside the Markdown. |
| `className` | `string` | — | Extra class on the button. |

## QuoteSelection

Watches text selections inside jcode-ui prose (`.jcode-prose` under a `[data-jcode-ui]` root) and floats a small **Quote** button at the selection. Picking it hands the plain text to `onQuote` — typically wired into the composer via a `ComposerHandle`. Renders in a portal so ancestor `overflow`/`transform` can't clip it.

```tsx
import { useRef } from 'react'
import { ChatInput, QuoteSelection, formatQuote } from 'jcode-ui'
import type { ComposerHandle } from 'jcode-ui-core/primitives'
import 'jcode-ui/styles.css'

function Panel() {
  const composer = useRef<ComposerHandle>(null)
  return (
    <>
      {/* …Thread… */}
      <QuoteSelection onQuote={(t) => composer.current?.insertText(formatQuote(t))} />
      <ChatInput ref={composer} />
    </>
  )
}
```

| Prop | Type | Default | Notes |
|------|------|---------|-------|
| `onQuote` | `(text: string) => void` | — | Receives the selected plain text on click. |
| `label` | `string` | `'Quote'` | Button label. |
| `maxLength` | `number` | `2000` | Max characters captured. |

### `formatQuote`

Turns selected text into a Markdown blockquote block ready for the composer — each line prefixed with `>` and a trailing blank line:

```ts
formatQuote('line one\nline two')
// => '> line one\n> line two\n\n'
```

## Related

- [ChatInput](/chat-ui/docs/components/chat-input) — the `ComposerHandle` (`insertText`)
- [Message](/chat-ui/docs/components/message)
