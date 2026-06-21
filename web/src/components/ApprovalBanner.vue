<script setup lang="ts">
import { ref } from 'vue'
import {
  ShieldCheckIcon,
  ShieldExclamationIcon,
  LockClosedIcon,
  ChevronDownIcon,
} from '@heroicons/vue/24/outline'
import type { PendingApproval } from '@/types/api'
import { useChatStore } from '@/stores/chat'

defineProps<{
  approval: PendingApproval
}>()

const store = useChatStore()
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
      >{{ approval.approved ? 'Allowed' : 'Denied' }}</span>
      <span v-if="approval.is_external" class="text-[10px] shrink-0" style="color: var(--color-muted-foreground)">· external</span>
    </div>
  </div>

  <!-- ─── PENDING — a real decision card: surface + 1px border + 3px breathing left accent bar ─── -->
  <div
    v-else
    class="relative my-2 overflow-hidden animate-fade-up"
    :style="{
      borderRadius: 'var(--radius-lg)',
      background: 'var(--color-surface)',
      border: '1px solid var(--color-border)',
      boxShadow: 'var(--shadow-sm)',
    }"
  >
    <!-- Left accent bar — the only chromatic signal, replacing the old full flood -->
    <span
      class="approval-accent absolute left-0 top-0 bottom-0"
      style="width: 3px; background: var(--color-warning-fg)"
      aria-hidden="true"
    />

    <div class="flex items-start gap-3 pl-4 pr-3 py-3">
      <div class="flex-1 min-w-0">
        <!-- Header: shield-lock glyph + label + external chip + args toggle -->
        <div class="flex items-center gap-1.5 mb-1.5">
          <LockClosedIcon
            class="w-3.5 h-3.5 shrink-0"
            style="color: var(--color-warning-fg)"
            aria-hidden="true"
          />
          <span class="text-xs font-medium" style="color: var(--color-foreground)">Approval needed</span>
          <span
            v-if="approval.is_external"
            class="ml-1 px-1.5 py-0.5 text-[10px] font-medium shrink-0"
            :style="{
              borderRadius: 'var(--radius-pill)',
              background: 'color-mix(in srgb, var(--color-warning-fg) 14%, transparent)',
              color: 'var(--color-warning-fg)',
            }"
          >external path</span>
          <button
            type="button"
            class="ml-auto shrink-0 inline-flex items-center cursor-pointer hover:opacity-70 transition-opacity"
            :aria-expanded="showArgs"
            aria-label="Toggle arguments"
            @click="showArgs = !showArgs"
          >
            <ChevronDownIcon
              class="w-3.5 h-3.5 transition-transform"
              :class="{ 'rotate-180': showArgs }"
              style="color: var(--color-muted-foreground)"
            />
          </button>
        </div>

        <!-- Tool name -->
        <div class="font-mono text-xs truncate" style="color: var(--color-foreground)">{{ approval.tool_name }}</div>

        <!-- Collapsible args (collapsed by default; keeps the resting height tight) -->
        <pre
          v-if="showArgs"
          class="text-[10px] mt-1.5 max-h-32 overflow-auto whitespace-pre-wrap font-mono leading-relaxed"
          style="color: var(--color-muted-foreground)"
        >{{ formatArgs(approval.tool_args) }}</pre>
      </div>

      <!-- Buttons: clear 3-tier hierarchy (primary / ghost / quiet). All disabled
           while a decision POST is in flight to prevent double-submit. -->
      <div class="flex gap-1.5 shrink-0 self-end" :class="{ 'approval-busy': approval.resolving }">
        <button
          type="button"
          class="px-3.5 py-1.5 text-xs rounded-md transition-colors cursor-pointer font-medium shadow-sm"
          style="background-color: var(--color-primary); color: var(--color-on-primary)"
          :disabled="approval.resolving"
          @click="store.resolveApproval(approval.id, true, false)"
        >
          Allow once
        </button>
        <button
          type="button"
          class="px-3.5 py-1.5 text-xs rounded-md transition-colors cursor-pointer font-medium"
          :style="armingAllowAll
            ? { backgroundColor: 'var(--color-destructive)', color: 'var(--color-on-destructive)' }
            : { backgroundColor: 'transparent', color: 'var(--color-foreground)', border: '1px solid var(--color-border)' }"
          :title="armingAllowAll ? 'Click again to auto-approve the rest of the session' : 'Approve this and auto-approve the rest of the session'"
          :disabled="approval.resolving"
          @click="armingAllowAll
            ? store.resolveApproval(approval.id, true, true)
            : (armingAllowAll = true)"
          @blur="armingAllowAll = false"
        >
          {{ armingAllowAll ? 'Confirm?' : 'Allow all' }}
        </button>
        <button
          type="button"
          class="approval-deny px-3 py-1.5 text-xs rounded-md transition-colors cursor-pointer font-medium"
          style="background-color: transparent; color: var(--color-muted-foreground)"
          :disabled="approval.resolving"
          @click="store.resolveApproval(approval.id, false)"
        >
          Deny
        </button>
      </div>
    </div>
  </div>
</template>

<style scoped>
/* Subtle attention pulse on the accent bar — opacity-only, no glow flood. */
@keyframes accent-breathe {
  0%, 100% { opacity: 0.55; }
  50% { opacity: 1; }
}
.approval-accent {
  animation: accent-breathe 2.4s ease-in-out infinite;
}

/* Deny stays quiet until hovered, then leans toward destructive —
   available, never equated with approval. */
.approval-deny:hover {
  color: var(--color-destructive) !important;
  background-color: var(--color-muted) !important;
}

/* In-flight: dim the button row and block further clicks. */
.approval-busy {
  opacity: 0.6;
}
.approval-busy button {
  cursor: progress;
}

@media (prefers-reduced-motion: reduce) {
  .approval-accent { animation: none !important; }
}
</style>
