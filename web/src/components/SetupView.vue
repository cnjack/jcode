<script setup lang="ts">
import { ref, computed, onMounted, watch } from 'vue'
import {
  CheckIcon,
  CheckCircleIcon,
  ChevronLeftIcon,
  ChevronRightIcon,
  EyeIcon,
  EyeSlashIcon,
} from '@heroicons/vue/24/outline'
import { useI18n } from 'vue-i18n'
import { useChatStore } from '@/stores/chat'
import { api } from '@/composables/api'
import ProviderIcon from './ProviderIcon.vue'
import type { SetupProvider } from '@/types/api'

const emit = defineEmits<{
  complete: []
}>()

const store = useChatStore()
const { t } = useI18n()

// The wizard no longer has a model-selection step: a default model is picked
// server-side for registry providers. Custom (OpenAI-compatible) providers
// must supply a model id, name and base URL.
type Step = 'provider' | 'apikey' | 'done'
const step = ref<Step>('provider')
const loading = ref(false)
const error = ref('')

// Step 1: Provider selection
const providers = ref<SetupProvider[]>([])
const selectedProvider = ref('')
const providerSearch = ref('')
const isCustom = ref(false) // chose "Custom" entry instead of a registry provider

// Step 2: API key + advanced
const apiKey = ref('')
const baseURL = ref('')
const showApiKey = ref(false)
const validating = ref(false)
const validationResult = ref<{ valid: boolean; error?: string } | null>(null)

// Custom provider fields (only when isCustom)
const customName = ref('')
const customId = ref('')
const customModelId = ref('')
const customReasoning = ref(false)

// Advanced settings (custom endpoint + headers). Capabilities (vision/thinking/
// reasoning_effort) are model-level, not provider-level, so they're not exposed
// here — they're driven by each model's registry metadata.
const advancedOpen = ref(false)
const headers = ref<{ key: string; value: string }[]>([])

const filteredProviders = computed(() => {
  const q = providerSearch.value.toLowerCase()
  return providers.value.filter(p =>
    p.name.toLowerCase().includes(q) || p.id.toLowerCase().includes(q)
  )
})

const selectedProviderInfo = computed(() =>
  providers.value.find(p => p.id === selectedProvider.value)
)

// Provider list loader — named so the provider step can offer a Retry when the
// first attempt fails (e.g. the sidecar wasn't ready yet). Without a retry path,
// a transient failure here would strand first-run setup until a full restart.
async function loadProviders() {
  loading.value = true
  error.value = ''
  try {
    providers.value = await api.setupProviders()
  } catch {
    error.value = 'Failed to load providers'
  }
  loading.value = false
}
onMounted(loadProviders)

function resetProviderState() {
  apiKey.value = ''
  baseURL.value = ''
  showApiKey.value = false
  validationResult.value = null
}

async function selectProvider(id: string) {
  // Switching to a different provider invalidates any key/base-url/validation
  // entered for the previous one — clear them so provider A's secret can't leak
  // into provider B's form and get submitted to B's endpoint.
  resetProviderState()
  selectedProvider.value = id
  isCustom.value = false
  customName.value = ''
  customId.value = ''
  customModelId.value = ''
  customReasoning.value = false
  headers.value = []
  advancedOpen.value = false
  step.value = 'apikey'
}

function selectCustomProvider() {
  resetProviderState()
  selectedProvider.value = ''
  isCustom.value = true
  customName.value = ''
  customId.value = ''
  customModelId.value = ''
  customReasoning.value = false
  // Custom providers always need a base URL — surface the advanced panel.
  baseURL.value = ''
  headers.value = []
  advancedOpen.value = true
  step.value = 'apikey'
}

function goBack() {
  error.value = ''
  validationResult.value = null
  step.value = 'provider'
  selectedProvider.value = ''
  isCustom.value = false
  resetProviderState()
}

function addHeaderRow() {
  headers.value.push({ key: '', value: '' })
}

function removeHeaderRow(i: number) {
  headers.value.splice(i, 1)
}

