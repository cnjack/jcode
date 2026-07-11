/**
 * ChatDemo — marketing-page footprint: the real jcode-ui, driven by a mock
 * runtime. Two modes:
 *
 *  - interactive (default on /chat-ui): starts on the ThreadWelcome empty
 *    state with starter Suggestions; typing (or picking a pill) streams a
 *    canned reply. A "tour" control replays the scripted conversation that
 *    walks through every ThreadItem kind.
 *  - autoplay (docs embeds / legacy): loops the scripted conversation.
 *
 * Controls in the chrome bar flip light/dark and desktop/mobile so visitors
 * see the theme system and responsive column without leaving the page.
 *
 * Layout is pure CSS (see component-demo.css / chatui.css). Do NOT rely on
 * Tailwind utilities from jcode-ui/styles.css here — that file only ships
 * classes used inside packages/jcode-ui, so host-side utilities are missing
 * and layout collapses (e.g. missing .flex-col → chrome bar ends up vertical).
 */

import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import {
  RuntimeProvider,
  ToolRegistryProvider,
  createDefaultToolRegistry,
  createMockRuntime,
  Thread,
  ChatInput,
  ThreadWelcome,
  Suggestions,
  ExportButton,
} from 'jcode-ui'
import 'jcode-ui/styles.css'
import './component-demo.css'
import { buildDemoScript, runScript } from './mockScript'
import type { ScriptRuntime } from './mockScript'

export interface ChatDemoProps {
  /** Autoplay the scripted tour in a loop (docs embeds). */
  loop?: boolean
  loopPause?: number
  height?: number
  chrome?: boolean
  /** Welcome + typing + suggestions instead of autoplay. */
  interactive?: boolean
  /** Show the theme/viewport/tour controls in the chrome bar. */
  controls?: boolean
}

const STARTERS = [
  { label: 'Fix the goroutine leak', prompt: 'Fix the goroutine leak in server.go and verify with tests.' },
  { label: 'Explain this codebase', prompt: 'Give me a map of this codebase — key packages and how they connect.' },
  { label: 'Add dark mode', prompt: 'Add a dark mode toggle to the settings page.' },
]

const FOLLOW_UPS = [
  { label: 'Run the full test suite' },
  { label: 'Show me the diff again' },
  { label: 'Write a commit message' },
]

/** Stream a canned assistant reply for interactive sends. */
function streamReply(rt: ScriptRuntime, userText: string): () => void {
  const timers: ReturnType<typeof setTimeout>[] = []
  rt.push({
    kind: 'message',
    seq: rt.nextSeq(),
    data: { id: `u_${Date.now()}`, role: 'user', content: userText, timestamp: Date.now() },
  })
  rt.setRunning(true)
  const reply =
    "You're driving the **real `jcode-ui` runtime** — this reply is streamed through `createMockRuntime`, the same seam a live agent backend plugs into.\n\nSwap the mock for `createExternalStoreRuntime` (Redux/Zustand) or `createAGUIRuntime` (any AG-UI backend) and the components don't change."
  const words = reply.split(/(?<=\s)/)
  let t = 350
  words.forEach((w, i) => {
    t += 24 + Math.min(w.length * 6, 60)
    timers.push(
      setTimeout(() => {
        rt.appendText(w)
        if (i === words.length - 1) rt.setRunning(false)
      }, t),
    )
  })
  return () => timers.forEach(clearTimeout)
}

