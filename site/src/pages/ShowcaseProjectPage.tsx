import { useRef, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { findProject } from '../data/showcase'
import './showcase.css'

export default function ShowcaseProjectPage() {
  const { id = '' } = useParams()
  const project = findProject(id)
  const [about, setAbout] = useState(false)
  const [frameKey, setFrameKey] = useState(0)
  const frameRef = useRef<HTMLIFrameElement>(null)

  if (!project) {
    return (
      <main className="container" style={{ padding: '80px 28px' }}>
        <span className="mono-label">unknown project</span>
        <h1 style={{ marginTop: 12 }}>Nothing here</h1>
        <p>
          No showcase project called <code>{id}</code>.
        </p>
        <Link className="btn btn-primary" to="/showcase">
          ← Back to showcase
        </Link>
      </main>
    )
  }

  return (
    <main className="project-page" style={{ ['--pj' as string]: project.accent }}>
      <div className="project-bar">
        <div className="project-bar-inner">
          <Link to="/showcase" className="project-back">
            ← Showcase
          </Link>
          <div className="project-title">
            <h1>{project.title}</h1>
            <span className="project-build">
              built by jcode · {project.build.model} · {project.build.rounds}{' '}
              {project.build.rounds > 1 ? 'rounds' : 'round'}
            </span>
          </div>
          <div className="project-actions">
            <button className="btn btn-ghost btn-sm" onClick={() => setAbout((v) => !v)}>
              {about ? 'Hide details' : 'About this build'}
            </button>
            <button
              className="btn btn-ghost btn-sm"
              onClick={() => setFrameKey((k) => k + 1)}
              title="Restart the demo"
            >
              ↺ Restart
            </button>
            <a
              className="btn btn-accent btn-sm"
              href={project.src}
              target="_blank"
              rel="noreferrer"
            >
              Open full screen ↗
            </a>
          </div>
        </div>
        {about && (
          <div className="project-about">
            <div className="project-about-inner">
              <div>
                <h3>The brief</h3>
                <p>{project.description}</p>
                <p className="pa-note">{project.build.note}</p>
              </div>
              {project.highlights.length > 0 && (
                <div>
                  <h3>Highlights</h3>
                  <ul>
                    {project.highlights.map((h) => (
                      <li key={h}>{h}</li>
                    ))}
                  </ul>
                </div>
              )}
              <div>
                <h3>Provenance</h3>
                <p className="pa-note">
                  Implemented autonomously by the jcode agent over ACP in an isolated
                  sandbox — no human-written code. Verified with deterministic checks and
                  real-browser sessions between rounds.
                </p>
                <div className="chal-tags">
                  {project.tags.map((t) => (
                    <span key={t}>{t}</span>
                  ))}
                </div>
              </div>
            </div>
          </div>
        )}
      </div>
      <div className="project-stage">
        <iframe
          key={frameKey}
          ref={frameRef}
          src={project.src}
          title={project.title}
          allow="fullscreen"
          sandbox="allow-scripts allow-same-origin allow-pointer-lock"
        />
      </div>
    </main>
  )
}
