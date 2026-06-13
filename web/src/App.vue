<script setup lang="ts">
import { ref, onMounted, nextTick, watch, onUnmounted } from 'vue'
import { normalizeMode } from '@/types/api'
import { useChatStore } from '@/stores/chat'
import { useProjectStore } from '@/stores/project'
import { useWebSocket } from '@/composables/ws'
import { useTheme } from '@/composables/useTheme'
import ChatMessageVue from '@/components/ChatMessage.vue'
import ToolCallCard from '@/components/ToolCallCard.vue'
import ApprovalBanner from '@/components/ApprovalBanner.vue'
import TodoPanel from '@/components/TodoPanel.vue'
import GoalBanner from '@/components/GoalBanner.vue'
import ChatInput from '@/components/ChatInput.vue'
import Sidebar from '@/components/Sidebar.vue'
import SettingsDialog from '@/components/SettingsDialog.vue'
import ProjectSwitcher from '@/components/ProjectSwitcher.vue'
import TerminalPanel from '@/components/TerminalPanel.vue'
import RightPanel from '@/components/RightPanel.vue'
import SetupView from '@/components/SetupView.vue'
import TopBar from '@/components/TopBar.vue'

const store = useChatStore()
const projectStore = useProjectStore()
const { resolvedTheme, toggleTheme } = useTheme()
const messagesEl = ref<HTMLDivElement | null>(null)
const settingsOpen = ref(false)
const projectsOpen = ref(false)
const sidebarCollapsed = ref(false)
const needsSetup = ref(false)

const bottomPanel = ref<'none' | 'terminal'>('none')
const bottomPanelHeight = ref(260)
const isResizingPanel = ref(false)
const rightPanelOpen = ref(false)
const rightPanelTab = ref<'files' | 'changes'>('files')

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
  onToolCall: (data) => store.addToolCall(data.name, data.args, data.tool_call_id, data.display_info),
  onToolResult: (data) => store.resolveToolCall(data.name, data.output, data.tool_call_id, data.error, data.display_output),
  onTokenUpdate: (data) => { store.tokenInfo = data },
  onAgentDone: (data) => store.agentDone(data?.error),
  onTodoUpdate: () => store.fetchTodos(),
  onGoalUpdate: (data) => { store.goal = data },
  onApprovalRequest: (data) => store.addApprovalRequest(data),
  onSessionReset: () => store.clearChat(),
  onModelChanged: (data) => {
    store.providerName = data.provider
    store.modelName = data.model
  },
  onModeChanged: (data) => {
    store.mode = normalizeMode(data.mode)
    store.autoApprove = store.mode === 'autopilot'
  },
  onApprovalModeChanged: (data) => {
    store.autoApprove = data.auto_approve
    if (data.auto_approve) store.mode = 'autopilot'
    else if (store.mode === 'autopilot') store.mode = 'ask'
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
  if ((e.ctrlKey || e.metaKey) && e.shiftKey && e.key === 'E') {
    e.preventDefault()
    togglePanel('files')
    return
  }
  if ((e.ctrlKey || e.metaKey) && e.shiftKey && e.key === 'G') {
    e.preventDefault()
    togglePanel('changes')
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
  const health = await store.fetchHealth()
  // Check if setup is needed — health returns needs_setup status
  if (health?.needs_setup) {
    needsSetup.value = true
    return
  }
  store.fetchConfig()
  store.fetchTodos()
  store.fetchGoal()
  store.fetchModels()
  store.fetchModelState()
  store.fetchSessions()
  store.fetchApprovalMode()
  store.fetchChannelState()
  if (store.pwd) {
    projectStore.ensureCurrentProject(store.pwd)
  }
  // Restore the current session content if available
  await store.restoreCurrentSession()
})

onUnmounted(() => {
  document.removeEventListener('keydown', handleGlobalKeydown)
})

function openFile() {
  // Open file in right panel files tab
  rightPanelOpen.value = true
  rightPanelTab.value = 'files'
}

function togglePanel(panel: 'terminal' | 'files' | 'changes') {
  if (panel === 'terminal') {
    bottomPanel.value = bottomPanel.value === 'terminal' ? 'none' : 'terminal'
    return
  }
  // files and changes toggle the right panel
  if (rightPanelOpen.value && rightPanelTab.value === panel) {
    rightPanelOpen.value = false
  } else {
    rightPanelOpen.value = true
    rightPanelTab.value = panel
  }
}

async function onProjectSwitched() {
  await store.fetchHealth()
  store.clearChat()
  store.fetchTodos()
  store.fetchGoal()
  store.fetchSessions()
  // Restore the current session for the new project
  await store.restoreCurrentSession()
}

