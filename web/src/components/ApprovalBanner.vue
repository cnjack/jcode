<script setup lang="ts">
import { ref } from 'vue'
import type { PendingApproval } from '@/types/api'
import { useChatStore } from '@/stores/chat'

defineProps<{
  approval: PendingApproval
}>()

const store = useChatStore()
const showArgs = ref(false)

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
      <svg
        class="w-3.5 h-3.5 shrink-0"
        :style="{ color: approval.approved ? 'var(--color-success-fg)' : 'var(--color-destructive)' }"
        viewBox="0 0 20 20" fill="currentColor" aria-hidden="true"
      >
        <path v-if="approval.approved" fill-rule="evenodd" d="M10 1.5l6 2.2v4.8c0 4-2.6 7.6-6 8.5-3.4-.9-6-4.5-6-8.5V3.7l6-2.2zm2.78 6.16a.75.75 0 00-1.06-1.06L9 9.32 8.28 8.6a.75.75 0 10-1.06 1.06l1.25 1.25a.75.75 0 001.06 0l3.25-3.25z" clip-rule="evenodd" />
        <path v-else fill-rule="evenodd" d="M10 1.5l6 2.2v4.8c0 4-2.6 7.6-6 8.5-3.4-.9-6-4.5-6-8.5V3.7l6-2.2zM8.28 7.22a.75.75 0 00-1.06 1.06L8.94 10l-1.72 1.72a.75.75 0 101.06 1.06L10 11.06l1.72 1.72a.75.75 0 101.06-1.06L11.06 10l1.72-1.72a.75.75 0 00-1.06-1.06L10 8.94 8.28 7.22z" clip-rule="evenodd" />
      </svg>
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
          <svg
            class="w-3.5 h-3.5 shrink-0"
            style="color: var(--color-warning-fg)"
            viewBox="0 0 20 20" fill="currentColor" aria-hidden="true"
          >
            <path fill-rule="evenodd" d="M10 1.5l6 2.2v4.8c0 4-2.6 7.6-6 8.5-3.4-.9-6-4.5-6-8.5V3.7l6-2.2zM9 9V6.5a1 1 0 112 0V9h.5a.75.75 0 01.75.75v3a.75.75 0 01-.75.75h-3a.75.75 0 01-.75-.75v-3A.75.75 0 018.5 9H9z" clip-rule="evenodd" />
          </svg>
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
            <svg
              class="w-3.5 h-3.5 transition-transform"
              :class="{ 'rotate-180': showArgs }"
              style="color: var(--color-muted-foreground)"
              viewBox="0 0 20 20" fill="currentColor"
            >
              <path fill-rule="evenodd" d="M5.23 7.21a.75.75 0 011.06.02L10 11.168l3.71-3.938a.75.75 0 111.08 1.04l-4.25 4.5a.75.75 0 01-1.08 0l-4.25-4.5a.75.75 0 01.02-1.06z" clip-rule="evenodd" />
            </svg>
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
          class="px-3.5 py-1.5 text-xs rounded-md text-white transition-colors cursor-pointer font-medium shadow-sm"
          style="background-color: var(--color-primary)"
          :disabled="approval.resolving"
          @click="store.resolveApproval(approval.id, true, false)"
        >
          Allow once
        </button>
        <button
          type="button"
          class="px-3.5 py-1.5 text-xs rounded-md transition-colors cursor-pointer font-medium"
          style="background-color: transparent; color: var(--color-foreground); border: 1px solid var(--color-border)"
          title="Approve this and auto-approve the rest of the session"
          :disabled="approval.resolving"
          @click="store.resolveApproval(approval.id, true, true)"
        >
          Allow all
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
