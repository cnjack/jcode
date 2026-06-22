<script setup lang="ts">
import { ref, computed, inject, onMounted } from 'vue'
import { Popover, PopoverButton, PopoverPanel } from '@headlessui/vue'
import {
  FolderIcon,
  FolderOpenIcon,
  CheckIcon,
  PlusIcon,
  ServerIcon,
  ChevronDownIcon,
  MagnifyingGlassIcon,
  ArrowLeftIcon,
} from '@heroicons/vue/24/outline'
import { useI18n } from 'vue-i18n'
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
const { t } = useI18n()

// Provided by App: opens the SSH wizard, optionally prefilled for a reconnect.
const openRemoteConnect = inject<(prefill?: RemoteMeta & { loadTaskUuid?: string }) => void>('openRemoteConnect')
// Provided by App: the unified post-switch handler (reload workspace state +
// restore the target session). Used so this inline picker behaves identically to
// the projects modal instead of landing on a blank welcome.
const onWorkspaceSwitched = inject<() => Promise<void> | void>('onWorkspaceSwitched')

const query = ref('')
// Inline error surfaced when a workspace switch fails — without this the picker
// would close (or do nothing) silently, unlike ProjectSwitcher which shows a
// red error row. Cleared on every new attempt.
const switchErr = ref('')

const activePath = computed(() => projectStore.activeProject?.path || store.pwd)
const activeIsRemote = computed(() => isRemotePath(activePath.value))
const activeName = computed(() => {
  const p = activePath.value
  if (!p) return t('workspace.none')
  return projectStore.nameForPath(p)
})

const workspaces = computed(() => {
  const q = query.value.trim().toLowerCase()
  // Drop local workspaces whose folder no longer exists on disk — selecting one
  // would only surface "path does not exist or is not a directory". Remote
  // (ssh://) labels are never validated locally, so they always stay listed.
  const nodes = projectStore.projectsForTree.filter(
    (n) => isRemotePath(n.path) || !projectStore.missingPaths.has(n.path),
  )
  if (!q) return nodes
  return nodes.filter((n) => n.name.toLowerCase().includes(q) || n.path.toLowerCase().includes(q))
})

// Re-stat the known workspaces whenever the picker is shown, so a folder deleted
// while the app was open disappears from the list. Runs on mount (the pill is
// always mounted) and again each time the panel is opened.
onMounted(() => projectStore.validateProjectPaths())
function onOpen() {
  projectStore.validateProjectPaths()
}

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
  // Guard empty path (browser may not have loaded a folder yet) — matches
  // ProjectSwitcher's `if (!browsePath.value) return`; otherwise we'd open a
  // bogus empty-path project.
  if (!path) return
  switchErr.value = ''
  const ok = await projectStore.openProject(path)
  if (!ok) {
    // Keep the panel open and show why, instead of failing silently.
    switchErr.value = projectStore.switchError || t('workspace.openError')
    return
  }
  // Route through the same post-switch handler the projects modal uses, so both
  // entry points reload identically and restore the target workspace's session.
  if (onWorkspaceSwitched) await onWorkspaceSwitched()
  else await store.resetToWelcomeAfterSwitch()
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
    try {
      const path = await pickFolder(activePath.value || undefined)
      // null = user cancelled the native dialog → do nothing.
      if (path) await applyLocalSwitch(path, close)
    } catch {
      // Native picker unavailable (e.g. dialog plugin missing) → in-app browser.
      openBrowser()
    }
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
  switchErr.value = ''
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
      <button class="ws-pill ws-pill-action" :disabled="store.isRunning" :title="activePath" @click="onOpen">
        <component :is="activeIsRemote ? ServerIcon : FolderOpenIcon" class="w-3.5 h-3.5 ws-pill-icon" />
        <span class="ws-name">{{ activeName }}</span>
        <ChevronDownIcon class="w-3 h-3 ws-caret" />
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
            <button class="ws-back" @click="showBrowser = false"><ArrowLeftIcon class="w-3.5 h-3.5" /></button>
            <input
              v-model="pathInput"
              class="ws-path-input"
              :placeholder="t('projectSwitcher.pathPlaceholder')"
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
            <div v-if="browseLoading" class="ws-hint">{{ t('workspace.loading') }}</div>
            <div v-else-if="browseFolders.length === 0" class="ws-hint">{{ t('workspace.noFolders') }}</div>
            <button
              v-for="folder in browseFolders"
              :key="folder.path"
              class="ws-row ws-folder"
              @click="loadFolders(folder.path)"
            >
              <FolderIcon class="w-3.5 h-3.5 ws-folder-icon" />
              <span class="ws-row-name">{{ folder.name }}</span>
            </button>
          </div>
          <div v-if="switchErr" class="ws-error">{{ switchErr }}</div>
          <div class="ws-browser-foot">
            <span class="ws-cur-path">{{ browsePath || '~' }}</span>
            <button class="ws-open-btn" :disabled="!browsePath" @click="applyLocalSwitch(browsePath, close)">{{ t('workspace.open') }}</button>
          </div>
        </div>

        <!-- Workspace list -->
        <div v-else class="ws-listview">
          <div class="ws-search">
            <MagnifyingGlassIcon class="w-3 h-3 ws-search-icon" />
            <input v-model="query" class="ws-search-input" :placeholder="t('workspace.search')" />
          </div>

          <div class="ws-list">
            <div v-if="workspaces.length === 0" class="ws-hint">{{ t('workspace.nonePlural') }}</div>
            <button
              v-for="node in workspaces"
              :key="node.path"
              class="ws-row"
              :class="{ active: isActive(node.path) }"
              @click="pickWorkspace(node, close)"
            >
              <component :is="isRemotePath(node.path) ? ServerIcon : FolderIcon" class="w-3.5 h-3.5 ws-row-icon" />
              <span class="ws-row-name">{{ node.name }}</span>
              <CheckIcon v-if="isActive(node.path)" class="w-3.5 h-3.5 ws-check" />
            </button>
          </div>

          <div v-if="switchErr" class="ws-error">{{ switchErr }}</div>
          <div class="ws-actions">
            <button class="ws-action" @click="openFolderAction(close)">
              <PlusIcon class="w-3.5 h-3.5" /> <span>{{ t('workspace.openFolder') }}</span>
            </button>
            <button class="ws-action" @click="openRemote(close)">
              <ServerIcon class="w-3.5 h-3.5" /> <span>{{ t('nav.remoteConnect') }}</span>
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
  color: var(--color-accent-neutral);
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
  background: var(--neutral-wash-soft);
}
.ws-row-icon,
.ws-folder-icon {
  color: var(--color-muted-foreground);
  flex-shrink: 0;
}
.ws-row.active .ws-row-icon {
  color: var(--color-accent-neutral);
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
  color: var(--color-accent-neutral);
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
  background: var(--color-accent-neutral);
  color: var(--color-surface);
  font-size: 12px;
  font-weight: 500;
  cursor: pointer;
  transition: opacity 0.15s;
}
.ws-open-btn:hover:not(:disabled) {
  opacity: 0.9;
}
.ws-open-btn:disabled {
  opacity: 0.45;
  cursor: not-allowed;
}

/* Inline switch-failure message — mirrors ProjectSwitcher's error row. */
.ws-error {
  flex-shrink: 0;
  margin: 4px 2px 0;
  padding: 6px 8px;
  border-radius: var(--radius-md);
  border: 1px solid var(--color-error-fg);
  background: var(--color-error-bg);
  color: var(--color-error-fg);
  font-size: 11.5px;
  line-height: 1.35;
  word-break: break-word;
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