// Build the headers payload: drop rows with a blank key.
function collectHeaders(): Record<string, string> | undefined {
  const out: Record<string, string> = {}
  for (const h of headers.value) {
    const k = h.key.trim()
    if (k) out[k] = h.value
  }
  return Object.keys(out).length ? out : undefined
}

// A validated "Connected" / "Failed" result only describes the key+base-URL that
// were tested. Once the user edits either, drop the stale result so the UI never
// shows a green "Connected" for a now-different (possibly broken) key.
watch([apiKey, baseURL], () => {
  validationResult.value = null
})

async function validateConnection() {
  if (!apiKey.value.trim()) return
  validating.value = true
  validationResult.value = null
  try {
    validationResult.value = await api.setupValidate({
      provider: selectedProvider.value || 'openai-compatible',
      api_key: apiKey.value.trim(),
      base_url: baseURL.value.trim() || undefined,
      headers: collectHeaders(),
    })
  } catch (err: unknown) {
    validationResult.value = { valid: false, error: err instanceof Error ? err.message : 'Validation failed' }
  }
  validating.value = false
}

// The model the server picked (registry default, or the custom model id). Shown
// on the done screen.
const resolvedModel = ref('')

async function submitSetup() {
  if (!apiKey.value.trim()) {
    error.value = t('setup.apiKeyRequired')
    return
  }
  if (isCustom.value) {
    if (!customId.value.trim()) {
      error.value = t('setup.customIdRequired')
      return
    }
    if (!baseURL.value.trim()) {
      error.value = t('setup.customUrlRequired')
      return
    }
    if (!customModelId.value.trim()) {
      error.value = t('setup.customModelRequired')
      return
    }
  }
  loading.value = true
  error.value = ''
  try {
    const provider = isCustom.value ? customId.value.trim() : selectedProvider.value
    const res = await api.setupComplete({
      provider,
      api_key: apiKey.value.trim(),
      model: isCustom.value ? customModelId.value.trim() : undefined,
      model_reasoning: isCustom.value ? customReasoning.value : undefined,
      base_url: baseURL.value.trim() || undefined,
      name: isCustom.value ? (customName.value.trim() || customId.value.trim()) : undefined,
      headers: collectHeaders(),
    })
    resolvedModel.value = res.model
    step.value = 'done'
    // Update store state
    store.providerName = provider
    store.modelName = res.model
    // Refresh models list
    store.fetchModels()
  } catch (err: unknown) {
    error.value = err instanceof Error ? err.message : 'Setup failed'
  }
  loading.value = false
}

function finish() {
  emit('complete')
}
</script>

