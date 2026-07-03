import { useEffect, useRef, useState } from 'react'
import {
  AgentText,
  Composer,
  DiffCard,
  DoneBanner,
  JcodeWordmark,
  Sidebar,
  ToolChip,
  UserBubble,
  WindowChrome,
} from './parts'

/* ------------------------------------------------------------------
   Live, clickable mini-replica of the jcode desktop app.
   Pick a workspace, type (or pick a suggested task), hit Send and
   watch a scripted agent session play back.
   ------------------------------------------------------------------ */

type Ev =
  | { kind: 'agent'; text: string }
  | { kind: 'tool'; icon: string; label: string; detail: string; ms: number }
  | { kind: 'diff'; file: string; lines: string[] }
  | { kind: 'done'; summary: string }

interface Script {
  chip: string
  prompt: string
  events: Ev[]
}

const SCRIPTS: Script[] = [
  {
    chip: 'Fix the flaky test',
    prompt: 'fix the flaky TestSessionRefresh in internal/auth',
    events: [
      { kind: 'agent', text: 'Let me look at that test and the code it covers.' },
      { kind: 'tool', icon: '⌕', label: 'grep', detail: '"TestSessionRefresh" — 2 hits', ms: 900 },
      { kind: 'tool', icon: '☰', label: 'read', detail: 'internal/auth/session_test.go', ms: 1100 },
      {
        kind: 'agent',
        text: 'The test races on time.Now() — token expiry is compared against a real clock. I\'ll inject a fake clock.',
      },
      {
        kind: 'diff',
        file: 'internal/auth/session.go',
        lines: [
          '- if token.ExpiresAt.Before(time.Now()) {',
          '+ if token.ExpiresAt.Before(s.clock.Now()) {',
          '    return s.refresh(ctx, token)',
        ],
      },
      { kind: 'tool', icon: '▶', label: 'execute', detail: 'go test ./internal/auth/... -count=20', ms: 2100 },
      { kind: 'done', summary: '2 files changed · 20/20 runs green · 41s' },
    ],
  },
  {
    chip: 'Add dark mode',
    prompt: 'add a dark mode toggle to the settings page',
    events: [
      { kind: 'agent', text: 'Scanning the theme setup and settings page.' },
      { kind: 'tool', icon: '⌕', label: 'glob', detail: 'src/styles/** — tokens.css found', ms: 800 },
      { kind: 'tool', icon: '☰', label: 'read', detail: 'src/pages/Settings.vue', ms: 1000 },
      {
        kind: 'agent',
        text: 'Tokens already use CSS custom properties — I\'ll add a [data-theme] switch and persist the choice.',
      },
      {
        kind: 'diff',
        file: 'src/styles/tokens.css',
        lines: [
          '+ [data-theme="dark"] {',
          '+   --bg: #16202c; --ink: #dce6f1;',
          '+ }',
        ],
      },
      { kind: 'tool', icon: '✎', label: 'edit', detail: 'src/pages/Settings.vue — toggle + store', ms: 1400 },
      { kind: 'tool', icon: '▶', label: 'execute', detail: 'pnpm typecheck && pnpm test', ms: 1900 },
      { kind: 'done', summary: '3 files changed · typecheck clean · tests green' },
    ],
  },
  {
    chip: 'Explain this codebase',
    prompt: 'give me a map of this codebase and where requests flow',
    events: [
      { kind: 'agent', text: 'I\'ll walk the entry points and trace a request end to end.' },
      { kind: 'tool', icon: '⌕', label: 'glob', detail: 'cmd/** internal/** — 214 files', ms: 900 },
      { kind: 'tool', icon: '☰', label: 'read', detail: 'cmd/server/main.go', ms: 1000 },
      { kind: 'tool', icon: '⌕', label: 'grep', detail: '"http.Handle" — 9 routes', ms: 900 },
      {
        kind: 'agent',
        text: 'Requests enter cmd/server → router (internal/api) → services (internal/core) → storage (internal/store). Auth wraps everything as middleware. Full write-up below.',
      },
      { kind: 'done', summary: 'Architecture map with 4 layers · 9 routes traced' },
    ],
  },
]

type Played =
  | { kind: 'user'; text: string }
  | { kind: 'agent'; text: string }
  | { kind: 'tool'; icon: string; label: string; detail: string; status: 'running' | 'done' }
  | { kind: 'diff'; file: string; lines: string[] }
  | { kind: 'done'; summary: string }

