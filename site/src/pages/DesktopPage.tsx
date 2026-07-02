import { Suspense, lazy } from 'react'
import { Link } from 'react-router-dom'
import Reveal from '../components/Reveal'
import Replica from '../components/desktop/Replica'
import './desktop.css'

const DesktopDemoSection = lazy(() => import('../components/desktop/DesktopDemoSection'))

const RELEASES = 'https://github.com/cnjack/jcode/releases'

export default function DesktopPage() {
  return (
    <main className="desktop-page">
      <header className="dk-hero">
        <div className="container">
          <Reveal>
            <span className="mono-label">jcode desktop</span>
          </Reveal>
          <Reveal delay={60}>
            <h1>
              The agent, in a <em>real window</em>.
            </h1>
          </Reveal>
          <Reveal delay={120}>
            <p className="dk-hero-sub">
              A native app built with Tauri around the exact same engine as the CLI — plus
              system notifications, a menu-bar tray, workspaces, automations and channels.
              Nothing is reimplemented; everything is native.
            </p>
          </Reveal>
          <Reveal delay={180}>
            <div className="dk-hero-actions">
              <a className="btn btn-accent" href={RELEASES} target="_blank" rel="noreferrer">
                ↓ Download for macOS
              </a>
              <a className="btn btn-ghost" href={RELEASES} target="_blank" rel="noreferrer">
                Windows &amp; Linux builds
              </a>
            </div>
          </Reveal>
          <Reveal delay={240}>
            <p className="dk-hero-note">free · open source · auto-updates via GitHub releases</p>
          </Reveal>
        </div>
      </header>

      <section className="dk-replica-sec">
        <div className="container">
          <Reveal>
            <Replica />
          </Reveal>
        </div>
      </section>

      <section className="dk-demo-sec">
        <div className="container">
          <Reveal>
            <div className="section-head">
              <span className="mono-label">20 seconds, start to finish</span>
              <h2>Watch a task run</h2>
              <p className="lead">
                Type what you want, hit send, and follow every tool call, diff and test run
                as it happens.
              </p>
            </div>
          </Reveal>
          <Reveal>
            <Suspense fallback={<div className="demo-placeholder">[ loading demo… ]</div>}>
              <DesktopDemoSection />
            </Suspense>
          </Reveal>
        </div>
      </section>

      <section className="dk-features">
        <div className="container">
          <div className="dk-feature-grid">
            <Reveal>
              <div className="dk-feature">
                <span className="mono-label">native</span>
                <h3>Feels like it belongs</h3>
                <p>
                  System notifications when the agent needs you. A tray icon that keeps
                  sessions alive. Single-instance focus, window-state memory and a native
                  folder picker.
                </p>
              </div>
            </Reveal>
            <Reveal delay={80}>
              <div className="dk-feature">
                <span className="mono-label">automations</span>
                <h3>Agents on a schedule</h3>
                <p>
                  Define recurring jobs — nightly dependency audits, changelog drafts, test
                  triage — and let jcode run them unattended while you sleep.
                </p>
              </div>
            </Reveal>
            <Reveal delay={160}>
              <div className="dk-feature">
                <span className="mono-label">channels</span>
                <h3>Step away, stay in the loop</h3>
                <p>
                  Link WeChat and get pinged when a task finishes or needs approval — and
                  send the agent new prompts straight from your phone.{' '}
                  <Link to="/docs/overview/channels">How channels work →</Link>
                </p>
              </div>
            </Reveal>
          </div>
        </div>
      </section>

      <section className="dk-arch">
        <div className="container dk-arch-grid">
          <Reveal>
            <div>
              <span className="mono-label">architecture</span>
              <h2>Not a second implementation</h2>
              <p>
                The desktop app is the same Go backend you run in the terminal, shipped as a
                sidecar process inside a Tauri shell. The Rust layer picks a free loopback
                port, spawns <code>jcode web</code>, health-polls it and renders it in a
                WebView — byte-for-byte the same UI, with native APIs bridged over IPC.
              </p>
              <p>
                <Link to="/docs/desktop">Read the architecture docs →</Link>
              </p>
            </div>
          </Reveal>
          <Reveal delay={100}>
            <pre className="dk-arch-diagram">{`┌────────── jcode.app (Tauri) ──────────┐
│ Rust shell                            │
│  ├─ picks a free loopback port        │
│  ├─ spawns sidecar: jcode web --port N │
│  ├─ tray · single-instance · state    │
│  └─ kills the sidecar on exit         │
│                                       │
│ WebView ──HTTP/WS──▶ 127.0.0.1:N      │
│                on the Go server        │
└───────────────────────────────────────┘`}</pre>
          </Reveal>
        </div>
      </section>

      <section className="dk-cta">
        <div className="container">
          <Reveal>
            <h2>Put an agent on your dock.</h2>
          </Reveal>
          <Reveal delay={80}>
            <div className="dk-hero-actions center">
              <a className="btn btn-accent" href={RELEASES} target="_blank" rel="noreferrer">
                ↓ Download the desktop app
              </a>
              <Link className="btn btn-ghost" to="/cli">
                Prefer the terminal? →
              </Link>
            </div>
          </Reveal>
        </div>
      </section>
    </main>
  )
}
