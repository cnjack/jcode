<script setup lang="ts">
import { ref, onMounted, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { api } from '@/composables/api'
import type { DiffEntry } from '@/types/api'

const { t } = useI18n()
const entries = ref<DiffEntry[]>([])
const mode = ref<'working' | 'staged' | 'branch' | 'session'>('session')
const loading = ref(false)
const selectedFile = ref<string | null>(null)

const modes = computed(() => [
  { value: 'session' as const, label: t('diff.modes.session') },
  { value: 'working' as const, label: t('diff.modes.working') },
  { value: 'staged' as const, label: t('diff.modes.staged') },
  { value: 'branch' as const, label: t('diff.modes.branch') },
])

async function fetchDiff() {
  loading.value = true
  try {
    const result = await api.diff(mode.value)
    entries.value = result.entries
    // Keep the current selection only if it still exists in the new entry set;
    // otherwise fall back to the first entry. Without this, switching mode to one
    // that doesn't contain the previously-selected file left the body blank
    // ("Select a file…") even though the file list had content.
    const stillThere = selectedFile.value && entries.value.some((e) => e.file === selectedFile.value)
    if (!stillThere) {
      selectedFile.value = entries.value.length > 0 ? (entries.value[0]?.file ?? null) : null
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

function statusBadge(status: string): { label: string; style: Record<string, string> } {
  switch (status) {
    case 'A': return { label: 'A', style: { background: 'var(--color-success-bg)', color: 'var(--color-success-fg)' } }
    case 'D': return { label: 'D', style: { background: 'var(--color-error-bg)', color: 'var(--color-error-fg)' } }
    default: return { label: 'M', style: { background: 'var(--color-warning-bg)', color: 'var(--color-warning-fg)' } }
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
  <div class="flex flex-col h-full" style="background: var(--color-background)">
    <!-- Header -->
    <div class="flex items-center justify-between px-3 py-1.5" style="border-bottom: 1px solid var(--color-border); background: var(--color-sidebar-bg)">
      <div class="flex items-center gap-2">
        <span class="text-[11px] font-semibold uppercase tracking-wider" style="color: var(--color-muted-foreground)">{{ t('diff.changes') }}</span>
        <div class="flex gap-0.5">
          <button
            v-for="m in modes"
            :key="m.value"
            class="dv-mode px-1.5 py-0.5 text-[10px] rounded cursor-pointer transition-colors font-medium"
            :class="{ active: mode === m.value }"
            @click="mode = m.value; fetchDiff()"
          >
            {{ m.label }}
          </button>
        </div>
      </div>
      <div class="flex items-center gap-2">
        <span v-if="totalChanges.additions || totalChanges.deletions" class="text-[10px] font-mono">
          <span style="color: var(--color-success-fg)">+{{ totalChanges.additions }}</span>
          <span class="mx-0.5" style="color: var(--color-border)">/</span>
          <span style="color: var(--color-error-fg)">-{{ totalChanges.deletions }}</span>
        </span>
        <button
          class="dv-mute text-[10px] cursor-pointer transition-colors font-medium"
          @click="fetchDiff"
        >
          {{ t('diff.refresh') }}
        </button>
      </div>
    </div>

    <div class="flex flex-col flex-1 min-h-0">
      <!-- File list -->
      <div class="overflow-y-auto shrink-0 max-h-[30%]" style="border-bottom: 1px solid var(--color-border)">
        <div v-if="entries.length === 0 && !loading" class="text-center text-[11px] py-6" style="color: var(--color-muted-foreground)">
          {{ t('diff.noChanges') }}
        </div>
        <div v-if="loading" class="text-center text-[11px] py-6 animate-pulse" style="color: var(--color-muted-foreground)">
          Loading...
        </div>
        <button
          v-for="entry in entries"
          :key="entry.file"
          class="dv-file w-full flex items-center gap-1.5 px-2 py-1.5 text-left cursor-pointer transition-colors"
          :class="{ active: selectedFile === entry.file }"
          @click="selectedFile = entry.file"
        >
          <span
            class="text-[9px] font-bold rounded px-1 py-px shrink-0"
            :style="statusBadge(entry.status).style"
          >
            {{ statusBadge(entry.status).label }}
          </span>
          <span class="text-[11px] font-mono truncate">{{ entry.file.split('/').pop() }}</span>
          <span class="text-[9px] font-mono ml-auto shrink-0">
            <span style="color: var(--color-success-fg)">+{{ entry.additions }}</span>
            <span class="ml-0.5" style="color: var(--color-error-fg)">-{{ entry.deletions }}</span>
          </span>
        </button>
      </div>

      <!-- Diff content -->
      <div class="flex-1 overflow-auto">
        <div v-if="!selectedEntry" class="text-center text-[11px] py-8" style="color: var(--color-muted-foreground)">
          {{ t('diff.selectFile') }}
        </div>
        <div v-else>
          <div class="px-3 py-1.5" style="border-bottom: 1px solid var(--color-border); background: var(--color-muted)">
            <span class="text-[11px] font-mono" style="color: var(--color-foreground)">{{ selectedEntry.file }}</span>
            <span class="text-[10px] font-mono ml-2">
              <span style="color: var(--color-success-fg)">+{{ selectedEntry.additions }}</span>
              <span class="mx-0.5" style="color: var(--color-border)">/</span>
              <span style="color: var(--color-error-fg)">-{{ selectedEntry.deletions }}</span>
            </span>
          </div>
          <div class="font-mono text-[11px] leading-5">
            <div
              v-for="(line, i) in parsePatchLines(selectedEntry.patch)"
              :key="i"
              class="dv-line px-3 border-l-2"
              :class="`dv-${line.type}`"
            >
              <pre class="whitespace-pre-wrap">{{ line.text }}</pre>
            </div>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.dv-mode {
  color: var(--color-muted-foreground);
}
.dv-mode:hover {
  color: var(--color-foreground);
  background: var(--color-muted);
}
.dv-mode.active {
  background: var(--accent-wash);
  color: var(--color-primary);
}

.dv-mute {
  color: var(--color-muted-foreground);
}
.dv-mute:hover {
  color: var(--color-foreground);
}

.dv-file {
  color: var(--color-muted-foreground);
}
.dv-file:hover {
  background: var(--color-muted);
}
.dv-file.active {
  background: var(--accent-wash-soft);
  color: var(--color-foreground);
}

.dv-line {
  border-color: transparent;
}
.dv-add {
  background: var(--color-success-bg);
  border-color: var(--color-success-fg);
  color: var(--color-success-fg);
}
.dv-del {
  background: var(--color-error-bg);
  border-color: var(--color-error-fg);
  color: var(--color-error-fg);
}
.dv-hunk {
  background: var(--color-info-bg);
  border-color: var(--color-info-fg);
  color: var(--color-info-fg);
}
.dv-ctx {
  color: var(--color-muted-foreground);
}
</style>
