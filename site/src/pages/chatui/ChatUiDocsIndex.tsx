/**
 * ChatUiDocsIndex — the landing page for /chat-ui/docs. Lists every jcode-ui
 * doc as cards (like the jcode docs home, but only the chat-ui set).
 */

import { Link } from 'react-router-dom'
import { CHAT_UI_DOCS } from '../../lib/chatUiDocs'

export default function ChatUiDocsIndex() {
  return (
    <div className="doc-main">
      <article className="doc-article">
        <nav className="doc-crumbs">
          <Link to="/chat-ui">chat-ui</Link>
          <span className="crumb-sep">/</span>
          <b>docs</b>
        </nav>
        <h1>jcode-ui documentation</h1>
        <p className="doc-lede">
          React components for AI chat interfaces — streaming, tool calls, approvals, ask-user flows.
          Backend-agnostic, token-driven, tree-shakeable.
        </p>
        <div className="doc-home-grid">
          {CHAT_UI_DOCS.filter((d) => d.slug !== 'index').map((d) => (
            <Link key={d.slug} to={`/chat-ui/docs/${d.slug}`} className="doc-home-card">
              <h3>{d.title}</h3>
              <p>{blurb(d.slug)}</p>
            </Link>
          ))}
        </div>
      </article>
    </div>
  )
}

function blurb(slug: string): string {
  switch (slug) {
    case 'runtime':
      return 'The ChatRuntime contract + ExternalStoreRuntime — connect any Redux-shaped store.'
    case 'primitives':
      return 'Headless components: Thread, MessageView, Composer, ToolCallView, ApprovalBlock, AskUserBlock.'
    case 'tool-renderers':
      return 'The tool-call plugin registry. 9 default renderers + writing your own.'
    case 'theming':
      return 'CSS custom properties for every color/radius/shadow. Light, dark, generated themes.'
    case 'components':
      return 'Component reference: props, slots, and examples for every styled component.'
    default:
      return ''
  }
}
