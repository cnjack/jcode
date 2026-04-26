<script setup lang="ts">
import { ref, nextTick, watch, computed, onMounted, onUnmounted } from 'vue'
import { useChatStore } from '@/stores/chat'
import { api } from '@/composables/api'
import type { SkillInfo, ChatImage } from '@/types/api'

const store = useChatStore()
const input = ref('')
const textarea = ref<HTMLTextAreaElement | null>(null)
const showModelPicker = ref(false)
const showModePicker = ref(false)
const showManageModels = ref(false)
const modelFilter = ref('')
const containerRef = ref<HTMLDivElement | null>(null)

const skills = ref<SkillInfo[]>([])
const showSlashMenu = ref(false)
const slashFilter = ref('')
const selectedSlashIdx = ref(0)

// Image attachment state
const pendingImages = ref<ChatImage[]>([])
const pendingImagePreviews = ref<string[]>([])
const fileInput = ref<HTMLInputElement | null>(null)

const modes = [
  { value: 'agent' as const, label: 'Agent', icon: '🔥' },
  { value: 'plan' as const, label: 'Plan', icon: '📋' },
]

const filteredSlashCommands = computed(() => {
  const filter = slashFilter.value.toLowerCase()
  return skills.value.filter(
    (s) => s.slash && s.slash.toLowerCase().includes(filter),
  )
})

const filteredProviders = computed(() => {
  const filter = modelFilter.value.toLowerCase()
  if (!filter) return store.providers

  return store.providers
    .map(p => ({
      ...p,
      models: p.models.filter(m =>
        (m.name || m.id).toLowerCase().includes(filter) ||
        p.name.toLowerCase().includes(filter)
      )
    }))
    .filter(p => p.models.length > 0)
})

// Get full display name for a model (e.g., "DeepSeek V4 Pro")
function getModelDisplayName(providerId: string, modelId: string): string {
  for (const p of store.providers) {
    if (p.id === providerId) {
      const m = p.models.find(model => model.id === modelId)
      return m?.name || modelId
    }
  }
  return modelId
}

function autoResize() {
  const el = textarea.value
  if (!el) return
  el.style.height = 'auto'
  el.style.height = Math.min(el.scrollHeight, 160) + 'px'
}

function handleKeyDown(e: KeyboardEvent) {
  // Handle ESC key for dialogs
  if (e.key === 'Escape') {
    if (showManageModels.value) {
      e.preventDefault()
      showManageModels.value = false
      modelFilter.value = ''
      return
    }
    if (showModelPicker.value) {
      e.preventDefault()
      showModelPicker.value = false
      return
    }
    if (showSlashMenu.value) {
      e.preventDefault()
      showSlashMenu.value = false
      return
    }
  }

  if (showSlashMenu.value) {
    if (e.key === 'ArrowDown') {
      e.preventDefault()
      selectedSlashIdx.value = Math.min(selectedSlashIdx.value + 1, filteredSlashCommands.value.length - 1)
      return
    }
    if (e.key === 'ArrowUp') {
      e.preventDefault()
      selectedSlashIdx.value = Math.max(selectedSlashIdx.value - 1, 0)
      return
    }
    if (e.key === 'Enter' || e.key === 'Tab') {
      const cmd = filteredSlashCommands.value[selectedSlashIdx.value]
      if (cmd) {
        e.preventDefault()
        applySlashCommand(cmd)
        return
      }
    }
    if (e.key === 'Escape') {
      e.preventDefault()
      showSlashMenu.value = false
      return
    }
  }

  if (e.key === 'Enter' && !e.shiftKey) {
    e.preventDefault()
    send()
  }
}

