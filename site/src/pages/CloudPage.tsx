import { Suspense, lazy } from 'react'
import { Link } from 'react-router-dom'
import Reveal from '../components/Reveal'
import './cloud.css'

const CloudDemoSection = lazy(() => import('../components/cloud/CloudDemoSection'))

const CLOUD_URL = 'https://cloud.j-code.net'

export default function CloudPage() {
  return (
    <main className="cloud-page">
      <header className="cl-hero">
        <div className="container cl-hero-grid">
          <div>
            <Reveal><span className="mono-label">jcode cloud</span></Reveal>
            <Reveal delay={60}>
              <h1>Your agent does not live <em>in one window.</em></h1>
            </Reveal>
            <Reveal delay={120}>
              <p className="cl-hero-sub">
                Continue Desktop tasks from a browser or phone, approve a new machine from one
                you already trust, and carry encrypted provider configuration between your own
                Desktops—without turning local jcode into a cloud-only app.
              </p>
            </Reveal>
            <Reveal delay={180}>
              <div className="cl-actions">
                <a className="btn btn-accent" href={CLOUD_URL} target="_blank" rel="noreferrer">
                  Open jcode Cloud ↗
                </a>
                <Link className="btn btn-ghost" to="/docs/cloud">Read the security model</Link>
              </div>
            </Reveal>
          </div>
          <Reveal delay={180}>
            <div className="cl-principle">
              <span className="cl-principle-num">01</span>
              <h2>Cloud is an extension, not a dependency.</h2>
              <p>
                Local Providers remain direct. Local sessions remain local until you choose
                otherwise. Signing out of Cloud does not disable Desktop.
              </p>
            </div>
          </Reveal>
        </div>
      </header>

      <section className="cl-demo">
        <div className="container">
          <Reveal>
            <div className="section-head">
              <span className="mono-label">the trust map</span>
              <h2>Two Provider lanes. One clear boundary.</h2>
              <p className="lead">
                Provider identity—including its canonical <code>kind</code> and icon—travels
                with the model catalog. Credentials follow the lane you chose.
              </p>
            </div>
          </Reveal>
          <Reveal>
            <Suspense fallback={<div className="demo-placeholder">[ loading trust map… ]</div>}>
              <CloudDemoSection />
            </Suspense>
          </Reveal>
        </div>
      </section>

      <section className="cl-lanes">
        <div className="container">
          <Reveal>
            <div className="cl-lane local">
              <div className="cl-lane-index">A</div>
              <div>
                <span className="mono-label">local provider</span>
                <h2>Desktop calls the model API directly.</h2>
                <p>
                  This is jcode as it has always worked. Your API key, base URL and custom
                  headers live on the machine. Cloud is optional; no login is required.
                </p>
              </div>
              <pre>{`Desktop
  └─ local provider
       └─ HTTPS ──▶ model API`}</pre>
            </div>
          </Reveal>
          <Reveal delay={80}>
            <div className="cl-lane hosted">
              <div className="cl-lane-index">B</div>
              <div>
                <span className="mono-label">cloud provider</span>
                <h2>Cloud owns the credential and proxies the call.</h2>
                <p>
                  Cluster and Project providers appear in the same model selector with the
                  same Provider names and icons. Desktop sends the selected catalog ID through
                  <code>cloud_proxy</code>; the server resolves the upstream credential.
                </p>
              </div>
              <pre>{`Desktop
  └─ cloud:<provider>/<model>
       └─ cloud_proxy ──▶ model API`}</pre>
            </div>
          </Reveal>
        </div>
      </section>

      <section className="cl-sync">
        <div className="container cl-sync-grid">
          <Reveal>
            <div>
              <span className="mono-label">desktop ↔ desktop</span>
              <h2>Your setup, encrypted before it leaves the machine.</h2>
              <p>
                Configuration sync is opt-in. It includes local Provider keys, base URLs,
                custom headers and per-model state. Every payload is encrypted with an Account
                Sync Key (ASK); Cloud stores only ciphertext.
              </p>
              <p>
                A newly signed-in Desktop cannot decrypt anything until an already trusted
                Desktop approves it in Settings. Device identity keys stay in the operating
                system keychain.
              </p>
              <Link to="/docs/cloud">Read the enrollment and recovery details →</Link>
            </div>
          </Reveal>
          <Reveal delay={100}>
            <ol className="cl-steps">
              <li>
                <span>01</span>
                <div><b>Request</b><small>New Desktop publishes only its identity public key.</small></div>
              </li>
              <li>
                <span>02</span>
                <div><b>Approve</b><small>A trusted Desktop wraps ASK for that exact device.</small></div>
              </li>
              <li>
                <span>03</span>
                <div><b>Reconcile</b><small>Provider envelopes sync with CAS and tombstones.</small></div>
              </li>
            </ol>
          </Reveal>
        </div>
      </section>

      <section className="cl-surfaces">
        <div className="container">
          <Reveal>
            <div className="section-head">
              <span className="mono-label">browser + mobile</span>
              <h2>Walk away from the desk, not the task.</h2>
            </div>
          </Reveal>
          <div className="cl-surface-grid">
            <Reveal>
              <article>
                <span>↗</span>
                <h3>Resume live conversations</h3>
                <p>See streaming output, send the next prompt and stop a run from Cloud.</p>
              </article>
            </Reveal>
            <Reveal delay={80}>
              <article>
                <span>✓</span>
                <h3>Approve where the context is</h3>
                <p>Tool approvals stay in the Desktop UI and companion surfaces—not a terminal detour.</p>
              </article>
            </Reveal>
            <Reveal delay={160}>
              <article>
                <span>⌘</span>
                <h3>Trust devices explicitly</h3>
                <p>Device login and encrypted config access are separate approvals by design.</p>
              </article>
            </Reveal>
          </div>
        </div>
      </section>

      <section className="cl-cta">
        <div className="container">
          <Reveal>
            <span className="mono-label">local when you want it · cloud when you need it</span>
            <h2>Keep the agent close. Take the session anywhere.</h2>
            <div className="cl-actions center">
              <a className="btn btn-accent" href={CLOUD_URL} target="_blank" rel="noreferrer">
                Open Cloud
              </a>
              <Link className="btn btn-ghost" to="/desktop">Get Desktop</Link>
            </div>
          </Reveal>
        </div>
      </section>
    </main>
  )
}
