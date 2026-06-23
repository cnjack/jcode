<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import { PlayIcon, XMarkIcon, TrashIcon, PencilSquareIcon, CheckCircleIcon, ExclamationCircleIcon } from '@heroicons/vue/24/outline'
import { useAutomationStore } from '@/stores/automation'
import AutomationEditorDialog from '@/components/AutomationEditorDialog.vue'
import type { AutomationItem, AutomationCreate, AutomationTemplate, AutomationRun } from '@/types/automation'

const props = defineProps<{ open: boolean }>()
const emit = defineEmits<{ (e: 'close'): void }>()

const store = useAutomationStore()

const view = ref<'list' | 'templates'>('list')
const statusFilter = ref<'all' | 'success' | 'failed'>('all')
const search = ref('')
const editorOpen = ref(false)
const editing = ref<AutomationItem | null>(null)
const prefill = ref<Partial<AutomationCreate> | null>(null)

watch(
  () => props.open,
  (open) => {
    if (open) {
      void store.fetchAll()
      void store.fetchTemplates()
      view.value = 'list'
    }
  },
)

const filteredRuns = computed(() => {
  const q = search.value.trim().toLowerCase()
  return store.runs.filter((r) => {
    if (statusFilter.value === 'success' && r.terminal_status !== 'success') return false
    if (statusFilter.value === 'failed' && r.terminal_status !== 'error') return false
    if (q && !r.title.toLowerCase().includes(q)) return false
    return true
  })
})

function newAutomation() {
  editing.value = null
  prefill.value = null
  editorOpen.value = true
}

function editAutomation(item: AutomationItem) {
  editing.value = item
  prefill.value = null
  editorOpen.value = true
}

function fromTemplate(t: AutomationTemplate) {
  editing.value = null
  prefill.value = { name: t.name, prompt: t.prompt, trigger: t.trigger, mode: t.suggest_mode }
  view.value = 'list'
  editorOpen.value = true
}

function runLabel(r: AutomationRun): string {
  const time = r.start_time ? new Date(r.start_time).toLocaleString() : ''
  return time
}

function isRunning(item: AutomationItem) {
  return item.state.last_status === 'running'
}
</script>

