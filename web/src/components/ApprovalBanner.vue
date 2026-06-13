<script setup lang="ts">
import type { PendingApproval } from '@/types/api'
import { useChatStore } from '@/stores/chat'

defineProps<{
  approval: PendingApproval
}>()

const store = useChatStore()

function formatArgs(args: string): string {
  try {
    return JSON.stringify(JSON.parse(args), null, 2).slice(0, 300)
  } catch {
    return args.slice(0, 300)
  }
}
</script>

<template>
  <div class="my-2 rounded-md border px-4 py-3 flex items-start gap-3 animate-fade-in"
    :style="{
      borderColor: approval.resolved
        ? approval.approved ? 'var(--color-success-bg)' : 'var(--color-error-bg)'
        : 'var(--color-warning-bg)',
      backgroundColor: approval.resolved
        ? approval.approved ? 'var(--color-success-bg)' : 'var(--color-error-bg)'
        : 'var(--color-warning-bg)',
    }">
    <div class="flex-1 min-w-0">
      <div class="text-xs mb-1 font-medium" style="color: var(--color-muted-foreground)">Approval required</div>
      <div class="font-mono text-xs" style="color: var(--color-foreground)">{{ approval.tool_name }}</div>
      <pre class="text-[10px] mt-1 max-h-12 overflow-hidden whitespace-pre-wrap font-mono" style="color: var(--color-muted-foreground)">{{ formatArgs(approval.tool_args) }}</pre>
      <div v-if="approval.is_external" class="text-[10px] mt-1 font-medium" style="color: var(--color-warning-fg)">External path</div>
    </div>
    <div v-if="!approval.resolved" class="flex gap-1.5 shrink-0">
      <button
        class="px-3.5 py-1.5 text-xs rounded-md text-white transition-colors cursor-pointer font-medium shadow-sm"
        style="background-color: var(--color-primary)"
        @click="store.resolveApproval(approval.id, true, false)">
        Allow once
      </button>
      <button
        class="px-3.5 py-1.5 text-xs rounded-md transition-colors cursor-pointer font-medium"
        style="background-color: var(--color-secondary); color: var(--color-foreground)"
        title="Approve this and auto-approve the rest of the session"
        @click="store.resolveApproval(approval.id, true, true)">
        Allow all
      </button>
      <button
        class="px-3.5 py-1.5 text-xs rounded-md transition-colors cursor-pointer font-medium"
        style="background-color: var(--color-secondary); color: var(--color-muted-foreground)"
        @click="store.resolveApproval(approval.id, false)">
        Deny
      </button>
    </div>
    <span v-else class="text-xs shrink-0 font-medium"
      :style="{ color: approval.approved ? 'var(--color-success-fg)' : 'var(--color-destructive)' }">
      {{ approval.approved ? 'Allowed' : 'Denied' }}
    </span>
  </div>
</template>
