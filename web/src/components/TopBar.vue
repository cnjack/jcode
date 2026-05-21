<script setup lang="ts">
import { computed } from 'vue'
import { Menu, SquareTerminal, FileDiff, FolderOpen, PanelRight } from 'lucide-vue-next'

type PanelType = 'terminal' | 'diff' | 'files' | 'right-panel'

const props = defineProps<{
  projectName: string
  pwd: string
  isRunning: boolean
  wsConnected: boolean
  activePanel: 'none' | PanelType
}>()

const emit = defineEmits<{
  'toggle-sidebar': []
  'toggle-panel': [panel: PanelType]
}>()

const status = computed(() => {
  if (props.isRunning) return { color: '#f59e0b', label: 'Working…' }
  if (props.wsConnected) return { color: '#22c55e', label: 'Ready' }
  return { color: '#9ca3af', label: 'Offline' }
})

const panelButtons = [
  { panel: 'terminal' as PanelType, icon: SquareTerminal },
  { panel: 'diff' as PanelType, icon: FileDiff },
  { panel: 'files' as PanelType, icon: FolderOpen },
  { panel: 'right-panel' as PanelType, icon: PanelRight },
]
</script>

<template>
  <header class="topbar">
    <div class="topbar-left">
      <button
        class="icon-btn"
        aria-label="Toggle sidebar"
        @click="emit('toggle-sidebar')"
      >
        <Menu :size="16" />
      </button>
      <nav class="breadcrumb">
        <span class="breadcrumb-project">{{ projectName }}</span>
        <span class="breadcrumb-sep">/</span>
        <span class="breadcrumb-path">{{ pwd }}</span>
      </nav>
    </div>

    <div class="topbar-right">
      <div class="panel-buttons">
        <button
          v-for="btn in panelButtons"
          :key="btn.panel"
          class="icon-btn"
          :class="{ active: activePanel === btn.panel }"
          :aria-label="`Toggle ${btn.panel}`"
          @click="emit('toggle-panel', btn.panel)"
        >
          <component :is="btn.icon" :size="20" />
        </button>
      </div>

      <div class="status-indicator">
        <span class="status-dot" :style="{ backgroundColor: status.color }" />
        <span class="status-label">{{ status.label }}</span>
      </div>
    </div>
  </header>
</template>

<style scoped>
.topbar {
  height: 52px;
  background: var(--color-sidebar-bg);
  border-bottom: 1px solid var(--color-border);
  padding: 0 14px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  font-family: var(--font-sans);
}

.topbar-left {
  display: flex;
  align-items: center;
  gap: 12px;
  min-width: 0;
}

.topbar-right {
  display: flex;
  align-items: center;
  gap: 12px;
}

.icon-btn {
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 6px;
  border: none;
  background: transparent;
  border-radius: 6px;
  color: var(--color-foreground);
  cursor: pointer;
  transition: background 0.15s, color 0.15s;
}

.icon-btn:hover {
  color: var(--color-primary);
}

.icon-btn.active {
  background: var(--color-muted);
  border-radius: 9999px;
}

.breadcrumb {
  display: flex;
  align-items: center;
  gap: 4px;
  min-width: 0;
}

.breadcrumb-project {
  font-size: 14px;
  font-weight: 600;
  color: var(--color-foreground);
  white-space: nowrap;
}

.breadcrumb-sep {
  font-size: 14px;
  color: var(--color-muted-foreground);
}

.breadcrumb-path {
  font-size: 12px;
  font-family: var(--font-mono);
  color: var(--color-muted-foreground);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  max-width: 15rem;
}

.panel-buttons {
  display: flex;
  align-items: center;
  gap: 4px;
}

.status-indicator {
  display: flex;
  align-items: center;
  gap: 6px;
  padding-left: 8px;
}

.status-dot {
  width: 6px;
  height: 6px;
  border-radius: 9999px;
  flex-shrink: 0;
}

.status-label {
  font-size: 12px;
  color: var(--color-muted-foreground);
  white-space: nowrap;
}
</style>
