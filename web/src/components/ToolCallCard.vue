<script setup lang="ts">
import { ref, computed } from 'vue'
import type { ToolCall } from '@/types/api'

defineOptions({ name: 'ToolCallCard' })

const props = defineProps<{
  tool: ToolCall
  depth?: number
}>()

const expanded = ref(false)
const isSubagent = computed(() => props.tool.name === 'subagent')
const subagentExpanded = ref(true)

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

function subagentName(): string {
  try {
    const parsed = JSON.parse(props.tool.args)
    return parsed.name || parsed.description || 'subagent'
  } catch {
    return 'subagent'
  }
}

function truncate(text: string, max: number): string {
  return text.length > max ? text.slice(0, max) + `… (${text.length} chars)` : text
}
</script>

<template>
  <!-- Subagent card -->
  <div v-if="isSubagent" class="my-2">
    <div
      class="rounded-xl border overflow-hidden transition-colors"
      :class="tool.status === 'running'
        ? 'border-violet-300 dark:border-violet-500/30 bg-violet-50/30 dark:bg-violet-500/5'
        : 'border-zinc-200 dark:border-zinc-700/60 bg-zinc-50/50 dark:bg-zinc-800/30'"
    >
      <button
        class="w-full flex items-center gap-2 px-3 py-2 text-left hover:bg-zinc-100/50 dark:hover:bg-zinc-700/30 transition-colors cursor-pointer"
        @click="subagentExpanded = !subagentExpanded"
      >
        <span class="text-[10px]" :class="{
          'text-violet-500 dark:text-violet-400 animate-pulse': tool.status === 'running',
          'text-emerald-600 dark:text-emerald-400': tool.status === 'done',
          'text-red-500 dark:text-red-400': tool.status === 'error',
        }">
          <template v-if="tool.status === 'running'">◈</template>
          <template v-else-if="tool.status === 'done'">✓</template>
          <template v-else>✗</template>
        </span>
        <span class="text-[10px] font-semibold text-violet-500 dark:text-violet-400 uppercase tracking-wider">Subagent</span>
        <span class="font-mono text-xs text-zinc-600 dark:text-zinc-300">{{ subagentName() }}</span>
        <span
          v-if="tool.status === 'running'"
          class="text-[10px] text-violet-400 dark:text-violet-400 animate-pulse"
        >working…</span>
        <span
          v-if="tool.children?.length"
          class="ml-auto text-[10px] text-zinc-400 dark:text-zinc-500 tabular-nums"
        >{{ tool.children.length }} calls</span>
        <svg
          class="w-3 h-3 text-zinc-400 dark:text-zinc-500 transition-transform shrink-0"
          :class="{ 'rotate-180': subagentExpanded }"
          viewBox="0 0 20 20" fill="currentColor"
        >
          <path fill-rule="evenodd" d="M5.23 7.21a.75.75 0 011.06.02L10 11.168l3.71-3.938a.75.75 0 111.08 1.04l-4.25 4.5a.75.75 0 01-1.08 0l-4.25-4.5a.75.75 0 01.02-1.06z" clip-rule="evenodd" />
        </svg>
      </button>

      <div v-if="subagentExpanded" class="border-t border-zinc-200/60 dark:border-zinc-700/40">
        <div
          v-if="tool.children?.length"
          class="px-2 py-1 max-h-80 overflow-y-auto"
        >
          <ToolCallCard
            v-for="child in tool.children"
            :key="child.id"
            :tool="child"
            :depth="(depth ?? 0) + 1"
          />
        </div>
        <div v-else-if="tool.status === 'running'" class="px-3 py-3 text-xs text-zinc-400 dark:text-zinc-500 animate-pulse">
          Starting subagent…
        </div>

        <div v-if="tool.output" class="px-3 py-2 border-t border-zinc-200/60 dark:border-zinc-700/40">
          <div class="text-[10px] text-zinc-400 dark:text-zinc-500 uppercase tracking-wider mb-1">Result</div>
          <div class="text-xs font-mono text-zinc-500 dark:text-zinc-400 whitespace-pre-wrap max-h-48 overflow-y-auto">
            {{ truncate(tool.output, 800) }}
          </div>
        </div>
        <div v-if="tool.error" class="px-3 py-2 border-t border-red-200 dark:border-red-500/20">
          <div class="text-xs text-red-500 dark:text-red-400 font-mono whitespace-pre-wrap">{{ tool.error }}</div>
        </div>
      </div>
    </div>
  </div>

  <!-- Regular tool call -->
  <div v-else class="my-1">
    <button
      class="w-full flex items-center gap-2 px-3 py-1.5 rounded-xl text-left hover:bg-zinc-100 dark:hover:bg-zinc-800/60 transition-colors cursor-pointer"
      @click="expanded = !expanded"
    >
      <span class="text-[10px]" :class="{
        'text-zinc-400 dark:text-zinc-500 animate-pulse': tool.status === 'running',
        'text-emerald-600 dark:text-emerald-400': tool.status === 'done',
        'text-red-500 dark:text-red-400': tool.status === 'error',
      }">
        <template v-if="tool.status === 'running'">●</template>
        <template v-else-if="tool.status === 'done'">✓</template>
        <template v-else>✗</template>
      </span>
      <span class="font-mono text-xs text-zinc-500 dark:text-zinc-400">{{ tool.name }}</span>
      <span v-if="tool.status === 'running'" class="text-[10px] text-zinc-400 dark:text-zinc-500 animate-pulse">running</span>
      <svg
        class="w-3 h-3 text-zinc-400 dark:text-zinc-500 ml-auto transition-transform"
        :class="{ 'rotate-180': expanded }"
        viewBox="0 0 20 20" fill="currentColor"
      >
        <path fill-rule="evenodd" d="M5.23 7.21a.75.75 0 011.06.02L10 11.168l3.71-3.938a.75.75 0 111.08 1.04l-4.25 4.5a.75.75 0 01-1.08 0l-4.25-4.5a.75.75 0 01.02-1.06z" clip-rule="evenodd" />
      </svg>
    </button>
    <div v-if="expanded" class="ml-5 pl-3 border-l border-zinc-200 dark:border-zinc-700/60 text-xs font-mono text-zinc-500 dark:text-zinc-400 py-1.5 max-h-48 overflow-y-auto">
      <div class="mb-1">
        <span class="text-zinc-400 dark:text-zinc-500">args:</span> {{ formatArgs(tool.args) }}
      </div>
      <div v-if="tool.output" class="whitespace-pre-wrap text-zinc-500 dark:text-zinc-400">
        {{ truncate(tool.output, 500) }}
      </div>
      <div v-if="tool.error" class="text-red-500 dark:text-red-400 whitespace-pre-wrap">{{ tool.error }}</div>
    </div>
  </div>
</template>
