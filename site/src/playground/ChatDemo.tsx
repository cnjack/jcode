/**
 * ChatDemo — the website "footprint" component.
 *
 * Renders a live, scripted jcode-ui Thread + ChatInput inside a framed window.
 * No backend: a MockRuntime plays the canned demo script on a loop. This is the
 * most concrete demonstration that the component library is self-contained and
 * reusable — the marketing site embeds it with zero wiring beyond a runtime.
 */

import { useEffect, useMemo, useRef, useState } from 'react'
import {
  RuntimeProvider,
  ToolRegistryProvider,
  createDefaultToolRegistry,
  createMockRuntime,
  Thread,
  ChatInput,
} from 'jcode-ui'
import 'jcode-ui/styles.css'
import { buildDemoScript, runScript } from './mockScript'

export interface ChatDemoProps {
  /** Loop the script after it finishes. Default true. */
  loop?: boolean
  /** Pause between loops (ms). Default 3000. */
  loopPause?: number
  /** Fixed height of the demo window. Default 480. */
  height?: number
  /** Show the framed chrome (titlebar dots). Default true. */
  chrome?: boolean
}

export function ChatDemo({ loop = true, loopPause = 3000, height = 480, chrome = true }: ChatDemoProps) {
  const registry = useMemo(() => createDefaultToolRegistry(), [])
  const [runtime, setRuntime] = useState(() => createMockRuntime())
  const cancelRef = useRef<(() => void) | null>(null)

  useEffect(() => {
    const rt = createMockRuntime()
    setRuntime(rt)
    const playOnce = () => {
      const cancel = runScript(rt as never, buildDemoScript())
      cancelRef.current = cancel
      // Schedule a reset + replay if looping.
      if (loop) {
        const totalDelay = buildDemoScript().reduce((a, s) => a + s.delay, 0) + loopPause
        const replay = setTimeout(() => {
          rt.setItems([])
          playOnce()
        }, totalDelay)
        cancelRef.current = () => {
          cancel()
          clearTimeout(replay)
        }
      }
    }
    const start = setTimeout(playOnce, 400)
    return () => {
      clearTimeout(start)
      cancelRef.current?.()
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [loop, loopPause])

  return (
    <div
      className="overflow-hidden rounded-xl border border-[var(--color-border,#2E2E2E)] bg-[var(--color-surface,#1A1A1A)] shadow-2xl"
      style={{ height }}
    >
      {chrome && (
        <div className="flex items-center gap-1.5 border-b border-[var(--color-border,#2E2E2E)] bg-[var(--color-muted,#2E2E2E)] px-3 py-2">
          <span className="h-2.5 w-2.5 rounded-full bg-[#ff5f57]" />
          <span className="h-2.5 w-2.5 rounded-full bg-[#febc2e]" />
          <span className="h-2.5 w-2.5 rounded-full bg-[#28c840]" />
          <span className="ml-2 text-[0.7rem] text-[#888]">jcode · chat-ui demo</span>
        </div>
      )}
      {runtime && (
        <RuntimeProvider runtime={runtime}>
          <ToolRegistryProvider registry={registry}>
            <div className="flex h-[calc(100%-2.25rem)] flex-col">
              <div className="flex-1 overflow-hidden">
                <Thread overscanBottom={8} />
              </div>
              <div className="border-t border-[var(--color-border,#2E2E2E)] p-2">
                <ChatInput placeholder="This demo plays automatically…" showContextBar={false} />
              </div>
            </div>
          </ToolRegistryProvider>
        </RuntimeProvider>
      )}
    </div>
  )
}
