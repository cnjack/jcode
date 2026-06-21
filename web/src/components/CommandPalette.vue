<script setup lang="ts">
import { ref, computed, watch, nextTick, inject } from 'vue'
import { Dialog, DialogPanel, TransitionRoot, TransitionChild } from '@headlessui/vue'
import { PlusIcon, Cog6ToothIcon, FolderOpenIcon, SunIcon, ChatBubbleLeftIcon, MagnifyingGlassIcon } from '@heroicons/vue/24/outline'
import { useI18n } from 'vue-i18n'
import { useChatStore } from '@/stores/chat'
import { useProjectStore, isRemotePath, parseRemoteLabel } from '@/stores/project'
import type { TaskItem, RemoteMeta } from '@/types/api'

const props = defineProps<{ open: boolean }>()
const emit = defineEmits<{
  close: []
  action: [name: 'settings' | 'projects' | 'theme']
}>()

const store = useChatStore()
const projectStore = useProjectStore()
const { t } = useI18n()
const openRemoteConnect = inject<(prefill?: RemoteMeta & { loadTaskUuid?: string }) => void>('openRemoteConnect')

const query = ref('')
const selectedIdx = ref(0)
const inputEl = ref<HTMLInputElement | null>(null)
const resultsEl = ref<HTMLElement | null>(null)

interface PaletteItem {
  id: string
  group: string
  label: string
  hint?: string
  icon: unknown
  run: () => void | Promise<void>
}

let opening = false
async function openTask(task: TaskItem) {
  // Guard against a double-trigger (held Enter / rapid clicks) interleaving two
  // project switches + session loads.
  if (opening) return
  opening = true
  emit('close')
  try {
    if (task.unread) projectStore.updateTaskMeta(task.uuid, { unread: false })
    const cur = projectStore.activeProject?.path || store.pwd
    // Remote tasks must reconnect through the SSH wizard (mirrors the sidebar) —
    // a local path switch to an ssh:// label would fail.
    if (isRemotePath(task.project)) {
      if (cur === task.project) {
        await store.loadSession(task.uuid)
      } else {
        const meta = parseRemoteLabel(task.project)
        if (meta) openRemoteConnect?.({ ...meta, loadTaskUuid: task.uuid })
      }
      return
    }
    if (cur !== task.project) {
      const ok = await projectStore.openProject(task.project)
      if (!ok) return
      await store.fetchHealth()
    }
    await store.loadSession(task.uuid)
  } finally {
    opening = false
  }
}

const actions = computed<PaletteItem[]>(() => [
  { id: 'a-new', group: t('commandPalette.groups.actions'), label: t('commandPalette.newTask'), icon: PlusIcon, run: () => { emit('close'); store.newSession() } },
  { id: 'a-proj', group: t('commandPalette.groups.actions'), label: t('nav.openProject'), icon: FolderOpenIcon, run: () => { emit('close'); emit('action', 'projects') } },
  { id: 'a-settings', group: t('commandPalette.groups.actions'), label: t('nav.openSettings'), icon: Cog6ToothIcon, run: () => { emit('close'); emit('action', 'settings') } },
  { id: 'a-theme', group: t('commandPalette.groups.actions'), label: t('nav.toggleTheme'), icon: SunIcon, run: () => { emit('close'); emit('action', 'theme') } },
])

const taskItems = computed<PaletteItem[]>(() =>
  projectStore.allTasks
    .filter((task) => !task.archived)
    .map((task) => ({
      id: 't-' + task.uuid,
      group: t('commandPalette.groups.tasks'),
      label: task.title || task.uuid.slice(0, 8) + '…',
      hint: projectStore.nameForPath(task.project),
      icon: ChatBubbleLeftIcon,
      run: () => openTask(task),
    })),
)

const results = computed<PaletteItem[]>(() => {
  const q = query.value.trim().toLowerCase()
  const all = [...actions.value, ...taskItems.value]
  if (!q) return [...actions.value, ...taskItems.value.slice(0, 8)]
  return all.filter((i) => i.label.toLowerCase().includes(q) || (i.hint || '').toLowerCase().includes(q))
})

// Group results in display order while keeping a flat index for keyboard nav.
const groups = computed(() => {
  const order: string[] = []
  const map: Record<string, PaletteItem[]> = {}
  results.value.forEach((item) => {
    if (!map[item.group]) {
      map[item.group] = []
      order.push(item.group)
    }
    map[item.group]!.push(item)
  })
  return order.map((g) => ({ name: g, items: map[g]! }))
})

function flatIndex(item: PaletteItem): number {
  return results.value.findIndex((i) => i.id === item.id)
}

watch(() => props.open, async (o) => {
  if (o) {
    query.value = ''
    selectedIdx.value = 0
    projectStore.fetchAllTasks()
    await nextTick()
    inputEl.value?.focus()
  }
})
watch(results, () => { selectedIdx.value = 0 })

function move(delta: number) {
  const n = results.value.length
  if (n === 0) return
  selectedIdx.value = (selectedIdx.value + delta + n) % n
  // Keep the highlighted row visible — the results list scrolls (max-height),
  // so arrow-key navigation could otherwise move the selection out of view.
  nextTick(() => {
    resultsEl.value?.querySelector('.cp-item.sel')?.scrollIntoView({ block: 'nearest' })
  })
}
function runSelected() {
  results.value[selectedIdx.value]?.run()
}
function onKeydown(e: KeyboardEvent) {
  if (e.key === 'ArrowDown') { e.preventDefault(); move(1) }
  else if (e.key === 'ArrowUp') { e.preventDefault(); move(-1) }
  else if (e.key === 'Enter') { e.preventDefault(); runSelected() }
}
</script>

