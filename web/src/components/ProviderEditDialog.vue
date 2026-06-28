<script setup lang="ts">
// ProviderEditDialog — a modal for a provider's CONNECTION fields only
// (display name, endpoint, API key, headers). Model management lives on the
// provider card in SettingsDialog, so this dialog never edits the model list.
//
// Two modes share one dialog:
//  - edit: editingProvider is set → connection fields for an existing provider
//  - add:  editingProvider is null → the add flow (select type → credentials).
//          A new custom provider is created connection-only; its models are
//          added afterward from the card.
//
// The dialog owns its own form state, seeded from the provider on open, and
// emits 'saved' after a successful save so the parent can refresh its list.
import { ref, watch, computed } from 'vue'
import { XMarkIcon, ChevronRightIcon, ArrowPathIcon } from '@heroicons/vue/24/outline'
import { useI18n } from 'vue-i18n'
import { api } from '@/composables/api'
import ProviderIcon from '@/components/ProviderIcon.vue'
import type { ProviderDetail, SetupProvider, ValidateResult } from '@/types/api'

const props = defineProps<{
  open: boolean
  editingProvider: ProviderDetail | null
  // The full registry provider list (for the add-flow "select type" step).
  setupProviders: SetupProvider[]
  // Already-configured provider ids, to hide them from the add-flow select step.
  configuredIds: string[]
}>()

const emit = defineEmits<{
  (e: 'close'): void
  (e: 'saved'): void
}>()

const { t } = useI18n()

// ── Form state ──
const step = ref<'select' | 'credentials'>('select')
const selectedProvider = ref('')
const isCustom = ref(false)
const customId = ref('')
const customName = ref('')
const apiKey = ref('')
const baseURL = ref('')
const headers = ref<{ key: string; value: string; ph?: string }[]>([])
const advancedOpen = ref(false)
const saving = ref(false)
const error = ref('')
// Test-connection state.
const testing = ref(false)
const testResult = ref<ValidateResult | null>(null)

const editingId = computed(() => props.editingProvider?.id ?? '')

// ── Seed form state when the dialog opens ──
watch(() => props.open, (isOpen) => {
  if (!isOpen) return
  error.value = ''
  testResult.value = null
  testing.value = false
  if (props.editingProvider) {
    step.value = 'credentials'
    selectedProvider.value = props.editingProvider.id
    isCustom.value = !!props.editingProvider.custom
    customId.value = props.editingProvider.id
    customName.value = props.editingProvider.name || ''
    apiKey.value = '' // masked server-side; blank ⇒ keep
    baseURL.value = props.editingProvider.base_url || ''
    headers.value = Object.entries(props.editingProvider.headers ?? {}).map(([key, value]) => ({ key, value: '', ph: value }))
    advancedOpen.value = headers.value.length > 0 || !!props.editingProvider.base_url
  } else {
    resetAddForm()
    step.value = 'select'
  }
})

function resetAddForm() {
  selectedProvider.value = ''
  isCustom.value = false
  customId.value = ''
  customName.value = ''
  apiKey.value = ''
  baseURL.value = ''
  headers.value = []
  advancedOpen.value = false
}

// ── Add-flow step 1: select a connection type ──
function selectProvider(id: string) {
  selectedProvider.value = id
  isCustom.value = false
  step.value = 'credentials'
}

function selectCustom() {
  selectedProvider.value = ''
  isCustom.value = true
  baseURL.value = ''
  advancedOpen.value = true
  step.value = 'credentials'
}

const selectableProviders = computed(() =>
  props.setupProviders.filter((p) => !props.configuredIds.includes(p.id)),
)

function addHeaderRow() {
  headers.value.push({ key: '', value: '' })
}

function removeHeaderRow(i: number) {
  headers.value.splice(i, 1)
}

// Build the headers payload: drop blank keys, keep blank values (server preserves secrets).
function headersPayload(): Record<string, string> | undefined {
  const out: Record<string, string> = {}
  for (const h of headers.value) {
    const k = h.key.trim()
    if (k) out[k] = h.value
  }
  return Object.keys(out).length ? out : undefined
}

