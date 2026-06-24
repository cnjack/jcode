<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { PlayIcon, TrashIcon, PencilSquareIcon, CheckCircleIcon, ExclamationCircleIcon, BoltIcon } from '@heroicons/vue/24/outline'
import { useAutomationStore } from '@/stores/automation'
import AutomationEditorDialog from '@/components/AutomationEditorDialog.vue'
import PageSurface from '@/components/PageSurface.vue'
import MenuSelect, { type MenuSelectOption } from '@/components/MenuSelect.vue'
import type { AutomationItem, AutomationCreate, AutomationTemplate, AutomationRun } from '@/types/automation'

// Automations is a *page* inside the shell, not a fixed overlay. The parent
// (<main> in App.vue) mounts this component when it's the active view, so there
// is no `open` prop to guard against — fetching happens on mount below. The
// page surface (inset chrome + title head) is provided by PageSurface; this
// component owns only its content.
const store = useAutomationStore()

const view = ref<'list' | 'templates'>('list')
const statusFilter = ref<'all' | 'success' | 'failed'>('all')
const statusOptions: MenuSelectOption[] = [
  { value: 'all', label: 'All' },
  { value: 'success', label: 'Success' },
  { value: 'failed', label: 'Failed' },
]
const search = ref('')
const editorOpen = ref(false)
const editing = ref<AutomationItem | null>(null)
const prefill = ref<Partial<AutomationCreate> | null>(null)

// Fetch on mount. The component is v-if'd by the parent, so this fires exactly
// when the page becomes active (and tears down on exit) — replacing the old
// watch(props.open) that assumed a persistent overlay.
onMounted(() => {
  void store.fetchAll()
  void store.fetchTemplates()
})

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
  <!-- The page surface (inset chrome + title head) is provided by PageSurface;
       this component owns only the content and the header actions (segmented
       toggle + New automation). No close button — dismissal is via Esc / the
       nav header / clicking a task. -->
  <PageSurface title="Automations">
    <template #actions>
      <div class="seg">
        <button :class="['seg-btn', { on: view === 'list' }]" @click="view = 'list'">Your automations</button>
        <button :class="['seg-btn', { on: view === 'templates' }]" @click="view = 'templates'">Templates</button>
      </div>
      <button class="btn-primary" @click="newAutomation">New automation</button>
    </template>

      <!-- ── Your automations ── -->
      <template v-if="view === 'list'">
        <div v-if="store.loading && !store.items.length" class="empty">Loading…</div>

        <!-- Empty state — a real designed panel (icon + heading + actions) rather
             than two stray lines of text floating on the canvas. -->
        <div v-else-if="!store.items.length" class="empty-hero">
          <div class="empty-hero-icon"><BoltIcon class="w-6 h-6" /></div>
          <h2 class="empty-hero-title">No automations yet</h2>
          <p class="empty-hero-sub">Use agents to handle recurring work on a cadence you choose.</p>
          <div class="empty-hero-actions">
            <button class="btn-primary" @click="newAutomation">New automation</button>
            <button class="btn-ghost" @click="view = 'templates'">Browse templates</button>
          </div>
        </div>

        <template v-else>
          <p class="section-sub">Use agents to handle recurring work on a cadence you choose.</p>

          <div class="cards">
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

          <!-- ── Recent runs ── The filter + search only appear once there are
               runs to act on, so an empty list never shows a stray, misaligned
               toolbar. -->
          <div class="runs-head">
            <h2>Recent runs</h2>
            <div v-if="store.runs.length" class="runs-tools">
              <MenuSelect
                v-model="statusFilter"
                :options="statusOptions"
                placement="bottom"
                title="Filter runs"
              />
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

    <AutomationEditorDialog
      :open="editorOpen"
      :editing="editing"
      :prefill="prefill"
      @close="editorOpen = false"
      @saved="store.fetchAll()"
    />
  </PageSurface>
</template>

<style scoped>
/* The page surface (inset chrome + title head + scroll body) is owned by
 * PageSurface. This component styles only its own content + the header actions
 * (segmented toggle + primary button). The .icon-btn below is reused by the
 * per-card edit/delete actions (not a close button). */
.seg { display: flex; gap: 2px; background: var(--color-muted); padding: 2px; border-radius: 999px; }
.seg-btn {
  padding: 4px 11px; font-size: 12px; border-radius: 999px; color: var(--color-muted-foreground); cursor: pointer;
  transition: background 0.15s, color 0.15s;
}
.seg-btn.on { background: var(--color-surface); color: var(--color-foreground); box-shadow: var(--shadow-sm); }
/* Content column matches the chat timeline's centered max-w-4xl + px-5 inset so
 * the cards/runs line up with the chat content rather than hugging the edges.
 * Scoped to the PageSurface body (the default slot's scroll container). */
:deep(.page-body) > * { max-width: 48rem; margin-left: auto; margin-right: auto; padding-left: 20px; padding-right: 20px; }
.section-sub { font-size: 13px; color: var(--color-muted-foreground); padding-top: 18px; padding-bottom: 18px; }
.empty { padding: 28px 0; color: var(--color-muted-foreground); font-size: 13.5px; }
.empty.sm { padding: 14px 0; font-size: 13px; }

