<script setup lang="ts">
// ModelEditDialog — a modal for adding or editing a single custom model on a
// provider. Separated from ProviderEditDialog (which only covers the connection)
// so model authoring has its own focused window.
//
// In add mode (model = null) all fields start blank; in edit mode they're seeded
// from the model being edited. On save the dialog emits 'save' with the model
// fields; the parent persists it by rebuilding the provider's custom_models.
import { ref, watch, computed, nextTick } from 'vue'
import { XMarkIcon } from '@heroicons/vue/24/outline'
import { useI18n } from 'vue-i18n'
import type { CustomModelDetail } from '@/types/api'

const props = defineProps<{
  open: boolean
  providerId: string
  // null = add mode; a model = edit mode (seeded from it).
  model: CustomModelDetail | null
  // Validation error set by the parent (e.g. duplicate-id conflict against an
  // existing or built-in model). Shown in place of the local required-field error.
  serverError?: string
}>()

const emit = defineEmits<{
  (e: 'close'): void
  (e: 'save', model: { id: string; name?: string; reasoning: boolean; context: number; attachment: boolean; effortTiers: string[]; isEdit: boolean; originalId?: string }): void
  // Clear a parent-set server error when the user edits the conflicting field.
  (e: 'clear-error'): void
}>()

const { t } = useI18n()

interface Draft {
  id: string
  name: string
  reasoning: boolean
  context: number
  attachment: boolean
  effortTiers: string[]
}

const draft = ref<Draft>({ id: '', name: '', reasoning: false, context: 0, attachment: false, effortTiers: [] })
const error = ref('')
const saving = ref(false)

// Inline tier-add input state.
const tierInputOpen = ref(false)
const tierInputValue = ref('')
const tierInputRef = ref<HTMLInputElement | null>(null)

const DEFAULT_TIERS = ['minimal', 'low', 'medium', 'high']
const isEdit = computed(() => !!props.model)

watch(() => props.open, (isOpen) => {
  if (!isOpen) return
  error.value = ''
  if (props.model) {
    draft.value = {
      id: props.model.id,
      name: props.model.name ?? '',
      reasoning: !!props.model.reasoning,
      context: props.model.context ?? 0,
      attachment: !!props.model.attachment,
      effortTiers: props.model.effort_tiers ? [...props.model.effort_tiers] : [],
    }
  } else {
    draft.value = { id: '', name: '', reasoning: false, context: 0, attachment: false, effortTiers: [] }
  }
})

function onReasoningToggle() {
  if (draft.value.reasoning && draft.value.effortTiers.length === 0) {
    draft.value.effortTiers = [...DEFAULT_TIERS]
  }
  if (!draft.value.reasoning) {
    draft.value.effortTiers = []
  }
}

function removeTier(tier: string) {
  const i = draft.value.effortTiers.indexOf(tier)
  if (i >= 0) draft.value.effortTiers.splice(i, 1)
}

async function openTierInput() {
  tierInputOpen.value = true
  tierInputValue.value = ''
  await nextTick()
  tierInputRef.value?.focus()
}

function commitTier() {
  const v = tierInputValue.value.trim()
  if (v && !draft.value.effortTiers.includes(v)) draft.value.effortTiers.push(v)
  tierInputOpen.value = false
  tierInputValue.value = ''
}

