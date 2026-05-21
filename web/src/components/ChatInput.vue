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
  <div ref="containerRef" class="chat-input-wrapper">
    <div class="chat-input-separator" />
    <div class="chat-input-container">
      <!-- Slash command menu -->
      <div
        v-if="showSlashMenu && filteredSlashCommands.length > 0"
        class="slash-menu"
      >
          <button
            v-for="(cmd, i) in filteredSlashCommands"
            :key="cmd.name"
            class="slash-item"
            :class="{ active: i === selectedSlashIdx }"
            @click="applySlashCommand(cmd)"
            @mouseenter="selectedSlashIdx = i"
          >
            <span class="slash-cmd">{{ cmd.slash }}</span>
            <span class="slash-desc">{{ cmd.description }}</span>
          </button>
        </div>

        <!-- Textarea area -->
        <div class="textarea-area">
          <!-- Image previews -->
          <div v-if="pendingImagePreviews.length > 0" class="image-previews">
            <div v-for="(preview, i) in pendingImagePreviews" :key="i" class="image-preview-item">
              <img :src="preview" />
              <button class="image-remove" @click="removeImage(i)">✕</button>
            </div>
          </div>

          <textarea
            ref="textarea"
            v-model="input"
            :placeholder="store.isRunning ? 'Agent is working…' : 'Ask JCODE, @agent, or /command'"
            rows="1"
            :disabled="store.isRunning"
            @keydown="handleKeyDown"
            @input="handleInput"
            @paste="handlePaste"
          />

          <!-- Hidden file input -->
          <input
            ref="fileInput"
            type="file"
            accept="image/*"
            multiple
            class="hidden"
            @change="handleImageSelect"
          />
      </div>

      <!-- Toolbar -->
      <div class="toolbar">
          <div class="toolbar-left">
            <!-- Paperclip / attach -->
            <button
              class="tool-btn attach-btn"
              :class="{ disabled: !store.imageSupport, 'has-images': pendingImages.length > 0 }"
              :title="!store.imageSupport ? 'Current model does not support images' : 'Attach images'"
              :disabled="!store.imageSupport"
              @click="store.imageSupport && triggerImageUpload()"
            >
              <svg class="w-4 h-4" viewBox="0 0 20 20" fill="none" stroke="currentColor" stroke-width="1.5">
                <path d="M15.621 4.379a3.5 3.5 0 00-4.95 0L4.05 11a2.5 2.5 0 003.536 3.536l6.621-6.621a1.5 1.5 0 00-2.121-2.121l-6.622 6.621" stroke-linecap="round" stroke-linejoin="round" />
              </svg>
              <span v-if="pendingImages.length > 0" class="attach-badge">{{ pendingImages.length }}</span>
            </button>

            <!-- Mode selector (Agent/Plan) -->
            <div class="relative">
              <button
                class="tool-btn dropdown-btn"
                :class="{ highlighted: store.mode === 'plan' }"
                @click.stop="showModePicker = !showModePicker; showModelPicker = false"
              >
                {{ store.mode === 'agent' ? 'Agent' : 'Plan' }}
                <svg class="w-3 h-3 opacity-60" viewBox="0 0 20 20" fill="currentColor">
                  <path fill-rule="evenodd" d="M5.23 7.21a.75.75 0 011.06.02L10 11.168l3.71-3.938a.75.75 0 111.08 1.04l-4.25 4.5a.75.75 0 01-1.08 0l-4.25-4.5a.75.75 0 01.02-1.06z" clip-rule="evenodd" />
                </svg>
              </button>
              <div v-if="showModePicker" class="dropdown-menu">
                <button
                  v-for="m in modes"
                  :key="m.value"
                  class="dropdown-item"
                  :class="{ active: store.mode === m.value }"
                  @click="selectMode(m.value)"
                >
                  {{ m.icon }} {{ m.label }}
                </button>
              </div>
            </div>

            <!-- Model selector -->
            <div class="relative">
              <button
                class="tool-btn dropdown-btn"
                @click.stop="showModelPicker = !showModelPicker; showModePicker = false"
              >
                {{ store.modelName || 'model' }}
                <svg class="w-3 h-3 opacity-60" viewBox="0 0 20 20" fill="currentColor">
                  <path fill-rule="evenodd" d="M5.23 7.21a.75.75 0 011.06.02L10 11.168l3.71-3.938a.75.75 0 111.08 1.04l-4.25 4.5a.75.75 0 01-1.08 0l-4.25-4.5a.75.75 0 01.02-1.06z" clip-rule="evenodd" />
                </svg>
              </button>
              <div
                v-if="showModelPicker"
                class="dropdown-menu model-menu"
              >
                <!-- Favorites section -->
                <template v-if="store.recentModels.length > 0 && store.favoriteModels.size > 0">
                  <div class="dropdown-section-title"><span>★</span> Favorites</div>
                  <button
                    v-for="r in store.recentModels.filter(r => store.favoriteModels.has(`${r.provider}/${r.model}`) && !(store.providerName === r.provider && store.modelName === r.model))"
                    :key="'fav-'+r.provider+'-'+r.model"
                    class="dropdown-item"
                    @click="selectModel(r.provider, r.model)"
                  >
                    <span class="text-amber-400 mr-1">★</span>{{ getModelDisplayName(r.provider, r.model) }}
                  </button>
                </template>

                <!-- Current Model -->
                <template v-if="store.providerName && store.modelName">
                  <div class="dropdown-section-title">Current</div>
                  <button class="dropdown-item active" @click="selectModel(store.providerName, store.modelName)">
                    ● {{ getModelDisplayName(store.providerName, store.modelName) }}
                  </button>
                </template>

                <!-- All providers (enabled only) -->
                <template v-for="p in store.enabledProviders" :key="p.id">
                  <div class="dropdown-section-title">{{ p.name }}</div>
                  <button
                    v-for="m in p.models"
                    :key="m.id"
                    class="dropdown-item group"
                    :class="{ active: store.providerName === p.id && store.modelName === m.id }"
                    @click="selectModel(p.id, m.id)"
                  >
                    <span class="truncate">{{ m.name || m.id }}</span>
                    <span v-if="m.recommended" class="recommend-badge">recommended</span>
                    <button
                      class="fav-star"
                      :class="{ 'is-fav': store.isFavorite(p.id, m.id) }"
                      @click.stop="store.toggleFavorite(p.id, m.id)"
                    >★</button>
                  </button>
                </template>
                <div v-if="store.enabledProviders.length === 0" class="dropdown-item disabled">
                  No models available
                </div>
                <!-- Manage models link -->
                <div class="dropdown-footer">
                  <button @click.stop="showModelPicker = false; showManageModels = true">
                    ⚙ Manage models…
                  </button>
                </div>
              </div>
            </div>

            <!-- Auto-approve toggle switch -->
            <label class="toggle-switch" :title="store.autoApprove ? 'Auto-approve ON' : 'Auto-approve OFF'">
              <input
                type="checkbox"
                :checked="store.autoApprove"
                @change="store.setAutoApprove(!store.autoApprove)"
              />
              <span class="toggle-slider"></span>
            </label>
          </div>

          <div class="toolbar-right">
            <span v-if="store.tokenInfo" class="token-count">
              {{ store.tokenInfo.total_tokens.toLocaleString() }} tokens
            </span>

            <!-- Stop button -->
            <button
              v-if="store.isRunning"
              class="stop-btn"
              title="Stop agent (Esc)"
              @click="store.stopAgent()"
            >
              <svg class="w-3.5 h-3.5" viewBox="0 0 24 24" fill="currentColor">
                <rect x="6" y="6" width="12" height="12" rx="2" />
              </svg>
              Stop
            </button>
            <!-- Send button -->
            <button
              v-else
              class="send-btn"
              :disabled="!input.trim() && pendingImages.length === 0"
              @click="send"
            >
              <svg class="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5">
                <path d="M5 12h14M12 5l7 7-7 7" />
              </svg>
              Send
            </button>
          </div>
        </div>
      </div>

    <!-- Manage Models Dialog -->
    <Teleport to="body">
      <div
        v-if="showManageModels"
        class="fixed inset-0 z-50 flex items-center justify-center bg-black/40"
        @click="showManageModels = false; modelFilter = ''"
      >
        <div class="w-full max-w-lg max-h-[70vh] flex flex-col mx-4 rounded-lg shadow-xl" style="background: var(--color-surface); border: 1px solid var(--color-border)" @click.stop>
          <div class="px-4 py-3" style="border-bottom: 1px solid var(--color-border)">
            <div class="flex items-center justify-between mb-2">
              <div>
                <h3 class="text-sm font-semibold" style="color: var(--color-foreground)">Manage Models</h3>
                <p class="text-[11px]" style="color: var(--color-muted-foreground)">Toggle which models appear in the model selector</p>
              </div>
              <button
                class="cursor-pointer"
                style="color: var(--color-muted-foreground)"
                @click="showManageModels = false; modelFilter = ''; store.fetchModels()"
              >✕</button>
            </div>
            <input
              v-model="modelFilter"
              type="text"
              placeholder="Filter models..."
              class="w-full px-2 py-1.5 text-xs rounded focus:outline-none focus:ring-1"
              style="border: 1px solid var(--color-border); background: var(--color-muted); color: var(--color-foreground); --tw-ring-color: var(--color-primary)"
            />
          </div>
          <div class="overflow-y-auto flex-1 py-2">
            <template v-for="p in filteredProviders" :key="'mgr-'+p.id">
              <div class="px-4 py-1.5 text-[10px] uppercase tracking-wider font-semibold sticky top-0" style="color: var(--color-muted-foreground); background: var(--color-surface)">
                {{ p.name }}
              </div>
              <label
                v-for="m in p.models"
                :key="'mgr-'+p.id+'-'+m.id"
                class="flex items-center gap-2.5 px-4 py-2 cursor-pointer transition-colors hover:opacity-80"
              >
                <input
                  type="checkbox"
                  :checked="m.enabled !== false"
                  class="w-4 h-4 rounded border-2 focus:ring-2 focus:ring-offset-0 cursor-pointer transition-all"
                  style="accent-color: var(--color-primary); border-color: var(--color-border)"
                  @change="store.toggleModelEnabled(p.id, m.id, ($event.target as HTMLInputElement).checked)"
                />
                <span class="text-xs flex-1 truncate" style="color: var(--color-foreground)">{{ m.name || m.id }}</span>
                <span v-if="m.recommended" class="text-[9px] px-1.5 py-0.5 rounded font-medium shrink-0" style="background: rgba(255,132,0,0.1); color: var(--color-primary)">recommended</span>
              </label>
            </template>
          </div>
        </div>
      </div>
    </Teleport>

    <!-- Channel toggle (only if available, shown as a subtle indicator) -->
    <button
      v-if="store.channelAvailable"
      class="channel-toggle"
      :class="{ active: store.channelEnabled }"
      :title="store.channelEnabled ? 'WeChat notifications ON' : 'WeChat notifications OFF'"
      @click="store.toggleChannel(!store.channelEnabled)"
    >
      <svg class="w-3 h-3" viewBox="0 0 20 20" fill="currentColor">
        <path d="M2 5a2 2 0 012-2h7a2 2 0 012 2v4a2 2 0 01-2 2H9l-3 3v-3H4a2 2 0 01-2-2V5z" />
        <path d="M15 7v2a4 4 0 01-4 4H9.828l-1.766 1.767A2 2 0 0011 16h2l3 3v-3h1a2 2 0 002-2V9a2 2 0 00-2-2h-2z" />
      </svg>
    </button>
  </div>