export function ChatDemo({
  loop = true,
  loopPause = 3000,
  height = 480,
  chrome = true,
  interactive = false,
  controls = false,
}: ChatDemoProps) {
  const shouldLoop =
    !interactive &&
    loop &&
    !new URLSearchParams(typeof window !== 'undefined' ? window.location.search : '').has('noloop')
  const registry = useMemo(() => createDefaultToolRegistry(), [])
  const [runtime] = useState(() => createMockRuntime())
  const [theme, setTheme] = useState<'dark' | 'light'>('dark')
  const [viewport, setViewport] = useState<'desktop' | 'mobile'>('desktop')
  const [touring, setTouring] = useState(false)
  const cancelRef = useRef<(() => void) | null>(null)

  // Interactive sends stream a canned reply through the runtime.
  useEffect(() => {
    if (!interactive) return
    const rt = runtime as ScriptRuntime
    const origSend = rt.actions.sendMessage
    // MockRuntime actions are a stable bag — patch sendMessage in place.
    ;(rt.actions as { sendMessage: typeof origSend }).sendMessage = (text) => {
      cancelRef.current?.()
      cancelRef.current = streamReply(rt, text)
    }
    return () => {
      ;(rt.actions as { sendMessage: typeof origSend }).sendMessage = origSend
      cancelRef.current?.()
    }
  }, [interactive, runtime])

  // Autoplay tour (non-interactive embeds).
  useEffect(() => {
    if (interactive) return
    const rt = runtime as ScriptRuntime
    let cancelled = false
    const playOnce = () => {
      const cancel = runScript(rt, buildDemoScript())
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
  }, [interactive, shouldLoop, loopPause])

  const replayTour = useCallback(() => {
    const rt = runtime as ScriptRuntime
    cancelRef.current?.()
    rt.setItems([])
    setTouring(true)
    const cancel = runScript(rt, buildDemoScript())
    const total = buildDemoScript().reduce((a, s) => a + s.delay, 0) + 500
    const done = setTimeout(() => setTouring(false), total)
    cancelRef.current = () => {
      cancel()
      clearTimeout(done)
      setTouring(false)
    }
  }, [runtime])

  return (
    <div
      className={`jcode-live-demo ${theme}${viewport === 'mobile' ? ' jcode-live-demo--mobile' : ''}`}
      style={{ height }}
    >
      {chrome && (
        <div className="jcode-live-demo__chrome">
          <span className="jcode-live-demo__dot jcode-live-demo__dot--red" />
          <span className="jcode-live-demo__dot jcode-live-demo__dot--yellow" />
          <span className="jcode-live-demo__dot jcode-live-demo__dot--green" />
          <span className="jcode-live-demo__title">jcode · chat-ui demo</span>
          {controls && (
            <span className="jcode-live-demo__controls">
              <button
                type="button"
                className="jcode-live-demo__ctl"
                onClick={replayTour}
                disabled={touring}
                title="Replay the scripted agent tour"
              >
                {touring ? 'touring…' : '▶ tour'}
              </button>
              <button
                type="button"
                className="jcode-live-demo__ctl"
                onClick={() => setTheme(theme === 'dark' ? 'light' : 'dark')}
                title="Toggle light/dark"
              >
                {theme === 'dark' ? '☀ light' : '☾ dark'}
              </button>
              <button
                type="button"
                className="jcode-live-demo__ctl"
                onClick={() => setViewport(viewport === 'desktop' ? 'mobile' : 'desktop')}
                title="Toggle mobile viewport"
              >
                {viewport === 'desktop' ? '▭ mobile' : '⛶ desktop'}
              </button>
            </span>
          )}
        </div>
      )}
      <RuntimeProvider runtime={runtime}>
        <ToolRegistryProvider registry={registry}>
          <div className="jcode-live-demo__body">
            <div className="jcode-live-demo__viewport">
              <div className="jcode-live-demo__thread">
                <Thread
                  virtualize
                  overscanBottom={8}
                  emptyState={
                    interactive ? (
                      <ThreadWelcome
                        title="Build agent chat UIs in React"
                        subtitle="Streaming, tool calls, approvals — batteries included. Try a starter or type below."
                      >
                        <Suggestions items={STARTERS} />
                      </ThreadWelcome>
                    ) : undefined
                  }
                  suggestions={interactive && !touring ? <Suggestions scroll items={FOLLOW_UPS} /> : undefined}
                />
              </div>
              <div className="jcode-live-demo__composer">
                <div className="jcode-live-demo__composer-bar">
                  <ExportButton filename="jcode-ui-demo.md" title="jcode-ui demo conversation" />
                </div>
                <ChatInput
                  placeholder={interactive ? 'Send a message — this demo is live…' : 'This demo plays automatically…'}
                  showContextBar={false}
                />
              </div>
            </div>
          </div>
        </ToolRegistryProvider>
      </RuntimeProvider>
    </div>
  )
}
