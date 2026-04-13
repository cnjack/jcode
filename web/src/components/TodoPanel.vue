<script setup lang="ts">
import { useChatStore } from '@/stores/chat'

const store = useChatStore()
</script>

<template>
  <div v-if="store.todos.length > 0" class="border-t border-stone-200 bg-stone-50 px-5 py-2.5 max-h-40 overflow-y-auto">
    <div class="max-w-3xl mx-auto">
      <div class="text-[10px] text-stone-400 uppercase tracking-wider mb-1.5">Tasks</div>
      <div v-for="todo in store.todos" :key="todo.ID" class="flex items-center gap-2 py-0.5">
        <span class="text-[10px] shrink-0"
          :class="todo.Status === 'completed' ? 'text-teal-600' : todo.Status === 'in-progress' ? 'text-amber-500' : 'text-stone-400'">
          {{ todo.Status === 'completed' ? '✓' : todo.Status === 'in-progress' ? '●' : '○' }}
        </span>
        <span class="text-xs" :class="todo.Status === 'completed' ? 'text-stone-400 line-through' : 'text-stone-600'">
          {{ todo.Title }}
        </span>
      </div>
    </div>
  </div>
</template>
