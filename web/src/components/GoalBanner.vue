<script setup lang="ts">
import { computed } from 'vue'
import { BoltIcon } from '@heroicons/vue/24/outline'
import { useI18n } from 'vue-i18n'
import { useChatStore } from '@/stores/chat'

const store = useChatStore()
const { t } = useI18n()

const goal = computed(() => store.goal)

const statusColor = computed(() => {
  switch (goal.value?.status) {
    case 'complete':
      return 'var(--color-success-fg)'
    case 'blocked':
      return 'var(--color-destructive)'
    default:
      return 'var(--color-accent-neutral)'
  }
})

// Human-readable status — maps the GoalStatus enum ('active' | 'complete' |
// 'blocked') to localized labels. Falls back to a de-underscored form for any
// unmapped value.
const statusLabel = computed(() => {
  const s = goal.value?.status || ''
  switch (s) {
    case 'active':
      return t('goal.status.active')
    case 'complete':
      return t('goal.status.completed')
    case 'blocked':
      return t('goal.status.blocked')
    default:
      return s.replace(/_/g, ' ')
  }
})

const tokensLabel = computed(() => {
  if (!goal.value) return ''
  const used = goal.value.tokens_used ?? 0
  if (used <= 0) return ''
  return used < 1000
    ? t('goal.tokens', { used })
    : t('goal.tokensK', { k: (used / 1000).toFixed(1) })
})
</script>

<template>
  <!-- Active goal display. Goals are set from the prompt box (Goal toggle
       in the input toolbar, or the /goal command). -->
  <div
    v-if="goal"
    class="mx-3 mt-2 rounded-md border px-3 py-2 flex items-start gap-2"
    :style="{ borderColor: 'var(--color-border)', backgroundColor: 'var(--color-secondary)' }"
  >
    <BoltIcon class="w-3.5 h-3.5 shrink-0 mt-0.5" :style="{ color: statusColor }" />
    <div class="flex-1 min-w-0">
      <div class="flex items-center gap-2">
        <span class="text-[10px] uppercase tracking-wide font-semibold" :style="{ color: statusColor }">
          {{ statusLabel }}
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
      class="goal-clear text-[10px] px-2 py-1 rounded shrink-0 cursor-pointer"
      style="background-color: var(--color-background); color: var(--color-muted-foreground)"
      :title="t('goal.clearGoal')"
      @click="store.clearGoal()"
    >
      {{ t('goal.clear') }}
    </button>
  </div>
</template>

<style scoped>
.goal-clear {
  transition: background-color var(--duration-fast) var(--ease-out),
              color var(--duration-fast) var(--ease-out);
}
.goal-clear:hover {
  background-color: var(--color-muted);
  color: var(--color-foreground);
}
</style>
