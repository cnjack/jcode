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
  <div class="my-2 rounded-lg border px-4 py-3 flex items-start gap-3"
    :class="approval.resolved
      ? approval.approved ? 'border-teal-200 bg-teal-50/50' : 'border-red-200 bg-red-50/50'
      : 'border-amber-200 bg-amber-50/50'">
    <div class="flex-1 min-w-0">
      <div class="text-xs text-stone-500 mb-1">Approval required</div>
      <div class="font-mono text-xs text-stone-700">{{ approval.tool_name }}</div>
      <pre class="text-[10px] text-stone-400 mt-1 max-h-12 overflow-hidden whitespace-pre-wrap">{{ formatArgs(approval.tool_args) }}</pre>
      <div v-if="approval.is_external" class="text-[10px] text-amber-600 mt-1">External path</div>
    </div>
    <div v-if="!approval.resolved" class="flex gap-1.5 shrink-0">
      <button
        class="px-3 py-1 text-xs rounded-md bg-teal-500 hover:bg-teal-600 text-white transition-colors cursor-pointer"
        @click="store.resolveApproval(approval.id, true)">
        Allow
      </button>
      <button
        class="px-3 py-1 text-xs rounded-md bg-stone-200 hover:bg-stone-300 text-stone-600 transition-colors cursor-pointer"
        @click="store.resolveApproval(approval.id, false)">
        Deny
      </button>
    </div>
    <span v-else class="text-xs shrink-0"
      :class="approval.approved ? 'text-teal-600' : 'text-red-500'">
      {{ approval.approved ? 'Allowed' : 'Denied' }}
    </span>
  </div>
</template>
