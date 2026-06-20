<script setup lang="ts">
import { ref } from 'vue'
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

const initialTab = makeTab()
const tabs = ref<Tab[]>([initialTab])
const activeId = ref(initialTab.id)

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
  const nextTab = tabs.value[newIdx]
  if (nextTab) activeId.value = nextTab.id
}
</script>

<template>
  <div class="flex flex-col h-full" style="background: var(--color-background)">
    <!-- Tab bar -->
    <div
      class="flex items-center shrink-0 px-2 gap-1"
      style="height: 32px; border-bottom: 1px solid var(--color-border); background: var(--color-background)"
    >
      <!-- Tabs -->
      <div class="flex items-center gap-0.5 h-full flex-1 min-w-0 overflow-x-auto">
        <button
          v-for="tab in tabs"
          :key="tab.id"
          class="tab-btn"
          :class="{ 'tab-active': tab.id === activeId }"
          @click="activeId = tab.id"
        >
          <!-- × on the left of the label, appears on hover -->
          <button
            class="tab-close"
            :aria-label="`Close ${tab.label}`"
            tabindex="-1"
            @click.stop="closeTab(tab.id)"
            @keydown.enter.stop.prevent="closeTab(tab.id)"
            @keydown.space.stop.prevent="closeTab(tab.id)"
          >
            <svg class="w-2.5 h-2.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round">
              <path d="M18 6L6 18M6 6l12 12" />
            </svg>
          </button>
          <span class="tab-label">{{ tab.label }}</span>
        </button>

        <!-- + New terminal after the last tab -->
        <button class="ctrl-btn" title="New terminal" @click="addTab">
          <svg class="w-3 h-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round">
            <path d="M12 5v14M5 12h14" />
          </svg>
        </button>
      </div>

      <!-- Controls: close panel only -->
      <div class="flex items-center gap-0.5 shrink-0">
        <button class="ctrl-btn" title="Close panel" @click="emit('close')">
          <svg class="w-3 h-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
            <path d="M18 6L6 18M6 6l12 12" />
          </svg>
        </button>
      </div>
    </div>

    <!-- Terminal area -->
    <div class="flex-1 min-h-0 relative" style="background: var(--color-background)">
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
  padding: 0 8px;
  height: 22px;
  font-size: 11px;
  font-family: var(--font-mono);
  font-weight: 500;
  border: none;
  border-radius: var(--radius-sm);
  background: transparent;
  cursor: pointer;
  color: var(--color-muted-foreground);
  white-space: nowrap;
  transition: background 0.1s, color 0.1s;
  flex-shrink: 0;
}
.tab-btn:hover {
  background: color-mix(in srgb, var(--color-foreground) 8%, transparent);
  color: var(--color-foreground);
}
.tab-active {
  background: color-mix(in srgb, var(--color-foreground) 10%, transparent);
  color: var(--color-foreground);
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
  padding: 0;
  border: none;
  border-radius: var(--radius-xs);
  background: transparent;
  color: inherit;
  cursor: pointer;
  flex-shrink: 0;
  opacity: 0;
  pointer-events: none;
  transition: opacity 0.1s, background 0.1s;
}
/* Show × when hovering the tab or for the active tab */
.tab-btn:hover .tab-close {
  opacity: 0.6;
  pointer-events: auto;
}
.tab-btn:hover .tab-close:hover {
  opacity: 1;
  background: color-mix(in srgb, var(--color-foreground) 15%, transparent);
}
.tab-active .tab-close {
  opacity: 0.4;
  pointer-events: auto;
}
.tab-active:hover .tab-close {
  opacity: 0.7;
}
.ctrl-btn {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 20px;
  height: 20px;
  border: none;
  background: transparent;
  border-radius: var(--radius-sm);
  cursor: pointer;
  color: var(--color-muted-foreground);
  transition: background 0.1s, color 0.1s;
}
.ctrl-btn:hover {
  background: color-mix(in srgb, var(--color-foreground) 8%, transparent);
  color: var(--color-foreground);
}
</style>
