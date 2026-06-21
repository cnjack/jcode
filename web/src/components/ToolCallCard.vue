<script setup lang="ts">
import { ref, computed } from 'vue'
import { ChevronDownIcon } from '@heroicons/vue/24/outline'
import { useI18n } from 'vue-i18n'
import type { ToolCall, TodoItem } from '@/types/api'
import TaskList from './TaskList.vue'
import AskUserCard from './AskUserCard.vue'

defineOptions({ name: 'ToolCallCard' })

const props = defineProps<{
  tool: ToolCall
  depth?: number
}>()

const { t } = useI18n()
const expanded = ref(false)
const isSubagent = computed(() => props.tool.name === 'subagent')
const isAskUser = computed(() => props.tool.name === 'ask_user')
const subagentExpanded = ref(true)

// Display info: use backend-provided or fall back to name
const displayTitle = computed(() => props.tool.displayInfo?.title || props.tool.name)
const displaySubtitle = computed(() => props.tool.displayInfo?.subtitle || '')
const isContextTool = computed(() => props.tool.displayInfo?.category === 'context')

// Determine render type for expanded content
const renderType = computed(() => {
  const name = props.tool.name
  if (name === 'execute') return 'terminal'
  if (name === 'read') return 'file-viewer'
  if (name === 'write') return 'file-viewer'
  if (name === 'edit' || name === 'multi_edit') return 'diff'
  if (name === 'grep') return 'search'
  if (name === 'todowrite' || name === 'todoread') return 'todo'
  if (name === 'load_skill') return 'skill'
  if (name === 'team_list') return 'team-list'
  if (name === 'team_send_message') return 'team-message'
  if (name === 'team_create') return 'team-create'
  if (name === 'team_spawn') return 'team-spawn'
  return 'generic'
})

// ─── Skill renderer helpers ───
const skillData = computed(() => {
  try {
    const skillName = JSON.parse(props.tool.args).name || ''
    const output = props.tool.output || ''
    const descMatch = output.match(/description="([^"]*)"/)
    return { name: skillName, description: descMatch ? descMatch[1] : '' }
  } catch { return { name: '', description: '' } }
})

// ─── Team list renderer helpers ───
interface TeamMember { name: string; status: string; type: string; progress: string }
const teamListData = computed(() => {
  const output = props.tool.output || ''
  const teamMatch = output.match(/^Team: (.+?) \((\d+)/)
  const teamName = teamMatch ? teamMatch[1] : ''
  const members: TeamMember[] = []
  for (const line of output.split('\n')) {
    const m = line.match(/@(\S+)\s+status=(\S+)\s+type=(\S*)(.*)/)
    if (m) {
      const progress = m[4] ? m[4].trim() : ''
      members.push({ name: m[1] ?? '', status: m[2] ?? '', type: m[3] ?? '', progress })
    }
  }
  return { teamName, members }
})

function memberStatusColor(status: string): string {
  if (status === 'running' || status === 'busy') return 'var(--color-primary)'
  if (status === 'done' || status === 'finished') return 'var(--color-success-fg)'
  if (status === 'error') return 'var(--color-destructive)'
  return 'var(--color-muted-foreground)'
}

// ─── Team create renderer helpers ───
const teamCreateData = computed(() => {
  try {
    const parsed = JSON.parse(props.tool.args)
    const output = props.tool.output || ''
    const leadMatch = output.match(/Lead agent: (\S+)/)
    return {
      teamName: parsed.team_name || '',
      description: parsed.description || '',
      lead: leadMatch ? leadMatch[1] : '',
    }
  } catch { return { teamName: '', description: '', lead: '' } }
})

// ─── Team spawn renderer helpers ───
const teamSpawnData = computed(() => {
  try {
    const parsed = JSON.parse(props.tool.args)
    const output = props.tool.output || ''
    const idMatch = output.match(/\(ID: ([^)]+)\)/)
    return {
      name: parsed.name || '',
      prompt: parsed.prompt || '',
      agentType: parsed.agent_type || '',
      id: idMatch ? idMatch[1] : '',
    }
  } catch { return { name: '', prompt: '', agentType: '', id: '' } }
})

