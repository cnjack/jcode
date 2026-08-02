import { useState } from 'react'
import { NavLink } from 'react-router-dom'
import Logo from './Logo'
import { preloadChatUIPage } from '../lib/routePreloads'

const GITHUB = 'https://github.com/cnjack/jcode'

export default function SiteNav() {
  const [open, setOpen] = useState(false)
  return (
    <nav className="site-nav">
      <div className="site-nav-inner">
        <Logo />
        <ul className={`site-nav-links${open ? ' open' : ''}`} onClick={() => setOpen(false)}>
          <li><NavLink to="/desktop">Desktop</NavLink></li>
          <li><NavLink to="/cloud">Cloud</NavLink></li>
          <li><NavLink to="/cli">CLI</NavLink></li>
          <li>
            <NavLink
              to="/chat-ui"
              onPointerEnter={preloadChatUIPage}
              onFocus={preloadChatUIPage}
            >
              Chat UI
            </NavLink>
          </li>
          <li><NavLink to="/showcase">Showcase</NavLink></li>
          <li><NavLink to="/docs">Docs</NavLink></li>
        </ul>
        <div className="site-nav-spacer" />
        <a className="nav-github" href={GITHUB} target="_blank" rel="noreferrer">
          <svg width="16" height="16" viewBox="0 0 16 16" fill="currentColor" aria-hidden>
            <path d="M8 0C3.58 0 0 3.58 0 8c0 3.54 2.29 6.53 5.47 7.59.4.07.55-.17.55-.38 0-.19-.01-.82-.01-1.49-2.01.37-2.53-.49-2.69-.94-.09-.23-.48-.94-.82-1.13-.28-.15-.68-.52-.01-.53.63-.01 1.08.58 1.23.82.72 1.21 1.87.87 2.33.66.07-.52.28-.87.51-1.07-1.78-.2-3.64-.89-3.64-3.95 0-.87.31-1.59.82-2.15-.08-.2-.36-1.02.08-2.12 0 0 .67-.21 2.2.82a7.5 7.5 0 0 1 4 0c1.53-1.04 2.2-.82 2.2-.82.44 1.1.16 1.92.08 2.12.51.56.82 1.27.82 2.15 0 3.07-1.87 3.75-3.65 3.95.29.25.54.73.54 1.48 0 1.07-.01 1.93-.01 2.2 0 .21.15.46.55.38A8.01 8.01 0 0 0 16 8c0-4.42-3.58-8-8-8Z" />
          </svg>
          <span>GitHub</span>
        </a>
        <button
          className="nav-burger"
          aria-label="Toggle menu"
          aria-expanded={open}
          onClick={() => setOpen((v) => !v)}
        >
          {open ? '✕' : '☰'}
        </button>
      </div>
    </nav>
  )
}
