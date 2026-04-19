<script setup lang="ts">
import { ref, onMounted, nextTick, watch, onUnmounted } from 'vue'
import { useChatStore } from '@/stores/chat'
import { useProjectStore } from '@/stores/project'
import { useWebSocket } from '@/composables/ws'
import { useTheme } from '@/composables/useTheme'
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
const { resolvedTheme, toggleTheme } = useTheme()
const messagesEl = ref<HTMLDivElement | null>(null)
const settingsOpen = ref(false)
const projectsOpen = ref(false)
const fileViewerOpen = ref(false)
const fileViewerPath = ref('')
const fileViewerContent = ref('')
const sidebarCollapsed = ref(false)

const bottomPanel = ref<'none' | 'terminal' | 'diff'>('none')
const bottomPanelHeight = ref(260)
const isResizingPanel = ref(false)

// Scroll-to-bottom
const isAtBottom = ref(true)
const showScrollBtn = ref(false)

function checkScrollPosition() {
  if (!messagesEl.value) return
  const el = messagesEl.value
  const threshold = 80
  isAtBottom.value = el.scrollHeight - el.scrollTop - el.clientHeight < threshold
  showScrollBtn.value = !isAtBottom.value
}

function scrollToBottom(smooth = true) {
  if (!messagesEl.value) return
  messagesEl.value.scrollTo({
    top: messagesEl.value.scrollHeight,
    behavior: smooth ? 'smooth' : 'instant',
  })
}

