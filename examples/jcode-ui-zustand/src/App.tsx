/**
 * jcode-ui + Zustand — external store pattern.
 *
 *   pnpm install && pnpm dev
 */

import { useMemo, useEffect } from 'react'
import {
  RuntimeProvider,
  ToolRegistryProvider,
  createDefaultToolRegistry,
  createExternalStoreRuntime,
  Thread,
  ChatInput,
} from 'jcode-ui'
import { seedDemo, useChatStore } from './store'

export function App() {
  const registry = useMemo(() => createDefaultToolRegistry(), [])

  const runtime = useMemo(
    () =>
      createExternalStoreRuntime({
        // Zustand stores are Redux-shaped: getState + subscribe
        store: useChatStore,
        select: (s) => ({
          items: s.items,
          isRunning: s.isRunning,
        }),
        actions: {
          sendMessage: (text) => {
            const { appendUser, appendAssistant, appendTool, setRunning } = useChatStore.getState()
            appendUser(text)
            setRunning(true)
            // Simulate an agent turn: tool then reply
            window.setTimeout(() => {
              appendTool({
                id: `t_${Date.now()}`,
                name: 'execute',
                args: JSON.stringify({ command: `echo ${JSON.stringify(text)}` }),
                status: 'done',
                timestamp: Date.now(),
                output: text + '\n',
                displayInfo: { title: 'execute', subtitle: 'echo' },
              })
              appendAssistant(`Zustand received: **${text}**`)
            }, 500)
          },
          enqueueMessage: (text) => {
            // Type-ahead while running — still append as user for this demo
            useChatStore.getState().appendUser(`[queued] ${text}`)
          },
          removeQueuedMessage: () => undefined,
          stop: () => useChatStore.getState().stop(),
          resolveApproval: (id, approved, approveAll) =>
            useChatStore.getState().resolveApproval(id, approved, approveAll),
          submitAskUser: (id, answers) => useChatStore.getState().submitAskUser(id, answers),
          editMessage: (id, text) => useChatStore.getState().editMessage(id, text),
        },
      }),
    [],
  )

  useEffect(() => {
    seedDemo()
  }, [])

  return (
    <div className="app">
      <header className="app-header">
        <strong>jcode-ui</strong>
        <span>external store · Zustand</span>
      </header>
      <RuntimeProvider runtime={runtime}>
        <ToolRegistryProvider registry={registry}>
          <main className="app-main">
            <Thread overscanBottom={8} />
          </main>
          <footer className="app-footer">
            <ChatInput
              slashCommands={[{ slash: '/clear', description: 'Not wired in this demo' }]}
              placeholder="Send via Zustand store…"
              showContextBar={false}
            />
          </footer>
        </ToolRegistryProvider>
      </RuntimeProvider>
    </div>
  )
}