// ── Test connection ──
// In edit mode the stored key is masked/blank; to test we send the entered key
// (if any) plus the provider id so the server resolves the base URL. When the
// key is blank in edit mode we can't really test (the server would reject an
// empty key), so we surface that as a hint rather than a failing request.
async function testConnection() {
  if (editingId.value && !apiKey.value) {
    testResult.value = { valid: false, error_type: 'server', error: t('settings.providers.testEnterKey') }
    return
  }
  testing.value = true
  testResult.value = null
  try {
    testResult.value = await api.setupValidate({
      provider: editingId.value || selectedProvider.value || 'openai-compatible',
      api_key: apiKey.value,
      base_url: baseURL.value || undefined,
      headers: headersPayload(),
    })
  } catch (err: unknown) {
    testResult.value = { valid: false, error_type: 'server', error: err instanceof Error ? err.message : 'failed' }
  }
  testing.value = false
}

// Any edit to credentials invalidates a prior test result.
watch([apiKey, baseURL, headers], () => { testResult.value = null }, { deep: true })

// ── Save ──
async function save() {
  error.value = ''
  // Custom-provider required fields (add mode only).
  if (!editingId.value && isCustom.value) {
    if (!customId.value.trim()) { error.value = t('settings.providers.customIdRequired'); return }
    if (!baseURL.value.trim()) { error.value = t('settings.providers.customUrlRequired'); return }
  }
  saving.value = true
  try {
    const advanced = { base_url: baseURL.value || undefined, headers: headersPayload() }
    if (editingId.value) {
      // Connection-only edit: models are managed on the provider card, so we
      // omit custom_models here (the server keeps the existing list when the
      // field is absent).
      await api.updateProvider(editingId.value, {
        api_key: apiKey.value || undefined,
        name: isCustom.value ? (customName.value || undefined) : undefined,
        ...advanced,
      })
    } else if (isCustom.value) {
      // A brand-new custom provider is created connection-only; its models are
      // added afterward from the card.
      await api.addProvider({
        id: customId.value.trim(),
        api_key: apiKey.value,
        name: customName.value.trim() || undefined,
        ...advanced,
      })
    } else {
      await api.addProvider({
        id: selectedProvider.value,
        api_key: apiKey.value,
        ...advanced,
      })
    }
    emit('saved')
    emit('close')
  } catch (err: unknown) {
    error.value = err instanceof Error ? err.message : 'Failed to save provider'
  }
  saving.value = false
}
</script>