function onSetupComplete() {
  needsSetup.value = false
  // Now load everything
  store.fetchConfig()
  store.fetchTodos()
  store.fetchGoal()
  store.fetchModels()
  store.fetchModelState()
  store.fetchSessions()
  store.fetchApprovalMode()
  store.fetchChannelState()
  if (store.pwd) {
    projectStore.ensureCurrentProject(store.pwd)
  }
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
  <div class="flex h-[100dvh] overflow-hidden transition-colors duration-300" style="background: var(--color-background); color: var(--color-foreground);">
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
      <!-- Top Bar -->
      <TopBar
        :project-name="store.projectName || 'jcode'"
        :pwd="store.pwd || ''"
        :is-running="store.isRunning"
        :ws-connected="store.wsConnected"
        :active-panel="bottomPanel === 'terminal' ? 'terminal' : rightPanelOpen ? rightPanelTab : 'none'"
        @toggle-sidebar="sidebarCollapsed = !sidebarCollapsed"
        @toggle-panel="togglePanel"
      />

      <!-- Chat area -->
      <div class="flex-1 flex flex-col min-h-0">
        <div
          ref="messagesEl"
          class="flex-1 overflow-y-auto scroll-smooth"
          @scroll="checkScrollPosition"
        >
          <!-- Welcome -->
          <div v-if="!store.hasMessages" class="flex flex-col items-center justify-center h-full text-center px-8 animate-fade-in">
            <div class="flex items-center gap-0 mb-5 select-none" style="font-family: var(--font-mono); font-size: 28px; font-weight: 700; letter-spacing: normal;">
              <span style="color: var(--color-muted-foreground)">[</span><span style="color: var(--color-primary);">J</span><span style="color: var(--color-foreground)">CODE</span><span style="color: var(--color-muted-foreground)">]</span>
            </div>
            <h2 class="text-lg font-semibold mb-1.5" style="font-family: var(--font-sans); color: var(--color-foreground)">What would you like to build?</h2>
            <p class="text-sm max-w-sm" style="color: var(--color-muted-foreground)">Send a message to start a conversation with jcode. Use <kbd class="px-1.5 py-0.5 text-[10px] font-mono rounded border" style="background: var(--color-muted); border-color: var(--color-border)">/</kbd> for commands.</p>
          </div>

          <!-- Timeline -->
          <div v-else class="max-w-4xl mx-auto px-5 py-6 space-y-0.5">
            <template v-for="item in store.timeline" :key="item.seq">
              <ChatMessageVue
                v-if="item.kind === 'message'"
                :message="item.data"
                :can-retry="item.data.role === 'assistant' && !store.isRunning"
                :can-edit="item.data.role === 'user' && !store.isRunning"
                class="animate-fade-up"
                @retry="store.retryFromMessage(item.data.id)"
                @edit="(text) => store.editAndResend(item.data.id, text)"
              />
              <ToolCallCard v-else-if="item.kind === 'tool'" :tool="item.data" class="animate-fade-up pl-9" />
              <ApprovalBanner v-else-if="item.kind === 'approval'" :approval="item.data" class="animate-fade-up" />
            </template>

            <!-- Typing indicator -->
            <div v-if="store.isRunning && store.timeline.length === 0" class="flex gap-1.5 py-5 pl-1">
              <span class="w-2 h-2 rounded-full" style="background: var(--color-primary); opacity: 0.6; animation: dot-pulse 1.4s ease-in-out infinite; animation-delay: 0ms" />
              <span class="w-2 h-2 rounded-full" style="background: var(--color-primary); opacity: 0.6; animation: dot-pulse 1.4s ease-in-out infinite; animation-delay: 160ms" />
              <span class="w-2 h-2 rounded-full" style="background: var(--color-primary); opacity: 0.6; animation: dot-pulse 1.4s ease-in-out infinite; animation-delay: 320ms" />
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
            class="absolute bottom-40 left-1/2 -translate-x-1/2 z-10 w-8 h-8 flex items-center justify-center rounded-full shadow-lg cursor-pointer transition-colors"
            style="background: var(--color-surface); border: 1px solid var(--color-border); color: var(--color-muted-foreground)"
            @click="scrollToBottom()"
          >
            <svg class="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
              <path d="M12 5v14M5 12l7 7 7-7" />
            </svg>
          </button>
        </transition>

        <TodoPanel />
        <GoalBanner />
        <ChatInput />
      </div>

      <!-- Bottom panel -->
      <div
        v-if="bottomPanel !== 'none'"
        class="relative"
        style="border-top: 1px solid var(--color-border)"
        :style="{ height: bottomPanelHeight + 'px' }"
      >
        <!-- Resize handle -->
        <div
          class="absolute -top-1 left-0 right-0 h-2 cursor-row-resize z-10 group"
          @mousedown="startResize"
        >
          <div class="absolute top-[3px] left-1/2 -translate-x-1/2 w-8 h-1 rounded-full transition-colors" style="background: var(--color-border)" />
        </div>
        <TerminalPanel v-if="bottomPanel === 'terminal'" @close="bottomPanel = 'none'" />
      </div>
    </main>

    <!-- Right Panel -->
    <transition
      enter-active-class="transition-all duration-200 ease-out"
      enter-from-class="translate-x-4 opacity-0"
      enter-to-class="translate-x-0 opacity-100"
      leave-active-class="transition-all duration-150 ease-in"
      leave-from-class="translate-x-0 opacity-100"
      leave-to-class="translate-x-4 opacity-0"
    >
      <RightPanel
        v-if="rightPanelOpen"
        :active-tab="rightPanelTab"
        @close="rightPanelOpen = false"
        @switch-tab="(tab) => rightPanelTab = tab"
      />
    </transition>

    <SettingsDialog :open="settingsOpen" @close="settingsOpen = false" />
    <ProjectSwitcher :open="projectsOpen" @close="projectsOpen = false" @project-switched="onProjectSwitched" />

    <!-- Setup overlay — shown when no providers are configured -->
    <SetupView v-if="needsSetup" @complete="onSetupComplete" />
  </div>
</template>
