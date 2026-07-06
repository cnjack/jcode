<script setup lang="ts">
import { ref, nextTick, watch, computed, onMounted, onUnmounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { useChatStore } from '@/stores/chat'
import { api } from '@/composables/api'
import type { SlashCommandInfo, ChatImage } from '@/types/api'
import WorkspacePicker from '@/components/WorkspacePicker.vue'
import BranchPicker from '@/components/BranchPicker.vue'
import ContextCapacityPopup from '@/components/ContextCapacityPopup.vue'
import ProviderIcon from '@/components/ProviderIcon.vue'
import { HandRaisedIcon, ShieldExclamationIcon, ClipboardDocumentListIcon, BoltIcon, PlusIcon, PaperClipIcon, XMarkIcon, ChevronDownIcon, StopIcon, PaperAirplaneIcon, MagnifyingGlassIcon, SquaresPlusIcon, PhotoIcon, WrenchScrewdriverIcon, CheckIcon, StarIcon, SparklesIcon } from '@heroicons/vue/24/outline'
import { StarIcon as StarIconSolid, CheckCircleIcon } from '@heroicons/vue/24/solid'

// Which way the workspace/branch pickers open. The docked composer opens them
// upward (default); the centered welcome composer has more empty room below, so
// it opens them downward to avoid clipping against the top of the canvas.
withDefaults(defineProps<{ pickerPlacement?: 'top' | 'bottom' }>(), {
  pickerPlacement: 'top',
})

// Fired when the user dispatches a message (sent now or queued while a turn is
// in flight). The parent uses it to snap the timeline to the bottom so you see
// your message land even if you'd scrolled up into history.
const emit = defineEmits<{ sent: [] }>()

const store = useChatStore()
const { t } = useI18n()
const input = ref('')
const textarea = ref<HTMLTextAreaElement | null>(null)
const showModelPicker = ref(false)
const showEffortPicker = ref(false)
const showModePicker = ref(false)
const showAddMenu = ref(false)
const showContextPopup = ref(false)

// Context-fill ring on the composer: the orange arc fills with the % of the
// context window in use, turning red as it approaches the limit.
const ctxRingCirc = 2 * Math.PI * 6.4
const ctxRingOffset = computed(() => {
  const p = Math.min(100, Math.max(0, store.tokenPercentage))
  return ctxRingCirc * (1 - p / 100)
})
const ctxRingColor = computed(() => (store.tokenPercentage >= 90 ? '#E24B4A' : 'var(--color-primary)'))
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
  { value: 'approval' as const, label: t('chat.modes.approval'), icon: HandRaisedIcon, sub: t('chat.modes.approvalSub'), risk: 'neutral' as const },
  { value: 'plan' as const, label: t('chat.modes.plan'), icon: ClipboardDocumentListIcon, sub: t('chat.modes.planSub'), risk: 'plan' as const },
  { value: 'full_access' as const, label: t('chat.modes.fullAccess'), icon: ShieldExclamationIcon, sub: t('chat.modes.fullAccessSub'), risk: 'danger' as const },
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

// The picker lists only models the user has enabled ("opened"). Disabled models
// stay hidden here but remain visible in the Manage dialog (which uses
// filteredProviders directly) so they can be re-enabled.
const pickerProviders = computed(() =>
  filteredProviders.value
    .map(p => ({ ...p, models: p.models.filter(m => m.enabled !== false) }))
    .filter(p => p.models.length > 0),
)

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

// Compact context-limit label for the subline: 200000 → "200K", 1000000 → "1M".
function formatContext(limit?: number): string | null {
  if (!limit || limit <= 0) return null
  if (limit >= 1_000_000) {
    const m = limit / 1_000_000
    return (Number.isInteger(m) ? m : m.toFixed(1)) + 'M'
  }
  if (limit >= 1000) return Math.round(limit / 1000) + 'K'
  return String(limit)
}

// Subline under a model row: "claude-sonnet-4-5 · 200K".
function modelSubline(providerId: string, m: { id: string; context_limit?: number } | undefined): string {
  if (!m) return ''
  const parts = [m.id]
  const ctx = formatContext(m.context_limit)
  if (ctx) parts.push(ctx)
  return parts.join(' · ')
}

// Look up the ModelInfo for a provider+model pair (used by recent/favorite refs
// which only carry ids).
function modelInfoFor(provider: string, model: string) {
  const p = store.providers.find((x) => x.id === provider)
  return p?.models.find((m) => m.id === model)
}

// Custom (OpenAI-compatible) provider ids, so their icon falls back to the
// OpenAI mark instead of a monogram.
const customProviderIds = computed(() => new Set(store.providers.filter((p) => p.custom).map((p) => p.id)))
function isCustomProvider(id: string) {
  return customProviderIds.value.has(id)
}

// The ModelInfo for the currently-active model — drives the pinned row subline
// + capability dots.
const currentModelInfo = computed(() => modelInfoFor(store.providerName, store.modelName))

// The reasoning-effort levels the current model accepts, taken from its
// models.dev reasoning_options (type === 'effort'). Empty when the model has
// no effort control — in which case the effort control is hidden entirely.
const currentEffortOptions = computed<string[]>(() => {
  const info = currentModelInfo.value
  if (!info?.reasoning_options) return []
  for (const o of info.reasoning_options) {
    if (o.type === 'effort' && o.values?.length) return o.values
  }
  return []
})

// Whether to show the per-model effort control: only when the active model
// advertises effort levels. A "" (off) option is always prepended so the user
// can clear the override.
const showEffortControl = computed(() => currentEffortOptions.value.length > 0)

// The user's saved effort choice for the current model ('' = unset/default).
const currentEffort = computed(() => store.getEffortOverride(store.providerName, store.modelName))

async function pickEffort(effort: string) {
  // Empty means "clear override" → send '' so the provider default is restored.
  await store.setModelEffort(store.providerName, store.modelName, effort)
}

// Favorite recent models (recent keeps the recency order), filtered by the
// search box and excluding the current model (it's pinned above).
const favoriteModelRefs = computed(() => {
  const q = modelFilter.value.trim().toLowerCase()
  return store.recentModels.filter((r) => {
    if (!store.favoriteModels.has(`${r.provider}/${r.model}`)) return false
    if (store.providerName === r.provider && store.modelName === r.model) return false
    if (!q) return true
    const name = getModelDisplayName(r.provider, r.model).toLowerCase()
    return name.includes(q) || r.provider.toLowerCase().includes(q)
  })
})

// Enter in the filter selects the first visible model.
function selectFirstFiltered() {
  const p = pickerProviders.value[0]
  const m = p?.models[0]
  if (p && m) selectModel(p.id, m.id)
}

// Counts for the Manage dialog footer: visible (after filter) vs total models.
const manageVisibleCount = computed(() =>
  filteredProviders.value.reduce((n, p) => n + p.models.length, 0),
)
const manageTotalCount = computed(() =>
  store.providers.reduce((n, p) => n + p.models.length, 0),
)

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
  if (!text && pendingImages.value.length === 0) return
  const images = pendingImages.value.length > 0 ? [...pendingImages.value] : undefined
  // Capture the running state before clearing the box: while a turn is in
  // flight we queue the message (terminal-style type-ahead) instead of sending
  // it now; it goes out automatically when the current turn completes.
  const queue = store.isRunning
  input.value = ''
  pendingImages.value = []
  pendingImagePreviews.value = []
  showSlashMenu.value = false
  await nextTick()
  autoResize()
  if (queue) {
    store.enqueueMessage(text || '(see attached images)', images)
  } else {
    store.sendMessage(text || '(see attached images)', images)
  }
  emit('sent')
}

