<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted, nextTick } from 'vue'
import {
  ArrowLeftIcon, PlayIcon, StopIcon, BoltIcon,
  CheckCircleIcon, ExclamationCircleIcon,
} from '@heroicons/vue/24/outline'
import { api } from '@/composables/api'
import { extractToolDisplayInfo } from '@/composables/toolInfo'
import ChatMessage from '@/components/ChatMessage.vue'
import ToolCallCard from '@/components/ToolCallCard.vue'
import type { AutomationRun, AutomationItem } from '@/types/automation'
import type { SessionEntry, ChatMessage as ChatMsg, ToolCall } from '@/types/api'
import type { TimelineItem } from '@/stores/chat'
import { useAutomationStore } from '@/stores/automation'

// AutomationRunView — the page you land on after clicking a run (or a card while
// it's running). It replays the run's session as a READ-ONLY timeline — the same
// ChatMessage + ToolCallCard vocabulary as the chat canvas — wrapped by a
// run-summary header and a Run-again/Stop footer (no composer: a historical run
// is something you watch, not type into).
//
// Data: a run's `session_id` IS a chat session. api.session(session_id) returns
// the SessionEntry[] JSONL, parsed here into the same TimelineItem shape the chat
// store uses (see restoreCurrentSession). Reusing ChatMessage + ToolCallCard means
// no new renderer — the run is just a replayed session with a different frame.

const props = defineProps<{ run: AutomationRun; automation?: AutomationItem | null }>()
const emit = defineEmits<{ (e: 'back'): void }>()

const store = useAutomationStore()

const timeline = ref<TimelineItem[]>([])
const loading = ref(true)
const error = ref<string | null>(null)
const messagesEl = ref<HTMLDivElement | null>(null)

// Whether this run is currently executing. Seeded from the run's status, then
// reconciled via a poll — the run view isn't wired to the task-scoped WS stream
// the chat canvas uses, so we refresh the runs list to detect completion.
const running = ref(props.run.terminal_status === 'running' || (!props.run.terminal_status && props.run.status === 'running'))

let seqCounter = 0
function nextSeq() { return ++seqCounter }
function genId(p: string) { return `${p}-${Math.random().toString(36).slice(2, 9)}` }

// ── Status helpers ── mirror the card/list vocabulary so state reads the same.
const statusKind = computed<'success' | 'error' | 'running'>(() => {
  const s = props.run.terminal_status || props.run.status
  if (s === 'success') return 'success'
  if (s === 'error') return 'error'
  return 'running'
})
const statusLabel = computed(() => ({ success: 'Completed', error: 'Failed', running: 'Running' })[statusKind.value])

// ── Run summary stats line ── the six facts you'd otherwise hunt for.
const windowLabel = computed(() => props.automation?.human_schedule || props.run.trigger_kind)
const startLabel = computed(() => {
  if (!props.run.start_time) return ''
  return new Date(props.run.start_time).toLocaleString()
})
const durLabel = computed(() => {
  if (!props.run.start_time) return ''
  const end = props.run.end_time ? new Date(props.run.end_time).getTime() : (running.value ? Date.now() : new Date(props.run.start_time).getTime())
  const sec = Math.max(0, Math.round((end - new Date(props.run.start_time).getTime()) / 1000))
  if (sec < 60) return `${sec}s`
  const m = Math.floor(sec / 60); const s = sec % 60
  return s ? `${m}m ${s}s` : `${m}m`
})
const toolCount = computed(() => timeline.value.filter((i) => i.kind === 'tool').length)

