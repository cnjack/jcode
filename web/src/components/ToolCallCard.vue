<script setup lang="ts">
import { ref, computed } from 'vue'
import type { ToolCall } from '@/types/api'
import { TOOL_ICONS } from '@/composables/toolInfo'

defineOptions({ name: 'ToolCallCard' })

const props = defineProps<{
  tool: ToolCall
  depth?: number
}>()

const expanded = ref(false)
const isSubagent = computed(() => props.tool.name === 'subagent')
const subagentExpanded = ref(true)

// Display info: use backend-provided or fall back to name
const displayTitle = computed(() => props.tool.displayInfo?.title || props.tool.name)
const displaySubtitle = computed(() => props.tool.displayInfo?.subtitle || '')
const displayIcon = computed(() => {
  const iconKey = props.tool.displayInfo?.icon || 'tool'
  return TOOL_ICONS[iconKey] || '🔧'
})
const isContextTool = computed(() => props.tool.displayInfo?.category === 'context')

// Determine render type for expanded content
const renderType = computed(() => {
  const name = props.tool.name
  if (name === 'execute') return 'terminal'
  if (name === 'read') return 'file-viewer'
  if (name === 'write') return 'file-viewer'
  if (name === 'edit' || name === 'multi_edit') return 'diff'
  return 'generic'
})

// ─── Terminal renderer helpers ───
const terminalCommand = computed(() => {
  try {
    const parsed = JSON.parse(props.tool.args)
    return parsed.command || ''
  } catch { return '' }
})

// ─── File viewer helpers ───
const filePath = computed(() => {
  try {
    const parsed = JSON.parse(props.tool.args)
    return parsed.file_path || ''
  } catch { return '' }
})

const fileName = computed(() => {
  if (!filePath.value) return ''
  const parts = filePath.value.replace(/\\/g, '/').split('/')
  return parts[parts.length - 1] || ''
})

const fileDir = computed(() => {
  if (!filePath.value) return ''
  const parts = filePath.value.replace(/\\/g, '/').split('/')
  parts.pop()
  const dir = parts.join('/')
  return dir || '/'
})

const fileContent = computed(() => {
  const name = props.tool.name
  if (name === 'write') {
    try {
      const parsed = JSON.parse(props.tool.args)
      return parsed.content || ''
    } catch { return '' }
  }
  // For read — output is the file content (with line numbers from the tool)
  return props.tool.output || ''
})

const fileLines = computed(() => {
  if (!fileContent.value) return []
  const raw = fileContent.value
  const lines = raw.split('\n')
  // The read tool outputs lines like "   1│content"
  // For write tool, content is raw — we add our own line numbers
  if (props.tool.name === 'read') {
    return lines.map((line: string) => {
      const match = line.match(/^\s*(\d+)│(.*)$/)
      if (match) return { num: parseInt(match[1]), text: match[2] }
      return { num: 0, text: line }
    }).filter((_: { num: number; text: string }, i: number, arr: { num: number; text: string }[]) => !(i === arr.length - 1 && _.text === ''))
  }
  // Write tool: raw content with line numbers we generate
  return lines.map((line: string, i: number) => ({ num: i + 1, text: line }))
})

