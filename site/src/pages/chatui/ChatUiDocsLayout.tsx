/**
 * ChatUiDocsLayout — the sidebar shell for jcode-ui component docs.
 *
 * SEPARATE from the jcode product docs (DocsLayout). The jcode-ui library is
 * an independent product, so its docs have their own nav tree (CHAT_UI_NAV_TREE)
 * and route prefix (/chat-ui/docs/*). The sidebar links back to the chat-ui
 * landing page and the npm/github, not to the jcode product docs.
 */

import { useEffect, useState } from 'react'
import { Link, NavLink, Outlet, useLocation } from 'react-router-dom'
import { CHAT_UI_NAV_TREE } from '../../lib/chatUiDocs'
import '../docs/docs.css'

export default function ChatUiDocsLayout() {
  const { pathname } = useLocation()
  const [menuOpen, setMenuOpen] = useState(false)

  useEffect(() => setMenuOpen(false), [pathname])

  return (
    <div className="docs-shell container">
      <button className="docs-menu-toggle" onClick={() => setMenuOpen((v) => !v)}>
        {menuOpen ? '✕ Close' : '☰ Docs menu'}
      </button>
      <aside className={`docs-side${menuOpen ? ' open' : ''}`}>
        <nav className="docs-nav">
          <NavLink to="/chat-ui/docs" end className="docs-nav-item docs-nav-top">
            jcode-ui docs
          </NavLink>
          {CHAT_UI_NAV_TREE.map((node) => (
            <div key={node.entry.slug} className="docs-nav-group">
              <NavLink to={`/chat-ui/docs/${node.entry.slug}`} end className="docs-nav-item">
                {node.entry.title}
              </NavLink>
              {node.children.length > 0 && (
                <div className="docs-nav-children">
                  {node.children.map((c) => (
                    <NavLink key={c.slug} to={`/chat-ui/docs/${c.slug}`} end className="docs-nav-item child">
                      {c.title}
                    </NavLink>
                  ))}
                </div>
              )}
            </div>
          ))}
        </nav>
        <div className="docs-side-foot">
          <Link to="/chat-ui">← Back to chat-ui overview</Link>
          <br />
          <a href="https://www.npmjs.com/package/jcode-ui" target="_blank" rel="noreferrer">
            jcode-ui on npm →
          </a>
        </div>
      </aside>
      <Outlet />
    </div>
  )
}
