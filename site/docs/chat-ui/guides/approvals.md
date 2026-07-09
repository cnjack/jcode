---
title: Approvals
parent: Guides
nav_order: 4
---

# Approvals

Agent coding UIs need human gates before destructive tools. jcode-ui treats approvals as first-class timeline items.

<div data-jcode-demo="approval"></div>

## Host responsibilities

1. When the agent requests confirmation, append:

```ts
{
  kind: 'approval',
  seq: ++seq,
  data: {
    id: 'ap_123',
    tool_name: 'execute',
    tool_args: JSON.stringify({ command: 'rm -rf build' }),
    is_external: false,
  },
}
```

2. Implement `resolveApproval`:

```ts
resolveApproval: async (id, approved, approveAll) => {
  await api.post('/approvals/resolve', { id, approved, approve_all: approveAll })
  // mark item resolved in the timeline
}
```

3. Optionally set `resolving: true` while the request is in flight.

## UX details

| Control | Behavior |
|---------|----------|
| Allow once | `approved=true`, `approveAll` unset |
| Allow all | Two-step arm → confirm; then `approveAll=true` |
| Deny | `approved=false` |

External paths (`is_external`) get a stronger chip — use when the tool target escapes the workspace root.

## Pair with tools

Approvals sit **alongside** tool cards, not inside them. Typical order: assistant text → approval → tool (running) → tool (done).