// ─── Diff renderer helpers ───
const diffData = computed(() => {
  try {
    const parsed = JSON.parse(props.tool.args)
    const oldStr: string = parsed.old_string || ''
    const newStr: string = parsed.new_string || ''
    const isCreate = !oldStr && newStr

    // For multi_edit, try to show edits array
    if (props.tool.name === 'multi_edit' && parsed.edits) {
      const edits = parsed.edits as Array<{ old_string: string; new_string: string }>
      let totalAdd = 0, totalDel = 0
      const sections: Array<{ type: 'add' | 'del' | 'context'; lines: string[] }> = []
      for (const edit of edits) {
        if (edit.old_string) {
          const lines = edit.old_string.split('\n')
          totalDel += lines.length
          sections.push({ type: 'del', lines })
        }
        if (edit.new_string) {
          const lines = edit.new_string.split('\n')
          totalAdd += lines.length
          sections.push({ type: 'add', lines })
        }
      }
      return { added: totalAdd, deleted: totalDel, sections, isCreate: false }
    }

    const addedLines = newStr ? newStr.split('\n') : []
    const deletedLines = oldStr ? oldStr.split('\n') : []

    const sections: Array<{ type: 'add' | 'del' | 'context'; lines: string[] }> = []
    if (deletedLines.length && !isCreate) {
      sections.push({ type: 'del', lines: deletedLines })
    }
    if (addedLines.length) {
      sections.push({ type: 'add', lines: addedLines })
    }

    return {
      added: addedLines.length,
      deleted: isCreate ? 0 : deletedLines.length,
      sections,
      isCreate,
    }
  } catch {
    return { added: 0, deleted: 0, sections: [], isCreate: false }
  }
})

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
  <!-- Subagent card -->
  <div v-if="isSubagent" class="my-2">
    <div
      class="rounded-md border overflow-hidden transition-colors"
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
      class="tool-trigger w-full flex items-center gap-2 px-3 py-1.5 rounded-md text-left transition-colors cursor-pointer"
      :class="[
        expanded
          ? 'bg-zinc-100 dark:bg-zinc-800/80 border border-zinc-200/80 dark:border-zinc-700/50'
          : 'hover:bg-zinc-100 dark:hover:bg-zinc-800/60 border border-transparent',
        tool.status === 'error' ? 'border-red-200/60 dark:border-red-500/20' : ''
      ]"
      @click="expanded = !expanded"
    >
      <!-- Status indicator -->
      <span class="text-[11px] shrink-0" :class="{
        'animate-pulse': tool.status === 'running',
      }">{{ displayIcon }}</span>

      <!-- Tool title -->
      <span class="text-xs font-medium" :class="{
        'text-zinc-500 dark:text-zinc-400': tool.status !== 'error',
        'text-red-500 dark:text-red-400': tool.status === 'error',
      }">
        <template v-if="tool.status === 'running'">
          <span class="inline-flex items-center gap-1">
            {{ displayTitle }}
            <span class="text-[10px] text-zinc-400 dark:text-zinc-500 animate-pulse">…</span>
          </span>
        </template>
        <template v-else>
          {{ displayTitle }}
        </template>
      </span>

      <!-- Subtitle: file path, command, pattern, etc. -->
      <span
        v-if="displaySubtitle && tool.status !== 'running'"
        class="text-xs font-mono truncate min-w-0 flex-1"
        :class="isContextTool
          ? 'text-zinc-400 dark:text-zinc-500'
          : 'text-emerald-600/80 dark:text-emerald-400/70'"
      >{{ displaySubtitle }}</span>

      <!-- Diff stats badge for edit tools -->
      <span
        v-if="renderType === 'diff' && diffData.added + diffData.deleted > 0 && tool.status !== 'running'"
        class="text-[10px] font-mono tabular-nums shrink-0"
      >
        <span v-if="diffData.added" class="text-emerald-600 dark:text-emerald-400">+{{ diffData.added }}</span>
        <span v-if="diffData.added && diffData.deleted" class="text-zinc-400 dark:text-zinc-500 mx-0.5">/</span>
        <span v-if="diffData.deleted" class="text-red-500 dark:text-red-400">-{{ diffData.deleted }}</span>
      </span>

      <!-- Error badge -->
      <span
        v-if="tool.status === 'error'"
        class="text-[9px] font-semibold uppercase tracking-wider text-red-500 dark:text-red-400 bg-red-50 dark:bg-red-500/10 px-1.5 py-0.5 rounded-md"
      >error</span>

      <!-- Expand arrow -->
      <svg
        class="w-3 h-3 text-zinc-400 dark:text-zinc-500 ml-auto transition-transform shrink-0"
        :class="{ 'rotate-180': expanded }"
        viewBox="0 0 20 20" fill="currentColor"
      >
        <path fill-rule="evenodd" d="M5.23 7.21a.75.75 0 011.06.02L10 11.168l3.71-3.938a.75.75 0 111.08 1.04l-4.25 4.5a.75.75 0 01-1.08 0l-4.25-4.5a.75.75 0 01.02-1.06z" clip-rule="evenodd" />
      </svg>
    </button>

    <!-- ═══════ Expanded: Terminal (execute) ═══════ -->
    <div
      v-if="expanded && renderType === 'terminal'"
      class="mt-1 rounded border overflow-hidden"
      :class="tool.status === 'error'
        ? 'border-red-300/60 dark:border-red-500/30'
        : 'border-zinc-200 dark:border-zinc-700/60'"
    >
      <!-- Terminal header -->
      <div class="flex items-center gap-1.5 px-3 py-1.5 bg-zinc-100 dark:bg-zinc-800/80 border-b border-zinc-200 dark:border-zinc-700/60">
        <span class="w-2 h-2 rounded-full bg-red-400/80"></span>
        <span class="w-2 h-2 rounded-full bg-yellow-400/80"></span>
        <span class="w-2 h-2 rounded-full bg-green-400/80"></span>
        <span class="text-[10px] text-zinc-400 dark:text-zinc-500 ml-1 font-mono">terminal</span>
      </div>
      <!-- Terminal body -->
      <div class="bg-zinc-950 dark:bg-[#0d1117] px-3 py-2 font-mono text-xs max-h-72 overflow-y-auto">
        <div class="text-emerald-400 dark:text-emerald-400">
          <span class="text-zinc-500 select-none">$ </span>
          <span class="text-zinc-200">{{ terminalCommand }}</span>
        </div>
        <div v-if="tool.displayOutput || tool.output" class="text-zinc-400 mt-1 whitespace-pre-wrap break-all">{{ truncate(tool.displayOutput || tool.output || '', 2000) }}</div>
        <div v-if="tool.error" class="text-red-400 mt-1 whitespace-pre-wrap">{{ tool.error }}</div>
        <div v-if="tool.status === 'running'" class="text-zinc-500 mt-1 animate-pulse">running…</div>
      </div>
    </div>

    <!-- ═══════ Expanded: File Viewer (read/write) ═══════ -->
    <div
      v-if="expanded && renderType === 'file-viewer'"
      class="mt-1 rounded border overflow-hidden"
      :class="tool.status === 'error'
        ? 'border-red-300/60 dark:border-red-500/30'
        : 'border-zinc-200 dark:border-zinc-700/60'"
    >
      <!-- File header -->
      <div class="flex items-center gap-2 px-3 py-1.5 bg-zinc-50 dark:bg-zinc-800/80 border-b border-zinc-200 dark:border-zinc-700/60">
        <span class="text-[11px]">{{ tool.name === 'write' ? '✏️' : '📄' }}</span>
        <span class="text-[10px] text-zinc-400 dark:text-zinc-500 font-mono truncate">{{ fileDir }}/</span>
        <span class="text-xs text-zinc-700 dark:text-zinc-300 font-mono font-medium">{{ fileName }}</span>
      </div>
      <!-- File body -->
      <div class="bg-white dark:bg-[#0d1117] max-h-72 overflow-y-auto">
        <table v-if="fileLines.length" class="w-full text-xs font-mono border-collapse">
          <tr v-for="line in fileLines" :key="line.num" class="hover:bg-zinc-100/50 dark:hover:bg-zinc-800/30">
            <td class="text-right select-none text-zinc-300 dark:text-zinc-600 pr-3 pl-2 py-0 w-1 align-top whitespace-nowrap">{{ line.num }}</td>
            <td class="text-zinc-700 dark:text-zinc-300 pr-3 py-0 whitespace-pre-wrap break-all">{{ line.text }}</td>
          </tr>
        </table>
        <div v-else-if="tool.error" class="px-3 py-2 text-xs text-red-500 dark:text-red-400 font-mono whitespace-pre-wrap">{{ tool.error }}</div>
        <div v-else-if="tool.status === 'running'" class="px-3 py-3 text-xs text-zinc-400 dark:text-zinc-500 animate-pulse">Loading…</div>
        <div v-else class="px-3 py-2 text-xs text-zinc-400 dark:text-zinc-500 italic">No content</div>
      </div>
    </div>

    <!-- ═══════ Expanded: Diff Viewer (edit/multi_edit) ═══════ -->
    <div
      v-if="expanded && renderType === 'diff'"
      class="mt-1 rounded border overflow-hidden"
      :class="tool.status === 'error'
        ? 'border-red-300/60 dark:border-red-500/30'
        : 'border-zinc-200 dark:border-zinc-700/60'"
    >
      <!-- Diff header -->
      <div class="flex items-center gap-2 px-3 py-1.5 bg-zinc-50 dark:bg-zinc-800/80 border-b border-zinc-200 dark:border-zinc-700/60">
        <span class="text-[11px]">✏️</span>
        <span class="text-[10px] text-zinc-400 dark:text-zinc-500 font-mono truncate">{{ fileDir }}/</span>
        <span class="text-xs text-zinc-700 dark:text-zinc-300 font-mono font-medium">{{ fileName }}</span>
        <span class="ml-auto text-[10px] font-mono tabular-nums shrink-0">
          <span v-if="diffData.added" class="text-emerald-600 dark:text-emerald-400">+{{ diffData.added }}</span>
          <span v-if="diffData.added && diffData.deleted" class="text-zinc-400 dark:text-zinc-500 mx-0.5">/</span>
          <span v-if="diffData.deleted" class="text-red-500 dark:text-red-400">-{{ diffData.deleted }}</span>
          <span v-if="diffData.isCreate" class="text-emerald-600 dark:text-emerald-400 italic">new file</span>
        </span>
      </div>
      <!-- Diff body -->
      <div class="bg-white dark:bg-[#0d1117] max-h-72 overflow-y-auto">
        <table v-if="diffData.sections.length" class="w-full text-xs font-mono border-collapse">
          <template v-for="(section, si) in diffData.sections" :key="si">
            <tr
              v-for="(line, li) in section.lines"
              :key="`${si}-${li}`"
              :class="{
                'bg-red-50/80 dark:bg-red-500/10': section.type === 'del',
                'bg-emerald-50/80 dark:bg-emerald-500/10': section.type === 'add',
              }"
            >
              <td class="text-right select-none pr-2 pl-2 py-0 w-1 align-top whitespace-nowrap"
                :class="{
                  'text-red-300 dark:text-red-500/60': section.type === 'del',
                  'text-emerald-300 dark:text-emerald-500/60': section.type === 'add',
                  'text-zinc-300 dark:text-zinc-600': section.type === 'context',
                }"
              >{{ li + 1 }}</td>
              <td class="px-1 py-0 w-4 select-none font-bold"
                :class="{
                  'text-red-400 dark:text-red-400': section.type === 'del',
                  'text-emerald-500 dark:text-emerald-400': section.type === 'add',
                }"
              >{{ section.type === 'del' ? '-' : section.type === 'add' ? '+' : ' ' }}</td>
              <td class="pr-3 py-0 whitespace-pre-wrap break-all"
                :class="{
                  'text-red-700 dark:text-red-300': section.type === 'del',
                  'text-emerald-700 dark:text-emerald-300': section.type === 'add',
                  'text-zinc-700 dark:text-zinc-300': section.type === 'context',
                }"
              >{{ line }}</td>
            </tr>
          </template>
        </table>
        <div v-else-if="tool.error" class="px-3 py-2 text-xs text-red-500 dark:text-red-400 font-mono whitespace-pre-wrap">{{ tool.error }}</div>
        <div v-else-if="tool.status === 'running'" class="px-3 py-3 text-xs text-zinc-400 dark:text-zinc-500 animate-pulse">Applying…</div>
        <div v-else class="px-3 py-2 text-xs text-zinc-400 dark:text-zinc-500 italic">No changes</div>
      </div>
      <!-- Diff result -->
      <div v-if="tool.output && !tool.error" class="px-3 py-1.5 border-t border-zinc-200/60 dark:border-zinc-700/40 bg-zinc-50/50 dark:bg-zinc-800/40">
        <div class="text-[10px] text-zinc-400 dark:text-zinc-500 font-mono">{{ truncate(tool.output, 200) }}</div>
      </div>
    </div>

    <!-- ═══════ Expanded: Generic fallback ═══════ -->
    <div
      v-if="expanded && renderType === 'generic'"
      class="ml-3 mt-0.5 pl-3 border-l-2 text-xs font-mono py-2 max-h-64 overflow-y-auto transition-all"
      :class="tool.status === 'error'
        ? 'border-red-300 dark:border-red-500/30'
        : 'border-zinc-200 dark:border-zinc-700/60'"
    >
      <div class="mb-1.5">
        <span class="text-[10px] text-zinc-400 dark:text-zinc-500 uppercase tracking-wider">args</span>
        <div class="text-zinc-500 dark:text-zinc-400 mt-0.5">{{ formatArgs(tool.args) }}</div>
      </div>
      <div v-if="tool.output" class="mt-2">
        <span class="text-[10px] text-zinc-400 dark:text-zinc-500 uppercase tracking-wider">output</span>
        <div class="text-zinc-500 dark:text-zinc-400 whitespace-pre-wrap mt-0.5">
          {{ truncate(tool.output, 500) }}
        </div>
      </div>
      <div v-if="tool.error" class="mt-2">
        <span class="text-[10px] text-red-400 dark:text-red-500 uppercase tracking-wider">error</span>
        <div class="text-red-500 dark:text-red-400 whitespace-pre-wrap mt-0.5">{{ tool.error }}</div>
      </div>
    </div>
  </div>
</template>
