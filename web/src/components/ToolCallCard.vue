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
  if (name === 'grep') return 'search'
  return 'generic'
})

// ─── Terminal renderer helpers ───
const terminalCommand = computed(() => {
  try {
    const parsed = JSON.parse(props.tool.args)
    return parsed.command || ''
  } catch { return '' }
})

// ─── Search renderer helpers ───
const searchArgsDisplay = computed(() => {
  try {
    const parsed = JSON.parse(props.tool.args)
    return Object.entries(parsed)
      .filter(([, v]) => v !== undefined && v !== null && v !== '')
      .map(([k, v]) => `${k}: ${typeof v === 'string' ? v : JSON.stringify(v)}`)
      .join('\n')
  } catch { return props.tool.args }
})

const searchResults = computed(() => {
  const output = props.tool.output || ''
  if (!output) return { lines: [], count: null }
  const countMatch = output.match(/\((\d+) (?:matches found|results?)\)/)
  const count = countMatch ? parseInt(countMatch[1]) : null
  const lines = output.split('\n')
    .filter(l => {
      const t = l.trim()
      return t && !t.startsWith('(')
    })
    .map(line => {
      const m = line.match(/^([^:]+):(\d+):(.*)$/)
      if (m) return { file: m[1], lineNum: parseInt(m[2]), content: m[3], isRef: true }
      return { file: '', lineNum: 0, content: line, isRef: false }
    })
  return { lines, count }
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

const shortFileDir = computed(() => {
  if (!fileDir.value) return ''
  const parts = fileDir.value.split('/').filter((p: string) => p)
  const lastTwo = parts.slice(-2)
  return lastTwo.length ? '…/' + lastTwo.join('/') : fileDir.value
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
        : ''"
      :style="tool.status !== 'running' ? 'border-color: var(--color-border); background: var(--color-surface)' : ''"
    >
      <button
        class="w-full flex items-center gap-2 px-3 py-2 text-left transition-colors cursor-pointer hover:opacity-80"
        style="background: transparent"
        @click="subagentExpanded = !subagentExpanded"
      >
        <span class="text-[10px]" :class="{
          'text-violet-500 dark:text-violet-400 animate-pulse': tool.status === 'running',
        }" :style="{
          color: tool.status === 'done' ? 'var(--color-primary)' : tool.status === 'error' ? 'var(--color-destructive)' : undefined
        }">
          <template v-if="tool.status === 'running'">◈</template>
          <template v-else-if="tool.status === 'done'">✓</template>
          <template v-else>✗</template>
        </span>
        <span class="text-[10px] font-semibold text-violet-500 dark:text-violet-400 uppercase tracking-wider">Subagent</span>
        <span class="font-mono text-xs" style="color: var(--color-foreground)">{{ subagentName() }}</span>
        <span
          v-if="tool.status === 'running'"
          class="text-[10px] text-violet-400 dark:text-violet-400 animate-pulse"
        >working…</span>
        <span
          v-if="tool.children?.length"
          class="ml-auto text-[10px] tabular-nums"
          style="color: var(--color-muted-foreground)"
        >{{ tool.children.length }} calls</span>
        <svg
          class="w-3 h-3 transition-transform shrink-0"
          :class="{ 'rotate-180': subagentExpanded }"
          style="color: var(--color-muted-foreground)"
          viewBox="0 0 20 20" fill="currentColor"
        >
          <path fill-rule="evenodd" d="M5.23 7.21a.75.75 0 011.06.02L10 11.168l3.71-3.938a.75.75 0 111.08 1.04l-4.25 4.5a.75.75 0 01-1.08 0l-4.25-4.5a.75.75 0 01.02-1.06z" clip-rule="evenodd" />
        </svg>
      </button>

      <div v-if="subagentExpanded" style="border-top: 1px solid var(--color-border)">
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
        <div v-else-if="tool.status === 'running'" class="px-3 py-3 text-xs animate-pulse" style="color: var(--color-muted-foreground)">
          Starting subagent…
        </div>

        <div v-if="tool.output" class="px-3 py-2" style="border-top: 1px solid var(--color-border)">
          <div class="text-[10px] uppercase tracking-wider mb-1" style="color: var(--color-muted-foreground)">Result</div>
          <div class="text-xs font-mono whitespace-pre-wrap max-h-48 overflow-y-auto" style="color: var(--color-muted-foreground)">
            {{ truncate(tool.output, 800) }}
          </div>
        </div>
        <div v-if="tool.error" class="px-3 py-2 border-t border-red-200 dark:border-red-500/20">
          <div class="text-xs font-mono whitespace-pre-wrap" style="color: var(--color-destructive)">{{ tool.error }}</div>
        </div>
      </div>
    </div>
  </div>

  <!-- Regular tool call -->
  <div v-else class="my-1">
    <button
      class="tool-trigger w-full flex items-center gap-2 px-3 py-1.5 rounded-md text-left transition-colors cursor-pointer border"
      :class="[
        expanded ? '' : 'border-transparent',
        tool.status === 'error' ? 'border-red-200/60 dark:border-red-500/20' : ''
      ]"
      :style="expanded
        ? 'background: var(--color-muted); border-color: var(--color-border)'
        : ''"
      @click="expanded = !expanded"
    >
      <!-- Status indicator -->
      <span class="text-[11px] shrink-0" :class="{
        'animate-pulse': tool.status === 'running',
      }">{{ displayIcon }}</span>

      <!-- Tool title -->
      <span class="text-xs font-medium" :style="{
        color: tool.status === 'error' ? 'var(--color-destructive)' : 'var(--color-muted-foreground)',
      }">
        <template v-if="tool.status === 'running'">
          <span class="inline-flex items-center gap-1">
            {{ displayTitle }}
            <span class="text-[10px] animate-pulse" style="color: var(--color-muted-foreground)">…</span>
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
        :style="isContextTool
          ? 'color: var(--color-muted-foreground)'
          : 'color: var(--color-primary)'"
      >{{ displaySubtitle }}</span>

      <!-- Diff stats badge for edit tools -->
      <span
        v-if="renderType === 'diff' && diffData.added + diffData.deleted > 0 && tool.status !== 'running'"
        class="text-[10px] font-mono tabular-nums shrink-0"
      >
        <span v-if="diffData.added" style="color: var(--color-primary)">+{{ diffData.added }}</span>
        <span v-if="diffData.added && diffData.deleted" class="mx-0.5" style="color: var(--color-muted-foreground)">/</span>
        <span v-if="diffData.deleted" class="text-red-500 dark:text-red-400">-{{ diffData.deleted }}</span>
      </span>

      <!-- Error badge -->
      <span
        v-if="tool.status === 'error'"
        class="text-[9px] font-semibold uppercase tracking-wider text-red-500 dark:text-red-400 bg-red-50 dark:bg-red-500/10 px-1.5 py-0.5 rounded-md"
      >error</span>

      <!-- Expand arrow -->
      <svg
        class="w-3 h-3 ml-auto transition-transform shrink-0"
        :class="{ 'rotate-180': expanded }"
        style="color: var(--color-muted-foreground)"
        viewBox="0 0 20 20" fill="currentColor"
      >
        <path fill-rule="evenodd" d="M5.23 7.21a.75.75 0 011.06.02L10 11.168l3.71-3.938a.75.75 0 111.08 1.04l-4.25 4.5a.75.75 0 01-1.08 0l-4.25-4.5a.75.75 0 01.02-1.06z" clip-rule="evenodd" />
      </svg>
    </button>

    <!-- ═══════ Expanded: Terminal (execute) ═══════ -->
    <div
      v-if="expanded && renderType === 'terminal'"
      class="mt-1 overflow-hidden"
      :class="tool.status === 'error'
        ? 'border border-red-300/60 dark:border-red-500/30'
        : ''"
      :style="{ borderRadius: 'var(--radius-xl)', border: tool.status !== 'error' ? '1px solid var(--color-border)' : undefined }"
    >
      <!-- Terminal body -->
      <div class="bg-[#fafafa] dark:bg-[#0d1117] px-3 py-2 font-mono text-xs max-h-72 overflow-y-auto">
        <div>
          <span class="text-zinc-400 dark:text-zinc-500 select-none">$ </span>
          <span class="text-zinc-800 dark:text-zinc-200">{{ terminalCommand }}</span>
        </div>
        <div v-if="tool.displayOutput || tool.output" class="text-zinc-600 dark:text-zinc-400 mt-1 whitespace-pre-wrap break-all">{{ truncate(tool.displayOutput || tool.output || '', 2000) }}</div>
        <div v-if="tool.error" class="text-red-600 dark:text-red-400 mt-1 whitespace-pre-wrap">{{ tool.error }}</div>
        <div v-if="tool.status === 'running'" class="text-zinc-400 dark:text-zinc-500 mt-1 animate-pulse">running…</div>
      </div>
    </div>

    <!-- ═══════ Expanded: File Viewer (read/write) ═══════ -->
    <div
      v-if="expanded && renderType === 'file-viewer'"
      class="mt-1 overflow-hidden"
      :class="tool.status === 'error'
        ? 'border border-red-300/60 dark:border-red-500/30'
        : ''"
      :style="{ borderRadius: 'var(--radius-xl)', border: tool.status !== 'error' ? '1px solid var(--color-border)' : undefined }"
    >
      <!-- File body -->
      <div class="bg-white dark:bg-[#0d1117] max-h-72 overflow-y-auto">
        <table v-if="fileLines.length" class="w-full text-xs font-mono border-collapse">
          <tr v-for="line in fileLines" :key="line.num" class="hover:bg-zinc-100/50 dark:hover:bg-zinc-800/30">
            <td class="text-right select-none pr-3 pl-2 py-0 w-1 align-top whitespace-nowrap" style="color: color-mix(in srgb, var(--color-muted-foreground), transparent 40%)">{{ line.num }}</td>
            <td class="pr-3 py-0 whitespace-pre-wrap break-all" style="color: var(--color-foreground)">{{ line.text }}</td>
          </tr>
        </table>
        <div v-else-if="tool.error" class="px-3 py-2 text-xs font-mono whitespace-pre-wrap" style="color: var(--color-destructive)">{{ tool.error }}</div>
        <div v-else-if="tool.status === 'running'" class="px-3 py-3 text-xs animate-pulse" style="color: var(--color-muted-foreground)">Loading…</div>
        <div v-else class="px-3 py-2 text-xs italic" style="color: var(--color-muted-foreground)">No content</div>
      </div>
    </div>

    <!-- ═══════ Expanded: Diff Viewer (edit/multi_edit) ═══════ -->
    <div
      v-if="expanded && renderType === 'diff'"
      class="mt-1 overflow-hidden"
      :class="tool.status === 'error'
        ? 'border border-red-300/60 dark:border-red-500/30'
        : ''"
      :style="{ borderRadius: 'var(--radius-xl)', border: tool.status !== 'error' ? '1px solid var(--color-border)' : undefined }"
    >
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
                }"
                :style="section.type === 'context' ? 'color: color-mix(in srgb, var(--color-muted-foreground), transparent 40%)' : ''"
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
                }"
                :style="section.type === 'context' ? 'color: var(--color-foreground)' : ''"
              >{{ line }}</td>
            </tr>
          </template>
        </table>
        <div v-else-if="tool.error" class="px-3 py-2 text-xs font-mono whitespace-pre-wrap" style="color: var(--color-destructive)">{{ tool.error }}</div>
        <div v-else-if="tool.status === 'running'" class="px-3 py-3 text-xs animate-pulse" style="color: var(--color-muted-foreground)">Applying…</div>
        <div v-else class="px-3 py-2 text-xs italic" style="color: var(--color-muted-foreground)">No changes</div>
      </div>
      <!-- Diff result -->
      <div v-if="tool.output && !tool.error" class="px-3 py-1 text-[10px] font-mono" style="background: var(--color-surface); color: var(--color-muted-foreground)">{{ truncate(tool.output, 200) }}</div>
    </div>

    <!-- ═══════ Expanded: Search Viewer (grep) ═══════ -->
    <div
      v-if="expanded && renderType === 'search'"
      class="mt-1 overflow-hidden"
      :style="{ borderRadius: 'var(--radius-xl)', border: '1px solid var(--color-border)' }"
    >
      <!-- Results -->
      <div class="px-3 py-2 max-h-72 overflow-y-auto" style="background: var(--color-surface)">
        <div v-if="searchArgsDisplay" class="text-[11px] font-mono whitespace-pre-wrap mb-2" style="color: var(--color-muted-foreground)">{{ searchArgsDisplay }}</div>
        <template v-if="searchResults.lines.length">
          <div
            v-for="(line, i) in searchResults.lines"
            :key="i"
            class="py-0.5"
          >
            <div v-if="line.isRef" class="flex items-baseline gap-1.5 text-[10px] font-mono">
              <span style="color: var(--color-primary)">{{ line.file }}</span>
              <span style="color: var(--color-muted-foreground)">:{{ line.lineNum }}</span>
            </div>
            <div class="text-xs font-mono whitespace-pre-wrap" style="color: var(--color-foreground)">{{ line.content }}</div>
          </div>
        </template>
        <div v-else-if="tool.status === 'running'" class="py-1 text-xs animate-pulse" style="color: var(--color-muted-foreground)">Searching…</div>
        <div v-else-if="tool.error" class="py-1 text-xs font-mono whitespace-pre-wrap" style="color: var(--color-destructive)">{{ tool.error }}</div>
        <div v-else class="py-1 text-xs italic" style="color: var(--color-muted-foreground)">No results</div>
        <div v-if="searchResults.count !== null" class="mt-1.5 text-[10px] font-mono" style="color: var(--color-muted-foreground)">({{ searchResults.count }} matches found)</div>
      </div>
    </div>

    <!-- ═══════ Expanded: Generic fallback ═══════ -->
    <div
      v-if="expanded && renderType === 'generic'"
      class="ml-3 mt-0.5 pl-3 border-l-2 text-xs font-mono py-2 max-h-64 overflow-y-auto transition-all"
      :style="'border-color: ' + (tool.status === 'error' ? 'var(--color-destructive)' : 'var(--color-border)')"
    >
      <div class="mb-1.5">
        <span class="text-[10px] uppercase tracking-wider" style="color: var(--color-muted-foreground)">args</span>
        <div class="mt-0.5" style="color: var(--color-muted-foreground)">{{ formatArgs(tool.args) }}</div>
      </div>
      <div v-if="tool.output" class="mt-2">
        <span class="text-[10px] uppercase tracking-wider" style="color: var(--color-muted-foreground)">output</span>
        <div class="whitespace-pre-wrap mt-0.5" style="color: var(--color-muted-foreground)">
          {{ truncate(tool.output, 500) }}
        </div>
      </div>
      <div v-if="tool.error" class="mt-2">
        <span class="text-[10px] uppercase tracking-wider" style="color: var(--color-destructive)">error</span>
        <div class="whitespace-pre-wrap mt-0.5" style="color: var(--color-destructive)">{{ tool.error }}</div>
      </div>
    </div>
  </div>
</template>