</template>

<style scoped>
.chat-input-wrapper {
  padding: 0;
  background: var(--color-background);
  position: relative;
}

.chat-input-separator {
  height: 1px;
  background: color-mix(in srgb, var(--color-border) 50%, transparent);
}

.chat-input-container {
  max-width: 48rem;
  margin: 0 auto;
  padding: 12px 20px 14px;
}

.textarea-area {
  padding: 0 0 8px;
  min-height: 48px;
}

.textarea-area textarea {
  width: 100%;
  background: transparent;
  border: none;
  outline: none;
  resize: none;
  font-size: 14px;
  line-height: 1.5;
  color: var(--color-foreground);
  min-height: 40px;
  max-height: 160px;
  font-family: var(--font-sans);
}

.textarea-area textarea::placeholder {
  color: var(--color-muted-foreground);
}

.textarea-area textarea:disabled {
  opacity: 0.5;
}

.image-previews {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin-bottom: 8px;
}

.image-preview-item {
  position: relative;
}

.image-preview-item img {
  width: 56px;
  height: 56px;
  object-fit: cover;
  border-radius: 6px;
  border: 1px solid var(--color-border);
}

.image-preview-item .image-remove {
  position: absolute;
  top: -4px;
  right: -4px;
  width: 16px;
  height: 16px;
  border-radius: 50%;
  background: var(--color-destructive);
  color: white;
  font-size: 9px;
  display: flex;
  align-items: center;
  justify-content: center;
  border: none;
  cursor: pointer;
  opacity: 0;
  transition: opacity 0.15s;
}

