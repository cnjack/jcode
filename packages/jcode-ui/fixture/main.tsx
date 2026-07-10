/**
 * Fixture timeline for visual acceptance of exploring groups, terminal
 * dual-channel output, and compact subagent cards.
 */
import { createRoot } from 'react-dom/client'
import {
  RuntimeProvider,
  createMockRuntime,
  Thread,
  ToolRegistryProvider,
  createDefaultToolRegistry,
} from '../src/index.ts'
import type { ThreadItem, ToolCall } from 'jcode-ui-core'
import '../src/styles/entry.css'

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
      id: 'm4',
      role: 'assistant',
      content: '### Subagent compact card',
      timestamp: Date.now(),
    },
  },
  {
    kind: 'tool',
    seq: 11,
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
          height: '80vh',
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
    </div>
  )
}

// Expand terminal + subagent cards after mount for screenshots.
createRoot(document.getElementById('root')!).render(<App />)

setTimeout(() => {
  document.querySelectorAll<HTMLButtonElement>('.jcode-toolcall > button').forEach((btn) => {
    const card = btn.closest('.jcode-toolcall')
    const name = card?.getAttribute('data-tool-name')
    if (name === 'execute' || name === 'subagent') {
      if (card?.getAttribute('data-expanded') !== 'true') btn.click()
    }
  })
  // Mark ready for Playwright.
  document.body.setAttribute('data-fixture-ready', 'true')
}, 300)
