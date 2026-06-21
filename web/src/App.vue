<script setup lang="ts">
import { ref, onMounted, nextTick, watch, onUnmounted, provide } from 'vue'
import { useI18n } from 'vue-i18n'
import { ChevronDoubleDownIcon } from '@heroicons/vue/24/outline'
import { normalizeMode } from '@/types/api'
import type { RemoteMeta } from '@/types/api'
import { useChatStore } from '@/stores/chat'
import { useProjectStore } from '@/stores/project'
import { useWebSocket } from '@/composables/ws'
import { useTheme } from '@/composables/useTheme'
import { useBranch } from '@/composables/useBranch'
import ChatMessageVue from '@/components/ChatMessage.vue'
import ToolCallCard from '@/components/ToolCallCard.vue'
import ApprovalBanner from '@/components/ApprovalBanner.vue'
import GoalBanner from '@/components/GoalBanner.vue'
import ChatInput from '@/components/ChatInput.vue'
import Sidebar from '@/components/Sidebar.vue'
import SettingsDialog from '@/components/SettingsDialog.vue'
import ProjectSwitcher from '@/components/ProjectSwitcher.vue'
import RemoteConnectWizard from '@/components/RemoteConnectWizard.vue'
import TerminalPanel from '@/components/TerminalPanel.vue'
import RightPanel from '@/components/RightPanel.vue'
import SetupView from '@/components/SetupView.vue'
import TopBar from '@/components/TopBar.vue'
import CommandPalette from '@/components/CommandPalette.vue'
import { useNotifications } from '@/composables/notifications'

const store = useChatStore()
const projectStore = useProjectStore()
const { t } = useI18n()
const { resolvedTheme, toggleTheme } = useTheme()
const { refresh: refreshBranch } = useBranch()
const { ensurePermission, notify } = useNotifications()
const messagesEl = ref<HTMLDivElement | null>(null)
const settingsOpen = ref(false)
const projectsOpen = ref(false)
const paletteOpen = ref(false)

// Remote-connect (SSH) wizard. `openRemoteConnect` is provided to descendants
// (WorkspacePicker, ProjectSwitcher, Sidebar) so any of them can launch or
// prefill it for a reconnect.
const remoteWizardOpen = ref(false)
const remotePrefill = ref<(RemoteMeta & { loadTaskUuid?: string }) | null>(null)
function openRemoteConnect(prefill?: RemoteMeta & { loadTaskUuid?: string }) {
  remotePrefill.value = prefill ?? null
  remoteWizardOpen.value = true
}
provide('openRemoteConnect', openRemoteConnect)
// Single post-switch handler shared by every local-workspace entry point
// (ProjectSwitcher via @project-switched, WorkspacePicker via inject) so they
// can't drift: all reload the full workspace state and restore the target
// workspace's session, instead of WorkspacePicker landing on a blank welcome
// while the projects modal restored the session.
provide('onWorkspaceSwitched', () => onProjectSwitched())

// When the wizard is launched from Settings it stacks ON TOP of the Settings
// overlay. headlessui treats a click inside the wizard as an "outside" click for
// Settings and would dismiss it (dropping the user back to the workspace), so we
// ignore Settings' close requests while the wizard is open. Settings then closes
// only via its own Back button, or programmatically when a remote bind succeeds.
function onSettingsClose() {
  if (remoteWizardOpen.value) return
  settingsOpen.value = false
}
const needsSetup = ref(false)

// Honor reduced-motion for the new-task ↔ conversation composer transition.
const reduceMotion = ref(
  typeof window !== 'undefined' && window.matchMedia
    ? window.matchMedia('(prefers-reduced-motion: reduce)').matches
    : false,
)

function onPaletteAction(name: 'settings' | 'projects' | 'theme') {
  if (name === 'settings') settingsOpen.value = true
  else if (name === 'projects') projectsOpen.value = true
  else if (name === 'theme') toggleTheme()
}

