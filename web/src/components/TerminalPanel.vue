<script setup lang="ts">
import { ref, computed } from 'vue'
import TerminalInstance from './TerminalInstance.vue'

const emit = defineEmits<{
  close: []
}>()

interface Tab {
  id: string
  label: string
}

let nextId = 1
function makeTab(): Tab {
  return { id: String(nextId++), label: `Shell ${nextId - 1}` }
}

const tabs = ref<Tab[]>([makeTab()])
const activeId = ref(tabs.value[0].id)

const activeIndex = computed(() => tabs.value.findIndex(t => t.id === activeId.value))

function addTab() {
  const tab = makeTab()
  tabs.value.push(tab)
  activeId.value = tab.id
}

function closeTab(id: string) {
  const idx = tabs.value.findIndex(t => t.id === id)
  if (idx === -1) return
  tabs.value.splice(idx, 1)
  if (tabs.value.length === 0) {
    emit('close')
    return
  }
  // Select adjacent tab
  const newIdx = Math.min(idx, tabs.value.length - 1)
  activeId.value = tabs.value[newIdx].id
}
</script>

<template>
  <div class="flex flex-col h-full" style="background-color: var(--color-muted)">
    <!-- Tab bar -->
    <div
      class="flex items-center shrink-0 border-b"
      style="border-color: var(--color-border); background-color: var(--color-surface); height: 36px"
    >
      <!-- Tabs -->
      <div class="flex items-stretch h-full overflow-x-auto flex-1 min-w-0">
        <button
          v-for="tab in tabs"
          :key="tab.id"
          class="tab-btn"
          :class="{ 'tab-active': tab.id === activeId }"
          @click="activeId = tab.id"
        >
          <span class="tab-label">{{ tab.label }}</span>
          <span
            v-if="tabs.length > 1"
            class="tab-close"
            @click.stop="closeTab(tab.id)"
          >
            <svg class="w-2.5 h-2.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round">
              <path d="M18 6L6 18M6 6l12 12" />
            </svg>
          </span>
        </button>
      </div>

      <!-- Controls -->
      <div class="flex items-center gap-1 px-2 shrink-0">
        <button
          class="ctrl-btn"
          title="New terminal"
          @click="addTab"
        >
          <svg class="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
            <path d="M12 5v14M5 12h14" />
          </svg>
        </button>
        <button
          class="ctrl-btn"
          title="Close panel"
          @click="emit('close')"
        >
          <svg class="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
            <path d="M18 6L6 18M6 6l12 12" />
          </svg>
        </button>
      </div>
    </div>

    <!-- Terminal area (all instances stacked, only active is visible) -->
    <div class="flex-1 min-h-0 relative px-1 py-1">
      <TerminalInstance
        v-for="tab in tabs"
        :key="tab.id"
        :active="tab.id === activeId"
      />
    </div>
  </div>
</template>

<style scoped>
.tab-btn {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  padding: 0 10px;
  height: 100%;
  font-size: 11px;
  font-weight: 500;
  border: none;
  background: transparent;
  cursor: pointer;
  color: var(--color-muted-foreground);
  border-right: 1px solid var(--color-border);
  white-space: nowrap;
  transition: background 0.1s, color 0.1s;
  flex-shrink: 0;
}
.tab-btn:hover {
  background: var(--color-muted);
  color: var(--color-foreground);
}
.tab-active {
  background: var(--color-muted);
  color: var(--color-foreground);
  border-bottom: 2px solid var(--color-primary);
}
.tab-label {
  pointer-events: none;
}
.tab-close {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 14px;
  height: 14px;
  border-radius: 3px;
  opacity: 0.5;
  transition: opacity 0.1s, background 0.1s;
}
.tab-close:hover {
  opacity: 1;
  background: color-mix(in srgb, var(--color-foreground) 15%, transparent);
}
.ctrl-btn {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 22px;
  height: 22px;
  border: none;
  background: transparent;
  border-radius: 4px;
  cursor: pointer;
  color: var(--color-muted-foreground);
  transition: background 0.1s, color 0.1s;
}
.ctrl-btn:hover {
  background: var(--color-muted);
  color: var(--color-foreground);
}
</style>
