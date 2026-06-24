<script setup lang="ts">
// The automation editor's project field. Built on HeadlessUI's Popover (with a
// manual `close()` callback on each row), NOT the Listbox — the Listbox's
// auto-close-on-select fought the dialog: changing `:model-value` mid-open
// re-rendered the panel and it would flicker then refuse to dismiss. The Popover
// pattern (the same one WorkspacePicker uses) hands us an explicit `close`, so a
// selection closes the panel deterministically. The trigger still reads as a
// bordered form control that matches the MenuSelect fields beside it.
//
// Emits a project PATH string (v-model:modelValue). Unlike WorkspacePicker it
// never *switches* the active workspace — it only records the path the automation
// will run in — so it deliberately does not call openProject.
//
// "Open folder…" uses the native OS picker on desktop; in the browser build it
// falls back to a small in-app folder browser anchored under the field.
import { computed } from 'vue'
import { Popover, PopoverButton, PopoverPanel } from '@headlessui/vue'
import {
  FolderIcon,
  FolderOpenIcon,
  CheckIcon,
  PlusIcon,
  ServerIcon,
  ChevronUpDownIcon,
  ArrowLeftIcon,
} from '@heroicons/vue/24/outline'
import { useProjectStore, isRemotePath } from '@/stores/project'
import { useFolderBrowser } from '@/composables/useFolderBrowser'
import { isTauri, pickFolder } from '@/composables/useDesktop'
import { useI18n } from 'vue-i18n'

const props = defineProps<{
  modelValue: string
  // Which way the dropdown opens. The automation dialog sits mid-viewport, so it
  // passes 'top' to open upward.
  placement?: 'top' | 'bottom'
  error?: boolean
}>()

const emit = defineEmits<{
  (e: 'update:modelValue', value: string): void
}>()

const projectStore = useProjectStore()
const { t } = useI18n()

// Show the full project path (not just its last segment) so same-named folders
// are distinguishable. When the path is long we collapse the middle — the
// leading and trailing segments are the most identifiable parts, and the
// uncut path is always reachable via the trigger's title tooltip.
const selectedLabel = computed(() => {
  const p = props.modelValue
  if (!p) return t('projectSwitcher.pathPlaceholder')
  return collapsePathMiddle(p)
})

// collapsePathMiddle trims a long path to ~max chars by hiding the middle,
// keeping a bit more of the tail (it ends in the project name, the highest-
// signal segment). A single … marks the cut.
function collapsePathMiddle(path: string, max = 44): string {
  if (path.length <= max) return path
  const budget = max - 1 // leave room for the ellipsis
  const head = Math.floor(budget * 0.42)
  const tail = budget - head
  return path.slice(0, head) + '…' + path.slice(path.length - tail)
}

const selectedIsRemote = computed(() => isRemotePath(props.modelValue))

// Known projects, minus local folders that no longer exist on disk (selecting
// one would only surface "path does not exist"). Remote (ssh://) labels are
// never validated locally, so they always stay listed.
const workspaces = computed(() =>
  projectStore.projectsForTree.filter(
    (n) => isRemotePath(n.path) || !projectStore.missingPaths.has(n.path),
  ),
)

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

// Pick a workspace row: record the path and close the panel. `close` is the
// PopoverPanel slot callback — calling it dismisses the dropdown immediately,
// which is the fix for the old Listbox's flicker/no-dismiss behaviour.
function pickWorkspace(path: string, close: () => void) {
  if (path && path !== props.modelValue) emit('update:modelValue', path)
  close()
}

// "Open folder": native OS picker on desktop, in-app browser otherwise. Kept
// open (no close()) because the browser sub-view replaces the list in place.
async function openFolderAction(close?: () => void) {
  if (isTauri) {
    try {
      const path = await pickFolder(props.modelValue || undefined)
      if (path && path !== props.modelValue) emit('update:modelValue', path)
      close?.()
      return
    } catch {
      openBrowser()
    }
    return
  }
  openBrowser()
}

function pickPath(path: string) {
  if (path && path !== props.modelValue) emit('update:modelValue', path)
  resetBrowser()
}
</script>

