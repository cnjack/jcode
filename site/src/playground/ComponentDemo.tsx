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
  useRuntimeState,
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
  ThreadWelcome,
  Suggestions,
  ConnectionBanner,
  ModelSelector,
  TaskList,
  Artifact,
  ThreadList,
} from 'jcode-ui'
import type { ModelSelectorOption } from 'jcode-ui'
import { TestResultsRenderer, StackTraceRenderer } from 'jcode-ui/tool-renderers'
import { createMockThreadStore, ThreadStoreProvider } from 'jcode-ui-core'
import type {
  Message as MessageData,
  ToolCall,
  Approval,
  ThreadItem,
  TokenSnapshot,
  TaskContextBreakdown,
  TodoItem,
  ThreadSummary,
  ConnectionState,
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
  | 'tool-gallery-2'
  | 'approval'
  | 'ask-user'
  | 'reasoning'
  | 'sources'
  | 'attachment'
  | 'context-bar'
  | 'theming'
  | 'welcome'
  | 'branching'
  | 'connection'
  | 'tasklist'
  | 'model-selector'
  | 'artifact'
  | 'thread-list'

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

// ─── 0.2.0 fixtures ───

/** A branched assistant message: `content` mirrors the active version (v2). */
const branchingAssistant: MessageData = {
  id: 'demo-branch',
  role: 'assistant',
  content: 'Use `sync.Map` for the shared registry — lock-free reads, no explicit mutex.',
  timestamp: ts,
  durationMs: 3800,
  activeVersionId: 'v2',
  versions: [
    {
      id: 'v1',
      content: 'Wrap every map access in a `sync.Mutex` — simplest correct fix.',
      timestamp: ts,
    },
    {
      id: 'v2',
      content: 'Use `sync.Map` for the shared registry — lock-free reads, no explicit mutex.',
      timestamp: ts + 1,
    },
    {
      id: 'v3',
      content: 'Shard the map by key hash into N buckets to cut lock contention under load.',
      timestamp: ts + 2,
    },
  ],
}

/** 8 models across 3 providers, for the ModelSelector demo. */
const demoModels: ModelSelectorOption[] = [
  { id: 'claude-opus-4', label: 'Claude Opus 4', provider: 'Anthropic', description: 'Most capable' },
  { id: 'claude-sonnet-4', label: 'Claude Sonnet 4', provider: 'Anthropic', description: 'Balanced' },
  { id: 'claude-haiku-4', label: 'Claude Haiku 4', provider: 'Anthropic', description: 'Fastest' },
  { id: 'gpt-5', label: 'GPT-5', provider: 'OpenAI', description: 'Flagship' },
  { id: 'gpt-5-mini', label: 'GPT-5 mini', provider: 'OpenAI' },
  { id: 'o4', label: 'o4', provider: 'OpenAI', description: 'Reasoning' },
  { id: 'gemini-2.5-pro', label: 'Gemini 2.5 Pro', provider: 'Google' },
  { id: 'gemini-2.5-flash', label: 'Gemini 2.5 Flash', provider: 'Google' },
]

/** 4 mixed-status tasks (all four status icons represented). */
const demoTaskItems: TodoItem[] = [
  { id: 1, title: 'Read the current parser', status: 'completed' },
  { id: 2, title: 'Extract the tokenizer into its own package', status: 'in_progress' },
  { id: 3, title: 'Add table-driven tests', status: 'pending' },
  { id: 4, title: 'Delete the legacy regex shim', status: 'cancelled' },
]

/** Relative-time anchor captured once so the ThreadList reads "5m ago" etc.
 *  Not used as a React key (rows key on thread.id), so this is display-only. */
const listNow = Date.now()

/** 5 threads, one running + two archived, for the ThreadList demo. */
const demoThreads: ThreadSummary[] = [
  { id: 'th1', title: 'Refactor auth middleware', updatedAt: listNow - 3 * 60_000, status: 'running' },
  { id: 'th2', title: 'Fix flaky payment test', updatedAt: listNow - 42 * 60_000, status: 'idle' },
  { id: 'th3', title: 'Draft the v0.2 release notes', updatedAt: listNow - 5 * 3_600_000, status: 'idle' },
  { id: 'th4', title: 'Investigate the memory leak', updatedAt: listNow - 3 * 86_400_000, status: 'idle', archived: true },
  { id: 'th5', title: 'Bump deps to latest', updatedAt: listNow - 21 * 86_400_000, status: 'idle', archived: true },
]

const listDirOutput = [
  'src/',
  'src/index.ts',
  'src/components/',
  'src/components/TaskList.tsx',
  'src/components/Artifact.tsx',
  'src/toolRenderers/',
  'src/toolRenderers/fileTree.tsx',
  'src/toolRenderers/testResults.tsx',
  'src/toolRenderers/stackTrace.tsx',
  'README.md',
  'package.json',
].join('\n')

const goTestOutput = [
  '=== RUN   TestAddition',
  '--- PASS: TestAddition (0.00s)',
  '=== RUN   TestSubtraction',
  '--- FAIL: TestSubtraction (0.01s)',
  '    math_test.go:24: Subtract(5, 3) = 1; want 2',
  '    math_test.go:25: values diverged after carry',
  '=== RUN   TestSkipped',
  '--- SKIP: TestSkipped (0.00s)',
  '    math_test.go:30: not implemented yet',
  'FAIL',
  'FAIL\tgithub.com/cnjack/jcode/math\t0.012s',
].join('\n')

const jsStack = [
  "TypeError: Cannot read properties of undefined (reading 'name')",
  '    at renderUser (/app/src/user.ts:42:18)',
  '    at Object.render (/app/src/app.ts:12:7)',
  '    at processTicksAndRejections (node:internal/process/task_queues:95:5)',
  '    at /app/node_modules/vite/dist/node/chunks/dep.js:1200:14',
].join('\n')

const artifactFile = [
  "import { defineConfig } from 'vite'",
  "import react from '@vitejs/plugin-react'",
  '',
  'export default defineConfig({',
  '  plugins: [react()],',
  '  server: { port: 5173 },',
  '})',
].join('\n')

const toolListDir: ToolCall = {
  id: 'demo-tg2-1',
  name: 'list_dir',
  args: JSON.stringify({ path: 'src' }),
  status: 'done',
  timestamp: ts,
  output: listDirOutput,
  displayOutput: listDirOutput,
  displayInfo: { title: 'list_dir', subtitle: 'src', icon: 'search', category: 'context' },
}

const toolGoTest: ToolCall = {
  id: 'demo-tg2-2',
  name: 'go_test',
  args: JSON.stringify({ command: 'go test ./math' }),
  status: 'error',
  timestamp: ts,
  output: goTestOutput,
  displayOutput: goTestOutput,
  meta: { exit_code: 1, duration_ms: 120 },
  displayInfo: { title: 'go test', subtitle: './math', category: 'execution' },
}

const toolJsStack: ToolCall = {
  id: 'demo-tg2-3',
  name: 'panic',
  args: JSON.stringify({ command: 'node app.js' }),
  status: 'error',
  timestamp: ts,
  error: jsStack,
  output: jsStack,
  displayInfo: { title: 'stack trace', subtitle: 'TypeError', category: 'execution' },
}

/** Gallery registry: defaults (list_dir → FileTree) + the two code renderers
 *  mapped onto demo tool names (mirrors the p5 fixture convention). Built once,
 *  never mutating the shared registry other demos read. */
let galleryRegistry2: ReturnType<typeof createDefaultToolRegistry> | null = null
function getGalleryRegistry2() {
  if (!galleryRegistry2) {
    const r = createDefaultToolRegistry()
    r.register('go_test', TestResultsRenderer)
    r.register('panic', StackTraceRenderer)
    galleryRegistry2 = r
  }
  return galleryRegistry2
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
    const margin = parseInt(rootMargin, 10) || 200
    const inView = () => {
      const r = el.getBoundingClientRect()
      return r.bottom > -margin && r.top < window.innerHeight + margin
    }
    // Sync check first: covers elements already in view at mount, and
    // environments (embedded webviews, automation panes) where IO callbacks
    // never fire. The interval is the same safety net for late scrolls there.
    if (typeof IntersectionObserver === 'undefined' || inView()) {
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
    const poll = setInterval(() => {
      if (inView()) setVisible(true)
    }, 600)
    return () => {
      io.disconnect()
      clearInterval(poll)
    }
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
  // Light by default: the docs page is light, and dark-on-dark previews made
  // host-styled text unreadable before the isolation guards landed. The moon
  // toggle shows each component's dark take.
  const [theme, setTheme] = useState<'light' | 'dark'>('light')
  const [copied, setCopied] = useState(false)
  const previewH = height ?? (id === 'thread' ? 420 : id === 'chat-input' ? 160 : undefined)
  // Scrolling demos need overflow clipping; static ones must not clip hover
  // popovers (ContextBar's breakdown escapes the slot box).
  const clips = id === 'thread' || id === 'chat-input'

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
            {tab === 'preview' && (
              <button
                type="button"
                className="jcode-demo-theme-toggle"
                onClick={() => setTheme(theme === 'dark' ? 'light' : 'dark')}
                title={theme === 'dark' ? 'Switch preview to light' : 'Switch preview to dark'}
              >
                {theme === 'dark' ? '☀' : '☾'}
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
          <button
            type="button"
            className="jcode-demo-theme-toggle"
            onClick={() => setTheme(theme === 'dark' ? 'light' : 'dark')}
            title={theme === 'dark' ? 'Switch preview to light' : 'Switch preview to dark'}
          >
            {theme === 'dark' ? '☀' : '☾'}
          </button>
        </div>
      )}
      {tab === 'preview' ? (
        <div
          className={`jcode-demo-preview-slot ${theme}`}
          style={{
            height: previewH,
            minHeight: previewH ?? 80,
            display: 'flex',
            flexDirection: 'column',
            overflow: clips ? 'hidden' : 'visible',
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

/** Reads the live message from the runtime so switchVersion re-renders it. */
function BranchingBody() {
  const { items } = useRuntimeState()
  const msg = items.find((i) => i.kind === 'message')
  if (!msg || msg.kind !== 'message') return null
  return <Message message={msg.data} />
}

/** Owns its own runtime so the buttons can patchState({ connection }). */
function ConnectionDemo() {
  const runtime = useMemo(
    () => createMockRuntime({ state: { connection: 'reconnecting' } }),
    [],
  )
  const states: ConnectionState[] = ['connected', 'reconnecting', 'disconnected']
  return (
    <RuntimeProvider runtime={runtime}>
      <div className="jcode-demo-pad" style={{ overflow: 'auto', flex: 1 }}>
        <div className="jcode-demo-row" style={{ marginBottom: 12 }}>
          {states.map((s) => (
            <button
              key={s}
              type="button"
              className="jcode-btn jcode-btn-secondary"
              onClick={() => runtime.patchState({ connection: s })}
            >
              {s}
            </button>
          ))}
        </div>
        <ConnectionBanner />
        <p className="jcode-demo-hint">
          <code>connected</code> renders nothing (recovering flashes a 2s
          &ldquo;Reconnected&rdquo;). Switch states to see the banner.
        </p>
      </div>
    </RuntimeProvider>
  )
}

/** Controlled model picker with a live "selected" readout. */
function ModelSelectorDemo() {
  const [value, setValue] = useState('claude-opus-4')
  return (
    <div
      className="jcode-demo-pad"
      style={{ display: 'flex', flexDirection: 'column', flex: 1, justifyContent: 'flex-end' }}
    >
      <p className="jcode-demo-hint" style={{ marginTop: 0 }}>
        Menu opens upward — the composer sits at the bottom of the thread.
      </p>
      <div className="jcode-demo-row">
        <ModelSelector models={demoModels} value={value} onChange={setValue} />
        <span className="jcode-demo-hint mono" style={{ marginTop: 0 }}>
          value: {value}
        </span>
      </div>
    </div>
  )
}

/** Artifact container wrapping file content, with a working Copy action. */
function ArtifactDemo() {
  const [copied, setCopied] = useState(false)
  const copy = () => {
    void navigator.clipboard?.writeText(artifactFile).then(() => {
      setCopied(true)
      setTimeout(() => setCopied(false), 1500)
    })
  }
  return (
    <div className="jcode-demo-pad" style={{ overflow: 'auto', flex: 1 }}>
      <Artifact
        title="vite.config.ts"
        subtitle="7 lines · typescript"
        icon={
          <svg viewBox="0 0 16 16" width="14" height="14" aria-hidden>
            <path
              d="M4 1.5h5L13 5.5V14a.5.5 0 0 1-.5.5h-8A.5.5 0 0 1 4 14z"
              fill="none"
              stroke="currentColor"
              strokeWidth="1.2"
            />
          </svg>
        }
        actions={
          <button type="button" className="jcode-btn jcode-btn-secondary" onClick={copy}>
            {copied ? 'Copied ✓' : 'Copy'}
          </button>
        }
      >
        <pre
          style={{
            margin: 0,
            padding: '0.75rem 0.9rem',
            fontFamily: 'var(--jcode-font-mono)',
            fontSize: 12.5,
            lineHeight: 1.6,
            whiteSpace: 'pre-wrap',
          }}
        >
          {artifactFile}
        </pre>
      </Artifact>
    </div>
  )
}

/** Thread list bound to a mock store (all actions wired, so menus render). */
function ThreadListDemo() {
  const store = useMemo(
    () => createMockThreadStore({ threads: demoThreads, activeId: 'th1' }),
    [],
  )
  return (
    <ThreadStoreProvider store={store}>
      <div style={{ height: '100%', maxWidth: 320, overflow: 'auto', padding: 8 }}>
        <ThreadList title="Sessions" />
      </div>
    </ThreadStoreProvider>
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

    case 'welcome':
      return (
        <WithRuntime className="jcode-demo-pad" style={{ overflow: 'auto', flex: 1 }}>
          <ThreadWelcome
            title="What can I help you ship?"
            subtitle="Ask about your codebase, kick off a task, or pick a starter below."
          >
            <Suggestions
              items={[
                { id: 's1', label: 'Explain this repo', prompt: 'Give me a tour of this repository.' },
                { id: 's2', label: 'Find the race condition', prompt: 'Find and fix the race condition in server.go.' },
                { id: 's3', label: 'Write tests', prompt: 'Add table-driven tests for the parser.' },
              ]}
            />
          </ThreadWelcome>
        </WithRuntime>
      )

    case 'branching':
      return (
        <WithRuntime
          className="jcode-demo-pad"
          style={{ overflow: 'auto', flex: 1 }}
          items={[msgItem(branchingAssistant, 1)]}
        >
          <BranchingBody />
        </WithRuntime>
      )

    case 'connection':
      return <ConnectionDemo />

    case 'tasklist':
      return (
        <div className="jcode-demo-pad" style={{ overflow: 'auto', flex: 1 }}>
          <TaskList title="Ship the parser refactor" items={demoTaskItems} />
        </div>
      )

    case 'model-selector':
      return <ModelSelectorDemo />

    case 'artifact':
      return <ArtifactDemo />

    case 'thread-list':
      return <ThreadListDemo />

    case 'tool-gallery-2': {
      const reg = getGalleryRegistry2()
      return (
        <WithRuntime className="jcode-demo-pad" style={{ overflow: 'auto', flex: 1 }}>
          <p className="jcode-demo-hint" style={{ marginTop: 0, marginBottom: 8 }}>
            Three new code renderers — click a card header to expand its output.
          </p>
          <div className="jcode-demo-gallery">
            {[toolListDir, toolGoTest, toolJsStack].map((t) => (
              <div key={t.id} className="jcode-demo-gallery-item">
                <div className="jcode-demo-gallery-label">{t.name}</div>
                <ToolCallCard tool={t} registry={reg} />
              </div>
            ))}
          </div>
        </WithRuntime>
      )
    }

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
