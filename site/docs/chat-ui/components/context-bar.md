---
title: ContextBar
parent: Components
nav_order: 10
---

# ContextBar

Token / context-window indicator. SVG ring showing occupancy (turns red at ≥90%) plus an optional hover popover with the bucket breakdown.

<div data-jcode-demo="context-bar"></div>

## Usage

```tsx
import { ContextBar } from 'jcode-ui'

// Reads tokenSnapshot from the runtime automatically:
<ContextBar size={20} breakdown={taskContextBreakdown} />
```

Embedded in `ChatInput` when `showContextBar` is true.

## Props

| Prop | Type | Default | Notes |
|------|------|---------|-------|
| `breakdown` | `TaskContextBreakdown \| null` | — | Host-provided per-task stats for the popover. |
| `size` | `number` | `20` | Ring diameter (px). |
| `dangerThreshold` | `number` | `0.9` | 0–1; ring turns red above. |
| `showPopover` | `boolean` | `true` | Hover breakdown. |

## Data sources

| Source | Field |
|--------|-------|
| Runtime | `tokenSnapshot.total_tokens` / `model_context_limit` |
| Host (optional) | `TaskContextBreakdown` for system/tools/mcp/skills/messages |

```ts
interface TaskContextBreakdown {
  context_limit: number
  system_prompt_tokens: number
  system_tools_tokens: number
  mcp_tools_tokens: number
  skills_tokens: number
  messages_tokens: number
}
```
