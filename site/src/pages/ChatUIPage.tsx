/**
 * /chat-ui — the jcode-ui component library showcase page.
 *
 * Hero with the live ChatDemo (the "footprint" component), then a feature grid
 * + install snippet + links into the docs. Mirrors the structure of HomePage
 * but showcases the reusable React library rather than the product.
 */

import { useMemo } from 'react'
import { Link } from 'react-router-dom'
import Reveal from '../components/Reveal'
import CopyButton from '../components/CopyButton'
import { ChatDemo } from '../playground/ChatDemo'
import { highlightDemoCode } from '../playground/highlightDemoCode'
import './chatui.css'

const INSTALL = 'pnpm add jcode-ui jcode-ui-core'

const QUICK_START = `import { RuntimeProvider, createExternalStoreRuntime, Thread, ChatInput } from 'jcode-ui'
import 'jcode-ui/styles.css'

const runtime = createExternalStoreRuntime({
  store,                       // your Redux/Zustand store
  select: (s) => ({            // project to RuntimeState
    items: s.chat.timeline,
    isRunning: s.chat.isRunning,
    tokenSnapshot: s.chat.tokenInfo,
    // …
  }),
  actions: { sendMessage, stop, resolveApproval, /* … */ },
})

export function App() {
  return (
    <RuntimeProvider runtime={runtime}>
      <Thread />
      <ChatInput />
    </RuntimeProvider>
  )
}`

const FEATURES = [
  {
    n: '01',
    title: 'Backend-agnostic runtime',
    body: 'Wrap any Redux-shaped store with createExternalStoreRuntime. The components never touch your state layer directly.',
  },
  {
    n: '02',
    title: 'Pluggable tool renderers',
    body: '9 default renderers (terminal, diff, search, file-viewer, …) plus a registry so you can render any custom tool your agent emits.',
  },
  {
    n: '03',
    title: 'Streaming + virtualization',
    body: 'TanStack Virtual under the hood, with the "follow only when at bottom" contract baked in. Handles 10k-message threads.',
  },
  {
    n: '04',
    title: 'Token-driven theming',
    body: 'Every color/radius/shadow is a CSS custom property. Light + dark + generated themes all work without touching component code.',
  },
  {
    n: '05',
    title: 'Interactive blocks',
    body: 'Approval gates (3-tier with arming) and ask-user question flows ship as first-class primitives, not afterthoughts.',
  },
  {
    n: '06',
    title: 'Headless or styled',
    body: 'Use the styled jcode-ui components, or drop down to jcode-ui-core primitives and bring your own Tailwind layer.',
  },
]

export default function ChatUIPage() {
  const quickStartHtml = useMemo(() => highlightDemoCode(QUICK_START, 'tsx'), [])

  return (
    <div className="chatui-page">
      {/* Hero */}
      <section className="chatui-hero">
        <Reveal>
          <p className="chatui-eyebrow">Open source · MIT</p>
          <h1 className="chatui-title">
            React components for <span className="accent">AI chat</span>
          </h1>
          <p className="chatui-lede">
            A drop-in chat UI for agents and copilots — streaming messages, tool calls, approvals,
            and ask-user interactions. Backend-agnostic, token-themed, and tree-shakeable.
          </p>
          <div className="chatui-cta">
            <div className="chatui-install">
              <code>{INSTALL}</code>
              <CopyButton text={INSTALL} />
            </div>
            <Link className="chatui-docs-link" to="/chat-ui/docs">
              Read the docs →
            </Link>
          </div>
        </Reveal>
      </section>

      {/* Live demo */}
      <section className="chatui-demo-section">
        <Reveal>
          <h2 className="chatui-section-title">Live demo</h2>
          <p className="chatui-section-lede">
            This is the real jcode-ui, driven by a mock runtime — no backend. It streams a scripted
            conversation through every item kind.
          </p>
          <div className="chatui-demo-frame">
            <ChatDemo height={520} />
          </div>
        </Reveal>
      </section>

      {/* Features */}
      <section className="chatui-features">
        <Reveal>
          <h2 className="chatui-section-title">What's inside</h2>
        </Reveal>
        <div className="chatui-feature-grid">
          {FEATURES.map((f) => (
            <Reveal key={f.n}>
              <div className="chatui-feature">
                <span className="chatui-feature-n">{f.n}</span>
                <h3>{f.title}</h3>
                <p>{f.body}</p>
              </div>
            </Reveal>
          ))}
        </div>
      </section>

      {/* Quick start */}
      <section className="chatui-quickstart">
        <Reveal>
          <h2 className="chatui-section-title">Quick start</h2>
          <div className="chatui-codeblock-wrap">
            <CopyButton text={QUICK_START} />
            <pre className="chatui-codeblock">
              <code
                className="hljs language-tsx"
                dangerouslySetInnerHTML={{ __html: quickStartHtml }}
              />
            </pre>
          </div>
        </Reveal>
      </section>
    </div>
  )
}
