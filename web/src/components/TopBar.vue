<script setup lang="ts">
import { computed, ref, onMounted, watch } from 'vue'
import {
  Menu,
  SquareTerminal,
  FileDiff,
  FolderOpen,
  ListChecks,
  GitBranch,
  GitCommitVertical,
  ChevronRight,
  ChevronDown,
  PanelRight,
} from 'lucide-vue-next'
import {
  Popover,
  PopoverButton,
  PopoverPanel,
  Menu as HMenu,
  MenuButton,
  MenuItems,
  MenuItem,
} from '@headlessui/vue'
import { useChatStore } from '@/stores/chat'
import { api } from '@/composables/api'

type PanelType = 'terminal' | 'files' | 'changes' | 'plan'

const store = useChatStore()

const props = defineProps<{
  projectName: string
  pwd: string
  isRunning: boolean
  wsConnected: boolean
  activePanel: 'none' | PanelType
  terminalOpen: boolean
}>()

// Terminal is a bottom panel and can be open alongside a right-panel tab, so it
// is tracked separately from activePanel (which reflects the right panel only).
function isCurrent(panel: PanelType): boolean {
  if (panel === 'terminal') return props.terminalOpen
  return props.activePanel === panel
}

const emit = defineEmits<{
  'toggle-sidebar': []
  'toggle-panel': [panel: PanelType]
}>()

const statusColor = computed(() => {
  if (props.isRunning) return '#f59e0b'
  if (props.wsConnected) return '#22c55e'
  return '#9ca3af'
})

const statusLabel = computed(() => {
  if (props.wsConnected) return 'Connected'
  if (props.isRunning) return 'Running'
  return 'Disconnected'
})

const sessionTitle = computed(() => {
  const session = store.sessions.find(s => s.uuid === store.currentSessionId)
  return session?.title || 'New Chat'
})

const sessionSubtitle = computed(() => {
  const session = store.sessions.find(s => s.uuid === store.currentSessionId)
  if (!session) return store.modelName || ''
  const model = session.model || store.modelName || ''
  const d = new Date(session.created_at)
  const time = d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
  return `${model} · ${time}`
})

const panelButtons = [
  { panel: 'plan' as PanelType, icon: ListChecks, label: 'Plan', shortcut: '⇧⌘P' },
  { panel: 'files' as PanelType, icon: FolderOpen, label: 'Files', shortcut: '⇧⌘E' },
  { panel: 'changes' as PanelType, icon: FileDiff, label: 'Changes', shortcut: '⇧⌘G' },
  { panel: 'terminal' as PanelType, icon: SquareTerminal, label: 'Terminal', shortcut: '⌘`' },
]

// Branch name is not exposed to the web frontend. The backend computes it
// (internal/util/envinfo.go GitBranch via `git rev-parse --abbrev-ref HEAD`)
// but no /api endpoint returns it, so we render the chip without a branch
// label rather than fabricating one. See followups.
const branchName = computed<string | null>(() => null)

// Diff stats are fetched on demand from the real /api/diff endpoint (working
// tree). We never fabricate numbers: if the fetch fails or returns nothing,
// diffStat stays null and the stat is omitted.
const diffStat = ref<{ additions: number; deletions: number } | null>(null)
const diffLoaded = ref(false)

async function loadDiffStat() {
  try {
    const result = await api.diff('working')
    const additions = result.entries.reduce((sum, e) => sum + e.additions, 0)
    const deletions = result.entries.reduce((sum, e) => sum + e.deletions, 0)
    diffStat.value = result.entries.length > 0 ? { additions, deletions } : null
  } catch (err) {
    console.error('Failed to fetch diff stat:', err)
    diffStat.value = null
  } finally {
    diffLoaded.value = true
  }
}

// Refresh stats whenever the popover is opened so the chip reflects current state.
function onChipClick() {
  loadDiffStat()
}

function openChanges(close: () => void) {
  emit('toggle-panel', 'changes')
  close()
}

// Show the diff stat on first paint and refresh it whenever a run finishes (the
// working tree likely changed), not only when the chip is clicked.
onMounted(loadDiffStat)
watch(
  () => props.isRunning,
  (running, was) => {
    if (was && !running) loadDiffStat()
  },
)
</script>