<template>
  <transition
    enter-active-class="transition-opacity duration-150"
    enter-from-class="opacity-0"
    leave-active-class="transition-opacity duration-100"
    leave-to-class="opacity-0"
  >
    <div v-if="open" class="prov-modal-overlay" @click.self="emit('close')">
      <div class="prov-modal" role="dialog" aria-modal="true">
        <header class="prov-modal-head">
          <h2>{{ editingProvider ? t('settings.providers.editProvider') : step === 'select' ? t('settings.providers.selectProvider') : t('settings.providers.enterApiKey') }}</h2>
          <button class="icon-btn" aria-label="Close" @click="emit('close')"><XMarkIcon class="w-5 h-5" /></button>
        </header>

        <div class="prov-modal-body">
          <!-- Step 1: select connection type -->
          <div v-if="step === 'select' && !editingProvider" class="prov-step-select">
            <div v-if="setupProviders.length === 0" class="prov-empty-hint">{{ t('settings.providers.loadingHint') }}</div>
            <div v-else-if="selectableProviders.length === 0" class="prov-empty-hint">{{ t('settings.providers.allConfigured') }}</div>
            <div v-else class="prov-type-list">
              <button
                v-for="p in selectableProviders"
                :key="p.id"
                class="prov-type-row"
                @click="selectProvider(p.id)"
              >
                <span class="prov-type-icon"><ProviderIcon :provider="p.id" :size="18" /></span>
                <span class="prov-type-name">{{ p.name }}</span>
                <span class="prov-type-id">{{ p.id }}</span>
                <ChevronRightIcon class="w-4 h-4 prov-type-chev" />
              </button>
              <button class="prov-type-row" @click="selectCustom">
                <span class="prov-type-icon"><ProviderIcon provider="openai" :custom="true" :size="18" /></span>
                <span class="prov-type-name">{{ t('settings.providers.customProvider') }}</span>
                <span class="prov-type-desc">{{ t('settings.providers.customProviderDesc') }}</span>
                <ChevronRightIcon class="w-4 h-4 prov-type-chev" />
              </button>
            </div>
          </div>

          <!-- Step 2: connection credentials -->
          <div v-else class="prov-step-cred">
            <!-- Breadcrumb back to type select (add mode only) -->
            <div v-if="!editingProvider" class="prov-crumb">
              <button class="s-btn s-btn-ghost s-btn-xs" @click="step = 'select'">‹</button>
              <span class="prov-crumb-id">{{ isCustom ? (customName || customId || t('settings.providers.customProvider')) : selectedProvider }}</span>
            </div>

            <!-- Custom-provider identity (add mode) -->
            <template v-if="isCustom && !editingProvider">
              <label class="s-field">
                <span class="s-label">{{ t('settings.providers.customId') }}</span>
                <input v-model="customId" type="text" class="s-input mono" :placeholder="t('settings.providers.customIdPlaceholder')" />
              </label>
              <label class="s-field">
                <span class="s-label">{{ t('settings.providers.customName') }}</span>
                <input v-model="customName" type="text" class="s-input" :placeholder="t('settings.providers.customNamePlaceholder')" />
              </label>
            </template>

            <label class="s-field">
              <span class="s-label">{{ t('settings.providers.apiKey') }}</span>
              <input v-model="apiKey" type="password" class="s-input mono" :placeholder="editingProvider ? t('settings.providers.apiKeyUnchanged') : t('settings.providers.apiKey')" @keydown.enter="save" />
            </label>

            <!-- Advanced: endpoint + headers -->
            <button class="s-adv-toggle" :aria-expanded="advancedOpen" @click="advancedOpen = !advancedOpen">
              <ChevronRightIcon class="w-3 h-3" :style="{ transform: advancedOpen ? 'rotate(90deg)' : 'none' }" />
              {{ t('settings.providers.advanced') }}
            </button>
            <div v-if="advancedOpen" class="prov-advanced">
              <label class="s-field">
                <span class="s-label">{{ t('settings.providers.endpoint') }}</span>
                <input v-model="baseURL" type="text" class="s-input mono" :placeholder="t('settings.providers.endpointPlaceholder')" />
              </label>
              <div class="s-field">
                <div class="s-field-head">
                  <span class="s-label">{{ t('settings.providers.headers') }}</span>
                  <button class="s-btn s-btn-ghost s-btn-xs" @click="addHeaderRow">+ {{ t('settings.providers.addHeader') }}</button>
                </div>
                <div v-for="(h, i) in headers" :key="i" class="s-kv">
                  <input v-model="h.key" type="text" class="s-input mono" :placeholder="t('settings.providers.headerKey')" />
                  <input v-model="h.value" type="text" class="s-input mono" :placeholder="h.ph || t('settings.providers.headerValue')" />
                  <button class="s-kv-rm" @click="removeHeaderRow(i)"><XMarkIcon class="w-3 h-3" /></button>
                </div>
                <div v-if="headers.length" class="s-helper">{{ t('settings.providers.headersHint') }}</div>
              </div>
            </div>

            <!-- Test-connection result -->
            <div v-if="error" class="s-error">{{ error }}</div>
            <div v-if="testResult" class="p-test-result" :class="testResult.valid ? 'p-test-ok' : { 'p-test-auth': testResult.error_type === 'auth', 'p-test-net': testResult.error_type === 'network' }">
              <template v-if="testResult.valid">
                <span class="p-test-ic">✓</span>
                <span>{{ t('settings.providers.testSuccess') }} · {{ t('settings.providers.testLatency') }} {{ testResult.latency_ms }}ms · {{ t('settings.providers.testModels', { n: testResult.model_count ?? 0 }) }}</span>
              </template>
              <template v-else>
                <span class="p-test-ic">✕</span>
                <span>
                  <b>{{ testResult.error_type === 'auth' ? t('settings.providers.testAuthFail') : testResult.error_type === 'network' ? t('settings.providers.testNetworkFail') : t('settings.providers.connectFailed') }}</b>
                  <template v-if="testResult.error"> · {{ testResult.error }}</template>
                </span>
              </template>
            </div>

            <!-- Actions: Test (left) + Save (right, compact, not full-width, not yellow) -->
            <div class="prov-actions">
              <button :disabled="testing || (!apiKey && !editingId)" class="s-btn s-btn-secondary s-btn-sm" @click="testConnection">
                <ArrowPathIcon v-if="testing" class="w-3.5 h-3.5 animate-spin" />
                {{ testing ? t('settings.providers.testing') : t('settings.providers.testConnection') }}
              </button>
              <button :disabled="saving || (!editingId && !apiKey)" class="s-btn s-btn-primary s-btn-sm prov-save" @click="save">
                {{ saving ? t('settings.providers.saving') : editingProvider ? t('common.save') : t('settings.providers.addBtn') }}
              </button>
            </div>
          </div>
        </div>
      </div>
    </div>
  </transition>