function selectModel(provider: string, model: string) {
  showModelPicker.value = false
  showEffortPicker.value = false
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

function selectMode(mode: 'approval' | 'plan' | 'full_access') {
  showModePicker.value = false
  store.switchMode(mode)
}

function modeLabel(m: string): string {
  return m === 'plan' ? t('chat.modes.plan') : m === 'full_access' ? t('chat.modes.fullAccess') : t('chat.modes.approval')
}

// The icon shown on the mode trigger reflects the active mode.
const currentModeIcon = computed(() => modes.value.find((m) => m.value === store.mode)?.icon ?? HandRaisedIcon)

function handleClickOutside(e: MouseEvent) {
  if (containerRef.value && !containerRef.value.contains(e.target as Node)) {
    showModelPicker.value = false
    showModePicker.value = false
    showAddMenu.value = false
    showSlashMenu.value = false
    showContextPopup.value = false
    showEffortPicker.value = false
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
    if (showContextPopup.value) {
      e.preventDefault()
      showContextPopup.value = false
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
  showContextPopup.value = false
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
    <!-- Type-ahead queue: messages composed while the agent is working. They are
         sent one at a time as each turn completes; the × removes one early. -->
    <div v-if="store.queuedMessages.length > 0" class="queued-list">
      <div v-for="(q, i) in store.queuedMessages" :key="q.id" class="queued-item">
        <span class="queued-index">{{ i + 1 }}</span>
        <span class="queued-text">{{ q.text }}</span>
        <span v-if="q.images && q.images.length > 0" class="queued-imgs">
          <PaperClipIcon class="w-3 h-3" />{{ q.images.length }}
        </span>
        <button
          class="queued-remove"
          :title="t('chat.removeQueued')"
          :aria-label="t('chat.removeQueued')"
          @click="store.removeQueuedMessage(q.id)"
        >
          <XMarkIcon class="w-3 h-3" />
        </button>
      </div>
    </div>
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
            <span v-if="cmd.type === 'flow'" class="slash-badge slash-badge-flow">workflow</span>
            <span class="slash-desc">{{ cmd.description }}</span>
          </button>
        </div>

        <!-- Workspace and branch are selected before a task starts. Once the
             conversation begins, hide the entire row so its context stays fixed. -->
        <div v-if="!store.hasMessages" class="composer-top">
          <WorkspacePicker :placement="pickerPlacement" />
          <BranchPicker :placement="pickerPlacement" />
        </div>

        <div class="chat-input-inner">
        <!-- Textarea area -->
        <div class="textarea-area">
          <textarea
            ref="textarea"
            v-model="input"
            :placeholder="store.isRunning ? t('chat.queuePlaceholder') : store.goalArmed ? t('chat.goalPlaceholder') : t('chat.placeholder')"
            rows="1"
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

            <!-- Mode selector (Ask for approval/Plan/Full access) — stays visible on the toolbar.
                 Full access is the one destructive mode; its trigger + row tint red so the
                 "agent can act without you" state is visible without opening the menu. -->
            <div class="relative">
              <button
                class="mo-trigger"
                :data-risk="store.mode === 'full_access' ? 'danger' : store.mode === 'plan' ? 'plan' : 'neutral'"
                :aria-expanded="showModePicker"
                @click.stop="showModePicker = !showModePicker; showModelPicker = false; showAddMenu = false"
              >
                <span class="mo-trigger-ic">
                  <component :is="currentModeIcon" class="w-3.5 h-3.5" />
                </span>
                <span :class="{ 'mo-trigger-danger': store.mode === 'full_access', 'mo-trigger-plan': store.mode === 'plan' }">{{ modeLabel(store.mode) }}</span>
                <ChevronDownIcon class="mo-trigger-chev" />
              </button>
              <div v-if="showModePicker" class="mo-panel">
                <button
                  v-for="m in modes"
                  :key="m.value"
                  class="mo-row"
                  :class="{ active: store.mode === m.value }"
                  @click="selectMode(m.value)"
                >
                  <span class="mo-ic" :class="{ danger: m.risk === 'danger', plan: m.risk === 'plan' }">
                    <component :is="m.icon" class="w-4 h-4" />
                  </span>
                  <span class="mo-body">
                    <span class="mo-title" :class="{ 'mo-title-danger': m.risk === 'danger', 'mo-title-plan': m.risk === 'plan' }">{{ m.label }}</span>
                    <span class="mo-sub">{{ m.sub }}</span>
                  </span>
                  <CheckIcon v-if="store.mode === m.value" class="w-3.5 h-3.5 mo-check" />
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
            <div v-if="store.tokenInfo && store.tokenInfo.total_tokens > 0 && store.hasMessages" class="relative">
              <button
                type="button"
                class="token-count token-count-btn ctx-trigger"
                :title="t('contextCapacity.title')"
                @click.stop="showContextPopup = !showContextPopup; showModelPicker = false; showModePicker = false; showAddMenu = false"
              >
                <svg class="ctx-ring" width="17" height="17" viewBox="0 0 16 16" aria-hidden="true">
                  <circle cx="8" cy="8" r="6.4" fill="none" stroke="color-mix(in srgb, var(--color-foreground) 20%, transparent)" stroke-width="2.2" />
                  <circle
                    cx="8" cy="8" r="6.4" fill="none"
                    :stroke="ctxRingColor" stroke-width="2.2" stroke-linecap="round"
                    :stroke-dasharray="ctxRingCirc" :stroke-dashoffset="ctxRingOffset"
                    transform="rotate(-90 8 8)"
                  />
                </svg>
                <span class="tabular-nums">{{ store.tokenPercentage }}%</span>
              </button>
              <ContextCapacityPopup v-if="showContextPopup" class="absolute bottom-full right-0 mb-2 z-50" />
            </div>

            <!-- Model selector (moved to the right, near Send). The trigger shows
                 the active provider's identity tile so the brand is readable at rest. -->
            <div class="relative flex items-center gap-1">
              <button
                class="mm-trigger"
                :aria-expanded="showModelPicker"
                @click.stop="showModelPicker = !showModelPicker; showModePicker = false; showAddMenu = false; showEffortPicker = false"
              >
                <ProviderIcon
                  v-if="store.providerName"
                  :provider="store.providerName"
                  :custom="isCustomProvider(store.providerName)"
                  :size="16"
                />
                {{ store.modelName ? getModelDisplayName(store.providerName, store.modelName) : 'model' }}
                <ChevronDownIcon class="mm-trigger-chev" />
              </button>

              <!-- Per-model reasoning-effort control. Only shown when the active
                   model advertises effort levels (reasoning_options type=effort). -->
              <button
                v-if="showEffortControl"
                class="effort-trigger"
                :class="{ on: currentEffort }"
                :aria-expanded="showEffortPicker"
                :title="t('chat.model.effortTitle')"
                @click.stop="showEffortPicker = !showEffortPicker; showModelPicker = false; showModePicker = false; showAddMenu = false"
              >
                <SparklesIcon class="w-3 h-3" />
                <span v-if="currentEffort">{{ currentEffort }}</span>
                <span v-else>{{ t('chat.model.effort') }}</span>
                <ChevronDownIcon class="w-3 h-3 effort-chev" />
              </button>
              <div v-if="showEffortControl && showEffortPicker" class="effort-panel">
                <button
                  class="effort-row"
                  :class="{ active: !currentEffort }"
                  @click.stop="pickEffort(''); showEffortPicker = false"
                >
                  <span>{{ t('chat.model.effortDefault') }}</span>
                  <CheckIcon v-if="!currentEffort" class="w-3.5 h-3.5" />
                </button>
                <button
                  v-for="opt in currentEffortOptions"
                  :key="opt"
                  class="effort-row"
                  :class="{ active: currentEffort === opt }"
                  @click.stop="pickEffort(opt); showEffortPicker = false"
                >
                  <span class="font-mono">{{ opt }}</span>
                  <CheckIcon v-if="currentEffort === opt" class="w-3.5 h-3.5" />
                </button>
              </div>

              <div
                v-if="showModelPicker"
                class="mm-panel align-right"
              >
                <!-- Search — first-class, filters both the list and favorites. -->
                <div class="mm-search">
                  <MagnifyingGlassIcon class="w-3.5 h-3.5" />
                  <input
                    v-model="modelFilter"
                    type="text"
                    :placeholder="t('chat.model.filter')"
                    @keydown.enter.prevent="selectFirstFiltered"
                  />
                  <kbd class="mm-search-kbd">/</kbd>
                </div>

                <!-- Pinned current row — never scrolls out of view. -->
                <div v-if="store.providerName && store.modelName" class="mm-pinned">
                  <CheckCircleIcon class="mm-pinned-pin" :title="t('chat.model.current')" />
                  <ProviderIcon :provider="store.providerName" :custom="isCustomProvider(store.providerName)" :size="22" />
                  <span class="mm-pinned-body">
                    <span class="mm-name">{{ getModelDisplayName(store.providerName, store.modelName) }}</span>
                    <span class="mm-id">{{ modelSubline(store.providerName, currentModelInfo) }}</span>
                  </span>
                  <span class="mm-caps">
                    <SparklesIcon v-if="currentModelInfo?.reasoning" class="w-3 h-3" :title="t('chat.model.reasoning')" />
                    <WrenchScrewdriverIcon v-if="currentModelInfo?.tool_call" class="w-3 h-3" :title="t('chat.model.tools')" />
                    <PhotoIcon v-if="currentModelInfo?.image_support" class="w-3 h-3" :title="t('chat.model.images')" />
                  </span>
                </div>

                <div class="mm-list">
                  <!-- Favorites (filtered by the same search) -->
                  <template v-if="favoriteModelRefs.length > 0">
                    <div class="mm-group">{{ t('chat.model.favorites') }} <span class="mm-group-count">{{ favoriteModelRefs.length }}</span></div>
                    <button
                      v-for="r in favoriteModelRefs"
                      :key="'fav-'+r.provider+'-'+r.model"
                      class="mm-row"
                      @click="selectModel(r.provider, r.model)"
                    >
                      <ProviderIcon :provider="r.provider" :custom="isCustomProvider(r.provider)" :size="22" />
                      <span class="mm-body">
                        <span class="mm-name">{{ getModelDisplayName(r.provider, r.model) }}</span>
                        <span class="mm-id">{{ modelSubline(r.provider, modelInfoFor(r.provider, r.model)) }}</span>
                      </span>
                      <span class="mm-caps">
                        <SparklesIcon v-if="modelInfoFor(r.provider, r.model)?.reasoning" class="w-3 h-3" :title="t('chat.model.reasoning')" />
                        <WrenchScrewdriverIcon v-if="modelInfoFor(r.provider, r.model)?.tool_call" class="w-3 h-3" :title="t('chat.model.tools')" />
                        <PhotoIcon v-if="modelInfoFor(r.provider, r.model)?.image_support" class="w-3 h-3" :title="t('chat.model.images')" />
                      </span>
                      <StarIconSolid class="w-3.5 h-3.5 mm-fav on" @click.stop="store.toggleFavorite(r.provider, r.model)" />
                    </button>
                  </template>

                  <!-- All providers (enabled models only), filtered by search. -->
                  <template v-for="p in pickerProviders" :key="p.id">
                    <div class="mm-group">{{ p.name }}</div>
                    <div
                      v-for="m in p.models"
                      :key="m.id"
                      class="mm-row"
                      :class="{ active: store.providerName === p.id && store.modelName === m.id }"
                      role="button"
                      tabindex="0"
                      @click="selectModel(p.id, m.id)"
                      @keydown.enter.prevent="selectModel(p.id, m.id)"
                      @keydown.space.prevent="selectModel(p.id, m.id)"
                    >
                      <ProviderIcon :provider="p.id" :custom="p.custom" :size="22" />
                      <span class="mm-body">
                        <span class="mm-name">{{ m.name || m.id }}</span>
                        <span class="mm-id">{{ modelSubline(p.id, m) }}</span>
                      </span>
                      <span v-if="m.recommended" class="mm-recommend">{{ t('common.recommended') }}</span>
                      <span class="mm-caps">
                        <SparklesIcon v-if="m.reasoning" class="w-3 h-3" :title="t('chat.model.reasoning')" />
                        <WrenchScrewdriverIcon v-if="m.tool_call" class="w-3 h-3" :title="t('chat.model.tools')" />
                        <PhotoIcon v-if="m.image_support" class="w-3 h-3" :title="t('chat.model.images')" />
                        <PhotoIcon v-else-if="m.image_support === false" class="w-3 h-3 mm-cap-warn" :title="t('chat.model.noImages')" />
                      </span>
                      <CheckIcon v-if="store.providerName === p.id && store.modelName === m.id" class="w-3.5 h-3.5 mm-check" />
                      <StarIcon
                        class="w-3.5 h-3.5 mm-fav"
                        :class="{ on: store.isFavorite(p.id, m.id) }"
                        @click.stop="store.toggleFavorite(p.id, m.id)"
                      />
                    </div>
                  </template>
                  <div v-if="pickerProviders.length === 0" class="mm-empty">
                    {{ modelFilter ? t('chat.model.noMatch') : t('chat.model.none') }}
                  </div>
                </div>

                <!-- Manage models link -->
                <div class="mm-foot">
                  <button @click.stop="showModelPicker = false; showManageModels = true">
                    <SquaresPlusIcon class="w-3.5 h-3.5" />
                    {{ t('chat.model.manage') }}
                  </button>
                </div>
              </div>
            </div>

            <!-- Stop button — halts the current turn. With a non-empty queue it
                 then sends the next queued message ("skip & continue"). -->
            <button
              v-if="store.isRunning"
              class="stop-btn"
              :title="store.queuedMessages.length > 0 ? t('chat.stopAndNext') : t('chat.stopAgent')"
              @click="store.stopAgent()"
            >
              <StopIcon class="w-3.5 h-3.5" />
              {{ t('chat.stop') }}
            </button>
            <!-- Send button. While a turn is running it stays available (only when
                 there is content) and queues the message instead of sending now. -->
            <button
              v-if="!store.isRunning || input.trim() || pendingImages.length > 0"
              class="send-btn"
              :disabled="!input.trim() && pendingImages.length === 0"
              :aria-label="store.isRunning ? t('chat.queue') : t('chat.send')"
              @click="send"
            >
              <PaperAirplaneIcon class="w-3.5 h-3.5" />
              {{ store.isRunning ? t('chat.queue') : t('chat.send') }}
            </button>
          </div>
        </div>
        </div><!-- /chat-input-inner -->
      </div>

    <!-- Manage Models Dialog — same anatomy as SettingsDialog: scrim + header
         with an icon tile + subtitle, a filter row, and a footer with a count
         summary + actions. Rows reuse the selector's identity tile + subline. -->
    <Teleport to="body">
      <div
        v-if="showManageModels"
        class="manage-scrim"
        @click="showManageModels = false; modelFilter = ''; store.fetchModels()"
      >
        <div class="manage-dlg" role="dialog" aria-modal="true" :aria-label="t('chat.model.manageTitle')" @click.stop>
          <div class="manage-head">
            <div class="manage-head-icon"><SquaresPlusIcon class="w-4 h-4" /></div>
            <div class="manage-head-text">
              <h3 class="manage-title">{{ t('chat.model.manageTitle') }}</h3>
              <p class="manage-sub">{{ t('chat.model.toggleVisibility') }}</p>
            </div>
            <button
              class="manage-close"
              :aria-label="t('common.close')"
              @click="showManageModels = false; modelFilter = ''; store.fetchModels()"
            ><XMarkIcon class="w-4 h-4" /></button>
          </div>

          <div class="manage-filter">
            <div class="manage-filter-wrap">
              <MagnifyingGlassIcon class="w-3.5 h-3.5" />
              <input
                v-model="modelFilter"
                type="text"
                :placeholder="t('chat.model.filter')"
              />
            </div>
            <span class="manage-filter-count">{{ manageVisibleCount }} / {{ manageTotalCount }}</span>
          </div>

          <div class="manage-body">
            <template v-for="p in filteredProviders" :key="'mgr-'+p.id">
              <div class="manage-prov">
                <ProviderIcon :provider="p.id" :size="18" />
                <span class="manage-prov-name">{{ p.name }}</span>
                <span class="manage-prov-id">{{ p.id }}</span>
                <span class="manage-prov-count">{{ p.models.length }}</span>
              </div>
              <div
                v-for="m in p.models"
                :key="'mgr-'+p.id+'-'+m.id"
                class="manage-row"
                :data-off="m.enabled === false ? 'true' : 'false'"
              >
                <ProviderIcon :provider="p.id" :size="18" />
                <span class="manage-row-text">
                  <span class="manage-row-name">{{ m.name || m.id }}</span>
                  <span class="manage-row-id">{{ modelSubline(p.id, m) }}</span>
                </span>
                <span v-if="m.recommended" class="mm-recommend">{{ t('common.recommended') }}</span>
                <button
                  class="manage-switch"
                  role="switch"
                  :aria-checked="m.enabled !== false ? 'true' : 'false'"
                  :aria-label="m.enabled !== false ? t('common.disable') : t('common.enable')"
                  @click="store.toggleModelEnabled(p.id, m.id, m.enabled === false)"
                />
              </div>
            </template>
            <div v-if="filteredProviders.length === 0" class="manage-empty">
              {{ modelFilter ? t('chat.model.noMatch') : t('chat.model.none') }}
            </div>
          </div>

          <div class="manage-foot">
            <span class="manage-foot-hint">{{ t('chat.model.visibleCount', { visible: manageVisibleCount, total: manageTotalCount }) }}</span>
            <div class="manage-foot-actions">
              <button class="manage-btn" @click="showManageModels = false; modelFilter = ''; store.fetchModels()">{{ t('common.done') }}</button>
            </div>
          </div>
        </div>
      </div>
    </Teleport>


  </div>
</template>

<style scoped>
.chat-input-wrapper {
  /* 20px horizontal matches the messages column's px-5 so the input box
     sits flush with the conversation content. */
  padding: 8px 20px 14px;
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

/* Type-ahead queue — pending messages stacked just above the composer, mirroring
   a terminal's queued input. Each row is removable via its × button. */
.queued-list {
  display: flex;
  flex-direction: column;
  gap: 4px;
  padding: 0 8px 6px;
  margin: 0 auto;
}

.queued-item {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 6px 8px 6px 10px;
  border-radius: var(--radius-lg);
  background: var(--color-muted);
  border: 1px solid var(--color-border);
  font-size: 12px;
  color: var(--color-foreground);
}

.queued-index {
  display: grid;
  place-items: center;
  width: 16px;
  height: 16px;
  flex-shrink: 0;
  border-radius: var(--radius-pill);
  background: var(--neutral-wash);
  color: var(--color-accent-neutral);
  font-size: 10px;
  font-weight: 600;
  font-family: var(--font-mono);
}

.queued-text {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.queued-imgs {
  display: inline-flex;
  align-items: center;
  gap: 2px;
  flex-shrink: 0;
  color: var(--color-muted-foreground);
  font-size: 11px;
  font-family: var(--font-mono);
}

.queued-remove {
  display: grid;
  place-items: center;
  width: 20px;
  height: 20px;
  flex-shrink: 0;
  border: none;
  background: transparent;
  border-radius: var(--radius-md);
  color: var(--color-muted-foreground);
  cursor: pointer;
  transition: background 0.15s, color 0.15s;
}

.queued-remove:hover {
  background: var(--color-secondary);
  color: var(--color-foreground);
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
  /* Softened red for the "Full access" mode — the stock error red read as
     "太红"; lightened toward the surface so it warns without shouting. Inherits
     down to the mode trigger and its dropdown panel. */
  --color-danger-soft: color-mix(in srgb, var(--color-error-fg) 72%, var(--color-background));
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
  background: var(--neutral-wash);
  color: var(--color-accent-neutral);
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
  color: var(--color-accent-neutral);
}

.attach-badge {
  position: absolute;
  top: -2px;
  right: -2px;
  width: 14px;
  height: 14px;
  border-radius: 50%;
  background: var(--color-accent-neutral);
  color: var(--color-surface);
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
  color: var(--color-accent-neutral);
  background: var(--neutral-wash);
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
  background: transparent;
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
  color: var(--color-accent-neutral);
}
.dmi-badge {
  margin-left: auto;
  font-size: 10px;
  font-family: var(--font-mono);
  color: var(--color-accent-neutral);
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
  background: var(--neutral-wash);
  color: var(--color-accent-neutral);
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
  background: color-mix(in srgb, var(--color-accent-neutral) 18%, transparent);
  color: var(--color-accent-neutral);
  cursor: pointer;
  transition: background 0.15s;
}
.goal-chip-x:hover {
  background: color-mix(in srgb, var(--color-accent-neutral) 34%, transparent);
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
  background: var(--neutral-wash);
  color: var(--color-accent-neutral);
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
  color: var(--color-accent-neutral);
}

.dropdown-item:hover .fav-star {
  opacity: 1;
}

/* ───────────────────────────────────────────────────────────────────────────
 * Mode selector — Ask for approval / Plan / Full access.
 * Flat three-item list; the only chromatic signal is the destructive tint on
 * Full access (the one mode that can act without you). Icons are stock
 * heroicons: HandRaised / ClipboardDocumentList / ShieldExclamation.
 * ─────────────────────────────────────────────────────────────────────────── */
.mo-trigger {
  display: inline-flex;
  align-items: center;
  gap: 7px;
  height: 28px;
  padding: 0 8px 0 6px;
  border: 1px solid transparent;
  border-radius: var(--radius-lg);
  background: transparent;
  color: var(--color-foreground);
  font-size: 12px;
  font-weight: 500;
  cursor: pointer;
  transition: background var(--duration-fast);
}
.mo-trigger:hover { background: var(--color-muted); }
.mo-trigger-ic {
  width: 18px;
  height: 18px;
  display: grid;
  place-items: center;
  flex-shrink: 0;
  background: transparent;
  color: var(--color-accent-neutral);
}
.mo-trigger[data-risk='danger'] .mo-trigger-ic { color: var(--color-danger-soft); }
.mo-trigger[data-risk='plan'] .mo-trigger-ic { color: var(--color-success); }
/* The label tints to match the active mode so the state reads at rest:
   soft red for Full access, green for Plan. */
.mo-trigger-danger { color: var(--color-danger-soft); }
.mo-trigger-plan { color: var(--color-success); }
.mo-trigger-chev {
  width: 12px;
  height: 12px;
  opacity: 0.55;
  transition: transform var(--duration-normal);
}
.mo-trigger[aria-expanded='true'] .mo-trigger-chev { transform: rotate(180deg); }

.mo-panel {
  position: absolute;
  bottom: 100%;
  left: 0;
  margin-bottom: 4px;
  z-index: var(--z-dropdown);
  width: 264px;
  padding: 4px;
  background: var(--color-surface);
  border: 1px solid var(--color-border);
  border-radius: var(--radius-xl);
  box-shadow: var(--shadow-lg);
}
.mo-row {
  display: flex;
  align-items: flex-start;
  gap: 10px;
  width: 100%;
  padding: 8px;
  border: none;
  background: transparent;
  border-radius: var(--radius-md);
  color: var(--color-foreground);
  font: inherit;
  text-align: left;
  cursor: pointer;
  transition: background var(--duration-fast);
}
.mo-row:hover { background: var(--color-muted); }
.mo-row.active { background: var(--neutral-wash); }
.mo-ic {
  width: 26px;
  height: 26px;
  display: grid;
  place-items: center;
  background: transparent;
  color: var(--color-accent-neutral);
  flex-shrink: 0;
  margin-top: 1px;
}
.mo-ic.danger { color: var(--color-danger-soft); }
.mo-ic.plan { color: var(--color-success); }
.mo-body {
  flex: 1;
  min-width: 0;
  padding-top: 1px;
}
.mo-title {
  display: block;
  font-size: 12.5px;
  font-weight: 500;
  color: var(--color-foreground);
  letter-spacing: -0.005em;
}
.mo-row.active .mo-title { font-weight: 600; }
.mo-title-danger { color: var(--color-danger-soft); }
.mo-title-plan { color: var(--color-success); }
.mo-sub {
  display: block;
  font-size: 10.5px;
  color: var(--color-muted-foreground);
  line-height: 1.4;
  margin-top: 1px;
}
.mo-check {
  flex-shrink: 0;
  margin-top: 3px;
  color: var(--color-accent-neutral);
}

/* ───────────────────────────────────────────────────────────────────────────
 * Model selector — identity tiles, capability dots, pinned current, search.
 * ─────────────────────────────────────────────────────────────────────────── */
.mm-trigger {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  height: 28px;
  padding: 0 8px;
  border: 1px solid transparent;
  border-radius: var(--radius-lg);
  background: transparent;
  color: var(--color-foreground);
  font-size: 12px;
  font-weight: 500;
  cursor: pointer;
  transition: background var(--duration-fast);
}
.mm-trigger:hover { background: var(--color-muted); }
.mm-trigger-chev {
  width: 12px;
  height: 12px;
  opacity: 0.55;
  transition: transform var(--duration-normal);
}
.mm-trigger[aria-expanded='true'] .mm-trigger-chev { transform: rotate(180deg); }

/* Per-model reasoning-effort control — visually matches the model picker
   trigger (transparent border, no background; muted bg only on hover). */
.effort-trigger {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  height: 28px;
  padding: 0 8px;
  border: 1px solid transparent;
  border-radius: var(--radius-lg);
  background: transparent;
  color: var(--color-foreground);
  font-size: 12px;
  font-weight: 500;
  cursor: pointer;
  transition: background var(--duration-fast);
}
.effort-trigger:hover { background: var(--color-muted); }
/* When an effort is selected, tint only the text (no chip fill/border), so the
   trigger stays visually consistent with the adjacent model picker. */
.effort-trigger.on { color: var(--color-primary); }
.effort-chev { opacity: 0.55; transition: transform var(--duration-normal); }
.effort-trigger[aria-expanded='true'] .effort-chev { transform: rotate(180deg); }
.effort-panel {
  position: absolute;
  bottom: 100%;
  right: 0;
  margin-bottom: 4px;
  z-index: var(--z-dropdown);
  min-width: 140px;
  padding: 4px;
  border: 1px solid var(--color-border);
  border-radius: var(--radius-md);
  background: var(--color-surface);
  box-shadow: var(--shadow-md);
  display: flex;
  flex-direction: column;
  gap: 1px;
}
.effort-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  width: 100%;
  padding: 6px 8px;
  border: none;
  border-radius: var(--radius-sm);
  background: transparent;
  color: var(--color-foreground);
  font-size: 12px;
  cursor: pointer;
  transition: background var(--duration-fast);
}
.effort-row:hover { background: var(--color-muted); }
.effort-row.active { color: var(--color-primary); font-weight: 600; }
.effort-row .heroicon { color: var(--color-primary); }

.mm-panel {
  position: absolute;
  bottom: 100%;
  left: 0;
  margin-bottom: 4px;
  z-index: var(--z-dropdown);
  width: 290px;
  max-height: 540px;
  display: flex;
  flex-direction: column;
  overflow: hidden;
  background: var(--color-surface);
  border: 1px solid var(--color-border);
  border-radius: var(--radius-xl);
  box-shadow: var(--shadow-lg);
}
.mm-panel.align-right { left: auto; right: 0; }

.mm-search {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 9px 12px;
  border-bottom: 1px solid var(--color-border);
  color: var(--color-foreground);
}
.mm-search input {
  flex: 1;
  border: none;
  outline: none;
  background: transparent;
  color: var(--color-foreground);
  font: inherit;
  font-size: 13px;
}
.mm-search input::placeholder { color: var(--color-foreground); opacity: 1; }
.mm-search-kbd {
  font-family: var(--font-mono);
  font-size: 10px;
  padding: 1px 5px;
  border-radius: var(--radius-sm);
  background: var(--color-muted);
  border: 1px solid var(--color-border);
  color: var(--color-muted-foreground);
}

/* Pinned current row. */
.mm-pinned {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 9px 12px;
  background: transparent;
  border-bottom: 1px solid var(--color-border);
}
.mm-pinned-pin {
  width: 17px;
  height: 17px;
  flex-shrink: 0;
  color: var(--color-accent-neutral);
}
.mm-pinned-body {
  display: flex;
  flex-direction: column;
  flex: 1;
  min-width: 0;
}

.mm-list {
  overflow-y: auto;
  padding: 4px;
  flex: 1;
}
.mm-group {
  padding: 8px 8px 4px;
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 10px;
  font-weight: 600;
  letter-spacing: 0.06em;
  text-transform: uppercase;
  color: var(--color-muted-foreground);
}
.mm-group-count {
  font-family: var(--font-mono);
  font-weight: 400;
  letter-spacing: 0;
  text-transform: none;
  opacity: 0.7;
}
.mm-row {
  display: flex;
  align-items: center;
  gap: 10px;
  width: 100%;
  padding: 6px 8px;
  border: none;
  background: transparent;
  border-radius: var(--radius-md);
  color: var(--color-foreground);
  font: inherit;
  text-align: left;
  cursor: pointer;
  transition: background var(--duration-fast);
}
.mm-row:hover { background: var(--color-muted); }
.mm-row.active { background: var(--neutral-wash); }
.mm-row.active .mm-name { color: var(--color-accent-neutral); font-weight: 600; }
.mm-body {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
}
.mm-name {
  font-size: 12.5px;
  color: var(--color-foreground);
  line-height: 1.25;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.mm-id {
  font-family: var(--font-mono);
  font-size: 10px;
  color: var(--color-muted-foreground);
  margin-top: 1px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
/* Capability dots — mono-stroke, identical baseline. Darkened + sized up a
 * touch so the icons stay legible against the active/pinned wash. */
.mm-caps {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  flex-shrink: 0;
  color: var(--color-foreground);
}
.mm-caps svg { width: 15px; height: 15px; stroke-width: 1.9; }
.mm-cap-warn { color: var(--color-warning-fg); }
.mm-recommend {
  font-size: 10px;
  font-weight: 600;
  letter-spacing: 0.02em;
  padding: 1px 6px;
  border-radius: var(--radius-xs);
  background: var(--neutral-wash-strong);
  border: 1px solid var(--color-border);
  color: var(--color-foreground);
  flex-shrink: 0;
}
.mm-check {
  flex-shrink: 0;
  color: var(--color-accent-neutral);
  stroke-width: 2;
}
.mm-fav {
  opacity: 0;
  flex-shrink: 0;
  background: transparent;
  border: none;
  color: var(--color-muted-foreground);
  cursor: pointer;
  transition: opacity var(--duration-fast), color var(--duration-fast);
}
.mm-row:hover .mm-fav,
.mm-fav.on { opacity: 1; }
.mm-fav.on { color: var(--color-primary); }
.mm-fav:hover { color: var(--color-primary); }
.mm-empty {
  text-align: center;
  font-size: 13px;
  color: var(--color-muted-foreground);
  padding: 20px 0;
}
.mm-foot {
  border-top: 1px solid var(--color-border);
  padding: 6px;
}
.mm-foot button {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 100%;
  padding: 7px 8px;
  border: none;
  background: transparent;
  color: var(--color-foreground);
  font: inherit;
  font-size: 12px;
  border-radius: var(--radius-md);
  cursor: pointer;
  transition: background var(--duration-fast), color var(--duration-fast);
}
.mm-foot button:hover {
  background: var(--color-muted);
  color: var(--color-foreground);
}

/* ───────────────────────────────────────────────────────────────────────────
 * Manage Models dialog — SettingsDialog anatomy (scrim + header + filter +
 * body + footer). Raw checkbox replaced by the s-switch shape.
 * ─────────────────────────────────────────────────────────────────────────── */
.manage-scrim {
  position: fixed;
  inset: 0;
  z-index: var(--z-modal);
  display: flex;
  align-items: center;
  justify-content: center;
  background: var(--backdrop);
  backdrop-filter: blur(6px);
  -webkit-backdrop-filter: blur(6px);
}
.manage-dlg {
  width: min(560px, 94vw);
  max-height: 70vh;
  display: flex;
  flex-direction: column;
  overflow: hidden;
  background: var(--color-surface);
  border: 1px solid var(--color-border);
  border-radius: var(--radius-xl);
  box-shadow: var(--shadow-lg);
  margin: 0 16px;
}
.manage-head {
  display: flex;
  align-items: flex-start;
  gap: 12px;
  padding: 16px 18px 14px;
  border-bottom: 1px solid var(--color-border);
}
.manage-head-icon {
  width: 30px;
  height: 30px;
  display: grid;
  place-items: center;
  border-radius: var(--radius-md);
  background: var(--neutral-wash);
  color: var(--color-accent-neutral);
  flex-shrink: 0;
}
.manage-head-text { flex: 1; min-width: 0; }
.manage-title {
  margin: 0;
  font-size: 14px;
  font-weight: 600;
  color: var(--color-foreground);
  letter-spacing: -0.01em;
}
.manage-sub {
  margin: 2px 0 0;
  font-size: 11.5px;
  color: var(--color-muted-foreground);
  line-height: 1.45;
}
.manage-close {
  margin-left: auto;
  width: 28px;
  height: 28px;
  flex-shrink: 0;
  display: grid;
  place-items: center;
  border: 1px solid transparent;
  background: transparent;
  color: var(--color-muted-foreground);
  border-radius: var(--radius-md);
  cursor: pointer;
  transition: background var(--duration-fast), color var(--duration-fast), border-color var(--duration-fast);
}
.manage-close:hover {
  background: var(--color-muted);
  color: var(--color-foreground);
  border-color: var(--color-border);
}
.manage-filter {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 10px 18px;
  border-bottom: 1px solid var(--color-border);
}
.manage-filter-wrap {
  flex: 1;
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 0 10px;
  height: 30px;
  background: var(--color-muted);
  border: 1px solid var(--color-border);
  border-radius: var(--radius-md);
  color: var(--color-muted-foreground);
  transition: border-color var(--duration-fast);
}
.manage-filter-wrap:focus-within { border-color: var(--color-accent-neutral); }
.manage-filter-wrap input {
  flex: 1;
  border: none;
  outline: none;
  background: transparent;
  color: var(--color-foreground);
  font: inherit;
  font-size: 12.5px;
}
.manage-filter-wrap input::placeholder { color: var(--color-muted-foreground); }
.manage-filter-count {
  font-family: var(--font-mono);
  font-size: 11px;
  color: var(--color-muted-foreground);
  flex-shrink: 0;
}
.manage-body {
  overflow-y: auto;
  flex: 1;
  padding: 6px 10px 10px;
}
.manage-prov {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 12px 8px 6px;
  position: sticky;
  top: 0;
  background: var(--color-surface);
  z-index: 1;
}
.manage-prov-name {
  font-size: 11px;
  font-weight: 600;
  color: var(--color-foreground);
}
.manage-prov-id {
  font-family: var(--font-mono);
  font-size: 10px;
  color: var(--color-muted-foreground);
}
.manage-prov-count {
  margin-left: auto;
  font-family: var(--font-mono);
  font-size: 10px;
  color: var(--color-muted-foreground);
}
.manage-row {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 8px;
  border-radius: var(--radius-md);
  transition: background var(--duration-fast);
}
.manage-row:hover { background: var(--color-muted); }
.manage-row[data-off='true'] { opacity: 0.5; }
.manage-row[data-off='true'] .manage-row-name { color: var(--color-muted-foreground); }
.manage-row-text {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
}
.manage-row-name {
  font-size: 12.5px;
  color: var(--color-foreground);
  line-height: 1.3;
}
.manage-row-id {
  font-family: var(--font-mono);
  font-size: 10px;
  color: var(--color-muted-foreground);
  margin-top: 1px;
}
.manage-switch {
  position: relative;
  width: 32px;
  height: 18px;
  flex-shrink: 0;
  border-radius: var(--radius-pill);
  border: none;
  background: var(--color-border);
  cursor: pointer;
  padding: 0;
  transition: background var(--duration-fast);
}
.manage-switch[aria-checked='true'] { background: var(--color-accent-neutral); }
.manage-switch::after {
  content: '';
  position: absolute;
  top: 2px;
  left: 2px;
  width: 14px;
  height: 14px;
  border-radius: 50%;
  background: var(--color-surface);
  box-shadow: var(--shadow-sm);
  transition: transform var(--duration-fast) var(--ease-out);
}
.manage-switch[aria-checked='true']::after { transform: translateX(14px); }
.manage-empty {
  text-align: center;
  font-size: 13px;
  color: var(--color-muted-foreground);
  padding: 32px 0;
}
.manage-foot {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 11px 18px;
  border-top: 1px solid var(--color-border);
  background: var(--color-muted);
}
.manage-foot-hint {
  font-size: 11px;
  color: var(--color-muted-foreground);
}
.manage-foot-actions { display: flex; gap: 8px; }
.manage-btn {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  height: 30px;
  padding: 0 13px;
  border: 1px solid transparent;
  border-radius: var(--radius-md);
  background: var(--color-accent-neutral);
  color: var(--color-surface);
  font: inherit;
  font-size: 12px;
  font-weight: 500;
  cursor: pointer;
  transition: background var(--duration-fast);
}
.manage-btn:hover {
  background: color-mix(in srgb, var(--color-accent-neutral) 88%, var(--color-background));
}

/* Token count */
.token-count {
  font-size: 10px;
  font-family: var(--font-mono);
  color: var(--color-muted-foreground);
}
/* Clickable variant: opens the context-capacity popup. */
.token-count-btn {
  background: none;
  border: none;
  padding: 2px 5px;
  border-radius: var(--radius-sm);
  cursor: pointer;
  transition: background var(--duration-fast), color var(--duration-fast);
}
.token-count-btn:hover {
  background: var(--color-secondary);
  color: var(--color-foreground);
}
/* Context-fill ring + percentage. */
.ctx-trigger {
  display: inline-flex;
  align-items: center;
  gap: 5px;
}
.ctx-ring {
  display: block;
  transition: stroke-dashoffset var(--duration-normal, 0.3s) ease;
}
.ctx-ring circle:last-child {
  transition: stroke-dashoffset var(--duration-normal, 0.3s) ease;
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
  color: var(--color-accent-neutral);
  flex-shrink: 0;
}

.slash-badge {
  flex-shrink: 0;
  font-size: 10px;
  line-height: 1;
  padding: 2px 6px;
  border-radius: 999px;
  font-weight: 600;
  letter-spacing: 0.02em;
}

.slash-badge-flow {
  color: var(--color-accent);
  background: var(--color-accent-wash);
  border: 1px solid var(--color-accent-wash);
}

.slash-desc {
  font-size: 11px;
  color: var(--color-muted-foreground);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.hidden {
  display: none;
}
</style>