<template>
  <header class="topbar">
    <div class="topbar-left">
      <button
        class="icon-btn"
        aria-label="Toggle sidebar"
        @click="emit('toggle-sidebar')"
      >
        <Menu :size="16" />
      </button>
      <div class="session-info">
        <span class="session-title">{{ sessionTitle }}</span>
        <span class="session-subtitle">{{ sessionSubtitle }}</span>
      </div>
    </div>

    <div class="topbar-right">
      <!-- Panel menu -->
      <HMenu as="div" class="panel-menu" v-slot="{ open }">
        <MenuButton class="panel-menu-btn" :class="{ open }" aria-label="Open panel" title="Panels">
          <PanelRight :size="16" />
          <ChevronDown :size="14" class="panel-menu-caret" />
        </MenuButton>
        <transition
          enter-active-class="pop-enter-active"
          enter-from-class="pop-enter-from"
          leave-active-class="pop-leave-active"
          leave-to-class="pop-leave-to"
        >
          <MenuItems class="panel-menu-items">
            <MenuItem v-for="btn in panelButtons" :key="btn.panel" v-slot="{ active }">
              <button
                class="panel-menu-item"
                :class="{ highlight: active, current: isCurrent(btn.panel) }"
                :aria-current="isCurrent(btn.panel) ? 'true' : undefined"
                @click="emit('toggle-panel', btn.panel)"
              >
                <component :is="btn.icon" :size="16" class="pmi-icon" />
                <span class="pmi-label">{{ btn.label }}</span>
                <span class="pmi-key">{{ btn.shortcut }}</span>
              </button>
            </MenuItem>
          </MenuItems>
        </transition>
      </HMenu>

      <!-- Workspace chip + popover -->
      <Popover class="workspace-popover" v-slot="{ close }">
        <PopoverButton class="workspace-chip" :aria-label="`Workspace status: ${statusLabel}`" :title="statusLabel" @click="onChipClick">
          <span class="chip-branch">
            <GitBranch :size="16" />
            <span v-if="branchName" class="chip-branch-name">{{ branchName }}</span>
          </span>
          <template v-if="diffStat">
            <span class="chip-divider" />
            <span class="chip-stat">
              <span class="stat-add text-emerald-600 dark:text-emerald-400">+{{ diffStat.additions }}</span>
              <span class="stat-del text-red-500 dark:text-red-400">-{{ diffStat.deletions }}</span>
            </span>
          </template>
          <span class="chip-divider" />
          <span class="status-dot" :style="{ backgroundColor: statusColor }" />
        </PopoverButton>

        <transition
          enter-active-class="pop-enter-active"
          enter-from-class="pop-enter-from"
          leave-active-class="pop-leave-active"
          leave-to-class="pop-leave-to"
        >
          <PopoverPanel class="workspace-panel">
            <!-- Changes row -->
            <div class="ws-row">
              <FileDiff :size="16" class="ws-icon" />
              <span class="ws-label">Changes</span>
              <span class="ws-right">
                <span v-if="diffStat" class="chip-stat">
                  <span class="stat-add text-emerald-600 dark:text-emerald-400">+{{ diffStat.additions }}</span>
                  <span class="stat-del text-red-500 dark:text-red-400">-{{ diffStat.deletions }}</span>
                </span>
                <button class="ws-action" @click="openChanges(close)">Review</button>
              </span>
            </div>

            <!-- Branch row -->
            <div class="ws-row">
              <GitBranch :size="16" class="ws-icon" />
              <span class="ws-label">
                {{ branchName || 'Branch' }}
              </span>
              <span class="ws-right">
                <ChevronRight :size="16" class="ws-chevron" />
              </span>
            </div>

            <!-- Commit or push row -->
            <div class="ws-row">
              <GitCommitVertical :size="16" class="ws-icon" />
              <span class="ws-label">Commit or push</span>
              <span class="ws-right">
                <ChevronRight :size="16" class="ws-chevron" />
              </span>
            </div>

            <div class="ws-sep" />

            <!-- Status row -->
            <div class="ws-row ws-row-static">
              <span class="status-dot ws-icon-dot" :style="{ backgroundColor: statusColor }" />
              <span class="ws-label">{{ statusLabel }}</span>
              <span class="ws-right ws-model" v-if="store.modelName">{{ store.modelName }}</span>
            </div>
          </PopoverPanel>
        </transition>
      </Popover>
    </div>
  </header>
</template>

<style scoped>
.topbar {
  height: 52px;
  background: var(--color-sidebar-bg);
  border-bottom: 1px solid var(--color-border);
  padding: 0 14px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  font-family: var(--font-sans);
}

.topbar-left {
  display: flex;
  align-items: center;
  gap: 10px;
  min-width: 0;
  flex: 1;
}

.topbar-right {
  display: flex;
  align-items: center;
  gap: 8px;
  flex: 1;
  justify-content: flex-end;
}

.icon-btn {
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 6px;
  border: none;
  background: transparent;
  border-radius: 6px;
  color: var(--color-muted-foreground);
  cursor: pointer;
  transition: background 0.15s, color 0.15s;
}

.icon-btn:hover {
  color: var(--color-foreground);
}

.session-info {
  display: flex;
  flex-direction: column;
  min-width: 0;
  gap: 1px;
}

.session-title {
  font-size: 13px;
  font-weight: 600;
  color: var(--color-foreground);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  max-width: 220px;
  line-height: 1.3;
}

