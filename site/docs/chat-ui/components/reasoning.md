---
title: Reasoning
parent: Components
nav_order: 7
---

# Reasoning

Collapsible model thinking / chain-of-thought. Usually auto-rendered by `Message` when `message.reasoning` is set.

<div data-jcode-demo="reasoning"></div>

## Usage

```tsx
import { Reasoning } from 'jcode-ui'

<Reasoning
  reasoning={msg.reasoning}
  durationMs={msg.durationMs}
  defaultExpanded={false}
/>
```

## Props

| Prop | Type | Default | Notes |
|------|------|---------|-------|
| `reasoning` | `string` | — | Markdown text of the model's thinking. |
| `defaultExpanded` | `boolean` | `false` | Start open. |
| `durationMs` | `number?` | — | Shows "Thought for Ns". |

## Wiring from the host

Set `reasoning` on assistant `Message` objects as the model streams thinking tokens (or after the turn completes). No separate message-part protocol is required — jcode-ui is field-driven.
