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
      content:
        '### Activity group — explorative (collapsed)\nAdjacent read/search tools collapse into one `Explored` line; click to expand the bordered row card.',
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
    seq: 20,
    data: {
      id: 'm-batch',
      role: 'assistant',
      content:
        '### Activity group — running (auto-expanded)\nThree parallel shells (same batchId): 2 done + 1 running with a live elapsed row. Header shows `Running…`; the card auto-collapses once everything settles.',
      timestamp: Date.now(),
    },
  },
  {
    kind: 'tool',
    seq: 21,
    data: tool({
      id: 'batch-1',
      name: 'execute',
      args: JSON.stringify({ command: 'go build ./...', description: 'Build all packages' }),
      status: 'done',
      batchId: 'batch-demo',
      batchIndex: 0,
      batchSize: 3,
      startedAt: Date.now() - 9000,
      displayInfo: { title: 'Shell', subtitle: 'go build ./...', category: 'execution', kind: 'shell' },
      streams: { stdout: 'ok', stderr: '', aggregated: 'ok' },
      meta: { exit_code: 0, duration_ms: 3200 },
      displayOutput: 'ok',
    }),
  },
  {
    kind: 'tool',
    seq: 22,
    data: tool({
      id: 'batch-2',
      name: 'execute',
      args: JSON.stringify({ command: 'go vet ./...', description: 'Vet all packages' }),
      status: 'done',
      batchId: 'batch-demo',
      batchIndex: 1,
      batchSize: 3,
      startedAt: Date.now() - 9000,
      displayInfo: { title: 'Shell', subtitle: 'go vet ./...', category: 'execution', kind: 'shell' },
      streams: { stdout: 'ok', stderr: '', aggregated: 'ok' },
      meta: { exit_code: 0, duration_ms: 2600 },
      displayOutput: 'ok',
    }),
  },
  {
    kind: 'tool',
    seq: 23,
    data: tool({
      id: 'batch-3',
      name: 'execute',
      args: JSON.stringify({ command: 'go test ./...', description: 'Run all tests' }),
      status: 'running',
      batchId: 'batch-demo',
      batchIndex: 2,
      batchSize: 3,
      startedAt: Date.now() - 65000,
      displayInfo: { title: 'Shell', subtitle: 'go test ./...', category: 'execution', kind: 'shell' },
    }),
  },
  {
    kind: 'message',
    seq: 24,
    data: {
      id: 'm-activity-done',
      role: 'assistant',
      content:
        '### Activity group — completed mixed (collapsed)\nOne muted line `Ran 1 command · read 1 file · edited 1 file`; expand it, then click a row to open that tool\'s full body (diff/terminal renderers) in place.',
      timestamp: Date.now(),
    },
  },
  {
    kind: 'tool',
    seq: 25,
    data: tool({
      id: 'act-shell',
      name: 'execute',
      args: JSON.stringify({ command: 'pnpm -r build', description: 'Build workspace' }),
      status: 'done',
      displayInfo: { title: 'Shell', subtitle: 'pnpm -r build', category: 'execution', kind: 'shell' },
      streams: { stdout: 'packages built: 3', stderr: '', aggregated: 'packages built: 3' },
      meta: { exit_code: 0, duration_ms: 5200 },
      displayOutput: 'packages built: 3',
    }),
  },
  {
    kind: 'tool',
    seq: 26,
    data: tool({
      id: 'act-read',
      name: 'read',
      args: JSON.stringify({ file_path: 'src/registry.ts' }),
      displayInfo: { title: 'Read', subtitle: 'src/registry.ts', category: 'context', kind: 'read', collapsible: true },
      output: "export const registry = new Map<string, Renderer>()",
      displayOutput: "export const registry = new Map<string, Renderer>()",
    }),
  },
  {
    kind: 'tool',
    seq: 27,
    data: tool({
      id: 'act-edit',
      name: 'edit',
      args: JSON.stringify({
        file_path: 'src/registry.ts',
        old_string: 'export const registry = new Map<string, Renderer>()',
        new_string: "export const registry = new Map<string, Renderer>()\nregistry.set('diff', DiffRenderer)",
      }),
      displayInfo: { title: 'Edit', subtitle: 'src/registry.ts', category: 'mutation', kind: 'edit' },
    }),
  },
  {
    kind: 'message',
    seq: 28,
    data: {
      id: 'm-activity-err',
      role: 'assistant',
      content:
        '### Activity group — failure visible while collapsed\nError icon + `1 failed` (and a muted `1 denied`) stay on the collapsed line.',
      timestamp: Date.now(),
    },
  },
  {
    kind: 'tool',
    seq: 29,
    data: tool({
      id: 'err-shell-1',
      name: 'execute',
      args: JSON.stringify({ command: 'go vet ./...', description: 'Vet all packages' }),
      status: 'error',
      displayInfo: { title: 'Shell', subtitle: 'go vet ./...', category: 'execution', kind: 'shell' },
      streams: { stdout: '', stderr: 'vet: internal/tools/execute.go:12: unused variable', aggregated: 'vet: unused variable' },
      meta: { exit_code: 1, duration_ms: 2600 },
      displayOutput: 'vet: internal/tools/execute.go:12: unused variable',
    }),
  },
  {
    kind: 'tool',
    seq: 29.2,
    data: tool({
      id: 'err-shell-2',
      name: 'execute',
      args: JSON.stringify({ command: 'gofmt -l .', description: 'Check formatting' }),
      status: 'done',
      displayInfo: { title: 'Shell', subtitle: 'gofmt -l .', category: 'execution', kind: 'shell' },
      streams: { stdout: '', stderr: '', aggregated: '' },
      meta: { exit_code: 0, duration_ms: 300 },
    }),
  },
  {
    kind: 'tool',
    seq: 29.4,
    data: tool({
      id: 'err-denied',
      name: 'execute',
      args: JSON.stringify({ command: 'rm -rf dist', description: 'Clean dist' }),
      status: 'done',
      denied: true,
      displayInfo: { title: 'Shell', subtitle: 'rm -rf dist', category: 'execution', kind: 'shell' },
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
      meta: { duration_ms: 42000 },
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
  {
    kind: 'message',
    seq: 14,
    data: {
      id: 'm-sa-running',
      role: 'assistant',
      content: '### Subagent inline progress (running)\nHeader shows `↳ current tool` while a child is running.',
      timestamp: Date.now(),
    },
  },
  {
    kind: 'tool',
    seq: 15,
    data: tool({
      id: 'sa2',
      name: 'subagent',
      args: JSON.stringify({ name: 'fix', description: 'Fix the flaky retry test' }),
      status: 'running',
      startedAt: Date.now() - 23000,
      displayInfo: { title: 'Subagent', subtitle: 'Fix the flaky retry test', kind: 'agent' },
      children: [
        tool({
          id: 'sb1',
          name: 'read',
          displayInfo: { title: 'Read', subtitle: 'retry_test.go', category: 'context', kind: 'read', collapsible: true },
        }),
        tool({
          id: 'sb2',
          name: 'execute',
          status: 'running',
          args: JSON.stringify({ command: 'go test ./internal/retry -run Flaky' }),
          displayInfo: { title: 'Shell', subtitle: 'go test ./internal/retry', kind: 'shell', collapsible: false },
        }),
      ],
    }),
  },
  // ─── Turn-changes demo: a user turn with edits/writes; the Thread inserts
  // a "Changed N files (+A −R)" summary card at the end of the turn. ───
  {
    kind: 'message',
    seq: 30,
    data: {
      id: 'u-turn',
      role: 'user',
      content: 'Apply the config refactor (turn-changes demo).',
      timestamp: Date.now(),
    },
  },
  {
    kind: 'tool',
    seq: 31,
    data: tool({
      id: 'tc-edit-1',
      name: 'edit',
      args: JSON.stringify({
        file_path: 'src/config.ts',
        old_string: "export const config = load('legacy')",
        new_string: "import { schema } from './config.schema.js'\n\nexport const config = load('v2', schema)",
      }),
      displayInfo: { title: 'Edit', subtitle: 'src/config.ts', category: 'mutation', kind: 'edit' },
    }),
  },
  {
    kind: 'tool',
    seq: 32,
    data: tool({
      id: 'tc-write-1',
      name: 'write',
      args: JSON.stringify({
        file_path: 'src/config.schema.json',
        content: '{\n  "$schema": "https://json-schema.org/draft-07/schema",\n  "type": "object",\n  "required": ["port"]\n}',
      }),
      displayInfo: { title: 'Write', subtitle: 'src/config.schema.json', category: 'mutation', kind: 'edit' },
    }),
  },
  {
    kind: 'tool',
    seq: 33,
    data: tool({
      id: 'tc-edit-2',
      name: 'edit',
      args: JSON.stringify({
        file_path: 'src/config.ts',
        old_string: "load('v2', schema)",
        new_string: "load('v2', schema) // validated",
      }),
      displayInfo: { title: 'Edit', subtitle: 'src/config.ts', category: 'mutation', kind: 'edit' },
    }),
  },
  {
    kind: 'tool',
    seq: 34,
    data: tool({
      id: 'tc-multi-1',
      name: 'multi_edit',
      args: JSON.stringify({
        file_path: 'src/server.ts',
        edits: [
          { old_string: 'const port = 3000', new_string: 'const port = config.port' },
          { old_string: 'app.listen(3000)', new_string: 'app.listen(port)\nlog.info({ port })' },
        ],
      }),
      displayInfo: { title: 'Multi Edit', subtitle: 'src/server.ts', category: 'mutation', kind: 'edit' },
    }),
  },
  {
    kind: 'message',
    seq: 35,
    data: {
      id: 'a-turn',
      role: 'assistant',
      content: 'Config refactor applied — schema extracted and the server now reads `config.port`.',
      timestamp: Date.now(),
    },
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
// Rows inside activity groups are left alone — collapsed groups must stay a
// single line, and the running group's live rows stay headers-only.
setTimeout(() => {
  const expandNames = new Set(['execute', 'subagent', 'list_dir', 'go_test', 'panic'])
  document.querySelectorAll<HTMLButtonElement>('.jcode-toolcall > button').forEach((btn) => {
    const card = btn.closest('.jcode-toolcall')
    if (card?.closest('.jcode-activity')) return
    const name = card?.getAttribute('data-tool-name')
    if (name && expandNames.has(name)) {
      if (card?.getAttribute('data-expanded') !== 'true') btn.click()
    }
  })
  document.body.setAttribute('data-fixture-ready', 'true')
}, 300)
