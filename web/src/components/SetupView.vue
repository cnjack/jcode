<script setup lang="ts">
import { ref, computed, onMounted, watch } from 'vue'
import { useChatStore } from '@/stores/chat'
import { api } from '@/composables/api'
import type { SetupProvider, SetupModel } from '@/types/api'

const emit = defineEmits<{
  complete: []
}>()

const store = useChatStore()

type Step = 'provider' | 'model' | 'apikey' | 'done'
const step = ref<Step>('provider')
const loading = ref(false)
const error = ref('')

// Step 1: Provider selection
const providers = ref<SetupProvider[]>([])
const selectedProvider = ref('')
const providerSearch = ref('')

// Step 2: Model selection
const models = ref<SetupModel[]>([])
const selectedModel = ref('')
const modelSearch = ref('')

// Step 3: API Key
const apiKey = ref('')
const baseURL = ref('')
const showApiKey = ref(false)
const validating = ref(false)
const validationResult = ref<{ valid: boolean; error?: string } | null>(null)

const filteredProviders = computed(() => {
  const q = providerSearch.value.toLowerCase()
  return providers.value.filter(p =>
    p.name.toLowerCase().includes(q) || p.id.toLowerCase().includes(q)
  )
})

const filteredModels = computed(() => {
  const q = modelSearch.value.toLowerCase()
  return models.value.filter(m =>
    m.name.toLowerCase().includes(q) || m.id.toLowerCase().includes(q)
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

// Model list loader for the currently-selected provider — retryable from the
// model step.
async function loadModels() {
  loading.value = true
  error.value = ''
  models.value = []
  try {
    models.value = await api.setupProviderModels(selectedProvider.value)
  } catch {
    error.value = 'Failed to load models'
  }
  loading.value = false
}

async function selectProvider(id: string) {
  // Switching to a different provider invalidates any key/base-url/validation
  // entered for the previous one — clear them so provider A's secret can't leak
  // into provider B's form and get submitted to B's endpoint.
  if (id !== selectedProvider.value) {
    apiKey.value = ''
    baseURL.value = ''
    showApiKey.value = false
    validationResult.value = null
  }
  selectedProvider.value = id
  modelSearch.value = ''
  // Advance to the model step regardless, so a load failure shows there with a
  // Retry (instead of silently keeping the user on the provider step).
  step.value = 'model'
  await loadModels()
}

function selectModel(id: string) {
  selectedModel.value = id
  step.value = 'apikey'
}

function goBack() {
  error.value = ''
  validationResult.value = null
  if (step.value === 'model') {
    step.value = 'provider'
    selectedProvider.value = ''
    models.value = []
    // Also clear the api-key step fields so a different provider chosen next
    // doesn't inherit the previous one's key/base URL.
    apiKey.value = ''
    baseURL.value = ''
    showApiKey.value = false
  } else if (step.value === 'apikey') {
    step.value = 'model'
    selectedModel.value = ''
  }
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
      provider: selectedProvider.value,
      api_key: apiKey.value.trim(),
      base_url: baseURL.value.trim() || undefined,
    })
  } catch (err: unknown) {
    validationResult.value = { valid: false, error: err instanceof Error ? err.message : 'Validation failed' }
  }
  validating.value = false
}

