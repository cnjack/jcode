<script setup lang="ts">
import { ref, computed } from 'vue'
import {
  ShieldCheckIcon,
  ShieldExclamationIcon,
  CommandLineIcon,
  PencilSquareIcon,
  DocumentTextIcon,
  ExclamationTriangleIcon,
  ChevronDownIcon,
} from '@heroicons/vue/24/outline'
import { useI18n } from 'vue-i18n'
import type { PendingApproval } from '@/types/api'
import { useChatStore } from '@/stores/chat'

const props = defineProps<{
  approval: PendingApproval
}>()

const store = useChatStore()
const { t } = useI18n()
const showArgs = ref(false)
// Two-step confirm for the high-stakes "Allow all" action (auto-approves the
// rest of the session). First click arms it; a second click on the now-red
// button actually submits. Auto-resets if the approval resolves meanwhile.
const armingAllowAll = ref(false)

function formatArgs(args: string): string {
  try {
    return JSON.stringify(JSON.parse(args), null, 2).slice(0, 300)
  } catch {
    return args.slice(0, 300)
  }
}

// Tool identity — icon + verb + the primary target (the path or command), so
// the card reads WHAT before WHY before WHAT TO DO. The verb and icon are
// keyed off the tool name; the target is pulled from the parsed args so it
// shows inline without a click.
interface ToolIdentity { icon: typeof CommandLineIcon; verb: string }
const toolIdentity = computed<ToolIdentity>(() => {
  switch (props.approval.tool_name) {
    case 'execute':   return { icon: CommandLineIcon,    verb: t('approval.runCommand') }
    case 'write':     return { icon: DocumentTextIcon,   verb: t('approval.createFile') }
    case 'edit':
    case 'multi_edit':return { icon: PencilSquareIcon,   verb: t('approval.editFile') }
    default:          return { icon: PencilSquareIcon,   verb: t('approval.toolAction', { name: props.approval.tool_name }) }
  }
})

// The one argument a user actually judges — the path for file tools, the
// command for execute. Pulled from tool_args so it shows inline by default;
// the full payload stays behind the "show args" toggle.
const primaryTarget = computed<string | null>(() => {
  try {
    const parsed = JSON.parse(props.approval.tool_args)
    if (typeof parsed === 'object' && parsed) {
      if (typeof parsed.command === 'string') return parsed.command
      if (typeof parsed.path === 'string') return parsed.path
      if (typeof parsed.file_path === 'string') return parsed.file_path
    }
  } catch { /* not JSON */ }
  return null
})
</script>

