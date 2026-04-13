<script setup lang="ts">
import { ref } from 'vue'
import { useProjectStore } from '@/stores/project'
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
const newName = ref('')
const newPath = ref('')
const showAddForm = ref(false)

function addProject() {
  const name = newName.value.trim()
  const path = newPath.value.trim()
  if (!name || !path) return

  projectStore.addProject(name, path)
  newName.value = ''
  newPath.value = ''
  showAddForm.value = false
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

      <div class="fixed inset-0 flex items-start justify-center pt-20 px-4">
        <TransitionChild
          enter="ease-out duration-150"
          enter-from="opacity-0 translate-y-2"
          enter-to="opacity-100 translate-y-0"
          leave="ease-in duration-100"
          leave-from="opacity-100 translate-y-0"
          leave-to="opacity-0 translate-y-2"
        >
          <DialogPanel class="w-full max-w-md bg-white border border-stone-200 rounded-xl p-5 shadow-xl">
            <DialogTitle class="text-sm font-semibold text-stone-800 mb-3">Projects</DialogTitle>

            <!-- Project list -->
            <div class="space-y-1 mb-4 max-h-60 overflow-y-auto">
              <div v-if="projectStore.projects.length === 0" class="text-xs text-stone-400 py-4 text-center">
                No projects yet
              </div>
              <div
                v-for="p in projectStore.projects"
                :key="p.id"
                class="group flex items-center gap-2 px-3 py-2 rounded-lg cursor-pointer transition-colors"
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
                  {{ p.name[0].toUpperCase() }}
                </div>
                <div class="min-w-0 flex-1">
                  <div class="text-sm text-stone-700 truncate">{{ p.name }}</div>
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

            <!-- Add form -->
            <div v-if="showAddForm" class="border-t border-stone-200 pt-3 space-y-2">
              <input
                v-model="newName"
                type="text"
                placeholder="Project name"
                class="w-full px-3 py-1.5 text-sm rounded-lg border border-stone-200 bg-stone-50 text-stone-700 outline-none focus:border-teal-400 transition-colors"
              />
              <input
                v-model="newPath"
                type="text"
                placeholder="/path/to/project"
                class="w-full px-3 py-1.5 text-sm rounded-lg border border-stone-200 bg-stone-50 text-stone-700 font-mono outline-none focus:border-teal-400 transition-colors"
              />
              <div class="flex gap-2 justify-end">
                <button
                  class="px-3 py-1 text-xs text-stone-500 hover:text-stone-700 cursor-pointer transition-colors"
                  @click="showAddForm = false"
                >
                  Cancel
                </button>
                <button
                  class="px-3 py-1 text-xs bg-teal-500 hover:bg-teal-600 text-white rounded-md cursor-pointer transition-colors"
                  @click="addProject"
                >
                  Add
                </button>
              </div>
            </div>

            <div v-else class="flex justify-between items-center pt-2 border-t border-stone-100">
              <button
                class="text-xs text-teal-600 hover:text-teal-700 cursor-pointer transition-colors"
                @click="showAddForm = true"
              >
                + Add project
              </button>
              <button
                class="px-3 py-1 text-xs text-stone-500 hover:text-stone-700 cursor-pointer transition-colors"
                @click="emit('close')"
              >
                Close
              </button>
            </div>
          </DialogPanel>
        </TransitionChild>
      </div>
    </Dialog>
  </TransitionRoot>
</template>