<template>
  <!-- Onboarding is a full-page, framed window so first-run matches the app's
       enclosed look and brand palette (orange), not a one-off green theme. -->
  <div class="setup-viewport" style="z-index: var(--z-modal)">
    <!-- Native title-bar drag strip — the setup overlay sits above .app-shell and
         covers the shell's own drag band, so on the macOS desktop shell the window
         would otherwise be undraggable during first-run. Rendered (via global CSS)
         only inside is-tauri-macos; a no-op element elsewhere. -->
    <div class="titlebar-drag" data-tauri-drag-region aria-hidden="true" />
    <div class="setup-frame">
      <div class="w-full max-w-lg mx-auto px-6">
        <!-- Logo -->
        <div class="flex items-center justify-center gap-0 mb-8 select-none" style="font-family: var(--font-mono); font-size: 32px; font-weight: 700;">
          <span style="color: var(--color-muted-foreground)">[</span><span style="color: var(--color-primary)">J</span><span style="color: var(--color-foreground)">CODE</span><span style="color: var(--color-muted-foreground)">]</span>
        </div>

        <!-- Done state -->
        <div v-if="step === 'done'" class="text-center animate-fade-in">
          <div class="w-16 h-16 rounded-full flex items-center justify-center mx-auto mb-5" style="background: var(--color-success-bg)">
            <CheckIcon class="w-8 h-8" style="color: var(--color-success-fg)" />
          </div>
          <h2 class="text-xl font-semibold mb-2" style="font-family: var(--font-sans); color: var(--color-foreground)">{{ t('setup.allSet') }}</h2>
          <p class="text-sm mb-6" style="color: var(--color-muted-foreground)">
            {{ t('setup.usingModel', { model: resolvedModel, provider: isCustom ? (customName || customId) : (selectedProviderInfo?.name || selectedProvider) }) }}
          </p>
          <button
            class="px-6 py-2.5 rounded-lg text-sm font-medium transition-opacity cursor-pointer shadow-sm hover:opacity-90"
            style="background: var(--color-primary); color: var(--color-on-primary)"
            @click="finish"
          >
            {{ t('setup.startCoding') }}
          </button>
        </div>

        <!-- Setup steps -->
        <div v-else class="rounded-xl overflow-hidden" style="background: var(--color-surface); border: 1px solid var(--color-border); box-shadow: var(--shadow-sm)">
          <!-- Step indicator -->
          <div class="flex items-center gap-2 px-6 pt-5 pb-3">
            <div class="flex items-center gap-1.5">
              <div
                v-for="s in (['provider', 'apikey'] as const)"
                :key="s"
                class="w-2 h-2 rounded-full transition-colors"
                :style="{ backgroundColor: step === s
                  ? 'var(--color-primary)'
                  : (['provider', 'apikey'].indexOf(step) > ['provider', 'apikey'].indexOf(s) ? 'var(--accent-fill)' : 'var(--color-border)') }"
              />
            </div>
            <span class="text-[10px] uppercase tracking-wider ml-auto" style="color: var(--color-muted-foreground)">
              {{ step === 'provider' ? t('setup.step', { n: 1, label: t('setup.chooseProvider') }) : t('setup.step', { n: 2, label: t('setup.enterApiKey') }) }}
            </span>
          </div>

          <!-- Provider selection -->
          <div v-if="step === 'provider'" class="px-6 pb-5">
            <h2 class="text-base font-semibold mb-1" style="font-family: var(--font-sans); color: var(--color-foreground)">{{ t('setup.chooseProvider') }}</h2>
            <p class="text-xs mb-3" style="color: var(--color-muted-foreground)">{{ t('setup.selectProviderDesc') }}</p>

            <input
              v-model="providerSearch"
              type="text"
              :placeholder="t('setup.searchProviders')"
              class="setup-input w-full px-3 py-2 text-sm rounded-lg mb-3 outline-none"
            />

            <div v-if="loading" class="text-center py-8 text-sm animate-pulse" style="color: var(--color-muted-foreground)">{{ t('setup.loadingProviders') }}</div>
            <div v-else-if="error" class="text-center py-8">
              <div class="text-sm mb-3" style="color: var(--color-error-fg)">{{ error }}</div>
              <button class="setup-retry px-4 py-1.5 text-xs rounded-lg cursor-pointer font-medium" @click="loadProviders">{{ t('setup.retry') }}</button>
            </div>
            <div v-else-if="filteredProviders.length === 0" class="text-center py-8 text-sm" style="color: var(--color-muted-foreground)">{{ t('setup.noProviders') }}</div>
            <div v-else class="space-y-1.5 max-h-72 overflow-y-auto pr-1">
              <button
                v-for="p in filteredProviders"
                :key="p.id"
                class="setup-option w-full px-4 py-3 text-left rounded-lg cursor-pointer group"
                :class="{ selected: selectedProvider === p.id }"
                @click="selectProvider(p.id)"
              >
                <div class="flex items-center justify-between gap-2">
                  <div class="flex items-center gap-2.5 min-w-0">
                    <ProviderIcon :provider="p.id" :size="22" />
                    <div class="min-w-0">
                      <div class="text-sm font-medium" style="color: var(--color-foreground)">{{ p.name }}</div>
                      <div v-if="p.doc" class="text-[10px] mt-0.5 truncate" style="color: var(--color-muted-foreground)">{{ p.doc }}</div>
                    </div>
                  </div>
                  <div class="flex items-center gap-2 shrink-0">
                    <span v-if="p.tag === 'recommended'" class="text-[10px] px-1.5 py-0.5 rounded-full font-medium" style="background: var(--accent-wash); color: var(--color-primary)">{{ t('common.recommended') }}</span>
                    <span v-if="p.tag === 'local'" class="text-[10px] px-1.5 py-0.5 rounded-full font-medium" style="background: var(--color-info-bg); color: var(--color-info-fg)">{{ t('common.local') }}</span>
                    <span v-if="p.configured" class="text-[10px] px-1.5 py-0.5 rounded-full font-medium" style="background: var(--color-success-bg); color: var(--color-success-fg)">{{ t('common.configured') }}</span>
                    <ChevronLeftIcon class="setup-chevron w-4 h-4 transition-colors" />
                  </div>
                </div>
              </button>

              <!-- Custom provider entry -->
              <button
                class="setup-option w-full px-4 py-3 text-left rounded-lg cursor-pointer group"
                :class="{ selected: isCustom }"
                @click="selectCustomProvider"
              >
                <div class="flex items-center justify-between gap-2">
                  <div class="flex items-center gap-2.5 min-w-0">
                    <div class="w-[22px] h-[22px] rounded-md grid place-items-center shrink-0" style="background: var(--color-muted)">
                      <span class="text-[11px] font-mono" style="color: var(--color-muted-foreground)">{ }</span>
                    </div>
                    <div class="min-w-0">
                      <div class="text-sm font-medium" style="color: var(--color-foreground)">{{ t('setup.customProvider') }}</div>
                      <div class="text-[10px] mt-0.5 truncate" style="color: var(--color-muted-foreground)">{{ t('setup.customProviderDesc') }}</div>
                    </div>
                  </div>
                  <ChevronLeftIcon class="setup-chevron w-4 h-4 transition-colors" />
                </div>
              </button>
            </div>
          </div>

          <!-- API Key + advanced settings -->
          <div v-if="step === 'apikey'" class="px-6 pb-5">
            <div class="flex items-center gap-2 mb-1">
              <button class="setup-back transition-colors cursor-pointer" @click="goBack">
                <ChevronRightIcon class="w-4 h-4" />
              </button>
              <h2 class="text-base font-semibold" style="font-family: var(--font-sans); color: var(--color-foreground)">{{ t('setup.enterApiKey') }}</h2>
            </div>
            <p class="text-xs mb-4 ml-6" style="color: var(--color-muted-foreground)">
              {{ isCustom ? t('setup.customProvider') : t('setup.for') }} <span class="font-mono">{{ isCustom ? (customName || customId || t('setup.customProvider')) : selectedProviderInfo?.name }}</span>
            </p>

            <div class="space-y-3 ml-6">
              <div v-if="!isCustom && selectedProviderInfo?.env?.length" class="px-3 py-2 rounded-md" style="background: var(--color-muted); border: 1px solid var(--color-border)">
                <div class="text-[10px] mb-1" style="color: var(--color-muted-foreground)">{{ t('setup.envVar') }}</div>
                <div class="text-xs font-mono" style="color: var(--color-foreground)">{{ selectedProviderInfo.env[0] }}</div>
              </div>

              <!-- Custom provider identity fields -->
              <template v-if="isCustom">
                <div>
                  <label class="block text-[10px] uppercase tracking-wider mb-1 font-medium" style="color: var(--color-muted-foreground)">{{ t('setup.customId') }}</label>
                  <input
                    v-model="customId"
                    type="text"
                    :placeholder="t('setup.customIdPlaceholder')"
                    class="setup-input w-full px-3 py-2 text-sm font-mono rounded-lg outline-none"
                  />
                </div>
                <div>
                  <label class="block text-[10px] uppercase tracking-wider mb-1 font-medium" style="color: var(--color-muted-foreground)">{{ t('setup.customName') }}</label>
                  <input
                    v-model="customName"
                    type="text"
                    :placeholder="t('setup.customNamePlaceholder')"
                    class="setup-input w-full px-3 py-2 text-sm rounded-lg outline-none"
                  />
                </div>
                <div>
                  <label class="block text-[10px] uppercase tracking-wider mb-1 font-medium" style="color: var(--color-muted-foreground)">{{ t('setup.customModelId') }}</label>
                  <input
                    v-model="customModelId"
                    type="text"
                    :placeholder="t('setup.customModelPlaceholder')"
                    class="setup-input w-full px-3 py-2 text-sm font-mono rounded-lg outline-none"
                  />
                </div>
                <div class="flex items-center justify-between">
                  <span class="text-[11px]" style="color: var(--color-foreground)">{{ t('setup.customReasoning') }}</span>
                  <button class="setup-switch" :data-on="customReasoning ? 'true' : 'false'" :aria-pressed="customReasoning" @click="customReasoning = !customReasoning" />
                </div>
              </template>

              <div>
                <label class="block text-[10px] uppercase tracking-wider mb-1 font-medium" style="color: var(--color-muted-foreground)">{{ t('setup.apiKey') }}</label>
                <div class="relative">
                  <input
                    v-model="apiKey"
                    :type="showApiKey ? 'text' : 'password'"
                    :placeholder="t('setup.apiKeyPlaceholder')"
                    class="setup-input w-full px-3 py-2 text-sm font-mono rounded-lg outline-none pr-10"
                    :class="{ valid: validationResult?.valid, invalid: validationResult?.valid === false }"
                    @keydown.enter="submitSetup"
                  />
                  <button
                    class="setup-back absolute right-2 top-1/2 -translate-y-1/2 cursor-pointer"
                    @click="showApiKey = !showApiKey"
                  >
                    <EyeIcon v-if="!showApiKey" class="w-4 h-4" />
                    <EyeSlashIcon v-else class="w-4 h-4" />
                  </button>
                </div>
              </div>

              <div>
                <label class="block text-[10px] uppercase tracking-wider mb-1 font-medium" style="color: var(--color-muted-foreground)">{{ t('setup.baseUrl') }}</label>
                <input
                  v-model="baseURL"
                  type="text"
                  :placeholder="isCustom ? 'https://your-endpoint/v1' : (selectedProviderInfo?.api || 'https://api.example.com/v1')"
                  class="setup-input w-full px-3 py-2 text-sm font-mono rounded-lg outline-none"
                  @keydown.enter="submitSetup"
                />
              </div>

              <!-- Advanced toggle -->
              <button class="setup-adv-toggle" :aria-expanded="advancedOpen" @click="advancedOpen = !advancedOpen">
                <ChevronRightIcon class="w-3 h-3 transition-transform" :style="{ transform: advancedOpen ? 'rotate(90deg)' : 'none' }" />
                {{ t('setup.advanced') }}
              </button>

              <div v-if="advancedOpen" class="space-y-3 pt-1">
                <!-- Custom headers -->
                <div>
                  <div class="flex items-center justify-between mb-1">
                    <label class="text-[10px] uppercase tracking-wider font-medium" style="color: var(--color-muted-foreground)">{{ t('setup.headers') }}</label>
                    <button class="setup-secondary px-2 py-0.5 text-[10px] rounded cursor-pointer" @click="addHeaderRow">+ {{ t('setup.addHeader') }}</button>
                  </div>
                  <div v-for="(h, i) in headers" :key="i" class="flex items-center gap-1.5 mb-1">
                    <input v-model="h.key" type="text" :placeholder="t('setup.headerKey')" class="setup-input flex-1 px-2 py-1.5 text-xs font-mono rounded outline-none" />
                    <input v-model="h.value" type="text" :placeholder="t('setup.headerValue')" class="setup-input flex-1 px-2 py-1.5 text-xs font-mono rounded outline-none" />
                    <button class="setup-back cursor-pointer px-1" @click="removeHeaderRow(i)">✕</button>
                  </div>
                </div>
              </div>

              <!-- Validate connection -->
              <div class="flex items-center gap-2">
                <button
                  :disabled="validating || !apiKey.trim()"
                  class="setup-secondary px-3 py-1.5 text-xs rounded-lg disabled:opacity-50 cursor-pointer transition-colors"
                  @click="validateConnection"
                >
                  {{ validating ? t('setup.checking') : t('setup.testConnection') }}
                </button>
                <span v-if="validationResult?.valid" class="text-xs flex items-center gap-1" style="color: var(--color-success-fg)">
                  <CheckCircleIcon class="w-3.5 h-3.5" />
                  {{ t('setup.connected') }}
                </span>
                <span v-if="validationResult?.valid === false" class="text-xs" style="color: var(--color-error-fg)">{{ validationResult.error }}</span>
              </div>

              <!-- Error -->
              <div v-if="error" class="px-3 py-2 rounded-md" style="background: var(--color-error-bg); border: 1px solid var(--color-error-fg)">
                <span class="text-xs" style="color: var(--color-error-fg)">{{ error }}</span>
              </div>

              <button
                :disabled="loading || !apiKey.trim()"
                class="w-full px-4 py-2.5 rounded-lg text-sm font-medium transition-opacity cursor-pointer shadow-sm hover:opacity-90 disabled:opacity-50 disabled:cursor-not-allowed"
                style="background: var(--color-primary); color: var(--color-on-primary)"
                @click="submitSetup"
              >
                {{ loading ? t('setup.settingUp') : t('setup.completeSetup') }}
              </button>
            </div>
          </div>
        </div>

        <!-- Footer hint -->
        <p v-if="step !== 'done'" class="text-center text-[10px] mt-4" style="color: var(--color-muted-foreground)">
          {{ t('setup.savedTo') }}
        </p>
      </div>
    </div>
  </div>