const bottomPanel = ref<'none' | 'terminal'>('none')
const bottomPanelHeight = ref(260)
const isResizingPanel = ref(false)
const rightPanelOpen = ref(false)
const rightPanelTab = ref<'files' | 'changes' | 'plan'>('files')

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
  onAgentDone: (data) => {
    store.agentDone(data?.error)
    notify(data?.error ? t('notifications.taskFailed') : t('notifications.taskFinished'), data?.error || t('notifications.finishedBody'))
  },
  onTodoUpdate: () => store.fetchTodos(),
  onGoalUpdate: (data) => { store.goal = data },
  onApprovalRequest: (data) => {
    store.addApprovalRequest(data)
    notify(t('notifications.approvalNeeded'), t('notifications.approvalBody'))
  },
  onAskUserRequest: (data) => store.attachAskUserRequest(data.id, data.questions),
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
  if ((e.ctrlKey || e.metaKey) && (e.key === 'k' || e.key === 'K')) {
    e.preventDefault()
    paletteOpen.value = !paletteOpen.value
    return
  }
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
  // Esc stops the agent only when no overlay is open — otherwise pressing Esc to
  // dismiss a dialog (Settings/Projects/Palette/Wizard) would also kill the run.
  if (
    e.key === 'Escape' &&
    store.isRunning &&
    !settingsOpen.value &&
    !projectsOpen.value &&
    !paletteOpen.value &&
    !remoteWizardOpen.value
  ) {
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
  if ((e.ctrlKey || e.metaKey) && e.shiftKey && e.key === 'P') {
    e.preventDefault()
    togglePanel('plan')
    return
  }
}

// True when the very first /api/health probe failed — the backend/sidecar isn't
// reachable yet (common on the desktop shell right after it navigates to the
// sidecar port). Drives a visible "can't connect" overlay with a Retry button,
// instead of silently falling through to an empty shell with console errors.
const connectionError = ref(false)
const booting = ref(false)

// All the workspace-scoped state that must be (re)loaded on boot, after setup,
// and on every project switch. Centralized so the three entry points can't drift
// out of sync (they previously each fetched a different subset, leaving stale
// models/approval-mode/channel/config after a switch).
function loadWorkspaceState() {
  store.fetchConfig()
  store.fetchTodos()
  store.fetchGoal()
  refreshBranch()
  store.fetchModels()
  store.fetchModelState()
  store.fetchSessions()
  store.fetchApprovalMode()
  store.fetchChannelState()
  if (store.pwd) {
    projectStore.ensureCurrentProject(store.pwd)
  }
}

// Initial boot: probe health first. Distinguishes three outcomes —
// unreachable (show connection error + Retry), needs setup (show wizard), or
// ready (load everything + restore the session).
async function boot() {
  booting.value = true
  try {
    const health = await store.fetchHealth()
    if (!health) {
      connectionError.value = true
      return
    }
    connectionError.value = false
    if (health.needs_setup) {
      needsSetup.value = true
      return
    }
    needsSetup.value = false
    loadWorkspaceState()
    await store.restoreCurrentSession()
  } finally {
    booting.value = false
  }
}

onMounted(async () => {
  document.addEventListener('keydown', handleGlobalKeydown)
  ensurePermission()
  await boot()
})

onUnmounted(() => {
  document.removeEventListener('keydown', handleGlobalKeydown)
  if (runTimer) clearInterval(runTimer)
})

function openFile() {
  // Open file in right panel files tab
  rightPanelOpen.value = true
  rightPanelTab.value = 'files'
}

function togglePanel(panel: 'terminal' | 'files' | 'changes' | 'plan') {
  if (panel === 'terminal') {
    bottomPanel.value = bottomPanel.value === 'terminal' ? 'none' : 'terminal'
    return
  }
  // files, changes, and plan toggle the right panel
  if (rightPanelOpen.value && rightPanelTab.value === panel) {
    rightPanelOpen.value = false
  } else {
    rightPanelOpen.value = true
    rightPanelTab.value = panel
  }
}

