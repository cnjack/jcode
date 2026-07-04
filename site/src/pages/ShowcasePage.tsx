import { useEffect } from 'react'
import { Link } from 'react-router-dom'
import Reveal from '../components/Reveal'
import { CHALLENGES, SPRINTS, type ShowcaseEntry } from '../data/showcase'
import './showcase.css'

function ChallengeCard({ p, i }: { p: ShowcaseEntry; i: number }) {
  return (
    <Reveal delay={(i % 2) * 80}>
      <Link to={`/showcase/${p.id}`} className="chal-card" style={{ ['--pj' as string]: p.accent }}>
        <div className="chal-preview">
          <iframe
            src={p.src}
            title={p.title}
            loading="lazy"
            tabIndex={-1}
            sandbox="allow-scripts allow-same-origin"
          />
          <div className="chal-preview-veil">
            <span className="btn btn-accent btn-sm">Open project →</span>
          </div>
        </div>
        <div className="chal-body">
          <div className="chal-meta">
            <span className="chal-kind">challenge</span>
            <span className="chal-build">
              {p.build.model} · {p.build.rounds} rounds · agent-built
            </span>
          </div>
          <h3>{p.title}</h3>
          <p>{p.tagline}</p>
          <div className="chal-tags">
            {p.tags.map((t) => (
              <span key={t}>{t}</span>
            ))}
          </div>
        </div>
      </Link>
    </Reveal>
  )
}

export default function ShowcasePage() {
  // A sandboxed demo iframe calls focus() on load (for keyboard control), which
  // pins the whole page scrolled down to it. Focus leaving the host into an
  // iframe fires window 'blur'; when it does, drop the iframe's focus and
  // restore the user's scroll position (0 until they actually scroll).
  useEffect(() => {
    let userY = 0
    const track = () => {
      userY = window.scrollY
    }
    const onBlur = () => {
      requestAnimationFrame(() => {
        const a = document.activeElement as HTMLElement | null
        if (a?.tagName === 'IFRAME') {
          a.blur()
          window.scrollTo(0, userY)
        }
      })
    }
    window.addEventListener('wheel', track, { passive: true })
    window.addEventListener('touchmove', track, { passive: true })
    window.addEventListener('blur', onBlur)
    return () => {
      window.removeEventListener('wheel', track)
      window.removeEventListener('touchmove', track)
      window.removeEventListener('blur', onBlur)
    }
  }, [])

  return (
    <main className="showcase-page">
      <header className="showcase-hero">
        <div className="container">
          <Reveal>
            <span className="mono-label">built by the agent, verified by machines</span>
          </Reveal>
          <Reveal delay={60}>
            <h1>The jcode Showcase</h1>
          </Reveal>
          <Reveal delay={120}>
            <p>
              Every project below was implemented end-to-end by the jcode agent running{' '}
              <em>unattended</em> — driven only by written briefs over multiple rounds,
              verified with deterministic checks and browser screenshots. No human wrote or
              edited a single line.
            </p>
          </Reveal>
        </div>
      </header>

      <section className="container">
        <Reveal>
          <div className="showcase-sec-head">
            <h2>Challenges</h2>
            <p>
              Multi-hour briefs — three or more feature rounds each, with review and fixes
              between rounds. The kind of work you'd hand a contractor.
            </p>
          </div>
        </Reveal>
        <div className="chal-grid">
          {CHALLENGES.map((p, i) => (
            <ChallengeCard key={p.id} p={p} i={i} />
          ))}
        </div>
      </section>

      <section className="container sprints-sec">
        <Reveal>
          <div className="showcase-sec-head">
            <h2>Sprints</h2>
            <p>
              Single-prompt tasks from jcode's unattended evaluation suite — one brief, one
              pass, shipped as-is.
            </p>
          </div>
        </Reveal>
        <div className="sprint-grid">
          {SPRINTS.map((p, i) => (
            <Reveal key={p.id} delay={(i % 3) * 60}>
              <Link
                to={`/showcase/${p.id}`}
                className="sprint-card"
                style={{ ['--pj' as string]: p.accent }}
              >
                <div className="sprint-preview">
                  <iframe
                    src={p.src}
                    title={p.title}
                    loading="lazy"
                    tabIndex={-1}
                    sandbox="allow-scripts allow-same-origin"
                  />
                </div>
                <h4>{p.title}</h4>
                <p>{p.tagline}</p>
              </Link>
            </Reveal>
          ))}
        </div>
      </section>

      <section className="container methodology">
        <Reveal>
          <div className="method-card">
            <h3>How this works</h3>
            <p>
              jcode runs headlessly over ACP (the Agent Client Protocol) inside an isolated
              sandbox. A commander process sends it a written brief; jcode plans, writes
              code, runs commands and finishes the turn. Deterministic oracles and a real
              browser then verify the result — page loads, no console errors, features
              actually work. Failures get written up and sent back as the next round's
              brief. What you see here is the final state of that loop.
            </p>
            <p className="method-links">
              <Link to="/docs/overview/agent">How the agent works →</Link>
            </p>
          </div>
        </Reveal>
      </section>
    </main>
  )
}