function save() {
  const id = draft.value.id.trim()
  if (!id) {
    error.value = t('settings.providers.customIdRequired')
    return
  }
  saving.value = true
  emit('save', {
    id,
    name: draft.value.name.trim() || undefined,
    reasoning: draft.value.reasoning,
    context: draft.value.context || 0,
    attachment: draft.value.attachment,
    effortTiers: draft.value.reasoning ? [...draft.value.effortTiers] : [],
    isEdit: isEdit.value,
    originalId: isEdit.value ? props.model?.id : undefined,
  })
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
    <div v-if="open" class="mdl-overlay" @click.self="emit('close')">
      <div class="mdl" role="dialog" aria-modal="true">
        <header class="mdl-head">
          <h2>{{ isEdit ? t('settings.providers.editModel') : t('settings.providers.addModel') }}</h2>
          <button class="icon-btn" :aria-label="t('common.close')" @click="emit('close')"><XMarkIcon class="w-5 h-5" /></button>
        </header>

        <div class="mdl-body">
          <label class="s-field">
            <span class="s-label">Model ID <span style="color: var(--color-destructive)">*</span></span>
            <input v-model="draft.id" type="text" class="s-input mono" :placeholder="t('settings.providers.customModelPlaceholder')" @input="emit('clear-error')" />
          </label>
          <label class="s-field">
            <span class="s-label">{{ t('settings.providers.customModelNamePlaceholder') }}</span>
            <input v-model="draft.name" type="text" class="s-input" />
          </label>
          <label class="s-field">
            <span class="s-label">{{ t('settings.providers.contextWindow') }}</span>
            <input v-model.number="draft.context" type="number" min="0" class="s-input mono" :placeholder="t('settings.providers.contextPlaceholder')" />
          </label>

          <!-- Capability toggles -->
          <div class="mdl-toggle">
            <div>
              <div class="text-[12px] font-medium">{{ t('settings.providers.supportImage') }}</div>
              <div class="text-[10.5px]" style="color: var(--color-muted-foreground)">{{ t('settings.providers.supportImageDesc') }}</div>
            </div>
            <button class="s-switch" :data-on="draft.attachment ? 'true' : 'false'" :aria-pressed="draft.attachment" @click="draft.attachment = !draft.attachment" />
          </div>
          <div class="mdl-toggle">
            <div>
              <div class="text-[12px] font-medium">{{ t('settings.providers.supportReasoning') }}</div>
              <div class="text-[10.5px]" style="color: var(--color-muted-foreground)">{{ t('settings.providers.supportReasoningDesc') }}</div>
            </div>
            <button class="s-switch" :data-on="draft.reasoning ? 'true' : 'false'" :aria-pressed="draft.reasoning" @click="draft.reasoning = !draft.reasoning; onReasoningToggle()" />
          </div>

          <!-- Effort-tier editor -->
          <div v-if="draft.reasoning" class="s-field">
            <span class="s-label">{{ t('settings.providers.effortTiers') }}</span>
            <div class="p-tiers">
              <span v-for="tier in draft.effortTiers" :key="tier" class="p-tier">
                {{ tier }}
                <button class="p-tier-x" :aria-label="t('common.remove')" @click="removeTier(tier)"><XMarkIcon class="w-2.5 h-2.5" /></button>
              </span>
              <span v-if="tierInputOpen" class="p-tier-input-wrap">
                <input
                  ref="tierInputRef"
                  v-model="tierInputValue"
                  type="text"
                  class="p-tier-input"
                  :placeholder="t('settings.providers.newTierPlaceholder')"
                  @keydown.enter.prevent="commitTier"
                  @keydown.esc="tierInputOpen = false"
                  @blur="commitTier"
                />
              </span>
              <button v-else class="p-tier-add" @click="openTierInput">+ {{ t('settings.providers.addTier') }}</button>
            </div>
            <span class="s-helper">{{ t('settings.providers.effortTiersHint') }}</span>
          </div>

          <div v-if="serverError || error" class="s-error">{{ serverError || error }}</div>

          <div class="mdl-actions">
            <button class="s-btn s-btn-ghost s-btn-sm" @click="emit('close')">{{ t('common.cancel') }}</button>
            <button class="s-btn s-btn-primary s-btn-sm" :disabled="!draft.id.trim() || saving" @click="save">
              {{ saving ? t('settings.providers.saving') : isEdit ? t('common.save') : t('common.add') }}
            </button>
          </div>
        </div>
      </div>
    </div>
  </transition>
</template>

<style scoped>
.mdl-overlay {
  position: fixed;
  inset: 0;
  z-index: calc(var(--z-modal) + 10);
  background: var(--backdrop);
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 24px;
}
.mdl {
  width: 100%;
  max-width: 440px;
  max-height: 85vh;
  background: var(--color-surface);
  border: 1px solid var(--color-border);
  border-radius: var(--radius-xl);
  box-shadow: var(--shadow-lg);
  display: flex;
  flex-direction: column;
  overflow: hidden;
}
.mdl-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 14px 16px;
  border-bottom: 1px solid var(--color-border);
}
.mdl-head h2 {
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
.icon-btn:hover { background: var(--color-secondary); color: var(--color-foreground); }
.mdl-body {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  padding: 16px;
  display: flex;
  flex-direction: column;
  gap: 12px;
}
.s-field { display: flex; flex-direction: column; }
.s-label { font-size: 11px; font-weight: 500; color: var(--color-foreground); margin-bottom: 5px; }
.s-helper { font-size: 11px; line-height: 1.45; color: var(--color-muted-foreground); margin-top: 5px; }
.s-error { font-size: 11px; line-height: 1.45; color: var(--color-destructive); }
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
.mdl-toggle { display: flex; align-items: center; justify-content: space-between; gap: 10px; padding: 4px 0; }
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
}
.s-btn:disabled { opacity: 0.5; cursor: not-allowed; }
.s-btn-primary { background: var(--color-foreground); color: var(--color-surface); }
.s-btn-primary:hover:not(:disabled) { background: color-mix(in srgb, var(--color-foreground) 85%, transparent); }
.s-btn-ghost { background: transparent; color: var(--color-foreground); }
.s-btn-ghost:hover:not(:disabled) { background: var(--color-secondary); }
.s-btn-sm { height: 28px; padding: 0 10px; font-size: 11.5px; border-radius: var(--radius-sm); }
.s-switch {
  position: relative;
  display: inline-block;
  width: 34px;
  height: 20px;
  flex-shrink: 0;
  background: var(--color-border);
  border-radius: var(--radius-pill);
  border: none;
  cursor: pointer;
  padding: 0;
  transition: background var(--duration-fast);
}
.s-switch[data-on='true'] { background: var(--color-accent-neutral); }
.s-switch::after {
  content: '';
  position: absolute;
  top: 2px;
  left: 2px;
  width: 16px;
  height: 16px;
  border-radius: 50%;
  background: var(--color-surface);
  box-shadow: var(--shadow-sm);
  transition: transform var(--duration-fast) var(--ease-out);
}
.s-switch[data-on='true']::after { transform: translateX(14px); }