.session-subtitle {
  font-size: 11px;
  color: var(--color-muted-foreground);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  max-width: 220px;
  line-height: 1.3;
}

/* Panel menu */
.panel-menu {
  position: relative;
  display: inline-flex;
}

.panel-menu-btn {
  display: inline-flex;
  align-items: center;
  gap: 3px;
  height: 30px;
  padding: 0 8px;
  border: 1px solid var(--color-border);
  border-radius: var(--radius-lg);
  background: transparent;
  color: var(--color-muted-foreground);
  cursor: pointer;
  transition: background 0.15s, color 0.15s, border-color 0.15s;
}

.panel-menu-btn:hover {
  color: var(--color-foreground);
  border-color: var(--color-foreground);
}

.panel-menu-btn.open {
  background: var(--color-muted);
  color: var(--color-foreground);
}

.panel-menu-caret {
  opacity: 0.6;
}

.panel-menu-items {
  position: absolute;
  top: calc(100% + 6px);
  right: 0;
  min-width: 224px;
  padding: 4px;
  background: var(--color-surface);
  border: 1px solid var(--color-border);
  border-radius: var(--radius-lg);
  box-shadow: var(--shadow-md);
  z-index: var(--z-dropdown);
  outline: none;
}

.panel-menu-item {
  display: flex;
  align-items: center;
  gap: 10px;
  width: 100%;
  padding: 7px 8px;
  border: none;
  background: transparent;
  border-radius: var(--radius-md);
  color: var(--color-foreground);
  font-size: 13px;
  text-align: left;
  cursor: pointer;
}

.panel-menu-item.highlight {
  background: var(--color-muted);
}

.pmi-icon {
  color: var(--color-muted-foreground);
  flex-shrink: 0;
}

.panel-menu-item.current .pmi-icon {
  color: var(--color-primary);
}

.pmi-label {
  flex: 1;
  white-space: nowrap;
}

.pmi-key {
  color: var(--color-muted-foreground);
  font-size: 11px;
  letter-spacing: 0.04em;
  flex-shrink: 0;
}

/* Workspace chip */
.workspace-popover {
  position: relative;
  display: inline-flex;
}

.workspace-chip {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  height: 30px;
  padding: 0 10px;
  border: 1px solid var(--color-border);
  border-radius: var(--radius-lg);
  background: transparent;
  color: var(--color-muted-foreground);
  cursor: pointer;
  transition: border-color 0.15s, color 0.15s;
}

.workspace-chip:hover {
  border-color: var(--color-foreground);
  color: var(--color-foreground);
}

.chip-branch {
  display: inline-flex;
  align-items: center;
  gap: 5px;
}

.chip-branch-name {
  font-size: 12px;
  font-weight: 500;
  max-width: 140px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.chip-divider {
  width: 1px;
  height: 16px;
  background: var(--color-border);
}

.chip-stat {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  font-family: var(--font-mono);
  font-size: 12px;
}

.status-dot {
  width: 8px;
  height: 8px;
  border-radius: 9999px;
  flex-shrink: 0;
}

/* Workspace popover panel */
.workspace-panel {
  position: absolute;
  top: calc(100% + 6px);
  right: 0;
  width: 260px;
  padding: 4px;
  background: var(--color-surface);
  border: 1px solid var(--color-border);
  border-radius: var(--radius-lg);
  box-shadow: var(--shadow-md);
  z-index: var(--z-dropdown);
}

.ws-row {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 8px;
  border-radius: var(--radius-md);
  font-size: 13px;
  color: var(--color-foreground);
}

.ws-row:not(.ws-row-static):hover {
  background: var(--color-muted);
}

.ws-icon {
  color: var(--color-muted-foreground);
  flex-shrink: 0;
}

.ws-icon-dot {
  width: 8px;
  height: 8px;
  margin: 0 4px;
}

.ws-label {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.ws-right {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  flex-shrink: 0;
}

.ws-chevron {
  color: var(--color-muted-foreground);
}

.ws-action {
  border: 1px solid var(--color-border);
  background: transparent;
  border-radius: var(--radius-md);
  padding: 2px 10px;
  font-size: 12px;
  font-weight: 500;
  color: var(--color-foreground);
  cursor: pointer;
  transition: background 0.15s, border-color 0.15s;
}

.ws-action:hover {
  background: var(--color-muted);
  border-color: var(--color-foreground);
}

.ws-sep {
  height: 1px;
  margin: 4px 0;
  background: var(--color-border);
}

.ws-model {
  font-family: var(--font-mono);
  font-size: 11px;
  color: var(--color-muted-foreground);
  max-width: 140px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

/* Popover transition */
.pop-enter-active,
.pop-leave-active {
  transition: opacity 0.12s ease, transform 0.12s ease;
}

.pop-enter-from,
.pop-leave-to {
  opacity: 0;
  transform: translateY(-4px);
}
</style>
