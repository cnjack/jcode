<script setup lang="ts">
import { useChatStore } from '@/stores/chat'

const store = useChatStore()
</script>

<template>
  <div v-if="store.todos.length > 0" class="border-t border-zinc-200 dark:border-zinc-800/80 bg-zinc-50/80 dark:bg-zinc-900/60 backdrop-blur-sm px-5 py-2.5 max-h-40 overflow-y-auto">
    <div class="max-w-3xl mx-auto">
      <div class="text-[10px] text-zinc-400 dark:text-zinc-500 uppercase tracking-wider mb-1.5 font-semibold">Tasks</div>
      <div v-for="todo in store.todos" :key="todo.id" class="flex items-center gap-2 py-0.5">
        <span class="text-[10px] shrink-0"
          :class="{
            'text-emerald-600 dark:text-emerald-400': todo.status === 'completed',
            'text-amber-500 dark:text-amber-400': todo.status === 'in_progress',
            'text-zinc-400 dark:text-zinc-600': todo.status === 'pending' || todo.status === 'cancelled',
          }">
          {{ todo.status === 'completed' ? '✓' : todo.status === 'in_progress' ? '●' : todo.status === 'cancelled' ? '✗' : '○' }}
        </span>
        <span class="text-xs"
          :class="todo.status === 'completed' || todo.status === 'cancelled'
            ? 'text-zinc-400 dark:text-zinc-600 line-through'
            : 'text-zinc-600 dark:text-zinc-300'">
          {{ todo.title }}
        </span>
      </div>
    </div>
  </div>
</template>