// WebSocket connection
const { connected } = useWebSocket({
  onAgentStart: () => { store.isRunning = true },
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

// Auto-scroll when timeline changes (only if user is at bottom)
watch(
  () => store.timeline.length + (store.messages.length > 0 ? store.messages[store.messages.length - 1]?.content?.length || 0 : 0),
  () => {
    if (isAtBottom.value) {
      nextTick(() => scrollToBottom(false))
    }
  },
)

// Global keyboard shortcuts
function handleGlobalKeydown(e: KeyboardEvent) {
  if ((e.ctrlKey || e.metaKey) && e.shiftKey && e.key === 'N') {
    e.preventDefault()
    store.newSession()
    return
  }
  if ((e.ctrlKey || e.metaKey) && e.key === ',') {
    e.preventDefault()
    settingsOpen.value = !settingsOpen.value
    return
  }
  if (e.key === 'Escape' && store.isRunning) {
    e.preventDefault()
    store.stopAgent()
    return
  }
  if ((e.ctrlKey || e.metaKey) && e.key === '`') {
    e.preventDefault()
    togglePanel('terminal')
    return
  }
  if ((e.ctrlKey || e.metaKey) && e.key === 'b') {
    e.preventDefault()
    sidebarCollapsed.value = !sidebarCollapsed.value
    return
  }
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

// Panel resize
function startResize(e: MouseEvent) {
  e.preventDefault()
  isResizingPanel.value = true
  const startY = e.clientY
  const startH = bottomPanelHeight.value
  function onMove(ev: MouseEvent) {
    const diff = startY - ev.clientY
    bottomPanelHeight.value = Math.max(120, Math.min(600, startH + diff))
  }
  function onUp() {
    isResizingPanel.value = false
    document.removeEventListener('mousemove', onMove)
    document.removeEventListener('mouseup', onUp)
  }
  document.addEventListener('mousemove', onMove)
  document.addEventListener('mouseup', onUp)
}
</script>

<template>
  <div class="flex h-[100dvh] overflow-hidden bg-zinc-50 dark:bg-zinc-950 transition-colors duration-300">
    <!-- Sidebar -->
    <transition
      enter-active-class="transition-all duration-300 ease-[cubic-bezier(0.16,1,0.3,1)]"
      enter-from-class="-translate-x-full opacity-0"
      enter-to-class="translate-x-0 opacity-100"
      leave-active-class="transition-all duration-200 ease-[cubic-bezier(0.7,0,0.84,0)]"
      leave-from-class="translate-x-0 opacity-100"
      leave-to-class="-translate-x-full opacity-0"
    >
      <Sidebar
        v-show="!sidebarCollapsed"
        @open-file="openFile"
        @open-settings="settingsOpen = true"
        @open-projects="projectsOpen = true"
        @toggle-theme="toggleTheme"
        :resolved-theme="resolvedTheme"
      />
    </transition>

    <!-- Main content -->
    <main class="flex-1 flex flex-col min-w-0 relative">
      <!-- Header bar -->
      <header class="flex items-center justify-between h-11 px-4 border-b border-zinc-200 dark:border-zinc-800/80 bg-white/80 dark:bg-zinc-900/80 backdrop-blur-xl shrink-0 z-10">
        <div class="flex items-center gap-2.5 min-w-0">
          <!-- Sidebar toggle -->
          <button
            class="w-7 h-7 flex items-center justify-center rounded-lg text-zinc-400 hover:text-zinc-600 dark:hover:text-zinc-300 hover:bg-zinc-100 dark:hover:bg-zinc-800 transition-colors cursor-pointer"
            @click="sidebarCollapsed = !sidebarCollapsed"
            title="Toggle sidebar (Ctrl+B)"
          >
            <svg class="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round">
              <path d="M3 6h18M3 12h18M3 18h18" />
            </svg>
          </button>
          <div class="flex items-center gap-1.5 text-sm min-w-0">
            <span class="font-semibold text-zinc-800 dark:text-zinc-200 truncate" style="font-family: var(--font-sans)">{{ store.projectName || 'jcode' }}</span>
            <span class="text-zinc-300 dark:text-zinc-700">/</span>
            <span class="text-zinc-400 dark:text-zinc-600 text-xs font-mono truncate max-w-60">{{ store.pwd }}</span>
          </div>
        </div>
        <div class="flex items-center gap-2">
          <!-- Panel toggles -->
          <div class="flex items-center gap-0.5 bg-zinc-100 dark:bg-zinc-800/60 rounded-lg p-0.5">
            <button
              class="px-2.5 py-1 text-[11px] font-medium rounded-md cursor-pointer transition-all duration-150"
              :class="bottomPanel === 'terminal'
                ? 'bg-white dark:bg-zinc-700 text-emerald-600 dark:text-emerald-400 shadow-sm'
                : 'text-zinc-400 dark:text-zinc-500 hover:text-zinc-600 dark:hover:text-zinc-300'"
              @click="togglePanel('terminal')"
              title="Terminal (Ctrl+`)"
            >
              Terminal
            </button>
            <button
              class="px-2.5 py-1 text-[11px] font-medium rounded-md cursor-pointer transition-all duration-150"
              :class="bottomPanel === 'diff'
                ? 'bg-white dark:bg-zinc-700 text-emerald-600 dark:text-emerald-400 shadow-sm'
                : 'text-zinc-400 dark:text-zinc-500 hover:text-zinc-600 dark:hover:text-zinc-300'"
              @click="togglePanel('diff')"
              title="Changes"
            >
              Changes
            </button>
          </div>
          <!-- Status indicator -->
          <div class="flex items-center gap-1.5 text-xs text-zinc-400 dark:text-zinc-500 pl-2 border-l border-zinc-200 dark:border-zinc-800">
            <span
              class="w-1.5 h-1.5 rounded-full transition-colors"
              :class="store.isRunning ? 'bg-amber-400 animate-pulse' : store.wsConnected ? 'bg-emerald-400' : 'bg-zinc-400 dark:bg-zinc-600'"
            />
            {{ store.isRunning ? 'Working…' : store.wsConnected ? 'Ready' : 'Offline' }}
          </div>
        </div>
      </header>

      <!-- Chat area -->
      <div class="flex-1 flex flex-col min-h-0">
        <div
          ref="messagesEl"
          class="flex-1 overflow-y-auto scroll-smooth"
          @scroll="checkScrollPosition"
        >
          <!-- Welcome -->
          <div v-if="!store.hasMessages" class="flex flex-col items-center justify-center h-full text-center px-8 animate-fade-in">
            <div class="w-14 h-14 rounded-2xl bg-gradient-to-br from-emerald-500/20 to-emerald-600/10 dark:from-emerald-400/15 dark:to-emerald-500/5 flex items-center justify-center mb-5 ring-1 ring-emerald-500/20">
              <svg class="w-7 h-7 text-emerald-500 dark:text-emerald-400" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5">
                <path d="M13 10V3L4 14h7v7l9-11h-7z" stroke-linecap="round" stroke-linejoin="round"/>
              </svg>
            </div>
            <h2 class="text-lg font-semibold text-zinc-800 dark:text-zinc-200 mb-1.5" style="font-family: var(--font-sans)">What would you like to build?</h2>
            <p class="text-sm text-zinc-500 dark:text-zinc-500 max-w-sm">Send a message to start a conversation with jcode. Use <kbd class="px-1.5 py-0.5 text-[10px] font-mono bg-zinc-100 dark:bg-zinc-800 rounded border border-zinc-200 dark:border-zinc-700">/</kbd> for commands.</p>
          </div>

          <!-- Timeline -->
          <div v-else class="max-w-3xl mx-auto px-5 py-6 space-y-0.5">
            <template v-for="item in store.timeline" :key="item.seq">
              <ChatMessageVue v-if="item.kind === 'message'" :message="item.data" class="animate-fade-up" />
              <ToolCallCard v-else-if="item.kind === 'tool'" :tool="item.data" class="animate-fade-up" />
              <ApprovalBanner v-else-if="item.kind === 'approval'" :approval="item.data" class="animate-fade-up" />
            </template>

            <!-- Typing indicator -->
            <div v-if="store.isRunning && store.timeline.length === 0" class="flex gap-1.5 py-5 pl-1">
              <span class="w-2 h-2 bg-emerald-400/60 rounded-full" style="animation: dot-pulse 1.4s ease-in-out infinite; animation-delay: 0ms" />
              <span class="w-2 h-2 bg-emerald-400/60 rounded-full" style="animation: dot-pulse 1.4s ease-in-out infinite; animation-delay: 160ms" />
              <span class="w-2 h-2 bg-emerald-400/60 rounded-full" style="animation: dot-pulse 1.4s ease-in-out infinite; animation-delay: 320ms" />
            </div>
          </div>
        </div>

        <!-- Scroll-to-bottom button -->
        <transition
          enter-active-class="transition-all duration-200 ease-out"
          enter-from-class="opacity-0 translate-y-2"
          enter-to-class="opacity-100 translate-y-0"
          leave-active-class="transition-all duration-150 ease-in"
          leave-from-class="opacity-100 translate-y-0"
          leave-to-class="opacity-0 translate-y-2"
        >
          <button
            v-if="showScrollBtn"
            class="absolute bottom-40 left-1/2 -translate-x-1/2 z-10 w-8 h-8 flex items-center justify-center rounded-full bg-white dark:bg-zinc-800 border border-zinc-200 dark:border-zinc-700 shadow-lg text-zinc-500 dark:text-zinc-400 hover:text-zinc-700 dark:hover:text-zinc-200 cursor-pointer transition-colors"
            @click="scrollToBottom()"
          >
            <svg class="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
              <path d="M12 5v14M5 12l7 7 7-7" />
            </svg>
          </button>
        </transition>

        <TodoPanel />
        <ChatInput />
      </div>

      <!-- Bottom panel -->
      <div
        v-if="bottomPanel !== 'none'"
        class="border-t border-zinc-200 dark:border-zinc-800 relative"
        :style="{ height: bottomPanelHeight + 'px' }"
      >
        <!-- Resize handle -->
        <div
          class="absolute -top-1 left-0 right-0 h-2 cursor-row-resize z-10 group"
          @mousedown="startResize"
        >
          <div class="absolute top-[3px] left-1/2 -translate-x-1/2 w-8 h-1 rounded-full bg-zinc-300 dark:bg-zinc-700 group-hover:bg-emerald-400 dark:group-hover:bg-emerald-500 transition-colors" />
        </div>
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
