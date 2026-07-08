/**
 * chat-ui docs pipeline — SEPARATE from the jcode product docs (docs.ts).
 *
 * The jcode-ui component library is an independent, npm-published product, so
 * its documentation lives in its own nav tree and route (/chat-ui/docs/*), NOT
 * mixed into the jcode product docs (/docs/*). This file mirrors docs.ts but
 * loads only site/docs/chat-ui/*.md and builds an independent nav.
 */

import GithubSlugger from 'github-slugger'

const rawDocs = import.meta.glob('../../docs/chat-ui/*.md', {
  query: '?raw',
  import: 'default',
  eager: true,
}) as Record<string, string>

export interface ChatUiDocEntry {
  slug: string
  title: string
  navOrder: number
  parent?: string
  hasChildren?: boolean
  body: string
  plain: string
  headings: { id: string; text: string; level: number }[]
}

export interface ChatUiNavNode {
  entry: ChatUiDocEntry
  /** Child docs (single level — chat-ui docs don't nest deeper). */
  children: ChatUiDocEntry[]
}

function parseFrontmatter(raw: string): { fm: Record<string, string | number | boolean>; body: string } {
  const m = raw.match(/^---\n([\s\S]*?)\n---\n([\s\S]*)$/)
  if (!m) return { fm: {}, body: raw }
  const fm: Record<string, string | number | boolean> = {}
  for (const line of m[1].split('\n')) {
    const mm = line.match(/^(\w+):\s*(.*)$/)
    if (!mm) continue
    const [, k, v] = mm
    const clean = (v || '').replace(/["']/g, '').trim()
    fm[k] = clean === 'true' ? true : clean === 'false' ? false : /^\d+$/.test(clean) ? Number(clean) : clean
  }
  return { fm, body: m[2] || '' }
}

function transformMarkdown(body: string): { html: string; headings: { id: string; text: string; level: number }[] } {
  // Minimal transform: the DocPage renders markdown via react-markdown, so we
  // only need to extract headings for the in-page ToC. The body is passed
  // through as-is.
  const headings: { id: string; text: string; level: number }[] = []
  const slugger = new GithubSlugger()
  for (const line of body.split('\n')) {
    const m = line.match(/^(#{2,4})\s+(.*)$/)
    if (m) {
      const level = m[1].length
      const text = m[2].replace(/[`*_]/g, '').trim()
      headings.push({ id: slugger.slug(text), text, level })
    }
  }
  return { html: body, headings }
}

const DOCS: ChatUiDocEntry[] = Object.entries(rawDocs).map(([path, raw]) => {
  const slug = path.replace('../../docs/chat-ui/', '').replace(/\.md$/, '')
  const { fm, body } = parseFrontmatter(raw)
  const { headings } = transformMarkdown(body)
  return {
    slug,
    title: String(fm.title ?? slug),
    navOrder: Number(fm.nav_order ?? 99),
    parent: fm.parent ? String(fm.parent) : undefined,
    hasChildren: Boolean(fm.has_children),
    body,
    plain: body.toLowerCase(),
    headings,
  }
})

DOCS.sort((a, b) => a.navOrder - b.navOrder)

export const CHAT_UI_DOCS: ChatUiDocEntry[] = DOCS

export const CHAT_UI_NAV_TREE: ChatUiNavNode[] = DOCS.filter((d) => !d.parent || d.hasChildren).map(
  (entry) => ({
    entry,
    children: DOCS.filter((d) => d.parent === entry.title),
  }),
)

/** Resolve a slug (e.g. 'runtime') to a doc entry, or undefined. */
export function getChatUiDoc(slug: string): ChatUiDocEntry | undefined {
  return DOCS.find((d) => d.slug === slug || d.slug === slug.replace(/\.md$/, ''))
}
