<script setup lang="ts">
import { ref } from 'vue'
import type { ToolCall } from '@/types/api'

const props = defineProps<{
  tool: ToolCall
}>()

const expanded = ref(false)

function formatArgs(args: string): string {
  try {
    const parsed = JSON.parse(args)
    return Object.entries(parsed)
      .map(([k, v]) => `${k}: ${typeof v === 'string' ? v.slice(0, 80) : JSON.stringify(v).slice(0, 80)}`)
      .join(', ')
  } catch {
    return args.slice(0, 120)
  }
}
</script>

<template>
  <div class="my-1">
    <button
      class="w-full flex items-center gap-2 px-3 py-1.5 rounded-lg text-left hover:bg-stone-100 transition-colors cursor-pointer"
      @click="expanded = !expanded"
    >
      <span class="text-[10px]" :class="{
        'text-stone-400': tool.status === 'running',
        'text-teal-600': tool.status === 'done',
        'text-red-500': tool.status === 'error',
      }">
        <template v-if="tool.status === 'running'">●</template>
        <template v-else-if="tool.status === 'done'">✓</template>
        <template v-else>✗</template>
      </span>
      <span class="font-mono text-xs text-stone-500">{{ tool.name }}</span>
      <span v-if="tool.status === 'running'" class="text-[10px] text-stone-400 animate-pulse">running</span>
      <svg
        class="w-3 h-3 text-stone-400 ml-auto transition-transform"
        :class="{ 'rotate-180': expanded }"
        viewBox="0 0 20 20" fill="currentColor"
      >
        <path fill-rule="evenodd" d="M5.23 7.21a.75.75 0 011.06.02L10 11.168l3.71-3.938a.75.75 0 111.08 1.04l-4.25 4.5a.75.75 0 01-1.08 0l-4.25-4.5a.75.75 0 01.02-1.06z" clip-rule="evenodd" />
      </svg>
    </button>
    <div v-if="expanded" class="ml-5 pl-3 border-l border-stone-200 text-xs font-mono text-stone-500 py-1.5 max-h-48 overflow-y-auto">
      <div class="mb-1">
        <span class="text-stone-400">args:</span> {{ formatArgs(tool.args) }}
      </div>
      <div v-if="tool.output" class="whitespace-pre-wrap text-stone-500">
        {{ tool.output.length > 500 ? tool.output.slice(0, 500) + `… (${tool.output.length} chars)` : tool.output }}
      </div>
      <div v-if="tool.error" class="text-red-500 whitespace-pre-wrap">{{ tool.error }}</div>
    </div>
  </div>
</template>
