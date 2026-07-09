/**
 * Minimal jcode-ui example — mock runtime only, no backend.
 *
 *   pnpm install
 *   pnpm dev
 */

import { useMemo } from 'react'
import {
  RuntimeProvider,
  ToolRegistryProvider,
  createDefaultToolRegistry,
  createMockRuntime,
  Thread,
  ChatInput,
} from 'jcode-ui'
import type { ThreadItem } from 'jcode-ui-core'

function seed(runtime: ReturnType<typeof createMockRuntime>) {
  let seq = 0
  const push = (item: Omit<ThreadItem, 'seq'> & { seq?: number }) => {
    runtime.push({ ...item, seq: ++seq } as ThreadItem)
  }

  push({
    kind: 'message',
    data: {
      id: 'u1',
      role: 'user',
      content: 'Show me a minimal jcode-ui thread.',
      timestamp: Date.now(),
    },
  })
  push({
    kind: 'message',
    data: {
      id: 'a1',
      role: 'assistant',
      content:
        'This example uses `createMockRuntime` — no backend. Type below; actions are recorded in `runtime.calls`.',
      timestamp: Date.now(),
      durationMs: 800,
    },
  })
  push({
    kind: 'tool',
    data: {
      id: 't1',
      name: 'execute',
      args: JSON.stringify({ command: 'echo hello' }),
      status: 'done',
      timestamp: Date.now(),
      output: 'hello\n',
      displayInfo: { title: 'execute', subtitle: 'echo hello' },
    },
  })
}

export function App() {
  const registry = useMemo(() => createDefaultToolRegistry(), [])
  const runtime = useMemo(() => {
    const rt = createMockRuntime({
      actions: {
        sendMessage: (text) => {
          const items = rt.getState().items
          const seq = items.reduce((m, i) => Math.max(m, i.seq), 0) + 1
          rt.push({
            kind: 'message',
            seq,
            data: {
              id: `u_${Date.now()}`,
              role: 'user',
              content: text,
              timestamp: Date.now(),
            },
          })
          // Fake assistant reply
          window.setTimeout(() => {
            const s2 = rt.getState().items.reduce((m, i) => Math.max(m, i.seq), 0) + 1
            rt.push({
              kind: 'message',
              seq: s2,
              data: {
                id: `a_${Date.now()}`,
                role: 'assistant',
                content: `Echo: ${text}`,
                timestamp: Date.now(),
              },
            })
          }, 400)
        },
      },
    })
    seed(rt)
    return rt
  }, [])

  return (
    <div className="app">
      <header className="app-header">
        <strong>jcode-ui</strong>
        <span>minimal · mock runtime</span>
      </header>
      <RuntimeProvider runtime={runtime}>
        <ToolRegistryProvider registry={registry}>
          <main className="app-main">
            <Thread virtualize={false} overscanBottom={8} />
          </main>
          <footer className="app-footer">
            <ChatInput placeholder="Say something…" showContextBar={false} />
          </footer>
        </ToolRegistryProvider>
      </RuntimeProvider>
    </div>
  )
}
