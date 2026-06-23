<script setup lang="ts">
import { ref, reactive, watch, computed } from 'vue'
import { XMarkIcon } from '@heroicons/vue/24/outline'
import { useAutomationStore } from '@/stores/automation'
import { useChatStore } from '@/stores/chat'
import type { AutomationItem, AutomationCreate, AutomationCadence } from '@/types/automation'

const props = defineProps<{ open: boolean; editing: AutomationItem | null; prefill?: Partial<AutomationCreate> | null }>()
const emit = defineEmits<{ (e: 'close'): void; (e: 'saved'): void }>()

const store = useAutomationStore()
const chat = useChatStore()

type FormTrigger = 'manual' | 'hourly' | 'daily' | 'weekly'
const weekdays = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat']

const form = reactive({
  name: '',
  trigger: 'daily' as FormTrigger,
  hour: 9,
  minute: 0,
  weekday: 1,
  projectPath: '',
  mode: 'full_access',
  prompt: '',
  enabled: true,
})
const saving = ref(false)
const localError = ref<string | null>(null)

const isSchedule = computed(() => form.trigger !== 'manual')
const showHour = computed(() => form.trigger === 'daily' || form.trigger === 'weekly')
const showWeekday = computed(() => form.trigger === 'weekly')

watch(
  () => props.open,
  (open) => {
    if (!open) return
    localError.value = null
    const e = props.editing
    if (e) {
      form.name = e.name
      form.trigger = e.trigger.type === 'manual' ? 'manual' : (e.trigger.cadence as FormTrigger)
      form.hour = e.trigger.hour ?? 9
      form.minute = e.trigger.minute ?? 0
      form.weekday = e.trigger.weekday ?? 1
      form.projectPath = e.project_path
      form.mode = e.mode || 'full_access'
      form.prompt = e.prompt
      form.enabled = e.enabled // preserve paused state when editing
    } else {
      form.name = ''
      form.trigger = 'daily'
      form.hour = 9
      form.minute = 0
      form.weekday = 1
      form.projectPath = props.prefill?.project_path || chat.pwd || ''
      form.mode = 'full_access'
      form.prompt = ''
      form.enabled = true
      if (props.prefill) Object.assign(form, normalizePrefill(props.prefill))
    }
  },
)

function normalizePrefill(p: Partial<AutomationCreate>) {
  const out: Partial<typeof form> = {}
  if (p.name) out.name = p.name
  if (p.prompt) out.prompt = p.prompt
  if (p.mode) out.mode = p.mode
  if (p.trigger) {
    out.trigger = (p.trigger.type === 'manual' ? 'manual' : p.trigger.cadence) as FormTrigger
    if (p.trigger.hour != null) out.hour = p.trigger.hour
    if (p.trigger.minute != null) out.minute = p.trigger.minute
    if (p.trigger.weekday != null) out.weekday = p.trigger.weekday
  }
  return out
}

function buildPayload(runNow: boolean): AutomationCreate {
  const trigger =
    form.trigger === 'manual'
      ? { type: 'manual' as const }
      : {
          type: 'schedule' as const,
          cadence: form.trigger as AutomationCadence,
          hour: form.hour,
          minute: form.minute,
          weekday: form.weekday,
        }
  // A scheduled automation runs unattended, so it must auto-approve.
  const mode = form.trigger === 'manual' ? form.mode : 'full_access'
  return {
    name: form.name.trim(),
    prompt: form.prompt.trim(),
    trigger,
    project_path: form.projectPath.trim(),
    mode,
    enabled: form.enabled,
    run_now: runNow,
  }
}

