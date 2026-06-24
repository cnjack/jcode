<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { PlayIcon, TrashIcon, PencilSquareIcon, CheckCircleIcon, ExclamationCircleIcon, BoltIcon, CalendarDaysIcon, HandRaisedIcon, ClockIcon } from '@heroicons/vue/24/outline'
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

// The selected run, if the user drilled into one. When set, the parent renders
// AutomationRunView instead; emitting 'open-run' hands a run up so a card's
// "run now" can optionally land on the detail page once the run registers.
const emit = defineEmits<{ (e: 'open-run', run: AutomationRun): void }>()

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

// ── Card state helpers ── drive the status strip colour + chip. A card maps to
// one of four states: running / error / success / paused. Paused is the absence
// of a run state (enabled=false), so it drops the chip and dims the card.
type CardState = 'running' | 'error' | 'success' | 'paused'
function cardState(a: AutomationItem): CardState {
  if (!a.enabled) return 'paused'
  const s = a.state.last_status
  if (s === 'running') return 'running'
  if (s === 'error') return 'error'
  return 'success'
}

// The cadence chip: the trigger as a compact label + icon (Daily/Weekly/Hourly/
// Manual). Falls back to a generic "Schedule"/"Manual" when cadence is absent.
const cadenceMap: Record<string, { label: string; icon: typeof CalendarDaysIcon }> = {
  daily: { label: 'Daily', icon: CalendarDaysIcon },
  weekly: { label: 'Weekly', icon: CalendarDaysIcon },
  hourly: { label: 'Hourly', icon: ClockIcon },
}
function cadenceOf(a: AutomationItem) {
  if (a.trigger.type === 'manual') return { label: 'Manual', icon: HandRaisedIcon }
  const c = a.trigger.cadence ? cadenceMap[a.trigger.cadence] : null
  return c ?? { label: 'Schedule', icon: CalendarDaysIcon }
}

// Next/last run line. Scheduled automations show "next" (the brand-accent line,
// the highest-signal text on the card); manual ones show "last run" instead.
function nextRunLabel(a: AutomationItem): string {
  if (a.trigger.type === 'manual') {
    const last = a.state.last_run_at
    if (!last) return 'Not run yet'
    const ok = a.state.last_status === 'success'
    return relTime(last) + (ok ? ' · ok' : a.state.last_status === 'error' ? ' · failed' : '')
  }
  const next = a.state.next_run_at
  return next ? 'in ' + relTimeFromNow(next) : a.human_schedule
}