</template>

<style scoped>
.setup-viewport {
  position: fixed;
  inset: 0;
  background: var(--color-background);
  display: flex;
}

.setup-frame {
  flex: 1;
  display: flex;
  align-items: center;
  justify-content: center;
  overflow: auto;
}

.setup-retry {
  background: var(--color-primary);
  color: var(--color-on-primary);
  transition: opacity 0.15s;
}
.setup-retry:hover {
  opacity: 0.9;
}

.setup-input {
  background: var(--color-background);
  border: 1px solid var(--color-border);
  color: var(--color-foreground);
  transition: border-color 0.15s;
}
.setup-input::placeholder {
  color: var(--color-muted-foreground);
}
.setup-input:focus {
  border-color: var(--color-primary);
}
.setup-input.valid {
  border-color: var(--color-success-fg);
}
.setup-input.invalid {
  border-color: var(--color-error-fg);
}

.setup-option {
  border: 1px solid var(--color-border);
  background: transparent;
  transition: border-color 0.15s, background 0.15s;
}
.setup-option:hover {
  border-color: color-mix(in srgb, var(--color-primary) 55%, var(--color-border));
  background: var(--color-muted);
}
.setup-option.selected {
  border-color: var(--color-primary);
  background: var(--accent-wash-soft);
}

