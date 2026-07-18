/**
 * TerminalPanel — bottom-docked terminal using xterm.js.
 *
 * Ported from web/src/components/TerminalPanel.vue (the tab container) +
 * web/src/components/TerminalInstance.vue (the actual xterm + PTY + WS
 * lifecycle). The React signature is a single-terminal panel { onClose }, so
 * the lifecycle from TerminalInstance is folded in here.
 *
 * On mount it creates a PTY via api.ptyCreate(), opens a WS to
 * `${wsBase()}/api/pty/${id}/ws`, attaches xterm + FitAddon + WebLinksAddon,
 * reads theme colors from CSS tokens via getComputedStyle, and sends resize
 * messages on container/theme changes. On unmount it kills the PTY, closes the
 * WS, and disposes the terminal.
 */

import { useEffect, useRef } from 'react'
import { useTranslation } from 'react-i18next'
import { XMarkIcon } from '@heroicons/react/24/outline'
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
  const termElRef = useRef<HTMLDivElement | null>(null)
  const onCloseRef = useRef(onClose)
  onCloseRef.current = onClose

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
      ws.binaryType = 'arraybuffer'

      ws.onopen = () => {
        sendResize()
      }
      ws.onclose = () => {
        term?.writeln(`\r\n\x1b[33m[Session ended]\x1b[0m`)
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

      fitAddon = new FitAddon()
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
        sessionId = result.id
        connectWS(result.id)
      } catch (err: unknown) {
        const msg = err instanceof Error ? err.message : String(err)
        term.writeln(`\r\n\x1b[31mFailed to create terminal: ${msg}\x1b[0m`)
      }
    }

    void init()

    // Cleanup on unmount: detach WS handlers before closing (so a racing frame
    // can't reach a disposed terminal), kill the PTY, dispose the terminal.
    return () => {
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
      }
      if (sessionId) {
        api.ptyKill(sessionId).catch(() => {})
        sessionId = ''
      }
      if (term) {
        term.dispose()
        term = null
      }
      fitAddon = null
    }
  }, [])

  return (
    <div className="flex h-full flex-col" style={{ background: 'var(--color-background)' }}>
      {/* Tab bar — a single terminal plus the close-panel control on the right. */}
      <div
        className="flex h-8 shrink-0 items-center gap-1 px-2"
        style={{ borderBottom: '1px solid var(--color-border)', background: 'var(--color-background)' }}
      >
        <div className="flex h-full min-w-0 flex-1 items-center gap-0.5 overflow-x-auto">
          <div
            role="button"
            tabIndex={0}
            aria-pressed="true"
            aria-label={t('terminal.shell', { n: 1 })}
            className="inline-flex h-[22px] shrink-0 items-center gap-1 rounded-[var(--radius-sm)] px-2 font-mono text-[11px] font-medium text-[var(--color-foreground)] transition-[background,color] duration-100"
            style={{
              fontFamily: 'var(--font-mono)',
              background: 'color-mix(in srgb, var(--color-foreground) 10%, transparent)',
            }}
          >
            <span className="pointer-events-none">{t('terminal.shell', { n: 1 })}</span>
          </div>
        </div>
        <div className="flex shrink-0 items-center gap-0.5">
          <button
            type="button"
            onClick={onCloseRef.current}
            title={t('terminal.closePanel')}
            className="flex h-5 w-5 items-center justify-center rounded-[var(--radius-sm)] text-[var(--color-muted-foreground)] transition-[background,color] duration-100 hover:bg-[color-mix(in_srgb,var(--color-foreground)_8%,transparent)] hover:text-[var(--color-foreground)]"
          >
            <XMarkIcon className="h-3 w-3" />
          </button>
        </div>
      </div>

      {/* Terminal area. Padding + term-bg live on .xterm via styles.css
          (mirrors Vue TerminalInstance :deep(.xterm) rules). */}
      <div
        ref={termElRef}
        className="relative min-h-0 flex-1"
        style={{ background: 'var(--term-bg, var(--color-background))' }}
      />
    </div>
  )
}