/* Empty state — a centered, designed panel so the page never reads as "blank". */
.empty-hero {
  display: flex;
  flex-direction: column;
  align-items: center;
  text-align: center;
  gap: 8px;
  max-width: 380px;
  padding-top: 72px;
  padding-bottom: 56px;
}
.empty-hero-icon {
  display: grid;
  place-items: center;
  width: 48px;
  height: 48px;
  margin-bottom: 6px;
  border-radius: 50%;
  color: var(--color-primary);
  background: color-mix(in srgb, var(--color-primary) 12%, transparent);
}
.empty-hero-title { font-size: 16px; font-weight: 600; color: var(--color-foreground); }
.empty-hero-sub { font-size: 13px; line-height: 1.55; color: var(--color-muted-foreground); }
.empty-hero-actions { display: flex; gap: 8px; margin-top: 14px; }
.btn-ghost {
  padding: 6px 12px; font-size: 12.5px; font-weight: 500; border-radius: var(--radius-lg, 10px);
  border: 1px solid var(--color-border); background: var(--color-surface); color: var(--color-foreground);
  cursor: pointer; transition: background 0.15s;
}
.btn-ghost:hover { background: var(--color-muted); }

.cards { display: grid; grid-template-columns: repeat(auto-fill, minmax(280px, 1fr)); gap: 14px; padding-bottom: 6px; }
.card {
  display: flex; flex-direction: column; gap: 8px; padding: 14px;
  background: var(--color-background); border: 1px solid var(--color-border); border-radius: var(--radius-xl, 14px);
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
.ok { color: var(--color-success); }
.err { color: var(--color-danger, var(--color-destructive)); }

.icon-btn { color: var(--color-muted-foreground); padding: 5px; border-radius: 8px; cursor: pointer; transition: background 0.15s, color 0.15s; }
.icon-btn:hover { background: var(--color-muted); color: var(--color-foreground); }
.icon-btn.sm { padding: 4px; }
.run-btn {
  display: inline-flex; align-items: center; justify-content: center; width: 28px; height: 28px;
  border-radius: 999px; background: var(--color-primary); color: var(--color-on-primary); cursor: pointer;
  transition: opacity 0.15s;
}
.run-btn:hover:not(:disabled) { opacity: 0.9; }
.run-btn:disabled { opacity: 0.5; cursor: not-allowed; }
.btn-primary {
  padding: 6px 12px; font-size: 12.5px; font-weight: 500; border-radius: var(--radius-lg, 10px);
  border: none; background: var(--color-primary); color: var(--color-on-primary); cursor: pointer; transition: opacity 0.15s;
}
.btn-primary:hover { opacity: 0.9; }

.switch { display: inline-flex; cursor: pointer; }
.switch input { display: none; }
.switch-track { width: 32px; height: 18px; border-radius: 999px; background: var(--color-muted); position: relative; transition: background 0.15s; }
.switch-knob { position: absolute; top: 2px; left: 2px; width: 14px; height: 14px; border-radius: 50%; background: var(--color-surface); box-shadow: var(--shadow-sm); transition: transform 0.15s; }
.switch input:checked + .switch-track { background: var(--color-primary); }
.switch input:checked + .switch-track .switch-knob { transform: translateX(14px); }

.runs-head { display: flex; align-items: center; justify-content: space-between; margin: 30px 0 12px; }
.runs-head h2 { font-size: 15px; font-weight: 600; }
.runs-tools { display: flex; gap: 8px; align-items: center; }
.run-search {
  height: 32px; padding: 0 10px; font-size: 12.5px; border: 1px solid var(--color-border); border-radius: var(--radius-lg, 10px);
  background: var(--color-background); color: var(--color-foreground); outline: none; min-width: 220px;
}
.run-search:focus { border-color: var(--color-primary); }
.runs { border: 1px solid var(--color-border); border-radius: var(--radius-xl, 14px); overflow: hidden; background: var(--color-background); margin-bottom: 24px; }
.run-row { display: flex; align-items: center; justify-content: space-between; gap: 12px; padding: 12px 16px; border-bottom: 1px solid var(--color-border); }
.run-row:last-child { border-bottom: none; }
.run-main { display: flex; flex-direction: column; gap: 3px; min-width: 0; }
.run-title { font-size: 13.5px; font-weight: 500; }
.run-time { display: inline-flex; align-items: center; gap: 5px; font-size: 11.5px; color: var(--color-muted-foreground); }
.run-time .dot { width: 7px; height: 7px; border-radius: 50%; background: var(--color-primary); }
.run-err { font-size: 11.5px; color: var(--color-danger, var(--color-destructive)); max-width: 50%; text-align: right; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }

.tpl-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(280px, 1fr)); gap: 14px; padding-bottom: 24px; }
.tpl-card {
  display: flex; flex-direction: column; gap: 8px; padding: 16px; text-align: left;
  background: var(--color-background); border: 1px solid var(--color-border); border-radius: var(--radius-xl, 14px); cursor: pointer;
  transition: border-color 0.15s, box-shadow 0.15s;
}
.tpl-card:hover { border-color: var(--color-primary); box-shadow: var(--shadow-sm); }
</style>
