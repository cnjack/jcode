/**
 * Fixture timeline for visual acceptance of exploring groups, terminal
 * dual-channel output, compact subagent cards, and the P5 first-class
 * components (TaskList, FileTree, TestResults, StackTrace, Artifact).
 */
import { createRoot } from 'react-dom/client'
import { DocumentTextIcon } from '@heroicons/react/24/outline'
import {
  RuntimeProvider,
  createMockRuntime,
  Thread,
  ToolRegistryProvider,
  createDefaultToolRegistry,
  ToolCallCard,
  TaskList,
  Artifact,
} from '../src/index.ts'
import { TestResultsRenderer, StackTraceRenderer } from '../src/toolRenderers/index.ts'
import type { ReactNode } from 'react'
import type { ThreadItem, ToolCall, TodoItem } from 'jcode-ui-core'
import '../src/styles/entry.css'
import '../src/styles/p5.css'

function tool(partial: Partial<ToolCall> & Pick<ToolCall, 'id' | 'name'>): ToolCall {
  return {
    args: '{}',
    status: 'done',
    timestamp: Date.now(),
    ...partial,
  }
}

const longStdout = Array.from({ length: 20 }, (_, i) => `stdout line ${i + 1}`).join('\n')
const longStderr = Array.from({ length: 12 }, (_, i) => `stderr line ${i + 1}`).join('\n')

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
  'src/styles/',
  'src/styles/p5.css',
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

const fileContent = [
  "import { defineConfig } from 'vite'",
  "import react from '@vitejs/plugin-react'",
  '',
  'export default defineConfig({',
  '  plugins: [react()],',
  '  server: { port: 5173 },',
  '})',
].join('\n')

const taskItems: TodoItem[] = [
  { id: 1, title: 'Extract TaskList into a first-class component', status: 'completed' },
  { id: 2, title: 'Add FileTree / TestResults / StackTrace renderers', status: 'in_progress' },
  { id: 3, title: 'Wire the Artifact container', status: 'pending' },
  { id: 4, title: 'Drop the legacy inline todo markup', status: 'cancelled' },
]

