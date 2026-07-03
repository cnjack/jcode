# jcode.net — product site

The public website for jcode (https://www.j-code.net): React + Vite + TypeScript.
It replaced the static `agent-eval/site/` pages (2026-07).

## Stack

- **Vite + React 18 + react-router** — SPA with lazy-loaded routes.
- **Docs** — `src/lib/docs.ts` imports the repo's `docs/**/*.md` straight from
  `../docs` at build time (`import.meta.glob` raw), strips Jekyll/kramdown syntax
  (front matter, `{: .note }` IALs, `{% link %}` tags), builds the sidebar tree from
  front-matter `nav_order`/`parent`, and renders with react-markdown in a
  Notion-help-style layout (sidebar + breadcrumbs + "On this page" scrollspy TOC +
  client-side search). Screenshots come from `docs/asset` → `public/docs-asset`.
- **Remotion** — `src/remotion/` holds frame-driven product demos (desktop app,
  CLI TUI) embedded with `@remotion/player`; they share presentational components
  with the live interactive desktop replica (`src/components/desktop/`).
- **Showcase** — `public/showcase-projects/<id>/` are self-contained static apps
  **built entirely by the jcode agent** through the ACP harness
  (`agent-eval/showcase/challenge_round.py`, multi-round briefs). Metadata lives in
  `src/data/showcase.ts`.
- **Fonts** — self-hosted woff2 in `public/fonts` (Bricolage Grotesque, Instrument
  Sans, IBM Plex Mono); no external requests anywhere on the site.

## Develop

```bash
cd site
pnpm install
pnpm dev        # http://localhost:5173
pnpm build      # tsc -b && vite build → dist/
```

## Deploy

`dist/` is a static SPA — serve with a fallback rewrite to `/index.html`
(Caddy: `try_files {path} /index.html`). Production: `/data/j-code-net` behind the
Caddy container on the www.j-code.net host. Every page carries the ICP footer.
