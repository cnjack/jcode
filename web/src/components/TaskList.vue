<script setup lang="ts">
import { ref, onMounted, onBeforeUnmount } from 'vue'
import { Circle, LoaderCircle, CircleCheck, Ban, CircleDot } from 'lucide-vue-next'
import type { TodoItem } from '@/types/api'

defineProps<{
  todos: TodoItem[]
}>()

// Respect prefers-reduced-motion: swap the spinning LoaderCircle for a
// static CircleDot so the spin actually stops (not just visually paused).
const reduceMotion = ref(false)
let mql: MediaQueryList | null = null
function syncMotion(e?: MediaQueryListEvent) {
  reduceMotion.value = e ? e.matches : (mql?.matches ?? false)
}
onMounted(() => {
  if (typeof window !== 'undefined' && window.matchMedia) {
    mql = window.matchMedia('(prefers-reduced-motion: reduce)')
    reduceMotion.value = mql.matches
    mql.addEventListener('change', syncMotion)
  }
})
onBeforeUnmount(() => {
  mql?.removeEventListener('change', syncMotion)
})
</script>

<template>
  <div class="space-y-0.5">
    <div
      v-for="todo in todos"
      :key="todo.id"
      class="relative flex items-center gap-2 py-1 pl-2 pr-1.5"
      :style="todo.status === 'in_progress'
        ? { backgroundColor: 'var(--color-warning-bg)', borderRadius: 'var(--radius-md)' }
        : undefined"
    >
      <!-- 2px left accent bar for the active task (inner span, not a single-sided border) -->
      <span
        v-if="todo.status === 'in_progress'"
        class="absolute left-0 top-0.5 bottom-0.5 w-0.5"
        style="background-color: var(--color-primary); border-radius: var(--radius-pill)"
        aria-hidden="true"
      />

      <!-- Status icon -->
      <CircleCheck
        v-if="todo.status === 'completed'"
        class="w-3.5 h-3.5 shrink-0"
        style="color: var(--color-success-fg)"
      />
      <Ban
        v-else-if="todo.status === 'cancelled'"
        class="w-3.5 h-3.5 shrink-0"
        style="color: var(--color-destructive)"
      />
      <template v-else-if="todo.status === 'in_progress'">
        <CircleDot
          v-if="reduceMotion"
          class="w-3.5 h-3.5 shrink-0"
          style="color: var(--color-primary)"
        />
        <LoaderCircle
          v-else
          class="w-3.5 h-3.5 shrink-0 animate-spin"
          style="color: var(--color-primary)"
        />
      </template>
      <Circle
        v-else
        class="w-3.5 h-3.5 shrink-0"
        style="color: var(--color-muted-foreground)"
      />

      <!-- Title -->
      <span
        class="text-xs sm:text-sm flex-1 min-w-0 truncate"
        :class="{ 'font-medium': todo.status === 'in_progress' }"
        :style="{
          color: (todo.status === 'completed' || todo.status === 'cancelled')
            ? 'var(--color-muted-foreground)'
            : 'var(--color-foreground)',
          textDecoration: (todo.status === 'completed' || todo.status === 'cancelled')
            ? 'line-through'
            : 'none',
        }"
      >{{ todo.title }}</span>
    </div>
  </div>
</template>

<style scoped>
/* Belt-and-suspenders: even if a spinner slips through, halt it for
   users who prefer reduced motion. */
@media (prefers-reduced-motion: reduce) {
  .animate-spin {
    animation: none;
  }
}
</style>
