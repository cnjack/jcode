import { Suspense, lazy } from 'react'
import { Link } from 'react-router-dom'
import Reveal from '../components/Reveal'
import CopyButton from '../components/CopyButton'
import './cli.css'

const CliDemoSection = lazy(() => import('../components/CliDemoSection'))

const INSTALL = 'curl -fsSL https://raw.githubusercontent.com/cnjack/jcode/main/script/install.sh | sh'

const MODES = [
  {
    cmd: 'jcode',
    title: 'Interactive TUI',
    body: 'The full experience: chat, inline diffs, approval dialogs, plan mode, live tool calls — all in your terminal.',
  },
  {
    cmd: 'jcode -p "fix the failing tests"',
    title: 'One-shot mode',
    body: 'Fire a single task from a script or CI job. jcode works until it finishes, prints the result and exits.',
  },
  {
    cmd: 'jcode web --port 8899',
    title: 'Web server',
    body: 'Serve the same agent to your browser — handy on remote boxes where a terminal is all you have.',
  },
  {
    cmd: 'jcode acp',
    title: 'Headless ACP',
    body: 'JSON-RPC over stdio for editors and automation. This is how Zed talks to jcode — and how our showcase was built.',
  },
]

const COMMANDS: [string, string][] = [
  ['/model', 'switch model mid-session'],
  ['/goal', 'set a persistent objective the agent drives to completion'],
  ['/resume', 'resume a previous session'],
  ['/ssh', 'open the SSH connection wizard'],
  ['/compact', 'compact conversation context'],
  ['/mcp', 'list and authenticate MCP servers'],
  ['/channel', 'link WeChat for push notifications'],
  ['/review-pr', 'run the PR review skill'],
]

export default function CliPage() {
  return (
    <main className="cli-page">
      <header className="cli-hero">
        <div className="container">
          <div className="cli-hero-grid">
            <div>
              <Reveal>
                <span className="mono-label cli-label">jcode cli</span>
              </Reveal>
              <Reveal delay={60}>
                <h1>
                  Your terminal just got a <em>teammate</em>.
                </h1>
              </Reveal>
              <Reveal delay={120}>
                <p className="cli-hero-sub">
                  The TUI is jcode's home turf: describe the task, watch the agent read,
                  plan, edit and test — and approve every step without your hands leaving
                  the keyboard.
                </p>
              </Reveal>
              <Reveal delay={180}>
                <div className="cli-install snippet">
                  <span className="prompt-char">$ </span>
                  {INSTALL}
                  <CopyButton text={INSTALL} />
                </div>
              </Reveal>
              <Reveal delay={240}>
                <div className="cli-hero-actions">
                  <Link to="/docs/get-started" className="btn btn-accent">
                    Get started →
                  </Link>
                  <Link to="/docs/commands" className="btn btn-ghost-dark">
                    Command reference
                  </Link>
                </div>
              </Reveal>
            </div>
          </div>
        </div>
      </header>

      <section className="cli-demo-band">
        <div className="container">
          <Reveal>
            <Suspense fallback={<div className="demo-placeholder dark">[ loading demo… ]</div>}>
              <CliDemoSection />
            </Suspense>
          </Reveal>
          <p className="cli-demo-caption">a real session, recreated frame by frame</p>
        </div>
      </section>

      <section className="cli-modes">
        <div className="container">
          <Reveal>
            <div className="section-head">
              <span className="mono-label">four ways in</span>
              <h2>One binary, every workflow</h2>
            </div>
          </Reveal>
          <div className="cli-mode-grid">
            {MODES.map((m, i) => (
              <Reveal key={m.cmd} delay={(i % 2) * 80}>
                <div className="cli-mode-card">
                  <code className="cli-mode-cmd">
                    <span className="prompt-char">$ </span>
                    {m.cmd}
                  </code>
                  <h3>{m.title}</h3>
                  <p>{m.body}</p>
                </div>
              </Reveal>
            ))}
          </div>
        </div>
      </section>

      <section className="cli-flow">
        <div className="container cli-flow-grid">
          <Reveal>
            <div>
              <span className="mono-label">stay in control</span>
              <h2>Plan first. Approve everything.</h2>
              <p>
                Start in <b>Plan Mode</b> and the agent explores your repo read-only, then
                presents a structured plan you approve, edit or reject. Switch to full
                access only when you're ready — and even then, every file edit shows a
                diff before it lands.
              </p>
              <p>
                <Link to="/docs/overview/plan-mode">How plan mode works →</Link>
              </p>
            </div>
          </Reveal>
          <Reveal delay={100}>
            <div className="cli-plan-card">
              <div className="cli-plan-head">◇ Plan — refactor config loader</div>
              <ol>
                <li>Add EnvOverride type to internal/config</li>
                <li>Layer resolution: file → env → flags</li>
                <li>Migrate 3 os.Getenv call sites</li>
                <li>Extend loader tests with override cases</li>
              </ol>
              <div className="cli-plan-actions">
                <span className="ok">[a]pprove</span>
                <span>[e]dit</span>
                <span className="no">[r]eject</span>
              </div>
            </div>
          </Reveal>
        </div>
      </section>

      <section className="cli-commands">
        <div className="container">
          <Reveal>
            <div className="section-head">
              <span className="mono-label">muscle memory friendly</span>
              <h2>Slash commands for everything</h2>
            </div>
          </Reveal>
          <Reveal>
            <div className="cli-cmd-table">
              {COMMANDS.map(([cmd, desc]) => (
                <div key={cmd} className="cli-cmd-row">
                  <code>{cmd}</code>
                  <span>{desc}</span>
                </div>
              ))}
              <div className="cli-cmd-row more">
                <code>/…</code>
                <span>
                  <Link to="/docs/commands">full command &amp; shortcut reference →</Link>
                </span>
              </div>
            </div>
          </Reveal>
        </div>
      </section>

      <section className="cli-remote">
        <div className="container">
          <div className="cli-remote-grid">
            <Reveal>
              <div className="cli-remote-card">
                <span className="mono-label">ssh</span>
                <h3>Remote repos, local feel</h3>
                <p>
                  <code>/ssh</code> connects the agent to any host — it reads, edits and
                  runs commands over there with the exact same tools.{' '}
                  <Link to="/docs/overview/ssh">SSH docs →</Link>
                </p>
              </div>
            </Reveal>
            <Reveal delay={80}>
              <div className="cli-remote-card">
                <span className="mono-label">sessions</span>
                <h3>Pick up where you left off</h3>
                <p>
                  Every conversation persists. <code>/resume</code> restores context,
                  tool history and goals — even after a reboot.{' '}
                  <Link to="/docs/overview/sessions">Sessions docs →</Link>
                </p>
              </div>
            </Reveal>
            <Reveal delay={160}>
              <div className="cli-remote-card">
                <span className="mono-label">teams</span>
                <h3>Spawn a whole crew</h3>
                <p>
                  Delegate subtasks to parallel child agents, or run a full team with a
                  shared task list.{' '}
                  <Link to="/docs/overview/agent-teams">Agent teams docs →</Link>
                </p>
              </div>
            </Reveal>
          </div>
        </div>
      </section>

      <section className="cli-cta">
        <div className="container">
          <Reveal>
            <h2>Alias it to two letters. You'll use it that much.</h2>
          </Reveal>
          <Reveal delay={80}>
            <div className="cli-install snippet cta">
              <span className="prompt-char">$ </span>
              {INSTALL}
              <CopyButton text={INSTALL} />
            </div>
          </Reveal>
        </div>
      </section>
    </main>
  )
}