.image-preview-item:hover .image-remove {
  opacity: 1;
}

/* Toolbar */
.toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 4px 0 0;
  gap: 8px;
}

.toolbar-left {
  display: flex;
  align-items: center;
  gap: 6px;
}

.toolbar-right {
  display: flex;
  align-items: center;
  gap: 8px;
}

.tool-btn {
  display: flex;
  align-items: center;
  gap: 4px;
  padding: 4px 8px;
  border: none;
  background: transparent;
  border-radius: 6px;
  font-size: 12px;
  color: var(--color-muted-foreground);
  cursor: pointer;
  transition: background 0.15s, color 0.15s;
  white-space: nowrap;
}

.tool-btn:hover {
  background: var(--color-muted);
  color: var(--color-foreground);
}

.tool-btn.highlighted {
  background: rgba(255, 132, 0, 0.1);
  color: var(--color-primary);
}

.attach-btn {
  padding: 4px 6px;
  position: relative;
}

.attach-btn.disabled {
  opacity: 0.4;
  cursor: not-allowed;
}

.attach-btn.has-images {
  color: var(--color-primary);
}

.attach-badge {
  position: absolute;
  top: -2px;
  right: -2px;
  width: 14px;
  height: 14px;
  border-radius: 50%;
  background: var(--color-primary);
  color: white;
  font-size: 9px;
  display: flex;
  align-items: center;
  justify-content: center;
  font-weight: 600;
}