// ─── Team message renderer helpers ───
const teamMsgData = computed(() => {
  try {
    const parsed = JSON.parse(props.tool.args)
    return {
      to: parsed.to || '',
      message: parsed.message || '',
      summary: parsed.summary || '',
    }
  } catch { return { to: '', message: '', summary: '' } }
})

// ─── Todo renderer helpers ───
const todoItems = computed((): TodoItem[] => {
  try {
    // Prefer output (final state), fall back to args (requested state)
    const output = props.tool.output || ''
    const jsonMatch = output.match(/\[.*\]/s)
    if (jsonMatch) return JSON.parse(jsonMatch[0]) as TodoItem[]
    const parsed = JSON.parse(props.tool.args)
    return (parsed.todos || []) as TodoItem[]
  } catch { return [] }
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
  const count = countMatch ? parseInt(countMatch[1] ?? '') : null
  const lines = output.split('\n')
    .filter(l => {
      const t = l.trim()
      return t && !t.startsWith('(')
    })
    .map(line => {
      const m = line.match(/^([^:]+):(\d+):(.*)$/)
      if (m) return { file: m[1] ?? '', lineNum: parseInt(m[2] ?? ''), content: m[3] ?? '', isRef: true }
      return { file: '', lineNum: 0, content: line, isRef: false }
    })
  return { lines, count }
})

