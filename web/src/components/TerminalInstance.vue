<script setup lang="ts">
import { ref, watch, onMounted, onUnmounted, nextTick } from 'vue'
import { Terminal } from '@xterm/xterm'
import { FitAddon } from '@xterm/addon-fit'
import { WebLinksAddon } from '@xterm/addon-web-links'
import { useI18n } from 'vue-i18n'
import { api } from '@/composables/api'
import { wsBase } from '@/composables/apiBase'
import { getAuthToken } from '@/composables/authToken'
import '@xterm/xterm/css/xterm.css'

const props = defineProps<{
  active: boolean
}>()

const emit = defineEmits<{
  connected: []
  disconnected: []
}>()

const { t } = useI18n()
const termEl = ref<HTMLDivElement | null>(null)
const connected = ref(false)

let term: Terminal | null = null
let fitAddon: FitAddon | null = null
let ws: WebSocket | null = null
let sessionId = ''
let resizeObserver: ResizeObserver | null = null
let themeObserver: MutationObserver | null = null

const termBg = ref(readToken('--term-bg', isDarkMode() ? '#18181b' : '#fafafa'))

function isDarkMode(): boolean {
  return document.documentElement.classList.contains('dark')
}

// Read a CSS custom property from :root and strip whitespace. Falls back to the
// provided default when the token is unset (e.g. before tokens.css loads).
function readToken(name: string, fallback: string): string {
  if (typeof window === 'undefined') return fallback
  const v = getComputedStyle(document.documentElement).getPropertyValue(name).trim()
  return v || fallback
}

// Build an xterm theme object from the --term-* tokens. xterm needs resolved
// color strings (it can't read var(...)), so we resolve them at call time — both
// on init and whenever the theme observer fires (light/dark switch). This is the
// single source for terminal colors; the palette lives in tokens.css.
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

async function init() {
  if (!termEl.value) return

  term = new Terminal({
    cursorBlink: true,
    fontSize: 13,
    fontFamily: "'JetBrains Mono', 'Fira Code', 'Cascadia Code', Menlo, Monaco, monospace",
    theme: termTheme(),
  })

  fitAddon = new FitAddon()
  term.loadAddon(fitAddon)
  term.loadAddon(new WebLinksAddon())
  term.open(termEl.value)
  fitAddon.fit()

  resizeObserver = new ResizeObserver(() => {
    if (props.active) {
      fitAddon?.fit()
      sendResize()
    }
  })
  resizeObserver.observe(termEl.value)

  themeObserver = new MutationObserver(() => {
    if (term) term.options.theme = termTheme()
    termBg.value = readToken('--term-bg', isDarkMode() ? '#18181b' : '#fafafa')
  })
  themeObserver.observe(document.documentElement, { attributes: true, attributeFilter: ['class'] })

  try {
    const result = await api.ptyCreate()
    sessionId = result.id
    connectWS(result.id)
  } catch (err: unknown) {
    const msg = err instanceof Error ? err.message : String(err)
    term.writeln(`\r\n\x1b[31m${t('terminal.failedCreate', { msg })}\x1b[0m`)
  }
}

function connectWS(ptyId: string) {
  if (!term) return
  const url = getWsUrl(ptyId)
  // Token (when the server requires auth) rides as the second WS subprotocol —
  // browsers can't set headers on a WS handshake. See auth.go.
  const token = getAuthToken()
  ws = token ? new WebSocket(url, ['jcode-auth', token]) : new WebSocket(url)
  ws.binaryType = 'arraybuffer'

  ws.onopen = () => {
    connected.value = true
    emit('connected')
    sendResize()
  }
  ws.onclose = () => {
    connected.value = false
    emit('disconnected')
    term?.writeln(`\r\n\x1b[33m${t('terminal.sessionEnded')}\x1b[0m`)
  }
  ws.onerror = () => {
    connected.value = false
  }

  ws.onmessage = (event) => {
    // Guard: the term may have been disposed by cleanup() (tab close / panel
    // hidden) before an in-flight WS frame arrives. Writing to a disposed
    // terminal throws, so check before writing.
    if (!term) return
    if (event.data instanceof ArrayBuffer) {
      term.write(new Uint8Array(event.data))
    } else {
      term.write(event.data)
    }
  }

  term.onData((data) => {
    if (ws && ws.readyState === WebSocket.OPEN) {
      ws.send(new TextEncoder().encode(data))
    }
  })
}

function cleanup() {
  resizeObserver?.disconnect()
  resizeObserver = null
  themeObserver?.disconnect()
  themeObserver = null
  if (ws) {
    // Detach handlers before closing so a frame racing in after close can't
    // reach a disposed terminal.
    ws.onmessage = null
    ws.onclose = null
    ws.onerror = null
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
  connected.value = false
}

// When tab becomes active, fit the terminal
watch(() => props.active, async (active) => {
  if (active && fitAddon && termEl.value) {
    await nextTick()
    requestAnimationFrame(() => {
      fitAddon?.fit()
      sendResize()
    })
  }
})

onMounted(init)
onUnmounted(cleanup)
</script>

<template>
  <div
    class="term-instance"
    :class="{ 'term-inactive': !active }"
    :style="{ background: termBg }"
    ref="termEl"
  />
</template>

<style scoped>
.term-instance {
  width: 100%;
  height: 100%;
  padding: 4px 4px 0;
}

/* Hidden but still laid out — xterm needs to measure the container */
.term-inactive {
  visibility: hidden;
  position: absolute;
  pointer-events: none;
  top: 0; left: 0;
  width: 100%;
  height: 100%;
}

:deep(.xterm) {
  height: 100%;
  padding: 4px 6px;
}
:deep(.xterm-viewport) {
  border-radius: 0;
}
:deep(.xterm-viewport::-webkit-scrollbar) {
  width: 5px;
}
:deep(.xterm-viewport::-webkit-scrollbar-track) {
  background: transparent;
}
:deep(.xterm-viewport::-webkit-scrollbar-thumb) {
  background: var(--color-border);
  border-radius: var(--radius-xs);
}</style>
