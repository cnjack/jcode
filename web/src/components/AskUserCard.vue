<script setup lang="ts">
import { ref, computed, reactive, watch, onUnmounted } from 'vue'
import type { ToolCall, AskUserQuestion, AskUserAnswer } from '@/types/api'
import { useChatStore } from '@/stores/chat'

defineOptions({ name: 'AskUserCard' })

const props = defineProps<{
  tool: ToolCall
}>()

const store = useChatStore()
const collapsed = ref(false)
// Locally remembered answers, set on submit/skip so the resolved view shows the
// choice immediately during the brief gap before the tool_result event lands.
const submitted = ref<string[] | null>(null)

// ─── Questions: prefer the backend-normalized list, else parse the tool args ───
const questions = computed<AskUserQuestion[]>(() => {
  if (props.tool.askUserQuestions?.length) return props.tool.askUserQuestions
  try {
    const parsed = JSON.parse(props.tool.args || '{}')
    if (Array.isArray(parsed.questions) && parsed.questions.length) {
      return parsed.questions as AskUserQuestion[]
    }
    // Legacy single-question form: { question, options:[{label}] }
    if (parsed.question) {
      return [{
        question: parsed.question,
        options: Array.isArray(parsed.options)
          ? parsed.options.map((o: { label: string }) => ({ label: o.label }))
          : undefined,
      }]
    }
  } catch { /* ignore */ }
  return []
})

// Pending = there is a live request id and the tool has not resolved yet.
const isPending = computed(() =>
  !!props.tool.askUserId && props.tool.status === 'running' && !props.tool.output,
)

const title = computed(() => questions.value[0]?.question || 'Asking')

// ─── Answer state (per question index) ───
const selections = reactive<Record<number, string[]>>({})
const freeText = reactive<Record<number, string>>({})

function isSelected(qi: number, label: string): boolean {
  return (selections[qi] || []).includes(label)
}

function toggleOption(qi: number, label: string, multi: boolean) {
  const cur = selections[qi] || []
  if (multi) {
    selections[qi] = cur.includes(label) ? cur.filter((l) => l !== label) : [...cur, label]
  } else {
    // Single-select: replace, and clear any "Other" free text.
    selections[qi] = cur.includes(label) ? [] : [label]
    freeText[qi] = ''
  }
}

function onFreeTextInput(qi: number, multi: boolean) {
  // Typing a custom answer clears option selection for single-select.
  if (!multi && freeText[qi]) selections[qi] = []
}

function buildAnswers(): AskUserAnswer[] {
  return questions.value.map((q, qi) => {
    const header = q.header || ''
    const text = (freeText[qi] || '').trim()
    const picked = selections[qi] || []
    if (q.multi_select) {
      const selected = [...picked]
      if (text) selected.push(text)
      return { question_header: header, answer: '', selected }
    }
    if (text) return { question_header: header, answer: text }
    return { question_header: header, answer: picked[0] || '' }
  })
}

function submit() {
  if (!props.tool.askUserId) return
  const answers = buildAnswers()
  submitted.value = answers.map((a) => a.answer || (a.selected || []).join(', '))
  store.submitAskUser(props.tool.askUserId, answers)
}

function skip() {
  if (!props.tool.askUserId) return
  submitted.value = []
  store.submitAskUser(props.tool.askUserId, [])
}

// Submit is allowed once every question has either a selection or free text.
const canSubmit = computed(() =>
  questions.value.every((q, qi) =>
    (selections[qi] && selections[qi].length > 0) || (freeText[qi] || '').trim() !== '',
  ),
)

// ─── Keyboard: digit keys select options for the common single-question case ───
function onKeydown(e: KeyboardEvent) {
  if (!isPending.value || collapsed.value || questions.value.length !== 1) return
  const target = e.target as HTMLElement | null
  if (target && (target.tagName === 'INPUT' || target.tagName === 'TEXTAREA')) return
  const q = questions.value[0]
  if (!q?.options?.length) return
  const n = parseInt(e.key, 10)
  if (!Number.isNaN(n) && n >= 1 && n <= q.options.length) {
    e.preventDefault()
    toggleOption(0, q.options[n - 1]!.label, !!q.multi_select)
  } else if (e.key === 'Enter' && canSubmit.value) {
    e.preventDefault()
    submit()
  }
}
// Only listen while this card is the live, pending one — resolved/replay cards
// (and there can be many in a long session) must not each hold a global listener.
watch(isPending, (pending) => {
  if (pending) window.addEventListener('keydown', onKeydown)
  else window.removeEventListener('keydown', onKeydown)
}, { immediate: true })
onUnmounted(() => window.removeEventListener('keydown', onKeydown))