async function save(runNow = false) {
  localError.value = null
  if (!form.name.trim()) { localError.value = 'Name is required.'; return }
  if (!form.prompt.trim()) { localError.value = 'Prompt is required.'; return }
  if (!form.projectPath.trim()) { localError.value = 'A project is required (no-project automations cannot run unattended).'; return }
  saving.value = true
  try {
    const payload = buildPayload(runNow)
    const ok = props.editing
      ? await store.update(props.editing.id, payload)
      : !!(await store.create(payload))
    if (ok) {
      emit('saved')
      emit('close')
    } else {
      localError.value = store.error || 'Failed to save automation.'
    }
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <transition
    enter-active-class="transition-opacity duration-150"
    enter-from-class="opacity-0"
    leave-active-class="transition-opacity duration-100"
    leave-to-class="opacity-0"
  >
    <div v-if="open" class="auto-modal-overlay" @click.self="emit('close')">
      <div class="auto-modal" role="dialog" aria-modal="true">
        <header class="auto-modal-head">
          <h2>{{ editing ? 'Edit automation' : 'New automation' }}</h2>
          <button class="icon-btn" aria-label="Close" @click="emit('close')"><XMarkIcon class="w-5 h-5" /></button>
        </header>

        <div class="auto-modal-body">
          <label class="field">
            <span class="field-label">Name</span>
            <input v-model="form.name" class="field-input" placeholder="e.g., Daily code review" />
          </label>

          <div class="field-row">
            <label class="field">
              <span class="field-label">Trigger</span>
              <select v-model="form.trigger" class="field-input">
                <option value="daily">Daily</option>
                <option value="weekly">Weekly</option>
                <option value="hourly">Hourly</option>
                <option value="manual">Manual</option>
              </select>
            </label>
            <label v-if="showWeekday" class="field">
              <span class="field-label">Day</span>
              <select v-model.number="form.weekday" class="field-input">
                <option v-for="(d, i) in weekdays" :key="i" :value="i">{{ d }}</option>
              </select>
            </label>
            <label v-if="showHour" class="field">
              <span class="field-label">Hour</span>
              <select v-model.number="form.hour" class="field-input">
                <option v-for="h in 24" :key="h - 1" :value="h - 1">{{ String(h - 1).padStart(2, '0') }}:00</option>
              </select>
            </label>
            <label v-if="isSchedule" class="field">
              <span class="field-label">Minute</span>
              <select v-model.number="form.minute" class="field-input">
                <option v-for="m in 60" :key="m - 1" :value="m - 1">:{{ String(m - 1).padStart(2, '0') }}</option>
              </select>
            </label>
          </div>

          <div class="cloud-row">
            <div>
              <div class="field-label">Run in the cloud</div>
              <div class="cloud-sub">Coming soon — runs even when your computer is off.</div>
            </div>
            <div class="cloud-toggle" title="Coming soon" aria-disabled="true"><span class="cloud-knob" /></div>
          </div>

          <label class="field">
            <span class="field-label">Project</span>
            <input v-model="form.projectPath" class="field-input mono" placeholder="/absolute/path/to/project" />
            <span class="field-hint">Required — scheduled automations run unattended in this project.</span>
          </label>

          <label v-if="!isSchedule" class="field">
            <span class="field-label">Mode</span>
            <select v-model="form.mode" class="field-input">
              <option value="full_access">Autopilot</option>
              <option value="approval">Ask</option>
              <option value="plan">Plan</option>
            </select>
          </label>
          <p v-else class="mode-note">Scheduled runs use <strong>Autopilot</strong> (unattended; approvals would block).</p>

          <label class="field">
            <span class="field-label">Prompt</span>
            <textarea v-model="form.prompt" class="field-input prompt" rows="4" placeholder="Describe what this automation should do…" />
          </label>

          <p v-if="localError" class="form-error">{{ localError }}</p>
        </div>

        <footer class="auto-modal-foot">
          <span class="foot-hint">Use “Create and run” to test your prompt right away.</span>
          <div class="foot-actions">
            <button class="btn-ghost" @click="emit('close')">Cancel</button>
            <button v-if="!editing" class="btn-ghost" :disabled="saving" @click="save(true)">Create and run</button>
            <button class="btn-primary" :disabled="saving" @click="save(false)">{{ editing ? 'Save' : 'Create' }}</button>
          </div>
        </footer>
      </div>
    </div>
  </transition>
</template>

<style scoped>
.auto-modal-overlay {
  position: fixed;
  inset: 0;
  z-index: var(--z-modal, 50);
  display: grid;
  place-items: center;
  padding: 24px;
  background: color-mix(in srgb, var(--color-background) 55%, transparent);
  backdrop-filter: blur(3px);
}
.auto-modal {
  width: min(560px, 100%);
  max-height: 88vh;
  display: flex;
  flex-direction: column;
  background: var(--color-surface);
  border: 1px solid var(--color-border);
  border-radius: var(--radius-2xl, 16px);
  box-shadow: var(--shadow-lg);
  overflow: hidden;
}
.auto-modal-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 18px 20px 12px;
}
.auto-modal-head h2 {
  font-size: 16px;
  font-weight: 600;
  color: var(--color-foreground);
}
.auto-modal-body {
  padding: 4px 20px 16px;
  overflow-y: auto;
  display: flex;
  flex-direction: column;
  gap: 14px;
}
.field { display: flex; flex-direction: column; gap: 6px; flex: 1; min-width: 0; }
.field-row { display: flex; gap: 10px; flex-wrap: wrap; }
.field-label { font-size: 12px; font-weight: 600; color: var(--color-foreground); }
.field-hint { font-size: 11px; color: var(--color-muted-foreground); }
.field-input {
  width: 100%;
  padding: 8px 10px;
  font-size: 13px;
  color: var(--color-foreground);
  background: var(--color-background);
  border: 1px solid var(--color-border);
  border-radius: var(--radius-lg, 10px);
  outline: none;
}
.field-input:focus { border-color: var(--color-primary); }
.field-input.mono { font-family: var(--font-mono); font-size: 12px; }
.field-input.prompt { resize: vertical; font-family: var(--font-sans); line-height: 1.5; }
.cloud-row {
  display: flex; align-items: center; justify-content: space-between;
  padding: 10px 12px; border: 1px dashed var(--color-border); border-radius: var(--radius-lg, 10px);
}
.cloud-sub { font-size: 11px; color: var(--color-muted-foreground); margin-top: 2px; }
.cloud-toggle {
  width: 38px; height: 22px; border-radius: 999px; background: var(--color-muted);
  position: relative; opacity: 0.55; cursor: not-allowed; flex-shrink: 0;
}
.cloud-knob { position: absolute; top: 3px; left: 3px; width: 16px; height: 16px; border-radius: 50%; background: var(--color-surface); }
.mode-note { font-size: 12px; color: var(--color-muted-foreground); }
.form-error { font-size: 12px; color: var(--color-danger, #dc2626); }
.auto-modal-foot {
  display: flex; align-items: center; justify-content: space-between; gap: 12px;
  padding: 12px 20px; border-top: 1px solid var(--color-border); background: var(--color-muted);
}
.foot-hint { font-size: 11px; color: var(--color-muted-foreground); }
.foot-actions { display: flex; gap: 8px; }
.icon-btn { color: var(--color-muted-foreground); padding: 4px; border-radius: 8px; cursor: pointer; }
.icon-btn:hover { background: var(--color-muted); }
.btn-ghost {
  padding: 7px 14px; font-size: 13px; border-radius: var(--radius-lg, 10px);
  border: 1px solid var(--color-border); background: var(--color-surface); color: var(--color-foreground); cursor: pointer;
}
.btn-ghost:hover:not(:disabled) { background: var(--color-muted); }
.btn-primary {
  padding: 7px 16px; font-size: 13px; font-weight: 500; border-radius: var(--radius-lg, 10px);
  border: none; background: var(--color-primary); color: #fff; cursor: pointer;
}
.btn-primary:disabled, .btn-ghost:disabled { opacity: 0.55; cursor: not-allowed; }
</style>
