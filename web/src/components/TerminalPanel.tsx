/**
 * TerminalPanel — bottom-docked, multi-tab terminal using xterm.js.
 *
 * Each tab owns an independent TerminalInstance, PTY, WebSocket, xterm and
 * cleanup lifecycle. Inactive tabs stay mounted so shell state is preserved.
 *
 * On mount it creates a PTY via api.ptyCreate(), opens a WS to
 * `${wsBase()}/api/pty/${id}/ws`, attaches xterm + FitAddon + WebLinksAddon,
 * reads theme colors from CSS tokens via getComputedStyle, and sends resize
 * messages on container/theme changes. On unmount it kills the PTY, closes the
 * WS, and disposes the terminal.
 */

import { useCallback, useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { PlusIcon, XMarkIcon } from '@heroicons/react/24/outline'
import { Terminal } from '@xterm/xterm'
import { FitAddon } from '@xterm/addon-fit'
import { WebLinksAddon } from '@xterm/addon-web-links'
import { api } from '../lib/api'
import { wsBase } from '../lib/apiBase'
import { getAuthToken } from '../lib/authToken'
import '@xterm/xterm/css/xterm.css'

interface Props {
  onClose: () => void
}

export function TerminalPanel({ onClose }: Props) {
  const { t } = useTranslation()
  const nextIDRef = useRef(2)
  const [tabs, setTabs] = useState([{ id: 1, number: 1 }])
  const [activeID, setActiveID] = useState(1)

  const addTab = useCallback(() => {
    const id = nextIDRef.current++
    setTabs((current) => [...current, { id, number: id }])
    setActiveID(id)
  }, [])

  const closeTab = useCallback((id: number) => {
    if (tabs.length === 1) {
      onClose()
      return
    }
    const index = tabs.findIndex((tab) => tab.id === id)
    const remaining = tabs.filter((tab) => tab.id !== id)
    if (activeID === id) setActiveID(remaining[Math.min(Math.max(index, 0), remaining.length - 1)].id)
    setTabs(remaining)
  }, [activeID, onClose, tabs])

  return (
    <div className="flex h-full flex-col bg-[var(--color-background)]">
      <div className="flex h-8 shrink-0 items-center border-b border-[var(--color-border)] bg-[var(--color-background)] px-2">
        <div role="tablist" aria-label={t('terminal.tabs')} className="flex h-full min-w-0 flex-1 items-end gap-1 overflow-x-auto">
          {tabs.map((tab) => {
            const label = t('terminal.shell', { n: tab.number })
            const active = activeID === tab.id
            return (
              <div key={tab.id} className={`mb-1 flex h-6 shrink-0 items-center rounded-[var(--radius-md)] transition-colors ${active ? 'bg-[var(--color-muted)] text-[var(--color-foreground)]' : 'text-[var(--color-muted-foreground)] hover:bg-[var(--color-muted)]'}`}>
                <button type="button" role="tab" aria-selected={active} onClick={() => setActiveID(tab.id)} className="h-full px-2 font-mono text-[10.5px] font-medium">
                  {label}
                </button>
                <button type="button" aria-label={t('terminal.closeTab', { label })} title={t('terminal.closeTab', { label })} onClick={() => closeTab(tab.id)} className="mr-1 grid h-4 w-4 place-items-center rounded-[var(--radius-sm)] hover:bg-[var(--color-background)] hover:text-[var(--color-foreground)]">
                  <XMarkIcon className="h-2.5 w-2.5" />
                </button>
              </div>
            )
          })}
        </div>
        <button type="button" onClick={addTab} aria-label={t('terminal.newTerminal')} title={t('terminal.newTerminal')} className="ml-1 grid h-6 w-6 shrink-0 place-items-center rounded-[var(--radius-md)] text-[var(--color-muted-foreground)] transition-colors hover:bg-[var(--color-muted)] hover:text-[var(--color-foreground)]">
          <PlusIcon className="h-3.5 w-3.5" />
        </button>
      </div>
      <div className="relative min-h-0 flex-1 bg-[var(--term-bg,var(--color-background))]">
        {tabs.map((tab) => <TerminalInstance key={tab.id} active={activeID === tab.id} />)}
      </div>
    </div>
  )
}

function TerminalInstance({ active }: { active: boolean }) {
  const { t } = useTranslation()
  const termElRef = useRef<HTMLDivElement | null>(null)
  const terminalRef = useRef<Terminal | null>(null)
  const fitAddonRef = useRef<FitAddon | null>(null)
  const socketRef = useRef<WebSocket | null>(null)

  useEffect(() => {
    const termEl = termElRef.current
    if (!termEl) return
    // Capture the narrowed, non-null element so the nested function declarations
    // below (which TS re-widens termEl back to HTMLDivElement | null inside)
    // keep a definitive HTMLElement reference for xterm.open + the observers.
    const el = termEl

    let term: Terminal | null = null
    let fitAddon: FitAddon | null = null
    let ws: WebSocket | null = null
    let sessionId = ''
    let resizeObserver: ResizeObserver | null = null
    let themeObserver: MutationObserver | null = null
    let disposed = false

    // Read a CSS custom property from :root and strip whitespace. Falls back to
    // the provided default when the token is unset (e.g. before tokens load).
    function readToken(name: string, fallback: string): string {
      const v = getComputedStyle(document.documentElement).getPropertyValue(name).trim()
      return v || fallback
    }

    // Build an xterm theme object from the --term-* tokens. xterm needs resolved
    // color strings (it can't read var(...)), so we resolve them at call time —
    // both on init and whenever the theme observer fires (light/dark switch).
    function termTheme() {
      return {
        background: readToken('--term-bg', '#18181b'),
        foreground: readToken('--term-fg', '#e4e4e7'),
        cursor: readToken('--term-cursor', '#FF8400'),
        selectionBackground: readToken('--term-selection', '#3f3f4680'),
        black: readToken('--term-black', '#09090b'),
        red: readToken('--term-red', '#ef4444'),
        green: readToken('--term-green', '#22c55e'),
        yellow: readToken('--term-yellow', '#eab308'),
        blue: readToken('--term-blue', '#3b82f6'),
        magenta: readToken('--term-magenta', '#a855f7'),
        cyan: readToken('--term-cyan', '#06b6d4'),
        white: readToken('--term-white', '#d4d4d8'),
        brightBlack: readToken('--term-bright-black', '#71717a'),
        brightRed: readToken('--term-bright-red', '#f87171'),
        brightGreen: readToken('--term-bright-green', '#4ade80'),
        brightYellow: readToken('--term-bright-yellow', '#facc15'),
        brightBlue: readToken('--term-bright-blue', '#60a5fa'),
        brightMagenta: readToken('--term-bright-magenta', '#c084fc'),
        brightCyan: readToken('--term-bright-cyan', '#22d3ee'),
        brightWhite: readToken('--term-bright-white', '#fafafa'),
      }
    }

    function getWsUrl(ptyId: string): string {
      // wsBase() yields the page origin in browser mode (same-origin) or the
      // resolved sidecar host in desktop mode (cross-origin). See apiBase.ts.
      return `${wsBase()}/api/pty/${encodeURIComponent(ptyId)}/ws`
    }

    function sendResize() {
      if (ws && ws.readyState === WebSocket.OPEN && term) {
        ws.send(JSON.stringify({ type: 'resize', cols: term.cols, rows: term.rows }))
      }
    }

    function connectWS(ptyId: string) {
      if (!term) return
      const url = getWsUrl(ptyId)
      // Token (when the server requires auth) rides as the second WS
      // subprotocol — browsers can't set headers on a WS handshake. See auth.go.
      const token = getAuthToken()
      ws = token ? new WebSocket(url, ['jcode-auth', token]) : new WebSocket(url)
      socketRef.current = ws
      ws.binaryType = 'arraybuffer'

      ws.onopen = () => {
        sendResize()
      }
      ws.onclose = () => {
        term?.writeln(`\r\n\x1b[33m${t('terminal.sessionEnded')}\x1b[0m`)
      }
      ws.onerror = () => {
        /* state handled by onclose */
      }
      ws.onmessage = (event) => {
        // Guard: the term may have been disposed by cleanup (panel hidden)
        // before an in-flight WS frame arrives. Writing to a disposed terminal
        // throws, so check before writing.
        if (!term) return
        if (event.data instanceof ArrayBuffer) {
          term.write(new Uint8Array(event.data))
        } else {
          term.write(event.data as string)
        }
      }

      term.onData((data) => {
        if (ws && ws.readyState === WebSocket.OPEN) {
          ws.send(new TextEncoder().encode(data))
        }
      })
    }

    async function init() {
      term = new Terminal({
        cursorBlink: true,
        fontSize: 13,
        fontFamily: "'JetBrains Mono', 'Fira Code', 'Cascadia Code', Menlo, Monaco, monospace",
        theme: termTheme(),
      })
      terminalRef.current = term

      fitAddon = new FitAddon()
      fitAddonRef.current = fitAddon
      term.loadAddon(fitAddon)
      term.loadAddon(new WebLinksAddon())
      term.open(el)
      fitAddon.fit()

      resizeObserver = new ResizeObserver(() => {
        fitAddon?.fit()
        sendResize()
      })
      resizeObserver.observe(el)

      themeObserver = new MutationObserver(() => {
        if (term) term.options.theme = termTheme()
      })
      themeObserver.observe(document.documentElement, { attributes: true, attributeFilter: ['class'] })

      try {
        const result = await api.ptyCreate()
        if (disposed) {
          void api.ptyKill(result.id).catch(() => {})
          return
        }
        sessionId = result.id
        connectWS(result.id)
      } catch (err: unknown) {
        const msg = err instanceof Error ? err.message : String(err)
        term?.writeln(`\r\n\x1b[31m${t('terminal.failedCreate', { msg })}\x1b[0m`)
      }
    }

    void init()

    // Cleanup on unmount: detach WS handlers before closing (so a racing frame
    // can't reach a disposed terminal), kill the PTY, dispose the terminal.
    return () => {
      disposed = true
      resizeObserver?.disconnect()
      resizeObserver = null
      themeObserver?.disconnect()
      themeObserver = null
      if (ws) {
        ws.onmessage = null
        ws.onclose = null
        ws.onerror = null
        ws.onopen = null
        ws.close()
        ws = null
        socketRef.current = null
      }
      if (sessionId) {
        api.ptyKill(sessionId).catch(() => {})
        sessionId = ''
      }
      if (term) {
        term.dispose()
        term = null
        terminalRef.current = null
      }
      fitAddon = null
      fitAddonRef.current = null
    }
  }, [])

  useEffect(() => {
    if (!active) return
    const frame = window.requestAnimationFrame(() => {
      fitAddonRef.current?.fit()
      const terminal = terminalRef.current
      const socket = socketRef.current
      if (terminal && socket?.readyState === WebSocket.OPEN) {
        socket.send(JSON.stringify({ type: 'resize', cols: terminal.cols, rows: terminal.rows }))
      }
      terminal?.focus()
    })
    return () => window.cancelAnimationFrame(frame)
  }, [active])

  return (
    <div ref={termElRef} aria-hidden={!active} className={`absolute inset-0 ${active ? '' : 'hidden'}`} />
  )
}
