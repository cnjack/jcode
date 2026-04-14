<script setup lang="ts">
import { ref, computed } from 'vue'
import type { ToolCall } from '@/types/api'

const props = defineProps<{
  tool: ToolCall
}>()

const expanded = ref(false)
const isSubagent = computed(() => props.tool.name === 'subagent')

/** Auto-expand subagent cards so users can see live progress. */
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
  <!-- Subagent: special nested card -->
  <div v-if="isSubagent" class="my-2">
    <div
      class="rounded-lg border border-stone-200 overflow-hidden"
      :class="tool.status === 'running' ? 'border-indigo-200 bg-indigo-50/30' : 'bg-stone-50/50'"
    >
      <!-- Subagent header -->
      <button
        class="w-full flex items-center gap-2 px-3 py-2 text-left hover:bg-stone-100/50 transition-colors cursor-pointer"
        @click="subagentExpanded = !subagentExpanded"
      >
        <span class="text-[10px]" :class="{
          'text-indigo-400 animate-pulse': tool.status === 'running',
          'text-teal-600': tool.status === 'done',
          'text-red-500': tool.status === 'error',
        }">
          <template v-if="tool.status === 'running'">◈</template>
          <template v-else-if="tool.status === 'done'">✓</template>
          <template v-else>✗</template>
        </span>
        <span class="text-[10px] font-medium text-indigo-500 uppercase tracking-wider">Subagent</span>
        <span class="font-mono text-xs text-stone-600">{{ subagentName() }}</span>
        <span
          v-if="tool.status === 'running'"
          class="text-[10px] text-indigo-400 animate-pulse"
        >working…</span>
        <span
          v-if="tool.children?.length"
          class="ml-auto text-[10px] text-stone-400 tabular-nums"
        >{{ tool.children.length }} calls</span>
        <svg
          class="w-3 h-3 text-stone-400 transition-transform shrink-0"
          :class="{ 'rotate-180': subagentExpanded }"
          viewBox="0 0 20 20" fill="currentColor"
        >
          <path fill-rule="evenodd" d="M5.23 7.21a.75.75 0 011.06.02L10 11.168l3.71-3.938a.75.75 0 111.08 1.04l-4.25 4.5a.75.75 0 01-1.08 0l-4.25-4.5a.75.75 0 01.02-1.06z" clip-rule="evenodd" />
        </svg>
      </button>

      <!-- Subagent body -->
      <div v-if="subagentExpanded" class="border-t border-stone-200/60">
        <!-- Inner tool calls -->
        <div
          v-if="tool.children?.length"
          class="px-3 py-2 space-y-0.5 max-h-64 overflow-y-auto"
        >
          <div
            v-for="(child, idx) in tool.children"
            :key="idx"
            class="flex items-start gap-2 text-xs py-0.5"
          >
            <span
              v-if="child.event === 'tool_call'"
              class="text-[10px] text-blue-400 mt-0.5 shrink-0"
            >→</span>
            <span
              v-else
              class="text-[10px] text-teal-400 mt-0.5 shrink-0"
            >←</span>
            <span class="font-mono text-stone-500 shrink-0">{{ child.toolName }}</span>
            <span class="text-stone-400 truncate">{{ truncate(child.detail, 120) }}</span>
          </div>
        </div>
        <div v-else-if="tool.status === 'running'" class="px-3 py-3 text-xs text-stone-400 animate-pulse">
          Starting subagent…
        </div>

        <!-- Final output (when done) -->
        <div v-if="tool.output" class="px-3 py-2 border-t border-stone-200/60">
          <div class="text-[10px] text-stone-400 uppercase tracking-wider mb-1">Result</div>
          <div class="text-xs font-mono text-stone-500 whitespace-pre-wrap max-h-48 overflow-y-auto">
            {{ truncate(tool.output, 800) }}
          </div>
        </div>
        <div v-if="tool.error" class="px-3 py-2 border-t border-red-100">
          <div class="text-xs text-red-500 font-mono whitespace-pre-wrap">{{ tool.error }}</div>
        </div>
      </div>
    </div>
  </div>

  <!-- Regular tool call -->
  <div v-else class="my-1">
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
        {{ truncate(tool.output, 500) }}
      </div>
      <div v-if="tool.error" class="text-red-500 whitespace-pre-wrap">{{ tool.error }}</div>
    </div>
  </div>
</template>
