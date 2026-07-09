---
title: Virtualization
parent: Guides
nav_order: 5
---

# Virtualization & stream follow

Long agent sessions can produce thousands of timeline rows. `Thread` uses TanStack Virtual by default.

## Defaults

| Prop | Default | Meaning |
|------|---------|---------|
| `virtualize` | `true` | Window the DOM |
| `overscanBottom` | `24` (styled) | Padding for sticky composer |
| scroll threshold | `80` px (primitive) | "At bottom" hysteresis |

## Follow-only-when-at-bottom

While the user is near the bottom, new / streaming content auto-scrolls into view. If they scroll up to read earlier tools, the viewport **must not** yank down on every token.

This contract is implemented in the headless `Thread` + `useAutoScroll` / `useStreamFollow` hooks.

## When to disable virtualization

```tsx
<Thread virtualize={false} />
```

Use for:

- Short session replays  
- Screenshot / visual tests  
- SSR first paint (hydrate, then enable)

## Performance tips

- Keep `seq` stable; don't remount the whole list on each token  
- Prefer updating the last message's `content` over pushing new message items per chunk  
- Avoid huge uncontrolled markdown in a single cell without collapse (tool cards already collapse)  
