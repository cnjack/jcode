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
        <div class="fixed inset-0 bg-black/40 dark:bg-black/60 backdrop-blur-sm" />
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
          <DialogPanel class="w-full max-w-lg bg-white dark:bg-zinc-900 border border-zinc-200 dark:border-zinc-700 rounded shadow-2xl overflow-hidden">
            <!-- Header -->
            <div class="px-5 pt-4 pb-2">
              <DialogTitle class="text-sm font-semibold text-zinc-800 dark:text-zinc-100" style="font-family: var(--font-sans)">
                {{ showBrowser ? 'Open Folder' : 'Projects' }}
              </DialogTitle>
            </div>

            <!-- Folder browser mode -->
            <div v-if="showBrowser" class="px-5 pb-4">
              <div class="flex items-center gap-2 mb-3">
                <input
                  v-model="pathInput"
                  type="text"
                  class="flex-1 px-3 py-1.5 text-sm font-mono rounded-md border border-zinc-200 dark:border-zinc-700 bg-zinc-50 dark:bg-zinc-800 text-zinc-700 dark:text-zinc-200 outline-none focus:border-emerald-400 dark:focus:border-emerald-500/60 transition-colors"
                  placeholder="/path/to/folder"
                  @keydown="handlePathKeyDown"
                />
                <button
                  class="px-3 py-1.5 text-xs font-medium bg-emerald-500 hover:bg-emerald-600 text-white rounded-md cursor-pointer transition-colors"
                  @click="handlePathSubmit"
                >
                  OK
                </button>
                <button
                  class="px-2 py-1.5 text-xs text-zinc-400 dark:text-zinc-500 hover:text-zinc-600 dark:hover:text-zinc-300 cursor-pointer transition-colors"
                  @click="showBrowser = false"
                >
                  Back
                </button>
              </div>

              <div class="border border-zinc-200 dark:border-zinc-700 rounded-md overflow-hidden max-h-80 overflow-y-auto bg-zinc-50 dark:bg-zinc-800/60">
                <button
                  v-if="browsePath && browsePath !== '/'"
                  class="w-full flex items-center gap-2 px-3 py-2 text-sm text-zinc-500 dark:text-zinc-400 hover:bg-zinc-100 dark:hover:bg-zinc-700 cursor-pointer transition-colors border-b border-zinc-100 dark:border-zinc-700/60"
                  @click="goUp"
                >
                  <span class="text-zinc-400 dark:text-zinc-500">..</span>
                </button>

                <div v-if="browseLoading" class="px-3 py-6 text-center text-xs text-zinc-400 dark:text-zinc-500 animate-pulse">
                  Loading...
                </div>

                <div v-else-if="browseFolders.length === 0" class="px-3 py-6 text-center text-xs text-zinc-400 dark:text-zinc-500">
                  No folders found
                </div>

                <button
                  v-for="folder in browseFolders"
                  :key="folder.path"
                  class="w-full flex items-center gap-2 px-3 py-2 text-sm text-left text-zinc-700 dark:text-zinc-300 hover:bg-emerald-50 dark:hover:bg-emerald-500/10 hover:text-emerald-700 dark:hover:text-emerald-400 cursor-pointer transition-colors border-b border-zinc-100 dark:border-zinc-700/60 last:border-0"
                  @click="navigateTo(folder)"
                >
                  <span class="text-zinc-400 dark:text-zinc-500 shrink-0">📁</span>
                  <span class="truncate">{{ folder.name }}</span>
                </button>
              </div>

              <div class="mt-3 flex items-center justify-between">
                <div class="text-[11px] text-zinc-400 dark:text-zinc-500 font-mono truncate flex-1 mr-2">
                  {{ displayPath }}
                </div>
                <button
                  class="px-4 py-1.5 text-xs font-medium bg-emerald-500 hover:bg-emerald-600 text-white rounded-md cursor-pointer transition-colors shrink-0"
                  @click="selectCurrentPath"
                >
                  Open Folder
                </button>
              </div>
            </div>

            <!-- Project list mode -->
            <div v-else>
              <div v-if="projectStore.switchError" class="px-5 py-2">
                <div class="text-xs text-red-500 dark:text-red-400 bg-red-50 dark:bg-red-500/10 border border-red-200 dark:border-red-500/20 rounded-md px-3 py-2">
                  {{ projectStore.switchError }}
                </div>
              </div>

              <div v-if="projectStore.switching" class="px-3 py-6 text-center text-xs text-zinc-400 dark:text-zinc-500 animate-pulse">
                Switching project...
              </div>

              <div v-else class="px-3 pb-2 max-h-72 overflow-y-auto">
                <div v-if="projectStore.projects.length === 0" class="text-xs text-zinc-400 dark:text-zinc-500 py-6 text-center">
                  No projects yet
                </div>
                <div
                  v-for="p in projectStore.projects"
                  :key="p.id"
                  class="group flex items-center gap-2 px-3 py-2.5 rounded-md cursor-pointer transition-colors"
                  :class="projectStore.activeId === p.id
                    ? 'bg-emerald-50 dark:bg-emerald-500/10 border border-emerald-200 dark:border-emerald-500/20'
                    : 'hover:bg-zinc-50 dark:hover:bg-zinc-800 border border-transparent'"
                  @click="selectProject(p.id)"
                >
                  <div
                    class="w-7 h-7 rounded-md flex items-center justify-center text-xs font-bold shrink-0"
                    :class="projectStore.activeId === p.id
                      ? 'bg-emerald-100 dark:bg-emerald-500/20 text-emerald-700 dark:text-emerald-400'
                      : 'bg-zinc-100 dark:bg-zinc-800 text-zinc-500 dark:text-zinc-400'"
                  >
                    {{ projectStore.projectName(p).charAt(0).toUpperCase() }}
                  </div>
                  <div class="min-w-0 flex-1">
                    <div class="text-sm text-zinc-700 dark:text-zinc-200 truncate">{{ projectStore.projectName(p) }}</div>
                    <div class="text-[10px] text-zinc-400 dark:text-zinc-500 font-mono truncate">{{ p.path }}</div>
                  </div>
                  <button
                    class="opacity-0 group-hover:opacity-100 p-1 text-zinc-400 dark:text-zinc-500 hover:text-red-500 dark:hover:text-red-400 cursor-pointer transition-all"
                    @click.stop="deleteProject(p.id)"
                    title="Remove project"
                  >
                    <svg class="w-3.5 h-3.5" viewBox="0 0 20 20" fill="currentColor">
                      <path fill-rule="evenodd" d="M8.75 1A2.75 2.75 0 006 3.75v.443c-.795.077-1.584.176-2.365.298a.75.75 0 10.23 1.482l.149-.022.841 10.518A2.75 2.75 0 007.596 19h4.807a2.75 2.75 0 002.742-2.53l.841-10.52.149.023a.75.75 0 00.23-1.482A41.03 41.03 0 0014 4.193V3.75A2.75 2.75 0 0011.25 1h-2.5zM10 4c.84 0 1.673.025 2.5.075V3.75c0-.69-.56-1.25-1.25-1.25h-2.5c-.69 0-1.25.56-1.25 1.25v.325C8.327 4.025 9.16 4 10 4zM8.58 7.72a.75.75 0 00-1.5.06l.3 7.5a.75.75 0 101.5-.06l-.3-7.5zm4.34.06a.75.75 0 10-1.5-.06l-.3 7.5a.75.75 0 101.5.06l.3-7.5z" clip-rule="evenodd" />
                    </svg>
                  </button>
                </div>
              </div>

              <div class="px-5 py-3 border-t border-zinc-100 dark:border-zinc-800 flex justify-between items-center">
                <button
                  class="text-xs text-emerald-600 dark:text-emerald-400 hover:text-emerald-700 dark:hover:text-emerald-300 cursor-pointer transition-colors font-medium"
                  @click="openBrowser"
                >
                  + Open Folder
                </button>
                <button
                  class="px-3 py-1 text-xs text-zinc-500 dark:text-zinc-400 hover:text-zinc-700 dark:hover:text-zinc-200 cursor-pointer transition-colors"
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