<template>
  <div class="ppp-root">
    <!-- Inline position:relative — headlessui's component root doesn't receive the
         SFC scoped attribute, so the absolute options panel needs an anchor. A
         Popover (not a Listbox) drives the dropdown so we control dismissal
         explicitly via the `close` slot callback — the Listbox's auto-close
         flickered and failed to dismiss inside the dialog. -->
    <Popover class="ppp-listbox" style="position: relative">
      <PopoverButton as="template">
        <button
          type="button"
          class="ppp-trigger"
          :class="{ error, remote: selectedIsRemote }"
          :title="modelValue || t('projectSwitcher.pathPlaceholder')"
        >
          <component :is="selectedIsRemote ? ServerIcon : (modelValue ? FolderOpenIcon : FolderIcon)" class="w-3.5 h-3.5 ppp-icon" />
          <span class="ppp-name">{{ selectedLabel }}</span>
          <ChevronUpDownIcon class="w-4 h-4 ppp-caret" />
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
          class="ppp-panel"
          :class="placement === 'top' ? 'place-top' : 'place-bottom'"
        >
          <button
            v-for="node in workspaces"
            :key="node.path"
            type="button"
            class="ppp-option"
            :class="{ selected: node.path === modelValue }"
            :title="node.path"
            @click="pickWorkspace(node.path, close)"
          >
            <component :is="isRemotePath(node.path) ? ServerIcon : FolderIcon" class="w-3.5 h-3.5 ppp-opt-icon" />
            <span class="ppp-opt-name">{{ collapsePathMiddle(node.path) }}</span>
            <CheckIcon v-if="node.path === modelValue" class="w-3.5 h-3.5 ppp-check" />
          </button>

          <div v-if="!workspaces.length" class="ppp-empty">{{ t('workspace.nonePlural') }}</div>

          <div class="ppp-sep" role="separator" />

          <button type="button" class="ppp-option ppp-action" @click="openFolderAction(close)">
            <PlusIcon class="w-3.5 h-3.5 ppp-opt-icon" />
            <span class="ppp-opt-name">{{ t('workspace.openFolder') }}</span>
          </button>
        </PopoverPanel>
      </transition>
    </Popover>

    <!-- Browser build fallback for "Open folder…": a small folder browser
         anchored under the field, with a click-away backdrop. -->
    <template v-if="showBrowser">
      <div class="ppp-browser-backdrop" @click="resetBrowser" />
      <div class="ppp-browser-pop" :class="placement === 'top' ? 'place-top' : 'place-bottom'">
        <div class="ppp-browser-head">
          <button class="ppp-back" @click="resetBrowser"><ArrowLeftIcon class="w-3.5 h-3.5" /></button>
          <input
            v-model="pathInput"
            class="ppp-path-input"
            :placeholder="t('projectSwitcher.pathPlaceholder')"
            @keydown.enter="handlePathSubmit"
          />
        </div>
        <div class="ppp-list">
          <button
            v-if="browsePath && browsePath !== '/'"
            class="ppp-row ppp-folder"
            @click="goUp"
          >
            <span class="ppp-folder-icon">..</span>
          </button>
          <div v-if="browseLoading" class="ppp-hint">{{ t('workspace.loading') }}</div>
          <div v-else-if="browseFolders.length === 0" class="ppp-hint">{{ t('workspace.noFolders') }}</div>
          <button
            v-for="folder in browseFolders"
            :key="folder.path"
            class="ppp-row ppp-folder"
            @click="loadFolders(folder.path)"
          >
            <FolderIcon class="w-3.5 h-3.5 ppp-folder-icon" />
            <span class="ppp-row-name">{{ folder.name }}</span>
          </button>
        </div>
        <div class="ppp-browser-foot">
          <span class="ppp-cur-path">{{ browsePath || '~' }}</span>
          <button class="ppp-open-btn" :disabled="!browsePath" @click="pickPath(browsePath)">
            {{ t('workspace.open') }}
          </button>
        </div>
      </div>
    </template>
  </div>
</template>

<style scoped>
/* Block layout (not flex) so the listbox + trigger stretch to the field width —
   a flex row would shrink the trigger to its label ("jack"). */
.ppp-root {
  position: relative;
  width: 100%;
}
.ppp-listbox {
  width: 100%;
  min-width: 0;
}

/* Trigger mirrors the MenuSelect field-control height/look so the project field
   reads as one of the form's select controls. min-width:0 lets width:100%
   actually clamp the field — without it an inline-flex trigger's intrinsic
   min-content width (a long absolute project path) can push it wider than its
   siblings (Trigger/Hour/Minute), so the Project field reads oversized and
   misaligned. Matches the MenuSelect .ms-trigger { min-width: 0 } pattern. */
.ppp-trigger {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  width: 100%;
  min-width: 0;
  height: 32px;
  padding: 0 10px;
  font-size: 13px;
  color: var(--color-foreground);
  background: var(--color-background);
  border: 1px solid var(--color-border);
  border-radius: var(--radius-lg);
  cursor: pointer;
  transition: border-color 0.15s;
}
.ppp-trigger:hover {
  border-color: color-mix(in srgb, var(--color-foreground) 32%, var(--color-border));
}
.ppp-trigger:focus-visible {
  border-color: var(--color-primary);
  outline: none;
}
.ppp-trigger.error {
  border-color: var(--color-danger, var(--color-destructive));
}
.ppp-icon {
  color: var(--color-muted-foreground);
  flex-shrink: 0;
}
.ppp-trigger.remote .ppp-icon {
  color: var(--color-accent-neutral);
}
.ppp-name {
  flex: 1;
  min-width: 0;
  font-weight: 500;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  text-align: left;
}
.ppp-caret {
  color: var(--color-muted-foreground);
  flex-shrink: 0;
  opacity: 0.75;
}

