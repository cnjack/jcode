---
title: ApprovalBanner
parent: Components
nav_order: 5
---

# ApprovalBanner

Human-in-the-loop approval gate. Three-tier decision: **allow once** / **allow all** (armed two-step) / **deny**.

<div data-jcode-demo="approval"></div>

## Usage

```tsx
import { ApprovalBanner } from 'jcode-ui'

<ApprovalBanner approval={ap} />
```

## Props

| Prop | Type | Notes |
|------|------|-------|
| `approval` | `Approval` | Gate data from the timeline. |

```ts
interface Approval {
  id: string
  tool_name: string
  tool_args: string
  is_external: boolean   // outside workspace — UI flags prominently
  resolved?: boolean
  approved?: boolean
  resolving?: boolean    // in-flight; disables controls
}
```

## States

| State | UI |
|-------|-----|
| Pending | Warning-tinted card: tool identity, primary target, optional external chip, collapsible args, button ramp |
| Allow all (first click) | Arms confirm (two-step — prevents accidents) |
| Resolved approved | Borderless inline note with ✓ |
| Resolved denied | Borderless inline note with ✗ |

Actions call `runtime.actions.resolveApproval(id, approved, approveAll?)`.

## Headless

`ApprovalBlock` from `jcode-ui-core/primitives` with `renderPending` / `renderResolved` slots.

## Guide

[Approvals guide](/chat-ui/docs/guides/approvals) — when to require approval in the host agent.
