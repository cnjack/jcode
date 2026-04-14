<script setup lang="ts">
import { ref, onMounted, watch } from 'vue'
import { useChatStore } from '@/stores/chat'
import { useProjectStore } from '@/stores/project'
import { api } from '@/composables/api'
import type { FileItem } from '@/types/api'

const store = useChatStore()
const projectStore = useProjectStore()

const activeTab = ref<'sessions' | 'files'>('sessions')
const files = ref<FileItem[]>([])
const currentPath = ref('')
const contextMenuId = ref<string | null>(null)

// Refresh files when project (pwd) changes.
watch(() => store.pwd, () => {
  currentPath.value = ''
  files.value = []
  if (activeTab.value === 'files') {
    loadFiles()
  }
})

const emit = defineEmits<{
  openFile: [path: string, content: string]
  openSettings: []
  openProjects: []
}>()

async function loadFiles(path?: string) {
  try {
    files.value = await api.files(path)
    currentPath.value = path || ''
  } catch (err) {
    console.error('Failed to fetch files:', err)
  }
}

async function handleFileClick(file: FileItem) {
  if (file.is_dir) {
    const newPath = currentPath.value ? `${currentPath.value}/${file.name}` : file.name
    await loadFiles(newPath)
  } else {
    const filePath = currentPath.value ? `${currentPath.value}/${file.name}` : file.name
    try {
      const data = await api.fileContent(filePath)
      emit('openFile', data.path, data.content)
    } catch (err) {
      console.error('Failed to open file:', err)
    }
  }
}

function goUp() {
  const parts = currentPath.value.split('/')
  parts.pop()
  loadFiles(parts.join('/') || undefined)
}

function switchTab(tab: 'sessions' | 'files') {
  activeTab.value = tab
  if (tab === 'files' && files.value.length === 0) {
    loadFiles()
  }
}

function toggleContextMenu(uuid: string, event: MouseEvent) {
  event.stopPropagation()
  contextMenuId.value = contextMenuId.value === uuid ? null : uuid
}

async function handleDelete(uuid: string) {
  contextMenuId.value = null
  await store.deleteSession(uuid)
}

function formatDate(ts: string): string {
  const d = new Date(ts)
  const now = new Date()
  if (d.toDateString() === now.toDateString()) {
    return d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
  }
  return d.toLocaleDateString([], { month: 'short', day: 'numeric' })
}

function fileIcon(file: FileItem): string {
  if (file.is_dir) return '📁'
  const ext = file.name.split('.').pop()?.toLowerCase()
  switch (ext) {
    case 'go': return '🔵'
    case 'ts': case 'tsx': return '🟦'
    case 'js': case 'jsx': return '🟨'
    case 'vue': return '🟩'
    case 'md': return '📝'
    case 'json': return '📋'
    case 'css': case 'scss': return '🎨'
    default: return '📄'
  }
}
</script>

