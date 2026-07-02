import { useEffect, useMemo, useState, isValidElement, type ReactNode } from 'react'
import { Link, useLocation, useParams } from 'react-router-dom'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import rehypeSlug from 'rehype-slug'
import rehypeRaw from 'rehype-raw'
import rehypeHighlight from 'rehype-highlight'
import CopyButton from '../../components/CopyButton'
import { FLAT_NAV, NAV_TREE, breadcrumbs, findDoc, type DocEntry } from '../../lib/docs'

/* ---------- helpers ---------- */

function textOf(node: ReactNode): string {
  if (node == null) return ''
  if (typeof node === 'string' || typeof node === 'number') return String(node)
  if (Array.isArray(node)) return node.map(textOf).join('')
  if (isValidElement(node)) return textOf((node.props as { children?: ReactNode }).children)
  return ''
}

const CALLOUT_META: Record<string, { icon: string; label: string }> = {
  NOTE: { icon: '✏️', label: 'Note' },
  WARNING: { icon: '⚠️', label: 'Warning' },
  IMPORTANT: { icon: '📌', label: 'Important' },
  NEW: { icon: '✨', label: 'New' },
  HIGHLIGHT: { icon: '💡', label: 'Tip' },
}

function resolveDocHref(href: string, currentSlug: string): { to?: string; external?: boolean; anchor?: boolean } {
  if (/^[a-z]+:/i.test(href)) return { external: true }
  if (href.startsWith('#')) return { anchor: true }
  if (href.startsWith('/')) return { to: href }
  // relative to the current doc's directory
  const dir = currentSlug.includes('/') ? currentSlug.slice(0, currentSlug.lastIndexOf('/') + 1) : ''
  const [pathPart, hash = ''] = href.split('#')
  const url = new URL(pathPart, `https://x/${dir}`)
  const clean = url.pathname.replace(/^\//, '').replace(/\.(md|html)$/, '').replace(/\/$/, '')
  return { to: `/docs/${clean}${hash ? `#${hash}` : ''}` }
}

/* ---------- On this page (scrollspy) ---------- */

function OnThisPage({ doc }: { doc: DocEntry }) {
  const [active, setActive] = useState('')
  useEffect(() => {
    const els = doc.headings
      .map((h) => document.getElementById(h.id))
      .filter((el): el is HTMLElement => !!el)
    if (els.length === 0) return
    const io = new IntersectionObserver(
      (entries) => {
        const visible = entries.filter((e) => e.isIntersecting)
        if (visible.length > 0) {
          setActive(visible[0].target.id)
        }
      },
      { rootMargin: '-80px 0px -70% 0px', threshold: 0 },
    )
    els.forEach((el) => io.observe(el))
    return () => io.disconnect()
  }, [doc])

  if (doc.headings.length < 2) return <div />
  return (
    <nav className="doc-toc" aria-label="On this page">
      <div className="doc-toc-label">On this page</div>
      <ul>
        {doc.headings.map((h) => (
          <li key={h.id} className={`lv${h.level}${active === h.id ? ' active' : ''}`}>
            <a href={`#${h.id}`}>{h.text}</a>
          </li>
        ))}
      </ul>
    </nav>
  )
}

/* ---------- docs landing ---------- */

function DocsHome() {
  return (
    <article className="doc-article doc-home">
      <span className="mono-label">documentation</span>
      <h1>Learn jcode</h1>
      <p className="doc-home-lead">
        Everything about installing, configuring and working with the jcode agent —
        from your first prompt to multi-agent teams over SSH.
      </p>
      <div className="doc-home-grid">
        {NAV_TREE.map((n) => (
          <Link key={n.entry.slug} to={`/docs/${n.entry.slug}`} className="doc-home-card">
            <h3>{n.entry.title}</h3>
            <p>
              {n.children.length > 0
                ? n.children
                    .slice(0, 4)
                    .map((c) => c.entry.title)
                    .join(' · ') + (n.children.length > 4 ? ' …' : '')
                : n.entry.plain.slice(0, 110) + '…'}
            </p>
          </Link>
        ))}
      </div>
    </article>
  )
}

/* ---------- main ---------- */

export default function DocPage() {
  const params = useParams()
  const { hash } = useLocation()
  const slug = (params['*'] ?? '').replace(/\/$/, '')
  const doc = useMemo(() => (slug ? findDoc(slug) : undefined), [slug])

  // jump to hash targets after content mounts
  useEffect(() => {
    if (!hash) return
    const id = decodeURIComponent(hash.slice(1))
    const t = setTimeout(() => {
      document.getElementById(id)?.scrollIntoView({ block: 'start' })
    }, 50)
    return () => clearTimeout(t)
  }, [hash, slug])

  if (!slug) return <DocsHome />

  if (!doc) {
    return (
      <article className="doc-article">
        <h1>Page not found</h1>
        <p>
          No doc at <code>{slug}</code>. Head back to the <Link to="/docs">docs home</Link>.
        </p>
      </article>
    )
  }

  const trail = breadcrumbs(doc)
  const flatIdx = FLAT_NAV.findIndex((d) => d.slug === doc.slug)
  const prev = flatIdx > 0 ? FLAT_NAV[flatIdx - 1] : undefined
  const next = flatIdx >= 0 && flatIdx < FLAT_NAV.length - 1 ? FLAT_NAV[flatIdx + 1] : undefined

  return (
    <div className="doc-main">
      <article className="doc-article">
        <nav className="doc-crumbs" aria-label="Breadcrumb">
          <Link to="/docs">Docs</Link>
          {trail.map((t) => (
            <span key={t.slug}>
              <span className="crumb-sep">/</span>
              {t.slug === doc.slug ? <b>{t.title}</b> : <Link to={`/docs/${t.slug}`}>{t.title}</Link>}
            </span>
          ))}
        </nav>
        <h1>{doc.title}</h1>
        <ReactMarkdown
          remarkPlugins={[remarkGfm]}
          rehypePlugins={[rehypeRaw, rehypeSlug, rehypeHighlight]}
          components={{
            a({ href = '', children }) {
              const r = resolveDocHref(href, doc.slug)
              if (r.external)
                return (
                  <a href={href} target="_blank" rel="noreferrer">
                    {children}
                  </a>
                )
              if (r.anchor) return <a href={href}>{children}</a>
              return <Link to={r.to!}>{children}</Link>
            },
            blockquote({ children }) {
              const txt = textOf(children).trim()
              const m = txt.match(/^\[!(\w+)\]/)
              if (m && CALLOUT_META[m[1]]) {
                const meta = CALLOUT_META[m[1]]
                return (
                  <div className={`doc-callout callout-${m[1].toLowerCase()}`}>
                    <span className="callout-icon" aria-hidden>
                      {meta.icon}
                    </span>
                    <div className="callout-body" data-strip-marker>
                      {children}
                    </div>
                  </div>
                )
              }
              return <blockquote>{children}</blockquote>
            },
            p({ children }) {
              // strip the [!NOTE] marker text from the first paragraph of a callout
              const arr = Array.isArray(children) ? children : [children]
              if (typeof arr[0] === 'string') {
                const m = (arr[0] as string).match(/^\[!\w+\]\s*/)
                if (m) {
                  const rest = [(arr[0] as string).slice(m[0].length), ...arr.slice(1)]
                  return <p>{rest}</p>
                }
              }
              return <p>{children}</p>
            },
            pre({ children }) {
              const code = textOf(children).replace(/\n$/, '')
              return (
                <div className="doc-codeblock">
                  <CopyButton text={code} />
                  <pre>{children}</pre>
                </div>
              )
            },
            table({ children }) {
              return (
                <div className="doc-table-scroll">
                  <table>{children}</table>
                </div>
              )
            },
            img({ src = '', alt = '' }) {
              return <img src={src} alt={alt} loading="lazy" className="doc-img" />
            },
          }}
        >
          {doc.body}
        </ReactMarkdown>

        <div className="doc-pager">
          {prev ? (
            <Link to={`/docs/${prev.slug}`} className="pager-card">
              <span>← Previous</span>
              <b>{prev.title}</b>
            </Link>
          ) : (
            <div />
          )}
          {next ? (
            <Link to={`/docs/${next.slug}`} className="pager-card next">
              <span>Next →</span>
              <b>{next.title}</b>
            </Link>
          ) : (
            <div />
          )}
        </div>
      </article>
      <OnThisPage doc={doc} />
    </div>
  )
}
