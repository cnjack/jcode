<script setup lang="ts">
import { ref, computed, inject } from 'vue'
import { Popover, PopoverButton, PopoverPanel } from '@headlessui/vue'
import {
  Folder,
  FolderOpen,
  Check,
  Plus,
  Server,
  ChevronDown,
  Search,
  ArrowLeft,
} from 'lucide-vue-next'
import { useChatStore } from '@/stores/chat'
import { useProjectStore, isRemotePath, parseRemoteLabel } from '@/stores/project'
import { useFolderBrowser } from '@/composables/useFolderBrowser'
import { isTauri, pickFolder } from '@/composables/useDesktop'
import type { RemoteMeta } from '@/types/api'

withDefaults(defineProps<{
  // Which way the panel opens relative to the trigger. The composer sits near
  // the bottom of the viewport, so it opens upward by default.
  placement?: 'top' | 'bottom'
}>(), { placement: 'top' })

const store = useChatStore()
const projectStore = useProjectStore()

// Provided by App: opens the SSH wizard, optionally prefilled for a reconnect.
const openRemoteConnect = inject<(prefill?: RemoteMeta & { loadTaskUuid?: string }) => void>('openRemoteConnect')

const query = ref('')

const activePath = computed(() => projectStore.activeProject?.path || store.pwd)
const activeIsRemote = computed(() => isRemotePath(activePath.value))
const activeName = computed(() => {
  const p = activePath.value
  if (!p) return 'No workspace'
  return projectStore.nameForPath(p)
})

const workspaces = computed(() => {
  const q = query.value.trim().toLowerCase()
  const nodes = projectStore.projectsForTree
  if (!q) return nodes
  return nodes.filter((n) => n.name.toLowerCase().includes(q) || n.path.toLowerCase().includes(q))
})

function isActive(path: string): boolean {
  return path === activePath.value
}

// ─── Folder browser sub-view (shared logic, see useFolderBrowser) ───
const {
  showBrowser,
  browsePath,
  browseFolders,
  browseLoading,
  pathInput,
  loadFolders,
  openBrowser,
  goUp,
  handlePathSubmit,
  resetBrowser,
} = useFolderBrowser()

async function applyLocalSwitch(path: string, close: () => void) {
  const ok = await projectStore.openProject(path)
  if (!ok) return
  await store.resetToWelcomeAfterSwitch()
  reset()
  close()
}

async function pickWorkspace(node: { id: string; path: string }, close: () => void) {
  // Remote workspaces must be reconnected through the SSH wizard.
  if (isRemotePath(node.path)) {
    const meta = parseRemoteLabel(node.path)
    close()
    reset()
    if (meta) openRemoteConnect?.(meta)
    return
  }
  if (node.path === activePath.value) {
    // Same workspace → start a fresh task in it.
    await store.newSession()
    reset()
    close()
    return
  }
  await applyLocalSwitch(node.path, close)
}

// "Open folder": on the desktop, use the native OS folder picker; in the
// browser, fall back to the in-app folder browser sub-view.
async function openFolderAction(close: () => void) {
  if (isTauri) {
    const path = await pickFolder(activePath.value || undefined)
    if (path) await applyLocalSwitch(path, close)
    return
  }
  openBrowser()
}

function openRemote(close: () => void) {
  close()
  reset()
  openRemoteConnect?.()
}

function reset() {
  resetBrowser()
  query.value = ''
}

</script>

