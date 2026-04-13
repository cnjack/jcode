<script setup lang="ts">
import { ref, onMounted, nextTick, watch, computed } from 'vue'
import { useChatStore } from '@/stores/chat'
import { useProjectStore } from '@/stores/project'
import { useSSE } from '@/composables/sse'
import ChatMessageVue from '@/components/ChatMessage.vue'
import ToolCallCard from '@/components/ToolCallCard.vue'
import ApprovalBanner from '@/components/ApprovalBanner.vue'
import TodoPanel from '@/components/TodoPanel.vue'
import ChatInput from '@/components/ChatInput.vue'
import Sidebar from '@/components/Sidebar.vue'
import FileViewer from '@/components/FileViewer.vue'
import SettingsDialog from '@/components/SettingsDialog.vue'
import ProjectSwitcher from '@/components/ProjectSwitcher.vue'
import TerminalPanel from '@/components/TerminalPanel.vue'
import DiffViewer from '@/components/DiffViewer.vue'

const store = useChatStore()
const projectStore = useProjectStore()
const messagesEl = ref<HTMLDivElement | null>(null)
const settingsOpen = ref(false)
const projectsOpen = ref(false)
const fileViewerOpen = ref(false)
const fileViewerPath = ref('')
const fileViewerContent = ref('')

// Bottom panel
const bottomPanel = ref<'none' | 'terminal' | 'diff'>('none')
const bottomPanelHeight = ref(250)

const { connected } = useSSE({
  onAgentText: (data) => store.appendAgentText(data.text),
  onToolCall: (data) => store.addToolCall(data.name, data.args),
  onToolResult: (data) => store.resolveToolCall(data.name, data.output, data.error),
  onTokenUpdate: (data) => { store.tokenInfo = data },
  onAgentDone: (data) => store.agentDone(data?.error),
  onTodoUpdate: () => store.fetchTodos(),
  onApprovalRequest: (data) => store.addApprovalRequest(data),
  onSessionReset: () => store.clearChat(),
  onModelChanged: (data) => {
    store.providerName = data.provider
    store.modelName = data.model
  },
  onModeChanged: (data) => {
    store.mode = data.mode as 'build' | 'plan'
  },
})

watch(connected, (val) => { store.sseConnected = val })

watch(
  () => store.messages.length + (store.messages[store.messages.length - 1]?.content?.length || 0),
  () => {
    nextTick(() => {
      if (messagesEl.value) {
        messagesEl.value.scrollTop = messagesEl.value.scrollHeight
      }
    })
  },
)

const timeline = computed(() => {
  const items: Array<{ type: 'message' | 'tool' | 'approval'; data: any; ts: number }> = []
  for (const m of store.messages) items.push({ type: 'message', data: m, ts: m.timestamp })
  for (const t of store.toolCalls) items.push({ type: 'tool', data: t, ts: t.timestamp })
  for (const a of store.approvals) items.push({ type: 'approval', data: a, ts: Date.now() })
  items.sort((a, b) => a.ts - b.ts)
  return items
})

function openFile(path: string, content: string) {
  fileViewerPath.value = path
  fileViewerContent.value = content
  fileViewerOpen.value = true
}

function togglePanel(panel: 'terminal' | 'diff') {
  bottomPanel.value = bottomPanel.value === panel ? 'none' : panel
}

onMounted(async () => {
  await store.fetchHealth()
  store.fetchConfig()
  store.fetchTodos()
  store.fetchModels()
  store.fetchSessions()
  // Auto-create project for current workspace
  if (store.pwd) {
    projectStore.ensureCurrentProject(store.pwd)
  }
})
</script>