<template>
  <transition
    enter-active-class="transition-opacity duration-150"
    enter-from-class="opacity-0"
    leave-active-class="transition-opacity duration-100"
    leave-to-class="opacity-0"
  >
    <div v-if="open" class="auto-shell">
      <header class="auto-top">
        <div class="auto-top-left">
          <h1>Automations</h1>
          <div class="seg">
            <button :class="['seg-btn', { on: view === 'list' }]" @click="view = 'list'">Your automations</button>
            <button :class="['seg-btn', { on: view === 'templates' }]" @click="view = 'templates'">Templates</button>
          </div>
        </div>
        <div class="auto-top-right">
          <button class="btn-primary" @click="newAutomation">New automation</button>
          <button class="icon-btn" aria-label="Close" @click="emit('close')"><XMarkIcon class="w-5 h-5" /></button>
        </div>
      </header>

      <div class="auto-scroll">
        <!-- ── Your automations ── -->
        <template v-if="view === 'list'">
          <p class="section-sub">Use agents to handle recurring work on a cadence you choose.</p>

          <div v-if="store.loading && !store.items.length" class="empty">Loading…</div>
          <div v-else-if="!store.items.length" class="empty">
            No automations yet. Click <strong>New automation</strong> or pick a
            <button class="link" @click="view = 'templates'">template</button>.
          </div>

          <div v-else class="cards">
            <div v-for="a in store.items" :key="a.id" class="card" :class="{ disabled: !a.enabled }">
              <div class="card-head">
                <span class="card-name">{{ a.name }}</span>
                <span class="badge">{{ a.badge }}</span>
              </div>
              <p class="card-prompt">{{ a.prompt }}</p>
              <div class="card-foot">
                <span class="card-meta">
                  <CheckCircleIcon v-if="a.state.last_status === 'success'" class="w-3.5 h-3.5 ok" />
                  <ExclamationCircleIcon v-else-if="a.state.last_status === 'error'" class="w-3.5 h-3.5 err" />
                  {{ a.human_schedule }}<template v-if="!a.enabled"> · paused</template>
                </span>
                <div class="card-actions">
                  <button class="icon-btn sm" title="Edit" @click="editAutomation(a)"><PencilSquareIcon class="w-4 h-4" /></button>
                  <button class="icon-btn sm" title="Delete" @click="store.remove(a.id)"><TrashIcon class="w-4 h-4" /></button>
                  <label class="switch" :title="a.enabled ? 'Enabled' : 'Disabled'">
                    <input type="checkbox" :checked="a.enabled" @change="store.setEnabled(a, ($event.target as HTMLInputElement).checked)" />
                    <span class="switch-track"><span class="switch-knob" /></span>
                  </label>
                  <button class="run-btn" :disabled="isRunning(a)" title="Run now" @click="store.runNow(a.id)">
                    <PlayIcon class="w-4 h-4" />
                  </button>
                </div>
              </div>
            </div>
          </div>

          <!-- ── Recent runs ── -->
          <div class="runs-head">
            <h2>Recent runs</h2>
            <div class="runs-tools">
              <select v-model="statusFilter" class="status-filter">
                <option value="all">All</option>
                <option value="success">Success</option>
                <option value="failed">Failed</option>
              </select>
              <input v-model="search" class="run-search" :placeholder="`Search ${store.runs.length} runs…`" />
            </div>
          </div>
          <div v-if="!filteredRuns.length" class="empty sm">No runs yet.</div>
          <div v-else class="runs">
            <div v-for="r in filteredRuns" :key="r.session_id" class="run-row">
              <div class="run-main">
                <span class="run-title">{{ r.title || 'Automation run' }}</span>
                <span class="run-time">
                  <CheckCircleIcon v-if="r.terminal_status === 'success'" class="w-3.5 h-3.5 ok" />
                  <ExclamationCircleIcon v-else-if="r.terminal_status === 'error'" class="w-3.5 h-3.5 err" />
                  <span v-else class="dot" />
                  {{ runLabel(r) }} · {{ r.trigger_kind }}
                </span>
              </div>
              <span v-if="r.error_reason" class="run-err">{{ r.error_reason }}</span>
            </div>
          </div>
        </template>

        <!-- ── Templates ── -->
        <template v-else>
          <p class="section-sub">Start from a template — pick a project and confirm.</p>
          <div class="tpl-grid">
            <button v-for="t in store.templates" :key="t.id" class="tpl-card" @click="fromTemplate(t)">
              <div class="card-head">
                <span class="card-name">{{ t.name }}</span>
                <span class="badge">{{ t.badge }}</span>
              </div>
              <p class="card-prompt">{{ t.description }}</p>
            </button>
          </div>
        </template>
      </div>

      <AutomationEditorDialog
        :open="editorOpen"
        :editing="editing"
        :prefill="prefill"
        @close="editorOpen = false"
        @saved="store.fetchAll()"
      />
    </div>
  </transition>
</template>

<style scoped>
.auto-shell {
  position: fixed;
  inset: 0;
  z-index: var(--z-modal, 50);
  display: flex;
  flex-direction: column;
  background: var(--color-background);
  color: var(--color-foreground);
}
.auto-top {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 18px 28px 14px;
  padding-top: max(18px, env(safe-area-inset-top));
}
.auto-top-left { display: flex; align-items: center; gap: 18px; }
.auto-top h1 { font-size: 19px; font-weight: 600; }
.auto-top-right { display: flex; align-items: center; gap: 10px; }
.seg { display: flex; gap: 2px; background: var(--color-muted); padding: 2px; border-radius: 999px; }
.seg-btn {
  padding: 5px 12px; font-size: 12.5px; border-radius: 999px; color: var(--color-muted-foreground); cursor: pointer;
}
.seg-btn.on { background: var(--color-surface); color: var(--color-foreground); box-shadow: var(--shadow-sm); }
.auto-scroll { flex: 1; overflow-y: auto; padding: 6px 28px 40px; max-width: 1100px; width: 100%; margin: 0 auto; }
.section-sub { font-size: 13px; color: var(--color-muted-foreground); margin-bottom: 18px; }
.empty { padding: 28px 0; color: var(--color-muted-foreground); font-size: 13.5px; }
.empty.sm { padding: 14px 0; font-size: 13px; }
.link { color: var(--color-primary); cursor: pointer; }