/* Options panel (a Listbox <ul>). */
.ppp-panel {
  position: absolute;
  left: 0;
  z-index: var(--z-dropdown, 40);
  width: 100%;
  min-width: 100%;
  max-height: min(54vh, 360px);
  overflow-y: auto;
  margin: 0;
  padding: 6px;
  list-style: none;
  background: var(--color-surface);
  border: 1px solid var(--color-border);
  border-radius: var(--radius-xl);
  box-shadow: var(--shadow-lg);
  outline: none;
}
.ppp-panel.place-top {
  bottom: calc(100% + 6px);
}
.ppp-panel.place-bottom {
  top: calc(100% + 4px);
}

.ppp-option {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 100%;
  padding: 7px 8px;
  border-radius: var(--radius-md);
  cursor: pointer;
  text-align: left;
  color: var(--color-foreground);
  font-size: 12.5px;
  transition: background 0.12s;
}
/* Now real <button>s (Popover rows) rather than Listbox <li v-slot=active> — so
   the hover/keyboard-focus highlight rides native :hover/:focus-visible instead
   of the Listbox's `active` prop. */
.ppp-option:hover,
.ppp-option:focus-visible {
  background: var(--color-muted);
  outline: none;
}
.ppp-option.selected {
  color: var(--color-accent-neutral);
}
.ppp-opt-icon {
  color: var(--color-muted-foreground);
  flex-shrink: 0;
}
.ppp-option.selected .ppp-opt-icon {
  color: var(--color-accent-neutral);
}
.ppp-opt-name {
  flex: 1;
  min-width: 0;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.ppp-check {
  color: var(--color-accent-neutral);
  flex-shrink: 0;
}
.ppp-empty {
  padding: 14px 8px;
  text-align: center;
  font-size: 11.5px;
  color: var(--color-muted-foreground);
}
.ppp-sep {
  height: 1px;
  margin: 4px 2px;
  background: var(--color-border);
  list-style: none;
}

/* ─── Browser build fallback popover ─── */
.ppp-browser-backdrop {
  position: fixed;
  inset: 0;
  z-index: var(--z-dropdown, 40);
}
.ppp-browser-pop {
  position: absolute;
  left: 0;
  z-index: calc(var(--z-dropdown, 40) + 1);
  width: 100%;
  max-width: 360px;
  max-height: min(54vh, 360px);
  display: flex;
  flex-direction: column;
  padding: 6px;
  background: var(--color-surface);
  border: 1px solid var(--color-border);
  border-radius: var(--radius-xl);
  box-shadow: var(--shadow-lg);
}
.ppp-browser-pop.place-top {
  bottom: calc(100% + 6px);
}
.ppp-browser-pop.place-bottom {
  top: calc(100% + 4px);
}

.ppp-browser-head {
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
.ppp-path-input {
  flex: 1;
  min-width: 0;
  border: none;
  outline: none;
  background: transparent;
  font-family: var(--font-mono);
  font-size: 11.5px;
  color: var(--color-foreground);
}
.ppp-path-input::placeholder {
  color: var(--color-muted-foreground);
}
.ppp-back {
  display: grid;
  place-items: center;
  border: none;
  background: transparent;
  color: var(--color-muted-foreground);
  cursor: pointer;
  flex-shrink: 0;
}
.ppp-back:hover {
  color: var(--color-foreground);
}

.ppp-list {
  flex: 1 1 auto;
  min-height: 0;
  overflow-y: auto;
  padding: 2px;
}
.ppp-row {
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
.ppp-row:hover {
  background: var(--color-muted);
}
.ppp-row-icon,
.ppp-folder-icon {
  color: var(--color-muted-foreground);
  flex-shrink: 0;
}
.ppp-row-name {
  flex: 1;
  min-width: 0;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.ppp-folder .ppp-folder-icon {
  font-family: var(--font-mono);
  font-size: 12px;
}
.ppp-hint {
  padding: 14px 8px;
  text-align: center;
  font-size: 11.5px;
  color: var(--color-muted-foreground);
}

.ppp-browser-foot {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-shrink: 0;
  margin-top: 4px;
  padding-top: 6px;
  border-top: 1px solid var(--color-border);
}
.ppp-cur-path {
  flex: 1;
  min-width: 0;
  font-family: var(--font-mono);
  font-size: 11px;
  color: var(--color-muted-foreground);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.ppp-open-btn {
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
.ppp-open-btn:hover:not(:disabled) {
  opacity: 0.9;
}
.ppp-open-btn:disabled {
  opacity: 0.45;
  cursor: not-allowed;
}

.pop-enter-active,
.pop-leave-active {
  transition: opacity 0.12s ease, transform 0.12s ease;
}
.pop-enter-from,
.pop-leave-to {
  opacity: 0;
  transform: translateY(4px);
}
.ppp-panel.place-top.pop-enter-from,
.ppp-panel.place-top.pop-leave-to {
  transform: translateY(-4px);
}
</style>
