import { Link } from 'react-router-dom'
import Reveal from '../components/Reveal'
import CopyButton from '../components/CopyButton'
import TypingTerminal, { type TermLine } from '../components/TypingTerminal'
import { CHALLENGES } from '../data/showcase'
import './home.css'

const INSTALL = 'curl -fsSL https://raw.githubusercontent.com/cnjack/jcode/main/script/install.sh | sh'

const HERO_SCRIPT: TermLine[] = [
  { t: 'cmd', text: 'jcode -p "fix the goroutine leak in server.go"' },
  { t: 'pause', ms: 500 },
  { t: 'agent', text: 'Reading server.go — the goroutine in handleConnection() is never joined.' },
  { t: 'tool', text: 'edit  path=server.go' },
  {
    t: 'diff',
    lines: [
      '- go handle(conn)',
      '+ wg.Add(1)',
      '+ go func() { defer wg.Done(); handle(conn) }()',
    ],
  },
  { t: 'ok', text: 'Edit applied' },
  { t: 'tool', text: 'execute  go test ./...' },
  { t: 'ok', text: 'ok  net/server  0.41s — leak fixed, tests green' },
  { t: 'pause', ms: 2400 },
]

const FEATURES = [
  {
    n: '01',
    title: 'Terminal-native TUI',
    body: 'Inline diffs, live tool calls and approval dialogs — a full agent experience without leaving the shell.',
    to: '/cli',
  },
  {
    n: '02',
    title: 'Desktop app',
    body: 'Workspaces, automations and channels in a native shell for the same engine.',
    to: '/desktop',
  },
  {
    n: '03',
    title: 'Plan before you act',
    body: 'Plan Mode explores read-only, presents a structured plan, and waits for your sign-off.',
    to: '/docs/overview/plan-mode',
  },
  {
    n: '04',
    title: 'Agent teams',
    body: 'Spawn parallel teammates and delegate subtasks to independent child agents.',
    to: '/docs/overview/agent-teams',
  },
  {
    n: '05',
    title: 'Bring your own model',
    body: 'Any OpenAI-compatible API. Switch models mid-session with one keystroke.',
    to: '/docs/overview/models',
  },
  {
    n: '06',
    title: 'Rich tool set',
    body: 'Read, edit, execute, grep, glob, todos, subagents — every call visible, every diff reviewable.',
    to: '/docs/tools',
  },
  {
    n: '07',
    title: 'SSH & Docker',
    body: 'Point the agent at a remote host or container and keep the exact same workflow.',
    to: '/docs/overview/ssh',
  },
  {
    n: '08',
    title: 'MCP & skills',
    body: 'Extend the agent with MCP servers and reusable skills your team shares.',
    to: '/docs/overview/mcp',
  },
]

const LOOP_STEPS = [
  { k: 'read', label: 'Read', body: 'explores your codebase with grep, glob and file reads' },
  { k: 'plan', label: 'Plan', body: 'proposes an approach you can approve or redirect' },
  { k: 'edit', label: 'Edit', body: 'writes surgical, reviewable diffs — never blind rewrites' },
  { k: 'run', label: 'Run', body: 'executes tests and commands, reads the results' },
  { k: 'report', label: 'Report', body: 'tells you exactly what changed and why' },
]