// Auto-open the Plan tab once when a plan first appears during an active run.
// Gated on isRunning so page loads and session switches (not running) never
// seize the panel, and one-shot per run so a manual close is respected for the
// rest of that run. A new run re-arms the one-shot.
const planAutoOpened = ref(false)
// Elapsed-time counter for the "Thinking…" footer; ticks once per second while
// the agent runs, resets on each new run. Appears in the UI only after 2s.
// Re-read the git branch whenever the active workspace changes (every switch
// path — the composer's WorkspacePicker, the projects modal, opening a task in
// another project, a remote connect — funnels through fetchHealth, which updates
// store.pwd). Without this, switching from a non-git workspace to a git one left
// the branch picker blank.
watch(() => store.pwd, (pwd) => { if (pwd) refreshBranch() })

const elapsed = ref(0)
let runTimer: ReturnType<typeof setInterval> | null = null
watch(() => store.isRunning, (running) => {
  if (running) {
    planAutoOpened.value = false
    elapsed.value = 0
    if (runTimer) clearInterval(runTimer)
    runTimer = setInterval(() => { elapsed.value++ }, 1000)
  } else if (runTimer) {
    clearInterval(runTimer)
    runTimer = null
  }
})
watch(() => store.todos.length, (len) => {
  if (len > 0 && store.isRunning && !planAutoOpened.value && !rightPanelOpen.value) {
    rightPanelOpen.value = true
    rightPanelTab.value = 'plan'
    planAutoOpened.value = true
  }
})

async function onProjectSwitched() {
  await store.fetchHealth()
  store.clearChat()
  // Reload the full workspace-scoped state (models/approval-mode/channel/config
  // included) so switching projects doesn't leave controls on the old project's
  // values.
  loadWorkspaceState()
  // Restore the current session for the new project
  await store.restoreCurrentSession()
}

