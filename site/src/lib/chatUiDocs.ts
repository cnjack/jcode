/**
 * chat-ui docs pipeline — SEPARATE from the jcode product docs (docs.ts).
 *
 * The jcode-ui component library is an independent, npm-published product, so
 * its documentation lives in its own nav tree and route (/chat-ui/docs/*), NOT
 * mixed into the jcode product docs (/docs/*). Supports nested sections:
 * components/, api/, guides/.
 */

import GithubSlugger from 'github-slugger'

const rawDocs = import.meta.glob('../../docs/chat-ui/**/*.md', {
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
    fm[k] =
      clean === 'true' ? true : clean === 'false' ? false : /^\d+$/.test(clean) ? Number(clean) : clean
  }
  return { fm, body: m[2] || '' }
}

function transformBody(body: string): string {
  // Leading blank lines after `---` are common — trim before stripping the H1.
  let out = body.replace(/^\s+/, '')
  // Strip leading h1 that duplicates front-matter title (page renders its own <h1>).
  out = out.replace(/^#\s+.+\n+/, '')
  // Normalize internal links that still point at product-docs style paths.
  out = out.replace(/\]\(\/docs\/chat-ui\/?/g, '](/chat-ui/docs/')
  out = out.replace(/\]\(\/chat-ui\/docs\/docs\//g, '](/chat-ui/docs/')
  return out
}

function extractHeadings(body: string): { id: string; text: string; level: number }[] {
  const headings: { id: string; text: string; level: number }[] = []
  const slugger = new GithubSlugger()
  let inFence = false
  for (const line of body.split('\n')) {
    if (/^\s*(```|~~~)/.test(line)) {
      inFence = !inFence
      continue
    }
    if (inFence) continue
    const m = line.match(/^(#{2,4})\s+(.*)$/)
    if (m) {
      const level = m[1].length
      const text = m[2].replace(/[`*_]/g, '').replace(/\[([^\]]*)\]\([^)]*\)/g, '$1').trim()
      headings.push({ id: slugger.slug(text), text, level })
    }
  }
  return headings
}

function toPlain(md: string): string {
  return md
    .replace(/```[\s\S]*?```/g, ' ')
    .replace(/<[^>]+>/g, ' ')
    .replace(/[#>*_`|[\]()!-]/g, ' ')
    .replace(/\s+/g, ' ')
    .toLowerCase()
}

const DOCS: ChatUiDocEntry[] = Object.entries(rawDocs).map(([path, raw]) => {
  // path like ../../docs/chat-ui/components/thread.md → components/thread
  const slug = path.replace(/^.*\/docs\/chat-ui\//, '').replace(/\.md$/, '')
  const { fm, body: rawBody } = parseFrontmatter(raw)
  const body = transformBody(rawBody)
  return {
    slug,
    title: String(fm.title ?? slug),
    navOrder: Number(fm.nav_order ?? 99),
    parent: fm.parent ? String(fm.parent) : undefined,
    hasChildren: Boolean(fm.has_children),
    body,
    plain: toPlain(body),
    headings: extractHeadings(body),
  }
})

DOCS.sort((a, b) => a.navOrder - b.navOrder || a.title.localeCompare(b.title))

export const CHAT_UI_DOCS: ChatUiDocEntry[] = DOCS

/** Top-level nav: docs without a parent. Section pages list their children. */
export const CHAT_UI_NAV_TREE: ChatUiNavNode[] = DOCS.filter((d) => !d.parent).map((entry) => ({
  entry,
  children: DOCS.filter((d) => d.parent === entry.title).sort(
    (a, b) => a.navOrder - b.navOrder || a.title.localeCompare(b.title),
  ),
}))

/** Depth-first flatten for prev/next pagination. */
export const CHAT_UI_FLAT_NAV: ChatUiDocEntry[] = CHAT_UI_NAV_TREE.flatMap((n) => [
  n.entry,
  ...n.children,
])

/** Resolve a slug (e.g. 'runtime' or 'components/thread') to a doc entry. */
export function getChatUiDoc(slug: string): ChatUiDocEntry | undefined {
  const clean = slug.replace(/\.md$/, '').replace(/^\/+/, '')
  return DOCS.find((d) => d.slug === clean || d.slug === clean.replace(/\/index$/, ''))
}

export interface ChatUiSearchHit {
  doc: ChatUiDocEntry
  snippet: string
  heading?: { id: string; text: string }
}

/** Lightweight search across jcode-ui docs. */
export function searchChatUiDocs(query: string, limit = 8): ChatUiSearchHit[] {
  const q = query.trim().toLowerCase()
  if (q.length < 2) return []
  const hits: (ChatUiSearchHit & { score: number })[] = []
  for (const doc of DOCS) {
    let score = 0
    let heading: ChatUiSearchHit['heading']
    if (doc.title.toLowerCase().includes(q)) score += 100
    if (doc.slug.toLowerCase().includes(q)) score += 60
    for (const h of doc.headings) {
      if (h.text.toLowerCase().includes(q)) {
        score += 40
        heading = heading ?? { id: h.id, text: h.text }
      }
    }
    const idx = doc.plain.indexOf(q)
    if (idx >= 0) {
      score += 10
      const start = Math.max(0, idx - 40)
      const raw = doc.plain.slice(start, idx + q.length + 60).trim()
      hits.push({
        doc,
        snippet: (start > 0 ? '…' : '') + raw + '…',
        heading,
        score,
      })
    } else if (score > 0) {
      hits.push({ doc, snippet: doc.plain.slice(0, 90) + '…', heading, score })
    }
  }
  return hits.sort((a, b) => b.score - a.score).slice(0, limit)
}
