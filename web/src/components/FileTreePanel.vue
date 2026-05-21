<script setup lang="ts">
import { ref, watch } from 'vue'
import { ChevronRight, File, Folder, ArrowLeft } from 'lucide-vue-next'
import { api } from '@/composables/api'
import type { FileItem } from '@/types/api'
import hljs from 'highlight.js'

const props = defineProps<{
  rootPath?: string
}>()

const currentPath = ref('')
const items = ref<FileItem[]>([])
const loading = ref(false)
const previewFile = ref<{ path: string; content: string } | null>(null)
const breadcrumbs = ref<string[]>([])

async function fetchDir(path: string) {
  loading.value = true
  try {
    items.value = await api.files(path || undefined)
    currentPath.value = path
    breadcrumbs.value = path ? path.split('/').filter(Boolean) : []
  } catch (err) {
    console.error('Failed to fetch files:', err)
    items.value = []
  } finally {
    loading.value = false
  }
}

async function openItem(item: FileItem) {
  if (item.is_dir) {
    const newPath = currentPath.value ? `${currentPath.value}/${item.name}` : item.name
    await fetchDir(newPath)
  } else {
    const filePath = currentPath.value ? `${currentPath.value}/${item.name}` : item.name
    try {
      const result = await api.fileContent(filePath)
      previewFile.value = result
    } catch (err) {
      console.error('Failed to fetch file content:', err)
    }
  }
}

function navigateTo(index: number) {
  if (index < 0) {
    previewFile.value = null
    fetchDir('')
  } else {
    previewFile.value = null
    const path = breadcrumbs.value.slice(0, index + 1).join('/')
    fetchDir(path)
  }
}

function goBack() {
  if (previewFile.value) {
    previewFile.value = null
    return
  }
  if (breadcrumbs.value.length > 0) {
    const parent = breadcrumbs.value.slice(0, -1).join('/')
    fetchDir(parent)
  }
}

function ext(path: string): string {
  const parts = path.split('.')
  return parts.length > 1 ? (parts[parts.length - 1] ?? '') : ''
}

function highlighted(content: string, path: string): string {
  const language = ext(path)
  if (language && hljs.getLanguage(language)) {
    return hljs.highlight(content, { language }).value
  }
  return hljs.highlightAuto(content).value
}

function formatSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`
}

watch(() => props.rootPath, () => {
  previewFile.value = null
  fetchDir(props.rootPath || '')
}, { immediate: true })
</script>

<template>
  <div class="file-tree-panel">
    <!-- Breadcrumb / navigation -->
    <div class="panel-nav">
      <button
        v-if="breadcrumbs.length > 0 || previewFile"
        class="back-btn"
        @click="goBack"
      >
        <ArrowLeft :size="14" />
      </button>
      <div class="breadcrumb">
        <button class="crumb" @click="navigateTo(-1)">root</button>
        <template v-for="(seg, i) in breadcrumbs" :key="i">
          <ChevronRight :size="10" class="crumb-sep" />
          <button class="crumb" @click="navigateTo(i)">{{ seg }}</button>
        </template>
        <template v-if="previewFile">
          <ChevronRight :size="10" class="crumb-sep" />
          <span class="crumb current">{{ previewFile.path.split('/').pop() }}</span>
        </template>
      </div>
    </div>

    <!-- File preview -->
    <div v-if="previewFile" class="file-preview">
      <pre class="preview-code"><code class="hljs" v-html="highlighted(previewFile.content, previewFile.path)" /></pre>
    </div>

    <!-- Directory listing -->
    <div v-else class="file-list">
      <div v-if="loading" class="loading-state">Loading…</div>
      <template v-else>
        <button
          v-for="item in items"
          :key="item.name"
          class="file-item"
          @click="openItem(item)"
        >
          <Folder v-if="item.is_dir" :size="14" class="item-icon folder" />
          <File v-else :size="14" class="item-icon file" />
          <span class="item-name">{{ item.name }}</span>
          <span v-if="!item.is_dir" class="item-size">{{ formatSize(item.size) }}</span>
        </button>
        <div v-if="items.length === 0 && !loading" class="empty-state">
          Empty directory
        </div>
      </template>
    </div>
  </div>
</template>

<style scoped>
.file-tree-panel {
  display: flex;
  flex-direction: column;
  height: 100%;
  overflow: hidden;
}

.panel-nav {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 8px 12px;
  border-bottom: 1px solid var(--color-border);
  min-height: 36px;
}

.back-btn {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 22px;
  height: 22px;
  border: none;
  background: transparent;
  border-radius: 4px;
  color: var(--color-muted-foreground);
  cursor: pointer;
  flex-shrink: 0;
}

.back-btn:hover {
  background: var(--color-muted);
  color: var(--color-foreground);
}

.breadcrumb {
  display: flex;
  align-items: center;
  gap: 2px;
  overflow: hidden;
  flex: 1;
}

.crumb {
  font-size: 11px;
  color: var(--color-muted-foreground);
  background: transparent;
  border: none;
  padding: 2px 4px;
  border-radius: 3px;
  cursor: pointer;
  white-space: nowrap;
}

.crumb:hover {
  color: var(--color-foreground);
  background: var(--color-muted);
}

.crumb.current {
  color: var(--color-foreground);
  cursor: default;
}

.crumb-sep {
  color: var(--color-muted-foreground);
  opacity: 0.5;
  flex-shrink: 0;
}

.file-list {
  flex: 1;
  overflow-y: auto;
  padding: 4px 0;
}

.file-item {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 100%;
  padding: 6px 12px;
  border: none;
  background: transparent;
  cursor: pointer;
  text-align: left;
  transition: background 0.1s;
}

.file-item:hover {
  background: var(--color-muted);
}

.item-icon {
  flex-shrink: 0;
}

.item-icon.folder {
  color: var(--color-primary);
}

.item-icon.file {
  color: var(--color-muted-foreground);
}

.item-name {
  font-size: 12px;
  color: var(--color-foreground);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  flex: 1;
}

.item-size {
  font-size: 10px;
  color: var(--color-muted-foreground);
  font-family: var(--font-mono);
  flex-shrink: 0;
}

.file-preview {
  flex: 1;
  overflow: auto;
  padding: 12px;
}

.preview-code {
  font-size: 11px;
  line-height: 1.6;
  font-family: var(--font-mono);
  margin: 0;
  white-space: pre-wrap;
  word-break: break-all;
}

.loading-state,
.empty-state {
  padding: 24px 12px;
  text-align: center;
  font-size: 12px;
  color: var(--color-muted-foreground);
}
</style>
