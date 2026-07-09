/**
 * ComponentDemo — embeddable live previews for jcode-ui docs.
 *
 * Markdown: <div data-jcode-demo="message" data-height="320"></div>
 *
 * Performance:
 *  - Mount only when scrolled near viewport (IntersectionObserver)
 *  - Styles imported once (side-effect of this module; host should also
 *    import in docs layout so first paint has tokens)
 *  - Thread demos always use virtualize={false} + explicit pixel height
 */

import { useEffect, useMemo, useRef, useState, type CSSProperties, type ReactNode } from 'react'
import {
  RuntimeProvider,
  ToolRegistryProvider,
  createDefaultToolRegistry,
  createMockRuntime,
  Thread,
  ChatInput,
  Message,
  ToolCallCard,
  ApprovalBanner,
  AskUserCard,
  Reasoning,
  Sources,
  AttachmentList,
  ContextBar,
} from 'jcode-ui'
import type {
  Message as MessageData,
  ToolCall,
  Approval,
  ThreadItem,
  TokenSnapshot,
  TaskContextBreakdown,
} from 'jcode-ui-core'
import 'jcode-ui/styles.css'
import './component-demo.css'
import { DEMO_SOURCES } from './demoSources'
import { highlightDemoCode } from './highlightDemoCode'

export type DemoId =
  | 'thread'
  | 'message'
  | 'chat-input'
  | 'tool-call'
  | 'tool-gallery'
  | 'approval'
  | 'ask-user'
  | 'reasoning'
  | 'sources'
  | 'attachment'
  | 'context-bar'
  | 'theming'

export interface ComponentDemoProps {
  id: DemoId | string
  height?: number
  /** Show Preview | Code tabs (default true when a source snippet exists). */
  showCode?: boolean
}

const ts = 1_700_000_000_000 // stable across HMR so React keys stay quiet

const sampleUser: MessageData = {
  id: 'demo-u1',
  role: 'user',
  content: 'Fix the race condition in `server.go` and run the tests.',
  timestamp: ts,
}

const sampleAssistant: MessageData = {
  id: 'demo-a1',
  role: 'assistant',
  content:
    "I found an unguarded map write. Here's the fix:\n\n```go\nmu.Lock()\ndefer mu.Unlock()\nm[key] = val\n```\n\nTests are green.",
  timestamp: ts + 1,
  durationMs: 4200,
  reasoning:
    'The panic stack points at concurrent map writes. The handler spawns a goroutine without synchronizing access to the shared map.',
  sources: [
    {
      id: 's1',
      title: 'Go memory model',
      url: 'https://go.dev/ref/mem',
      snippet: 'Happens-before rules for channel operations and mutexes.',
    },
    {
      id: 's2',
      title: 'server.go:48',
      snippet: 'go process(req) // no lock around m[key]',
    },
  ],
}

const toolTerminal: ToolCall = {
  id: 'demo-t1',
  name: 'execute',
  args: JSON.stringify({ command: 'go test ./...' }),
  status: 'done',
  timestamp: ts,
  output: 'ok  \tgithub.com/acme/server\t0.412s\nPASS',
  displayInfo: { title: 'execute', subtitle: 'go test ./...', icon: 'terminal', category: 'execution' },
}

const toolRead: ToolCall = {
  id: 'demo-t2',
  name: 'read',
  args: JSON.stringify({ path: 'server.go' }),
  status: 'done',
  timestamp: ts,
  output: '   1│package main\n   2│\n   3│func handle(r *Request) {\n   4│  go process(r)\n   5│}',
  displayInfo: { title: 'read', subtitle: 'server.go', icon: 'file', category: 'context' },
}

