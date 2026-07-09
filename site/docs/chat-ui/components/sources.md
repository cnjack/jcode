---
title: Sources
parent: Components
nav_order: 8
---

# Sources

Citation list for a message. Auto-rendered by `Message` when `message.sources` is set.

<div data-jcode-demo="sources"></div>

## Usage

```tsx
import { Sources } from 'jcode-ui'

<Sources sources={msg.sources} />
```

## Props

| Prop | Type | Notes |
|------|------|-------|
| `sources` | `MessageSource[]` | |

```ts
interface MessageSource {
  id: string
  title: string
  url?: string
  snippet?: string
}
```

Click a chip to open the snippet popover; URL titles link out in a new tab.