// ── Parse the session JSONL into a timeline. Same mapping as the chat store's
// restoreCurrentSession: user/assistant → message, tool_call/tool_result → tool.
function applyEntries(entries: SessionEntry[]) {
  timeline.value = []
  const pending = new Map<string, ToolCall>()
  for (const e of entries) {
    if (e.type === 'user' && e.content) {
      const msg: ChatMsg = { id: genId('m'), role: 'user', content: e.content, timestamp: e.timestamp ? new Date(e.timestamp).getTime() : Date.now() }
      timeline.value.push({ kind: 'message', data: msg, seq: nextSeq() })
    } else if (e.type === 'assistant' && e.content) {
      const msg: ChatMsg = { id: genId('m'), role: 'assistant', content: e.content, timestamp: e.timestamp ? new Date(e.timestamp).getTime() : Date.now() }
      timeline.value.push({ kind: 'message', data: msg, seq: nextSeq() })
    } else if (e.type === 'tool_call' && e.name) {
      const tc: ToolCall = {
        id: genId('tc'),
        toolCallID: e.tool_call_id,
        name: e.name,
        args: e.args || '',
        status: 'running',
        timestamp: e.timestamp ? new Date(e.timestamp).getTime() : Date.now(),
        displayInfo: extractToolDisplayInfo(e.name, e.args || ''),
      }
      timeline.value.push({ kind: 'tool', data: tc, seq: nextSeq() })
      if (e.tool_call_id) pending.set(e.tool_call_id, tc)
    } else if (e.type === 'tool_result') {
      let resolved = false
      if (e.tool_call_id) {
        const tc = pending.get(e.tool_call_id)
        if (tc) {
          tc.output = e.output || ''
          tc.error = e.error || ''
          tc.status = e.error ? 'error' : 'done'
          pending.delete(e.tool_call_id)
          resolved = true
        }
      }
      if (!resolved && e.name) {
        for (let i = timeline.value.length - 1; i >= 0; i--) {
          const item = timeline.value[i]
          if (item && item.kind === 'tool' && item.data.name === e.name && item.data.status === 'running') {
            item.data.output = e.output || ''
            item.data.error = e.error || ''
            item.data.status = e.error ? 'error' : 'done'
            break
          }
        }
      }
    }
  }
  // Mark any tool calls that never got a result as done (replay of a finished run).
  for (const tc of pending.values()) tc.status = 'done'
}

async function load() {
  loading.value = true
  error.value = null
  try {
    const entries = await api.session(props.run.session_id)
    applyEntries(entries)
    await nextTick(() => { if (running.value) scrollToBottom(false) })
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e)
  } finally {
    loading.value = false
  }
}

function scrollToBottom(smooth = true) {
  if (!messagesEl.value) return
  messagesEl.value.scrollTo({ top: messagesEl.value.scrollHeight, behavior: smooth ? 'smooth' : 'instant' })
}

// ── Actions ──
async function runAgain() {
  if (!props.automation) return
  await store.runNow(props.automation.id)
  // Give the new run a moment to register, then refresh and re-seed this view.
  setTimeout(() => { void store.fetchAll() }, 1200)
}

async function stopRun() {
  try {
    await api.stop(props.run.session_id)
    running.value = false
  } catch {
    // Surface as a stopped state regardless — the user asked to stop.
    running.value = false
  }
}

// ── Lifecycle ── fetch on mount; poll the runs list while a run is live so the
// header + status reconcile without a task-scoped WS subscription.
let pollTimer: ReturnType<typeof setInterval> | null = null
onMounted(() => {
  void load()
  if (running.value) {
    pollTimer = setInterval(async () => {
      await store.fetchAll()
      const fresh = store.runs.find((r) => r.session_id === props.run.session_id)
      if (fresh && fresh.terminal_status && fresh.terminal_status !== 'running') {
        running.value = false
        if (pollTimer) { clearInterval(pollTimer); pollTimer = null }
        void load() // re-render the now-complete timeline
      }
    }, 3000)
  }
})
onUnmounted(() => { if (pollTimer) clearInterval(pollTimer) })

// The first user message is the automation prompt — render it distinctly.
const promptEntry = computed(() => timeline.value.find((i) => i.kind === 'message' && i.data.role === 'user'))
const timelineRest = computed(() => {
  const p = promptEntry.value
  if (!p) return timeline.value
  return timeline.value.filter((i) => i !== p)
})
</script>