const toolDiff: ToolCall = {
  id: 'demo-t3',
  name: 'edit',
  args: JSON.stringify({
    path: 'server.go',
    old_string: 'go process(r)',
    new_string: 'mu.Lock()\ndefer mu.Unlock()\ngo process(r)',
  }),
  status: 'done',
  timestamp: ts,
  output:
    '@@ -3,3 +3,5 @@\n func handle(r *Request) {\n-  go process(r)\n+  mu.Lock()\n+  defer mu.Unlock()\n+  go process(r)\n }',
  displayInfo: { title: 'edit', subtitle: 'server.go', icon: 'diff', category: 'mutation' },
}

const toolGrep: ToolCall = {
  id: 'demo-t4',
  name: 'grep',
  args: JSON.stringify({ pattern: 'go process', path: '.' }),
  status: 'done',
  timestamp: ts,
  output: 'server.go:4:  go process(r)\nhandler.go:12:  go process(job)',
  displayInfo: { title: 'grep', subtitle: 'go process', icon: 'search', category: 'context' },
}

const toolTodo: ToolCall = {
  id: 'demo-t5',
  name: 'todowrite',
  args: JSON.stringify({
    todos: [
      { id: 1, title: 'Find race', status: 'completed' },
      { id: 2, title: 'Add mutex', status: 'completed' },
      { id: 3, title: 'Run tests', status: 'in_progress' },
    ],
  }),
  status: 'done',
  timestamp: ts,
  displayInfo: { title: 'todowrite', subtitle: '3 tasks', icon: 'todo', category: 'context' },
}

const toolSkill: ToolCall = {
  id: 'demo-t6',
  name: 'load_skill',
  args: JSON.stringify({ name: 'go-concurrency', description: 'Mutex and channel patterns' }),
  status: 'done',
  timestamp: ts,
  output: 'Skill loaded: go-concurrency',
  displayInfo: { title: 'load_skill', subtitle: 'go-concurrency', icon: 'skill', category: 'context' },
}

const toolTeam: ToolCall = {
  id: 'demo-t7',
  name: 'team_list',
  args: '{}',
  status: 'done',
  timestamp: ts,
  output: 'Team: ship-it (2 members)\n@lead status=idle type=coordinator\n@tester status=busy type=worker',
  displayInfo: { title: 'team_list', subtitle: '2 members', icon: 'team', category: 'context' },
}

const toolGeneric: ToolCall = {
  id: 'demo-t8',
  name: 'custom_deploy',
  args: JSON.stringify({ env: 'staging', region: 'us-east-1' }),
  status: 'done',
  timestamp: ts,
  output: '{"ok":true,"url":"https://staging.example.com"}',
  displayInfo: { title: 'custom_deploy', subtitle: 'staging', category: 'execution' },
}

const sampleApproval: Approval = {
  id: 'demo-ap1',
  tool_name: 'execute',
  tool_args: JSON.stringify({ command: 'rm -rf ./build' }),
  is_external: false,
}

const sampleAskTool: ToolCall = {
  id: 'demo-ask1',
  name: 'ask_user',
  args: '{}',
  status: 'running',
  timestamp: ts,
  askUserId: 'demo-ask1',
  askUserQuestions: [
    {
      question: 'Which test strategy should I use?',
      header: 'test strategy',
      options: [
        { label: 'Unit only', description: 'Fast, package-local' },
        { label: 'Integration', description: 'Hit the real DB' },
        { label: 'Both', description: 'Unit then integration' },
      ],
    },
  ],
  displayInfo: { title: 'ask_user', subtitle: '1 question', category: 'context' },
}

const tokenSnapshot: TokenSnapshot = {
  total_tokens: 98000,
  model_context_limit: 128000,
  prompt_tokens: 92000,
  completion_tokens: 6000,
}

const contextBreakdown: TaskContextBreakdown = {
  context_limit: 128000,
  system_prompt_tokens: 1200,
  system_tools_tokens: 8400,
  mcp_tools_tokens: 2100,
  skills_tokens: 900,
  messages_tokens: 85400,
}

function msgItem(data: MessageData, seq: number): ThreadItem {
  return { kind: 'message', data, seq }
}
function toolItem(data: ToolCall, seq: number): ThreadItem {
  return { kind: 'tool', data, seq }
}