<template>
  <div class="ws-bar">
  <!-- Inline position:relative — headlessui's Popover root doesn't receive the
       SFC scoped attribute, so the scoped `.ws-popover { position: relative }`
       never applies and the absolutely-positioned panel would otherwise anchor
       to the composer card (landing far below the pill). Inline style is the
       reliable way to make the panel sit right next to its trigger. -->
  <Popover class="ws-popover" style="position: relative">
    <PopoverButton as="template" :disabled="store.isRunning">
      <button class="ws-pill ws-pill-action" :disabled="store.isRunning" :title="activePath">
        <component :is="activeIsRemote ? Server : FolderOpen" :size="14" class="ws-pill-icon" />
        <span class="ws-name">{{ activeName }}</span>
        <ChevronDown :size="13" class="ws-caret" />
      </button>
    </PopoverButton>

    <transition
      enter-active-class="pop-enter-active"
      enter-from-class="pop-enter-from"
      leave-active-class="pop-leave-active"
      leave-to-class="pop-leave-to"
    >
      <PopoverPanel
        v-slot="{ close }"
        class="ws-panel"
        :class="placement === 'top' ? 'place-top' : 'place-bottom'"
      >
        <!-- Folder browser -->
        <div v-if="showBrowser" class="ws-browser">
          <div class="ws-browser-head">
            <button class="ws-back" @click="showBrowser = false"><ArrowLeft :size="14" /></button>
            <input
              v-model="pathInput"
              class="ws-path-input"
              placeholder="/path/to/folder"
              @keydown.enter="handlePathSubmit"
            />
          </div>
          <div class="ws-list">
            <button
              v-if="browsePath && browsePath !== '/'"
              class="ws-row ws-folder"
              @click="goUp"
            >
              <span class="ws-folder-icon">..</span>
            </button>
            <div v-if="browseLoading" class="ws-hint">Loading…</div>
            <div v-else-if="browseFolders.length === 0" class="ws-hint">No folders</div>
            <button
              v-for="folder in browseFolders"
              :key="folder.path"
              class="ws-row ws-folder"
              @click="loadFolders(folder.path)"
            >
              <Folder :size="14" class="ws-folder-icon" />
              <span class="ws-row-name">{{ folder.name }}</span>
            </button>
          </div>
          <div class="ws-browser-foot">
            <span class="ws-cur-path">{{ browsePath || '~' }}</span>
            <button class="ws-open-btn" @click="applyLocalSwitch(browsePath, close)">Open</button>
          </div>
        </div>

        <!-- Workspace list -->
        <div v-else class="ws-listview">
          <div class="ws-search">
            <Search :size="13" class="ws-search-icon" />
            <input v-model="query" class="ws-search-input" placeholder="Search workspaces" />
          </div>

          <div class="ws-list">
            <div v-if="workspaces.length === 0" class="ws-hint">No workspaces</div>
            <button
              v-for="node in workspaces"
              :key="node.path"
              class="ws-row"
              :class="{ active: isActive(node.path) }"
              @click="pickWorkspace(node, close)"
            >
              <component :is="isRemotePath(node.path) ? Server : Folder" :size="14" class="ws-row-icon" />
              <span class="ws-row-name">{{ node.name }}</span>
              <Check v-if="isActive(node.path)" :size="14" class="ws-check" />
            </button>
          </div>

          <div class="ws-actions">
            <button class="ws-action" @click="openFolderAction(close)">
              <Plus :size="14" /> <span>Open folder</span>
            </button>
            <button class="ws-action" @click="openRemote(close)">
              <Server :size="14" /> <span>Remote connect</span>
            </button>
          </div>
        </div>
      </PopoverPanel>
    </transition>
  </Popover>
  </div>
</template>

<style scoped>
.ws-bar {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  min-width: 0;
}
.ws-popover {
  position: relative;
  display: inline-flex;
  min-width: 0;
}

/* Two distinct pills: an interactive workspace selector + a read-only branch. */
.ws-pill {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  height: 28px;
  padding: 0 9px;
  border: 1px solid transparent;
  border-radius: var(--radius-lg);
  font-size: 12.5px;
  color: var(--color-foreground);
  min-width: 0;
}
.ws-pill-icon {
  flex-shrink: 0;
}

