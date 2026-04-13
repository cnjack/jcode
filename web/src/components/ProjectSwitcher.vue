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
}>()

const projectStore = useProjectStore()
const showBrowser = ref(false)
const browsePath = ref('')
const browseFolders = ref<BrowseFolder[]>([])
const browseLoading = ref(false)

// Editable path input
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
  } catch (err: any) {
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
  const proj = projectStore.addProject(browsePath.value)
  projectStore.setActive(proj.id)
  showBrowser.value = false
  emit('close')
}

function selectProject(id: string) {
  projectStore.setActive(id)
  emit('close')
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
        <div class="fixed inset-0 bg-black/20" />
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
          <DialogPanel class="w-full max-w-lg bg-white border border-stone-200 rounded-xl shadow-xl overflow-hidden">
            <!-- Header -->
            <div class="px-5 pt-4 pb-2">
              <DialogTitle class="text-sm font-semibold text-stone-800">
                {{ showBrowser ? 'Open Folder' : 'Projects' }}
              </DialogTitle>
            </div>

            <!-- Folder browser mode -->
            <div v-if="showBrowser" class="px-5 pb-4">
              <!-- Path input -->
              <div class="flex items-center gap-2 mb-3">
                <input
                  v-model="pathInput"
                  type="text"
                  class="flex-1 px-3 py-1.5 text-sm font-mono rounded-lg border border-stone-200 bg-stone-50 text-stone-700 outline-none focus:border-teal-400 transition-colors"
                  placeholder="/path/to/folder"
                  @keydown="handlePathKeyDown"
                />
                <button
                  class="px-3 py-1.5 text-xs font-medium bg-teal-500 hover:bg-teal-600 text-white rounded-lg cursor-pointer transition-colors"
                  @click="handlePathSubmit"
                >
                  OK
                </button>
                <button
                  class="px-2 py-1.5 text-xs text-stone-400 hover:text-stone-600 cursor-pointer transition-colors"
                  @click="showBrowser = false"
                >
                  Back
                </button>
              </div>

              <!-- Folder list -->
              <div class="border border-stone-200 rounded-lg overflow-hidden max-h-80 overflow-y-auto bg-stone-50">
                <!-- Go up -->
                <button
                  v-if="browsePath && browsePath !== '/'"
                  class="w-full flex items-center gap-2 px-3 py-2 text-sm text-stone-500 hover:bg-stone-100 cursor-pointer transition-colors border-b border-stone-100"
                  @click="goUp"
                >
                  <span class="text-stone-400">..</span>
                </button>

                <div v-if="browseLoading" class="px-3 py-6 text-center text-xs text-stone-400 animate-pulse">
                  Loading...
                </div>

                <div v-else-if="browseFolders.length === 0" class="px-3 py-6 text-center text-xs text-stone-400">
                  No folders found
                </div>

                <button
                  v-for="folder in browseFolders"
                  :key="folder.path"
                  class="w-full flex items-center gap-2 px-3 py-2 text-sm text-left text-stone-700 hover:bg-teal-50 hover:text-teal-700 cursor-pointer transition-colors border-b border-stone-100 last:border-0"
                  @click="navigateTo(folder)"
                >
                  <span class="text-stone-400 shrink-0">📁</span>
                  <span class="truncate">{{ folder.name }}</span>
                </button>
              </div>

              <!-- Select current folder -->
              <div class="mt-3 flex items-center justify-between">
                <div class="text-[11px] text-stone-400 font-mono truncate flex-1 mr-2">
                  {{ displayPath }}
                </div>
                <button
                  class="px-4 py-1.5 text-xs font-medium bg-teal-500 hover:bg-teal-600 text-white rounded-lg cursor-pointer transition-colors shrink-0"
                  @click="selectCurrentPath"
                >
                  Open Folder
                </button>
              </div>
            </div>

            <!-- Project list mode -->
            <div v-else>
              <div class="px-3 pb-2 max-h-72 overflow-y-auto">
                <div v-if="projectStore.projects.length === 0" class="text-xs text-stone-400 py-6 text-center">
                  No projects yet
                </div>
                <div
                  v-for="p in projectStore.projects"
                  :key="p.id"
                  class="group flex items-center gap-2 px-3 py-2.5 rounded-lg cursor-pointer transition-colors"
                  :class="projectStore.activeId === p.id
                    ? 'bg-teal-50 border border-teal-200'
                    : 'hover:bg-stone-50 border border-transparent'"
                  @click="selectProject(p.id)"
                >
                  <div
                    class="w-7 h-7 rounded-lg flex items-center justify-center text-xs font-bold shrink-0"
                    :class="projectStore.activeId === p.id
                      ? 'bg-teal-100 text-teal-700'
                      : 'bg-stone-100 text-stone-500'"
                  >
                    {{ projectStore.projectName(p)[0].toUpperCase() }}
                  </div>
                  <div class="min-w-0 flex-1">
                    <div class="text-sm text-stone-700 truncate">{{ projectStore.projectName(p) }}</div>
                    <div class="text-[10px] text-stone-400 font-mono truncate">{{ p.path }}</div>
                  </div>
                  <button
                    class="opacity-0 group-hover:opacity-100 p-1 text-stone-400 hover:text-red-500 cursor-pointer transition-all"
                    @click.stop="deleteProject(p.id)"
                    title="Remove project"
                  >
                    <svg class="w-3.5 h-3.5" viewBox="0 0 20 20" fill="currentColor">
                      <path fill-rule="evenodd" d="M8.75 1A2.75 2.75 0 006 3.75v.443c-.795.077-1.584.176-2.365.298a.75.75 0 10.23 1.482l.149-.022.841 10.518A2.75 2.75 0 007.596 19h4.807a2.75 2.75 0 002.742-2.53l.841-10.52.149.023a.75.75 0 00.23-1.482A41.03 41.03 0 0014 4.193V3.75A2.75 2.75 0 0011.25 1h-2.5zM10 4c.84 0 1.673.025 2.5.075V3.75c0-.69-.56-1.25-1.25-1.25h-2.5c-.69 0-1.25.56-1.25 1.25v.325C8.327 4.025 9.16 4 10 4zM8.58 7.72a.75.75 0 00-1.5.06l.3 7.5a.75.75 0 101.5-.06l-.3-7.5zm4.34.06a.75.75 0 10-1.5-.06l-.3 7.5a.75.75 0 101.5.06l.3-7.5z" clip-rule="evenodd" />
                    </svg>
                  </button>
                </div>
              </div>

              <div class="px-5 py-3 border-t border-stone-100 flex justify-between items-center">
                <button
                  class="text-xs text-teal-600 hover:text-teal-700 cursor-pointer transition-colors"
                  @click="openBrowser"
                >
                  + Open Folder
                </button>
                <button
                  class="px-3 py-1 text-xs text-stone-500 hover:text-stone-700 cursor-pointer transition-colors"
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
