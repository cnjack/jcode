<script setup lang="ts">
import { ref, computed } from 'vue'
import { XMarkIcon } from '@heroicons/vue/24/outline'
import { useI18n } from 'vue-i18n'
import FileTreePanel from './FileTreePanel.vue'
import DiffViewer from './DiffViewer.vue'
import TaskList from './TaskList.vue'
import { useChatStore } from '@/stores/chat'

defineProps<{
  activeTab: 'files' | 'changes' | 'plan'
}>()

const emit = defineEmits<{
  close: []
  'switch-tab': [tab: 'files' | 'changes' | 'plan']
}>()

const store = useChatStore()
const { t } = useI18n()
const total = computed(() => store.todos.length)
const completed = computed(() => store.todos.filter((t) => t.status === 'completed').length)
const progressPct = computed(() => (total.value ? Math.round((completed.value / total.value) * 100) : 0))

const panelWidth = ref(320)

function startResize(e: MouseEvent) {
  e.preventDefault()
  const startX = e.clientX
  const startWidth = panelWidth.value

  function onMove(e: MouseEvent) {
    const dx = startX - e.clientX
    panelWidth.value = Math.min(600, Math.max(220, startWidth + dx))
  }
  function onUp() {
    window.removeEventListener('mousemove', onMove)
    window.removeEventListener('mouseup', onUp)
  }
  window.addEventListener('mousemove', onMove)
  window.addEventListener('mouseup', onUp)
}
</script>

<template>
  <aside class="right-panel" :style="{ width: panelWidth + 'px' }">
    <!-- Resize handle -->
    <div class="resize-handle" @mousedown="startResize" />
    <div class="panel-header">
      <div class="panel-tabs">
        <button
          class="tab-btn"
          :class="{ active: activeTab === 'plan' }"
          @click="emit('switch-tab', 'plan')"
        >
          {{ t('rightPanel.plan') }}
        </button>
        <button
          class="tab-btn"
          :class="{ active: activeTab === 'files' }"
          @click="emit('switch-tab', 'files')"
        >
          {{ t('rightPanel.files') }}
        </button>
        <button
          class="tab-btn"
          :class="{ active: activeTab === 'changes' }"
          @click="emit('switch-tab', 'changes')"
        >
          {{ t('rightPanel.changes') }}
        </button>
      </div>
      <button class="close-btn" @click="emit('close')">
        <XMarkIcon class="w-3.5 h-3.5" />
      </button>
    </div>
    <div class="panel-content">
      <!-- Keyed on the active workspace so switching projects remounts these and
           re-fetches: both only load on mount, so without the key the Files tree
           and Changes diff would keep showing the previous project's content. -->
      <FileTreePanel v-if="activeTab === 'files'" :key="store.pwd" />
      <DiffViewer v-else-if="activeTab === 'changes'" :key="store.pwd" />
      <div v-else class="plan-pane">
        <div class="plan-head">
          <span class="plan-title">{{ t('rightPanel.plan') }}</span>
          <div class="plan-track"><div class="plan-fill" :style="{ width: progressPct + '%' }" /></div>
          <span class="plan-count">{{ completed }} / {{ total }}</span>
        </div>
        <div class="plan-list">
          <TaskList v-if="total" :todos="store.todos" />
          <div v-else class="plan-empty">{{ t('rightPanel.noTasks') }}</div>
        </div>
      </div>
    </div>
  </aside>
</template>

<style scoped>
.right-panel {
  min-width: 220px;
  max-width: 600px;
  height: 100%;
  display: flex;
  flex-direction: column;
  background: var(--color-background);
  border-left: 1px solid var(--color-border);
  overflow: hidden;
  position: relative;
}

.panel-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 0 8px 0 12px;
  height: 40px;
  border-bottom: 1px solid var(--color-border);
  flex-shrink: 0;
}

.panel-tabs {
  display: flex;
  align-items: center;
  gap: 2px;
}

.tab-btn {
  font-size: 12px;
  font-weight: 500;
  padding: 4px 10px;
  border: none;
  background: transparent;
  border-radius: var(--radius-md);
  color: var(--color-muted-foreground);
  cursor: pointer;
  transition: background 0.15s, color 0.15s;
}

.tab-btn:hover {
  color: var(--color-foreground);
}

.tab-btn.active {
  color: var(--color-foreground);
  background: var(--color-muted);
}

.close-btn {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 24px;
  height: 24px;
  border: none;
  background: transparent;
  border-radius: var(--radius-sm);
  color: var(--color-muted-foreground);
  cursor: pointer;
}

.close-btn:hover {
  background: var(--color-muted);
  color: var(--color-foreground);
}

.resize-handle {
  position: absolute;
  left: 0;
  top: 0;
  bottom: 0;
  width: 4px;
  cursor: col-resize;
  z-index: 10;
  transition: background 0.15s;
}

.resize-handle:hover {
  background: color-mix(in srgb, var(--color-primary) 40%, transparent);
}

.panel-content {
  flex: 1;
  overflow: hidden;
}

.plan-pane {
  display: flex;
  flex-direction: column;
  height: 100%;
}

.plan-head {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 10px 12px;
  border-bottom: 1px solid var(--color-border);
  flex-shrink: 0;
}

.plan-title {
  font-size: 12px;
  font-weight: 500;
  color: var(--color-foreground);
}

.plan-track {
  flex: 1;
  height: 5px;
  border-radius: var(--radius-pill);
  background: var(--color-border);
  overflow: hidden;
}

.plan-fill {
  height: 100%;
  background: var(--color-primary);
  border-radius: var(--radius-pill);
  transition: width var(--duration-slow) var(--ease-out);
}

.plan-count {
  font-family: var(--font-mono);
  font-size: 11px;
  color: var(--color-muted-foreground);
  white-space: nowrap;
}

.plan-list {
  flex: 1;
  overflow-y: auto;
  padding: 6px;
}

.plan-empty {
  padding: 16px 8px;
  font-size: 13px;
  color: var(--color-muted-foreground);
  text-align: center;
}
</style>
