<!-- eslint-disable vue/multi-word-component-names -->
<script setup lang="ts">
import { ref, watch } from 'vue'
import { useChatStore } from '@/stores/chat'
import { api } from '@/composables/api'
import type { FileItem } from '@/types/api'

const store = useChatStore()

defineProps<{
  resolvedTheme: 'light' | 'dark'
}>()

const activeTab = ref<'sessions' | 'files'>('sessions')
const files = ref<FileItem[]>([])
const currentPath = ref('')

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
  toggleTheme: []
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

async function handleDelete(uuid: string) {
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
  <aside class="w-[var(--sidebar-width)] flex flex-col shrink-0 relative" style="background: var(--color-sidebar-bg); border-right: 1px solid var(--color-border)">
    <!-- Project header -->
    <div class="px-3 pt-4 pb-3">
      <button
        class="flex items-center gap-2.5 mb-3 w-full text-left cursor-pointer rounded-md p-2 -m-0.5 transition-colors group"
        style="color: var(--color-foreground)"
        @click="emit('openProjects')"
      >
        <div class="w-8 h-8 rounded-md flex items-center justify-center text-[10px] font-bold shadow-sm" style="background: var(--color-foreground); color: #fff; font-family: var(--font-mono)">
          [<span style="color: var(--color-primary);">J</span>]
        </div>
        <div class="min-w-0 flex-1">
          <div class="text-sm font-semibold truncate" style="font-family: var(--font-sans); color: var(--color-foreground)">{{ store.projectName || 'jcode' }}</div>
          <div class="text-[10px] font-mono truncate" style="color: var(--color-muted-foreground)">{{ store.pwd }}</div>
        </div>
        <svg class="w-4 h-4 shrink-0 transition-colors" style="color: var(--color-muted-foreground)" viewBox="0 0 20 20" fill="currentColor">
          <path d="M3 10a1.5 1.5 0 113 0 1.5 1.5 0 01-3 0zM8.5 10a1.5 1.5 0 113 0 1.5 1.5 0 01-3 0zM15.5 8.5a1.5 1.5 0 100 3 1.5 1.5 0 000-3z" />
        </svg>
      </button>
      <button
        class="w-full py-2 text-xs font-medium rounded-md transition-all cursor-pointer"
        style="border: 1px solid var(--color-border); color: var(--color-muted-foreground)"
        @click="store.newSession()"
      >
        + New conversation
      </button>
    </div>

    <!-- Tabs -->
    <div class="flex mx-3" style="border-bottom: 1px solid var(--color-border)">
      <button
        v-for="tab in (['sessions', 'files'] as const)"
        :key="tab"
        class="flex-1 pb-2 text-[11px] font-medium text-center capitalize transition-colors cursor-pointer relative"
        :style="{ color: activeTab === tab ? 'var(--color-foreground)' : 'var(--color-muted-foreground)' }"
        @click="switchTab(tab)"
      >
        {{ tab }}
        <span
          v-if="activeTab === tab"
          class="absolute bottom-0 left-1/4 right-1/4 h-0.5 rounded-full"
          style="background: var(--color-primary)"
        />
      </button>
    </div>

    <!-- Sessions list -->
    <div v-if="activeTab === 'sessions'" class="flex-1 overflow-y-auto px-2 py-2">
      <div v-if="store.sessions.length === 0" class="text-center text-[11px] py-10" style="color: var(--color-muted-foreground)">
        No conversations yet
      </div>
      <div
        v-for="s in store.sessions"
        :key="s.uuid"
        class="group relative flex items-center gap-2 px-2.5 py-2.5 rounded-md cursor-pointer transition-colors mb-0.5"
        :style="{ background: s.uuid === store.currentSessionId ? 'var(--color-muted)' : 'transparent' }"
        @click="store.loadSession(s.uuid)"
      >
        <div class="min-w-0 flex-1">
          <div class="text-xs truncate font-medium" style="color: var(--color-foreground)">{{ s.title || s.uuid.slice(0, 8) + '…' }}</div>
          <div class="text-[10px] mt-0.5 font-mono" style="color: var(--color-muted-foreground)">{{ s.model }} · {{ formatDate(s.created_at) }}</div>
        </div>
        <button
          class="opacity-0 group-hover:opacity-100 shrink-0 w-6 h-6 flex items-center justify-center rounded transition-all cursor-pointer"
          style="color: var(--color-muted-foreground)"
          @click.stop="handleDelete(s.uuid)"
          title="Archive"
        >
          <svg class="w-3.5 h-3.5" viewBox="0 0 20 20" fill="currentColor">
            <path d="M2 3a1 1 0 011-1h14a1 1 0 011 1v2a1 1 0 01-1 1H3a1 1 0 01-1-1V3z" />
            <path fill-rule="evenodd" d="M3 8h14v7a2 2 0 01-2 2H5a2 2 0 01-2-2V8zm5 3a1 1 0 011-1h2a1 1 0 110 2H9a1 1 0 01-1-1z" clip-rule="evenodd" />
          </svg>
        </button>
      </div>
    </div>

    <!-- Files list -->
    <div v-if="activeTab === 'files'" class="flex-1 overflow-y-auto px-2 py-2">
      <button
        v-if="currentPath"
        class="flex items-center gap-1.5 px-2.5 py-1.5 text-xs mb-1 cursor-pointer rounded transition-colors w-full"
        style="color: var(--color-muted-foreground)"
        @click="goUp"
      >
        <svg class="w-3 h-3" viewBox="0 0 20 20" fill="currentColor"><path fill-rule="evenodd" d="M17 10a.75.75 0 01-.75.75H5.612l4.158 3.96a.75.75 0 11-1.04 1.08l-5.5-5.25a.75.75 0 010-1.08l5.5-5.25a.75.75 0 111.04 1.08L5.612 9.25H16.25A.75.75 0 0117 10z" clip-rule="evenodd" /></svg>
        ..
      </button>
      <div
        v-for="file in files"
        :key="file.name"
        class="flex items-center gap-2 px-2.5 py-1.5 rounded text-xs cursor-pointer transition-colors truncate"
        :style="{ color: file.is_dir ? 'var(--color-foreground)' : 'var(--color-muted-foreground)', fontWeight: file.is_dir ? '500' : '400' }"
        @click="handleFileClick(file)"
      >
        <span class="text-[11px] shrink-0">{{ fileIcon(file) }}</span>
        <span class="truncate">{{ file.name }}</span>
      </div>
    </div>

    <!-- Footer -->
    <div class="px-3 py-2.5 flex items-center justify-between" style="border-top: 1px solid var(--color-border)">
      <div class="text-[10px] font-mono truncate max-w-36" style="color: var(--color-muted-foreground)">
        {{ store.providerName }}/{{ store.modelName }}
      </div>
      <div class="flex items-center gap-1">
        <!-- Theme toggle -->
        <button
          class="w-7 h-7 flex items-center justify-center rounded transition-colors cursor-pointer"
          style="color: var(--color-muted-foreground)"
          @click="emit('toggleTheme')"
          :title="resolvedTheme === 'dark' ? 'Switch to light mode' : 'Switch to dark mode'"
        >
          <svg v-if="resolvedTheme === 'dark'" class="w-3.5 h-3.5" viewBox="0 0 20 20" fill="currentColor">
            <path d="M10 2a.75.75 0 01.75.75v1.5a.75.75 0 01-1.5 0v-1.5A.75.75 0 0110 2zM10 15a.75.75 0 01.75.75v1.5a.75.75 0 01-1.5 0v-1.5A.75.75 0 0110 15zM10 7a3 3 0 100 6 3 3 0 000-6zM15.657 5.404a.75.75 0 10-1.06-1.06l-1.061 1.06a.75.75 0 001.06 1.06l1.06-1.06zM6.464 14.596a.75.75 0 10-1.06-1.06l-1.06 1.06a.75.75 0 001.06 1.06l1.06-1.06zM18 10a.75.75 0 01-.75.75h-1.5a.75.75 0 010-1.5h1.5A.75.75 0 0118 10zM5 10a.75.75 0 01-.75.75h-1.5a.75.75 0 010-1.5h1.5A.75.75 0 015 10zM14.596 15.657a.75.75 0 001.06-1.06l-1.06-1.061a.75.75 0 10-1.06 1.06l1.06 1.06zM5.404 6.464a.75.75 0 001.06-1.06l-1.06-1.06a.75.75 0 10-1.06 1.06l1.06 1.06z" />
          </svg>
          <svg v-else class="w-3.5 h-3.5" viewBox="0 0 20 20" fill="currentColor">
            <path fill-rule="evenodd" d="M7.455 2.004a.75.75 0 01.26.77 7 7 0 009.958 7.967.75.75 0 011.067.853A8.5 8.5 0 116.647 1.921a.75.75 0 01.808.083z" clip-rule="evenodd" />
          </svg>
        </button>
        <!-- Settings -->
        <button
          class="w-7 h-7 flex items-center justify-center rounded transition-colors cursor-pointer"
          style="color: var(--color-muted-foreground)"
          @click="emit('openSettings')"
          title="Settings (Ctrl+,)"
        >
          <svg class="w-3.5 h-3.5" viewBox="0 0 20 20" fill="currentColor">
            <path fill-rule="evenodd" d="M7.84 1.804A1 1 0 018.82 1h2.36a1 1 0 01.98.804l.331 1.652a6.993 6.993 0 011.929 1.115l1.598-.54a1 1 0 011.186.447l1.18 2.044a1 1 0 01-.205 1.251l-1.267 1.113a7.047 7.047 0 010 2.228l1.267 1.113a1 1 0 01.206 1.25l-1.18 2.045a1 1 0 01-1.187.447l-1.598-.54a6.993 6.993 0 01-1.929 1.115l-.33 1.652a1 1 0 01-.98.804H8.82a1 1 0 01-.98-.804l-.331-1.652a6.993 6.993 0 01-1.929-1.115l-1.598.54a1 1 0 01-1.186-.447l-1.18-2.044a1 1 0 01.205-1.251l1.267-1.114a7.05 7.05 0 010-2.227L1.821 7.773a1 1 0 01-.206-1.25l1.18-2.045a1 1 0 011.187-.447l1.598.54A6.993 6.993 0 017.51 3.456l.33-1.652zM10 13a3 3 0 100-6 3 3 0 000 6z" clip-rule="evenodd" />
          </svg>
        </button>
      </div>
    </div>
  </aside>
</template>