<template>
  <div class="run-surface">
    <!-- ── run summary header ── replaces the "Automations" title. Breadcrumb
         back-link, automation name + outcome chip, and a dense stats line. -->
    <header class="run-head">
      <button class="breadcrumb" @click="emit('back')">
        <ArrowLeftIcon class="w-3.5 h-3.5" /> Automations
      </button>
      <div class="run-title-row">
        <span class="run-title">{{ automation?.name || run.title || 'Automation run' }}</span>
        <span class="status-chip lg" :class="statusKind">
          <CheckCircleIcon v-if="statusKind === 'success'" class="w-3.5 h-3.5" />
          <ExclamationCircleIcon v-else-if="statusKind === 'error'" class="w-3.5 h-3.5" />
          <PlayIcon v-else class="w-3.5 h-3.5" />
          {{ statusLabel }}
        </span>
      </div>
      <div class="run-stats">
        <span class="k">trigger</span><span class="v">{{ run.trigger_kind }}</span>
        <template v-if="windowLabel"><span class="sep">·</span><span class="k">window</span><span class="v">{{ windowLabel }}</span></template>
        <template v-if="startLabel"><span class="sep">·</span><span class="k">ran</span><span class="v">{{ startLabel }}</span></template>
        <template v-if="durLabel"><span class="sep">·</span><span class="k">duration</span><span class="v">{{ durLabel }}</span></template>
        <template v-if="toolCount"><span class="sep">·</span><span class="k">tools</span><span class="v">{{ toolCount }} calls</span></template>
      </div>
      <div v-if="run.error_reason && statusKind === 'error'" class="run-err-banner">{{ run.error_reason }}</div>
    </header>

    <!-- ── timeline ── the run's session, same centered column + vocabulary as
         the chat canvas. -->
    <div ref="messagesEl" class="run-scroll">
      <div class="timeline">
        <div v-if="loading" class="empty">Loading run…</div>
        <div v-else-if="error" class="empty err">Couldn't load this run: {{ error }}</div>
        <div v-else-if="!timeline.length" class="empty">No activity recorded for this run.</div>
        <template v-else>
          <!-- the automation prompt: brand-tinted block, not a user avatar -->
          <div v-if="promptEntry && promptEntry.kind === 'message'" class="run-prompt">
            <div class="run-prompt-icon"><BoltIcon class="w-4 h-4" /></div>
            <div class="run-prompt-body">
              <div class="run-prompt-label">Automation prompt</div>
              <div class="run-prompt-text">{{ (promptEntry.data as ChatMsg).content }}</div>
            </div>
          </div>

          <!-- the rest of the timeline (assistant messages + tool calls) -->
          <template v-for="item in timelineRest" :key="item.seq">
            <ChatMessage
              v-if="item.kind === 'message'"
              :message="item.data"
              class="run-msg"
            />
            <ToolCallCard v-else-if="item.kind === 'tool'" :tool="item.data" class="run-tool" />
          </template>

          <!-- live thinking footer (running runs only) -->
          <div v-if="running && !loading" class="thinking">
            <span class="dots"><span /><span /><span /></span>
            <span class="thinking-label">Thinking…</span>
          </div>
        </template>
      </div>
    </div>

    <!-- ── footer ── replaces the composer. A historical run ends in an action
         bar; a live run shows Stop. -->
    <footer class="run-foot">
      <div class="run-foot-inner">
        <span class="run-foot-hint">
          <template v-if="running"><span class="live">●</span> Running for {{ durLabel }} · {{ toolCount }} tool calls so far.</template>
          <template v-else-if="toolCount">Completed in {{ durLabel }} · {{ toolCount }} tool calls.</template>
          <template v-else>Completed in {{ durLabel }}.</template>
        </span>
        <div class="run-foot-actions">
          <button v-if="running" class="btn-danger" @click="stopRun">
            <StopIcon class="w-3.5 h-3.5" /> Stop run
          </button>
          <button v-else-if="automation" class="btn-primary" @click="runAgain">
            <PlayIcon class="w-3.5 h-3.5" /> Run again
          </button>
        </div>
      </div>
    </footer>
  </div>
</template>

<style scoped>
.run-surface {
  display: flex; flex-direction: column;
  min-height: 0; height: 100%;
  background: var(--color-surface);
  border: 1px solid var(--color-border);
  border-radius: var(--radius-2xl);
  box-shadow: var(--shadow-sm);
  overflow: hidden;
  color: var(--color-foreground);
  margin: 48px 14px 14px;
}

/* ── run summary header ── */
.run-head {
  flex-shrink: 0;
  max-width: 48rem; width: 100%;
  margin-inline: auto;
  padding: 14px 20px;
  border-bottom: 1px solid var(--color-border);
}
.breadcrumb {
  display: inline-flex; align-items: center; gap: 5px;
  font-family: inherit; font-size: 12px;
  color: var(--color-muted-foreground);
  background: none; border: none; padding: 0;
  cursor: pointer; margin-bottom: 10px;
  transition: color 0.15s;
}
.breadcrumb:hover { color: var(--color-foreground); }
.run-title-row { display: flex; align-items: center; justify-content: space-between; gap: 10px; }
.run-title { font-size: 18px; font-weight: 600; letter-spacing: -0.01em; }

.status-chip {
  display: inline-flex; align-items: center; gap: 4px;
  font-size: 10.5px; font-weight: 600; line-height: 1;
  padding: 3px 7px; border-radius: 999px; white-space: nowrap;
}
.status-chip.lg { padding: 4px 9px; font-size: 11.5px; }
.status-chip.success { color: var(--color-success-fg); background: var(--color-success-bg); }
.status-chip.error   { color: var(--color-error-fg);   background: var(--color-error-bg); }
.status-chip.running { color: var(--color-primary);    background: var(--accent-wash, color-mix(in srgb, var(--color-primary) 11%, transparent)); }

