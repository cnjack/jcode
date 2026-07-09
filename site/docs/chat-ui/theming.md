---
title: Theming
parent: jcode-ui
nav_order: 4
---

# Theming

Every color, radius, shadow, and transition in jcode-ui is a CSS custom property. There are no
hardcoded hex values in the components — so you can retheme the entire UI by overriding tokens,
with zero component edits.

## Importing the base theme

```ts
import 'jcode-ui/styles.css'
```

This brings in:

1. Tailwind CSS 4 utilities.
2. The base design tokens (`:root` light + `.dark` dark).
3. Animations (shimmer for running tools).
4. Component-local styles (code blocks, diff tables).

## Light / dark mode

Dark mode is toggled by a `.dark` class on `<html>`:

```html
<html class="dark"> ... </html>
```

Toggle it at runtime:

```ts
document.documentElement.classList.toggle('dark')
```

## Overriding tokens

Override any token in your own CSS, scoped to `:root` and/or `.dark`:

```css
:root {
  --color-primary: #6366f1;       /* rebrand the accent */
  --color-background: #fafaf9;
  --radius-lg: 10px;
}
.dark {
  --color-primary: #818cf8;
  --color-background: #0a0a0a;
}
```

Every component picks up the change — buttons, the send icon, selection highlights, focus rings.

## The token reference

These are the tokens jcode-ui components read (defined in `tokens.css`):

**Colors** — `--color-primary`, `--color-background`, `--color-surface`, `--color-foreground`,
`--color-muted-foreground`, `--color-border`, `--color-muted`, `--color-secondary`,
`--color-success`/`-bg`/`-fg`, `--color-error`/`-bg`/`-fg`, `--color-warning`/`-bg`/`-fg`,
`--color-info`/`-bg`/`-fg`, `--color-destructive`, `--color-on-primary`, `--color-on-destructive`.

**Code** — `--code-bg`, `--code-border`, plus the full `--hljs-*` syntax-highlight palette.

**Accent washes** (derived from `--color-primary` via `color-mix`) — `--accent-wash-soft`,
`--accent-wash`, `--accent-wash-strong`, `--accent-border`, `--accent-fill`, `--accent-selection`.

**Radii** — `--radius-xs` / `sm` / `md` / `lg` / `xl` / `2xl` / `pill`.

**Shadows** — `--shadow-sm` / `md` / `lg` / `xl`, `--backdrop`.

**Motion** — `--ease-out`, `--ease-in`, `--ease-spring`, `--duration-fast` / `normal` / `slow` /
`slower`.

## Generated themes

jcode itself ships generated themes (dracula, nord, solarized, …) produced by a Go generator from a
single palette. Those live outside this package — they're emitted as
`html[data-theme="<id>"] { --color-…: … }` blocks and loaded by the host app. jcode-ui's components
work with any of them because they only read tokens.