.ws-pill-action {
  max-width: 230px;
  background: transparent;
  cursor: pointer;
  transition: background 0.15s, transform 0.06s ease;
}
.ws-pill-action .ws-pill-icon {
  color: var(--color-primary);
}
.ws-pill-action:hover:not(:disabled) {
  background: var(--color-muted);
}
.ws-pill-action:active:not(:disabled) {
  transform: translateY(0.5px);
}
.ws-pill-action:disabled {
  opacity: 0.55;
  cursor: not-allowed;
}
.ws-name {
  font-weight: 500;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.ws-caret {
  color: var(--color-muted-foreground);
  flex-shrink: 0;
  margin-left: 1px;
}

.ws-panel {
  position: absolute;
  left: 0;
  z-index: 40;
  width: 320px;
  max-width: 84vw;
  /* Cap the height so the popover never overflows a short window (it would
     otherwise get clipped); the list region scrolls instead. */
  max-height: min(54vh, 360px);
  display: flex;
  flex-direction: column;
  padding: 6px;
  background: var(--color-surface);
  border: 1px solid var(--color-border);
  border-radius: var(--radius-xl);
  box-shadow: var(--shadow-lg);
}
/* Both panel views (workspace list + folder browser) are flex columns so their
   middle list region is the part that scrolls within the capped height. */
.ws-browser,
.ws-listview {
  display: flex;
  flex-direction: column;
  min-height: 0;
  flex: 1 1 auto;
}
.ws-panel.place-top {
  bottom: calc(100% + 6px);
}
.ws-panel.place-bottom {
  top: calc(100% + 6px);
}

/* Search */
.ws-search,
.ws-browser-head {
  display: flex;
  align-items: center;
  gap: 6px;
  flex-shrink: 0;
  padding: 5px 8px;
  margin-bottom: 4px;
  border: 1px solid var(--color-border);
  border-radius: var(--radius-lg);
  background: var(--color-background);
}
.ws-search-icon {
  color: var(--color-muted-foreground);
  flex-shrink: 0;
}
.ws-search-input,
.ws-path-input {
  flex: 1;
  min-width: 0;
  border: none;
  outline: none;
  background: transparent;
  font-size: 12.5px;
  color: var(--color-foreground);
}
.ws-path-input {
  font-family: var(--font-mono);
  font-size: 11.5px;
}
.ws-search-input::placeholder,
.ws-path-input::placeholder {
  color: var(--color-muted-foreground);
}
.ws-back {
  display: grid;
  place-items: center;
  border: none;
  background: transparent;
  color: var(--color-muted-foreground);
  cursor: pointer;
  flex-shrink: 0;
}
.ws-back:hover {
  color: var(--color-foreground);
}

/* List */
.ws-list {
  flex: 1 1 auto;
  min-height: 0;
  overflow-y: auto;
  padding: 2px;
}
.ws-row {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 100%;
  padding: 7px 8px;
  border: none;
  background: transparent;
  border-radius: var(--radius-md);
  cursor: pointer;
  text-align: left;
  color: var(--color-foreground);
  font-size: 12.5px;
  transition: background 0.12s;
}
.ws-row:hover {
  background: var(--color-muted);
}
.ws-row.active {
  background: var(--accent-wash-soft);
}
.ws-row-icon,
.ws-folder-icon {
  color: var(--color-muted-foreground);
  flex-shrink: 0;
}
.ws-row.active .ws-row-icon {
  color: var(--color-primary);
}
.ws-row-name {
  flex: 1;
  min-width: 0;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.ws-folder .ws-folder-icon {
  font-family: var(--font-mono);
  font-size: 12px;
}
.ws-check {
  color: var(--color-primary);
  flex-shrink: 0;
}
.ws-hint {
  padding: 14px 8px;
  text-align: center;
  font-size: 11.5px;
  color: var(--color-muted-foreground);
}

/* Action rows */
.ws-actions {
  margin-top: 4px;
  padding-top: 4px;
  border-top: 1px solid var(--color-border);
  display: flex;
  flex-direction: column;
  flex-shrink: 0;
  gap: 2px;
}
.ws-action {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 100%;
  padding: 8px;
  border: none;
  background: transparent;
  border-radius: var(--radius-md);
  cursor: pointer;
  font-size: 12.5px;
  color: var(--color-foreground);
  transition: background 0.12s;
}
.ws-action:hover {
  background: var(--color-muted);
}
.ws-action svg {
  color: var(--color-muted-foreground);
  flex-shrink: 0;
}

/* Browser footer */
.ws-browser-foot {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-shrink: 0;
  margin-top: 4px;
  padding-top: 6px;
  border-top: 1px solid var(--color-border);
}
.ws-cur-path {
  flex: 1;
  min-width: 0;
  font-family: var(--font-mono);
  font-size: 11px;
  color: var(--color-muted-foreground);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.ws-open-btn {
  flex-shrink: 0;
  padding: 5px 14px;
  border: none;
  border-radius: var(--radius-md);
  background: var(--color-primary);
  color: #fff;
  font-size: 12px;
  font-weight: 500;
  cursor: pointer;
  transition: opacity 0.15s;
}
.ws-open-btn:hover {
  opacity: 0.9;
}

/* Panel transition */
.pop-enter-active,
.pop-leave-active {
  transition: opacity 0.12s ease, transform 0.12s ease;
}
.pop-enter-from,
.pop-leave-to {
  opacity: 0;
  transform: translateY(4px);
}
.ws-panel.place-bottom.pop-enter-from,
.ws-panel.place-bottom.pop-leave-to {
  transform: translateY(-4px);
}
</style>
