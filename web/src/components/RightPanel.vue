<script setup lang="ts">
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
</script>

<template>
  <aside class="right-panel">
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
  width: 320px;
  min-width: 280px;
  max-width: 480px;
  height: 100%;
  display: flex;
  flex-direction: column;
  background: var(--color-background);
  border-left: 1px solid var(--color-border);
  overflow: hidden;
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

.panel-content {
  flex: 1;
  overflow: hidden;
}
</style>