function handlePaste(e: ClipboardEvent) {
  if (!store.imageSupport) return
  const items = e.clipboardData?.items
  if (!items) return
  for (const item of Array.from(items)) {
    if (!item.type.startsWith('image/')) continue
    e.preventDefault()
    const file = item.getAsFile()
    if (!file) continue
    const reader = new FileReader()
    reader.onload = () => {
      const result = reader.result as string
      const commaIdx = result.indexOf(',')
      if (commaIdx < 0) return
      const base64Data = result.substring(commaIdx + 1)
      pendingImages.value.push({ data: base64Data, media_type: file.type })
      pendingImagePreviews.value.push(result)
    }
    reader.readAsDataURL(file)
  }
}

function handleInput() {
  autoResize()
  const text = input.value
  if (text.startsWith('/')) {
    slashFilter.value = text.slice(1)
    showSlashMenu.value = true
    selectedSlashIdx.value = 0
  } else {
    showSlashMenu.value = false
  }
}

function applySlashCommand(skill: SkillInfo) {
  input.value = skill.slash + ' '
  showSlashMenu.value = false
  nextTick(() => textarea.value?.focus())
}

async function send() {
  const text = input.value.trim()
  if ((!text && pendingImages.value.length === 0) || store.isRunning) return
  const images = pendingImages.value.length > 0 ? [...pendingImages.value] : undefined
  input.value = ''
  pendingImages.value = []
  pendingImagePreviews.value = []
  showSlashMenu.value = false
  await nextTick()
  autoResize()
  store.sendMessage(text || '(see attached images)', images)
}

function selectModel(provider: string, model: string) {
  showModelPicker.value = false
  store.switchModel(provider, model)
}

function triggerImageUpload() {
  fileInput.value?.click()
}

function handleImageSelect(e: Event) {
  const target = e.target as HTMLInputElement
  const files = target.files
  if (!files) return
  for (const file of Array.from(files)) {
    if (!file.type.startsWith('image/')) continue
    if (file.size > 10 * 1024 * 1024) continue // 10MB limit
    const reader = new FileReader()
    reader.onload = () => {
      const result = reader.result as string
      // result is "data:<media_type>;base64,<data>"
      const commaIdx = result.indexOf(',')
      if (commaIdx < 0) return
      const base64Data = result.substring(commaIdx + 1)
      pendingImages.value.push({ data: base64Data, media_type: file.type })
      pendingImagePreviews.value.push(result)
    }
    reader.readAsDataURL(file)
  }
  // Reset input so the same file can be re-selected
  target.value = ''
}

function removeImage(index: number) {
  pendingImages.value.splice(index, 1)
  pendingImagePreviews.value.splice(index, 1)
}

function selectMode(mode: 'agent' | 'plan') {
  showModePicker.value = false
  store.switchMode(mode)
}

function handleClickOutside(e: MouseEvent) {
  if (containerRef.value && !containerRef.value.contains(e.target as Node)) {
    showModelPicker.value = false
    showModePicker.value = false
    showSlashMenu.value = false
    if (showManageModels.value) {
      showManageModels.value = false
      modelFilter.value = ''
    }
  }
}

function handleGlobalKey(e: KeyboardEvent) {
  if ((e.ctrlKey || e.metaKey) && e.key === 'l') {
    e.preventDefault()
    textarea.value?.focus()
  }
  // Global ESC handler for dialogs
  if (e.key === 'Escape') {
    if (showManageModels.value) {
      e.preventDefault()
      showManageModels.value = false
      modelFilter.value = ''
      return
    }
    if (showModelPicker.value) {
      e.preventDefault()
      showModelPicker.value = false
      return
    }
  }
}

onMounted(async () => {
  document.addEventListener('click', handleClickOutside)
  document.addEventListener('keydown', handleGlobalKey)
  try {
    skills.value = await api.skillsList()
  } catch { /* ignore */ }
})

onUnmounted(() => {
  document.removeEventListener('click', handleClickOutside)
  document.removeEventListener('keydown', handleGlobalKey)
})

watch(() => store.isRunning, (running) => {
  if (!running) nextTick(() => textarea.value?.focus())
})
</script>

