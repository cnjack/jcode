<script setup lang="ts">
import { ref } from 'vue'
import { X } from 'lucide-vue-next'
import FileTreePanel from './FileTreePanel.vue'
import DiffViewer from './DiffViewer.vue'

defineProps<{
  activeTab: 'files' | 'changes'
}>()

const emit = defineEmits<{
  close: []
  'switch-tab': [tab: 'files' | 'changes']
}>()

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
          :class="{ active: activeTab === 'files' }"
          @click="emit('switch-tab', 'files')"
        >
          Files
        </button>
        <button
          class="tab-btn"
          :class="{ active: activeTab === 'changes' }"
          @click="emit('switch-tab', 'changes')"
        >
          Changes
        </button>
      </div>
      <button class="close-btn" @click="emit('close')">
        <X :size="14" />
      </button>
    </div>
    <div class="panel-content">
      <FileTreePanel v-if="activeTab === 'files'" />
      <DiffViewer v-else />
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
  border-radius: 6px;
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
  border-radius: 4px;
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
</style>
