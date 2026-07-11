/**
 * Mermaid plugin (opt-in) — renders ```` ```mermaid ```` fences to SVG.
 *
 *   import { registerMermaid, bindMermaid } from 'jcode-ui/plugins/mermaid'
 *   await registerMermaid({ theme: 'neutral' })
 *   // after markdown HTML is injected into `container`:
 *   await bindMermaid(container)
 *
 * marked renderers are synchronous but `mermaid.render` is async, so
 * registration installs a code-block hook that emits a placeholder (keeping the
 * raw source as a fallback), and `bindMermaid(root)` swaps in the rendered SVG
 * after injection. `markdown.ts` never imports this file (tree-shake).
 */

import { registerCodeBlockRenderer } from '../lib/markdown.js'

type MermaidModule = (typeof import('mermaid'))['default']

export interface MermaidPluginOptions {
  /** Mermaid theme, e.g. 'default' | 'neutral' | 'dark' | 'forest'. */
  theme?: string
  /** Any other mermaid.initialize() option is passed through. */
  [key: string]: unknown
}

let mermaidApi: MermaidModule | null = null
let seq = 0
let warned = false

function warnOnce(message: string, err?: unknown): void {
  if (warned) return
  warned = true
  console.warn(`[jcode-ui] ${message}`, err ?? '')
}

const HTML_ESCAPES: Record<string, string> = {
  '&': '&amp;',
  '<': '&lt;',
  '>': '&gt;',
  '"': '&quot;',
  "'": '&#39;',
}
function escapeHtml(s: string): string {
  return s.replace(/[&<>"']/g, (c) => HTML_ESCAPES[c])
}

/**
 * Dynamically load mermaid and install the ```` ```mermaid ```` code-block hook.
 * On import failure (mermaid not installed) it warns once and leaves such
 * fences to render as ordinary code blocks.
 */
export async function registerMermaid(opts: MermaidPluginOptions = {}): Promise<void> {
  try {
    mermaidApi = (await import('mermaid')).default
  } catch (err) {
    warnOnce('mermaid is not installed — ```mermaid rendered as code. Run `npm i mermaid`.', err)
    return
  }

  mermaidApi.initialize({ startOnLoad: false, securityLevel: 'strict', ...opts })

  registerCodeBlockRenderer(({ code, lang }) => {
    if (lang !== 'mermaid') return null
    const enc = encodeURIComponent(code)
    return (
      `<div class="jcode-mermaid" data-mermaid-src="${enc}">` +
      `<pre class="jcode-mermaid__source">${escapeHtml(code)}</pre>` +
      `</div>`
    )
  })
}

/**
 * Render every un-rendered `.jcode-mermaid` placeholder under `root`.
 * Idempotent per element; call after injecting markdown HTML. On a diagram
 * error the placeholder keeps its source `<pre>` as a fallback.
 */
export async function bindMermaid(root: HTMLElement): Promise<void> {
  const api = mermaidApi
  if (!api) return
  const nodes = Array.from(root.querySelectorAll<HTMLElement>('.jcode-mermaid[data-mermaid-src]'))
  for (const node of nodes) {
    if (node.getAttribute('data-mermaid-done') === '1') continue
    node.setAttribute('data-mermaid-done', '1')

    const enc = node.getAttribute('data-mermaid-src') || ''
    let src = enc
    try {
      src = decodeURIComponent(enc)
    } catch {
      /* keep raw */
    }

    try {
      const { svg } = await api.render(`jcode-mermaid-${++seq}`, src)
      node.innerHTML = svg
    } catch (err) {
      node.removeAttribute('data-mermaid-done')
      node.setAttribute('data-mermaid-error', '1')
      warnOnce('mermaid failed to render a diagram.', err)
      // leave the <pre> source fallback in place
    }
  }
}