function onSetupComplete() {
  needsSetup.value = false
  connectionError.value = false
  // Now load everything (fresh setup → no prior session to restore).
  loadWorkspaceState()
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
  <!-- One continuous surface: sidebar, top bar and chat area share a single
       background with no hard dividers, so the regions read as one enclosed
       space (包裹感) rather than separately-bordered panels. -->
  <div class="app-shell relative flex h-[100dvh] overflow-hidden transition-colors duration-300" style="background: var(--color-background); color: var(--color-foreground);">
    <!-- Native title-bar drag strip. Only rendered (via CSS) inside the macOS
         desktop shell, where the window uses an overlay title bar: it reserves
         the band the traffic-light buttons float over and lets the user drag
         the window. A no-op empty element in the browser. -->
    <div class="titlebar-drag" data-tauri-drag-region aria-hidden="true" />

    <!-- The single top-right control (panel menu + connection dot), floated into
         the title-bar zone — there is no separate top bar anymore. -->
    <TopBar
      :is-running="store.isRunning"
      :ws-connected="store.wsConnected"
      :active-panel="rightPanelOpen ? rightPanelTab : 'none'"
      :terminal-open="bottomPanel === 'terminal'"
      @toggle-panel="togglePanel"
    />

    <!-- Sidebar — always visible (no collapse toggle on the desktop shell). -->
    <Sidebar
      @open-file="openFile"
      @open-settings="settingsOpen = true"
      @open-projects="projectsOpen = true"
      @toggle-theme="toggleTheme"
      :resolved-theme="resolvedTheme"
    />

    <!-- Main content — shell tone (same as sidebar); the conversation lives in
         an inset surface panel below, so it reads as one continuous shell that
         wraps a distinct chat canvas (包裹感). -->
    <main class="flex-1 flex flex-col min-w-0 relative">
      <!-- Chat area — inset surface panel: distinct tone, rounded, wrapped with
           breathing room so it reads as a distinct chat canvas (包裹感). -->

      <div class="chat-panel flex-1 flex flex-col min-h-0 relative">
        <!-- Smoothly hand off between the centered new-task composer and the
             docked conversation composer: the welcome slides down + fades out and
             the conversation rises + fades in, so the composer reads as settling
             at the bottom. No `appear`: an `appear` + `mode="out-in"` initial
             enter could stick at opacity-0 and leave the whole new-task screen
             invisible. The welcome now renders at full opacity on first load;
             the welcome↔conversation crossfade still plays on message send. -->
        <transition
          :css="!reduceMotion"
          enter-active-class="transition-all duration-300 ease-out"
          enter-from-class="opacity-0 translate-y-3"
          enter-to-class="opacity-100 translate-y-0"
          leave-active-class="transition-all duration-200 ease-in"
          leave-from-class="opacity-100 translate-y-0"
          leave-to-class="opacity-0 translate-y-4"
          mode="out-in"
        >
        <!-- New-task welcome: the composer lives in the CENTER of the canvas
             (with its workspace chip on top) until the first message is sent. -->
        <div
          v-if="!store.hasMessages"
          key="welcome"
          class="welcome flex-1 flex flex-col items-center px-6 overflow-y-auto"
        >
          <!-- Soft brand-tinted aura gives the empty canvas a focal point. -->
          <div class="welcome-aura" aria-hidden="true" />

          <!-- Top half: the hero floats just above the centered composer. -->
          <div class="welcome-hero flex-1 min-h-0 flex flex-col items-center justify-end pb-10">
            <div class="welcome-logo select-none">
              <span class="wl-dim">[</span><span class="wl-j">J</span><span class="wl-fg">CODE</span><span class="wl-dim">]</span>
            </div>
            <h2 class="welcome-title">
              {{ t('welcome.startIn', { project: store.projectName || 'jcode' }) }}
            </h2>
            <p class="welcome-sub">
              <i18n-t keypath="welcome.subtitle" tag="span">
                <template #kbd><kbd class="welcome-kbd">/</kbd></template>
              </i18n-t>
            </p>
          </div>

          <!-- Composer sits on the vertical centerline. Its pickers open
               downward into the empty lower half (room above is tighter). -->
          <div class="welcome-composer w-full max-w-2xl">
            <ChatInput picker-placement="bottom" />
          </div>

          <!-- Bottom half balances the center. -->
          <div class="flex-1 min-h-0" aria-hidden="true" />
        </div>

        <!-- Active conversation: scrollable timeline + bottom composer. -->
        <div v-else key="convo" class="flex-1 flex flex-col min-h-0">
          <div
            ref="messagesEl"
            class="flex-1 overflow-y-auto scroll-smooth rounded-t-[13px]"
            @scroll="checkScrollPosition"
          >
            <div class="max-w-4xl mx-auto px-5 py-6 space-y-0.5">
              <template v-for="item in store.timeline" :key="item.seq">
                <ChatMessageVue
                  v-if="item.kind === 'message'"
                  :message="item.data"
                  :can-edit="item.data.role === 'user' && !store.isRunning"
                  class="animate-fade-up"
                  @edit="(text) => store.editAndResend(item.data.id, text)"
                />
                <ToolCallCard v-else-if="item.kind === 'tool'" :tool="item.data" class="animate-fade-up pl-9" />
                <ApprovalBanner v-else-if="item.kind === 'approval'" :approval="item.data" class="animate-fade-up" />
              </template>

              <!-- Thinking footer: single source of truth for "agent is working".
                   Trails the last timeline item, rides existing auto-scroll, and
                   stays visible the whole run (initial wait AND while content
                   accumulates) — not only when the timeline is empty. -->
              <transition
                enter-active-class="transition-opacity duration-300 ease-out"
                enter-from-class="opacity-0"
                enter-to-class="opacity-100"
                leave-active-class="transition-opacity duration-150 ease-in"
                leave-from-class="opacity-100"
                leave-to-class="opacity-0"
              >
                <div
                  v-if="store.isRunning"
                  class="flex items-center gap-2.5 py-3 pl-9 select-none"
                  role="status"
                  aria-live="polite"
                  :aria-label="t('chat.thinking')"
                >
                  <span class="flex gap-1" aria-hidden="true">
                    <span class="w-1.5 h-1.5 rounded-full animate-dot-pulse" style="background: var(--color-primary); animation-delay: 0ms" />
                    <span class="w-1.5 h-1.5 rounded-full animate-dot-pulse" style="background: var(--color-primary); animation-delay: 160ms" />
                    <span class="w-1.5 h-1.5 rounded-full animate-dot-pulse" style="background: var(--color-primary); animation-delay: 320ms" />
                  </span>
                  <span class="thinking-label text-[13px]" style="font-family: var(--font-sans)">{{ t('chat.thinking') }}</span>
                  <span
                    v-if="elapsed >= 2"
                    class="text-xs tabular-nums"
                    style="font-family: var(--font-mono); color: var(--color-muted-foreground); opacity: 0.6"
                  >{{ elapsed }}s</span>
                </div>
              </transition>
            </div>
          </div>

          <!-- Scroll-to-bottom button -->
          <!-- Scroll-to-bottom button — anchored as its own flex row above the
               composer (no magic px offset), so it tracks composer height. -->
          <div class="relative h-0">
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
                class="absolute bottom-2 left-1/2 -translate-x-1/2 z-10 w-8 h-8 flex items-center justify-center rounded-full shadow-lg cursor-pointer transition-colors"
                style="background: var(--color-surface); border: 1px solid var(--color-border); color: var(--color-muted-foreground)"
                @click="scrollToBottom()"
              >
                <ChevronDoubleDownIcon class="w-4 h-4" />
              </button>
            </transition>
          </div>

          <GoalBanner />
          <ChatInput />
        </div>
        </transition>
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

    <SettingsDialog :open="settingsOpen" @close="onSettingsClose" />
    <ProjectSwitcher :open="projectsOpen" @close="projectsOpen = false" @project-switched="onProjectSwitched" />
    <RemoteConnectWizard
      :open="remoteWizardOpen"
      :prefill="remotePrefill"
      @close="remoteWizardOpen = false"
      @bound="remoteWizardOpen = false; settingsOpen = false"
    />
    <CommandPalette :open="paletteOpen" @close="paletteOpen = false" @action="onPaletteAction" />

    <!-- Setup overlay — shown when no providers are configured -->
    <SetupView v-if="needsSetup" @complete="onSetupComplete" />

    <!-- Connection-error overlay — the first health probe failed, so the local
         server isn't reachable. Replaces the old silent fall-through to an empty
         shell. Retry re-runs the boot sequence. -->
    <div v-if="connectionError" class="conn-error-overlay">
      <div class="conn-error-card">
        <div class="conn-error-title">{{ t('connection.errorTitle') }}</div>
        <div class="conn-error-msg">{{ t('connection.errorBody') }}</div>
        <button class="conn-error-retry" :disabled="booting" @click="boot">
          {{ booting ? t('connection.retrying') : t('connection.retry') }}
        </button>
      </div>
    </div>
  </div>
</template>

<style scoped>
/* The titlebar-drag strip + macOS top inset live in the GLOBAL style.css
   (Vue's scoped compiler mangles `:global(...) .child` selectors). */

/* Connection-error overlay — full-window, above everything, so a backend that
   never came up shows an actionable message instead of an empty shell. */
.conn-error-overlay {
  position: fixed;
  inset: 0;
  z-index: var(--z-modal);
  display: grid;
  place-items: center;
  padding: 24px;
  background: var(--color-background);
}
.conn-error-card {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 12px;
  max-width: 420px;
  text-align: center;
  padding: 28px 32px;
  background: var(--color-surface);
  border: 1px solid var(--color-border);
  border-radius: var(--radius-2xl);
  box-shadow: var(--shadow-lg);
}
.conn-error-title {
  font-size: 15px;
  font-weight: 600;
  color: var(--color-foreground);
}
.conn-error-msg {
  font-size: 13px;
  line-height: 1.6;
  color: var(--color-muted-foreground);
}
.conn-error-retry {
  margin-top: 4px;
  padding: 8px 22px;
  border: none;
  border-radius: var(--radius-lg);
  background: var(--color-primary);
  color: var(--color-on-primary);
  font-size: 13px;
  font-weight: 500;
  cursor: pointer;
  transition: opacity 0.15s;
}
.conn-error-retry:hover:not(:disabled) {
  opacity: 0.9;
}
.conn-error-retry:disabled {
  opacity: 0.55;
  cursor: not-allowed;
}

/* The conversation + composer live in one inset surface panel so the chat
   canvas reads as distinct from the sidebar shell, wrapped with breathing room
   above (below the top bar) and below (above the window edge) — 包裹感. */
.chat-panel {
  background: var(--color-surface);
  border: 1px solid var(--color-border);
  border-radius: var(--radius-2xl);
  /* Top margin clears the floating panel control at the top-right, so it sits
     in a band above the surface instead of overlapping its corner. On the
     desktop shell the 28px title-bar strip already provides most of it (see the
     is-tauri-macos override in style.css). */
  margin: 40px 14px 14px;
  /* NOT overflow:hidden — that would clip the composer's upward model/slash
     menus on short viewports. The scroll area rounds its own top corners
     (rounded-t) and the composer is inset, so the panel corners stay clean. */
  box-shadow: var(--shadow-sm);
}

/* ─── New-task welcome ─── composer on the vertical centerline, hero floating
   above it, a soft brand-tinted aura behind for focus, and small accents (the
   glowing J, the orange project name) so the empty state reads as designed. */
.welcome {
  position: relative;
}
.welcome-aura {
  position: absolute;
  z-index: 0;
  top: 40%;
  left: 50%;
  width: min(640px, 78%);
  height: 420px;
  transform: translate(-50%, -50%);
  pointer-events: none;
  background: radial-gradient(
    ellipse at center,
    color-mix(in srgb, var(--color-primary) 13%, transparent),
    transparent 70%
  );
  filter: blur(6px);
}
.welcome-hero,
.welcome-composer {
  position: relative;
  z-index: 1;
}
.welcome-logo {
  display: flex;
  align-items: center;
  font-family: var(--font-mono);
  font-size: 26px;
  font-weight: 700;
  letter-spacing: 0.06em;
  margin-bottom: 26px;
}
.welcome-logo .wl-dim {
  color: var(--color-text-muted, var(--color-muted-foreground));
}
.welcome-logo .wl-j {
  color: var(--color-primary);
  text-shadow: 0 0 22px color-mix(in srgb, var(--color-primary) 50%, transparent);
}
.welcome-logo .wl-fg {
  color: var(--color-foreground);
}
.welcome-title {
  font-family: var(--font-sans);
  font-size: 30px;
  font-weight: 600;
  line-height: 1.1;
  letter-spacing: -0.025em;
  color: var(--color-foreground);
  text-align: center;
  margin-bottom: 12px;
  text-wrap: balance;
}
.welcome-project {
  color: var(--color-primary);
}
.welcome-sub {
  max-width: 24rem;
  font-size: 13.5px;
  line-height: 1.6;
  color: var(--color-muted-foreground);
  text-align: center;
}
.welcome-kbd {
  padding: 1px 6px;
  font-family: var(--font-mono);
  font-size: 10px;
  border-radius: var(--radius-sm);
  background: var(--color-muted);
  border: 1px solid var(--color-border);
}

@media (max-width: 640px) {
  .chat-panel {
    margin: 2px 8px 8px;
  }
}
</style>