/** Shared registry — building default renderers once is much cheaper. */
let sharedRegistry: ReturnType<typeof createDefaultToolRegistry> | null = null
function getSharedRegistry() {
  if (!sharedRegistry) sharedRegistry = createDefaultToolRegistry()
  return sharedRegistry
}

function useVisible(rootMargin = '200px'): [React.RefCallback<HTMLDivElement>, boolean] {
  const nodeRef = useRef<HTMLDivElement | null>(null)
  const [visible, setVisible] = useState(false)
  const setRef = useMemo(
    () => (el: HTMLDivElement | null) => {
      nodeRef.current = el
    },
    [],
  )
  useEffect(() => {
    const el = nodeRef.current
    if (!el || visible) return
    if (typeof IntersectionObserver === 'undefined') {
      setVisible(true)
      return
    }
    const io = new IntersectionObserver(
      (entries) => {
        if (entries.some((e) => e.isIntersecting)) {
          setVisible(true)
          io.disconnect()
        }
      },
      { rootMargin, threshold: 0.01 },
    )
    io.observe(el)
    return () => io.disconnect()
  }, [visible, rootMargin])
  return [setRef, visible]
}

function DemoChrome({
  id,
  height,
  showCode,
  children,
}: {
  id: string
  height?: number
  showCode: boolean
  children: ReactNode
}) {
  const code = DEMO_SOURCES[id]
  const canCode = showCode && Boolean(code)
  const [tab, setTab] = useState<'preview' | 'code'>('preview')
  const [copied, setCopied] = useState(false)
  const previewH = height ?? (id === 'thread' ? 420 : id === 'chat-input' ? 160 : undefined)

  const highlighted = useMemo(
    () => (code ? highlightDemoCode(code, 'tsx') : ''),
    [code],
  )

  const copy = () => {
    if (!code) return
    void navigator.clipboard?.writeText(code).then(() => {
      setCopied(true)
      setTimeout(() => setCopied(false), 1600)
    })
  }

  return (
    <div
      className="jcode-demo jcode-demo-shell"
      style={previewH && tab === 'preview' ? { height: previewH + (canCode ? 40 : 0) } : undefined}
    >
      {canCode && (
        <div className="jcode-demo-tabbar">
          <div className="jcode-demo-tabs">
            <button
              type="button"
              className={tab === 'preview' ? 'active' : ''}
              onClick={() => setTab('preview')}
            >
              Preview
            </button>
            <button type="button" className={tab === 'code' ? 'active' : ''} onClick={() => setTab('code')}>
              Code
            </button>
          </div>
          <div className="jcode-demo-tab-actions">
            {tab === 'code' && (
              <button type="button" className="jcode-demo-copy" onClick={copy}>
                {copied ? 'Copied ✓' : 'Copy'}
              </button>
            )}
            <span className="jcode-demo-label">{id}</span>
          </div>
        </div>
      )}
      {!canCode && (
        <div className="jcode-demo-chrome">
          <span className="jcode-demo-dot red" />
          <span className="jcode-demo-dot yellow" />
          <span className="jcode-demo-dot green" />
          <span className="jcode-demo-label">live preview · {id}</span>
        </div>
      )}
      {tab === 'preview' ? (
        <div
          className="jcode-demo-preview-slot dark"
          style={{
            height: previewH,
            minHeight: previewH ?? 80,
            display: 'flex',
            flexDirection: 'column',
            overflow: 'hidden',
          }}
        >
          {children}
        </div>
      ) : (
        <pre className="jcode-demo-code">
          <code
            className="hljs language-tsx"
            dangerouslySetInnerHTML={{ __html: highlighted }}
          />
        </pre>
      )}
    </div>
  )
}