<template>
  <div ref="containerRef" class="border-t border-zinc-200 dark:border-zinc-800/80 bg-white/80 dark:bg-zinc-900/80 backdrop-blur-sm px-5 py-3">
    <div class="max-w-3xl mx-auto">
      <div class="bg-zinc-50 dark:bg-zinc-800/60 border border-zinc-200 dark:border-zinc-700/60 rounded-md px-3.5 py-2.5 transition-all relative">
        <!-- Slash command menu -->
        <div
          v-if="showSlashMenu && filteredSlashCommands.length > 0"
          class="absolute bottom-full mb-2 left-0 right-0 z-30 bg-white dark:bg-zinc-800 border border-zinc-200 dark:border-zinc-700 rounded-md shadow-lg dark:shadow-2xl py-1.5 max-h-48 overflow-y-auto"
        >
          <button
            v-for="(cmd, i) in filteredSlashCommands"
            :key="cmd.name"
            class="w-full px-3.5 py-2 text-left flex items-start gap-2.5 cursor-pointer transition-colors"
            :class="i === selectedSlashIdx
              ? 'bg-emerald-50 dark:bg-emerald-500/10'
              : 'hover:bg-zinc-50 dark:hover:bg-zinc-700/50'"
            @click="applySlashCommand(cmd)"
            @mouseenter="selectedSlashIdx = i"
          >
            <span class="text-xs font-mono text-emerald-600 dark:text-emerald-400 shrink-0">{{ cmd.slash }}</span>
            <span class="text-[11px] text-zinc-500 dark:text-zinc-400 truncate">{{ cmd.description }}</span>
          </button>
        </div>

        <!-- Image previews -->
        <div v-if="pendingImagePreviews.length > 0" class="flex flex-wrap gap-2 mb-2">
          <div v-for="(preview, i) in pendingImagePreviews" :key="i" class="relative group">
            <img :src="preview" class="w-16 h-16 object-cover rounded border border-zinc-200 dark:border-zinc-700" />
            <button
              class="absolute -top-1.5 -right-1.5 w-4 h-4 rounded-full bg-red-500 text-white text-[9px] flex items-center justify-center opacity-0 group-hover:opacity-100 transition-opacity cursor-pointer"
              @click="removeImage(i)"
            >✕</button>
          </div>
        </div>

        <textarea
          ref="textarea"
          v-model="input"
          :placeholder="store.isRunning ? 'Agent is working…' : 'Ask anything… (/ for commands)'"
          rows="1"
          :disabled="store.isRunning"
          class="w-full bg-transparent text-zinc-800 dark:text-zinc-100 text-sm resize-none outline-none placeholder-zinc-400 dark:placeholder-zinc-500 min-h-6 max-h-40 leading-relaxed disabled:opacity-50"
          @keydown="handleKeyDown"
          @input="handleInput"
          @paste="handlePaste"
        />

        <!-- Hidden file input for image upload -->
        <input
          ref="fileInput"
          type="file"
          accept="image/*"
          multiple
          class="hidden"
          @change="handleImageSelect"
        />
        <!-- Toolbar row -->
        <div class="flex items-center justify-between mt-1.5 pt-1.5 border-t border-zinc-200/60 dark:border-zinc-700/40">
          <div class="flex items-center gap-1.5">
            <!-- Image attach "+" button -->
            <button
              class="w-6 h-6 flex items-center justify-center rounded border transition-colors shrink-0"
              :class="[
                store.imageSupport ? 'cursor-pointer' : 'cursor-not-allowed opacity-40',
                pendingImages.length > 0
                  ? 'border-blue-400 dark:border-blue-500 bg-blue-50 dark:bg-blue-500/15 text-blue-600 dark:text-blue-400'
                  : 'border-zinc-300 dark:border-zinc-600 text-zinc-400 dark:text-zinc-500 hover:text-zinc-600 dark:hover:text-zinc-300 hover:border-zinc-400 dark:hover:border-zinc-500 hover:bg-zinc-100 dark:hover:bg-zinc-700/60'
              ]"
              :title="!store.imageSupport ? 'Current model does not support images' : pendingImages.length > 0 ? `${pendingImages.length} image(s) attached — click to add more` : 'Attach images'"
              :disabled="!store.imageSupport"
              @click="store.imageSupport && triggerImageUpload()"
            >
              <svg v-if="pendingImages.length === 0" class="w-3.5 h-3.5" viewBox="0 0 20 20" fill="currentColor">
                <path d="M10 5a1 1 0 011 1v3h3a1 1 0 110 2h-3v3a1 1 0 11-2 0v-3H6a1 1 0 110-2h3V6a1 1 0 011-1z" />
              </svg>
              <span v-else class="text-[10px] font-bold">{{ pendingImages.length }}</span>
            </button>

            <!-- Mode selector -->
            <div class="relative">
              <button
                class="flex items-center gap-1 px-2 py-0.5 text-[11px] rounded transition-colors cursor-pointer"
                :class="store.mode === 'plan'
                  ? 'bg-amber-100 dark:bg-amber-500/15 text-amber-600 dark:text-amber-400'
                  : 'bg-zinc-100 dark:bg-zinc-700/60 text-zinc-500 dark:text-zinc-400 hover:text-zinc-700 dark:hover:text-zinc-200'"
                @click.stop="showModePicker = !showModePicker; showModelPicker = false"
              >
                {{ store.mode === 'agent' ? '🔥 Agent' : '📋 Plan' }}
                <svg class="w-3 h-3 opacity-50" viewBox="0 0 20 20" fill="currentColor">
                  <path fill-rule="evenodd" d="M5.23 7.21a.75.75 0 011.06.02L10 11.168l3.71-3.938a.75.75 0 111.08 1.04l-4.25 4.5a.75.75 0 01-1.08 0l-4.25-4.5a.75.75 0 01.02-1.06z" clip-rule="evenodd" />
                </svg>
              </button>
              <div v-if="showModePicker" class="absolute bottom-full mb-1 left-0 z-20 bg-white dark:bg-zinc-800 border border-zinc-200 dark:border-zinc-700 rounded-md shadow-lg dark:shadow-2xl py-1 min-w-28">
                <button
                  v-for="m in modes"
                  :key="m.value"
                  class="w-full px-3 py-1.5 text-xs cursor-pointer select-none text-left transition-colors rounded"
                  :class="store.mode === m.value
                    ? 'text-emerald-600 dark:text-emerald-400 bg-emerald-50 dark:bg-emerald-500/10'
                    : 'text-zinc-500 dark:text-zinc-400 hover:bg-zinc-50 dark:hover:bg-zinc-700/50 hover:text-zinc-700 dark:hover:text-zinc-200'"
                  @click="selectMode(m.value)"
                >
                  {{ m.icon }} {{ m.label }}
                </button>
              </div>
            </div>

            <!-- Model selector -->
            <div class="relative">
              <button
                class="flex items-center gap-1 px-2 py-0.5 text-[11px] rounded bg-zinc-100 dark:bg-zinc-700/60 text-zinc-500 dark:text-zinc-400 hover:text-zinc-700 dark:hover:text-zinc-200 cursor-pointer transition-colors"
                @click.stop="showModelPicker = !showModelPicker; showModePicker = false"
              >
                {{ store.modelName || 'model' }}
                <svg class="w-3 h-3 opacity-50" viewBox="0 0 20 20" fill="currentColor">
                  <path fill-rule="evenodd" d="M5.23 7.21a.75.75 0 011.06.02L10 11.168l3.71-3.938a.75.75 0 111.08 1.04l-4.25 4.5a.75.75 0 01-1.08 0l-4.25-4.5a.75.75 0 01.02-1.06z" clip-rule="evenodd" />
                </svg>
              </button>
              <div
                v-if="showModelPicker"
                class="absolute bottom-full mb-1 left-0 z-20 bg-white dark:bg-zinc-800 border border-zinc-200 dark:border-zinc-700 rounded-md shadow-lg dark:shadow-2xl py-1.5 max-h-72 overflow-y-auto min-w-56"
              >
                <!-- Favorites section -->
                <template v-if="store.recentModels.length > 0 && store.favoriteModels.size > 0">
                  <div class="px-3 py-1 text-[10px] text-amber-500 dark:text-amber-400 uppercase tracking-wider font-semibold sticky top-0 bg-white dark:bg-zinc-800 flex items-center gap-1">
                    <span>★</span> Favorites
                  </div>
                  <button
                    v-for="r in store.recentModels.filter(r => store.favoriteModels.has(`${r.provider}/${r.model}`) && !(store.providerName === r.provider && store.modelName === r.model))"
                    :key="'fav-'+r.provider+'-'+r.model"
                    class="w-full px-3 py-1.5 text-xs text-left cursor-pointer select-none truncate transition-colors text-zinc-500 dark:text-zinc-400 hover:bg-zinc-50 dark:hover:bg-zinc-700/50 hover:text-zinc-700 dark:hover:text-zinc-200"
                    @click="selectModel(r.provider, r.model)"
                  >
                    <span class="text-amber-400 mr-1">★</span>{{ getModelDisplayName(r.provider, r.model) }}
                  </button>
                </template>

                <!-- Current Model section -->
                <template v-if="store.providerName && store.modelName">
                  <div class="px-3 py-1 text-[10px] text-zinc-400 dark:text-zinc-500 uppercase tracking-wider font-semibold sticky top-0 bg-white dark:bg-zinc-800 border-t border-zinc-100 dark:border-zinc-700/50">
                    Current Model
                  </div>
                  <button
                    class="w-full px-3 py-1.5 text-xs text-left cursor-pointer select-none truncate transition-colors text-emerald-600 dark:text-emerald-400 bg-emerald-50 dark:bg-emerald-500/10"
                    @click="selectModel(store.providerName, store.modelName)"
                  >
                    ● {{ getModelDisplayName(store.providerName, store.modelName) }}
                  </button>
                </template>

                <!-- All providers section (only enabled models) -->
                <template v-for="p in store.enabledProviders" :key="p.id">
                  <div class="px-3 py-1 text-[10px] text-zinc-400 dark:text-zinc-500 uppercase tracking-wider font-semibold sticky top-0 bg-white dark:bg-zinc-800 border-t border-zinc-100 dark:border-zinc-700/50">
                    {{ p.name }}
                  </div>
                  <button
                    v-for="m in p.models"
                    :key="m.id"
                    class="w-full px-3 py-1.5 text-xs text-left cursor-pointer select-none transition-colors group"
                    :class="store.providerName === p.id && store.modelName === m.id
                      ? 'text-emerald-600 dark:text-emerald-400 bg-emerald-50 dark:bg-emerald-500/10'
                      : 'text-zinc-500 dark:text-zinc-400 hover:bg-zinc-50 dark:hover:bg-zinc-700/50 hover:text-zinc-700 dark:hover:text-zinc-200'"
                    @click="selectModel(p.id, m.id)"
                  >
                    <span class="truncate">{{ m.name || m.id }}</span>
                    <span v-if="m.recommended" class="ml-1 text-[9px] text-emerald-500 dark:text-emerald-400">recommended</span>
                    <button
                      class="ml-1 opacity-0 group-hover:opacity-100 transition-opacity cursor-pointer inline"
                      :class="store.isFavorite(p.id, m.id) ? 'text-amber-400 opacity-100' : 'text-zinc-300 dark:text-zinc-600'"
                      @click.stop="store.toggleFavorite(p.id, m.id)"
                      :title="store.isFavorite(p.id, m.id) ? 'Remove from favorites' : 'Add to favorites'"
                    >★</button>
                  </button>
                </template>
                <div v-if="store.enabledProviders.length === 0" class="px-3 py-2 text-xs text-zinc-400 dark:text-zinc-500">
                  No models available
                </div>
                <!-- Manage models link -->
                <div class="border-t border-zinc-100 dark:border-zinc-700/50 px-3 py-1.5">
                  <button
                    class="w-full text-xs text-left text-zinc-400 dark:text-zinc-500 hover:text-zinc-600 dark:hover:text-zinc-300 cursor-pointer transition-colors"
                    @click.stop="showModelPicker = false; showManageModels = true"
                  >
                    ⚙ Manage models…
                  </button>
                </div>
              </div>
            </div>

            <!-- Manage Models Dialog (teleported to body for proper centering) -->
            <Teleport to="body">
              <div
                v-if="showManageModels"
                class="fixed inset-0 z-50 flex items-center justify-center bg-black/30 dark:bg-black/50"
                @click="showManageModels = false; modelFilter = ''"
              >
                <div class="bg-white dark:bg-zinc-800 border border-zinc-200 dark:border-zinc-700 rounded-lg shadow-xl dark:shadow-2xl w-full max-w-lg max-h-[70vh] flex flex-col mx-4" @click.stop>
                  <div class="px-4 py-3 border-b border-zinc-200 dark:border-zinc-700">
                    <div class="flex items-center justify-between mb-2">
                      <div>
                        <h3 class="text-sm font-semibold text-zinc-800 dark:text-zinc-100">Manage Models</h3>
                        <p class="text-[11px] text-zinc-400 dark:text-zinc-500">Toggle which models appear in the model selector</p>
                      </div>
                      <button
                        class="text-zinc-400 hover:text-zinc-600 dark:hover:text-zinc-200 cursor-pointer"
                        @click="showManageModels = false; modelFilter = ''; store.fetchModels()"
                      >✕</button>
                    </div>
                    <input
                      v-model="modelFilter"
                      type="text"
                      placeholder="Filter models..."
                      class="w-full px-2 py-1.5 text-xs border border-zinc-200 dark:border-zinc-600 rounded bg-white dark:bg-zinc-700 text-zinc-800 dark:text-zinc-100 placeholder-zinc-400 dark:placeholder-zinc-500 focus:outline-none focus:ring-1 focus:ring-emerald-500"
                    />
                  </div>
                  <div class="overflow-y-auto flex-1 py-2">
                    <template v-for="p in filteredProviders" :key="'mgr-'+p.id">
                      <div class="px-4 py-1.5 text-[10px] text-zinc-400 dark:text-zinc-500 uppercase tracking-wider font-semibold sticky top-0 bg-white dark:bg-zinc-800">
                        {{ p.name }}
                      </div>
                      <label
                        v-for="m in p.models"
                        :key="'mgr-'+p.id+'-'+m.id"
                        class="flex items-center gap-2.5 px-4 py-2 hover:bg-zinc-50 dark:hover:bg-zinc-700/30 cursor-pointer transition-colors"
                      >
                        <input
                          type="checkbox"
                          :checked="m.enabled !== false"
                          class="w-4 h-4 rounded border-2 border-zinc-300 dark:border-zinc-600 text-emerald-500 focus:ring-2 focus:ring-emerald-500 focus:ring-offset-0 cursor-pointer transition-all"
                          @change="store.toggleModelEnabled(p.id, m.id, ($event.target as HTMLInputElement).checked)"
                        />
                        <span class="text-xs text-zinc-700 dark:text-zinc-300 flex-1 truncate">{{ m.name || m.id }}</span>
                        <span v-if="m.recommended" class="text-[9px] px-1.5 py-0.5 rounded bg-emerald-50 dark:bg-emerald-500/10 text-emerald-600 dark:text-emerald-400 font-medium shrink-0">recommended</span>
                      </label>
                    </template>
                  </div>
                </div>
              </div>
            </Teleport>

            <!-- Auto-approve toggle -->
            <button
              class="flex items-center gap-1 px-2 py-0.5 text-[11px] rounded transition-colors cursor-pointer"
              :class="store.autoApprove
                ? 'bg-emerald-100 dark:bg-emerald-500/15 text-emerald-600 dark:text-emerald-400 hover:bg-emerald-200 dark:hover:bg-emerald-500/25'
                : 'bg-zinc-100 dark:bg-zinc-700/60 text-zinc-400 dark:text-zinc-500 hover:text-zinc-600 dark:hover:text-zinc-300 hover:bg-zinc-200 dark:hover:bg-zinc-600/60'"
              :title="store.autoApprove ? 'Auto-approve ON' : 'Auto-approve OFF'"
              @click="store.setAutoApprove(!store.autoApprove)"
            >
              <svg class="w-3 h-3" viewBox="0 0 20 20" fill="currentColor">
                <path fill-rule="evenodd" d="M16.704 4.153a.75.75 0 01.143 1.052l-8 10.5a.75.75 0 01-1.127.075l-4.5-4.5a.75.75 0 011.06-1.06l3.894 3.893 7.48-9.817a.75.75 0 011.05-.143z" clip-rule="evenodd" />
              </svg>
              Auto
            </button>

            <!-- Channel toggle -->
            <button
              v-if="store.channelAvailable"
              class="flex items-center gap-1 px-2 py-0.5 text-[11px] rounded transition-colors cursor-pointer"
              :class="store.channelEnabled
                ? 'bg-emerald-100 dark:bg-emerald-500/15 text-emerald-600 dark:text-emerald-400 hover:bg-emerald-200 dark:hover:bg-emerald-500/25'
                : 'bg-zinc-100 dark:bg-zinc-700/60 text-zinc-400 dark:text-zinc-500 hover:text-zinc-600 dark:hover:text-zinc-300 hover:bg-zinc-200 dark:hover:bg-zinc-600/60'"
              :title="store.channelEnabled ? 'WeChat notifications ON' : 'WeChat notifications OFF'"
              @click="store.toggleChannel(!store.channelEnabled)"
            >
              <svg class="w-3 h-3" viewBox="0 0 20 20" fill="currentColor">
                <path d="M2 5a2 2 0 012-2h7a2 2 0 012 2v4a2 2 0 01-2 2H9l-3 3v-3H4a2 2 0 01-2-2V5z" />
                <path d="M15 7v2a4 4 0 01-4 4H9.828l-1.766 1.767A2 2 0 0011 16h2l3 3v-3h1a2 2 0 002-2V9a2 2 0 00-2-2h-2z" />
              </svg>
              WeChat
            </button>
          </div>

          <div class="flex items-center gap-2">
            <span v-if="store.tokenInfo" class="text-[10px] text-zinc-400 dark:text-zinc-500 font-mono">
              {{ store.tokenInfo.total_tokens.toLocaleString() }} tokens
              <template v-if="store.tokenPercentage > 0"> · {{ store.tokenPercentage }}%</template>
            </span>
            <!-- Stop button -->
            <button
              v-if="store.isRunning"
              class="w-7 h-7 flex items-center justify-center rounded-md bg-red-500 hover:bg-red-600 text-white transition-colors cursor-pointer shadow-sm"
              title="Stop agent (Esc)"
              @click="store.stopAgent()"
            >
              <svg class="w-3.5 h-3.5" viewBox="0 0 24 24" fill="currentColor">
                <rect x="6" y="6" width="12" height="12" rx="2" />
              </svg>
            </button>
            <!-- Send button -->
            <button
              v-else
              class="w-7 h-7 flex items-center justify-center rounded-md bg-emerald-500 hover:bg-emerald-600 text-white transition-colors disabled:opacity-30 disabled:cursor-not-allowed cursor-pointer shadow-sm"
              :disabled="!input.trim() && pendingImages.length === 0"
              @click="send"
            >
              <svg class="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5">
                <path d="M5 12h14M12 5l7 7-7 7" />
              </svg>
            </button>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>
