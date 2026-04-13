<script setup lang="ts">
import { ref, onMounted, onUnmounted, watch, nextTick } from 'vue'
import { Terminal } from '@xterm/xterm'
import { FitAddon } from '@xterm/addon-fit'
import { WebLinksAddon } from '@xterm/addon-web-links'
import { api } from '@/composables/api'
import '@xterm/xterm/css/xterm.css'

const termEl = ref<HTMLDivElement | null>(null)
const connected = ref(false)
const sessionId = ref('')

let term: Terminal | null = null
let fitAddon: FitAddon | null = null
let ws: WebSocket | null = null
let resizeObserver: ResizeObserver | null = null

function getWsUrl(ptyId: string): string {
  const proto = location.protocol === 'https:' ? 'wss:' : 'ws:'
  return `${proto}//${location.host}/api/pty/${encodeURIComponent(ptyId)}/ws`
}

async function initTerminal() {
  if (!termEl.value) return

  // Create xterm instance
  term = new Terminal({
    cursorBlink: true,
    fontSize: 13,
    fontFamily: "'JetBrains Mono', 'Fira Code', 'Cascadia Code', Menlo, Monaco, monospace",
    theme: {
      background: '#fafaf9',
      foreground: '#292524',
      cursor: '#0d9488',
      selectionBackground: '#d6d3d180',
      black: '#1c1917',
      red: '#dc2626',
      green: '#16a34a',
      yellow: '#ca8a04',
      blue: '#2563eb',
      magenta: '#9333ea',
      cyan: '#0891b2',
      white: '#d6d3d1',
      brightBlack: '#78716c',
      brightRed: '#ef4444',
      brightGreen: '#22c55e',
      brightYellow: '#eab308',
      brightBlue: '#3b82f6',
      brightMagenta: '#a855f7',
      brightCyan: '#06b6d4',
      brightWhite: '#fafaf9',
    },
  })

  fitAddon = new FitAddon()
  term.loadAddon(fitAddon)
  term.loadAddon(new WebLinksAddon())
  term.open(termEl.value)
  fitAddon.fit()

  // Observe container resizes
  resizeObserver = new ResizeObserver(() => {
    fitAddon?.fit()
    if (ws && ws.readyState === WebSocket.OPEN && term) {
      ws.send(JSON.stringify({
        type: 'resize',
        cols: term.cols,
        rows: term.rows,
      }))
    }
  })
  resizeObserver.observe(termEl.value)

  // Create PTY session on backend
  try {
    const result = await api.ptyCreate()
    sessionId.value = result.id
    connectWS(result.id)
  } catch (err: any) {
    term.writeln(`\r\n\x1b[31mFailed to create terminal: ${err.message}\x1b[0m`)
  }
}

function connectWS(ptyId: string) {
  if (!term) return

  const url = getWsUrl(ptyId)
  ws = new WebSocket(url)
  ws.binaryType = 'arraybuffer'

  ws.onopen = () => {
    connected.value = true
    // Send initial resize
    ws!.send(JSON.stringify({
      type: 'resize',
      cols: term!.cols,
      rows: term!.rows,
    }))
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
    term?.writeln('\r\n\x1b[33m[Session ended]\x1b[0m')
  }

  ws.onerror = () => {
    connected.value = false
  }

  // Terminal input → WebSocket
  term.onData((data) => {
    if (ws && ws.readyState === WebSocket.OPEN) {
      ws.send(new TextEncoder().encode(data))
    }
  })
}

async function reconnect() {
  cleanup()
  await nextTick()
  initTerminal()
}

function cleanup() {
  resizeObserver?.disconnect()
  resizeObserver = null
  if (ws) {
    ws.close()
    ws = null
  }
  if (sessionId.value) {
    api.ptyKill(sessionId.value).catch(() => {})
    sessionId.value = ''
  }
  if (term) {
    term.dispose()
    term = null
  }
  fitAddon = null
  connected.value = false
}

onMounted(initTerminal)
onUnmounted(cleanup)
</script>

<template>
  <div class="flex flex-col h-full bg-stone-50">
    <!-- Header -->
    <div class="flex items-center justify-between px-3 py-1 border-b border-stone-200 bg-stone-100/80 shrink-0">
      <div class="flex items-center gap-2">
        <span class="text-[11px] font-medium text-stone-500 uppercase tracking-wider">Terminal</span>
        <span
          class="w-1.5 h-1.5 rounded-full"
          :class="connected ? 'bg-emerald-400' : 'bg-stone-300'"
        />
      </div>
      <button
        class="text-[10px] text-stone-400 hover:text-stone-600 cursor-pointer transition-colors"
        @click="reconnect"
        title="New terminal"
      >
        + New
      </button>
    </div>

    <!-- xterm container -->
    <div ref="termEl" class="flex-1 min-h-0 px-1 py-1" />
  </div>
</template>

<style scoped>
:deep(.xterm) {
  height: 100%;
  padding: 2px;
}
:deep(.xterm-viewport) {
  background-color: #fafaf9 !important;
}
</style>