.cards { display: grid; grid-template-columns: repeat(auto-fill, minmax(280px, 1fr)); gap: 14px; }
.card {
  display: flex; flex-direction: column; gap: 8px; padding: 14px;
  background: var(--color-surface); border: 1px solid var(--color-border); border-radius: var(--radius-xl, 14px);
  transition: border-color 0.15s, box-shadow 0.15s;
}
.card:hover { box-shadow: var(--shadow-sm); }
.card.disabled { opacity: 0.66; }
.card-head { display: flex; align-items: center; justify-content: space-between; gap: 8px; }
.card-name { font-size: 14px; font-weight: 600; }
.badge {
  font-size: 10.5px; font-weight: 600; padding: 2px 8px; border-radius: 999px;
  color: var(--color-primary); background: color-mix(in srgb, var(--color-primary) 14%, transparent);
}
.card-prompt {
  font-size: 12.5px; line-height: 1.5; color: var(--color-muted-foreground);
  display: -webkit-box; -webkit-line-clamp: 2; line-clamp: 2; -webkit-box-orient: vertical; overflow: hidden;
}
.card-foot { display: flex; align-items: center; justify-content: space-between; gap: 8px; margin-top: 2px; }
.card-meta { display: inline-flex; align-items: center; gap: 5px; font-size: 11.5px; color: var(--color-muted-foreground); }
.card-actions { display: flex; align-items: center; gap: 4px; }
.ok { color: #16a34a; }
.err { color: var(--color-danger, #dc2626); }

.icon-btn { color: var(--color-muted-foreground); padding: 5px; border-radius: 8px; cursor: pointer; }
.icon-btn:hover { background: var(--color-muted); }
.icon-btn.sm { padding: 4px; }
.run-btn {
  display: inline-flex; align-items: center; justify-content: center; width: 28px; height: 28px;
  border-radius: 999px; background: var(--color-primary); color: #fff; cursor: pointer;
}
.run-btn:disabled { opacity: 0.5; cursor: not-allowed; }
.btn-primary { padding: 7px 14px; font-size: 13px; font-weight: 500; border-radius: var(--radius-lg, 10px); border: none; background: var(--color-primary); color: #fff; cursor: pointer; }

.switch { display: inline-flex; cursor: pointer; }
.switch input { display: none; }
.switch-track { width: 32px; height: 18px; border-radius: 999px; background: var(--color-muted); position: relative; transition: background 0.15s; }
.switch-knob { position: absolute; top: 2px; left: 2px; width: 14px; height: 14px; border-radius: 50%; background: var(--color-surface); box-shadow: var(--shadow-sm); transition: transform 0.15s; }
.switch input:checked + .switch-track { background: var(--color-primary); }
.switch input:checked + .switch-track .switch-knob { transform: translateX(14px); }

.runs-head { display: flex; align-items: center; justify-content: space-between; margin: 30px 0 12px; }
.runs-head h2 { font-size: 15px; font-weight: 600; }
.runs-tools { display: flex; gap: 8px; }
.status-filter, .run-search {
  padding: 6px 10px; font-size: 12.5px; border: 1px solid var(--color-border); border-radius: var(--radius-lg, 10px);
  background: var(--color-surface); color: var(--color-foreground); outline: none;
}
.run-search { min-width: 220px; }
.runs { border: 1px solid var(--color-border); border-radius: var(--radius-xl, 14px); overflow: hidden; background: var(--color-surface); }
.run-row { display: flex; align-items: center; justify-content: space-between; gap: 12px; padding: 12px 16px; border-bottom: 1px solid var(--color-border); }
.run-row:last-child { border-bottom: none; }
.run-main { display: flex; flex-direction: column; gap: 3px; min-width: 0; }
.run-title { font-size: 13.5px; font-weight: 500; }
.run-time { display: inline-flex; align-items: center; gap: 5px; font-size: 11.5px; color: var(--color-muted-foreground); }
.run-time .dot { width: 7px; height: 7px; border-radius: 50%; background: var(--color-primary); }
.run-err { font-size: 11.5px; color: var(--color-danger, #dc2626); max-width: 50%; text-align: right; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }

.tpl-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(280px, 1fr)); gap: 14px; }
.tpl-card {
  display: flex; flex-direction: column; gap: 8px; padding: 16px; text-align: left;
  background: var(--color-surface); border: 1px solid var(--color-border); border-radius: var(--radius-xl, 14px); cursor: pointer;
  transition: border-color 0.15s, box-shadow 0.15s;
}
.tpl-card:hover { border-color: var(--color-primary); box-shadow: var(--shadow-sm); }
</style>