const items: ThreadItem[] = [
  {
    kind: 'message',
    seq: 1,
    data: {
      id: 'm1',
      role: 'assistant',
      content: '### Exploring group\nAdjacent read/search tools collapse into one summary.',
      timestamp: Date.now(),
    },
  },
  {
    kind: 'tool',
    seq: 2,
    data: tool({
      id: 't1',
      name: 'read',
      displayInfo: { title: 'Read', subtitle: 'execute.go', category: 'context', kind: 'read', collapsible: true },
    }),
  },
  {
    kind: 'tool',
    seq: 3,
    data: tool({
      id: 't2',
      name: 'read',
      displayInfo: { title: 'Read', subtitle: 'web.go', category: 'context', kind: 'read', collapsible: true },
    }),
  },
  {
    kind: 'tool',
    seq: 4,
    data: tool({
      id: 't3',
      name: 'grep',
      displayInfo: { title: 'Search', subtitle: 'ToolOutput', category: 'context', kind: 'search', collapsible: true },
    }),
  },
  {
    kind: 'tool',
    seq: 5,
    data: tool({
      id: 't4',
      name: 'glob',
      displayInfo: { title: 'Glob', subtitle: '**/*_test.go', category: 'context', kind: 'search', collapsible: true },
    }),
  },
  {
    kind: 'message',
    seq: 6,
    data: {
      id: 'm2',
      role: 'assistant',
      content: '### Terminal success',
      timestamp: Date.now(),
    },
  },
  {
    kind: 'tool',
    seq: 7,
    data: tool({
      id: 'term-ok',
      name: 'execute',
      args: JSON.stringify({ command: 'echo hello && seq 1 20', description: 'Print greeting' }),
      status: 'done',
      displayInfo: { title: 'Shell', subtitle: 'Print greeting', category: 'execution', kind: 'shell' },
      streams: { stdout: 'hello\n' + longStdout, stderr: '', aggregated: 'hello\n' + longStdout },
      meta: { exit_code: 0, duration_ms: 240 },
      displayOutput: 'hello\n' + longStdout,
    }),
  },
  {
    kind: 'message',
    seq: 8,
    data: {
      id: 'm3',
      role: 'assistant',
      content: '### Terminal failure + stderr',
      timestamp: Date.now(),
    },
  },
  {
    kind: 'tool',
    seq: 9,
    data: tool({
      id: 'term-err',
      name: 'execute',
      args: JSON.stringify({ command: 'go test ./broken', description: 'Run broken tests' }),
      status: 'error',
      displayInfo: { title: 'Shell', subtitle: 'Run broken tests', category: 'execution', kind: 'shell' },
      streams: {
        stdout: longStdout,
        stderr: longStderr + '\nFAIL\tgithub.com/cnjack/jcode/broken\t0.01s',
        aggregated: longStdout + '\n' + longStderr,
      },
      meta: { exit_code: 1, duration_ms: 1820 },
      displayOutput: longStdout + '\n' + longStderr,
    }),
  },
  {
    kind: 'message',
    seq: 10,
    data: {
      id: 'm-tree',
      role: 'assistant',
      content: '### FileTree renderer (list_dir / glob)',
      timestamp: Date.now(),
    },
  },
  {
    kind: 'tool',
    seq: 11,
    data: tool({
      id: 'ls1',
      name: 'list_dir',
      args: JSON.stringify({ path: 'packages/jcode-ui/src' }),
      displayInfo: { title: 'List', subtitle: 'packages/jcode-ui/src', category: 'context', kind: 'search' },
      output: listDirOutput,
      displayOutput: listDirOutput,
    }),
  },
  {
    kind: 'message',
    seq: 12,
    data: {
      id: 'm4',
      role: 'assistant',
      content: '### Subagent compact card',
      timestamp: Date.now(),
    },
  },
  {
    kind: 'tool',
    seq: 13,
    data: tool({
      id: 'sa1',
      name: 'subagent',
      args: JSON.stringify({ name: 'explore', description: 'Map tool output dual-channel' }),
      status: 'done',
      displayInfo: { title: 'Subagent', subtitle: 'Map tool output dual-channel', kind: 'agent' },
      children: [
        tool({
          id: 'sc1',
          name: 'read',
          displayInfo: { title: 'Read', subtitle: 'exec_format.go', category: 'context', kind: 'read', collapsible: true },
        }),
        tool({
          id: 'sc2',
          name: 'grep',
          displayInfo: { title: 'Search', subtitle: 'BuildExecResult', category: 'context', kind: 'search', collapsible: true },
        }),
        tool({
          id: 'sc3',
          name: 'read',
          displayInfo: { title: 'Read', subtitle: 'ToolCallCard.tsx', category: 'context', kind: 'read', collapsible: true },
        }),
        tool({
          id: 'sc4',
          name: 'execute',
          args: JSON.stringify({ command: 'go test ./internal/tools -run Format' }),
          displayInfo: { title: 'Shell', subtitle: 'go test ./internal/tools', kind: 'shell', collapsible: false },
          meta: { exit_code: 0, duration_ms: 1100 },
        }),
      ],
      displayOutput: 'Found dual-channel `BuildExecResult` + exploring groups in the timeline.\n\n- Model string keeps STDOUT/STDERR labels\n- UI uses streams/meta',
    }),
  },
]

const runtime = createMockRuntime({ items, isRunning: false })
const registry = createDefaultToolRegistry()

// P5-section registry: map demo tool names onto the new renderers so a host can
// see how `registry.register('execute', TestResultsRenderer)` etc. would look.
const p5Registry = createDefaultToolRegistry()
p5Registry.register('go_test', TestResultsRenderer)
p5Registry.register('panic', StackTraceRenderer)

