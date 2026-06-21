<script setup lang="ts">
import { watch, computed, inject } from 'vue'
import { useProjectStore, parseRemoteLabel } from '@/stores/project'
import { useFolderBrowser } from '@/composables/useFolderBrowser'
import { isTauri, pickFolder } from '@/composables/useDesktop'
import type { RemoteMeta } from '@/types/api'
import {
  Dialog,
  DialogPanel,
  DialogTitle,
  TransitionRoot,
  TransitionChild,
} from '@headlessui/vue'
import { FolderIcon, TrashIcon } from '@heroicons/vue/24/outline'
import { useI18n } from 'vue-i18n'

const props = defineProps<{
  open: boolean
}>()

const emit = defineEmits<{
  close: []
  projectSwitched: []
}>()

const projectStore = useProjectStore()
const { t } = useI18n()
const openRemoteConnect = inject<(prefill?: RemoteMeta) => void>('openRemoteConnect')

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

const displayPath = computed(() => browsePath.value || '~')

// Reset the folder browser each time the dialog opens.
watch(() => props.open, (isOpen) => {
  if (isOpen) resetBrowser()
})

function selectCurrentPath() {
  if (!browsePath.value) return
  projectStore.openProject(browsePath.value).then((ok) => {
    if (ok) {
      showBrowser.value = false
      emit('close')
      emit('projectSwitched')
    }
  })
}

// "Open Folder": on the desktop use the native OS picker (same as the composer's
// WorkspacePicker); in the browser, or if the native picker is unavailable, fall
// back to the in-app folder browser.
async function openFolderAction() {
  if (isTauri) {
    try {
      const path = await pickFolder()
      if (!path) return // cancelled
      const ok = await projectStore.openProject(path)
      if (ok) { emit('close'); emit('projectSwitched') }
      return
    } catch {
      /* native picker unavailable → in-app browser */
    }
  }
  openBrowser()
}

async function selectProject(id: string) {
  const project = projectStore.projects.find((p) => p.id === id)
  // Remote workspaces are reconnected through the SSH wizard, not a local switch.
  if (project?.remote) {
    emit('close')
    openRemoteConnect?.(parseRemoteLabel(project.path) ?? undefined)
    return
  }
  const ok = await projectStore.switchToProject(id)
  if (ok) {
    emit('close')
    emit('projectSwitched')
  }
}

function openRemote() {
  emit('close')
  openRemoteConnect?.()
}

function deleteProject(id: string) {
  projectStore.removeProject(id)
}
</script>

