<script setup lang="ts">
import { ref, onMounted, onUnmounted, nextTick } from 'vue'
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

function isDarkMode(): boolean {
  return document.documentElement.classList.contains('dark')
}

const darkTheme = {
  background: '#18181b',
  foreground: '#e4e4e7',
  cursor: '#10b981',
  selectionBackground: '#3f3f4680',
  black: '#09090b',
  red: '#ef4444',
  green: '#22c55e',
  yellow: '#eab308',
  blue: '#3b82f6',
  magenta: '#a855f7',
  cyan: '#06b6d4',
  white: '#d4d4d8',
  brightBlack: '#71717a',
  brightRed: '#f87171',
  brightGreen: '#4ade80',
  brightYellow: '#facc15',
  brightBlue: '#60a5fa',
  brightMagenta: '#c084fc',
  brightCyan: '#22d3ee',
  brightWhite: '#fafafa',
}

const lightTheme = {
  background: '#fafafa',
  foreground: '#27272a',
  cursor: '#10b981',
  selectionBackground: '#d4d4d880',
  black: '#18181b',
  red: '#dc2626',
  green: '#16a34a',
  yellow: '#ca8a04',
  blue: '#2563eb',
  magenta: '#9333ea',
  cyan: '#0891b2',
  white: '#d4d4d8',
  brightBlack: '#71717a',
  brightRed: '#ef4444',
  brightGreen: '#22c55e',
  brightYellow: '#eab308',
  brightBlue: '#3b82f6',
  brightMagenta: '#a855f7',
  brightCyan: '#06b6d4',
  brightWhite: '#fafafa',
}

function getWsUrl(ptyId: string): string {
  const proto = location.protocol === 'https:' ? 'wss:' : 'ws:'
  return `${proto}//${location.host}/api/pty/${encodeURIComponent(ptyId)}/ws`
}

async function initTerminal() {
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

  // Watch for theme changes
  const observer = new MutationObserver(() => {
    if (term) {
      term.options.theme = isDarkMode() ? darkTheme : lightTheme
    }
  })
  observer.observe(document.documentElement, { attributes: true, attributeFilter: ['class'] })

  try {
    const result = await api.ptyCreate()
    sessionId.value = result.id
    connectWS(result.id)
  } catch (err: unknown) {
    const message = err instanceof Error ? err.message : String(err)
    term.writeln(`\r\n\x1b[31mFailed to create terminal: ${message}\x1b[0m`)
  }
}

function connectWS(ptyId: string) {
  if (!term) return

  const url = getWsUrl(ptyId)
  ws = new WebSocket(url)
  ws.binaryType = 'arraybuffer'

  ws.onopen = () => {
    connected.value = true
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
  <div class="flex flex-col h-full" style="background-color: var(--color-muted)">
    <!-- Header -->
    <div class="flex items-center justify-between px-3 py-1.5 border-b shrink-0" style="border-color: var(--color-border); background-color: var(--color-surface)">
      <div class="flex items-center gap-2">
        <span class="text-[11px] font-semibold uppercase tracking-wider" style="color: var(--color-muted-foreground)">Terminal</span>
        <span
          class="w-1.5 h-1.5 rounded-full"
          :style="{ backgroundColor: connected ? 'var(--color-primary)' : 'var(--color-border)' }"
        />
      </div>
      <button
        class="text-[10px] cursor-pointer transition-colors font-medium"
        style="color: var(--color-muted-foreground)"
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
  background-color: var(--term-bg, #fafafa) !important;
}
.dark :deep(.xterm-viewport) {
  background-color: #18181b !important;
}
</style>