/* Toggle switch */
.toggle-switch {
  position: relative;
  display: inline-block;
  width: 32px;
  height: 18px;
  cursor: pointer;
}

.toggle-switch input {
  opacity: 0;
  width: 0;
  height: 0;
}

.toggle-slider {
  position: absolute;
  inset: 0;
  background: var(--color-muted);
  border-radius: 9999px;
  transition: background 0.2s;
}

.toggle-slider::before {
  content: '';
  position: absolute;
  width: 14px;
  height: 14px;
  left: 2px;
  bottom: 2px;
  background: white;
  border-radius: 50%;
  transition: transform 0.2s;
}

.toggle-switch input:checked + .toggle-slider {
  background: var(--color-primary);
}

.toggle-switch input:checked + .toggle-slider::before {
  transform: translateX(14px);
}

/* Dropdown menus */
.dropdown-menu {
  position: absolute;
  bottom: 100%;
  left: 0;
  margin-bottom: 4px;
  z-index: 20;
  min-width: 140px;
  padding: 4px 0;
  background: var(--color-surface);
  border: 1px solid var(--color-border);
  border-radius: 10px;
  box-shadow: 0 4px 12px rgba(0, 0, 0, 0.15);
}

.dropdown-menu.model-menu {
  min-width: 220px;
  max-height: 280px;
  overflow-y: auto;
}

.dropdown-item {
  width: 100%;
  padding: 6px 12px;
  font-size: 12px;
  text-align: left;
  border: none;
  background: transparent;
  color: var(--color-muted-foreground);
  cursor: pointer;
  transition: background 0.1s;
  display: flex;
  align-items: center;
  gap: 4px;
}

