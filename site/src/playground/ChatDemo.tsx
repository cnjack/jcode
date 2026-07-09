/**
 * ChatDemo — marketing-page footprint: scripted Thread + ChatInput.
 *
 * Layout is pure CSS (see component-demo.css / chatui.css). Do NOT rely on
 * Tailwind utilities from jcode-ui/styles.css here — that file only ships
 * classes used inside packages/jcode-ui, so host-side utilities are missing
 * and layout collapses (e.g. missing .flex-col → chrome bar ends up vertical).
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
import './component-demo.css'
import { buildDemoScript, runScript } from './mockScript'

export interface ChatDemoProps {
  loop?: boolean
  loopPause?: number
  height?: number
  chrome?: boolean
}

export function ChatDemo({ loop = true, loopPause = 3000, height = 480, chrome = true }: ChatDemoProps) {
  const shouldLoop =
    loop && !new URLSearchParams(typeof window !== 'undefined' ? window.location.search : '').has('noloop')
  const registry = useMemo(() => createDefaultToolRegistry(), [])
  const [runtime, setRuntime] = useState(() => createMockRuntime())
  const cancelRef = useRef<(() => void) | null>(null)

  useEffect(() => {
    const rt = createMockRuntime()
    setRuntime(rt)
    let cancelled = false
    const playOnce = () => {
      const cancel = runScript(rt as never, buildDemoScript())
      if (shouldLoop) {
        const totalDelay = buildDemoScript().reduce((a, s) => a + s.delay, 0) + loopPause
        const replay = setTimeout(() => {
          if (cancelled) return
          rt.setItems([])
          playOnce()
        }, totalDelay)
        cancelRef.current = () => {
          cancel()
          clearTimeout(replay)
        }
      } else {
        cancelRef.current = cancel
      }
    }
    const start = setTimeout(playOnce, 400)
    return () => {
      cancelled = true
      clearTimeout(start)
      cancelRef.current?.()
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [shouldLoop, loopPause])

  return (
    <div className="jcode-live-demo dark" style={{ height }}>
      {chrome && (
        <div className="jcode-live-demo__chrome">
          <span className="jcode-live-demo__dot jcode-live-demo__dot--red" />
          <span className="jcode-live-demo__dot jcode-live-demo__dot--yellow" />
          <span className="jcode-live-demo__dot jcode-live-demo__dot--green" />
          <span className="jcode-live-demo__title">jcode · chat-ui demo</span>
        </div>
      )}
      {runtime && (
        <RuntimeProvider runtime={runtime}>
          <ToolRegistryProvider registry={registry}>
            <div className="jcode-live-demo__body">
              <div className="jcode-live-demo__thread">
                <Thread virtualize overscanBottom={8} />
              </div>
              <div className="jcode-live-demo__composer">
                <ChatInput placeholder="This demo plays automatically…" showContextBar={false} />
              </div>
            </div>
          </ToolRegistryProvider>
        </RuntimeProvider>
      )}
    </div>
  )
}