</template>

<style scoped>
.prov-modal-overlay {
  position: fixed;
  inset: 0;
  z-index: var(--z-modal);
  background: var(--backdrop);
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 24px;
}
.prov-modal {
  width: 100%;
  max-width: 480px;
  max-height: min(80vh, 640px);
  background: var(--color-surface);
  border: 1px solid var(--color-border);
  border-radius: var(--radius-xl);
  box-shadow: var(--shadow-lg);
  display: flex;
  flex-direction: column;
  overflow: hidden;
}
.prov-modal-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 14px 16px;
  border-bottom: 1px solid var(--color-border);
}
.prov-modal-head h2 {
  font-size: 14px;
  font-weight: 600;
  color: var(--color-foreground);
  margin: 0;
}
.icon-btn {
  width: 30px;
  height: 30px;
  display: grid;
  place-items: center;
  border: none;
  background: transparent;
  border-radius: var(--radius-md);
  color: var(--color-muted-foreground);
  cursor: pointer;
}
.icon-btn:hover {
  background: var(--color-secondary);
  color: var(--color-foreground);
}
.prov-modal-body {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  padding: 16px;
  display: flex;
  flex-direction: column;
  gap: 12px;
}

/* Step 1: type list */
.prov-type-list {
  display: flex;
  flex-direction: column;
  gap: 4px;
}
.prov-type-row {
  display: flex;
  align-items: center;
  gap: 10px;
  width: 100%;
  padding: 9px 11px;
  border: 1px solid var(--color-border);
  border-radius: var(--radius-md);
  background: var(--color-surface);
  color: var(--color-foreground);
  cursor: pointer;
  text-align: left;
  transition: background var(--duration-fast);
}
.prov-type-row:hover {
  background: var(--color-secondary);
}
.prov-type-icon {
  width: 28px;
  height: 28px;
  display: grid;
  place-items: center;
  flex-shrink: 0;
}
.prov-type-icon-fallback {
  font-family: var(--font-mono);
  font-weight: 700;
  color: var(--color-muted-foreground);
}
.prov-type-name {
  font-size: 12.5px;
  font-weight: 600;
}
.prov-type-id {
  font-size: 11px;
  font-family: var(--font-mono);
  color: var(--color-muted-foreground);
  margin-left: 4px;
}
.prov-type-desc {
  font-size: 11px;
  color: var(--color-muted-foreground);
}
.prov-type-chev {
  margin-left: auto;
  color: var(--color-muted-foreground);
  flex-shrink: 0;
}
.prov-empty-hint {
  font-size: 11px;
  color: var(--color-muted-foreground);
  text-align: center;
  padding: 16px 0;
}

/* Step 2: credentials */
.prov-crumb {
  display: flex;
  align-items: center;
  gap: 6px;
}
.prov-crumb-id {
  font-size: 10.5px;
  font-family: var(--font-mono);
  color: var(--color-muted-foreground);
}
.prov-advanced {
  display: flex;
  flex-direction: column;
  gap: 12px;
  padding-left: 2px;
}

