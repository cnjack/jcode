<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
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

onMounted(async () => {
  loading.value = true
  try {
    providers.value = await api.setupProviders()
  } catch {
    error.value = 'Failed to load providers'
  }
  loading.value = false
})

async function selectProvider(id: string) {
  selectedProvider.value = id
  loading.value = true
  error.value = ''
  try {
    models.value = await api.setupProviderModels(id)
    step.value = 'model'
    modelSearch.value = ''
  } catch {
    error.value = 'Failed to load models'
  }
  loading.value = false
}

function selectModel(id: string) {
  selectedModel.value = id
  step.value = 'apikey'
  // Auto-fill base URL from provider info if available
  const prov = selectedProviderInfo.value
  if (prov?.api && !baseURL.value) {
    baseURL.value = ''
    // Don't auto-fill, the backend will use the default
  }
}

function goBack() {
  error.value = ''
  validationResult.value = null
  if (step.value === 'model') {
    step.value = 'provider'
    selectedProvider.value = ''
    models.value = []
  } else if (step.value === 'apikey') {
    step.value = 'model'
    selectedModel.value = ''
  }
}

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
  <div class="fixed inset-0 bg-zinc-50 dark:bg-zinc-950 flex items-center justify-center z-50">
    <div class="w-full max-w-lg mx-auto px-6">
      <!-- Logo -->
      <div class="flex items-center justify-center gap-0 mb-8 select-none" style="font-family: 'JetBrains Mono', ui-monospace, SFMono-Regular, 'Roboto Mono', Menlo, Monaco, monospace; font-size: 32px; font-weight: 700;">
        <span class="text-zinc-400 dark:text-zinc-500">[</span><span style="color: #FF8400;">J</span><span class="text-zinc-900 dark:text-zinc-300">CODE</span><span class="text-zinc-400 dark:text-zinc-500">]</span>
      </div>

      <!-- Done state -->
      <div v-if="step === 'done'" class="text-center animate-fade-in">
        <div class="w-16 h-16 rounded-full bg-emerald-100 dark:bg-emerald-500/15 flex items-center justify-center mx-auto mb-5">
          <svg class="w-8 h-8 text-emerald-600 dark:text-emerald-400" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
            <path d="M5 13l4 4L19 7" />
          </svg>
        </div>
        <h2 class="text-xl font-semibold text-zinc-800 dark:text-zinc-100 mb-2" style="font-family: var(--font-sans)">You're all set!</h2>
        <p class="text-sm text-zinc-500 dark:text-zinc-400 mb-6">
          Using <span class="font-mono text-zinc-700 dark:text-zinc-300">{{ selectedModel }}</span> via <span class="font-mono text-zinc-700 dark:text-zinc-300">{{ selectedProviderInfo?.name || selectedProvider }}</span>
        </p>
        <button
          class="px-6 py-2.5 bg-emerald-500 hover:bg-emerald-600 text-white rounded-lg text-sm font-medium transition-colors cursor-pointer shadow-sm"
          @click="finish"
        >
          Start coding
        </button>
      </div>

      <!-- Setup steps -->
      <div v-else class="bg-white dark:bg-zinc-900 border border-zinc-200 dark:border-zinc-700 rounded-xl shadow-sm overflow-hidden">
        <!-- Step indicator -->
        <div class="flex items-center gap-2 px-6 pt-5 pb-3">
          <div class="flex items-center gap-1.5">
            <div
              v-for="s in (['provider', 'model', 'apikey'] as const)"
              :key="s"
              class="w-2 h-2 rounded-full transition-colors"
              :class="step === s ? 'bg-emerald-500' : ['provider', 'model', 'apikey'].indexOf(step) > ['provider', 'model', 'apikey'].indexOf(s) ? 'bg-emerald-400' : 'bg-zinc-300 dark:bg-zinc-600'"
            />
          </div>
          <span class="text-[10px] text-zinc-400 dark:text-zinc-500 uppercase tracking-wider ml-auto">
            {{ step === 'provider' ? 'Step 1: Choose Provider' : step === 'model' ? 'Step 2: Choose Model' : 'Step 3: API Key' }}
          </span>
        </div>

        <!-- Provider selection -->
        <div v-if="step === 'provider'" class="px-6 pb-5">
          <h2 class="text-base font-semibold text-zinc-800 dark:text-zinc-100 mb-1" style="font-family: var(--font-sans)">Choose a Provider</h2>
          <p class="text-xs text-zinc-500 dark:text-zinc-400 mb-3">Select the AI provider you'd like to use.</p>

          <input
            v-model="providerSearch"
            type="text"
            placeholder="Search providers..."
            class="w-full px-3 py-2 text-sm bg-zinc-50 dark:bg-zinc-800 border border-zinc-200 dark:border-zinc-700 rounded-lg mb-3 outline-none focus:border-emerald-400 dark:focus:border-emerald-500 transition-colors"
          />

          <div v-if="loading" class="text-center py-8 text-sm text-zinc-400 animate-pulse">Loading providers...</div>
          <div v-else-if="filteredProviders.length === 0" class="text-center py-8 text-sm text-zinc-400">No providers found</div>
          <div v-else class="space-y-1.5 max-h-72 overflow-y-auto pr-1">
            <button
              v-for="p in filteredProviders"
              :key="p.id"
              class="w-full px-4 py-3 text-left rounded-lg border transition-all cursor-pointer group"
              :class="selectedProvider === p.id
                ? 'border-emerald-400 dark:border-emerald-500 bg-emerald-50 dark:bg-emerald-500/10'
                : 'border-zinc-200 dark:border-zinc-700 hover:border-emerald-300 dark:hover:border-emerald-600 hover:bg-zinc-50 dark:hover:bg-zinc-800'"
              @click="selectProvider(p.id)"
            >
              <div class="flex items-center justify-between">
                <div>
                  <div class="text-sm font-medium text-zinc-800 dark:text-zinc-100">{{ p.name }}</div>
                  <div v-if="p.doc" class="text-[10px] text-zinc-400 dark:text-zinc-500 mt-0.5">{{ p.doc }}</div>
                </div>
                <div class="flex items-center gap-2">
                  <span v-if="p.tag === 'recommended'" class="text-[10px] px-1.5 py-0.5 rounded-full bg-amber-100 dark:bg-amber-500/15 text-amber-600 dark:text-amber-400 font-medium">Recommended</span>
                  <span v-if="p.tag === 'local'" class="text-[10px] px-1.5 py-0.5 rounded-full bg-blue-100 dark:bg-blue-500/15 text-blue-600 dark:text-blue-400 font-medium">Local</span>
                  <span v-if="p.configured" class="text-[10px] px-1.5 py-0.5 rounded-full bg-emerald-100 dark:bg-emerald-500/15 text-emerald-600 dark:text-emerald-400 font-medium">configured</span>
                  <svg class="w-4 h-4 text-zinc-300 dark:text-zinc-600 group-hover:text-emerald-400 transition-colors" viewBox="0 0 20 20" fill="currentColor">
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
            <button class="text-zinc-400 hover:text-zinc-600 dark:hover:text-zinc-300 transition-colors cursor-pointer" @click="goBack">
              <svg class="w-4 h-4" viewBox="0 0 20 20" fill="currentColor">
                <path fill-rule="evenodd" d="M12.79 5.23a.75.75 0 01-.02 1.06L8.832 10l3.938 3.71a.75.75 0 11-1.04 1.08l-4.5-4.25a.75.75 0 010-1.08l4.5-4.25a.75.75 0 011.06.02z" clip-rule="evenodd" />
              </svg>
            </button>
            <h2 class="text-base font-semibold text-zinc-800 dark:text-zinc-100" style="font-family: var(--font-sans)">Choose a Model</h2>
          </div>
          <p class="text-xs text-zinc-500 dark:text-zinc-400 mb-3 ml-6">For <span class="font-mono">{{ selectedProviderInfo?.name }}</span></p>

          <input
            v-model="modelSearch"
            type="text"
            placeholder="Search models..."
            class="w-full px-3 py-2 text-sm bg-zinc-50 dark:bg-zinc-800 border border-zinc-200 dark:border-zinc-700 rounded-lg mb-3 outline-none focus:border-emerald-400 dark:focus:border-emerald-500 transition-colors"
          />

          <div v-if="loading" class="text-center py-8 text-sm text-zinc-400 animate-pulse">Loading models...</div>
          <div v-else-if="filteredModels.length === 0" class="text-center py-8 text-sm text-zinc-400">No models found</div>
          <div v-else class="space-y-1 max-h-72 overflow-y-auto pr-1">
            <button
              v-for="m in filteredModels"
              :key="m.id"
              class="w-full px-4 py-2.5 text-left rounded-lg border transition-all cursor-pointer"
              :class="selectedModel === m.id
                ? 'border-emerald-400 dark:border-emerald-500 bg-emerald-50 dark:bg-emerald-500/10'
                : 'border-zinc-200 dark:border-zinc-700 hover:border-emerald-300 dark:hover:border-emerald-600 hover:bg-zinc-50 dark:hover:bg-zinc-800'"
              @click="selectModel(m.id)"
            >
              <div class="flex items-center justify-between">
                <div>
                  <div class="text-sm font-medium text-zinc-800 dark:text-zinc-100 font-mono">{{ m.id }}</div>
                  <div v-if="m.name && m.name !== m.id" class="text-[10px] text-zinc-400 dark:text-zinc-500 mt-0.5">{{ m.name }}</div>
                </div>
                <div class="flex items-center gap-2">
                  <span v-if="m.context_limit" class="text-[10px] text-zinc-400 dark:text-zinc-500">{{ (m.context_limit / 1000).toFixed(0) }}k ctx</span>
                  <span v-if="m.reasoning" class="text-[10px] px-1.5 py-0.5 rounded-full bg-blue-100 dark:bg-blue-500/15 text-blue-600 dark:text-blue-400">reasoning</span>
                  <svg class="w-4 h-4 text-zinc-300 dark:text-zinc-600" viewBox="0 0 20 20" fill="currentColor">
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
            <button class="text-zinc-400 hover:text-zinc-600 dark:hover:text-zinc-300 transition-colors cursor-pointer" @click="goBack">
              <svg class="w-4 h-4" viewBox="0 0 20 20" fill="currentColor">
                <path fill-rule="evenodd" d="M12.79 5.23a.75.75 0 01-.02 1.06L8.832 10l3.938 3.71a.75.75 0 11-1.04 1.08l-4.5-4.25a.75.75 0 010-1.08l4.5-4.25a.75.75 0 011.06.02z" clip-rule="evenodd" />
              </svg>
            </button>
            <h2 class="text-base font-semibold text-zinc-800 dark:text-zinc-100" style="font-family: var(--font-sans)">Enter API Key</h2>
          </div>
          <p class="text-xs text-zinc-500 dark:text-zinc-400 mb-4 ml-6">
            For <span class="font-mono">{{ selectedProviderInfo?.name }}</span> · <span class="font-mono">{{ selectedModel }}</span>
          </p>

          <div class="space-y-3 ml-6">
            <div v-if="selectedProviderInfo?.env?.length" class="px-3 py-2 rounded-md bg-zinc-50 dark:bg-zinc-800 border border-zinc-200 dark:border-zinc-700">
              <div class="text-[10px] text-zinc-400 dark:text-zinc-500 mb-1">Environment variable</div>
              <div class="text-xs font-mono text-zinc-600 dark:text-zinc-300">{{ selectedProviderInfo.env[0] }}</div>
            </div>

            <div>
              <label class="block text-[10px] text-zinc-400 dark:text-zinc-500 uppercase tracking-wider mb-1 font-medium">API Key</label>
              <div class="relative">
                <input
                  v-model="apiKey"
                  :type="showApiKey ? 'text' : 'password'"
                  placeholder="sk-..."
                  class="w-full px-3 py-2 text-sm font-mono bg-zinc-50 dark:bg-zinc-800 border rounded-lg outline-none transition-colors pr-10"
                  :class="validationResult?.valid ? 'border-emerald-400 dark:border-emerald-500' : validationResult?.valid === false ? 'border-red-300 dark:border-red-500' : 'border-zinc-200 dark:border-zinc-700 focus:border-emerald-400 dark:focus:border-emerald-500'"
                  @keydown.enter="submitSetup"
                />
                <button
                  class="absolute right-2 top-1/2 -translate-y-1/2 text-zinc-400 hover:text-zinc-600 dark:hover:text-zinc-300 cursor-pointer"
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
              <label class="block text-[10px] text-zinc-400 dark:text-zinc-500 uppercase tracking-wider mb-1 font-medium">Base URL <span class="normal-case">(optional)</span></label>
              <input
                v-model="baseURL"
                type="text"
                :placeholder="selectedProviderInfo?.api || 'https://api.example.com/v1'"
                class="w-full px-3 py-2 text-sm font-mono bg-zinc-50 dark:bg-zinc-800 border border-zinc-200 dark:border-zinc-700 rounded-lg outline-none focus:border-emerald-400 dark:focus:border-emerald-500 transition-colors"
                @keydown.enter="submitSetup"
              />
            </div>

            <!-- Validate connection -->
            <div class="flex items-center gap-2">
              <button
                :disabled="validating || !apiKey.trim()"
                class="px-3 py-1.5 text-xs border border-zinc-200 dark:border-zinc-700 rounded-lg hover:bg-zinc-50 dark:hover:bg-zinc-800 disabled:opacity-50 cursor-pointer transition-colors text-zinc-600 dark:text-zinc-300"
                @click="validateConnection"
              >
                {{ validating ? 'Checking...' : 'Test Connection' }}
              </button>
              <span v-if="validationResult?.valid" class="text-xs text-emerald-600 dark:text-emerald-400 flex items-center gap-1">
                <svg class="w-3.5 h-3.5" viewBox="0 0 20 20" fill="currentColor"><path fill-rule="evenodd" d="M10 18a8 8 0 100-16 8 8 0 000 16zm3.857-9.809a.75.75 0 00-1.214-.882l-3.483 4.79-1.88-1.88a.75.75 0 10-1.06 1.061l2.5 2.5a.75.75 0 001.137-.089l4-5.5z" clip-rule="evenodd" /></svg>
                Connected
              </span>
              <span v-if="validationResult?.valid === false" class="text-xs text-red-500">{{ validationResult.error }}</span>
            </div>

            <!-- Error -->
            <div v-if="error" class="px-3 py-2 rounded-md bg-red-50 dark:bg-red-500/10 border border-red-200 dark:border-red-500/30">
              <span class="text-xs text-red-600 dark:text-red-400">{{ error }}</span>
            </div>

            <button
              :disabled="loading || !apiKey.trim()"
              class="w-full px-4 py-2.5 bg-emerald-500 hover:bg-emerald-600 disabled:opacity-50 disabled:cursor-not-allowed text-white rounded-lg text-sm font-medium transition-colors cursor-pointer shadow-sm"
              @click="submitSetup"
            >
              {{ loading ? 'Setting up...' : 'Complete Setup' }}
            </button>
          </div>
        </div>
      </div>

      <!-- Footer hint -->
      <p v-if="step !== 'done'" class="text-center text-[10px] text-zinc-400 dark:text-zinc-600 mt-4">
        Configuration saved to <span class="font-mono">~/.jcode/config.json</span>
      </p>
    </div>
  </div>
</template>