<template>
  <!-- ─── RESOLVED — dissolves into the borderless tool-row stream ─── -->
  <div
    v-if="approval.resolved"
    class="pl-9 animate-fade-up"
  >
    <div class="flex items-center gap-1.5 py-1">
      <!-- Shield glyph carries the resolved state (success / destructive) -->
      <ShieldCheckIcon
        v-if="approval.approved"
        class="w-3.5 h-3.5 shrink-0"
        style="color: var(--color-success-fg)"
        aria-hidden="true"
      />
      <ShieldExclamationIcon
        v-else
        class="w-3.5 h-3.5 shrink-0"
        style="color: var(--color-destructive)"
        aria-hidden="true"
      />
      <span class="text-xs font-mono truncate min-w-0" style="color: var(--color-muted-foreground)">{{ approval.tool_name }}</span>
      <span
        class="text-[10px] shrink-0"
        :style="{ color: approval.approved ? 'var(--color-success-fg)' : 'var(--color-error-fg)' }"
      >{{ approval.approved ? t('approval.allowed') : t('approval.denied') }}</span>
      <span v-if="approval.is_external" class="text-[10px] shrink-0" style="color: var(--color-muted-foreground)">{{ t('approval.external') }}</span>
    </div>
  </div>

  <!-- ─── PENDING — a decision card: surface + 1px border + 3px left accent.
       Leads with the tool identity (icon tile + verb), shows the primary
       target inline, and a strict three-tier button ramp. ─── -->
  <div
    v-else
    class="ap-card relative my-2 overflow-hidden animate-fade-up"
  >
    <!-- Left accent bar — the one chromatic signal, warning-tinted. -->
    <span
      class="approval-accent absolute left-0 top-0 bottom-0"
      style="width: 3px; background: var(--color-warning-fg)"
      aria-hidden="true"
    />

    <div class="ap-main">
      <!-- Tool identity tile — same squircle primitive as the model mark and
           the mode tile, warning-tinted because every approval is a pause. -->
      <div class="ap-tool">
        <component :is="toolIdentity.icon" class="w-4 h-4" aria-hidden="true" />
      </div>

      <div class="ap-body">
        <!-- Header: verb + external chip -->
        <div class="ap-head">
          <span class="ap-verb">{{ toolIdentity.verb }}</span>
          <span
            v-if="approval.is_external"
            class="ap-chip"
          >
            <ExclamationTriangleIcon class="ap-chip-icon" aria-hidden="true" />
            {{ t('approval.externalPath') }}
          </span>
        </div>

        <!-- Primary target — the path/command, shown inline by default. -->
        <div v-if="primaryTarget" class="ap-target">{{ primaryTarget }}</div>
        <div v-else class="ap-target ap-target-mono">{{ approval.tool_name }}</div>

        <!-- Full payload — quiet toggle, collapsed by default. -->
        <button
          v-if="approval.tool_args && approval.tool_args !== '{}'"
          type="button"
          class="ap-args-toggle"
          :aria-expanded="showArgs"
          :aria-label="t('approval.toggleArgs')"
          @click="showArgs = !showArgs"
        >
          <ChevronDownIcon
            class="ap-args-chev"
            :class="{ 'ap-args-chev-open': showArgs }"
            aria-hidden="true"
          />
          {{ showArgs ? t('approval.hideArgs') : t('approval.showArgs') }}
        </button>
        <pre
          v-if="showArgs"
          class="ap-args"
        >{{ formatArgs(approval.tool_args) }}</pre>
      </div>

      <!-- Buttons: clear 3-tier hierarchy (primary / outlined / ghost). All
           disabled while a decision POST is in flight to prevent double-submit. -->
      <div class="ap-actions" :class="{ 'approval-busy': approval.resolving }">
        <button
          type="button"
          class="ap-btn ap-btn-yes"
          :disabled="approval.resolving"
          @click="store.resolveApproval(approval.id, true, false)"
        >
          {{ t('approval.allowOnce') }}
        </button>
        <button
          type="button"
          class="ap-btn ap-btn-all"
          :class="{ armed: armingAllowAll }"
          :title="armingAllowAll ? t('approval.confirmHint') : t('approval.allowAllTitle')"
          :disabled="approval.resolving"
          @click="armingAllowAll
            ? store.resolveApproval(approval.id, true, true)
            : (armingAllowAll = true)"
          @blur="armingAllowAll = false"
        >
          {{ armingAllowAll ? t('approval.confirm') : t('approval.allowAll') }}
        </button>
        <button
          type="button"
          class="ap-btn ap-btn-no"
          :disabled="approval.resolving"
          @click="store.resolveApproval(approval.id, false)"
        >
          {{ t('approval.deny') }}
        </button>
      </div>
    </div>
  </div>
</template>

<style scoped>
.ap-card {
  background: var(--color-surface);
  border: 1px solid var(--color-border);
  border-radius: var(--radius-lg);
  box-shadow: var(--shadow-sm);
}

/* Subtle attention pulse on the accent bar — opacity-only, no glow flood. */
@keyframes accent-breathe {
  0%, 100% { opacity: 0.55; }
  50% { opacity: 1; }
}
.approval-accent {
  animation: accent-breathe 2.4s ease-in-out infinite;
}

.ap-main {
  display: flex;
  align-items: stretch;
  gap: 14px;
  padding: 13px 14px 13px 17px;
}

/* Tool identity tile — warning-tinted squircle, the same shape used for the
   provider mark and the mode tile, so "an icon in a soft squircle" is the
   project's one identity primitive. */
