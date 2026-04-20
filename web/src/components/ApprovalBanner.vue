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
    :class="approval.resolved
      ? approval.approved
        ? 'border-emerald-200 dark:border-emerald-500/20 bg-emerald-50/50 dark:bg-emerald-500/5'
        : 'border-red-200 dark:border-red-500/20 bg-red-50/50 dark:bg-red-500/5'
      : 'border-amber-200 dark:border-amber-500/20 bg-amber-50/50 dark:bg-amber-500/5'">
    <div class="flex-1 min-w-0">
      <div class="text-xs text-zinc-500 dark:text-zinc-400 mb-1 font-medium">Approval required</div>
      <div class="font-mono text-xs text-zinc-700 dark:text-zinc-200">{{ approval.tool_name }}</div>
      <pre class="text-[10px] text-zinc-400 dark:text-zinc-500 mt-1 max-h-12 overflow-hidden whitespace-pre-wrap font-mono">{{ formatArgs(approval.tool_args) }}</pre>
      <div v-if="approval.is_external" class="text-[10px] text-amber-600 dark:text-amber-400 mt-1 font-medium">External path</div>
    </div>
    <div v-if="!approval.resolved" class="flex gap-1.5 shrink-0">
      <button
        class="px-3.5 py-1.5 text-xs rounded-md bg-emerald-500 hover:bg-emerald-600 text-white transition-colors cursor-pointer font-medium shadow-sm"
        @click="store.resolveApproval(approval.id, true)">
        Allow
      </button>
      <button
        class="px-3.5 py-1.5 text-xs rounded-md bg-zinc-200 dark:bg-zinc-700 hover:bg-zinc-300 dark:hover:bg-zinc-600 text-zinc-600 dark:text-zinc-300 transition-colors cursor-pointer font-medium"
        @click="store.resolveApproval(approval.id, false)">
        Deny
      </button>
    </div>
    <span v-else class="text-xs shrink-0 font-medium"
      :class="approval.approved ? 'text-emerald-600 dark:text-emerald-400' : 'text-red-500 dark:text-red-400'">
      {{ approval.approved ? 'Allowed' : 'Denied' }}
    </span>
  </div>
</template>