// Relative-time helpers — compact ("3d ago", "in 6h 12m"). Kept tiny since the
// backend already supplies human_schedule for the schedule line itself.
function relTime(iso: string): string {
  const diff = Date.now() - new Date(iso).getTime()
  return relLabel(diff)
}
function relTimeFromNow(iso: string): string {
  const diff = new Date(iso).getTime() - Date.now()
  return relLabel(diff)
}
function relLabel(diff: number): string {
  const abs = Math.abs(diff)
  const min = 60000, hr = 60 * min, day = 24 * hr
  if (abs < min) return 'just now'
  if (abs < hr) return `${Math.round(abs / min)}m`
  if (abs < day) {
    const h = Math.floor(abs / hr); const m = Math.round((abs % hr) / min)
    return m ? `${h}h ${m}m` : `${h}h`
  }
  return `${Math.round(abs / day)}d`
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
      <button class="btn-primary" @click="newAutomation">
        <PencilSquareIcon class="w-3.5 h-3.5" />
        New automation
      </button>
    </template>

      <!-- ── Your automations ── -->
      <template v-if="view === 'list'">
        <div v-if="store.loading && !store.items.length" class="empty">Loading…</div>

        <!-- Empty state — a real designed panel (icon + heading + actions) that
             fills the page column and is vertically centred, rather than two
             stray lines of text floating at the top of the canvas. -->
        <div v-else-if="!store.items.length" class="col center-empty">
          <div class="empty-hero">
            <div class="empty-hero-icon"><BoltIcon class="w-5 h-5" /></div>
            <h2 class="empty-hero-title">No automations yet</h2>
            <p class="empty-hero-sub">Use agents to handle recurring work on a cadence you choose.</p>
            <div class="empty-hero-actions">
              <button class="btn-primary" @click="newAutomation">New automation</button>
              <button class="btn-ghost" @click="view = 'templates'">Browse templates</button>
            </div>
          </div>
        </div>

        <template v-else>
          <div class="col">
            <p class="section-sub">Use agents to handle recurring work on a cadence you choose.</p>

            <div class="cards">
              <div
                v-for="a in store.items"
                :key="a.id"
                class="card"
                :class="cardState(a)"
              >
                <div class="card-top">
                  <div class="card-title-row">
                    <span class="card-name">{{ a.name }}</span>
                    <span class="cadence">
                      <component :is="cadenceOf(a).icon" class="w-3 h-3" />
                      {{ cadenceOf(a).label }}
                    </span>
                  </div>
                  <!-- Status chip — labelled (Ran ok / Running / Failed). Paused
                       cards omit it entirely. -->
                  <span v-if="cardState(a) === 'success'" class="status-chip ok"><CheckCircleIcon class="w-3 h-3" />Ran ok</span>
                  <span v-else-if="cardState(a) === 'running'" class="status-chip run"><PlayIcon class="w-3 h-3" />Running</span>
                  <span v-else-if="cardState(a) === 'error'" class="status-chip err"><ExclamationCircleIcon class="w-3 h-3" />Failed</span>
                </div>
                <p class="card-prompt">{{ a.prompt }}</p>
                <div class="card-meta">
                  <span class="meta-label">Schedule</span>
                  <span class="meta-value">{{ a.human_schedule }}</span>
                  <span class="meta-label">{{ a.trigger.type === 'manual' ? 'Last run' : 'Next run' }}</span>
                  <span class="meta-value" :class="{ next: a.trigger.type !== 'manual' && a.enabled }">{{ nextRunLabel(a) }}</span>
                  <div class="card-actions">
                    <button class="icon-btn sm" title="Edit" @click="editAutomation(a)"><PencilSquareIcon class="w-4 h-4" /></button>
                    <button class="icon-btn sm danger" title="Delete" @click="store.remove(a.id)"><TrashIcon class="w-4 h-4" /></button>
                    <label class="switch" :title="a.enabled ? 'Enabled' : 'Disabled'">
                      <input type="checkbox" :checked="a.enabled" @change="store.setEnabled(a, ($event.target as HTMLInputElement).checked)" />
                      <span class="switch-track"><span class="switch-knob" /></span>
                    </label>
                    <button class="run-btn" :class="{ live: isRunning(a) }" :disabled="isRunning(a)" title="Run now" @click="store.runNow(a.id)">
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
              <button
                v-for="r in filteredRuns"
                :key="r.session_id"
                class="run-row"
                :data-status="r.terminal_status === 'success' ? 'success' : r.terminal_status === 'error' ? 'error' : 'running'"
                @click="emit('open-run', r)"
              >
                <div class="run-main">
                  <span class="run-title">{{ r.title || 'Automation run' }}</span>
                  <span class="run-meta">
                    <span>{{ runLabel(r) }}</span>
                    <span class="sep">·</span>
                    <span class="trigger-tag">{{ r.trigger_kind }}</span>
                  </span>
                  <span v-if="r.error_reason" class="run-err">{{ r.error_reason }}</span>
                </div>
              </button>
            </div>
          </div>
        </template>
      </template>

      <!-- ── Templates ── -->
      <template v-else>
        <div class="col">
          <p class="section-sub">Start from a template — pick a project and confirm.</p>
          <div class="tpl-grid">
            <button v-for="t in store.templates" :key="t.id" class="tpl-card" @click="fromTemplate(t)">
              <div class="card-top">
                <span class="card-name">{{ t.name }}</span>
                <span class="badge">{{ t.badge }}</span>
              </div>
              <p class="card-prompt">{{ t.description }}</p>
              <div class="tpl-foot">{{ t.suggest_mode === 'full_access' ? 'Autopilot' : t.suggest_mode === 'approval' ? 'Ask' : 'Plan' }} · {{ t.trigger.type === 'manual' ? 'manual' : 'schedule' }}</div>
            </button>
          </div>
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
 * (segmented toggle + primary button). */
.seg { display: flex; gap: 2px; background: var(--color-muted); padding: 2px; border-radius: 999px; }
.seg-btn {
  padding: 4px 11px; font-family: inherit; font-size: 12px;
  border: none; border-radius: 999px; color: var(--color-muted-foreground); cursor: pointer;
  transition: background 0.15s, color 0.15s;
}
.seg-btn.on { background: var(--color-surface); color: var(--color-foreground); box-shadow: var(--shadow-sm); }

/* Content column — every block aligns to this centered max-w so cards/runs line
 * up with the header rather than hugging the panel edges. The empty-state
 * variant fills the body and centres the hero vertically. */
.col { max-width: 48rem; width: 100%; margin-left: auto; margin-right: auto; padding-left: 20px; padding-right: 20px; }
.col.center-empty {
  flex: 1; display: flex; flex-direction: column; align-items: center; justify-content: center;
  max-width: 380px; text-align: center;
}
.section-sub { font-size: 13px; color: var(--color-muted-foreground); padding-top: 18px; padding-bottom: 18px; line-height: 1.5; }
.empty { padding: 28px 0; color: var(--color-muted-foreground); font-size: 13.5px; }
.empty.sm { padding: 14px 0; font-size: 13px; }

/* Empty state — a centred, designed panel so the page never reads as "blank".
   The icon is a quiet outlined tile (no solid orange fill) so it harmonises with
   the surface rather than punching a hot-coloured disc into the canvas. */
.empty-hero { display: flex; flex-direction: column; align-items: center; gap: 10px; }
.empty-hero-icon {
  display: grid; place-items: center;
  width: 40px; height: 40px; margin-bottom: 4px;
  border-radius: var(--radius-lg);
  color: var(--color-muted-foreground);
  border: 1px solid var(--color-border);
  background: var(--color-surface);
}
.empty-hero-title { font-size: 15px; font-weight: 600; color: var(--color-foreground); }
.empty-hero-sub { font-size: 13px; line-height: 1.55; color: var(--color-muted-foreground); }
.empty-hero-actions { display: flex; gap: 8px; margin-top: 12px; }
.btn-ghost {
  padding: 6px 12px; font-family: inherit; font-size: 12.5px; font-weight: 500; border-radius: var(--radius-lg, 10px);
  border: 1px solid var(--color-border); background: var(--color-surface); color: var(--color-foreground);
  cursor: pointer; transition: background 0.15s;
}
.btn-ghost:hover { background: var(--color-muted); }

/* ── Automation cards ── redesigned anatomy: status strip + cadence/status
   header, prompt, then a labelled Schedule / Next-run meta grid. */
.cards { display: grid; grid-template-columns: repeat(auto-fill, minmax(300px, 1fr)); gap: 14px; padding-bottom: 6px; }
.card {
  position: relative;
  display: flex; flex-direction: column; gap: 8px; padding: 14px 14px 12px;
  background: var(--color-background); border: 1px solid var(--color-border); border-radius: var(--radius-xl, 14px);
  overflow: hidden;
  transition: border-color 0.15s, box-shadow 0.15s;
}
.card:hover { box-shadow: var(--shadow-sm); }
/* Status strip — a 3px left edge whose colour encodes the run state. */
.card::before {
  content: ""; position: absolute; left: 0; top: 0; bottom: 0; width: 3px; background: var(--color-border);
}
.card.success::before { background: var(--color-success); }
.card.error::before   { background: var(--color-destructive); }
.card.running::before { background: var(--color-primary); }
.card.paused::before  { background: var(--color-border); }
.card.paused { opacity: 0.7; }

.card-top { display: flex; align-items: center; justify-content: space-between; gap: 8px; }
.card-title-row { display: flex; align-items: center; gap: 8px; min-width: 0; }
.card-name { font-size: 14px; font-weight: 600; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
/* Cadence chip — the trigger as a compact pill. */
.cadence {
  display: inline-flex; align-items: center; gap: 4px;
  font-size: 11px; font-weight: 500; line-height: 1;
  padding: 3px 8px; border-radius: 999px;
  color: var(--color-muted-foreground); background: var(--color-muted);
  white-space: nowrap; flex-shrink: 0;
}
/* Status chip — labelled. */
.status-chip {
  display: inline-flex; align-items: center; gap: 4px;
  font-size: 10.5px; font-weight: 600; line-height: 1;
  padding: 3px 7px; border-radius: 999px; white-space: nowrap; flex-shrink: 0;
}
.status-chip.ok  { color: var(--color-success-fg); background: var(--color-success-bg); }
.status-chip.err { color: var(--color-error-fg);   background: var(--color-error-bg); }
.status-chip.run { color: var(--color-primary);    background: var(--accent-wash, color-mix(in srgb, var(--color-primary) 11%, transparent)); }

.card-prompt {
  font-size: 12.5px; line-height: 1.5; color: var(--color-muted-foreground);
  display: -webkit-box; -webkit-line-clamp: 2; line-clamp: 2; -webkit-box-orient: vertical; overflow: hidden;
  min-height: 2.6em;
}

/* Meta grid — the schedule and the next/last run are two labelled facts, not
   two clauses joined by "·". Actions sit in a right column spanning both rows. */
.card-meta {
  display: grid;
  grid-template-columns: auto 1fr;
  grid-auto-rows: auto;
  align-items: center;
  gap: 4px 12px;
  margin-top: 4px;
  padding-top: 10px;
  border-top: 1px solid var(--color-border);
}
.meta-label {
  font-family: var(--font-mono);
  grid-column: 1;
  font-size: 9.5px; font-weight: 600; letter-spacing: 0.06em; text-transform: uppercase;
  color: var(--color-muted-foreground);
}
.meta-value {
  grid-column: 2;
  font-size: 12px; color: var(--color-foreground);
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
}
/* The next-run value is the highest-signal text on a scheduled card — brand
   accent so the eye lands there. */
.meta-value.next {
  color: var(--color-primary); font-weight: 600; font-variant-numeric: tabular-nums;
}

.card-actions {
  grid-column: 3; grid-row: 1 / span 2;
  display: flex; align-items: center; gap: 2px;
}
.icon-btn { color: var(--color-muted-foreground); padding: 5px; border-radius: var(--radius-md, 8px); cursor: pointer; transition: background 0.15s, color 0.15s; }
.icon-btn:hover { background: var(--color-muted); color: var(--color-foreground); }
.icon-btn.sm { padding: 4px; }
.icon-btn.danger:hover { color: var(--color-destructive); }

/* Run-now button — a quiet ghost icon button in the same family as edit/delete,
   NOT a primary-coloured chip. A vivid orange button on every card floods the
   grid with accent and dominates the row. */
.run-btn {
  display: inline-grid; place-items: center;
  width: 26px; height: 26px;
  border: none; border-radius: var(--radius-md, 8px);
  background: transparent; color: var(--color-muted-foreground);
  cursor: pointer; transition: background 0.15s, color 0.15s;
}
.run-btn:hover:not(:disabled) { background: var(--color-muted); color: var(--color-foreground); }
.run-btn:disabled { opacity: 0.45; cursor: not-allowed; }
.run-btn.live { color: var(--color-primary); }

.btn-primary {
  display: inline-flex; align-items: center; gap: 6px;
  padding: 6px 12px; font-family: inherit; font-size: 12.5px; font-weight: 500; border-radius: var(--radius-lg, 10px);
  border: none; background: var(--color-primary); color: var(--color-on-primary); cursor: pointer; transition: opacity 0.15s;
}
.btn-primary:hover { opacity: 0.9; }

.switch { display: inline-flex; cursor: pointer; }
.switch input { display: none; }
.switch-track { width: 32px; height: 18px; border-radius: 999px; background: var(--color-muted); position: relative; transition: background 0.15s; }
.switch-knob { position: absolute; top: 2px; left: 2px; width: 14px; height: 14px; border-radius: 50%; background: var(--color-surface); box-shadow: var(--shadow-sm); transition: transform 0.15s; }
.switch input:checked + .switch-track { background: var(--color-primary); }
.switch input:checked + .switch-track .switch-knob { transform: translateX(14px); }

/* ── Recent runs — a real list with a status colour-bar + tabular metadata. */
.runs-head { display: flex; align-items: center; justify-content: space-between; margin-top: 30px; margin-bottom: 12px; }
.runs-head h2 { font-size: 15px; font-weight: 600; }
.runs-tools { display: flex; gap: 8px; align-items: center; }
.run-search {
  height: 32px; padding: 0 10px; font-family: inherit; font-size: 12.5px; border: 1px solid var(--color-border); border-radius: var(--radius-lg, 10px);
  background: var(--color-background); color: var(--color-foreground); outline: none; min-width: 220px;
}
.run-search:focus { border-color: var(--color-primary); }
.runs { border: 1px solid var(--color-border); border-radius: var(--radius-xl, 14px); overflow: hidden; background: var(--color-background); margin-bottom: 24px; }
.run-row {
  position: relative;
  display: block; width: 100%; text-align: left;
  padding: 12px 16px 12px 18px;
  background: transparent; border: none; border-bottom: 1px solid var(--color-border);
  cursor: pointer; transition: background 0.15s;
  font-family: inherit;
}
.run-row:last-child { border-bottom: none; }
.run-row:hover { background: var(--neutral-wash-soft, color-mix(in srgb, var(--color-foreground) 8%, transparent)); }
.run-row::before {
  content: ""; position: absolute; left: 0; top: 6px; bottom: 6px;
  width: 3px; border-radius: 999px; background: var(--color-border);
}
.run-row[data-status="success"]::before { background: var(--color-success); }
.run-row[data-status="error"]::before   { background: var(--color-destructive); }
.run-row[data-status="running"]::before { background: var(--color-primary); }
.run-main { display: flex; flex-direction: column; gap: 3px; min-width: 0; }
.run-title { font-size: 13.5px; font-weight: 500; }
.run-meta {
  display: inline-flex; align-items: center; gap: 8px;
  font-size: 11.5px; color: var(--color-muted-foreground); font-variant-numeric: tabular-nums;
}
.run-meta .sep { color: var(--color-border); }
.trigger-tag {
  font-size: 10.5px; font-weight: 600; padding: 1px 6px; border-radius: var(--radius-sm, 4px);
  color: var(--color-muted-foreground); background: var(--color-muted);
}
.run-err { font-size: 11.5px; line-height: 1.45; color: var(--color-error-fg); margin-top: 2px; }

/* ── Templates ── */
.tpl-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(300px, 1fr)); gap: 14px; padding-bottom: 24px; }
.tpl-card {
  display: flex; flex-direction: column; gap: 8px; padding: 16px; text-align: left;
  background: var(--color-background); border: 1px solid var(--color-border); border-radius: var(--radius-xl, 14px); cursor: pointer;
  transition: border-color 0.15s, box-shadow 0.15s;
  font-family: inherit;
}
.tpl-card:hover { border-color: var(--color-primary); box-shadow: var(--shadow-sm); }
.tpl-card .card-top { margin-bottom: 2px; }
.badge {
  font-size: 10.5px; font-weight: 600; padding: 2px 8px; border-radius: 999px;
  color: var(--color-primary); background: var(--accent-wash, color-mix(in srgb, var(--color-primary) 14%, transparent));
}
.tpl-foot { display: flex; align-items: center; gap: 6px; font-size: 11.5px; color: var(--color-muted-foreground); margin-top: 2px; }
</style>