export default function HomePage() {
  return (
    <main className="home">
      {/* ---------- hero ---------- */}
      <header className="hero">
        <div className="container hero-grid">
          <div className="hero-copy">
            <Reveal>
              <span className="mono-label">open-source ai coding agent</span>
            </Reveal>
            <Reveal delay={60}>
              <h1>
                Think it.
                <br />
                <em>Code it</em>
                <span className="hero-dot">.</span>
              </h1>
            </Reveal>
            <Reveal delay={120}>
              <p className="hero-sub">
                jcode reads your codebase, writes surgical edits, runs commands and reports
                every step — in your terminal, on your desktop, or in the browser.
                No black boxes.
              </p>
            </Reveal>
            <Reveal delay={180}>
              <div className="hero-install snippet">
                <span className="prompt-char">$ </span>
                {INSTALL}
                <CopyButton text={INSTALL} />
              </div>
            </Reveal>
            <Reveal delay={240}>
              <div className="hero-actions">
                <Link className="btn btn-accent" to="/docs/get-started">
                  Get started →
                </Link>
                <a
                  className="btn btn-ghost"
                  href="https://github.com/cnjack/jcode"
                  target="_blank"
                  rel="noreferrer"
                >
                  ★ GitHub
                </a>
              </div>
            </Reveal>
            <Reveal delay={300}>
              <p className="hero-note">
                MIT licensed · bring your own model · macOS / Linux / Windows
              </p>
            </Reveal>
          </div>
          <Reveal className="hero-term" delay={200}>
            <TypingTerminal script={HERO_SCRIPT} />
          </Reveal>
        </div>
        <div className="hero-grid-lines" aria-hidden />
      </header>

      {/* ---------- surfaces ---------- */}
      <section className="surfaces">
        <div className="container">
          <Reveal>
            <div className="section-head">
              <span className="mono-label">one agent, three surfaces</span>
              <h2>Meet it where you work</h2>
              <p className="lead">
                The same engine, session model and tool set — pick the interface that fits
                the moment.
              </p>
            </div>
          </Reveal>
          <div className="surface-cards">
            <Reveal delay={0}>
              <Link to="/cli" className="surface-card surface-cli">
                <div className="surface-art" aria-hidden>
                  <span className="sc-prompt">&gt;_</span>
                </div>
                <h3>CLI</h3>
                <p>The terminal TUI: diffs, approvals and plan mode at typing speed.</p>
                <span className="surface-more">Explore the CLI →</span>
              </Link>
            </Reveal>
            <Reveal delay={80}>
              <Link to="/desktop" className="surface-card surface-desktop">
                <div className="surface-art" aria-hidden>
                  <span className="sc-window">
                    <i />
                    <i />
                    <i />
                  </span>
                </div>
                <h3>Desktop</h3>
                <p>Workspaces, automations and channels in a native app.</p>
                <span className="surface-more">Explore the app →</span>
              </Link>
            </Reveal>
            <Reveal delay={160}>
              <Link to="/docs/web-interface" className="surface-card surface-web">
                <div className="surface-art" aria-hidden>
                  <span className="sc-globe">◍</span>
                </div>
                <h3>Web</h3>
                <p>
                  <code>jcode web</code> serves the full experience to your browser — great
                  over SSH.
                </p>
                <span className="surface-more">Read the docs →</span>
              </Link>
            </Reveal>
          </div>
        </div>
      </section>

      {/* ---------- agent loop ---------- */}
      <section className="loop-band">
        <div className="container">
          <Reveal>
            <div className="section-head">
              <span className="mono-label">how it works</span>
              <h2>An agent you can audit</h2>
              <p className="lead">
                Every cycle is visible: what it read, what it wants to change, what ran and
                what came back.
              </p>
            </div>
          </Reveal>
          <ol className="loop-steps">
            {LOOP_STEPS.map((s, i) => (
              <Reveal as="li" key={s.k} delay={i * 70}>
                <span className="loop-idx">{String(i + 1).padStart(2, '0')}</span>
                <h4>{s.label}</h4>
                <p>{s.body}</p>
              </Reveal>
            ))}
          </ol>
        </div>
      </section>

      {/* ---------- features ---------- */}
      <section className="features">
        <div className="container">
          <Reveal>
            <div className="section-head">
              <span className="mono-label">capabilities</span>
              <h2>Everything you need to code with AI</h2>
            </div>
          </Reveal>
          <div className="feature-grid">
            {FEATURES.map((f, i) => (
              <Reveal key={f.n} delay={(i % 4) * 60}>
                <Link to={f.to} className="feature-card">
                  <span className="feature-n">[{f.n}]</span>
                  <h4>{f.title}</h4>
                  <p>{f.body}</p>
                </Link>
              </Reveal>
            ))}
          </div>
        </div>
      </section>

      {/* ---------- showcase teaser ---------- */}
      <section className="showcase-teaser">
        <div className="container">
          <Reveal>
            <div className="section-head">
              <span className="mono-label">proof of work</span>
              <h2>We don’t just demo it. We let it build.</h2>
              <p className="lead">
                Everything in the showcase was built end-to-end by the jcode agent running
                unattended — multi-round briefs, real verification, no human-written code.
              </p>
            </div>
          </Reveal>
          <div className="teaser-grid">
            {CHALLENGES.map((c, i) => (
              <Reveal key={c.id} delay={i * 70}>
                <Link
                  to={`/showcase/${c.id}`}
                  className="teaser-card"
                  style={{ ['--pj' as string]: c.accent }}
                >
                  <span className="teaser-kind">challenge</span>
                  <h3>{c.title}</h3>
                  <p>{c.tagline}</p>
                  <div className="teaser-tags">
                    {c.tags.map((t) => (
                      <span key={t}>{t}</span>
                    ))}
                  </div>
                </Link>
              </Reveal>
            ))}
          </div>
          <Reveal>
            <div className="teaser-cta">
              <Link to="/showcase" className="btn btn-primary">
                Open the showcase →
              </Link>
            </div>
          </Reveal>
        </div>
      </section>

      {/* ---------- bottom CTA ---------- */}
      <section className="cta-band">
        <div className="container cta-inner">
          <Reveal>
            <h2>
              Your codebase is waiting<span className="hero-dot">.</span>
            </h2>
          </Reveal>
          <Reveal delay={80}>
            <div className="hero-install snippet cta-install">
              <span className="prompt-char">$ </span>
              {INSTALL}
              <CopyButton text={INSTALL} />
            </div>
          </Reveal>
          <Reveal delay={140}>
            <p>
              Then run <code>jcode</code> in any repository and say what you want done.{' '}
              <Link to="/docs/get-started">Full setup guide →</Link>
            </p>
          </Reveal>
        </div>
      </section>
    </main>
  )
}
