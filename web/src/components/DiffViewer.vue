<script setup lang="ts">
import { ref, onMounted, computed } from 'vue'
import { api } from '@/composables/api'
import type { DiffEntry } from '@/types/api'

const entries = ref<DiffEntry[]>([])
const mode = ref<'working' | 'staged' | 'branch'>('working')
const loading = ref(false)
const selectedFile = ref<string | null>(null)

const modes = [
  { value: 'working' as const, label: 'Working' },
  { value: 'staged' as const, label: 'Staged' },
  { value: 'branch' as const, label: 'Branch' },
]

async function fetchDiff() {
  loading.value = true
  try {
    const result = await api.diff(mode.value)
    entries.value = result.entries
    if (entries.value.length > 0 && !selectedFile.value) {
      selectedFile.value = entries.value[0].file
    }
  } catch (err) {
    console.error('Failed to fetch diff:', err)
    entries.value = []
  } finally {
    loading.value = false
  }
}

const selectedEntry = computed(() =>
  entries.value.find((e) => e.file === selectedFile.value) || null,
)

const totalChanges = computed(() => ({
  additions: entries.value.reduce((sum, e) => sum + e.additions, 0),
  deletions: entries.value.reduce((sum, e) => sum + e.deletions, 0),
}))

function statusBadge(status: string) {
  switch (status) {
    case 'A': return { label: 'A', cls: 'bg-emerald-100 text-emerald-700' }
    case 'D': return { label: 'D', cls: 'bg-red-100 text-red-700' }
    default: return { label: 'M', cls: 'bg-amber-100 text-amber-700' }
  }
}

function parsePatchLines(patch: string) {
  const lines = patch.split('\n')
  return lines.map((line) => {
    if (line.startsWith('+') && !line.startsWith('+++')) {
      return { text: line, type: 'add' as const }
    }
    if (line.startsWith('-') && !line.startsWith('---')) {
      return { text: line, type: 'del' as const }
    }
    if (line.startsWith('@@')) {
      return { text: line, type: 'hunk' as const }
    }
    return { text: line, type: 'ctx' as const }
  })
}

onMounted(fetchDiff)
</script>

<template>
  <div class="flex flex-col h-full bg-stone-50">
    <!-- Header -->
    <div class="flex items-center justify-between px-3 py-1.5 border-b border-stone-200 bg-stone-100/80">
      <div class="flex items-center gap-2">
        <span class="text-[11px] font-medium text-stone-500 uppercase tracking-wider">Changes</span>
        <div class="flex gap-0.5">
          <button
            v-for="m in modes"
            :key="m.value"
            class="px-1.5 py-0.5 text-[10px] rounded cursor-pointer transition-colors"
            :class="mode === m.value
              ? 'bg-teal-100 text-teal-700'
              : 'text-stone-400 hover:text-stone-600 hover:bg-stone-200'"
            @click="mode = m.value; fetchDiff()"
          >
            {{ m.label }}
          </button>
        </div>
      </div>
      <div class="flex items-center gap-2">
        <span v-if="totalChanges.additions || totalChanges.deletions" class="text-[10px] font-mono">
          <span class="text-emerald-600">+{{ totalChanges.additions }}</span>
          <span class="text-stone-300 mx-0.5">/</span>
          <span class="text-red-500">-{{ totalChanges.deletions }}</span>
        </span>
        <button
          class="text-[10px] text-stone-400 hover:text-stone-600 cursor-pointer transition-colors"
          @click="fetchDiff"
        >
          ↻ Refresh
        </button>
      </div>
    </div>

    <div class="flex flex-1 min-h-0">
      <!-- File list -->
      <div class="w-48 border-r border-stone-200 overflow-y-auto shrink-0">
        <div v-if="entries.length === 0 && !loading" class="text-center text-[11px] text-stone-400 py-6">
          No changes
        </div>
        <div v-if="loading" class="text-center text-[11px] text-stone-400 py-6 animate-pulse">
          Loading...
        </div>
        <button
          v-for="entry in entries"
          :key="entry.file"
          class="w-full flex items-center gap-1.5 px-2 py-1.5 text-left cursor-pointer transition-colors"
          :class="selectedFile === entry.file ? 'bg-teal-50 text-stone-700' : 'text-stone-500 hover:bg-stone-100'"
          @click="selectedFile = entry.file"
        >
          <span
            class="text-[9px] font-bold rounded px-1 py-px shrink-0"
            :class="statusBadge(entry.status).cls"
          >
            {{ statusBadge(entry.status).label }}
          </span>
          <span class="text-[11px] font-mono truncate">{{ entry.file.split('/').pop() }}</span>
        </button>
      </div>

      <!-- Diff content -->
      <div class="flex-1 overflow-auto">
        <div v-if="!selectedEntry" class="text-center text-[11px] text-stone-400 py-8">
          Select a file to view changes
        </div>
        <div v-else>
          <div class="px-3 py-1.5 border-b border-stone-200 bg-stone-100/50">
            <span class="text-[11px] font-mono text-stone-600">{{ selectedEntry.file }}</span>
            <span class="text-[10px] font-mono ml-2">
              <span class="text-emerald-600">+{{ selectedEntry.additions }}</span>
              <span class="text-stone-300 mx-0.5">/</span>
              <span class="text-red-500">-{{ selectedEntry.deletions }}</span>
            </span>
          </div>
          <div class="font-mono text-[11px] leading-5">
            <div
              v-for="(line, i) in parsePatchLines(selectedEntry.patch)"
              :key="i"
              class="px-3 border-l-2"
              :class="{
                'bg-emerald-50 border-emerald-400 text-emerald-800': line.type === 'add',
                'bg-red-50 border-red-400 text-red-800': line.type === 'del',
                'bg-blue-50 border-blue-300 text-blue-600': line.type === 'hunk',
                'border-transparent text-stone-500': line.type === 'ctx',
              }"
            >
              <pre class="whitespace-pre-wrap">{{ line.text }}</pre>
            </div>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>
