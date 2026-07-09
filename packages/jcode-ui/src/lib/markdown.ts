/**
 * Markdown rendering — ported verbatim from the Vue composable
 * (`web/src/composables/markdown.ts`). marked + marked-highlight (highlight.js
 * auto-detect) + DOMPurify sanitization. Framework-agnostic; returns an HTML
 * string the consumer injects via dangerouslySetInnerHTML.
 */

import { Marked } from 'marked'
import { markedHighlight } from 'marked-highlight'
import hljs from 'highlight.js'
import DOMPurify from 'dompurify'

const marked = new Marked(
  markedHighlight({
    emptyLangClass: 'hljs',
    langPrefix: 'hljs language-',
    highlight(code: string, lang: string) {
      if (lang && hljs.getLanguage(lang)) {
        return hljs.highlight(code, { language: lang }).value
      }
      return hljs.highlightAuto(code).value
    },
  }),
)

marked.setOptions({
  breaks: true,
  gfm: true,
})

/** Wrap each <table> so wide GFM tables scroll inside a framed container. */
function wrapTables(html: string): string {
  return html
    .replace(/<table(\s[^>]*)?>/gi, '<div class="jcode-md-table-wrap"><table$1>')
    .replace(/<\/table>/gi, '</table></div>')
}

export function renderMarkdown(text: string): string {
  const raw = marked.parse(text) as string
  const withTables = wrapTables(typeof raw === 'string' ? raw : String(raw))
  return DOMPurify.sanitize(withTables, {
    ADD_ATTR: ['target', 'class'],
    ADD_TAGS: ['mark'],
  })
}