async function submitSetup() {
  if (!apiKey.value.trim()) {
    error.value = 'API Key is required'
    return
  }
  loading.value = true
  error.value = ''
  try {
    await api.setupComplete({
      provider: selectedProvider.value,
      model: selectedModel.value,
      api_key: apiKey.value.trim(),
      base_url: baseURL.value.trim() || undefined,
    })
    step.value = 'done'
    // Update store state
    store.providerName = selectedProvider.value
    store.modelName = selectedModel.value
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
            <svg class="w-8 h-8" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" style="color: var(--color-success-fg)">
              <path d="M5 13l4 4L19 7" />
            </svg>
          </div>
          <h2 class="text-xl font-semibold mb-2" style="font-family: var(--font-sans); color: var(--color-foreground)">You're all set!</h2>
          <p class="text-sm mb-6" style="color: var(--color-muted-foreground)">
            Using <span class="font-mono" style="color: var(--color-foreground)">{{ selectedModel }}</span> via <span class="font-mono" style="color: var(--color-foreground)">{{ selectedProviderInfo?.name || selectedProvider }}</span>
          </p>
          <button
            class="px-6 py-2.5 rounded-lg text-sm font-medium transition-opacity cursor-pointer shadow-sm hover:opacity-90"
            style="background: var(--color-primary); color: var(--color-on-primary, #fff)"
            @click="finish"
          >
            Start coding
          </button>
        </div>

        <!-- Setup steps -->
        <div v-else class="rounded-xl overflow-hidden" style="background: var(--color-surface); border: 1px solid var(--color-border); box-shadow: var(--shadow-sm)">
          <!-- Step indicator -->
          <div class="flex items-center gap-2 px-6 pt-5 pb-3">
            <div class="flex items-center gap-1.5">
              <div
                v-for="s in (['provider', 'model', 'apikey'] as const)"
                :key="s"
                class="w-2 h-2 rounded-full transition-colors"
                :style="{ backgroundColor: step === s
                  ? 'var(--color-primary)'
                  : (['provider', 'model', 'apikey'].indexOf(step) > ['provider', 'model', 'apikey'].indexOf(s) ? 'var(--accent-fill)' : 'var(--color-border)') }"
              />
            </div>
            <span class="text-[10px] uppercase tracking-wider ml-auto" style="color: var(--color-muted-foreground)">
              {{ step === 'provider' ? 'Step 1: Choose Provider' : step === 'model' ? 'Step 2: Choose Model' : 'Step 3: API Key' }}
            </span>
          </div>

          <!-- Provider selection -->
          <div v-if="step === 'provider'" class="px-6 pb-5">
            <h2 class="text-base font-semibold mb-1" style="font-family: var(--font-sans); color: var(--color-foreground)">Choose a Provider</h2>
            <p class="text-xs mb-3" style="color: var(--color-muted-foreground)">Select the AI provider you'd like to use.</p>

            <input
              v-model="providerSearch"
              type="text"
              placeholder="Search providers..."
              class="setup-input w-full px-3 py-2 text-sm rounded-lg mb-3 outline-none"
            />

            <div v-if="loading" class="text-center py-8 text-sm animate-pulse" style="color: var(--color-muted-foreground)">Loading providers...</div>
            <div v-else-if="error" class="text-center py-8">
              <div class="text-sm mb-3" style="color: var(--color-error-fg)">{{ error }}</div>
              <button class="setup-retry px-4 py-1.5 text-xs rounded-lg cursor-pointer font-medium" @click="loadProviders">Retry</button>
            </div>
            <div v-else-if="filteredProviders.length === 0" class="text-center py-8 text-sm" style="color: var(--color-muted-foreground)">No providers found</div>
            <div v-else class="space-y-1.5 max-h-72 overflow-y-auto pr-1">
              <button
                v-for="p in filteredProviders"
                :key="p.id"
                class="setup-option w-full px-4 py-3 text-left rounded-lg cursor-pointer group"
                :class="{ selected: selectedProvider === p.id }"
                @click="selectProvider(p.id)"
              >
                <div class="flex items-center justify-between">
                  <div>
                    <div class="text-sm font-medium" style="color: var(--color-foreground)">{{ p.name }}</div>
                    <div v-if="p.doc" class="text-[10px] mt-0.5" style="color: var(--color-muted-foreground)">{{ p.doc }}</div>
                  </div>
                  <div class="flex items-center gap-2">
                    <span v-if="p.tag === 'recommended'" class="text-[10px] px-1.5 py-0.5 rounded-full font-medium" style="background: var(--accent-wash); color: var(--color-primary)">Recommended</span>
                    <span v-if="p.tag === 'local'" class="text-[10px] px-1.5 py-0.5 rounded-full font-medium" style="background: var(--color-info-bg); color: var(--color-info-fg)">Local</span>
                    <span v-if="p.configured" class="text-[10px] px-1.5 py-0.5 rounded-full font-medium" style="background: var(--color-success-bg); color: var(--color-success-fg)">configured</span>
                    <svg class="setup-chevron w-4 h-4 transition-colors" viewBox="0 0 20 20" fill="currentColor">
                      <path fill-rule="evenodd" d="M7.21 14.77a.75.75 0 01.02-1.06L11.168 10 7.23 6.29a.75.75 0 111.04-1.08l4.5 4.25a.75.75 0 010 1.08l-4.5 4.25a.75.75 0 01-1.06-.02z" clip-rule="evenodd" />
                    </svg>
                  </div>
                </div>
              </button>
            </div>
          </div>

          <!-- Model selection -->
          <div v-if="step === 'model'" class="px-6 pb-5">
            <div class="flex items-center gap-2 mb-1">
              <button class="setup-back transition-colors cursor-pointer" @click="goBack">
                <svg class="w-4 h-4" viewBox="0 0 20 20" fill="currentColor">
                  <path fill-rule="evenodd" d="M12.79 5.23a.75.75 0 01-.02 1.06L8.832 10l3.938 3.71a.75.75 0 11-1.04 1.08l-4.5-4.25a.75.75 0 010-1.08l4.5-4.25a.75.75 0 011.06.02z" clip-rule="evenodd" />
                </svg>
              </button>
              <h2 class="text-base font-semibold" style="font-family: var(--font-sans); color: var(--color-foreground)">Choose a Model</h2>
            </div>
            <p class="text-xs mb-3 ml-6" style="color: var(--color-muted-foreground)">For <span class="font-mono">{{ selectedProviderInfo?.name }}</span></p>

            <input
              v-model="modelSearch"
              type="text"
              placeholder="Search models..."
              class="setup-input w-full px-3 py-2 text-sm rounded-lg mb-3 outline-none"
            />

            <div v-if="loading" class="text-center py-8 text-sm animate-pulse" style="color: var(--color-muted-foreground)">Loading models...</div>
            <div v-else-if="error" class="text-center py-8">
              <div class="text-sm mb-3" style="color: var(--color-error-fg)">{{ error }}</div>
              <button class="setup-retry px-4 py-1.5 text-xs rounded-lg cursor-pointer font-medium" @click="loadModels">Retry</button>
            </div>
            <div v-else-if="filteredModels.length === 0" class="text-center py-8 text-sm" style="color: var(--color-muted-foreground)">No models found</div>
            <div v-else class="space-y-1 max-h-72 overflow-y-auto pr-1">
              <button
                v-for="m in filteredModels"
                :key="m.id"
                class="setup-option w-full px-4 py-2.5 text-left rounded-lg cursor-pointer"
                :class="{ selected: selectedModel === m.id }"
                @click="selectModel(m.id)"
              >
                <div class="flex items-center justify-between">
                  <div>
                    <div class="text-sm font-medium font-mono" style="color: var(--color-foreground)">{{ m.id }}</div>
                    <div v-if="m.name && m.name !== m.id" class="text-[10px] mt-0.5" style="color: var(--color-muted-foreground)">{{ m.name }}</div>
                  </div>
                  <div class="flex items-center gap-2">
                    <span v-if="m.context_limit" class="text-[10px]" style="color: var(--color-muted-foreground)">{{ (m.context_limit / 1000).toFixed(0) }}k ctx</span>
                    <span v-if="m.reasoning" class="text-[10px] px-1.5 py-0.5 rounded-full" style="background: var(--color-info-bg); color: var(--color-info-fg)">reasoning</span>
                    <svg class="setup-chevron w-4 h-4" viewBox="0 0 20 20" fill="currentColor">
                      <path fill-rule="evenodd" d="M7.21 14.77a.75.75 0 01.02-1.06L11.168 10 7.23 6.29a.75.75 0 111.04-1.08l4.5 4.25a.75.75 0 010 1.08l-4.5 4.25a.75.75 0 01-1.06-.02z" clip-rule="evenodd" />
                    </svg>
                  </div>
                </div>
              </button>
            </div>
          </div>

          <!-- API Key input -->
          <div v-if="step === 'apikey'" class="px-6 pb-5">
            <div class="flex items-center gap-2 mb-1">
              <button class="setup-back transition-colors cursor-pointer" @click="goBack">
                <svg class="w-4 h-4" viewBox="0 0 20 20" fill="currentColor">
                  <path fill-rule="evenodd" d="M12.79 5.23a.75.75 0 01-.02 1.06L8.832 10l3.938 3.71a.75.75 0 11-1.04 1.08l-4.5-4.25a.75.75 0 010-1.08l4.5-4.25a.75.75 0 011.06.02z" clip-rule="evenodd" />
                </svg>
              </button>
              <h2 class="text-base font-semibold" style="font-family: var(--font-sans); color: var(--color-foreground)">Enter API Key</h2>
            </div>
            <p class="text-xs mb-4 ml-6" style="color: var(--color-muted-foreground)">
              For <span class="font-mono">{{ selectedProviderInfo?.name }}</span> · <span class="font-mono">{{ selectedModel }}</span>
            </p>

            <div class="space-y-3 ml-6">
              <div v-if="selectedProviderInfo?.env?.length" class="px-3 py-2 rounded-md" style="background: var(--color-muted); border: 1px solid var(--color-border)">
                <div class="text-[10px] mb-1" style="color: var(--color-muted-foreground)">Environment variable</div>
                <div class="text-xs font-mono" style="color: var(--color-foreground)">{{ selectedProviderInfo.env[0] }}</div>
              </div>

              <div>
                <label class="block text-[10px] uppercase tracking-wider mb-1 font-medium" style="color: var(--color-muted-foreground)">API Key</label>
                <div class="relative">
                  <input
                    v-model="apiKey"
                    :type="showApiKey ? 'text' : 'password'"
                    placeholder="sk-..."
                    class="setup-input w-full px-3 py-2 text-sm font-mono rounded-lg outline-none pr-10"
                    :class="{ valid: validationResult?.valid, invalid: validationResult?.valid === false }"
                    @keydown.enter="submitSetup"
                  />
                  <button
                    class="setup-back absolute right-2 top-1/2 -translate-y-1/2 cursor-pointer"
                    @click="showApiKey = !showApiKey"
                  >
                    <svg v-if="!showApiKey" class="w-4 h-4" viewBox="0 0 20 20" fill="currentColor">
                      <path d="M10 12.5a2.5 2.5 0 100-5 2.5 2.5 0 000 5z" />
                      <path fill-rule="evenodd" d="M.664 10.59a1.651 1.651 0 010-1.186A10.004 10.004 0 0110 3c4.257 0 7.893 2.66 9.336 6.41.147.381.146.804 0 1.186A10.004 10.004 0 0110 17c-4.257 0-7.893-2.66-9.336-6.41zM14 10a4 4 0 11-8 0 4 4 0 018 0z" clip-rule="evenodd" />
                    </svg>
                    <svg v-else class="w-4 h-4" viewBox="0 0 20 20" fill="currentColor">
                      <path fill-rule="evenodd" d="M3.28 2.22a.75.75 0 00-1.06 1.06l14.5 14.5a.75.75 0 101.06-1.06l-14.5-14.5z" clip-rule="evenodd" />
                      <path d="M4.262 6.49A8.97 8.97 0 002.175 10.3a1.655 1.655 0 000 .4 10.004 10.004 0 007.548 5.953 8.97 8.97 0 004.988-.628l-1.446-1.446a4.003 4.003 0 01-5.54-5.54L4.262 6.49z" />
                    </svg>
                  </button>
                </div>
              </div>

              <div>
                <label class="block text-[10px] uppercase tracking-wider mb-1 font-medium" style="color: var(--color-muted-foreground)">Base URL <span class="normal-case">(optional)</span></label>
                <input
                  v-model="baseURL"
                  type="text"
                  :placeholder="selectedProviderInfo?.api || 'https://api.example.com/v1'"
                  class="setup-input w-full px-3 py-2 text-sm font-mono rounded-lg outline-none"
                  @keydown.enter="submitSetup"
                />
              </div>

              <!-- Validate connection -->
              <div class="flex items-center gap-2">
                <button
                  :disabled="validating || !apiKey.trim()"
                  class="setup-secondary px-3 py-1.5 text-xs rounded-lg disabled:opacity-50 cursor-pointer transition-colors"
                  @click="validateConnection"
                >
                  {{ validating ? 'Checking...' : 'Test Connection' }}
                </button>
                <span v-if="validationResult?.valid" class="text-xs flex items-center gap-1" style="color: var(--color-success-fg)">
                  <svg class="w-3.5 h-3.5" viewBox="0 0 20 20" fill="currentColor"><path fill-rule="evenodd" d="M10 18a8 8 0 100-16 8 8 0 000 16zm3.857-9.809a.75.75 0 00-1.214-.882l-3.483 4.79-1.88-1.88a.75.75 0 10-1.06 1.061l2.5 2.5a.75.75 0 001.137-.089l4-5.5z" clip-rule="evenodd" /></svg>
                  Connected
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
                style="background: var(--color-primary); color: var(--color-on-primary, #fff)"
                @click="submitSetup"
              >
                {{ loading ? 'Setting up...' : 'Complete Setup' }}
              </button>
            </div>
          </div>
        </div>

        <!-- Footer hint -->
        <p v-if="step !== 'done'" class="text-center text-[10px] mt-4" style="color: var(--color-muted-foreground)">
          Configuration saved to <span class="font-mono">~/.jcode/config.json</span>
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
  color: var(--color-on-primary, #fff);
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
</style>
