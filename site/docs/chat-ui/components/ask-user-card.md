---
title: AskUserCard
parent: Components
nav_order: 6
---

# AskUserCard

Interactive question block for mid-run agent questions. Single + multi-select, free-text "Other", digit-key shortcuts (1–9).

<div data-jcode-demo="ask-user"></div>

## Usage

```tsx
import { AskUserCard } from 'jcode-ui'

// Usually rendered from ToolCallCard when tool.askUserQuestions is set:
<AskUserCard tool={toolWithAskUser} />
```

## Props

| Prop | Type | Notes |
|------|------|-------|
| `tool` | `ToolCall` | Must include `askUserQuestions` / `askUserId` while pending. |

## Question shape

```ts
interface AskUserQuestion {
  question: string
  header?: string
  options?: { label: string; description?: string }[]
  multi_select?: boolean
}
```

On submit, calls `actions.submitAskUser(askUserId, answers[])`.

## Keyboard

| Key | Action |
|-----|--------|
| `1`–`9` | Toggle option by index |
| Enter | Submit (when valid) |

## Headless

`AskUserBlock` with `renderPending(questions, controls)` — controls expose `toggleOption`, `setOther`, `submit`, `skip`.