<template>
  <aside class="w-64 bg-stone-50 border-r border-stone-200 flex flex-col shrink-0" @click="contextMenuId = null">
    <!-- Project header -->
    <div class="px-4 pt-4 pb-3">
      <button
        class="flex items-center gap-2.5 mb-3 w-full text-left cursor-pointer hover:bg-stone-100 rounded-lg p-1 -m-1 transition-colors"
        @click="emit('openProjects')"
      >
        <div class="w-7 h-7 rounded-lg bg-teal-100 text-teal-700 flex items-center justify-center text-sm font-bold">
          {{ (store.projectName || 'J').charAt(0).toUpperCase() }}
        </div>
        <div class="min-w-0 flex-1">
          <div class="text-sm font-medium text-stone-700 truncate">{{ store.projectName || 'jcode' }}</div>
          <div class="text-[10px] text-stone-400 truncate">{{ store.pwd }}</div>
        </div>
        <svg class="w-3 h-3 text-stone-400 shrink-0" viewBox="0 0 20 20" fill="currentColor">
          <path fill-rule="evenodd" d="M5.23 7.21a.75.75 0 011.06.02L10 11.168l3.71-3.938a.75.75 0 111.08 1.04l-4.25 4.5a.75.75 0 01-1.08 0l-4.25-4.5a.75.75 0 01.02-1.06z" clip-rule="evenodd" />
        </svg>
      </button>
      <button
        class="w-full py-1.5 text-xs font-medium rounded-lg border border-stone-200 text-stone-500 hover:text-stone-700 hover:border-stone-300 hover:bg-stone-100 transition-colors cursor-pointer"
        @click="store.newSession()"
      >
        + New conversation
      </button>
    </div>

    <!-- Tabs -->
    <div class="flex mx-4 border-b border-stone-200">
      <button
        v-for="tab in (['sessions', 'files'] as const)"
        :key="tab"
        class="flex-1 pb-2 text-[11px] text-center capitalize transition-colors cursor-pointer"
        :class="activeTab === tab
          ? 'text-stone-700 border-b-2 border-teal-500'
          : 'text-stone-400 hover:text-stone-600'"
        @click="switchTab(tab)"
      >
        {{ tab }}
      </button>
    </div>

    <!-- Sessions list -->
    <div v-if="activeTab === 'sessions'" class="flex-1 overflow-y-auto px-2 py-2">
      <div v-if="store.sessions.length === 0" class="text-center text-[11px] text-stone-400 py-8">
        No conversations yet
      </div>
      <div
        v-for="s in store.sessions"
        :key="s.uuid"
        class="group relative flex items-center gap-2 px-2 py-2 rounded-lg cursor-pointer hover:bg-stone-100 transition-colors mb-0.5"
        @click="store.loadSession(s.uuid)"
      >
        <div class="min-w-0 flex-1">
          <div class="text-xs text-stone-600 truncate">{{ s.uuid.slice(0, 8) }}…</div>
          <div class="text-[10px] text-stone-400 mt-0.5">{{ s.model }} · {{ formatDate(s.created_at) }}</div>
        </div>
        <button
          class="opacity-0 group-hover:opacity-100 shrink-0 w-6 h-6 flex items-center justify-center rounded text-stone-400 hover:text-red-500 hover:bg-stone-200 transition-all cursor-pointer"
          @click.stop="toggleContextMenu(s.uuid, $event)"
          title="Delete"
        >
          <svg class="w-3.5 h-3.5" viewBox="0 0 20 20" fill="currentColor">
            <path fill-rule="evenodd" d="M8.75 1A2.75 2.75 0 006 3.75v.443c-.795.077-1.584.176-2.365.298a.75.75 0 10.23 1.482l.149-.022.841 10.518A2.75 2.75 0 007.596 19h4.807a2.75 2.75 0 002.742-2.53l.841-10.52.149.023a.75.75 0 00.23-1.482A41.03 41.03 0 0014 4.193V3.75A2.75 2.75 0 0011.25 1h-2.5zM10 4c.84 0 1.673.025 2.5.075V3.75c0-.69-.56-1.25-1.25-1.25h-2.5c-.69 0-1.25.56-1.25 1.25v.325C8.327 4.025 9.16 4 10 4zM8.58 7.72a.75.75 0 00-1.5.06l.3 7.5a.75.75 0 101.5-.06l-.3-7.5zm4.34.06a.75.75 0 10-1.5-.06l-.3 7.5a.75.75 0 101.5.06l.3-7.5z" clip-rule="evenodd" />
          </svg>
        </button>
        <!-- Confirmation popup -->
        <div
          v-if="contextMenuId === s.uuid"
          class="absolute right-0 top-full z-10 mt-1 bg-white border border-stone-200 rounded-lg shadow-lg py-1 min-w-[120px]"
          @click.stop
        >
          <button
            class="w-full px-3 py-1.5 text-xs text-left text-red-500 hover:bg-stone-50 cursor-pointer"
            @click="handleDelete(s.uuid)"
          >
            Delete session
          </button>
        </div>
      </div>
    </div>

    <!-- Files list -->
    <div v-if="activeTab === 'files'" class="flex-1 overflow-y-auto px-2 py-2">
      <button
        v-if="currentPath"
        class="flex items-center gap-1.5 px-2 py-1 text-xs text-stone-400 hover:text-stone-600 mb-1 cursor-pointer"
        @click="goUp"
      >
        ← ..
      </button>
      <div
        v-for="file in files"
        :key="file.name"
        class="flex items-center gap-1.5 px-2 py-1 rounded text-xs cursor-pointer hover:bg-stone-100 transition-colors truncate"
        :class="file.is_dir ? 'text-stone-700 font-medium' : 'text-stone-500'"
        @click="handleFileClick(file)"
      >
        <span class="text-[10px] shrink-0">{{ fileIcon(file) }}</span>
        <span class="truncate">{{ file.name }}</span>
      </div>
    </div>

    <!-- Footer -->
    <div class="border-t border-stone-200 px-3 py-2.5 flex items-center justify-between">
      <div class="text-[10px] text-stone-400 font-mono truncate">
        {{ store.providerName }}/{{ store.modelName }}
      </div>
      <button
        class="w-6 h-6 flex items-center justify-center rounded text-stone-400 hover:text-stone-600 hover:bg-stone-100 transition-colors cursor-pointer"
        @click="emit('openSettings')"
        title="Settings"
      >
        <svg class="w-3.5 h-3.5" viewBox="0 0 20 20" fill="currentColor">
          <path fill-rule="evenodd" d="M7.84 1.804A1 1 0 018.82 1h2.36a1 1 0 01.98.804l.331 1.652a6.993 6.993 0 011.929 1.115l1.598-.54a1 1 0 011.186.447l1.18 2.044a1 1 0 01-.205 1.251l-1.267 1.113a7.047 7.047 0 010 2.228l1.267 1.113a1 1 0 01.206 1.25l-1.18 2.045a1 1 0 01-1.187.447l-1.598-.54a6.993 6.993 0 01-1.929 1.115l-.33 1.652a1 1 0 01-.98.804H8.82a1 1 0 01-.98-.804l-.331-1.652a6.993 6.993 0 01-1.929-1.115l-1.598.54a1 1 0 01-1.186-.447l-1.18-2.044a1 1 0 01.205-1.251l1.267-1.114a7.05 7.05 0 010-2.227L1.821 7.773a1 1 0 01-.206-1.25l1.18-2.045a1 1 0 011.187-.447l1.598.54A6.993 6.993 0 017.51 3.456l.33-1.652zM10 13a3 3 0 100-6 3 3 0 000 6z" clip-rule="evenodd" />
        </svg>
      </button>
    </div>
  </aside>
</template>