.run-stats {
  display: flex; flex-wrap: wrap; align-items: center;
  gap: 4px 10px; margin-top: 7px;
  font-size: 12px; color: var(--color-muted-foreground); font-variant-numeric: tabular-nums;
}
.run-stats .sep { color: var(--color-border); }
.run-stats .k { font-family: var(--font-mono); font-size: 10.5px; }
.run-stats .v { color: var(--color-foreground); font-weight: 500; }
.run-err-banner {
  margin-top: 9px; padding: 8px 10px;
  font-size: 12px; line-height: 1.5; color: var(--color-error-fg);
  background: var(--color-error-bg); border-radius: var(--radius-md, 6px);
}

/* ── timeline ── centered column, same vocabulary as the chat canvas. */
.run-scroll { flex: 1; min-height: 0; overflow-y: auto; }
.timeline { max-width: 48rem; margin-inline: auto; padding: 10px 20px 40px; }
.empty { padding: 28px 0; color: var(--color-muted-foreground); font-size: 13.5px; }
.empty.err { color: var(--color-error-fg); }

/* the automation prompt — brand-tinted block with an eyebrow, so it reads as
   "this came from the schedule", not a hand-typed user message. */
.run-prompt {
  display: flex; gap: 11px;
  margin: 8px 0 4px; padding: 14px 16px;
  background: var(--accent-wash-soft, color-mix(in srgb, var(--color-primary) 8%, transparent));
  border: 1px solid var(--accent-border, color-mix(in srgb, var(--color-primary) 30%, transparent));
  border-radius: var(--radius-xl, 12px);
}
.run-prompt-icon {
  flex-shrink: 0; width: 28px; height: 28px;
  display: grid; place-items: center;
  border-radius: var(--radius-md, 6px);
  color: var(--color-primary); background: var(--color-surface);
  border: 1px solid var(--accent-border, color-mix(in srgb, var(--color-primary) 30%, transparent));
}
.run-prompt-body { min-width: 0; }
.run-prompt-label {
  font-family: var(--font-mono); font-size: 10px; font-weight: 600; letter-spacing: 0.06em;
  text-transform: uppercase; color: var(--color-primary); margin-bottom: 4px;
}
.run-prompt-text { font-size: 13.5px; line-height: 1.6; color: var(--color-foreground); }

/* reuse the chat components but indent tool cards into the message column */
.run-msg { padding: 10px 0; }
.run-tool { padding-left: 2.25rem; margin: 4px 0; }

/* live thinking footer */
.thinking { display: flex; align-items: center; gap: 9px; padding: 13px 0 13px 2.25rem; }
.thinking .dots { display: flex; gap: 4px; }
.thinking .dots span {
  width: 6px; height: 6px; border-radius: 50%;
  background: var(--color-accent-neutral, var(--color-foreground));
  animation: dot-pulse 1.2s ease-in-out infinite;
}
.thinking .dots span:nth-child(2) { animation-delay: 0.16s; }
.thinking .dots span:nth-child(3) { animation-delay: 0.32s; }
.thinking-label { font-size: 13px; color: var(--color-muted-foreground); }
@keyframes dot-pulse { 0%, 100% { opacity: 0.3; transform: scale(0.8); } 50% { opacity: 1; transform: scale(1); } }

/* ── footer ── replaces the composer. */
.run-foot { flex-shrink: 0; border-top: 1px solid var(--color-border); background: var(--color-muted); }
.run-foot-inner {
  max-width: 48rem; margin-inline: auto; padding: 12px 20px;
  display: flex; align-items: center; justify-content: space-between; gap: 12px;
}
.run-foot-hint { font-size: 12px; color: var(--color-muted-foreground); min-width: 0; }
.run-foot-hint .live { color: var(--color-primary); font-weight: 600; }
.run-foot-actions { display: flex; gap: 8px; flex-shrink: 0; }
.btn-primary {
  display: inline-flex; align-items: center; gap: 6px;
  padding: 6px 12px; font-family: inherit; font-size: 12.5px; font-weight: 500;
  border: none; border-radius: var(--radius-lg, 10px);
  background: var(--color-primary); color: var(--color-on-primary); cursor: pointer; transition: opacity 0.15s;
}
.btn-primary:hover { opacity: 0.9; }
.btn-danger {
  display: inline-flex; align-items: center; gap: 6px;
  padding: 6px 12px; font-family: inherit; font-size: 12.5px; font-weight: 500;
  border: none; border-radius: var(--radius-lg, 10px);
  background: var(--color-destructive); color: var(--color-on-destructive); cursor: pointer; transition: opacity 0.15s;
}
.btn-danger:hover { opacity: 0.9; }
</style>