export default function Replica() {
  const [ws, setWs] = useState('jtype')
  const [draft, setDraft] = useState('')
  const [played, setPlayed] = useState<Played[] | null>(null)
  const [busy, setBusy] = useState(false)
  const timers = useRef<number[]>([])
  const scrollRef = useRef<HTMLDivElement>(null)

  const clear = () => {
    timers.current.forEach(clearTimeout)
    timers.current = []
  }
  useEffect(() => clear, [])

  useEffect(() => {
    const el = scrollRef.current
    if (el) el.scrollTop = el.scrollHeight
  }, [played])

  const play = (script: Script, userText?: string) => {
    clear()
    setBusy(true)
    setPlayed([{ kind: 'user', text: userText || script.prompt }])
    let at = 600
    const t = (fn: () => void, ms: number) => {
      timers.current.push(window.setTimeout(fn, ms))
    }
    for (const ev of script.events) {
      if (ev.kind === 'tool') {
        const start = at
        t(() => {
          setPlayed((p) => [
            ...(p ?? []),
            { kind: 'tool', icon: ev.icon, label: ev.label, detail: ev.detail, status: 'running' },
          ])
        }, start)
        at += ev.ms
        t(() => {
          setPlayed((p) =>
            (p ?? []).map((x, i, arr) =>
              i === arr.length - 1 && x.kind === 'tool' ? { ...x, status: 'done' } : x,
            ),
          )
        }, at)
        at += 260
      } else if (ev.kind === 'agent') {
        t(() => setPlayed((p) => [...(p ?? []), { kind: 'agent', text: ev.text }]), at)
        at += 1300
      } else if (ev.kind === 'diff') {
        t(() => setPlayed((p) => [...(p ?? []), ev]), at)
        at += 1500
      } else {
        t(() => {
          setPlayed((p) => [...(p ?? []), ev])
          setBusy(false)
        }, at)
      }
    }
  }

  const onSend = () => {
    if (busy) return
    const text = draft.trim()
    const script =
      SCRIPTS.find((s) => text && s.prompt.toLowerCase().includes(text.slice(0, 8).toLowerCase())) ??
      SCRIPTS[0]
    play(script, text || undefined)
    setDraft('')
  }

  const reset = () => {
    clear()
    setPlayed(null)
    setBusy(false)
  }

  return (
    <WindowChrome>
      <Sidebar active={ws} onSelect={(n) => setWs(n)} />
      <section className="dk-main">
        {played === null ? (
          <div className="dk-empty">
            <div className="dk-halo" aria-hidden />
            <JcodeWordmark />
            <h3 className="dk-empty-title">Start a new task in {ws}</h3>
            <p className="dk-empty-sub">
              Send a message to start. <span className="dk-kbd">/</span> for commands.
            </p>
            <Composer
              workspace={ws}
              value={draft}
              onChange={setDraft}
              onSend={onSend}
              sendActive={draft.trim().length > 0}
            />
            <div className="dk-chips">
              {SCRIPTS.map((s) => (
                <button key={s.chip} className="dk-chip" onClick={() => play(s)}>
                  {s.chip}
                </button>
              ))}
            </div>
            <p className="dk-replica-note">
              ↑ this is a live miniature of the real app — click around
            </p>
          </div>
        ) : (
          <div className="dk-session">
            <div className="dk-session-head">
              <span className="dk-session-ws">
                <span className="dk-folder">▤</span> {ws}
              </span>
              <span className={`dk-session-state${busy ? ' running' : ''}`}>
                {busy ? '◌ running' : '✓ finished'}
              </span>
              <button className="dk-session-reset" onClick={reset}>
                ✕ new task
              </button>
            </div>
            <div className="dk-stream" ref={scrollRef}>
              {played.map((ev, i) => {
                switch (ev.kind) {
                  case 'user':
                    return <UserBubble key={i}>{ev.text}</UserBubble>
                  case 'agent':
                    return <AgentText key={i}>{ev.text}</AgentText>
                  case 'tool':
                    return (
                      <ToolChip
                        key={i}
                        icon={ev.icon}
                        label={ev.label}
                        detail={ev.detail}
                        status={ev.status}
                      />
                    )
                  case 'diff':
                    return <DiffCard key={i} file={ev.file} lines={ev.lines} />
                  case 'done':
                    return <DoneBanner key={i} summary={ev.summary} />
                }
              })}
              {busy && <div className="dk-thinking">thinking…</div>}
            </div>
          </div>
        )}
      </section>
    </WindowChrome>
  )
}
