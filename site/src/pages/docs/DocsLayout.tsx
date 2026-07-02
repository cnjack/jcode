import { useEffect, useMemo, useRef, useState } from 'react'
import { Link, NavLink, Outlet, useLocation, useNavigate } from 'react-router-dom'
import { NAV_TREE, searchDocs, type SearchHit } from '../../lib/docs'
import './docs.css'

function SearchBox() {
  const [q, setQ] = useState('')
  const [open, setOpen] = useState(false)
  const nav = useNavigate()
  const boxRef = useRef<HTMLDivElement>(null)
  const hits = useMemo(() => searchDocs(q), [q])

  useEffect(() => {
    const onDown = (e: MouseEvent) => {
      if (!boxRef.current?.contains(e.target as Node)) setOpen(false)
    }
    document.addEventListener('mousedown', onDown)
    return () => document.removeEventListener('mousedown', onDown)
  }, [])

  const go = (h: SearchHit) => {
    setOpen(false)
    setQ('')
    nav(`/docs/${h.doc.slug}${h.heading ? `#${h.heading.id}` : ''}`)
  }

  return (
    <div className="docs-search" ref={boxRef}>
      <span className="docs-search-icon" aria-hidden>
        ⌕
      </span>
      <input
        value={q}
        placeholder="Search docs…"
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
            <button key={i} className="dsp-hit" onClick={() => go(h)}>
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

export default function DocsLayout() {
  const { pathname } = useLocation()
  const [menuOpen, setMenuOpen] = useState(false)

  useEffect(() => setMenuOpen(false), [pathname])

  return (
    <div className="docs-shell container">
      <button className="docs-menu-toggle" onClick={() => setMenuOpen((v) => !v)}>
        {menuOpen ? '✕ Close' : '☰ Docs menu'}
      </button>
      <aside className={`docs-side${menuOpen ? ' open' : ''}`}>
        <SearchBox />
        <nav className="docs-nav">
          <NavLink to="/docs" end className="docs-nav-item docs-nav-top">
            Documentation home
          </NavLink>
          {NAV_TREE.map((node) => (
            <div key={node.entry.slug} className="docs-nav-group">
              <NavLink to={`/docs/${node.entry.slug}`} end className="docs-nav-item">
                {node.entry.title}
              </NavLink>
              {node.children.length > 0 && (
                <div className="docs-nav-children">
                  {node.children.map((c) => (
                    <NavLink key={c.entry.slug} to={`/docs/${c.entry.slug}`} end className="docs-nav-item child">
                      {c.entry.title}
                    </NavLink>
                  ))}
                </div>
              )}
            </div>
          ))}
        </nav>
        <div className="docs-side-foot">
          <Link to="/showcase">See what jcode builds →</Link>
        </div>
      </aside>
      <Outlet />
    </div>
  )
}