function WithRuntime({
  children,
  items = [],
  token,
  style,
  className,
}: {
  children: ReactNode
  items?: ThreadItem[]
  token?: TokenSnapshot | null
  style?: CSSProperties
  className?: string
}) {
  const registry = useMemo(() => getSharedRegistry(), [])
  const runtime = useMemo(
    () =>
      createMockRuntime({
        items,
        state: token ? { tokenSnapshot: token } : undefined,
      }),
    // fixtures are static per demo mount
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [],
  )
  return (
    <div className={className} style={style}>
      <RuntimeProvider runtime={runtime}>
        <ToolRegistryProvider registry={registry}>{children}</ToolRegistryProvider>
      </RuntimeProvider>
    </div>
  )
}

function DemoBody({ id }: { id: string }): ReactNode {
  switch (id) {
    case 'thread':
      return (
        <WithRuntime
          className="jcode-live-demo__body"
          style={{ flex: 1, minHeight: 0, height: '100%' }}
          items={[
            msgItem(sampleUser, 1),
            toolItem(toolRead, 2),
            toolItem(toolDiff, 3),
            msgItem(sampleAssistant, 4),
          ]}
          token={tokenSnapshot}
        >
          <div className="jcode-live-demo__thread">
            <Thread virtualize={false} overscanBottom={12} />
          </div>
          <div className="jcode-live-demo__composer">
            <ChatInput placeholder="Type to try the composer…" showContextBar />
          </div>
        </WithRuntime>
      )

    case 'message':
      return (
        <WithRuntime className="jcode-demo-pad" style={{ overflow: 'auto', flex: 1 }}>
          <Message message={sampleUser} canEdit />
          <Message message={sampleAssistant} />
        </WithRuntime>
      )

    case 'chat-input':
      return (
        <WithRuntime className="jcode-demo-pad" style={{ flex: 1 }} token={tokenSnapshot}>
          <ChatInput
            slashCommands={[
              { slash: '/goal', description: 'Set the session goal' },
              { slash: '/compact', description: 'Compact conversation context' },
            ]}
            allowImages
            showContextBar
            placeholder="Send a message… try / for slash commands"
          />
        </WithRuntime>
      )

    case 'tool-call':
      return (
        <WithRuntime className="jcode-demo-pad" style={{ overflow: 'auto', flex: 1 }}>
          <ToolCallCard tool={toolTerminal} />
          <div style={{ height: 8 }} />
          <ToolCallCard tool={toolDiff} />
        </WithRuntime>
      )

    case 'tool-gallery':
      return (
        <WithRuntime className="jcode-demo-pad" style={{ overflow: 'auto', flex: 1 }}>
          <div className="jcode-demo-gallery">
            {[toolTerminal, toolRead, toolDiff, toolGrep, toolTodo, toolSkill, toolTeam, toolGeneric].map(
              (t) => (
                <div key={t.id} className="jcode-demo-gallery-item">
                  <div className="jcode-demo-gallery-label">{t.name}</div>
                  <ToolCallCard tool={t} />
                </div>
              ),
            )}
          </div>
        </WithRuntime>
      )

    case 'approval':
      return (
        <WithRuntime className="jcode-demo-pad" style={{ overflow: 'auto', flex: 1 }}>
          <ApprovalBanner approval={sampleApproval} />
          <div style={{ height: 12 }} />
          <ApprovalBanner
            approval={{
              ...sampleApproval,
              id: 'demo-ap2',
              resolved: true,
              approved: true,
            }}
          />
        </WithRuntime>
      )

    case 'ask-user':
      return (
        <WithRuntime className="jcode-demo-pad" style={{ overflow: 'auto', flex: 1 }}>
          <AskUserCard tool={sampleAskTool} />
        </WithRuntime>
      )

    case 'reasoning':
      return (
        <div className="jcode-demo-pad" style={{ overflow: 'auto', flex: 1 }}>
          <Reasoning reasoning={sampleAssistant.reasoning!} durationMs={3200} defaultExpanded />
        </div>
      )

    case 'sources':
      return (
        <div className="jcode-demo-pad" style={{ overflow: 'auto', flex: 1 }}>
          <Sources sources={sampleAssistant.sources!} />
        </div>
      )

    case 'attachment': {
      // Readable SVG tiles (not 1×1 PNGs that look broken when stretched).
      const svgB64 = (fill: string, label: string) =>
        typeof btoa === 'function'
          ? btoa(
              `<svg xmlns="http://www.w3.org/2000/svg" width="128" height="128" viewBox="0 0 128 128">` +
                `<rect width="128" height="128" rx="16" fill="${fill}"/>` +
                `<text x="64" y="72" text-anchor="middle" font-family="ui-sans-serif,system-ui,sans-serif" font-size="28" font-weight="600" fill="white">${label}</text>` +
                `</svg>`,
            )
          : ''
      return (
        <div className="jcode-demo-pad" style={{ overflow: 'auto', flex: 1 }}>
          <p className="jcode-demo-hint" style={{ marginBottom: 10 }}>
            Composer strip — click tile to preview, × to remove.
          </p>
          <AttachmentList
            images={[
              {
                data: svgB64('#0ea5e9', 'A'),
                media_type: 'image/svg+xml',
                name: 'shot-a.svg',
              },
              {
                data: svgB64('#f97316', 'B'),
                media_type: 'image/svg+xml',
                name: 'shot-b.svg',
              },
            ]}
            onRemove={() => undefined}
            size={64}
          />
          <p className="jcode-demo-hint" style={{ marginTop: 14 }}>
            Enable in ChatInput with <code>allowImages</code> (paste + paperclip).
          </p>
        </div>
      )
    }

    case 'context-bar':
      return (
        <WithRuntime className="jcode-demo-pad" style={{ flex: 1 }} token={tokenSnapshot}>
          <div className="jcode-demo-row">
            <span className="jcode-demo-hint">Token ring (hover for breakdown):</span>
            <ContextBar size={28} breakdown={contextBreakdown} />
            <span className="jcode-demo-hint mono">98k / 128k · 76%</span>
          </div>
        </WithRuntime>
      )

    case 'theming':
      return (
        <div className="jcode-demo-pad" style={{ overflow: 'auto', flex: 1 }}>
          <div className="jcode-demo-swatches">
            {[
              ['--color-primary', 'Primary'],
              ['--color-background', 'Background'],
              ['--color-surface', 'Surface'],
              ['--color-foreground', 'Foreground'],
              ['--color-border', 'Border'],
              ['--color-success-fg', 'Success'],
              ['--color-error-fg', 'Error'],
              ['--color-warning-fg', 'Warning'],
            ].map(([token, label]) => (
              <div key={token} className="jcode-demo-swatch">
                <div className="jcode-demo-swatch-chip" style={{ background: `var(${token})` }} />
                <code>{token}</code>
                <span>{label}</span>
              </div>
            ))}
          </div>
        </div>
      )

    default:
      return (
        <div className="jcode-demo-missing" style={{ padding: 16 }}>
          Unknown demo: <code>{id}</code>
        </div>
      )
  }
}

export function ComponentDemo({ id, height, showCode = true }: ComponentDemoProps) {
  const [hostRef, visible] = useVisible('240px')
  const placeholderH = height ?? (id === 'thread' ? 420 : id === 'chat-input' ? 160 : 120)

  return (
    <div ref={hostRef}>
      {!visible ? (
        <div
          className="jcode-demo jcode-demo-placeholder"
          style={{ minHeight: placeholderH }}
          aria-hidden
        >
          <div className="jcode-demo-chrome">
            <span className="jcode-demo-dot red" />
            <span className="jcode-demo-dot yellow" />
            <span className="jcode-demo-dot green" />
            <span className="jcode-demo-label">loading preview…</span>
          </div>
          <div className="jcode-demo-placeholder-body" />
        </div>
      ) : (
        <DemoChrome id={id} height={height} showCode={showCode}>
          <DemoBody id={id} />
        </DemoChrome>
      )}
    </div>
  )
}
