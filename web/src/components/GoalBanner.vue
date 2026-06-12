<script setup lang="ts">
import { computed } from 'vue'
import { useChatStore } from '@/stores/chat'

const store = useChatStore()

const goal = computed(() => store.goal)

const statusColor = computed(() => {
  switch (goal.value?.status) {
    case 'complete':
      return 'var(--color-success-fg)'
    case 'blocked':
      return 'var(--color-destructive)'
    default:
      return 'var(--color-primary)'
  }
})

const tokensLabel = computed(() => {
  if (!goal.value) return ''
  const used = goal.value.tokens_used ?? 0
  if (used <= 0) return ''
  return used < 1000 ? `${used} tokens` : `${(used / 1000).toFixed(1)}k tokens`
})
</script>

<template>
  <!-- Active goal display. Goals are set from the prompt box (🎯 Goal toggle
       in the input toolbar, or the /goal command). -->
  <div
    v-if="goal"
    class="mx-3 mt-2 rounded-md border px-3 py-2 flex items-start gap-2"
    :style="{ borderColor: 'var(--color-border)', backgroundColor: 'var(--color-secondary)' }"
  >
    <span class="text-base leading-none" title="Session goal">🎯</span>
    <div class="flex-1 min-w-0">
      <div class="flex items-center gap-2">
        <span class="text-[10px] uppercase tracking-wide font-semibold" :style="{ color: statusColor }">
          {{ goal.status }}
        </span>
        <span v-if="tokensLabel" class="text-[10px]" style="color: var(--color-muted-foreground)">
          {{ tokensLabel }}
        </span>
      </div>
      <div class="text-xs mt-0.5 break-words" style="color: var(--color-foreground)">
        {{ goal.objective }}
      </div>
    </div>
    <button
      class="text-[10px] px-2 py-1 rounded shrink-0 cursor-pointer"
      style="background-color: var(--color-background); color: var(--color-muted-foreground)"
      title="Clear goal"
      @click="store.clearGoal()"
    >
      Clear
    </button>
  </div>
</template>