/* Tier editor */
.p-tiers { display: flex; flex-wrap: wrap; gap: 6px; align-items: center; }
.p-tier {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  height: 24px;
  padding: 0 4px 0 9px;
  background: var(--color-surface);
  border: 1px solid var(--color-border);
  border-radius: var(--radius-pill);
  font-size: 11px;
  font-family: var(--font-mono);
  color: var(--color-foreground);
}
.p-tier-x {
  width: 16px;
  height: 16px;
  display: grid;
  place-items: center;
  border: none;
  background: none;
  border-radius: 50%;
  color: var(--color-muted-foreground);
  cursor: pointer;
}
.p-tier-x:hover { background: var(--color-error-bg); color: var(--color-error-fg); }
.p-tier-add {
  height: 24px;
  padding: 0 10px;
  background: transparent;
  border: 1px dashed var(--color-border);
  border-radius: var(--radius-pill);
  font-size: 11px;
  color: var(--color-muted-foreground);
  cursor: pointer;
}
.p-tier-add:hover { border-color: var(--color-border-active); color: var(--color-foreground); }
.p-tier-input-wrap { display: inline-flex; align-items: center; }
.p-tier-input {
  height: 24px;
  padding: 0 10px;
  background: var(--color-surface);
  border: 1px solid var(--color-border-active);
  border-radius: var(--radius-pill);
  font-size: 11px;
  font-family: var(--font-mono);
  color: var(--color-foreground);
  outline: none;
  width: 96px;
}
.p-tier-input:focus { box-shadow: 0 0 0 3px var(--accent-wash-soft); }

.mdl-actions { display: flex; align-items: center; justify-content: flex-end; gap: 8px; margin-top: 4px; }
</style>
