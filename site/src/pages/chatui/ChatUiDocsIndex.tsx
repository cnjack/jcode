/**
 * ChatUiDocsIndex — landing page for /chat-ui/docs.
 */

import { Link } from 'react-router-dom'
import { CHAT_UI_NAV_TREE } from '../../lib/chatUiDocs'

export default function ChatUiDocsIndex() {
  return (
    <div className="doc-main">
      <article className="doc-article">
        <nav className="doc-crumbs" aria-label="Breadcrumb">
          <Link to="/chat-ui">chat-ui</Link>
          <span className="crumb-sep">/</span>
          <b>docs</b>
        </nav>
        <h1>jcode-ui documentation</h1>
        <p>
          React components for AI chat interfaces — streaming messages, tool calls, approvals, and
          ask-user interactions. Backend-agnostic, token-driven, and published as{' '}
          <code>jcode-ui</code> + <code>jcode-ui-core</code>.
        </p>
        <p>
          Prefer a guided start? Jump to <Link to="/chat-ui/docs/installation">Installation</Link>{' '}
          or the <Link to="/chat-ui">live playground</Link>.
        </p>

        <div className="doc-home-grid" style={{ marginTop: '2rem' }}>
          {CHAT_UI_NAV_TREE.map((node) => (
            <div key={node.entry.slug} className="doc-home-section">
              <Link to={`/chat-ui/docs/${node.entry.slug}`} className="doc-home-card">
                <b>{node.entry.title}</b>
                <span className="doc-home-meta">{node.entry.slug}</span>
              </Link>
              {node.children.length > 0 && (
                <ul className="doc-home-children">
                  {node.children.map((c) => (
                    <li key={c.slug}>
                      <Link to={`/chat-ui/docs/${c.slug}`}>{c.title}</Link>
                    </li>
                  ))}
                </ul>
              )}
            </div>
          ))}
        </div>
      </article>
    </div>
  )
}
