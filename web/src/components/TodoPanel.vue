<script setup lang="ts">
import { useChatStore } from '@/stores/chat'

const store = useChatStore()
</script>

<template>
  <div v-if="store.todos.length > 0" class="border-t backdrop-blur-sm px-5 py-2.5 max-h-40 overflow-y-auto" style="border-color: var(--color-border); background-color: var(--color-muted)">
    <div class="max-w-3xl mx-auto">
      <div class="text-[10px] uppercase tracking-wider mb-1.5 font-semibold" style="color: var(--color-muted-foreground)">Tasks</div>
      <div v-for="todo in store.todos" :key="todo.id" class="flex items-center gap-2 py-0.5">
        <span class="text-[10px] shrink-0"
          :style="{
            color: todo.status === 'completed' ? 'var(--color-success-fg)'
              : todo.status === 'in_progress' ? 'var(--color-warning-fg)'
              : 'var(--color-muted-foreground)',
          }">
          {{ todo.status === 'completed' ? '✓' : todo.status === 'in_progress' ? '●' : todo.status === 'cancelled' ? '✗' : '○' }}
        </span>
        <span class="text-xs"
          :style="{
            color: (todo.status === 'completed' || todo.status === 'cancelled') ? 'var(--color-muted-foreground)' : 'var(--color-foreground)',
            textDecoration: (todo.status === 'completed' || todo.status === 'cancelled') ? 'line-through' : 'none',
          }">
          {{ todo.title }}
        </span>
      </div>
    </div>
  </div>
</template>
