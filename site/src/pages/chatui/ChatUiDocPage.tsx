/**
 * ChatUiDocPage — renders a single jcode-ui doc page from CHAT_UI_DOCS.
 * Live previews ship with this (route-lazy) chunk.
 */

import { Suspense, isValidElement, useMemo, type HTMLAttributes, type ReactNode } from 'react'
import { useParams, Link } from 'react-router-dom'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import rehypeRaw from 'rehype-raw'
import rehypeSlug from 'rehype-slug'
import rehypeHighlight from 'rehype-highlight'
import CopyButton from '../../components/CopyButton'
import { CHAT_UI_FLAT_NAV, getChatUiDoc } from '../../lib/chatUiDocs'
import '../../playground/component-demo.css'

function textOf(node: ReactNode): string {
  if (node == null) return ''
  if (typeof node === 'string' || typeof node === 'number') return String(node)
  if (Array.isArray(node)) return node.map(textOf).join('')
  if (isValidElement(node)) return textOf((node.props as { children?: ReactNode }).children)
  return ''
}

/** Drop a leading ATX H1 that duplicates frontmatter `title` (page already renders <h1>). */
function stripDuplicateLeadingH1(body: string, title: string): string {
  const escaped = title.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
  return body.replace(new RegExp(`^#\\s+${escaped}\\s*\\n+`, 'i'), '')
}

// jcode-ui + demos. Imported eagerly: this page is already a lazy ROUTE chunk,
// and a nested React.lazy proved fragile (Suspense occasionally never settled
// after dep re-optimization). The demos ship with the docs route bundle.
import { ComponentDemo } from '../../playground/ComponentDemo'

function rewriteInternalHref(href: string): string {
  if (/^https?:/.test(href) || href.startsWith('#') || href.startsWith('mailto:')) return href
  if (href.startsWith('/docs/chat-ui')) {
    return href.replace('/docs/chat-ui', '/chat-ui/docs')
  }
  if (href.startsWith('/chat-ui/docs/')) return href
  if (href.startsWith('/chat-ui')) return href
  if (!href.startsWith('/')) {
    const clean = href.replace(/^\.\//, '').replace(/\.md$/, '')
    return `/chat-ui/docs/${clean}`
  }
  return href
}

function DemoSlot({ id, height, showCode }: { id: string; height?: number; showCode: boolean }) {
  return (
    <Suspense
      fallback={
        <div className="jcode-demo jcode-demo-placeholder" style={{ minHeight: height ?? 160 }}>
          <div className="jcode-demo-chrome">
            <span className="jcode-demo-dot red" />
            <span className="jcode-demo-dot yellow" />
            <span className="jcode-demo-dot green" />
            <span className="jcode-demo-label">loading preview…</span>
          </div>
          <div className="jcode-demo-placeholder-body" />
        </div>
      }
    >
      <ComponentDemo id={id} height={height} showCode={showCode} />
    </Suspense>
  )
}

function readDataAttr(
  props: Record<string, unknown>,
  kebab: string,
  camel: string,
): string | undefined {
  const nodeProps = (props.node as { properties?: Record<string, unknown> } | undefined)?.properties
  const v = props[kebab] ?? props[camel] ?? nodeProps?.[kebab] ?? nodeProps?.[camel]
  if (v == null || v === '') return undefined
  return String(v)
}

export default function ChatUiDocPage() {
  const { '*': splat = '' } = useParams()
  const slug = splat.replace(/\.md$/, '').replace(/^chat-ui\//, '')
  const doc = getChatUiDoc(slug)

  if (!doc) {
    return (
      <div className="doc-main">
        <article className="doc-article">
          <h1>Doc not found</h1>
          <p>
            No jcode-ui doc at <code>{slug}</code>. See the <Link to="/chat-ui/docs">jcode-ui docs index</Link>.
          </p>
        </article>
      </div>
    )
  }

  const idx = CHAT_UI_FLAT_NAV.findIndex((d) => d.slug === doc.slug)
  const prev = idx > 0 ? CHAT_UI_FLAT_NAV[idx - 1] : undefined
  const next = idx >= 0 && idx < CHAT_UI_FLAT_NAV.length - 1 ? CHAT_UI_FLAT_NAV[idx + 1] : undefined
  const body = useMemo(() => stripDuplicateLeadingH1(doc.body, doc.title), [doc.body, doc.title])

  return (
    <div className="doc-main">
      <article className="doc-article">
        <nav className="doc-crumbs" aria-label="Breadcrumb">
          <Link to="/chat-ui">chat-ui</Link>
          <span className="crumb-sep">/</span>
          <Link to="/chat-ui/docs">docs</Link>
          {doc.parent && (
            <>
              <span className="crumb-sep">/</span>
              <span>{doc.parent}</span>
            </>
          )}
          <span className="crumb-sep">/</span>
          <b>{doc.title}</b>
        </nav>
        <h1>{doc.title}</h1>
        <ReactMarkdown
          remarkPlugins={[remarkGfm]}
          rehypePlugins={[rehypeRaw, rehypeSlug, rehypeHighlight]}
          components={{
            a({ href = '', children }) {
              const to = rewriteInternalHref(href)
              if (/^https?:/.test(to)) {
                return (
                  <a href={to} target="_blank" rel="noreferrer">
                    {children}
                  </a>
                )
              }
              if (to.startsWith('/')) {
                return <Link to={to}>{children}</Link>
              }
              return <a href={to}>{children}</a>
            },
            // Navy code card + copy button (same pattern as product DocPage).
            pre({ children }) {
              const code = textOf(children).replace(/\n$/, '')
              return (
                <div className="doc-codeblock">
                  <CopyButton text={code} />
                  <pre>{children}</pre>
                </div>
              )
            },
            div(props) {
              const raw = props as Record<string, unknown>
              const demoId = readDataAttr(raw, 'data-jcode-demo', 'dataJcodeDemo')
              if (demoId) {
                const hRaw = readDataAttr(raw, 'data-height', 'dataHeight')
                const height = hRaw != null ? Number(hRaw) : undefined
                const codeAttr = readDataAttr(raw, 'data-code', 'dataCode') ?? 'true'
                const showCode = codeAttr !== 'false' && codeAttr !== '0'
                return (
                  <DemoSlot
                    id={demoId}
                    height={Number.isFinite(height) ? height : undefined}
                    showCode={showCode}
                  />
                )
              }
              const { node: _node, ...rest } = raw
              return <div {...(rest as HTMLAttributes<HTMLDivElement>)} />
            },
          }}
        >
          {body}
        </ReactMarkdown>
        <nav className="doc-pager">
          {prev ? (
            <Link to={`/chat-ui/docs/${prev.slug}`} className="pager-card">
              <span className="pager-label">← Previous</span>
              <b>{prev.title}</b>
            </Link>
          ) : (
            <span />
          )}
          {next ? (
            <Link to={`/chat-ui/docs/${next.slug}`} className="pager-card next">
              <span className="pager-label">Next →</span>
              <b>{next.title}</b>
            </Link>
          ) : (
            <span />
          )}
        </nav>
      </article>
    </div>
  )
}
