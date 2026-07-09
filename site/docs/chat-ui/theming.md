---
title: Theming
nav_order: 6
---

# Theming

Every color, radius, shadow, and transition in jcode-ui is a CSS custom property. There are no
hardcoded hex values in the components — retheme by overriding tokens, with zero component edits.

<div data-jcode-demo="theming"></div>

## Importing the base theme

```ts
import 'jcode-ui/styles.css'
```

This brings in:

1. Tailwind CSS 4 utilities used by components  
2. Base design tokens (`:root` light + `.dark` dark)  
3. Animations (shimmer for running tools)  
4. Component-local styles (code blocks, diff tables)

## Light / dark mode

Dark mode is toggled by a `.dark` class on `<html>` (or a wrapping ancestor):

```html
<html class="dark"> ... </html>
```

```ts
document.documentElement.classList.toggle('dark')
```

## Overriding tokens

```css
:root {
  --color-primary: #6366f1;
  --color-background: #fafaf9;
  --radius-lg: 10px;
}
.dark {
  --color-primary: #818cf8;
  --color-background: #0a0a0a;
}
```

## Token reference

**Colors** — `--color-primary`, `--color-background`, `--color-surface`, `--color-foreground`,
`--color-muted-foreground`, `--color-border`, `--color-muted`, `--color-secondary`,
`--color-success` / `-bg` / `-fg`, `--color-error` / `-bg` / `-fg`, `--color-warning` / `-bg` / `-fg`,
`--color-info` / `-bg` / `-fg`, `--color-destructive`, `--color-on-primary`, `--color-on-destructive`.

**Code** — `--code-bg`, `--code-border`, plus the full `--hljs-*` syntax-highlight palette.

**Accent washes** (from `--color-primary` via `color-mix`) — `--accent-wash-soft`, `--accent-wash`,
`--accent-wash-strong`, `--accent-border`, `--accent-fill`, `--accent-selection`.

**Radii** — `--radius-xs` / `sm` / `md` / `lg` / `xl` / `2xl` / `pill`.

**Shadows** — `--shadow-sm` / `md` / `lg` / `xl`, `--backdrop`.

**Motion** — `--ease-out`, `--ease-in`, `--ease-spring`, `--duration-fast` / `normal` / `slow` / `slower`.

## Generated themes (jcode product)

jcode ships generated themes (dracula, nord, solarized, …) from a Go palette generator as
`html[data-theme="<id>"] { --color-… }` blocks. jcode-ui only reads tokens, so those themes work as long as the host sets the same custom properties.
