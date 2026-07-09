/**
 * Syntax-highlight demo source snippets (TSX) for the Preview | Code tab.
 * Uses highlight.js core + only the languages we need to keep the chunk small.
 */

import hljs from 'highlight.js/lib/core'
import typescript from 'highlight.js/lib/languages/typescript'
import xml from 'highlight.js/lib/languages/xml'
import javascript from 'highlight.js/lib/languages/javascript'

let registered = false

function ensureRegistered() {
  if (registered) return
  // TSX = TypeScript + XML (JSX tags). Order matters: register deps first.
  hljs.registerLanguage('xml', xml)
  hljs.registerLanguage('javascript', javascript)
  hljs.registerLanguage('typescript', typescript)
  registered = true
}

/** Escape then highlight; falls back to escaped plain text on failure. */
export function highlightDemoCode(source: string, language: 'tsx' | 'typescript' | 'javascript' = 'tsx'): string {
  ensureRegistered()
  const lang = language === 'tsx' ? 'typescript' : language
  try {
    // highlight.js treats JSX as typescript when xml is registered (tsx mode via sublanguage).
    // Prefer explicit 'typescript' which covers most demo sources (imports + JSX).
    return hljs.highlight(source, { language: lang, ignoreIllegals: true }).value
  } catch {
    return escapeHtml(source)
  }
}

function escapeHtml(s: string): string {
  return s
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
}
