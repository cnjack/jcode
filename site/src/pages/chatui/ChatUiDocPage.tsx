/**
 * ChatUiDocPage — renders a single jcode-ui doc page from CHAT_UI_DOCS.
 * Mirrors DocPage but reads from the chat-ui doc set + uses /chat-ui/docs links.
 */

import { useParams, Link } from 'react-router-dom'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import rehypeRaw from 'rehype-raw'
import rehypeSlug from 'rehype-slug'
import rehypeHighlight from 'rehype-highlight'
import { CHAT_UI_DOCS, getChatUiDoc } from '../../lib/chatUiDocs'

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
            No jcode-ui doc at <code>{slug}</code>. See the{' '}
            <Link to="/chat-ui/docs">jcode-ui docs index</Link>.
          </p>
        </article>
      </div>
    )
  }

  // prev/next within the chat-ui doc set (flat nav order).
  const idx = CHAT_UI_DOCS.findIndex((d) => d.slug === doc.slug)
  const prev = idx > 0 ? CHAT_UI_DOCS[idx - 1] : undefined
  const next = idx >= 0 && idx < CHAT_UI_DOCS.length - 1 ? CHAT_UI_DOCS[idx + 1] : undefined

  return (
    <div className="doc-main">
      <article className="doc-article">
        <nav className="doc-crumbs" aria-label="Breadcrumb">
          <Link to="/chat-ui">chat-ui</Link>
          <span className="crumb-sep">/</span>
          <Link to="/chat-ui/docs">docs</Link>
          <span className="crumb-sep">/</span>
          <b>{doc.title}</b>
        </nav>
        <h1>{doc.title}</h1>
        <ReactMarkdown
          remarkPlugins={[remarkGfm]}
          rehypePlugins={[rehypeRaw, rehypeSlug, rehypeHighlight]}
          components={{
            a({ href = '', children }) {
              if (/^https?:/.test(href)) {
                return (
                  <a href={href} target="_blank" rel="noreferrer">
                    {children}
                  </a>
                )
              }
              return <a href={href}>{children}</a>
            },
          }}
        >
          {doc.body}
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