// ─── Resolved / replay display: extract the chosen answers from tool output ───
const resolvedAnswers = computed<string[]>(() => {
  const out = props.tool.output || ''
  // No output yet (just submitted, awaiting tool_result) → show local choice.
  if (!out) return submitted.value ?? []
  // Backend "no answer" sentinels (skip / cancelled) → render the empty state
  // for the whole card, not as a fake answer mapped onto the first question.
  if (/^The user did not provide (an answer|any answers)\.?$/.test(out.trim())) return []
  if (out.startsWith('ask_user cancelled')) return []
  // Multi-question JSON form: { answers: [{question_header, answer, selected}] }
  try {
    const parsed = JSON.parse(out)
    if (Array.isArray(parsed.answers)) {
      return parsed.answers.map((a: { answer?: string; selected?: string[] }) =>
        a.answer || (a.selected || []).join(', '),
      )
    }
  } catch { /* not JSON */ }
  // Single-question plain form: "User's answer: X"
  const m = out.match(/^User's answer:\s*([\s\S]*)$/)
  if (m) return [m[1]!.trim()]
  return [out.trim()]
})
</script>

<template>
  <div class="my-1.5">
    <!-- Collapsed: one-line summary (matches the inline "Asking …" affordance) -->
    <button
      v-if="collapsed"
      class="w-full flex items-center gap-1.5 pl-0 pr-1 py-1 text-left cursor-pointer hover:opacity-70 transition-opacity"
      style="background: transparent"
      @click="collapsed = false"
    >
      <span class="text-xs font-medium" style="color: var(--color-muted-foreground)">Asking</span>
      <span v-if="questions[0]?.header" class="text-xs font-mono" style="color: var(--color-primary)">{{ questions[0].header }}</span>
      <span class="text-xs font-mono truncate" style="color: var(--color-muted-foreground)">{{ title }}</span>
      <svg class="w-3 h-3 shrink-0" style="color: var(--color-muted-foreground)" viewBox="0 0 20 20" fill="currentColor">
        <path fill-rule="evenodd" d="M5.23 7.21a.75.75 0 011.06.02L10 11.168l3.71-3.938a.75.75 0 111.08 1.04l-4.25 4.5a.75.75 0 01-1.08 0l-4.25-4.5a.75.75 0 01.02-1.06z" clip-rule="evenodd" />
      </svg>
    </button>

    <!-- Expanded card -->
    <div
      v-else
      class="overflow-hidden mr-2"
      :style="{ borderRadius: 'var(--radius-xl)', border: '1px solid var(--color-border)', background: 'var(--color-surface)' }"
    >
      <!-- Header -->
      <div class="flex items-center gap-2 px-3.5 py-2.5" :style="{ borderBottom: '1px solid var(--color-border)' }">
        <span
          class="w-1.5 h-1.5 rounded-full shrink-0"
          :class="{ 'animate-pulse': isPending }"
          :style="{ background: isPending ? 'var(--color-primary)' : 'var(--color-muted-foreground)' }"
        />
        <span class="text-sm font-medium flex-1 min-w-0 truncate" style="color: var(--color-foreground)">{{ title }}</span>
        <button class="shrink-0 cursor-pointer hover:opacity-70" title="Collapse" @click="collapsed = true">
          <svg class="w-3.5 h-3.5 rotate-180" style="color: var(--color-muted-foreground)" viewBox="0 0 20 20" fill="currentColor">
            <path fill-rule="evenodd" d="M5.23 7.21a.75.75 0 011.06.02L10 11.168l3.71-3.938a.75.75 0 111.08 1.04l-4.25 4.5a.75.75 0 01-1.08 0l-4.25-4.5a.75.75 0 01.02-1.06z" clip-rule="evenodd" />
          </svg>
        </button>
        <button v-if="isPending" class="shrink-0 cursor-pointer hover:opacity-70" title="Skip" @click="skip">
          <svg class="w-3.5 h-3.5" style="color: var(--color-muted-foreground)" viewBox="0 0 20 20" fill="currentColor">
            <path d="M6.28 5.22a.75.75 0 00-1.06 1.06L8.94 10l-3.72 3.72a.75.75 0 101.06 1.06L10 11.06l3.72 3.72a.75.75 0 101.06-1.06L11.06 10l3.72-3.72a.75.75 0 00-1.06-1.06L10 8.94 6.28 5.22z" />
          </svg>
        </button>
      </div>

      <!-- ════ Interactive (pending) ════ -->
      <div v-if="isPending" class="px-2.5 py-2">
        <div v-for="(q, qi) in questions" :key="qi" :class="{ 'mt-2 pt-2': qi > 0 }" :style="qi > 0 ? 'border-top: 1px solid var(--color-border)' : ''">
          <!-- Question text (when multiple questions, each gets its own label) -->
          <div v-if="questions.length > 1" class="px-1 pb-1.5 text-xs font-medium" style="color: var(--color-foreground)">{{ q.question }}</div>

          <!-- Options -->
          <button
            v-for="(opt, oi) in q.options"
            :key="oi"
            class="ask-opt w-full flex items-center gap-3 px-3 py-2 text-left cursor-pointer rounded-lg"
            :class="{ 'ask-opt-selected': isSelected(qi, opt.label) }"
            :style="{ border: isSelected(qi, opt.label) ? '1px solid var(--color-primary)' : '1px solid transparent' }"
            @click="toggleOption(qi, opt.label, !!q.multi_select)"
          >
            <span
              v-if="q.multi_select"
              class="w-4 h-4 rounded shrink-0 flex items-center justify-center text-[10px]"
              :style="{
                border: '1.5px solid ' + (isSelected(qi, opt.label) ? 'var(--color-primary)' : 'var(--color-border)'),
                background: isSelected(qi, opt.label) ? 'var(--color-primary)' : 'transparent',
                color: 'white',
              }"
            >{{ isSelected(qi, opt.label) ? '✓' : '' }}</span>
            <div class="flex-1 min-w-0">
              <div class="text-sm" style="color: var(--color-foreground)">{{ opt.label }}</div>
              <div v-if="opt.description" class="text-xs mt-0.5" style="color: var(--color-muted-foreground)">{{ opt.description }}</div>
            </div>
            <span
              class="shrink-0 text-[10px] font-mono tabular-nums w-4 text-center rounded"
              style="color: var(--color-muted-foreground)"
            >{{ oi + 1 }}</span>
          </button>

          <!-- Free-form "Other" input -->
          <div class="mt-1 px-1">
            <div v-if="q.options?.length" class="px-2 pt-1 pb-0.5 text-sm" style="color: var(--color-foreground)">Other</div>
            <input
              v-model="freeText[qi]"
              type="text"
              placeholder="Type your own answer here"
              class="w-full px-2.5 py-1.5 text-sm rounded-lg outline-none"
              :style="{ background: 'var(--color-background)', border: '1px solid var(--color-border)', color: 'var(--color-foreground)' }"
              @input="onFreeTextInput(qi, !!q.multi_select)"
              @keydown.enter.prevent="canSubmit && submit()"
            />
          </div>
        </div>

        <!-- Footer -->
        <div class="flex items-center justify-end gap-2 px-1 pt-2.5 pb-0.5">
          <button
            class="px-3 py-1.5 text-xs rounded-md cursor-pointer transition-colors"
            style="color: var(--color-muted-foreground)"
            @click="skip"
          >Skip</button>
          <button
            class="px-3.5 py-1.5 text-xs rounded-md font-medium transition-opacity flex items-center gap-1.5"
            :class="canSubmit ? 'cursor-pointer' : 'cursor-not-allowed opacity-40'"
            :style="{ background: 'var(--color-primary)', color: 'white' }"
            :disabled="!canSubmit"
            @click="submit"
          >
            <span>Submit</span>
            <span class="text-[10px] opacity-80">↵</span>
          </button>
        </div>
      </div>

      <!-- ════ Resolved / replay ════ -->
      <div v-else class="px-3.5 py-2.5 space-y-2">
        <template v-if="resolvedAnswers.length">
          <div v-for="(q, qi) in questions" :key="qi">
            <div v-if="questions.length > 1" class="text-xs" style="color: var(--color-muted-foreground)">{{ q.question }}</div>
            <div class="text-sm flex items-baseline gap-1.5">
              <span class="shrink-0 text-[10px]" style="color: var(--color-primary)">▸</span>
              <span style="color: var(--color-foreground)">{{ resolvedAnswers[qi] || '—' }}</span>
            </div>
          </div>
        </template>
        <div v-else class="text-xs italic" style="color: var(--color-muted-foreground)">No answer recorded</div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.ask-opt {
  background: transparent;
  transition: background 0.12s ease;
}
.ask-opt:hover {
  background: var(--color-muted);
}
.ask-opt-selected,
.ask-opt-selected:hover {
  background: color-mix(in srgb, var(--color-primary), transparent 88%);
}
</style>
