<script setup lang="ts">
import { ref, nextTick } from 'vue'
import { api } from '@/composables/api'

interface TerminalEntry {
  command: string
  output: string
  exitCode: number
  timestamp: number
}

const history = ref<TerminalEntry[]>([])
const input = ref('')
const running = ref(false)
const outputEl = ref<HTMLDivElement | null>(null)

async function execute() {
  const cmd = input.value.trim()
  if (!cmd || running.value) return
  input.value = ''
  running.value = true

  try {
    const result = await api.exec(cmd)
    history.value.push({
      command: cmd,
      output: result.output,
      exitCode: result.exit_code,
      timestamp: Date.now(),
    })
  } catch (err: any) {
    history.value.push({
      command: cmd,
      output: `Error: ${err.message}`,
      exitCode: -1,
      timestamp: Date.now(),
    })
  } finally {
    running.value = false
    await nextTick()
    if (outputEl.value) {
      outputEl.value.scrollTop = outputEl.value.scrollHeight
    }
  }
}

function handleKeyDown(e: KeyboardEvent) {
  if (e.key === 'Enter' && !e.shiftKey) {
    e.preventDefault()
    execute()
  }
}

function clearHistory() {
  history.value = []
}
</script>

<template>
  <div class="flex flex-col h-full bg-stone-50 border-t border-stone-200">
    <!-- Header -->
    <div class="flex items-center justify-between px-3 py-1.5 border-b border-stone-200 bg-stone-100/80">
      <span class="text-[11px] font-medium text-stone-500 uppercase tracking-wider">Terminal</span>
      <button
        v-if="history.length > 0"
        class="text-[10px] text-stone-400 hover:text-stone-600 cursor-pointer transition-colors"
        @click="clearHistory"
      >
        Clear
      </button>
    </div>

    <!-- Output -->
    <div ref="outputEl" class="flex-1 overflow-y-auto p-3 font-mono text-xs space-y-2 min-h-0">
      <div v-if="history.length === 0" class="text-stone-400 text-center py-4">
        Run commands in the workspace directory
      </div>
      <div v-for="entry in history" :key="entry.timestamp" class="space-y-0.5">
        <div class="flex items-center gap-1.5">
          <span class="text-teal-600">$</span>
          <span class="text-stone-700">{{ entry.command }}</span>
        </div>
        <pre
          v-if="entry.output"
          class="whitespace-pre-wrap text-[11px] leading-relaxed pl-4"
          :class="entry.exitCode !== 0 ? 'text-red-600' : 'text-stone-500'"
        >{{ entry.output }}</pre>
        <div v-if="entry.exitCode !== 0" class="text-[10px] text-red-500 pl-4">
          exit code: {{ entry.exitCode }}
        </div>
      </div>
      <div v-if="running" class="flex items-center gap-1.5 text-stone-400">
        <span class="animate-pulse">●</span> running...
      </div>
    </div>

    <!-- Input -->
    <div class="border-t border-stone-200 px-3 py-2 flex items-center gap-2">
      <span class="text-teal-600 font-mono text-xs">$</span>
      <input
        v-model="input"
        type="text"
        placeholder="Enter command..."
        class="flex-1 bg-transparent text-stone-700 text-xs font-mono outline-none placeholder-stone-400"
        @keydown="handleKeyDown"
        :disabled="running"
      />
    </div>
  </div>
</template>