const goTestTool = tool({
  id: 'gt1',
  name: 'go_test',
  args: JSON.stringify({ command: 'go test ./math' }),
  status: 'error',
  displayInfo: { title: 'Tests', subtitle: 'go test ./math', category: 'execution', kind: 'shell' },
  output: goTestOutput,
  displayOutput: goTestOutput,
  meta: { exit_code: 1, duration_ms: 120 },
})

const panicTool = tool({
  id: 'pn1',
  name: 'panic',
  args: JSON.stringify({ command: 'node app.js' }),
  status: 'error',
  displayInfo: { title: 'Stack', subtitle: 'TypeError', category: 'execution', kind: 'shell' },
  error: jsStack,
  output: jsStack,
})

function Panel({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section style={{ marginTop: 24 }}>
      <h2 style={{ fontSize: 13, marginBottom: 8, opacity: 0.7 }}>{title}</h2>
      <div
        data-jcode-ui=""
        style={{
          maxWidth: 720,
          border: '1px solid var(--jcode-color-border)',
          borderRadius: 12,
          padding: 16,
          background: 'var(--jcode-color-surface)',
        }}
      >
        {children}
      </div>
    </section>
  )
}

function App() {
  return (
    <div
      style={{
        minHeight: '100vh',
        background: 'var(--color-background, #0f1115)',
        color: 'var(--color-foreground, #e8e8ec)',
        padding: '24px',
        fontFamily: 'var(--font-sans, system-ui, sans-serif)',
      }}
    >
      <h1 style={{ fontSize: 16, marginBottom: 16, opacity: 0.8 }}>Tool output UX fixture</h1>
      <div
        id="fixture-thread"
        style={{
          maxWidth: 720,
          height: '70vh',
          border: '1px solid var(--color-border, #333)',
          borderRadius: 12,
          overflow: 'hidden',
          background: 'var(--color-background, #0f1115)',
        }}
      >
        <RuntimeProvider runtime={runtime}>
          <ToolRegistryProvider registry={registry}>
            <Thread virtualize={false} />
          </ToolRegistryProvider>
        </RuntimeProvider>
      </div>

      <ToolRegistryProvider registry={p5Registry}>
        <Panel title="TaskList (first-class)">
          <TaskList title="P5 promotion" items={taskItems} />
        </Panel>

        <Panel title="TestResults renderer (go test)">
          <ToolCallCard tool={goTestTool} />
        </Panel>

        <Panel title="StackTrace renderer (JS Error)">
          <ToolCallCard tool={panicTool} />
        </Panel>

        <Panel title="Artifact container (wrapping file content)">
          <Artifact
            title="vite.config.ts"
            subtitle="7 lines · typescript"
            icon={<DocumentTextIcon />}
            actions={<span style={{ fontSize: 11, opacity: 0.6 }}>Copy</span>}
            onClose={() => {}}
          >
            <pre
              style={{
                margin: 0,
                padding: '0.75rem',
                fontFamily: 'var(--jcode-font-mono)',
                fontSize: 12,
                whiteSpace: 'pre-wrap',
              }}
            >
              {fileContent}
            </pre>
          </Artifact>
        </Panel>
      </ToolRegistryProvider>
    </div>
  )
}

createRoot(document.getElementById('root')!).render(<App />)

// Expand terminal + subagent + P5 demo cards after mount for screenshots.
setTimeout(() => {
  const expandNames = new Set(['execute', 'subagent', 'list_dir', 'go_test', 'panic'])
  document.querySelectorAll<HTMLButtonElement>('.jcode-toolcall > button').forEach((btn) => {
    const card = btn.closest('.jcode-toolcall')
    const name = card?.getAttribute('data-tool-name')
    if (name && expandNames.has(name)) {
      if (card?.getAttribute('data-expanded') !== 'true') btn.click()
    }
  })
  document.body.setAttribute('data-fixture-ready', 'true')
}, 300)
