---
title: Artifact
parent: Components
nav_order: 17
---

# Artifact

A titled card container for rich tool output — file viewers, diffs, previews, generated documents. Header (icon / title / subtitle) + an optional actions slot + an optional close button, over a scrollable content region.

<div data-jcode-demo="artifact" data-height="280"></div>

## Usage

`Artifact` is a pure container — you own the body. The `actions` slot is where copy / download / open controls go.

```tsx
import { Artifact } from 'jcode-ui'
import 'jcode-ui/styles.css'

<Artifact
  title="vite.config.ts"
  subtitle="7 lines · typescript"
  icon={<DocumentIcon />}
  actions={<button type="button" onClick={() => copy(source)}>Copy</button>}
  onClose={() => setOpen(false)}
>
  <pre style={{ margin: 0, padding: '0.75rem' }}>{source}</pre>
</Artifact>
```

## Props

| Prop | Type | Default | Notes |
|------|------|---------|-------|
| `title` | `string` | — | Primary label in the header. |
| `subtitle` | `string` | — | Secondary label (path, size, language…). Rendered muted + mono. |
| `icon` | `ReactNode` | — | Leading icon node (heroicon, extension dot…). |
| `actions` | `ReactNode` | — | Right-aligned actions (copy, download, open…). |
| `onClose` | `() => void` | — | When provided, a close (✕) button appears at the far right. |
| `maxHeight` | `string \| number` | `'24rem'` | Max height of the scrollable content region. |
| `className` | `string` | — | Extra classes on the root. |
| `children` | `ReactNode` | — | The body (rendered inside the scroll region). |

## Related

- [ToolCallCard](/chat-ui/docs/components/tool-call-card)
- [Code tool renderers](/chat-ui/docs/components/tool-renderers-code)