.dropdown-item:hover {
  background: var(--color-muted);
}

.dropdown-item.active {
  color: var(--color-primary);
  background: rgba(255, 132, 0, 0.1);
}

.dropdown-item.disabled {
  cursor: default;
  opacity: 0.5;
}

.dropdown-section-title {
  padding: 4px 12px;
  font-size: 10px;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.05em;
  color: var(--color-muted-foreground);
  border-top: 1px solid var(--color-border);
  margin-top: 2px;
}

.dropdown-section-title:first-child {
  border-top: none;
  margin-top: 0;
}

.dropdown-footer {
  padding: 4px 12px;
  border-top: 1px solid var(--color-border);
}

.dropdown-footer button {
  width: 100%;
  text-align: left;
  font-size: 12px;
  color: var(--color-muted-foreground);
  background: transparent;
  border: none;
  cursor: pointer;
  padding: 4px 0;
}

.recommend-badge {
  font-size: 9px;
  padding: 1px 5px;
  border-radius: 4px;
  background: rgba(255, 132, 0, 0.1);
  color: var(--color-primary);
  font-weight: 500;
  margin-left: 4px;
}

.fav-star {
  opacity: 0;
  margin-left: auto;
  background: transparent;
  border: none;
  color: var(--color-muted-foreground);
  cursor: pointer;
  font-size: 12px;
  padding: 0 2px;
}

.fav-star.is-fav {
  opacity: 1;
  color: #fbbf24;
}

.dropdown-item:hover .fav-star {
  opacity: 1;
}

/* Token count */
.token-count {
  font-size: 10px;
  font-family: var(--font-mono);
  color: var(--color-muted-foreground);
}

/* Send & Stop buttons */
.send-btn {
  display: flex;
  align-items: center;
  gap: 5px;
  padding: 5px 12px;
  border: none;
  border-radius: 8px;
  background: var(--color-primary);
  color: white;
  font-size: 12px;
  font-weight: 500;
  cursor: pointer;
  transition: opacity 0.15s;
  white-space: nowrap;
}

.send-btn:disabled {
  opacity: 0.3;
  cursor: not-allowed;
}

.send-btn:not(:disabled):hover {
  opacity: 0.9;
}

.stop-btn {
  display: flex;
  align-items: center;
  gap: 5px;
  padding: 5px 12px;
  border: none;
  border-radius: 8px;
  background: var(--color-destructive);
  color: white;
  font-size: 12px;
  font-weight: 500;
  cursor: pointer;
  white-space: nowrap;
}

/* Slash menu */
.slash-menu {
  position: absolute;
  bottom: 100%;
  left: 0;
  right: 0;
  margin-bottom: 8px;
  z-index: 30;
  padding: 6px 0;
  max-height: 200px;
  overflow-y: auto;
  background: var(--color-surface);
  border: 1px solid var(--color-border);
  border-radius: 10px;
  box-shadow: 0 4px 12px rgba(0, 0, 0, 0.15);
}

.slash-item {
  width: 100%;
  padding: 8px 14px;
  text-align: left;
  border: none;
  background: transparent;
  cursor: pointer;
  display: flex;
  align-items: flex-start;
  gap: 10px;
  transition: background 0.1s;
}

.slash-item.active,
.slash-item:hover {
  background: var(--color-muted);
}

.slash-cmd {
  font-size: 12px;
  font-family: var(--font-mono);
  color: var(--color-primary);
  flex-shrink: 0;
}

.slash-desc {
  font-size: 11px;
  color: var(--color-muted-foreground);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

/* Channel toggle */
.channel-toggle {
  position: absolute;
  top: 8px;
  right: 16px;
  width: 24px;
  height: 24px;
  display: flex;
  align-items: center;
  justify-content: center;
  border: none;
  background: transparent;
  border-radius: 6px;
  color: var(--color-muted-foreground);
  cursor: pointer;
  transition: color 0.15s, background 0.15s;
}

.channel-toggle.active {
  color: var(--color-primary);
  background: rgba(255, 132, 0, 0.1);
}

.hidden {
  display: none;
}
</style>