<template>
  <TransitionRoot :show="open" as="template">
    <Dialog @close="emit('close')" class="relative z-50">
      <TransitionChild
        enter="ease-out duration-150"
        enter-from="opacity-0"
        enter-to="opacity-100"
        leave="ease-in duration-100"
        leave-from="opacity-100"
        leave-to="opacity-0"
      >
        <div class="fixed inset-0" style="background: var(--backdrop); backdrop-filter: blur(6px); -webkit-backdrop-filter: blur(6px)" />
      </TransitionChild>

      <div class="fixed inset-0 flex items-start justify-center pt-16 px-4">
        <TransitionChild
          enter="ease-out duration-150"
          enter-from="opacity-0 translate-y-2"
          enter-to="opacity-100 translate-y-0"
          leave="ease-in duration-100"
          leave-from="opacity-100 translate-y-0"
          leave-to="opacity-0 translate-y-2"
        >
          <DialogPanel class="w-full max-w-lg overflow-hidden" style="background: var(--color-surface); border: 1px solid var(--color-border); border-radius: var(--radius-xl); box-shadow: var(--shadow-lg)">
            <!-- Header -->
            <div class="px-5 pt-4 pb-2">
              <DialogTitle class="text-sm font-semibold" style="font-family: var(--font-sans); color: var(--color-foreground)">
                {{ showBrowser ? t('projectSwitcher.openFolder') : t('projectSwitcher.title') }}
              </DialogTitle>
            </div>

            <!-- Folder browser mode -->
            <div v-if="showBrowser" class="px-5 pb-4">
              <div class="flex items-center gap-2 mb-3">
                <input
                  v-model="pathInput"
                  type="text"
                  class="ps-input flex-1 px-3 py-1.5 text-sm font-mono rounded-md outline-none"
                  :placeholder="t('projectSwitcher.pathPlaceholder')"
                  @keydown.enter="handlePathSubmit"
                />
                <button
                  class="px-3 py-1.5 text-xs font-medium rounded-md cursor-pointer transition-opacity hover:opacity-90"
                  style="background: var(--color-primary); color: var(--color-on-primary)"
                  @click="handlePathSubmit"
                >
                  OK
                </button>
                <button
                  class="ps-muted-btn px-2 py-1.5 text-xs cursor-pointer transition-colors"
                  @click="showBrowser = false"
                >
                  Back
                </button>
              </div>

              <div class="rounded-md overflow-hidden max-h-80 overflow-y-auto" style="border: 1px solid var(--color-border); background: var(--color-background)">
                <button
                  v-if="browsePath && browsePath !== '/'"
                  class="ps-folder w-full flex items-center gap-2 px-3 py-2 text-sm cursor-pointer transition-colors"
                  style="color: var(--color-muted-foreground); border-bottom: 1px solid var(--color-border)"
                  @click="goUp"
                >
                  <span style="color: var(--color-muted-foreground)">..</span>
                </button>

                <div v-if="browseLoading" class="px-3 py-6 text-center text-xs animate-pulse" style="color: var(--color-muted-foreground)">
                  {{ t('projectSwitcher.loading') }}
                </div>

                <div v-else-if="browseFolders.length === 0" class="px-3 py-6 text-center text-xs" style="color: var(--color-muted-foreground)">
                  {{ t('projectSwitcher.noFolders') }}
                </div>

                <button
                  v-for="folder in browseFolders"
                  :key="folder.path"
                  class="ps-folder w-full flex items-center gap-2 px-3 py-2 text-sm text-left cursor-pointer transition-colors"
                  style="color: var(--color-foreground); border-bottom: 1px solid var(--color-border)"
                  @click="loadFolders(folder.path)"
                >
                  <FolderIcon class="w-3.5 h-3.5 shrink-0" style="color: var(--color-muted-foreground)" />
                  <span class="truncate">{{ folder.name }}</span>
                </button>
              </div>

              <div class="mt-3 flex items-center justify-between">
                <div class="text-[11px] font-mono truncate flex-1 mr-2" style="color: var(--color-muted-foreground)">
                  {{ displayPath }}
                </div>
                <button
                  class="px-4 py-1.5 text-xs font-medium rounded-md cursor-pointer transition-opacity hover:opacity-90 shrink-0"
                  style="background: var(--color-primary); color: var(--color-on-primary)"
                  @click="selectCurrentPath"
                >
                  {{ t('projectSwitcher.openFolder') }}
                </button>
              </div>
            </div>

            <!-- Project list mode -->
            <div v-else>
              <div v-if="projectStore.switchError" class="px-5 py-2">
                <div class="text-xs rounded-md px-3 py-2" style="color: var(--color-error-fg); background: var(--color-error-bg); border: 1px solid var(--color-error-fg)">
                  {{ projectStore.switchError }}
                </div>
              </div>

              <div v-if="projectStore.switching" class="px-3 py-6 text-center text-xs animate-pulse" style="color: var(--color-muted-foreground)">
                {{ t('projectSwitcher.switching') }}
              </div>

              <div v-else class="px-3 pb-2 max-h-72 overflow-y-auto">
                <div v-if="projectStore.projects.length === 0" class="text-xs py-6 text-center" style="color: var(--color-muted-foreground)">
                  {{ t('projectSwitcher.noProjects') }}
                </div>
                <div
                  v-for="p in projectStore.projects"
                  :key="p.id"
                  class="ps-project group flex items-center gap-2 px-3 py-2.5 rounded-md cursor-pointer transition-colors"
                  :class="{ active: projectStore.activeId === p.id }"
                  @click="selectProject(p.id)"
                >
                  <div
                    class="w-7 h-7 rounded-md flex items-center justify-center text-xs font-bold shrink-0"
                    :style="projectStore.activeId === p.id
                      ? { background: 'var(--accent-wash-strong)', color: 'var(--color-primary)' }
                      : { background: 'var(--color-muted)', color: 'var(--color-muted-foreground)' }"
                  >
                    {{ projectStore.projectName(p).charAt(0).toUpperCase() }}
                  </div>
                  <div class="min-w-0 flex-1">
                    <div class="text-sm truncate" style="color: var(--color-foreground)">{{ projectStore.projectName(p) }}</div>
                    <div class="text-[10px] font-mono truncate" style="color: var(--color-muted-foreground)">{{ p.path }}</div>
                  </div>
                  <button
                    class="ps-delete opacity-0 group-hover:opacity-100 p-1 cursor-pointer transition-all"
                    @click.stop="deleteProject(p.id)"
                    :title="t('projectSwitcher.removeProject')"
                  >
                    <TrashIcon class="w-3.5 h-3.5" />
                  </button>
                </div>
              </div>

              <div class="px-5 py-3 flex justify-between items-center" style="border-top: 1px solid var(--color-border)">
                <div class="flex items-center gap-4">
                  <button
                    class="text-xs cursor-pointer transition-opacity hover:opacity-80 font-medium"
                    style="color: var(--color-primary)"
                    @click="openFolderAction"
                  >
                    {{ t('projectSwitcher.openFolderBtn') }}
                  </button>
                  <button
                    class="text-xs cursor-pointer transition-opacity hover:opacity-80 font-medium"
                    style="color: var(--color-primary)"
                    @click="openRemote"
                  >
                    {{ t('nav.remoteConnect') }}
                  </button>
                </div>
                <button
                  class="ps-muted-btn px-3 py-1 text-xs cursor-pointer transition-colors"
                  @click="emit('close')"
                >
                  {{ t('common.close') }}
                </button>
              </div>
            </div>
          </DialogPanel>
        </TransitionChild>
      </div>
    </Dialog>
  </TransitionRoot>
</template>

<style scoped>
.ps-input {
  background: var(--color-background);
  border: 1px solid var(--color-border);
  color: var(--color-foreground);
  transition: border-color 0.15s;
}
.ps-input::placeholder {
  color: var(--color-muted-foreground);
}
.ps-input:focus {
  border-color: var(--color-primary);
}

.ps-muted-btn {
  color: var(--color-muted-foreground);
}
.ps-muted-btn:hover {
  color: var(--color-foreground);
}

.ps-folder:last-child {
  border-bottom: none !important;
}
.ps-folder:hover {
  background: var(--accent-wash-soft);
  color: var(--color-primary) !important;
}

.ps-project {
  border: 1px solid transparent;
}
.ps-project:hover {
  background: var(--color-muted);
}
.ps-project.active {
  background: var(--accent-wash-soft);
  border-color: var(--accent-border);
}

.ps-delete {
  color: var(--color-muted-foreground);
}
.ps-delete:hover {
  color: var(--color-destructive);
}
</style>
