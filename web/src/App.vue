<script setup lang="ts">
import { ref, onMounted, nextTick, watch, onUnmounted } from 'vue'
import { useChatStore } from '@/stores/chat'
import { useProjectStore } from '@/stores/project'
import { useWebSocket } from '@/composables/ws'
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

// WebSocket connection
const { connected } = useWebSocket({
  onAgentText: (data) => store.appendAgentText(data.text),
  onToolCall: (data) => store.addToolCall(data.name, data.args, data.tool_call_id),
  onToolResult: (data) => store.resolveToolCall(data.name, data.output, data.tool_call_id, data.error),
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
    store.mode = data.mode === 'build' ? 'agent' : (data.mode as 'agent' | 'plan')
  },
  onApprovalModeChanged: (data) => {
    store.autoApprove = data.auto_approve
  },
  onSubagentProgress: (data) => {
    store.addSubagentProgress(data.agent_name, data.event, data.tool_name, data.detail)
  },
  onUserMessage: (data) => {
    store.addMessage('user', data.content, data.source || undefined)
    store.isRunning = true
  },
})

watch(connected, (val) => { store.wsConnected = val })

// Auto-scroll when timeline changes
watch(
  () => store.timeline.length + (store.messages.length > 0 ? store.messages[store.messages.length - 1]?.content?.length || 0 : 0),
  () => {
    nextTick(() => {
      if (messagesEl.value) {
        messagesEl.value.scrollTop = messagesEl.value.scrollHeight
      }
    })
  },
)

// Global keyboard shortcuts
function handleGlobalKeydown(e: KeyboardEvent) {
  // Ctrl/Cmd+Shift+N: New conversation
  if ((e.ctrlKey || e.metaKey) && e.shiftKey && e.key === 'N') {
    e.preventDefault()
    store.newSession()
    return
  }
  // Ctrl/Cmd+,: Open settings
  if ((e.ctrlKey || e.metaKey) && e.key === ',') {
    e.preventDefault()
    settingsOpen.value = !settingsOpen.value
    return
  }
  // Escape: Stop agent if running
  if (e.key === 'Escape' && store.isRunning) {
    e.preventDefault()
    store.stopAgent()
    return
  }
  // Ctrl/Cmd+`: Toggle terminal
  if ((e.ctrlKey || e.metaKey) && e.key === '`') {
    e.preventDefault()
    togglePanel('terminal')
    return
  }
  // Ctrl/Cmd+L: Focus input (handled in ChatInput)
}

onMounted(async () => {
  document.addEventListener('keydown', handleGlobalKeydown)
  await store.fetchHealth()
  store.fetchConfig()
  store.fetchTodos()
  store.fetchModels()
  store.fetchSessions()
  store.fetchApprovalMode()
  store.fetchChannelState()
  if (store.pwd) {
    projectStore.ensureCurrentProject(store.pwd)
  }
})

onUnmounted(() => {
  document.removeEventListener('keydown', handleGlobalKeydown)
})

function openFile(path: string, content: string) {
  fileViewerPath.value = path
  fileViewerContent.value = content
  fileViewerOpen.value = true
}

function togglePanel(panel: 'terminal' | 'diff') {
  bottomPanel.value = bottomPanel.value === panel ? 'none' : panel
}

async function onProjectSwitched() {
  await store.fetchHealth()
  store.clearChat()
  store.fetchTodos()
  store.fetchSessions()
}
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
              title="Terminal (Ctrl+`)"
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
              :class="store.isRunning ? 'bg-amber-400 animate-pulse' : store.wsConnected ? 'bg-emerald-400' : 'bg-stone-300'"
            />
            {{ store.isRunning ? 'Working…' : store.wsConnected ? 'Ready' : 'Offline' }}
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

          <!-- Timeline (sequential via store.timeline) -->
          <div v-else class="max-w-3xl mx-auto px-5 py-6 space-y-1">
            <template v-for="item in store.timeline" :key="item.seq">
              <ChatMessageVue v-if="item.kind === 'message'" :message="item.data" />
              <ToolCallCard v-else-if="item.kind === 'tool'" :tool="item.data" />
              <ApprovalBanner v-else-if="item.kind === 'approval'" :approval="item.data" />
            </template>

            <!-- Typing indicator -->
            <div v-if="store.isRunning && store.timeline.length === 0" class="flex gap-1 py-4 pl-1">
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
    <ProjectSwitcher :open="projectsOpen" @close="projectsOpen = false" @project-switched="onProjectSwitched" />
  </div>
</template>
