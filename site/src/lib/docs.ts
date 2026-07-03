import GithubSlugger from 'github-slugger'

/* ------------------------------------------------------------------
   Docs pipeline: imports the repo's docs/*.md (Just-the-Docs flavored)
   and turns them into a clean nav tree + transformed markdown.
   ------------------------------------------------------------------ */

const rawDocs = import.meta.glob('../../../docs/{*.md,overview/*.md,tools/*.md}', {
  query: '?raw',
  import: 'default',
  eager: true,
}) as Record<string, string>

/** internal drafts that never ship to the site */
const EXCLUDE = new Set([
  'index', // jekyll home — the docs landing is custom-built
  'model-research',
  'tool-search-architecture-draft',
  'usage-stats',
])

export interface DocEntry {
  slug: string // e.g. 'get-started', 'overview/agent'
  title: string
  navOrder: number
  parent?: string
  hasChildren?: boolean
  body: string // transformed markdown
  plain: string // lowercased text for search
  headings: { id: string; text: string; level: number }[]
}

export interface NavNode {
  entry: DocEntry
  children: NavNode[]
}

function parseFrontMatter(src: string): { meta: Record<string, string>; body: string } {
  if (!src.startsWith('---')) return { meta: {}, body: src }
  const end = src.indexOf('\n---', 3)
  if (end === -1) return { meta: {}, body: src }
  const meta: Record<string, string> = {}
  for (const line of src.slice(3, end).split('\n')) {
    const m = line.match(/^([a-zA-Z_]+):\s*(.*)$/)
    if (m) meta[m[1]] = m[2].trim()
  }
  return { meta, body: src.slice(end + 4).replace(/^\s*\n/, '') }
}

const CALLOUT_CLASSES = ['note', 'warning', 'important', 'new', 'highlight'] as const

function transformBody(body: string, slug: string): string {
  let out = body

  // {% link path/to.md %} → /docs/path/to
  out = out.replace(/\{%\s*link\s+([^%]+?)\s*%\}/g, (_, p: string) => {
    return '/docs/' + p.trim().replace(/\.md$/, '')
  })
  // other liquid tags — drop
  out = out.replace(/\{%[^%]*%\}/g, '')

  // kramdown callout IAL preceding a blockquote → GitHub alert marker
  for (const cls of CALLOUT_CLASSES) {
    const re = new RegExp(`^\\{:\\s*\\.${cls}[^}]*\\}\\s*\\n>`, 'gm')
    out = out.replace(re, `> [!${cls.toUpperCase()}]\n>`)
  }
  // remaining kramdown IALs (block or inline) — strip
  out = out.replace(/^\{:[^}]*\}\s*$/gm, '')
  out = out.replace(/\{:[^}]*\}/g, '')

  // asset image paths → /docs-asset/
  out = out.replace(/\]\((?:\.\.\/)*asset\//g, '](/docs-asset/')

  // strip a leading h1 that duplicates the front-matter title (we render our own)
  out = out.replace(/^#\s+.+\n+/, '')

  void slug
  return out
}

function extractHeadings(md: string) {
  const slugger = new GithubSlugger()
  const headings: { id: string; text: string; level: number }[] = []
  let inFence = false
  for (const line of md.split('\n')) {
    if (/^\s*(```|~~~)/.test(line)) inFence = !inFence
    if (inFence) continue
    const m = line.match(/^(#{2,3})\s+(.+?)\s*$/)
    if (m) {
      const text = m[2].replace(/[*_`]/g, '').replace(/\[([^\]]*)\]\([^)]*\)/g, '$1')
      headings.push({ id: slugger.slug(text), text, level: m[1].length })
    }
  }
  return headings
}

function toPlain(md: string): string {
  return md
    .replace(/```[\s\S]*?```/g, ' ')
    .replace(/[#>*_`|[\]()!-]/g, ' ')
    .replace(/\s+/g, ' ')
    .toLowerCase()
}

function buildAll(): DocEntry[] {
  const entries: DocEntry[] = []
  for (const [path, src] of Object.entries(rawDocs)) {
    const slug = path
      .replace(/^.*\/docs\//, '')
      .replace(/\.md$/, '')
    if (EXCLUDE.has(slug)) continue
    const { meta, body } = parseFrontMatter(src)
    if (meta.nav_exclude === 'true') continue
    if (!meta.title) continue // untitled drafts don't ship
    const transformed = transformBody(body, slug)
    entries.push({
      slug,
      title: meta.title,
      navOrder: Number(meta.nav_order ?? 999),
      parent: meta.parent || undefined,
      hasChildren: meta.has_children === 'true',
      body: transformed,
      plain: toPlain(transformed),
      headings: extractHeadings(transformed),
    })
  }
  return entries
}

export const DOCS: DocEntry[] = buildAll()

const byTitle = new Map(DOCS.map((d) => [d.title, d]))

export const NAV_TREE: NavNode[] = DOCS.filter((d) => !d.parent)
  .sort((a, b) => a.navOrder - b.navOrder || a.title.localeCompare(b.title))
  .map((entry) => ({
    entry,
    children: DOCS.filter((c) => c.parent === entry.title)
      .sort((a, b) => a.navOrder - b.navOrder || a.title.localeCompare(b.title))
      .map((c) => ({ entry: c, children: [] })),
  }))

/** depth-first flatten used for prev/next pagination */
export const FLAT_NAV: DocEntry[] = NAV_TREE.flatMap((n) => [
  n.entry,
  ...n.children.map((c) => c.entry),
])

export function findDoc(slug: string): DocEntry | undefined {
  return DOCS.find((d) => d.slug === slug)
}

export function breadcrumbs(doc: DocEntry): DocEntry[] {
  const trail: DocEntry[] = []
  if (doc.parent) {
    const p = byTitle.get(doc.parent)
    if (p) trail.push(p)
  }
  trail.push(doc)
  return trail
}

export interface SearchHit {
  doc: DocEntry
  snippet: string
  heading?: { id: string; text: string }
}

export function searchDocs(query: string, limit = 8): SearchHit[] {
  const q = query.trim().toLowerCase()
  if (q.length < 2) return []
  const hits: SearchHit[] = []
  for (const doc of DOCS) {
    let score = 0
    let heading: SearchHit['heading']
    if (doc.title.toLowerCase().includes(q)) score += 100
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
      hits.push({ doc, snippet: (start > 0 ? '…' : '') + raw + '…', heading })
    } else if (score > 0) {
      hits.push({ doc, snippet: doc.plain.slice(0, 90) + '…', heading })
    }
    if (score > 0) (hits[hits.length - 1] as SearchHit & { score?: number }).score = score
  }
  return hits
    .sort(
      (a, b) =>
        ((b as SearchHit & { score?: number }).score ?? 0) -
        ((a as SearchHit & { score?: number }).score ?? 0),
    )
    .slice(0, limit)
}
