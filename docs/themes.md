---
title: Themes
nav_order: 10
---

# Themes

jcode ships a set of **built-in color themes** shared by the terminal UI and the
web interface. There are two categories — **dark** and **light** — and you pick
one per surface. Both surfaces draw from the same catalog, generated from a
single source so their colors never drift.

{: .note }
There is no custom-theme support by design — the catalog below is the full set.

## Built-in themes

| Theme | id | Appearance |
|---|---|---|
| jcode Dark *(default dark)* | `jcode-dark` | Dark |
| Midnight | `midnight` | Dark |
| Dracula | `dracula` | Dark |
| Nord | `nord-dark` | Dark |
| jcode Light *(default light)* | `jcode-light` | Light |
| GitHub Light | `github-light` | Light |
| Solarized Light | `solarized-light` | Light |

The `jcode-*` themes keep jcode's brand orange (`#FF8400`) as the accent;
third-party themes (Dracula, Nord, Solarized) use their own native accent.

## Terminal (TUI)

### Switching themes

Type **`/theme`** in the input to open the theme picker:

- **↑ / ↓** — live-preview each theme (the whole UI repaints as you move)
- **Enter** — apply and save the highlighted theme
- **Esc** — cancel and revert to the theme you started with
- **/** — filter the list by name

The chosen theme is written to your config and restored on the next launch.
Rendered markdown and code blocks in the transcript also follow the theme's
light/dark appearance. `/theme` is listed alongside the other
[commands](commands.html#slash-commands).

### Startup default

When no theme is saved, jcode auto-selects a default from your terminal's
background color — `jcode-dark` for a dark terminal, `jcode-light` for a light
one. Save a theme with `/theme` (or set it in config) to pin it explicitly and
skip auto-detection.

### Config

The terminal theme is stored in `~/.jcode/config.json` under `theme`:

```json
{ "theme": "nord-dark" }
```

Leave it unset (or remove it) to fall back to background auto-detection. See
[Configuration](configuration.html#theme).

## Web interface

Open **Settings → Appearance** to choose a theme:

- **System** — follow your operating system's light/dark preference (updates
  live when the OS setting changes)
- **Dark** / **Light** swatch grids — each card is a true mini-preview rendered
  in that theme's own colors; click one to apply it

The sidebar's sun/moon button is a quick light ⇄ dark flip between the two
brand defaults (`jcode-dark` and `jcode-light`); use the Appearance tab to pick
a specific named theme.

The web choice is saved in the browser (`localStorage`) and applied via a
`data-theme` attribute on `<html>`, with a small inline script that sets it
before the app boots so there's no color flash on load. See
[Web Interface](web-interface.html#theme--dark-mode) for the rest of the
browser UI.

## Terminal and web are independent

The two surfaces are intentionally **not** synced:

- the **terminal** stores its theme in `~/.jcode/config.json`
- the **web** UI stores its theme in the browser's `localStorage`

They share the same catalog but you can run, say, `nord-dark` in the terminal
and `github-light` in the browser at the same time.

## Troubleshooting

**Terminal colors look washed out or wrong.** Themes use 24-bit (truecolor)
values. On a terminal without truecolor support, lipgloss degrades them to the
nearest 256- or 16-color value — every theme stays usable, but the colors won't
be exact. Check that your terminal advertises truecolor (most modern ones do;
`COLORTERM` is usually `truecolor` or `24bit`). Inside tmux or over SSH you may
need to enable truecolor passthrough.

**The startup theme isn't what I expected.** With no saved `theme`, jcode picks
a default from the terminal background; if detection guesses wrong, run
`/theme` and pick one to pin it explicitly.

**The web UI briefly flashes the wrong theme.** The theme is applied by an
inline script before the app boots, keyed off your saved choice. A persistent
flash usually means a stale saved value — reselect a theme in
**Settings → Appearance** to rewrite it.

## For contributors

### Single source of truth

Every theme is defined once, in Go:

```
internal/theme/palette.go        # the Theme catalog (~26 semantic tokens each)
        │  go generate ./internal/theme/...   (run by `make generate`)
        ├─►  web/src/styles/tokens.generated.css        # [data-theme="…"] blocks
        └─►  web/src/composables/themes.generated.ts     # the picker registry
```

The web token set and the picker list are a pure function of the Go palette, so
the terminal and web renderers can't drift. Both generated files are build
artifacts (git-ignored) and are produced before `vite build` — `make generate`
runs the generator, `make build` runs it before building the frontend.

{: .important }
Don't edit `tokens.generated.css` or `themes.generated.ts` by hand — they are
overwritten on every `make generate`. Change `internal/theme/palette.go` and
regenerate.

### Adding a theme

1. Add a `Theme` entry to the `builtins` slice in
   [`internal/theme/palette.go`](https://github.com/cnjack/jcode/blob/main/internal/theme/palette.go),
   filling in every token with a `#RRGGBB` value and setting `Appearance` to
   `Dark` or `Light`.
2. Run `make generate` (or `go generate ./internal/theme/...`).
3. Run `go test ./internal/theme/...` — the tests verify that every token is a
   valid hex color and that the id follows the naming convention (light theme
   ids end in `-light`, which a pre-boot web script relies on).

The new theme then appears automatically in the TUI `/theme` picker and the web
Appearance tab — no other code changes needed.

### How it renders

- **Terminal:** the palette and all lipgloss styles are rebuilt from the active
  theme by `ApplyTheme` (in `internal/tui/styles.go`), so switching repaints the
  entire UI at runtime. Markdown (rendered by glamour) follows the theme's
  light/dark appearance.
- **Web:** components style exclusively through `--color-*` CSS variables, so a
  theme is just a different set of values for the same variables — adding one
  touches no component code.
