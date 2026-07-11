---
title: Migrating to 0.2
parent: Guides
nav_order: 1
---

# Migrating to 0.2 (scoped tokens)

0.2's breaking change is **token scoping**: every design token moved from
`:root` to the `[data-jcode-ui]` attribute scope and gained a `--jcode-`
prefix. Nothing leaks into your page anymore, and your variables can't collide
with ours.

```css
/* 0.1 — global, unprefixed */
:root { --color-primary: #FF8400; }

/* 0.2 — scoped, prefixed */
[data-jcode-ui] { --jcode-color-primary: #FF8400; }
```

Components stamp `data-jcode-ui` on their own roots — no wrapper element is
required. Dark mode still keys off a `.dark` ancestor (e.g. `<html class="dark">`).

## Fast path: compat.css

If you themed 0.1 via the old names, add one import and keep shipping:

```ts
import 'jcode-ui/styles.css'
import 'jcode-ui/compat.css'   // AFTER styles.css
```

`compat.css` re-reads every legacy name (`--color-primary`, `--radius-md`,
`--hljs-*`, …) from your page and maps it onto the scoped token. Generated
themes that override `--color-*` on `:root`/`html[data-theme]` keep working
unchanged.

## Proper migration

Rename your overrides and move them into the scope:

| 0.1 | 0.2 |
|-----|-----|
| `--color-*` | `--jcode-color-*` |
| `--radius-*` / `--shadow-*` | `--jcode-radius-*` / `--jcode-shadow-*` |
| `--accent-*` / `--neutral-*` | `--jcode-accent-*` / `--jcode-neutral-*` |
| `--font-sans` / `--font-mono` | `--jcode-font-sans` / `--jcode-font-mono` |
| `--code-*` / `--term-*` / `--hljs-*` | `--jcode-code-*` / `--jcode-term-*` / `--jcode-hljs-*` |
| `--ease-*` / `--duration-*` / `--z-*` | `--jcode-ease-*` / `--jcode-duration-*` / `--jcode-z-*` |

Declare them on `[data-jcode-ui]` (load your CSS after ours so it wins):

```css
[data-jcode-ui] { --jcode-color-primary: #6366f1; }
.dark [data-jcode-ui] { --jcode-color-primary: #818cf8; }
```

`--jcode-col-max`, `--jcode-col-pad-x` and `--jcode-gutter` were already
prefixed and are unchanged.

## shadcn apps

Skip both of the above — inherit your existing theme:

```ts
import 'jcode-ui/styles.css'
import 'jcode-ui/shadcn.css'   // maps --primary/--background/--radius/… onto jcode tokens
```

## Also renamed

Animation utility classes gained the same prefix (`.animate-fade-in` →
`.jcode-animate-fade-in`, keyframes `fade-in` → `jcode-fade-in`). If you
reused ours, rename; if you have your own with those names, they no longer
collide — that's the point.

## New optional runtime actions

`RuntimeActions` grew optional capabilities: `resolveApprovalOption`,
`regenerate`, `switchVersion`, `submitFeedback`, `retryMessage`. All are
opt-in — controls only render when the host provides the action. Existing
runtimes compile unchanged (one exception: `RuntimeState.connection` is a new
required slice, defaulted to `'connected'` by `normalizeState`, so
`createExternalStoreRuntime`/`createMockRuntime` users are unaffected).