.setup-chevron {
  color: var(--color-border);
}
.setup-option:hover .setup-chevron {
  color: var(--color-primary);
}

.setup-back {
  color: var(--color-muted-foreground);
}
.setup-back:hover {
  color: var(--color-foreground);
}

.setup-secondary {
  border: 1px solid var(--color-border);
  color: var(--color-muted-foreground);
}
.setup-secondary:hover {
  background: var(--color-muted);
  color: var(--color-foreground);
}

.setup-adv-toggle {
  display: flex;
  align-items: center;
  gap: 0.25rem;
  font-size: 11px;
  color: var(--color-muted-foreground);
  cursor: pointer;
  background: transparent;
  border: none;
  padding: 0;
}
.setup-adv-toggle:hover {
  color: var(--color-foreground);
}

.setup-switch {
  width: 28px;
  height: 16px;
  border-radius: 9999px;
  background: var(--color-muted);
  border: 1px solid var(--color-border);
  position: relative;
  cursor: pointer;
  transition: background-color 0.15s;
  flex-shrink: 0;
}
.setup-switch::after {
  content: '';
  position: absolute;
  top: 1px;
  left: 1px;
  width: 12px;
  height: 12px;
  border-radius: 50%;
  background: var(--color-foreground);
  transition: transform 0.15s;
}
.setup-switch[data-on='true'] {
  background: var(--color-primary);
}
.setup-switch[data-on='true']::after {
  transform: translateX(12px);
  background: var(--color-on-primary);
}
</style>
