# Jui

A tiny, opinionated **UI component library** and design-token system written in
plain HTML, CSS and vanilla JavaScript. **Zero dependencies. No build step.**

This repository ships **two** things:

| File | What it is |
| --- | --- |
| `css/jui.css` | **The library** — design tokens + every component style |
| `js/jui.js`   | **The library** — component behaviours (theme, dismiss, auto-grow…) |
| `index.html`  | The documentation site (built *from* Jui's own components) |
| `css/docs.css`| Docs-shell-only styles (layout, code blocks, sidebar) |
| `js/docs.js`  | Docs-shell-only behaviour (filter, copy, scroll-spy, swatches) |

## Quick start

Serve the folder with any static server and open it:

```bash
python3 -m http.server 8080
# then visit http://localhost:8080/
```

To use Jui in your own project, copy two files and reference them with
**relative** paths:

```html
<link rel="stylesheet" href="./css/jui.css" />
<script src="./js/jui.js"></script>

<button class="jui-btn">Click me</button>
```

That's it — no bundler, no framework, no network requests.

## Architecture

- **Tokens first.** Everything visual flows from CSS custom properties on
  `:root`, with a designed dark-theme override block on `[data-theme="dark"]`.
  The accent is composed from `--jui-accent-h/s/l` so a single value recolors
  the whole library.
- **BEM-ish class names.** `.jui-btn`, `.jui-card`, with modifiers like
  `.jui-btn--outline` and `.jui-btn--sm`.
- **Progressive behaviour.** `jui.js` is optional and purely additive: it wires
  up behaviours via `data-*` attributes (`data-jui="autogrow"`,
  `data-jui="loading-toggle"`, `data-jui-dismiss`) and exposes a small
  `window.Jui` API. Components render correctly even before JS runs.
- **Self-documenting.** The docs site dogfoods every component — the sidebar
  filter, code blocks and buttons are all Jui.

## Components (35)

**Forms:** Button, Input, Textarea, Select, Checkbox, Radio, Switch,
Slider · **Data display:** Badge, Tag, Card, Avatar, Kbd, Accordion ·
**Data:** Table, Pagination, Stat, Progress, Skeleton, Spinner, Empty state ·
**Navigation:** Tabs, Breadcrumbs, Stepper, Command palette · **Overlay:**
Modal, Drawer, Dropdown, Tooltip, Popover · **Feedback:** Alert, Toast

### Round 1 — primitives

| Component | Highlights |
| --- | --- |
| Button | solid / outline / ghost / soft · accent / neutral / danger · sm / md / lg · disabled · loading spinner · icon slot |
| Badge | solid + soft · all semantics · sm / md · with-dot |
| Tag | neutral / accent / semantic tints · leading icon · dismissable |
| Card | header / body / footer · hoverable · clickable · horizontal media |
| Alert | success / warning / danger / info · icon · dismissable · soft background |
| Avatar | image + initials · deterministic hue · xs–lg · rounded / circle · status dot · group with overflow |
| Input | label / help / error · prefix / suffix icons · sm / md / lg · disabled |
| Textarea | label / help / error · auto-grow |
| Kbd | key styling · composable combinations |

### Round 2 — interactive components (all keyboard accessible + ARIA)

| Component | Data attribute / API | Highlights |
| --- | --- | --- |
| Select | `data-jui="select"` | combobox + listbox · ↑↓/Home/End · type-ahead · value mirrored to hidden input · `jui:select` event |
| Checkbox | `.jui-checkbox` / `data-jui="checkbox-group"` | native input · working indeterminate "select all" over children |
| Radio | `.jui-radio` | native inputs · arrow-key movement works natively |
| Switch | `.jui-switch` (+ `role="switch"`) | track+thumb · `aria-checked` synced · sm/md · disabled |
| Slider | `data-jui="slider"` | styled range · filled track · floating value bubble · min/max/step · disabled |
| Modal | `data-jui="modal-trigger"` + dialog | focus trap · ESC/overlay close · scroll lock · focus return · sm/md/lg |
| Drawer | `data-jui="drawer-trigger"` | right/left slide · same a11y contract as Modal |
| Dropdown | `data-jui="dropdown"` | role=menu/menuitem · arrow nav · Home/End · separators · disabled · flip · `jui:dropdown` event |
| Tooltip | `data-jui-tooltip="…"` / `data-jui="tooltip"` | 400ms hover delay · instant on focus · 4 placements · viewport-clamped |
| Popover | `data-jui="popover"` | click-toggled · focus in/out · ESC/outside close |
| Tabs | `data-jui="tabs"` | roving tabindex · ←/→/Home/End · sliding indicator · underline + pill variants |
| Accordion | `data-jui="accordion"` | animated height · chevron rotate · single-open or `data-multiple` |
| Toast | `Jui.toast({ title, message, variant, action })` | live region · 4 variants · progress bar (hover-pause) · action button · queue cap 5 |

### Round 3 — data, navigation, command palette & dashboard

| Component | Data attribute / API | Highlights |
| --- | --- | --- |
| Table | `data-jui="table-sort"` | striped/hover/compact · sticky header scroll · sortable `th[data-sort-key]` (number/string inferred) · `aria-sort` |
| Pagination | `data-jui="pagination"` | prev/next + numbered + ellipsis · `aria-current="page"` · `data-pagination-output` mirror · sm size |
| Stat | `.jui-stat` | label · big value · delta up/down · sparkline · icon |
| Progress | `.jui-progress` / `.jui-progress-ring` | linear (semantic + striped) & SVG ring · `role="progressbar"` · `--value` / `data-value` |
| Skeleton | `.jui-skeleton` | text / circle / rect shimmer · paused under reduced-motion |
| Spinner | `.jui-spinner` | sizes · colors · inline · in-button |
| Empty state | `.jui-empty` | hand-drawn theme-aware SVG · title · text · action · compact |
| Breadcrumbs | `.jui-breadcrumbs` | chevron separators · `aria-current="page"` · collapsed ellipsis |
| Stepper | `data-jui="stepper"` | done/current/upcoming · connectors · vertical · Next/Back |
| Command palette | `data-jui="command-palette"` / `Jui.commandPalette()` | ⌘/Ctrl+K · fuzzy match + highlight · `aria-activedescendant` · recent jumps |
| Theme customizer | top-bar gear (popover) | accent hue slider · radius scale · density · reset · persisted to `localStorage` |
| Dashboard | Examples section | a full SaaS overview built only from Jui components |

**Accessibility fix (this round).** Overlays (Modal/Drawer/Popover) now flip
`visibility` to `visible` instantly on open (delayed only on close), and focus
is moved into the panel with a double `requestAnimationFrame` — so `focus()`
never targets a still-hidden element. Escape is handled by a document-level
listener that closes the topmost open overlay regardless of where focus sits,
and Tooltips now appear instantly on keyboard focus (the delay is hover-only).

### JavaScript API (`window.Jui`)

```js
Jui.init(root);                       // initialize dynamically added DOM
Jui.getTheme(); Jui.setTheme("dark"); Jui.toggleTheme();
Jui.toast({ title: "Saved", variant: "success", action: { label: "Undo", onClick: fn } });
Jui.openOverlay(dialogEl);            // open a Modal/Drawer imperatively
Jui.closeOverlay(dialogEl);
Jui.commandPalette({ items: [...], onSelect: fn });  // programmatic palette
```

Events bubble off each component: `jui:select` (`{value}`), `jui:dropdown`
(`{value}`), `jui:pagination` (`{page, pages}`), `jui:stepper`
(`{current, total}`), `jui:theme` (`{theme}`), `jui:dismiss`.

## Theming

Toggle dark mode by setting `data-theme="dark"` on `<html>` (the docs site
persists the choice to `localStorage` and reads it back before first paint).
Recolor the accent by overriding the H/S/L pieces:
```css
:root {
  --jui-accent-h: 152;
  --jui-accent-s: 56%;
  --jui-accent-l: 45%;
}
/* --jui-accent, hover/active/soft/contrast derive automatically */
```

The radius tokens multiply by `--jui-radius-scale` (default `1`; the customizer
uses `0.35` sharp → `1.7` round), and the whole spacing scale shrinks under
`data-density="compact"`. The docs top-bar gear opens a **theme customizer**
popover (built on the Popover component) that drives the accent hue, radius
scale and density live — every choice is persisted to `localStorage` under
`jui-customizer` and restored before first paint.

### Dark mode

Flip `data-theme="dark"` on `<html>` — surfaces desaturate, borders soften,
shadows recede and the accent lightens for contrast. Use the sun/moon button in
the top bar to try it now.

## License

MIT — use it however you like.

---

Built autonomously by jcode.