.ap-tool {
  width: 34px;
  height: 34px;
  flex-shrink: 0;
  display: grid;
  place-items: center;
  border-radius: var(--radius-md);
  background: var(--color-warning-bg);
  color: var(--color-warning-fg);
  align-self: flex-start;
}

.ap-body {
  flex: 1;
  min-width: 0;
}
.ap-head {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 2px;
}
.ap-verb {
  font-size: 12.5px;
  font-weight: 600;
  color: var(--color-foreground);
  letter-spacing: -0.005em;
}
.ap-chip {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  height: 17px;
  padding: 0 7px;
  border-radius: var(--radius-pill);
  font-size: 10px;
  font-weight: 500;
  background: var(--color-warning-bg);
  color: var(--color-warning-fg);
  white-space: nowrap;
}
.ap-chip-icon { width: 11px; height: 11px; }

/* The primary argument — the part you actually judge. Always visible. */
.ap-target {
  font-family: var(--font-mono);
  font-size: 12px;
  color: var(--color-foreground);
  margin-top: 4px;
  line-height: 1.4;
  word-break: break-all;
}
.ap-target-mono { color: var(--color-muted-foreground); }

.ap-args-toggle {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  margin-top: 8px;
  border: none;
  background: transparent;
  color: var(--color-muted-foreground);
  font: inherit;
  font-size: 11px;
  cursor: pointer;
  padding: 0;
}
.ap-args-toggle:hover { color: var(--color-foreground); }
.ap-args-chev {
  width: 11px;
  height: 11px;
  transition: transform var(--duration-fast);
}
.ap-args-chev-open { transform: rotate(180deg); }

.ap-args {
  margin: 8px 0 0;
  padding: 8px 10px;
  background: var(--color-muted);
  border: 1px solid var(--color-border);
  border-radius: var(--radius-md);
  font-family: var(--font-mono);
  font-size: 10.5px;
  line-height: 1.55;
  color: var(--color-muted-foreground);
  max-height: 96px;
  overflow-y: auto;
  white-space: pre-wrap;
}

/* Button ramp — three distinct roles, equal height. */
.ap-actions {
  display: flex;
  align-items: center;
  gap: 6px;
  flex-shrink: 0;
  align-self: flex-end;
}
.ap-btn {
  height: 30px;
  padding: 0 13px;
  border-radius: var(--radius-md);
  font: inherit;
  font-size: 12px;
  font-weight: 500;
  cursor: pointer;
  transition: background var(--duration-fast), color var(--duration-fast), border-color var(--duration-fast);
  white-space: nowrap;
  border: 1px solid transparent;
}

/* Allow once — the primary, safe yes. Accent-neutral fill (orange stays
   reserved for the send/hero token); calm, deliberate confirm. */
.ap-btn-yes {
  background: var(--color-accent-neutral);
  color: var(--color-surface);
  box-shadow: var(--shadow-sm);
}
.ap-btn-yes:hover:not(:disabled) {
  background: color-mix(in srgb, var(--color-accent-neutral) 88%, var(--color-background));
}

/* Allow all — outlined neutral; arms to destructive on click. */
.ap-btn-all {
  background: transparent;
  color: var(--color-foreground);
  border-color: var(--color-border);
}
.ap-btn-all:hover:not(:disabled) { background: var(--color-muted); }
.ap-btn-all.armed {
  background: var(--color-destructive);
  color: var(--color-on-destructive);
  border-color: transparent;
}

/* Deny — ghost; leans red only on hover, never at rest. */
.ap-btn-no {
  background: transparent;
  color: var(--color-muted-foreground);
}
.ap-btn-no:hover:not(:disabled) {
  background: var(--color-error-bg);
  color: var(--color-error-fg);
}

/* In-flight: dim the button row and block further clicks. */
.approval-busy {
  opacity: 0.6;
}
.approval-busy .ap-btn {
  cursor: progress;
}

@media (prefers-reduced-motion: reduce) {
  .approval-accent { animation: none !important; }
}
</style>