// ─── File viewer helpers ───
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
      if (match) return { num: parseInt(match[1] ?? ''), text: match[2] ?? '' }
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
  // Count by code point ([...str]) so CJK text — where one character is far
  // wider visually than one UTF-16 code unit — truncates at a comparable visual
  // length to ASCII text. The trailing "(N chars)" still reports the raw length
  // for debugging.
  const chars = [...text]
  return chars.length > max ? chars.slice(0, max).join('') + `… (${chars.length} chars)` : text
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
  <!-- Subagent card — borderless header, children indented below -->
  <div v-if="isSubagent" class="my-1">
    <!-- Header: no box, just a plain label row -->
    <button
      class="w-full flex items-center gap-1.5 pl-0 pr-1 py-1 text-left cursor-pointer hover:opacity-70 transition-opacity"
      style="background: transparent"
      @click="subagentExpanded = !subagentExpanded"
    >
      <span
        class="text-[10px] shrink-0"
        :class="{ 'animate-pulse': tool.status === 'running' }"
        :style="{ color: tool.status === 'done' ? 'var(--color-primary)' : tool.status === 'error' ? 'var(--color-destructive)' : 'var(--color-muted-foreground)' }"
      >
        <template v-if="tool.status === 'running'">◈</template>
        <template v-else-if="tool.status === 'done'">✓</template>
        <template v-else>✗</template>
      </span>
      <span class="text-[10px] font-semibold uppercase tracking-wider" style="color: var(--color-muted-foreground)">{{ t('tool.subagentLabel') }}</span>
      <span class="text-[11px] font-mono" style="color: var(--color-foreground)">{{ subagentName() }}</span>
      <span v-if="tool.status === 'running'" class="text-[10px] animate-pulse" style="color: var(--color-muted-foreground)">{{ t('tool.working') }}</span>
      <span v-if="tool.children?.length" class="ml-auto text-[10px] tabular-nums" style="color: var(--color-muted-foreground)">{{ t('tool.calls', { n: tool.children.length }) }}</span>
      <ChevronDownIcon
        class="w-3 h-3 transition-transform shrink-0 ml-1"
        :class="{ 'rotate-180': subagentExpanded }"
        style="color: var(--color-muted-foreground)"
      />
    </button>

    <!-- Children: same inset as regular tool content -->
    <div v-if="subagentExpanded" class="mx-2 mt-1 mb-1">
      <div v-if="tool.children?.length" class="max-h-[600px] overflow-y-auto">
        <ToolCallCard
          v-for="child in tool.children"
          :key="child.id"
          :tool="child"
          :depth="(depth ?? 0) + 1"
        />
      </div>
      <div v-else-if="tool.status === 'running'" class="py-2 text-xs animate-pulse" style="color: var(--color-muted-foreground)">
        {{ t('tool.starting') }}
      </div>
      <div
        v-if="tool.output"
        class="overflow-hidden mt-1"
        :style="{ borderRadius: 'var(--radius-xl)', border: '1px solid var(--color-border)' }"
      >
        <div class="px-3 py-2 text-xs font-mono whitespace-pre-wrap max-h-48 overflow-y-auto" style="color: var(--color-muted-foreground); background: var(--color-surface)">{{ truncate(tool.output, 800) }}</div>
      </div>
      <div v-if="tool.error" class="mt-1 px-3 py-2 text-xs font-mono whitespace-pre-wrap overflow-hidden" :style="{ borderRadius: 'var(--radius-xl)', border: '1px solid var(--color-destructive)', color: 'var(--color-destructive)' }">{{ tool.error }}</div>
    </div>
  </div>

  <!-- Ask User — interactive question card (or static answer on replay) -->
  <AskUserCard v-else-if="isAskUser" :tool="tool" />

  <!-- Regular tool call -->
  <div v-else class="my-1">
    <!-- Trigger: only shown when collapsed -->
    <button
      v-if="!expanded"
      class="w-full flex items-center gap-1.5 pl-0 pr-1 py-1 text-left cursor-pointer hover:opacity-70 transition-opacity"
      style="background: transparent"
      @click="expanded = true"
    >
      <span
        class="text-xs font-medium"
        :class="{ 'shimmer-running': tool.status === 'running' }"
        :style="{ color: tool.status === 'error' ? 'var(--color-destructive)' : 'var(--color-muted-foreground)' }"
      >{{ displayTitle }}</span>
      <span
        v-if="displaySubtitle"
        class="text-xs font-mono truncate"
        :style="isContextTool ? 'color: var(--color-muted-foreground)' : 'color: var(--color-primary)'"
      >{{ displaySubtitle }}</span>
      <ChevronDownIcon class="w-3 h-3 shrink-0" style="color: var(--color-muted-foreground)" />
      <span v-if="renderType === 'diff' && diffData.added + diffData.deleted > 0" class="ml-auto text-[10px] font-mono tabular-nums shrink-0">
        <span v-if="diffData.added" style="color: var(--color-success-fg)">+{{ diffData.added }}</span>
        <span v-if="diffData.added && diffData.deleted" class="mx-0.5" style="color: var(--color-muted-foreground)">/</span>
        <span v-if="diffData.deleted" style="color: var(--color-error-fg)">-{{ diffData.deleted }}</span>
      </span>
    </button>

    <!-- Expanded: header outside, content box below -->
    <div v-else>
      <!-- Header: plain text row, no box — click to collapse -->
      <div class="flex items-center gap-1.5 pl-0 pr-1 py-1 cursor-pointer hover:opacity-70 transition-opacity" @click="expanded = false">
        <span
          class="text-xs font-medium"
          :class="{ 'shimmer-running': tool.status === 'running' }"
          :style="{ color: tool.status === 'error' ? 'var(--color-destructive)' : 'var(--color-muted-foreground)' }"
        >{{ displayTitle }}</span>
        <span
          v-if="displaySubtitle"
          class="text-xs font-mono truncate"
          :style="isContextTool ? 'color: var(--color-muted-foreground)' : 'color: var(--color-primary)'"
        >{{ displaySubtitle }}</span>
        <ChevronDownIcon class="w-3 h-3 shrink-0 rotate-180" style="color: var(--color-muted-foreground)" />
        <span v-if="renderType === 'diff' && diffData.added + diffData.deleted > 0" class="ml-auto text-[10px] font-mono tabular-nums shrink-0">
          <span v-if="diffData.added" style="color: var(--color-success-fg)">+{{ diffData.added }}</span>
          <span v-if="diffData.added && diffData.deleted" class="mx-0.5" style="color: var(--color-muted-foreground)">/</span>
          <span v-if="diffData.deleted" style="color: var(--color-error-fg)">-{{ diffData.deleted }}</span>
        </span>
      </div>
      <!-- Content box: only content gets the border. data-selectable opts the
           tool output (terminal/file/diff/search/generic) back into text
           selection inside the desktop shell, where .app-shell sets
           user-select:none — otherwise the most copy-worthy content (command
           output, file contents, grep hits) couldn't be selected. -->
      <div
        data-selectable
        class="overflow-hidden ml-0 mr-2 mt-1.5 mb-1.5"
        :style="{ borderRadius: 'var(--radius-xl)', border: tool.status === 'error' ? '1px solid var(--color-destructive)' : '1px solid var(--color-border)' }"
      >

      <!-- ═══════ Terminal (execute) ═══════ -->
      <div v-if="renderType === 'terminal'" class="px-3 py-2 font-mono text-xs max-h-72 overflow-y-auto" style="background: var(--color-muted)">
        <div>
          <span class="select-none" style="color: var(--color-muted-foreground)">$ </span>
          <span style="color: var(--color-foreground)">{{ terminalCommand }}</span>
        </div>
        <div v-if="tool.displayOutput || tool.output" class="mt-1 whitespace-pre-wrap break-all" style="color: var(--color-muted-foreground)">{{ truncate(tool.displayOutput || tool.output || '', 2000) }}</div>
        <div v-if="tool.error" class="mt-1 whitespace-pre-wrap" style="color: var(--color-error-fg)">{{ tool.error }}</div>
        <div v-if="tool.status === 'running'" class="mt-1 animate-pulse" style="color: var(--color-muted-foreground)">{{ t('tool.running') }}</div>
      </div>

      <!-- ═══════ File Viewer (read/write) ═══════ -->
      <div v-else-if="renderType === 'file-viewer'" class="max-h-72 overflow-y-auto" style="background: var(--color-surface)">
        <table v-if="fileLines.length" class="w-full text-xs font-mono border-collapse">
          <tr v-for="line in fileLines" :key="line.num" class="hover:bg-[var(--accent-wash-soft)]">
            <td class="text-right select-none pr-3 pl-2 py-0 w-1 align-top whitespace-nowrap" style="color: color-mix(in srgb, var(--color-muted-foreground), transparent 40%)">{{ line.num }}</td>
            <td class="pr-3 py-0 whitespace-pre-wrap break-all" style="color: var(--color-foreground)">{{ line.text }}</td>
          </tr>
        </table>
        <div v-else-if="tool.error" class="px-3 py-2 text-xs font-mono whitespace-pre-wrap" style="color: var(--color-destructive)">{{ tool.error }}</div>
        <div v-else-if="tool.status === 'running'" class="px-3 py-3 text-xs animate-pulse" style="color: var(--color-muted-foreground)">{{ t('tool.loading') }}</div>
        <div v-else class="px-3 py-2 text-xs italic" style="color: var(--color-muted-foreground)">{{ t('tool.noContent') }}</div>
      </div>

      <!-- ═══════ Diff Viewer (edit/multi_edit) ═══════ -->
      <div v-else-if="renderType === 'diff'" class="max-h-72 overflow-y-auto" style="background: var(--color-surface)">
        <table v-if="diffData.sections.length" class="w-full text-xs font-mono border-collapse">
          <template v-for="(section, si) in diffData.sections" :key="si">
            <tr
              v-for="(line, li) in section.lines"
              :key="`${si}-${li}`"
              :style="section.type === 'del' ? 'background: var(--color-error-bg)' : section.type === 'add' ? 'background: var(--color-success-bg)' : ''"
            >
              <td class="text-right select-none pr-2 pl-2 py-0 w-1 align-top whitespace-nowrap"
                :style="section.type === 'del' ? 'color: var(--color-error-fg)' : section.type === 'add' ? 'color: var(--color-success-fg)' : 'color: color-mix(in srgb, var(--color-muted-foreground), transparent 40%)'"
              >{{ li + 1 }}</td>
              <td class="px-1 py-0 w-4 select-none font-bold"
                :style="section.type === 'del' ? 'color: var(--color-error-fg)' : section.type === 'add' ? 'color: var(--color-success-fg)' : ''"
              >{{ section.type === 'del' ? '-' : section.type === 'add' ? '+' : ' ' }}</td>
              <td class="pr-3 py-0 whitespace-pre-wrap break-all"
                :style="section.type === 'del' ? 'color: var(--color-error-fg)' : section.type === 'add' ? 'color: var(--color-success-fg)' : 'color: var(--color-foreground)'"
              >{{ line }}</td>
            </tr>
          </template>
        </table>
        <div v-else-if="tool.error" class="px-3 py-2 text-xs font-mono whitespace-pre-wrap" style="color: var(--color-destructive)">{{ tool.error }}</div>
        <div v-else-if="tool.status === 'running'" class="px-3 py-3 text-xs animate-pulse" style="color: var(--color-muted-foreground)">{{ t('tool.applying') }}</div>
        <div v-else class="px-3 py-2 text-xs italic" style="color: var(--color-muted-foreground)">{{ t('tool.noChanges') }}</div>
        <div v-if="tool.output && !tool.error" class="px-3 py-1 text-[10px] font-mono" style="background: var(--color-surface); color: var(--color-muted-foreground)">{{ truncate(tool.output, 200) }}</div>
      </div>

      <!-- ═══════ Search Viewer (grep) ═══════ -->
      <div v-else-if="renderType === 'search'" class="px-3 py-2 max-h-72 overflow-y-auto" style="background: var(--color-surface)">
        <div v-if="searchArgsDisplay" class="text-[11px] font-mono whitespace-pre-wrap mb-2" style="color: var(--color-muted-foreground)">{{ searchArgsDisplay }}</div>
        <template v-if="searchResults.lines.length">
          <div v-for="(line, i) in searchResults.lines" :key="i" class="py-0.5">
            <div v-if="line.isRef" class="flex items-baseline gap-1.5 text-[10px] font-mono">
              <span style="color: var(--color-primary)">{{ line.file }}</span>
              <span style="color: var(--color-muted-foreground)">:{{ line.lineNum }}</span>
            </div>
            <div class="text-xs font-mono whitespace-pre-wrap" style="color: var(--color-foreground)">{{ line.content }}</div>
          </div>
        </template>
        <div v-else-if="tool.status === 'running'" class="py-1 text-xs animate-pulse" style="color: var(--color-muted-foreground)">{{ t('tool.searching') }}</div>
        <div v-else-if="tool.error" class="py-1 text-xs font-mono whitespace-pre-wrap" style="color: var(--color-destructive)">{{ tool.error }}</div>
        <div v-else class="py-1 text-xs italic" style="color: var(--color-muted-foreground)">{{ t('tool.noResults') }}</div>
        <div v-if="searchResults.count !== null" class="mt-1.5 text-[10px] font-mono" style="color: var(--color-muted-foreground)">{{ t('tool.matchesFound', { n: searchResults.count }) }}</div>
      </div>

      <!-- ═══════ Todo List ═══════ -->
      <div v-else-if="renderType === 'todo'" class="px-3 py-2 max-h-64 overflow-y-auto" style="background: var(--color-surface)">
        <TaskList v-if="todoItems.length" :todos="todoItems" />
        <div v-else-if="tool.status === 'running'" class="py-1 text-xs animate-pulse" style="color: var(--color-muted-foreground)">{{ t('tool.loading') }}</div>
        <div v-else class="py-1 text-xs italic" style="color: var(--color-muted-foreground)">{{ t('tool.noTodos') }}</div>
        <div v-if="tool.error" class="mt-1.5 text-xs font-mono whitespace-pre-wrap" style="color: var(--color-destructive)">{{ tool.error }}</div>
      </div>

      <!-- ═══════ Skill Loader ═══════ -->
      <div v-else-if="renderType === 'skill'" class="px-3 py-2.5" style="background: var(--color-surface)">
        <div class="flex items-center gap-2">
          <span class="text-[11px] font-semibold font-mono" style="color: var(--color-foreground)">{{ skillData.name }}</span>
          <span
            v-if="tool.status === 'running'"
            class="text-[10px] animate-pulse"
            style="color: var(--color-muted-foreground)"
          >{{ t('tool.loadingLower') }}</span>
        </div>
        <div
          v-if="skillData.description"
          class="mt-1 text-[11px] leading-snug"
          style="color: var(--color-muted-foreground)"
        >{{ skillData.description }}</div>
        <div v-if="tool.error" class="mt-1 text-[11px] font-mono" style="color: var(--color-destructive)">{{ tool.error }}</div>
      </div>

      <!-- ═══════ Team List ═══════ -->
      <div v-else-if="renderType === 'team-list'" class="px-3 py-2 max-h-64 overflow-y-auto" style="background: var(--color-surface)">
        <div
          v-if="teamListData.teamName"
          class="flex items-center gap-2 mb-2 pb-1.5"
          style="border-bottom: 1px solid var(--color-border)"
        >
          <span class="text-[10px] uppercase tracking-wider font-semibold" style="color: var(--color-muted-foreground)">{{ t('tool.teamLabel') }}</span>
          <span class="text-xs font-mono font-semibold" style="color: var(--color-foreground)">{{ teamListData.teamName }}</span>
          <span class="ml-auto text-[10px] tabular-nums" style="color: var(--color-muted-foreground)">{{ t('tool.members', { n: teamListData.members.length }) }}</span>
        </div>
        <div v-if="teamListData.members.length" class="space-y-0.5">
          <div v-for="(member, idx) in teamListData.members" :key="`${member.name}-${idx}`" class="flex items-center gap-2 py-0.5">
            <span class="text-[11px] w-3 text-center shrink-0" :style="{ color: memberStatusColor(member.status) }">●</span>
            <span class="text-xs font-mono flex-1" style="color: var(--color-foreground)">@{{ member.name }}</span>
            <span
              class="text-[10px] font-mono px-1.5 py-0.5 rounded tabular-nums"
              style="background: var(--color-muted); color: var(--color-muted-foreground)"
            >{{ member.status }}</span>
            <span v-if="member.type" class="text-[10px]" style="color: var(--color-muted-foreground)">{{ member.type }}</span>
          </div>
        </div>
        <div v-else-if="tool.status === 'running'" class="py-1 text-xs animate-pulse" style="color: var(--color-muted-foreground)">{{ t('tool.loading') }}</div>
        <div v-else class="py-1 text-xs italic" style="color: var(--color-muted-foreground)">{{ t('tool.noTeammates') }}</div>
        <div v-if="tool.error" class="mt-1.5 text-xs font-mono" style="color: var(--color-destructive)">{{ tool.error }}</div>
      </div>

      <!-- ═══════ Team Create ═══════ -->
      <div v-else-if="renderType === 'team-create'" class="px-3 py-2.5" style="background: var(--color-surface)">
        <div class="flex items-center gap-2 mb-1">
          <span class="text-xs font-mono font-semibold" style="color: var(--color-foreground)">{{ teamCreateData.teamName }}</span>
          <span v-if="tool.status === 'done' && !tool.error" class="ml-auto text-[10px] font-semibold" style="color: var(--color-primary)">{{ t('tool.created') }}</span>
          <span v-else-if="tool.status === 'running'" class="ml-auto text-[10px] animate-pulse" style="color: var(--color-muted-foreground)">{{ t('tool.creating') }}</span>
        </div>
        <div v-if="teamCreateData.description" class="text-[11px] leading-snug" style="color: var(--color-muted-foreground)">{{ teamCreateData.description }}</div>
        <div v-if="teamCreateData.lead" class="mt-1.5 text-[10px] font-mono" style="color: var(--color-muted-foreground)">{{ t('tool.lead') }} {{ teamCreateData.lead }}</div>
        <div v-if="tool.error" class="mt-1 text-xs font-mono" style="color: var(--color-destructive)">{{ tool.error }}</div>
      </div>

      <!-- ═══════ Team Spawn ═══════ -->
      <div v-else-if="renderType === 'team-spawn'" class="px-3 py-2.5" style="background: var(--color-surface)">
        <div class="flex items-center gap-2 mb-1">
          <span class="text-xs font-mono font-semibold" style="color: var(--color-foreground)">@{{ teamSpawnData.name }}</span>
          <span
            v-if="teamSpawnData.agentType"
            class="text-[10px] px-1.5 py-0.5 rounded"
            style="background: var(--color-muted); color: var(--color-muted-foreground)"
          >{{ teamSpawnData.agentType }}</span>
          <span v-if="tool.status === 'done' && !tool.error" class="ml-auto text-[10px] font-semibold" style="color: var(--color-primary)">{{ t('tool.runningMark') }}</span>
          <span v-else-if="tool.status === 'running'" class="ml-auto text-[10px] animate-pulse" style="color: var(--color-muted-foreground)">{{ t('tool.spawning') }}</span>
        </div>
        <div
          v-if="teamSpawnData.prompt"
          class="text-[11px] leading-snug"
          style="color: var(--color-muted-foreground); font-style: italic; white-space: pre-wrap"
        >{{ truncate(teamSpawnData.prompt, 150) }}</div>
        <div v-if="teamSpawnData.id" class="mt-1.5 text-[10px] font-mono" style="color: var(--color-muted-foreground)">{{ t('tool.id') }} {{ teamSpawnData.id }}</div>
        <div v-if="tool.error" class="mt-1 text-xs font-mono" style="color: var(--color-destructive)">{{ tool.error }}</div>
      </div>

      <!-- ═══════ Team Message ═══════ -->
      <div v-else-if="renderType === 'team-message'" class="px-3 py-2.5" style="background: var(--color-surface)">
        <!-- Header: recipient + sent status -->
        <div class="flex items-center gap-2 mb-1.5">
          <span class="text-[10px]" style="color: var(--color-muted-foreground)">→</span>
          <span class="text-xs font-mono font-semibold" style="color: var(--color-foreground)">
            {{ teamMsgData.to === '*' ? t('tool.all') : '@' + teamMsgData.to }}
          </span>
          <span
            v-if="tool.status === 'done' && !tool.error"
            class="ml-auto text-[10px] font-semibold"
            style="color: var(--color-primary)"
          >{{ t('tool.sent') }}</span>
          <span
            v-else-if="tool.status === 'running'"
            class="ml-auto text-[10px] animate-pulse"
            style="color: var(--color-muted-foreground)"
          >{{ t('tool.sending') }}</span>
        </div>
        <!-- Summary -->
        <div v-if="teamMsgData.summary" class="text-[11px] leading-snug font-medium mb-1" style="color: var(--color-foreground)">
          {{ teamMsgData.summary }}
        </div>
        <!-- Message body (truncated) -->
        <div
          v-if="teamMsgData.message"
          class="text-[11px] leading-snug"
          style="color: var(--color-muted-foreground); font-style: italic; white-space: pre-wrap"
        >{{ truncate(teamMsgData.message, 200) }}</div>
        <div v-if="tool.error" class="mt-1.5 text-xs font-mono" style="color: var(--color-destructive)">{{ tool.error }}</div>
      </div>

      <!-- ═══════ Generic fallback ═══════ -->
      <div v-else class="ml-3 pl-3 border-l-2 text-xs font-mono py-2 max-h-64 overflow-y-auto"
        :style="'border-color: ' + (tool.status === 'error' ? 'var(--color-destructive)' : 'var(--color-border)')"
      >
        <div class="mb-1.5">
          <span class="text-[10px] uppercase tracking-wider" style="color: var(--color-muted-foreground)">{{ t('tool.args') }}</span>
          <div class="mt-0.5" style="color: var(--color-muted-foreground)">{{ formatArgs(tool.args) }}</div>
        </div>
        <div v-if="tool.output" class="mt-2">
          <span class="text-[10px] uppercase tracking-wider" style="color: var(--color-muted-foreground)">{{ t('tool.output') }}</span>
          <div class="whitespace-pre-wrap mt-0.5" style="color: var(--color-muted-foreground)">{{ truncate(tool.output, 500) }}</div>
        </div>
        <div v-if="tool.error" class="mt-2">
          <span class="text-[10px] uppercase tracking-wider" style="color: var(--color-destructive)">{{ t('tool.error') }}</span>
          <div class="whitespace-pre-wrap mt-0.5" style="color: var(--color-destructive)">{{ tool.error }}</div>
        </div>
      </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
@keyframes shimmer-sweep {
  0% { background-position: -200% center; }
  100% { background-position: 200% center; }
}
.shimmer-running {
  background: linear-gradient(
    90deg,
    var(--color-muted-foreground) 20%,
    var(--color-foreground) 50%,
    var(--color-muted-foreground) 80%
  );
  background-size: 200% auto;
  -webkit-background-clip: text;
  background-clip: text;
  -webkit-text-fill-color: transparent;
  animation: shimmer-sweep 1.8s linear infinite;
}
</style>
