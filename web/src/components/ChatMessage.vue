<script setup lang="ts">
import { renderMarkdown } from '@/composables/markdown'
import type { ChatMessage } from '@/types/api'

defineProps<{
  message: ChatMessage
}>()
</script>

<template>
  <div
    :class="[
      'py-3',
      message.role === 'user' ? 'pl-4 border-l-2 border-stone-300' : '',
      message.role === 'system' ? 'pl-4 border-l-2 border-amber-400/50' : '',
    ]"
  >
    <div
      :class="[
        'text-[10px] font-medium uppercase tracking-wider mb-1.5',
        message.role === 'user' ? 'text-stone-500' : message.role === 'assistant' ? 'text-teal-600' : 'text-amber-600',
      ]"
    >
      {{ message.role === 'user' ? 'You' : message.role === 'assistant' ? 'jcode' : 'System' }}
    </div>
    <div
      class="prose prose-sm max-w-none prose-pre:bg-stone-100 prose-pre:border prose-pre:border-stone-200 prose-pre:rounded-lg prose-code:text-teal-700 prose-a:text-teal-600 prose-headings:text-stone-800 prose-strong:text-stone-800 prose-p:text-stone-600 prose-li:text-stone-600"
      v-html="renderMarkdown(message.content)"
    />
  </div>
</template>