<template>
  <div class="flex h-screen bg-white text-stone-700">
    <Sidebar
      @open-file="openFile"
      @open-settings="settingsOpen = true"
      @open-projects="projectsOpen = true"
    />

    <main class="flex-1 flex flex-col min-w-0">
      <!-- Top bar -->
      <header class="flex items-center justify-between h-11 px-5 border-b border-stone-200 bg-stone-50/80 shrink-0">
        <div class="flex items-center gap-2 text-sm text-stone-500">
          <span class="font-medium text-stone-700">{{ store.projectName || 'jcode' }}</span>
          <span class="text-stone-300">/</span>
          <span class="text-stone-400 text-xs font-mono">{{ store.pwd }}</span>
        </div>
        <div class="flex items-center gap-3">
          <!-- Bottom panel toggles -->
          <div class="flex items-center gap-0.5 border border-stone-200 rounded-lg overflow-hidden">
            <button
              class="px-2 py-1 text-[11px] cursor-pointer transition-colors"
              :class="bottomPanel === 'terminal' ? 'bg-teal-50 text-teal-700' : 'text-stone-400 hover:text-stone-600 hover:bg-stone-50'"
              @click="togglePanel('terminal')"
              title="Terminal"
            >
              ⌘ Terminal
            </button>
            <button
              class="px-2 py-1 text-[11px] cursor-pointer transition-colors"
              :class="bottomPanel === 'diff' ? 'bg-teal-50 text-teal-700' : 'text-stone-400 hover:text-stone-600 hover:bg-stone-50'"
              @click="togglePanel('diff')"
              title="Changes"
            >
              ± Changes
            </button>
          </div>
          <div class="flex items-center gap-1.5 text-xs text-stone-400">
            <span
              class="w-1.5 h-1.5 rounded-full"
              :class="store.isRunning ? 'bg-amber-400 animate-pulse' : store.sseConnected ? 'bg-emerald-400' : 'bg-stone-300'"
            />
            {{ store.isRunning ? 'Working…' : store.sseConnected ? 'Ready' : 'Offline' }}
          </div>
        </div>
      </header>

      <!-- Chat area -->
      <div class="flex-1 flex flex-col min-h-0">
        <div ref="messagesEl" class="flex-1 overflow-y-auto">
          <!-- Welcome -->
          <div v-if="!store.hasMessages" class="flex flex-col items-center justify-center h-full text-center px-8">
            <div class="text-3xl mb-3 opacity-50">⚡</div>
            <div class="text-base text-stone-500 mb-1">What would you like to build?</div>
            <div class="text-xs text-stone-400">Send a message to start a conversation with jcode.</div>
          </div>

          <!-- Timeline -->
          <div v-else class="max-w-3xl mx-auto px-5 py-6 space-y-1">
            <template v-for="item in timeline" :key="item.data.id || item.ts">
              <ChatMessageVue v-if="item.type === 'message'" :message="item.data" />
              <ToolCallCard v-else-if="item.type === 'tool'" :tool="item.data" />
              <ApprovalBanner v-else-if="item.type === 'approval'" :approval="item.data" />
            </template>

            <!-- Typing indicator -->
            <div v-if="store.isRunning && !store.messages.some(m => m.role === 'assistant' && m.id)" class="flex gap-1 py-4 pl-1">
              <span class="w-1.5 h-1.5 bg-stone-300 rounded-full animate-bounce" style="animation-delay: 0ms" />
              <span class="w-1.5 h-1.5 bg-stone-300 rounded-full animate-bounce" style="animation-delay: 150ms" />
              <span class="w-1.5 h-1.5 bg-stone-300 rounded-full animate-bounce" style="animation-delay: 300ms" />
            </div>
          </div>
        </div>

        <TodoPanel />
        <ChatInput />
      </div>

      <!-- Bottom panel -->
      <div
        v-if="bottomPanel !== 'none'"
        class="border-t border-stone-200"
        :style="{ height: bottomPanelHeight + 'px' }"
      >
        <TerminalPanel v-if="bottomPanel === 'terminal'" />
        <DiffViewer v-else-if="bottomPanel === 'diff'" />
      </div>
    </main>

    <FileViewer
      v-if="fileViewerOpen"
      :path="fileViewerPath"
      :content="fileViewerContent"
      @close="fileViewerOpen = false"
    />

    <SettingsDialog :open="settingsOpen" @close="settingsOpen = false" />
    <ProjectSwitcher :open="projectsOpen" @close="projectsOpen = false" />
  </div>
</template>
