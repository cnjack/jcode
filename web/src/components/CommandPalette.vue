<script setup lang="ts">
import { ref, computed, watch, nextTick } from 'vue'
import { Dialog, DialogPanel, TransitionRoot, TransitionChild } from '@headlessui/vue'
import { Plus, Settings, FolderOpen, SunMoon, MessageSquare, CornerDownLeft } from 'lucide-vue-next'
import { useChatStore } from '@/stores/chat'
import { useProjectStore } from '@/stores/project'
import type { TaskItem } from '@/types/api'

const props = defineProps<{ open: boolean }>()
const emit = defineEmits<{
  close: []
  action: [name: 'settings' | 'projects' | 'theme']
}>()

const store = useChatStore()
const projectStore = useProjectStore()

const query = ref('')
const selectedIdx = ref(0)
const inputEl = ref<HTMLInputElement | null>(null)

interface PaletteItem {
  id: string
  group: string
  label: string
  hint?: string
  icon: unknown
  run: () => void | Promise<void>
}

async function openTask(task: TaskItem) {
  emit('close')
  if (task.unread) projectStore.updateTaskMeta(task.uuid, { unread: false })
  const cur = projectStore.activeProject?.path || store.pwd
  if (cur !== task.project) {
    const ok = await projectStore.openProject(task.project)
    if (!ok) return
    await store.fetchHealth()
  }
  await store.loadSession(task.uuid)
}

const actions = computed<PaletteItem[]>(() => [
  { id: 'a-new', group: 'Actions', label: 'New task', icon: Plus, run: () => { emit('close'); store.newSession() } },
  { id: 'a-proj', group: 'Actions', label: 'Open project…', icon: FolderOpen, run: () => { emit('close'); emit('action', 'projects') } },
  { id: 'a-settings', group: 'Actions', label: 'Open settings', icon: Settings, run: () => { emit('close'); emit('action', 'settings') } },
  { id: 'a-theme', group: 'Actions', label: 'Toggle theme', icon: SunMoon, run: () => { emit('action', 'theme') } },
])

const taskItems = computed<PaletteItem[]>(() =>
  projectStore.allTasks
    .filter((t) => !t.archived)
    .map((t) => ({
      id: 't-' + t.uuid,
      group: 'Tasks',
      label: t.title || t.uuid.slice(0, 8) + '…',
      hint: projectStore.nameForPath(t.project),
      icon: MessageSquare,
      run: () => openTask(t),
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
              <svg class="cp-search-icon" viewBox="0 0 20 20" fill="none" stroke="currentColor" stroke-width="1.7">
                <circle cx="9" cy="9" r="6" /><path d="M14 14l3.5 3.5" stroke-linecap="round" />
              </svg>
              <input
                ref="inputEl"
                v-model="query"
                class="cp-input"
                placeholder="Search tasks or run a command…"
                @keydown="onKeydown"
              />
              <kbd class="cp-esc">Esc</kbd>
            </div>

            <div class="cp-results">
              <div v-if="results.length === 0" class="cp-empty">No results</div>
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
                  <component :is="item.icon" :size="15" class="cp-item-icon" />
                  <span class="cp-item-label">{{ item.label }}</span>
                  <span v-if="item.hint" class="cp-item-hint">{{ item.hint }}</span>
                  <CornerDownLeft v-if="flatIndex(item) === selectedIdx" :size="13" class="cp-enter" />
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
  color: var(--color-muted-foreground);
  flex-shrink: 0;
}
</style>
