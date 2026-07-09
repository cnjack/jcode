---
title: Slash commands
parent: Guides
nav_order: 3
---

# Slash commands

`ChatInput` / headless `Composer` filter a host-provided command list when the user types `/`.

<div data-jcode-demo="chat-input" data-height="140"></div>

## Provide commands

```tsx
const slashCommands = [
  { slash: '/goal', description: 'Set or update the session goal' },
  { slash: '/compact', description: 'Compact conversation context' },
  { slash: '/clear', description: 'Clear the timeline' },
]

<ChatInput slashCommands={slashCommands} />
```

Selecting a row inserts the `slash` text at the caret (the host can still intercept on send).

## Handle on send

Slash menus only insert text. Interpretation happens in `sendMessage`:

```ts
actions: {
  sendMessage: (text, images) => {
    if (text.startsWith('/goal ')) {
      dispatch(setGoal(text.slice(6)))
      return
    }
    if (text.trim() === '/clear') {
      dispatch(clearTimeline())
      return
    }
    dispatch(sendToAgent({ text, images }))
  },
  // …
}
```

## Headless customization

Use `Composer` + `renderSlashMenu` for a design-system listbox, keyboard highlight, or remote-filtered results.
