/**
 * ChatUiDocsLayout — sidebar shell for jcode-ui component docs.
 * Separate from product DocsLayout; includes search over chat-ui docs only.
 */

import { useEffect, useMemo, useRef, useState } from 'react'
import { Link, NavLink, Outlet, useLocation, useNavigate } from 'react-router-dom'
import { CHAT_UI_NAV_TREE, searchChatUiDocs, type ChatUiSearchHit } from '../../lib/chatUiDocs'
import '../docs/docs.css'

function SearchBox() {
  const [q, setQ] = useState('')
  const [open, setOpen] = useState(false)
  const nav = useNavigate()
  const boxRef = useRef<HTMLDivElement>(null)
  const hits = useMemo(() => searchChatUiDocs(q), [q])

  useEffect(() => {
    const onDown = (e: MouseEvent) => {
      if (!boxRef.current?.contains(e.target as Node)) setOpen(false)
    }
    document.addEventListener('mousedown', onDown)
    return () => document.removeEventListener('mousedown', onDown)
  }, [])

  const go = (h: ChatUiSearchHit) => {
    setOpen(false)
    setQ('')
    nav(`/chat-ui/docs/${h.doc.slug}${h.heading ? `#${h.heading.id}` : ''}`)
  }

  return (
    <div className="docs-search" ref={boxRef}>
      <span className="docs-search-icon" aria-hidden>
        ⌕
      </span>
      <input
        value={q}
        placeholder="Search jcode-ui docs…"
        onChange={(e) => {
          setQ(e.target.value)
          setOpen(true)
        }}
        onFocus={() => setOpen(true)}
        onKeyDown={(e) => {
          if (e.key === 'Enter' && hits[0]) go(hits[0])
          if (e.key === 'Escape') setOpen(false)
        }}
      />
      {open && q.trim().length >= 2 && (
        <div className="docs-search-pop">
          {hits.length === 0 && <div className="dsp-empty">No results for “{q}”</div>}
          {hits.map((h, i) => (
            <button key={i} className="dsp-hit" type="button" onClick={() => go(h)}>
              <span className="dsp-title">
                {h.doc.title}
                {h.heading ? ` › ${h.heading.text}` : ''}
              </span>
              <span className="dsp-snippet">{h.snippet}</span>
            </button>
          ))}
        </div>
      )}
    </div>
  )
}

export default function ChatUiDocsLayout() {
  const { pathname } = useLocation()
  const [menuOpen, setMenuOpen] = useState(false)

  useEffect(() => setMenuOpen(false), [pathname])

  return (
    <div className="docs-shell container">
      <button className="docs-menu-toggle" type="button" onClick={() => setMenuOpen((v) => !v)}>
        {menuOpen ? '✕ Close' : '☰ Docs menu'}
      </button>
      <aside className={`docs-side${menuOpen ? ' open' : ''}`}>
        <SearchBox />
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