<template>
  <TransitionRoot :show="open" as="template">
    <Dialog @close="emit('close')" class="relative" style="z-index: var(--z-modal)">
      <TransitionChild
        enter="ease-out duration-150" enter-from="opacity-0" enter-to="opacity-100"
        leave="ease-in duration-100" leave-from="opacity-100" leave-to="opacity-0">
        <div class="fixed inset-0" style="background: var(--backdrop); backdrop-filter: blur(6px); -webkit-backdrop-filter: blur(6px)" />
      </TransitionChild>

      <div class="fixed inset-0 flex items-start justify-center pt-[12vh] px-4">
        <TransitionChild
          enter="ease-out duration-150" enter-from="opacity-0 scale-[0.98] -translate-y-1"
          enter-to="opacity-100 scale-100 translate-y-0"
          leave="ease-in duration-100" leave-from="opacity-100 scale-100"
          leave-to="opacity-0 scale-[0.98] -translate-y-1">
          <DialogPanel class="cp-panel">
            <div class="cp-input-row">
              <MagnifyingGlassIcon class="cp-search-icon" />
              <input
                ref="inputEl"
                v-model="query"
                class="cp-input"
                :placeholder="t('commandPalette.placeholder')"
                @keydown="onKeydown"
              />
              <kbd class="cp-esc">Esc</kbd>
            </div>

            <div ref="resultsEl" class="cp-results">
              <div v-if="results.length === 0" class="cp-empty">{{ t('commandPalette.noResults') }}</div>
              <template v-for="g in groups" :key="g.name">
                <div class="cp-group-label">{{ g.name }}</div>
                <button
                  v-for="item in g.items"
                  :key="item.id"
                  class="cp-item"
                  :class="{ sel: flatIndex(item) === selectedIdx }"
                  @click="item.run()"
                  @mousemove="selectedIdx = flatIndex(item)"
                >
                  <component :is="item.icon" class="w-3.5 h-3.5 cp-item-icon" />
                  <span class="cp-item-label">{{ item.label }}</span>
                  <span v-if="item.hint" class="cp-item-hint">{{ item.hint }}</span>
                  <kbd v-if="flatIndex(item) === selectedIdx" class="cp-enter">↵</kbd>
                </button>
              </template>
            </div>
          </DialogPanel>
        </TransitionChild>
      </div>
    </Dialog>
  </TransitionRoot>
</template>

<style scoped>
.cp-panel {
  width: min(560px, 94vw);
  max-height: 60vh;
  display: flex;
  flex-direction: column;
  overflow: hidden;
  background: var(--color-surface);
  border: 1px solid var(--color-border);
  border-radius: var(--radius-xl);
  box-shadow: var(--shadow-lg);
}
.cp-input-row {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 12px 14px;
  border-bottom: 1px solid var(--color-border);
}
.cp-search-icon {
  width: 16px;
  height: 16px;
  color: var(--color-muted-foreground);
  flex-shrink: 0;
}
.cp-input {
  flex: 1;
  background: transparent;
  border: none;
  outline: none;
  font-size: 14px;
  color: var(--color-foreground);
  font-family: var(--font-sans);
}
.cp-input::placeholder {
  color: var(--color-muted-foreground);
}
.cp-esc {
  font-size: 10px;
  font-family: var(--font-mono);
  padding: 2px 6px;
  border-radius: var(--radius-sm);
  background: var(--color-secondary);
  border: 1px solid var(--color-border);
  color: var(--color-muted-foreground);
}
.cp-results {
  overflow-y: auto;
  padding: 6px;
}
.cp-empty {
  text-align: center;
  font-size: 13px;
  color: var(--color-muted-foreground);
  padding: 24px 0;
}
.cp-group-label {
  font-size: 10px;
  font-weight: 600;
  letter-spacing: 0.06em;
  text-transform: uppercase;
  color: var(--color-muted-foreground);
  padding: 8px 8px 4px;
}
.cp-item {
  display: flex;
  align-items: center;
  gap: 10px;
  width: 100%;
  padding: 8px 10px;
  border: none;
  background: transparent;
  border-radius: var(--radius-md);
  cursor: pointer;
  text-align: left;
}
.cp-item.sel {
  background: var(--color-muted);
}
.cp-item-icon {
  color: var(--color-muted-foreground);
  flex-shrink: 0;
}
.cp-item-label {
  flex: 1;
  min-width: 0;
  font-size: 13px;
  color: var(--color-foreground);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.cp-item-hint {
  font-size: 11px;
  font-family: var(--font-mono);
  color: var(--color-muted-foreground);
  flex-shrink: 0;
}
.cp-enter {
  font-family: var(--font-mono);
  font-size: 11px;
  padding: 1px 5px;
  border-radius: var(--radius-sm);
  background: var(--color-secondary);
  border: 1px solid var(--color-border);
  color: var(--color-muted-foreground);
  flex-shrink: 0;
}
</style>
