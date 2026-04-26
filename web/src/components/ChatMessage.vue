<script setup lang="ts">
import { renderMarkdown } from '@/composables/markdown'
import type { ChatMessage } from '@/types/api'
import { ref, nextTick } from 'vue'

const props = defineProps<{
  message: ChatMessage
  canRetry?: boolean
  canEdit?: boolean
}>()

const emit = defineEmits<{
  retry: []
  edit: [newText: string]
}>()

const copied = ref(false)
const editing = ref(false)
const editText = ref('')
const editTextarea = ref<HTMLTextAreaElement | null>(null)

function copyContent() {
  navigator.clipboard.writeText(props.message.content).then(() => {
    copied.value = true
    setTimeout(() => { copied.value = false }, 1500)
  }).catch((err) => {
    console.error('Failed to copy:', err)
  })
}

function startEdit() {
  editText.value = props.message.content
  editing.value = true
  nextTick(() => {
    editTextarea.value?.focus()
  })
}

function confirmEdit() {
  const text = editText.value.trim()
  if (text) {
    emit('edit', text)
  }
  editing.value = false
}

function cancelEdit() {
  editing.value = false
}

function handleEditKeyDown(e: KeyboardEvent) {
  if (e.key === 'Enter' && !e.shiftKey) {
    e.preventDefault()
    confirmEdit()
  }
  if (e.key === 'Escape') {
    e.preventDefault()
    cancelEdit()
  }
}
</script>

<template>
  <div class="py-3 animate-fade-in group/msg">
    <!-- Role label + action buttons -->
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
        <template v-if="message.role === 'assistant'">
          <span style="color: #FF8400;">J</span>
        </template>
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
        {{ message.role === 'user' ? (message.source === 'wechat' ? 'WeChat' : 'You') : message.role === 'assistant' ? '[J]CODE' : 'System' }}
      </span>

      <!-- Action buttons: visible on hover or keyboard focus-within -->
      <div class="flex items-center gap-0.5 ml-1 opacity-0 group-hover/msg:opacity-100 group-focus-within/msg:opacity-100 transition-opacity duration-150">
        <!-- Copy button -->
        <button
          class="w-5 h-5 flex items-center justify-center rounded text-zinc-400 hover:text-zinc-600 dark:hover:text-zinc-300 hover:bg-zinc-100 dark:hover:bg-zinc-800 transition-colors cursor-pointer"
          :title="copied ? 'Copied!' : 'Copy'"
          @click="copyContent"
        >
          <svg v-if="!copied" class="w-3 h-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
            <rect x="9" y="9" width="13" height="13" rx="2" />
            <path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1" />
          </svg>
          <svg v-else class="w-3 h-3 text-emerald-500" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round">
            <polyline points="20 6 9 17 4 12" />
          </svg>
        </button>

        <!-- Retry button (assistant messages) -->
        <button
          v-if="canRetry"
          class="w-5 h-5 flex items-center justify-center rounded text-zinc-400 hover:text-amber-500 dark:hover:text-amber-400 hover:bg-zinc-100 dark:hover:bg-zinc-800 transition-colors cursor-pointer"
          title="Retry"
          @click="emit('retry')"
        >
          <svg class="w-3 h-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
            <path d="M3 12a9 9 0 1 0 9-9 9.75 9.75 0 0 0-6.74 2.74L3 8" />
            <path d="M3 3v5h5" />
          </svg>
        </button>

        <!-- Edit button (user messages) -->
        <button
          v-if="canEdit && !editing"
          class="w-5 h-5 flex items-center justify-center rounded text-zinc-400 hover:text-zinc-600 dark:hover:text-zinc-300 hover:bg-zinc-100 dark:hover:bg-zinc-800 transition-colors cursor-pointer"
          title="Edit"
          @click="startEdit"
        >
          <svg class="w-3 h-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
            <path d="M11 4H4a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2v-7" />
            <path d="M18.5 2.5a2.121 2.121 0 0 1 3 3L12 15l-4 1 1-4Z" />
          </svg>
        </button>
      </div>
    </div>

    <!-- Images -->
    <div v-if="message.images && message.images.length > 0" class="pl-7 mb-2 flex flex-wrap gap-2">
      <img
        v-for="(img, i) in message.images"
        :key="i"
        :src="`data:${img.media_type};base64,${img.data}`"
        class="max-w-64 max-h-48 rounded border border-zinc-200 dark:border-zinc-700 object-contain cursor-pointer hover:opacity-90 transition-opacity"
        @click="($event.target as HTMLImageElement).classList.toggle('max-w-64'); ($event.target as HTMLImageElement).classList.toggle('max-w-full')"
      />
    </div>

    <!-- Content or Edit mode -->
    <div v-if="!editing" class="prose-chat pl-7" v-html="renderMarkdown(message.content)" />

    <!-- Inline edit mode -->
    <div v-else class="pl-7">
      <textarea
        ref="editTextarea"
        v-model="editText"
        class="w-full min-h-20 max-h-80 resize-y rounded-lg border border-zinc-300 dark:border-zinc-600 bg-white dark:bg-zinc-800 text-sm text-zinc-800 dark:text-zinc-200 px-3 py-2 focus:outline-none focus:ring-2 focus:ring-emerald-500/40 focus:border-emerald-500 dark:focus:border-emerald-400 transition-colors"
        @keydown="handleEditKeyDown"
      />
      <div class="flex items-center gap-2 mt-2">
        <button
          class="px-3 py-1 text-xs font-medium rounded bg-emerald-500 hover:bg-emerald-600 text-white transition-colors cursor-pointer"
          @click="confirmEdit"
        >
          Send
        </button>
        <button
          class="px-3 py-1 text-xs font-medium rounded bg-zinc-100 hover:bg-zinc-200 dark:bg-zinc-700 dark:hover:bg-zinc-600 text-zinc-600 dark:text-zinc-300 transition-colors cursor-pointer"
          @click="cancelEdit"
        >
          Cancel
        </button>
        <span class="text-[10px] text-zinc-400">Enter to send · Shift+Enter for newline · Esc to cancel</span>
      </div>
    </div>
  </div>
</template>
