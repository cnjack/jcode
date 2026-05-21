<script setup lang="ts">
import { computed } from 'vue'
import { Menu, SquareTerminal, FileDiff, FolderOpen, PanelRight, Search } from 'lucide-vue-next'
import { useChatStore } from '@/stores/chat'

type PanelType = 'terminal' | 'diff' | 'files' | 'right-panel'

const store = useChatStore()

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

const statusColor = computed(() => {
  if (props.isRunning) return '#f59e0b'
  if (props.wsConnected) return '#22c55e'
  return '#9ca3af'
})

const sessionTitle = computed(() => {
  const session = store.sessions.find(s => s.uuid === store.currentSessionId)
  return session?.title || 'New Chat'
})

const sessionSubtitle = computed(() => {
  const session = store.sessions.find(s => s.uuid === store.currentSessionId)
  if (!session) return store.modelName || ''
  const model = session.model || store.modelName || ''
  const d = new Date(session.created_at)
  const time = d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
  return `${model} · ${time}`
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
      <div class="session-info">
        <span class="session-title">{{ sessionTitle }}</span>
        <span class="session-subtitle">{{ sessionSubtitle }}</span>
      </div>
    </div>

    <div class="topbar-center">
      <div class="search-pill">
        <Search :size="12" />
        <span>Search</span>
      </div>
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
          <component :is="btn.icon" :size="18" />
        </button>
      </div>

      <div class="status-indicator">
        <span class="status-dot" :style="{ backgroundColor: statusColor }" />
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
  gap: 10px;
  min-width: 0;
  flex: 1;
}

.topbar-center {
  display: flex;
  align-items: center;
  justify-content: center;
}

.topbar-right {
  display: flex;
  align-items: center;
  gap: 8px;
  flex: 1;
  justify-content: flex-end;
}

.icon-btn {
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 6px;
  border: none;
  background: transparent;
  border-radius: 6px;
  color: var(--color-muted-foreground);
  cursor: pointer;
  transition: background 0.15s, color 0.15s;
}

.icon-btn:hover {
  color: var(--color-foreground);
}

.icon-btn.active {
  color: var(--color-foreground);
  background: var(--color-muted);
}

.session-info {
  display: flex;
  flex-direction: column;
  min-width: 0;
  gap: 1px;
}

.session-title {
  font-size: 13px;
  font-weight: 600;
  color: var(--color-foreground);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  max-width: 220px;
  line-height: 1.3;
}

.session-subtitle {
  font-size: 11px;
  color: var(--color-muted-foreground);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  max-width: 220px;
  line-height: 1.3;
}

.search-pill {
  display: flex;
  align-items: center;
  gap: 5px;
  padding: 5px 14px;
  border-radius: 9999px;
  border: 1px solid var(--color-border);
  font-size: 12px;
  color: var(--color-muted-foreground);
  cursor: pointer;
  transition: border-color 0.15s;
  white-space: nowrap;
}

.search-pill:hover {
  border-color: var(--color-foreground);
}

.panel-buttons {
  display: flex;
  align-items: center;
  gap: 2px;
}

.status-indicator {
  display: flex;
  align-items: center;
  padding-left: 8px;
}

.status-dot {
  width: 8px;
  height: 8px;
  border-radius: 9999px;
  flex-shrink: 0;
}
</style>
