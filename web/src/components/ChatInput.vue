<script setup lang="ts">
import { ref, nextTick, watch, computed, onMounted, onUnmounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { useChatStore } from '@/stores/chat'
import { api } from '@/composables/api'
import type { SlashCommandInfo, ChatImage } from '@/types/api'
import WorkspacePicker from '@/components/WorkspacePicker.vue'
import BranchPicker from '@/components/BranchPicker.vue'
import { useBranch } from '@/composables/useBranch'
import { ChatBubbleLeftIcon, ClipboardDocumentListIcon, BoltIcon, PlusIcon, PaperClipIcon, XMarkIcon, ChevronDownIcon, ChatBubbleLeftRightIcon, StopIcon, PaperAirplaneIcon } from '@heroicons/vue/24/outline'

// Which way the workspace/branch pickers open. The docked composer opens them
// upward (default); the centered welcome composer has more empty room below, so
// it opens them downward to avoid clipping against the top of the canvas.
withDefaults(defineProps<{ pickerPlacement?: 'top' | 'bottom' }>(), {
  pickerPlacement: 'top',
})

const store = useChatStore()
const { t } = useI18n()
// Current git branch (singleton) — used to decide whether the composer's top row
// is worth showing once the workspace picker is hidden mid-conversation.
const { current: branchCurrent } = useBranch()
const input = ref('')
const textarea = ref<HTMLTextAreaElement | null>(null)
const showModelPicker = ref(false)
const showModePicker = ref(false)
const showAddMenu = ref(false)
const showManageModels = ref(false)
const modelFilter = ref('')
const containerRef = ref<HTMLDivElement | null>(null)

const slashCommands = ref<SlashCommandInfo[]>([])
const showSlashMenu = ref(false)
const slashFilter = ref('')
const selectedSlashIdx = ref(0)

// Image attachment state
const pendingImages = ref<ChatImage[]>([])
const pendingImagePreviews = ref<string[]>([])
const fileInput = ref<HTMLInputElement | null>(null)

const modes = computed(() => [
  { value: 'ask' as const, label: t('chat.modes.ask'), icon: ChatBubbleLeftIcon },
  { value: 'plan' as const, label: t('chat.modes.plan'), icon: ClipboardDocumentListIcon },
  { value: 'autopilot' as const, label: t('chat.modes.autopilot'), icon: BoltIcon },
])

const filteredSlashCommands = computed(() => {
  const filter = slashFilter.value.toLowerCase()
  return slashCommands.value.filter(
    (s) => s.slash.toLowerCase().startsWith('/' + filter),
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
  // While an IME composition is active (e.g. confirming a CJK candidate), let
  // the IME own every key — pressing Enter to commit a character must not send
  // the message or trigger menu shortcuts.
  if (e.isComposing || e.keyCode === 229) return

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

function applySlashCommand(cmd: SlashCommandInfo) {
  input.value = cmd.slash + ' '
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

// Insert a trigger character at the cursor from the "+" menu. For "/" at the
// start of an empty box this also opens the slash-command menu via handleInput.
function insertToken(char: string) {
  showAddMenu.value = false
  const el = textarea.value
  const start = el ? el.selectionStart : input.value.length
  const end = el ? el.selectionEnd : input.value.length
  input.value = input.value.slice(0, start) + char + input.value.slice(end)
  nextTick(() => {
    el?.focus()
    const pos = start + char.length
    el?.setSelectionRange(pos, pos)
    handleInput()
  })
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

function selectMode(mode: 'ask' | 'plan' | 'autopilot') {
  showModePicker.value = false
  store.switchMode(mode)
}

function modeLabel(m: string): string {
  return m === 'plan' ? t('chat.modes.plan') : m === 'autopilot' ? t('chat.modes.autopilot') : t('chat.modes.ask')
}

function handleClickOutside(e: MouseEvent) {
  if (containerRef.value && !containerRef.value.contains(e.target as Node)) {
    showModelPicker.value = false
    showModePicker.value = false
    showAddMenu.value = false
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
    slashCommands.value = await api.slashCommands()
  } catch { /* ignore */ }
})

onUnmounted(() => {
  document.removeEventListener('click', handleClickOutside)
  document.removeEventListener('keydown', handleGlobalKey)
})

watch(() => store.isRunning, (running) => {
  if (!running) nextTick(() => textarea.value?.focus())
})

// Reset all composer-local state when the active session changes (switching
// sessions / projects). clearChat() only resets store-level state, so without
// this a half-typed draft, attached images, or an open menu from session A
// would leak into session B and risk being sent to the wrong conversation.
watch(() => store.currentSessionId, () => {
  input.value = ''
  pendingImages.value = []
  pendingImagePreviews.value = []
  showSlashMenu.value = false
  showModelPicker.value = false
  showModePicker.value = false
  showAddMenu.value = false
  showManageModels.value = false
})

// If the newly-selected model can't accept images, drop any already-attached
// ones so they aren't silently sent to (and rejected by) a text-only model.
watch(() => store.imageSupport, (supported) => {
  if (!supported && pendingImages.value.length > 0) {
    pendingImages.value = []
    pendingImagePreviews.value = []
  }
})
</script>

<template>
  <div ref="containerRef" class="chat-input-wrapper">
    <div class="chat-input-card" :class="{ 'composer-elevated': !store.hasMessages }">
      <!-- Slash command menu -->
      <div
        v-if="showSlashMenu && filteredSlashCommands.length > 0"
        class="slash-menu"
      >
          <button
            v-for="(cmd, i) in filteredSlashCommands"
            :key="cmd.slash"
            class="slash-item"
            :class="{ active: i === selectedSlashIdx }"
            @click="applySlashCommand(cmd)"
            @mouseenter="selectedSlashIdx = i"
          >
            <span class="slash-cmd">{{ cmd.slash }}</span>
            <span class="slash-desc">{{ cmd.description }}</span>
          </button>
        </div>

        <!-- Workspace selector — pick the workspace for this task directly on the
             composer (replaces opening the projects modal from the sidebar). Only
             on the new-task screen: once a conversation has started the workspace
             is fixed for that task, so switching it here is meaningless. The
             branch picker stays (switching branch mid-task is still useful). The
             whole row is dropped when it would be empty (in a conversation with no
             git branch) so it leaves no blank gap above the composer. -->
        <div v-if="!store.hasMessages || branchCurrent" class="composer-top">
          <WorkspacePicker v-if="!store.hasMessages" :placement="pickerPlacement" />
          <BranchPicker :placement="pickerPlacement" />
        </div>

        <div class="chat-input-inner">
        <!-- Textarea area -->
        <div class="textarea-area">
          <textarea
            ref="textarea"
            v-model="input"
            :placeholder="store.isRunning ? t('chat.workingPlaceholder') : store.goalArmed ? t('chat.goalPlaceholder') : t('chat.placeholder')"
            rows="1"
            :disabled="store.isRunning"
            @keydown="handleKeyDown"
            @input="handleInput"
            @paste="handlePaste"
          />

          <!-- Image previews — sit BELOW the textarea so pasted attachments grow
               downward into the toolbar gap instead of pushing the text down. -->
          <div v-if="pendingImagePreviews.length > 0" class="image-previews">
            <div v-for="(preview, i) in pendingImagePreviews" :key="i" class="image-preview-item">
              <img :src="preview" />
              <button class="image-remove" @click="removeImage(i)">✕</button>
            </div>
          </div>

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
            <!-- "+" menu: attach files, slash command, and Goal (only the items
                 that have real functionality). -->
            <div class="relative">
              <button
                class="add-btn"
                :class="{ open: showAddMenu }"
                :title="t('chat.add')"
                @click.stop="showAddMenu = !showAddMenu; showModelPicker = false; showModePicker = false"
              >
                <PlusIcon class="w-4 h-4" />
              </button>
              <div v-if="showAddMenu" class="dropdown-menu add-menu">
                <button
                  class="dropdown-item"
                  :class="{ disabled: !store.imageSupport }"
                  :disabled="!store.imageSupport"
                  :title="!store.imageSupport ? t('chat.model.noImages') : ''"
                  @click="triggerImageUpload(); showAddMenu = false"
                >
                  <PaperClipIcon class="w-3.5 h-3.5 dmi-icon" /> <span>{{ t('chat.attachFiles') }}</span>
                  <span v-if="pendingImages.length > 0" class="dmi-badge">{{ pendingImages.length }}</span>
                </button>
                <button class="dropdown-item" @click="insertToken('/')">
                  <span class="dmi-icon dmi-slash">/</span> <span>{{ t('chat.command') }}</span>
                </button>
                <button
                  class="dropdown-item"
                  :class="{ active: store.goalArmed }"
                  :title="store.goal ? t('chat.goalHint.replace') : t('chat.goalHint.next')"
                  @click="store.goalArmed = !store.goalArmed; showAddMenu = false"
                >
                  <BoltIcon class="w-3.5 h-3.5 dmi-icon" /> <span>{{ t('chat.goal') }}</span>
                </button>
              </div>
            </div>

            <!-- Mode selector (Ask/Plan/Autopilot) — stays visible on the toolbar. -->
            <div class="relative">
              <button
                class="tool-btn dropdown-btn"
                :class="{ highlighted: store.mode !== 'ask' }"
                @click.stop="showModePicker = !showModePicker; showModelPicker = false; showAddMenu = false"
              >
                {{ modeLabel(store.mode) }}
                <ChevronDownIcon class="w-3 h-3 opacity-60" />
              </button>
              <div v-if="showModePicker" class="dropdown-menu">
                <button
                  v-for="m in modes"
                  :key="m.value"
                  class="dropdown-item"
                  :class="{ active: store.mode === m.value }"
                  @click="selectMode(m.value)"
                >
                  <component :is="m.icon" class="w-3.5 h-3.5" /> {{ m.label }}
                </button>
              </div>
            </div>

            <!-- Goal chip — appears once Goal is armed (from the + menu); its ×
                 disarms it. Mirrors the Codex "目标" pill. -->
            <template v-if="store.goalArmed">
              <span class="tb-divider" aria-hidden="true" />
              <div class="goal-chip" :title="store.goal ? t('chat.goalHint.nextReplaces') : t('chat.goalHint.next')">
                <button class="goal-chip-x" :title="t('chat.goalHint.remove')" @click="store.goalArmed = false">
                  <XMarkIcon class="w-2.5 h-2.5" />
                </button>
                <BoltIcon class="w-3 h-3" />
                <span>Goal</span>
              </div>
            </template>
          </div>

          <div class="toolbar-right">
            <span v-if="store.tokenInfo" class="token-count">
              {{ store.tokenInfo.total_tokens.toLocaleString() }} tokens
            </span>

            <!-- Model selector (moved to the right, near Send) -->
            <div class="relative">
              <button
                class="tool-btn dropdown-btn"
                @click.stop="showModelPicker = !showModelPicker; showModePicker = false; showAddMenu = false"
              >
                {{ store.modelName ? getModelDisplayName(store.providerName, store.modelName) : 'model' }}
                <ChevronDownIcon class="w-3 h-3 opacity-60" />
              </button>
              <div
                v-if="showModelPicker"
                class="dropdown-menu model-menu align-right"
              >
                <!-- Favorites section -->
                <template v-if="store.recentModels.length > 0 && store.favoriteModels.size > 0">
                  <div class="dropdown-section-title"><span>★</span> {{ t('chat.model.favorites') }}</div>
                  <button
                    v-for="r in store.recentModels.filter(r => store.favoriteModels.has(`${r.provider}/${r.model}`) && !(store.providerName === r.provider && store.modelName === r.model))"
                    :key="'fav-'+r.provider+'-'+r.model"
                    class="dropdown-item"
                    @click="selectModel(r.provider, r.model)"
                  >
                    <span class="mr-1" style="color: var(--color-primary)">★</span>{{ getModelDisplayName(r.provider, r.model) }}
                  </button>
                </template>

                <!-- Current Model -->
                <template v-if="store.providerName && store.modelName">
                  <div class="dropdown-section-title">{{ t('chat.model.current') }}</div>
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
                    <span v-if="m.recommended" class="recommend-badge">{{ t('common.recommended') }}</span>
                    <button
                      class="fav-star"
                      :class="{ 'is-fav': store.isFavorite(p.id, m.id) }"
                      @click.stop="store.toggleFavorite(p.id, m.id)"
                    >★</button>
                  </button>
                </template>
                <div v-if="store.enabledProviders.length === 0" class="dropdown-item disabled">
                  {{ t('chat.model.none') }}
                </div>
                <!-- Manage models link -->
                <div class="dropdown-footer">
                  <button @click.stop="showModelPicker = false; showManageModels = true">
                    {{ t('chat.model.manage') }}
                  </button>
                </div>
              </div>
            </div>

            <!-- Channel toggle (inline) -->
            <button
              v-if="store.channelAvailable"
              class="channel-btn"
              :class="{ active: store.channelEnabled }"
              :title="store.channelEnabled ? t('chat.wechatOn') : t('chat.wechatOff')"
              @click="store.toggleChannel(!store.channelEnabled)"
            >
              <ChatBubbleLeftRightIcon class="w-3 h-3" />
            </button>
            <!-- Stop button -->
            <button
              v-if="store.isRunning"
              class="stop-btn"
              :title="t('chat.stopAgent')"
              @click="store.stopAgent()"
            >
              <StopIcon class="w-3.5 h-3.5" />
              {{ t('chat.stop') }}
            </button>
            <!-- Send button -->
            <button
              v-else
              class="send-btn"
              :disabled="!input.trim() && pendingImages.length === 0"
              :aria-label="t('chat.send')"
              @click="send"
            >
              <PaperAirplaneIcon class="w-3.5 h-3.5" />
              {{ t('chat.send') }}
            </button>
          </div>
        </div>
        </div><!-- /chat-input-inner -->
      </div>

    <!-- Manage Models Dialog -->
    <Teleport to="body">
      <div
        v-if="showManageModels"
        class="fixed inset-0 z-50 flex items-center justify-center bg-[var(--backdrop)] backdrop-blur-sm"
        @click="showManageModels = false; modelFilter = ''"
      >
        <div class="w-full max-w-lg max-h-[70vh] flex flex-col mx-4 rounded-lg shadow-xl" style="background: var(--color-surface); border: 1px solid var(--color-border)" @click.stop>
          <div class="px-4 py-3" style="border-bottom: 1px solid var(--color-border)">
            <div class="flex items-center justify-between mb-2">
              <div>
                <h3 class="text-sm font-semibold" style="color: var(--color-foreground)">{{ t('chat.model.manageTitle') }}</h3>
                <p class="text-[11px]" style="color: var(--color-muted-foreground)">{{ t('chat.model.toggleVisibility') }}</p>
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
              :placeholder="t('chat.model.filter')"
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
                <span v-if="m.recommended" class="text-[9px] px-1.5 py-0.5 rounded font-medium shrink-0" style="background: var(--accent-wash); color: var(--color-primary)">recommended</span>
              </label>
            </template>
          </div>
        </div>
      </div>
    </Teleport>


  </div>
</template>

<style scoped>
.chat-input-wrapper {
  padding: 8px 16px 14px;
  /* Sits inside the surface chat panel — transparent so it blends with it. */
  background: transparent;
  position: relative;
}

.chat-input-card {
  margin: 0 auto;
  border-radius: var(--radius-xl);
  padding: 4px 6px 10px;
  background: transparent;
  position: relative;
  display: flex;
  flex-direction: column;
}

.composer-top {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 0 2px 5px;
  min-width: 0;
}

/* On the new-task screen the composer is the centerpiece: lift the whole thing
   (workspace pills + input + toolbar) into one cohesive elevated card with a
   soft, background-tinted shadow. The docked conversation composer keeps the
   recessed, frameless look. */
.composer-elevated {
  background: var(--color-surface);
  border: 1px solid var(--color-border);
  border-radius: var(--radius-2xl);
  /* Tighter at the top (workspace row), a touch more white below the toolbar. */
  padding: 6px 12px 12px;
  box-shadow: var(--shadow-xl);
}
.composer-elevated .composer-top {
  padding: 2px 4px 9px;
}
.composer-elevated .chat-input-inner {
  border-radius: var(--radius-xl);
}

.chat-input-inner {
  /* Recessed: always one step darker than the surface panel, derived from it so
     the depth cue holds on every theme (some light themes have a background
     token that is lighter than surface, which would otherwise invert it). */
  background: color-mix(in srgb, var(--color-surface) 90%, #000);
  border: 1px solid var(--color-border);
  border-radius: var(--radius-lg);
  padding: 14px 16px 0;
  transition: border-color 0.2s;
}

.chat-input-inner:focus-within {
  border-color: color-mix(in srgb, var(--color-foreground) 30%, transparent);
}

.textarea-area {
  padding: 0 0 8px;
}

.textarea-area textarea {
  width: 100%;
  background: transparent;
  border: none;
  outline: none;
  resize: none;
  font-size: 14px;
  line-height: 1.6;
  color: var(--color-foreground);
  min-height: 28px;
  max-height: 200px;
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
  /* Rendered below the textarea now: separate from the text above, not the
     toolbar below (the toolbar already owns the bottom padding). */
  margin-top: 8px;
}

.image-preview-item {
  position: relative;
}

.image-preview-item img {
  width: 56px;
  height: 56px;
  object-fit: cover;
  border-radius: var(--radius-md);
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
  color: var(--color-on-destructive);
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
  padding: 8px 0 12px;
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
  border-radius: var(--radius-md);
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
  background: var(--accent-wash);
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
  color: var(--color-on-primary);
  font-size: 9px;
  display: flex;
  align-items: center;
  justify-content: center;
  font-weight: 600;
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
  border-radius: var(--radius-lg);
  box-shadow: var(--shadow-md);
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
  background: var(--accent-wash);
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

/* "+" add menu button + items */
.add-btn {
  display: grid;
  place-items: center;
  width: 30px;
  height: 30px;
  border: none;
  background: var(--color-muted);
  border-radius: var(--radius-md);
  color: var(--color-muted-foreground);
  cursor: pointer;
  transition: background 0.15s, color 0.15s;
}
.add-btn:hover,
.add-btn.open {
  background: var(--color-secondary);
  color: var(--color-foreground);
}
.add-menu {
  min-width: 188px;
}
.add-menu .dropdown-item {
  gap: 9px;
  padding: 7px 12px;
  color: var(--color-foreground);
}
.dmi-icon {
  color: var(--color-muted-foreground);
  flex-shrink: 0;
}
/* The "/ Command" menu item uses a literal slash character instead of an icon
   (heroicons has no slash glyph); size it to match the other menu icons. */
.dmi-slash {
  width: 15px;
  text-align: center;
  font-family: var(--font-mono);
  font-size: 14px;
  line-height: 1;
}
.dropdown-item.active .dmi-icon {
  color: var(--color-primary);
}
.dmi-badge {
  margin-left: auto;
  font-size: 10px;
  font-family: var(--font-mono);
  color: var(--color-primary);
}

/* Right-anchored dropdown (the model picker now lives on the toolbar's right) */
.dropdown-menu.align-right {
  left: auto;
  right: 0;
}

/* Subtle divider + the armed-Goal chip (Codex-style "× Goal" pill) */
.tb-divider {
  width: 1px;
  height: 16px;
  background: var(--color-border);
  margin: 0 2px;
  flex-shrink: 0;
}
.goal-chip {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  height: 26px;
  padding: 0 9px 0 4px;
  border-radius: var(--radius-md);
  background: var(--accent-wash);
  color: var(--color-primary);
  font-size: 12px;
  font-weight: 500;
}
.goal-chip-x {
  display: grid;
  place-items: center;
  width: 16px;
  height: 16px;
  border: none;
  border-radius: var(--radius-pill);
  background: color-mix(in srgb, var(--color-primary) 18%, transparent);
  color: var(--color-primary);
  cursor: pointer;
  transition: background 0.15s;
}
.goal-chip-x:hover {
  background: color-mix(in srgb, var(--color-primary) 34%, transparent);
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
  border-radius: var(--radius-sm);
  background: var(--accent-wash);
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
  color: var(--color-primary);
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
  border-radius: var(--radius-lg);
  background: var(--color-primary);
  color: var(--color-on-primary);
  font-size: 12px;
  font-weight: 500;
  cursor: pointer;
  transition: opacity 0.15s, transform 0.08s var(--ease-out);
  white-space: nowrap;
}

.send-btn:disabled {
  opacity: 0.45;
  cursor: not-allowed;
}

.send-btn:not(:disabled):hover {
  opacity: 0.92;
}

.send-btn:not(:disabled):active {
  transform: translateY(0.5px);
}

.stop-btn {
  display: flex;
  align-items: center;
  gap: 5px;
  padding: 5px 12px;
  border: none;
  border-radius: var(--radius-lg);
  background: var(--color-destructive);
  color: var(--color-on-destructive);
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
  border-radius: var(--radius-lg);
  box-shadow: var(--shadow-md);
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

/* Channel button (inline in toolbar) */
.channel-btn {
  width: 24px;
  height: 24px;
  display: flex;
  align-items: center;
  justify-content: center;
  border: none;
  background: transparent;
  border-radius: var(--radius-md);
  color: var(--color-muted-foreground);
  cursor: pointer;
  transition: color 0.15s, background 0.15s;
}

.channel-btn:hover {
  background: var(--color-muted);
}

.channel-btn.active {
  color: var(--color-primary);
  background: var(--accent-wash);
}

.hidden {
  display: none;
}
</style>
