/**
 * KaTeX plugin (opt-in) — enables `$…$` inline and `$$…$$` block math.
 *
 *   import { registerKatex } from 'jcode-ui/plugins/katex'
 *   import 'katex/dist/katex.min.css'   // host provides katex + its CSS
 *   await registerKatex()
 *
 * `markdown.ts` never imports this file, so a consumer who never calls
 * `registerKatex()` ships no math code and leaves `$…$` as literal text.
 * `katex.renderToString` is synchronous, so only the module load is async.
 */

import { registerMathRenderer } from '../lib/markdown.js'
import type { MathRenderer } from '../lib/markdown.js'

export interface KatexPluginOptions {
  /** Throw instead of rendering an error node. Default: false. */
  throwOnError?: boolean
  /** Any other KaTeX option is passed through. */
  [key: string]: unknown
}

let warned = false
function warnOnce(message: string, err?: unknown): void {
  if (warned) return
  warned = true
  console.warn(`[jcode-ui] ${message}`, err ?? '')
}

/**
 * Dynamically load KaTeX and wire the markdown math renderer. On import failure
 * (katex not installed) it warns once and leaves math as text.
 */
export async function registerKatex(opts: KatexPluginOptions = {}): Promise<void> {
  let katex: (typeof import('katex'))['default']
  try {
    katex = (await import('katex')).default
  } catch (err) {
    warnOnce('katex is not installed — math left as text. Run `npm i katex`.', err)
    return
  }

  const render: MathRenderer = (tex, displayMode) =>
    katex.renderToString(tex, { throwOnError: false, ...opts, displayMode })

  registerMathRenderer(render)
}