/* shared form primitives (mirrors SettingsDialog's .s-* but scoped here) */
.s-field {
  display: flex;
  flex-direction: column;
}
.s-field-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 5px;
}
.s-label {
  font-size: 11px;
  font-weight: 500;
  color: var(--color-foreground);
  margin-bottom: 5px;
}
.s-helper {
  font-size: 11px;
  line-height: 1.45;
  color: var(--color-muted-foreground);
  margin-top: 5px;
}
.s-error {
  font-size: 11px;
  line-height: 1.45;
  color: var(--color-destructive);
  margin-top: 4px;
}
.s-input {
  width: 100%;
  height: 32px;
  padding: 0 10px;
  background: var(--color-surface);
  border: 1px solid var(--color-border);
  border-radius: var(--radius-md);
  color: var(--color-foreground);
  font-size: 12px;
  font-family: var(--font-sans);
  outline: none;
  transition: border-color var(--duration-fast), box-shadow var(--duration-fast);
}
.s-input::placeholder { color: var(--color-muted-foreground); opacity: 0.7; }
.s-input:focus { border-color: var(--color-primary); box-shadow: 0 0 0 3px var(--accent-wash-soft); }
.s-input.mono { font-family: var(--font-mono); }
.s-kv {
  display: flex;
  gap: 8px;
  margin-bottom: 8px;
}
.s-kv-rm {
  width: 28px;
  height: 32px;
  display: grid;
  place-items: center;
  border: none;
  background: transparent;
  color: var(--color-muted-foreground);
  border-radius: var(--radius-md);
  cursor: pointer;
  flex-shrink: 0;
}
.s-kv-rm:hover { background: var(--color-secondary); color: var(--color-foreground); }
.s-adv-toggle {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  font-size: 11px;
  font-weight: 500;
  color: var(--color-muted-foreground);
  background: none;
  border: none;
  cursor: pointer;
  padding: 2px 0;
  align-self: flex-start;
}
.s-adv-toggle:hover { color: var(--color-foreground); }
.s-btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 6px;
  height: 32px;
  padding: 0 12px;
  border-radius: var(--radius-md);
  font-size: 12px;
  font-weight: 500;
  font-family: var(--font-sans);
  cursor: pointer;
  border: 1px solid transparent;
  white-space: nowrap;
  transition: background var(--duration-fast), border-color var(--duration-fast), opacity var(--duration-fast);
}
.s-btn:disabled { opacity: 0.5; cursor: not-allowed; }
.s-btn-primary {
  background: var(--color-foreground);
  color: var(--color-surface);
}
.s-btn-primary:hover:not(:disabled) { background: color-mix(in srgb, var(--color-foreground) 85%, transparent); }
.s-btn-secondary {
  background: var(--color-surface);
  border-color: var(--color-border);
  color: var(--color-foreground);
}
.s-btn-secondary:hover:not(:disabled) { background: var(--color-secondary); }
.s-btn-ghost { background: transparent; color: var(--color-foreground); }
.s-btn-ghost:hover:not(:disabled) { background: var(--color-secondary); }
.s-btn-sm { height: 28px; padding: 0 10px; font-size: 11.5px; border-radius: var(--radius-sm); }
.s-btn-xs { height: 22px; padding: 0 7px; font-size: 10px; border-radius: var(--radius-sm); }

/* Test-connection banner */
.p-test-result {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 8px 11px;
  border-radius: var(--radius-md);
  font-size: 11px;
  line-height: 1.4;
}
.p-test-ic { font-weight: 700; flex-shrink: 0; }
.p-test-ok { background: var(--color-success-bg); color: var(--color-success-fg); }
.p-test-auth { background: var(--color-error-bg); color: var(--color-error-fg); }
.p-test-net { background: var(--color-warning-bg); color: var(--color-warning-fg); }

/* Actions: Test left, Save right — Save is compact (not full-width, not yellow) */
.prov-actions {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 8px;
  margin-top: 4px;
}
.prov-save {
  min-width: 84px;
}
.animate-spin { animation: prov-spin 0.7s linear infinite; }
@keyframes prov-spin { to { transform: rotate(360deg); } }
</style>
