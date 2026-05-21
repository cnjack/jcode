<script setup lang="ts">
import { ref, watch, onMounted, onUnmounted, nextTick } from 'vue'
import { Terminal } from '@xterm/xterm'
import { FitAddon } from '@xterm/addon-fit'
import { WebLinksAddon } from '@xterm/addon-web-links'
import { api } from '@/composables/api'
import '@xterm/xterm/css/xterm.css'

const props = defineProps<{
  active: boolean
}>()

const emit = defineEmits<{
  connected: []
  disconnected: []
}>()

const termEl = ref<HTMLDivElement | null>(null)
const connected = ref(false)

let term: Terminal | null = null
let fitAddon: FitAddon | null = null
let ws: WebSocket | null = null
let sessionId = ''
let resizeObserver: ResizeObserver | null = null
let themeObserver: MutationObserver | null = null

function isDarkMode(): boolean {
  return document.documentElement.classList.contains('dark')
}

const darkTheme = {
  background: '#18181b', foreground: '#e4e4e7', cursor: '#10b981',
  selectionBackground: '#3f3f4680', black: '#09090b', red: '#ef4444',
  green: '#22c55e', yellow: '#eab308', blue: '#3b82f6', magenta: '#a855f7',
  cyan: '#06b6d4', white: '#d4d4d8', brightBlack: '#71717a', brightRed: '#f87171',
  brightGreen: '#4ade80', brightYellow: '#facc15', brightBlue: '#60a5fa',
  brightMagenta: '#c084fc', brightCyan: '#22d3ee', brightWhite: '#fafafa',
}
const lightTheme = {
  background: '#fafafa', foreground: '#27272a', cursor: '#10b981',
  selectionBackground: '#d4d4d880', black: '#18181b', red: '#dc2626',
  green: '#16a34a', yellow: '#ca8a04', blue: '#2563eb', magenta: '#9333ea',
  cyan: '#0891b2', white: '#d4d4d8', brightBlack: '#71717a', brightRed: '#ef4444',
  brightGreen: '#22c55e', brightYellow: '#eab308', brightBlue: '#3b82f6',
  brightMagenta: '#a855f7', brightCyan: '#06b6d4', brightWhite: '#fafafa',
}

function getWsUrl(ptyId: string): string {
  const proto = location.protocol === 'https:' ? 'wss:' : 'ws:'
  return `${proto}//${location.host}/api/pty/${encodeURIComponent(ptyId)}/ws`
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
    theme: isDarkMode() ? darkTheme : lightTheme,
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
    if (term) term.options.theme = isDarkMode() ? darkTheme : lightTheme
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

function connectWS(ptyId: string) {
  if (!term) return
  const url = getWsUrl(ptyId)
  ws = new WebSocket(url)
  ws.binaryType = 'arraybuffer'

  ws.onopen = () => {
    connected.value = true
    emit('connected')
    sendResize()
  }
  ws.onmessage = (event) => {
    if (event.data instanceof ArrayBuffer) {
      term!.write(new Uint8Array(event.data))
    } else {
      term!.write(event.data)
    }
  }
  ws.onclose = () => {
    connected.value = false
    emit('disconnected')
    term?.writeln('\r\n\x1b[33m[Session ended]\x1b[0m')
  }
  ws.onerror = () => {
    connected.value = false
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
    ref="termEl"
  />
</template>

<style scoped>
.term-instance {
  width: 100%;
  height: 100%;
  padding: 2px;
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
  padding: 2px;
}
</style>
