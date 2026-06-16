<script setup lang="ts">
import { ref, watch, computed } from 'vue'
import { useProjectStore } from '@/stores/project'
import { api } from '@/composables/api'
import type { BrowseFolder } from '@/types/api'
import {
  Dialog,
  DialogPanel,
  DialogTitle,
  TransitionRoot,
  TransitionChild,
} from '@headlessui/vue'

const props = defineProps<{
  open: boolean
}>()

const emit = defineEmits<{
  close: []
  projectSwitched: []
}>()

const projectStore = useProjectStore()
const showBrowser = ref(false)
const browsePath = ref('')
const browseFolders = ref<BrowseFolder[]>([])
const browseLoading = ref(false)

const pathInput = ref('')

const displayPath = computed(() => browsePath.value || '~')

watch(() => props.open, (isOpen) => {
  if (isOpen) {
    showBrowser.value = false
    browsePath.value = ''
    pathInput.value = ''
    browseFolders.value = []
  }
})

async function loadFolders(path?: string) {
  browseLoading.value = true
  try {
    const result = await api.browse(path)
    browsePath.value = result.current
    pathInput.value = result.current
    browseFolders.value = result.folders
  } catch (err: unknown) {
    console.error('Browse failed:', err)
    browseFolders.value = []
  } finally {
    browseLoading.value = false
  }
}

function openBrowser() {
  showBrowser.value = true
  loadFolders()
}

function navigateTo(folder: BrowseFolder) {
  loadFolders(folder.path)
}

function goUp() {
  if (!browsePath.value) return
  const parts = browsePath.value.split('/')
  parts.pop()
  const parent = parts.join('/') || '/'
  loadFolders(parent)
}

function handlePathSubmit() {
  const path = pathInput.value.trim()
  if (path) {
    loadFolders(path)
  }
}

function handlePathKeyDown(e: KeyboardEvent) {
  if (e.key === 'Enter') {
    handlePathSubmit()
  }
}

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

async function selectProject(id: string) {
  const ok = await projectStore.switchToProject(id)
  if (ok) {
    emit('close')
    emit('projectSwitched')
  }
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
        <div class="fixed inset-0" style="background: rgba(8,8,8,0.5); backdrop-filter: blur(6px); -webkit-backdrop-filter: blur(6px)" />
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
                {{ showBrowser ? 'Open Folder' : 'Projects' }}
              </DialogTitle>
            </div>

            <!-- Folder browser mode -->
            <div v-if="showBrowser" class="px-5 pb-4">
              <div class="flex items-center gap-2 mb-3">
                <input
                  v-model="pathInput"
                  type="text"
                  class="ps-input flex-1 px-3 py-1.5 text-sm font-mono rounded-md outline-none"
                  placeholder="/path/to/folder"
                  @keydown="handlePathKeyDown"
                />
                <button
                  class="px-3 py-1.5 text-xs font-medium rounded-md cursor-pointer transition-opacity hover:opacity-90"
                  style="background: var(--color-primary); color: var(--color-on-primary, #fff)"
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
                  Loading...
                </div>

                <div v-else-if="browseFolders.length === 0" class="px-3 py-6 text-center text-xs" style="color: var(--color-muted-foreground)">
                  No folders found
                </div>

                <button
                  v-for="folder in browseFolders"
                  :key="folder.path"
                  class="ps-folder w-full flex items-center gap-2 px-3 py-2 text-sm text-left cursor-pointer transition-colors"
                  style="color: var(--color-foreground); border-bottom: 1px solid var(--color-border)"
                  @click="navigateTo(folder)"
                >
                  <span class="shrink-0" style="color: var(--color-muted-foreground)">📁</span>
                  <span class="truncate">{{ folder.name }}</span>
                </button>
              </div>

              <div class="mt-3 flex items-center justify-between">
                <div class="text-[11px] font-mono truncate flex-1 mr-2" style="color: var(--color-muted-foreground)">
                  {{ displayPath }}
                </div>
                <button
                  class="px-4 py-1.5 text-xs font-medium rounded-md cursor-pointer transition-opacity hover:opacity-90 shrink-0"
                  style="background: var(--color-primary); color: var(--color-on-primary, #fff)"
                  @click="selectCurrentPath"
                >
                  Open Folder
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
                Switching project...
              </div>

              <div v-else class="px-3 pb-2 max-h-72 overflow-y-auto">
                <div v-if="projectStore.projects.length === 0" class="text-xs py-6 text-center" style="color: var(--color-muted-foreground)">
                  No projects yet
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
                      ? { background: 'rgba(255,132,0,0.14)', color: 'var(--color-primary)' }
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
                    title="Remove project"
                  >
                    <svg class="w-3.5 h-3.5" viewBox="0 0 20 20" fill="currentColor">
                      <path fill-rule="evenodd" d="M8.75 1A2.75 2.75 0 006 3.75v.443c-.795.077-1.584.176-2.365.298a.75.75 0 10.23 1.482l.149-.022.841 10.518A2.75 2.75 0 007.596 19h4.807a2.75 2.75 0 002.742-2.53l.841-10.52.149.023a.75.75 0 00.23-1.482A41.03 41.03 0 0014 4.193V3.75A2.75 2.75 0 0011.25 1h-2.5zM10 4c.84 0 1.673.025 2.5.075V3.75c0-.69-.56-1.25-1.25-1.25h-2.5c-.69 0-1.25.56-1.25 1.25v.325C8.327 4.025 9.16 4 10 4zM8.58 7.72a.75.75 0 00-1.5.06l.3 7.5a.75.75 0 101.5-.06l-.3-7.5zm4.34.06a.75.75 0 10-1.5-.06l-.3 7.5a.75.75 0 101.5.06l.3-7.5z" clip-rule="evenodd" />
                    </svg>
                  </button>
                </div>
              </div>

              <div class="px-5 py-3 flex justify-between items-center" style="border-top: 1px solid var(--color-border)">
                <button
                  class="text-xs cursor-pointer transition-opacity hover:opacity-80 font-medium"
                  style="color: var(--color-primary)"
                  @click="openBrowser"
                >
                  + Open Folder
                </button>
                <button
                  class="ps-muted-btn px-3 py-1 text-xs cursor-pointer transition-colors"
                  @click="emit('close')"
                >
                  Close
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
  background: rgba(255, 132, 0, 0.08);
  color: var(--color-primary) !important;
}

.ps-project {
  border: 1px solid transparent;
}
.ps-project:hover {
  background: var(--color-muted);
}
.ps-project.active {
  background: rgba(255, 132, 0, 0.08);
  border-color: rgba(255, 132, 0, 0.3);
}

.ps-delete {
  color: var(--color-muted-foreground);
}
.ps-delete:hover {
  color: var(--color-destructive);
}
</style>
