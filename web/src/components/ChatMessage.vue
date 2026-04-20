<script setup lang="ts">
import { renderMarkdown } from '@/composables/markdown'
import type { ChatMessage } from '@/types/api'

defineProps<{
  message: ChatMessage
}>()
</script>

<template>
  <div class="py-3 animate-fade-in">
    <!-- Role label -->
    <div class="flex items-center gap-2 mb-2">
      <div
        class="w-5 h-5 rounded flex items-center justify-center text-[9px] font-bold shrink-0"
        :class="{
          'bg-emerald-500/15 text-emerald-600 dark:bg-emerald-400/15 dark:text-emerald-400': message.role === 'assistant',
          'bg-zinc-200 text-zinc-500 dark:bg-zinc-700 dark:text-zinc-400': message.role === 'user' && message.source !== 'wechat',
          'bg-green-500/15 text-green-600 dark:bg-green-400/15 dark:text-green-400': message.role === 'user' && message.source === 'wechat',
          'bg-amber-500/15 text-amber-600 dark:bg-amber-400/15 dark:text-amber-400': message.role === 'system',
        }"
      >
        <template v-if="message.role === 'assistant'">J</template>
        <template v-else-if="message.role === 'user' && message.source === 'wechat'">W</template>
        <template v-else-if="message.role === 'user'">U</template>
        <template v-else>S</template>
      </div>
      <span
        class="text-[10px] font-semibold uppercase tracking-wider"
        :class="{
          'text-emerald-600 dark:text-emerald-400': message.role === 'assistant',
          'text-zinc-500 dark:text-zinc-400': message.role === 'user' && message.source !== 'wechat',
          'text-green-600 dark:text-green-400': message.role === 'user' && message.source === 'wechat',
          'text-amber-600 dark:text-amber-400': message.role === 'system',
        }"
      >
        {{ message.role === 'user' ? (message.source === 'wechat' ? 'WeChat' : 'You') : message.role === 'assistant' ? 'jcode' : 'System' }}
      </span>
    </div>
    <!-- Content -->
    <div
      class="prose-chat pl-7"
      v-html="renderMarkdown(message.content)"
    />
  </div>
</template>
