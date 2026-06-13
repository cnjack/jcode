<script setup lang="ts">
import { renderMarkdown } from '@/composables/markdown'
import type { ChatMessage } from '@/types/api'
import { ref, nextTick, computed } from 'vue'

const props = defineProps<{
  message: ChatMessage
  canRetry?: boolean
  canEdit?: boolean
}>()

// System messages carry a level: 'error' (red), 'notice' (muted, e.g. Stopped),
// or undefined (default warning styling).
const systemColor = computed(() => {
  if (props.message.level === 'error') return 'var(--color-destructive)'
  if (props.message.level === 'notice') return 'var(--color-muted-foreground)'
  return 'var(--color-warning-fg)'
})
const systemLabel = computed(() => (props.message.level === 'error' ? 'Error' : 'System'))

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
    <div class="flex items-center gap-2.5 mb-2">
      <div
        class="w-7 h-7 rounded-full flex items-center justify-center text-[10px] font-bold shrink-0"
        :style="{
          background: message.role === 'assistant' ? 'var(--color-primary)' :
                      message.role === 'user' && message.source === 'wechat' ? 'var(--color-info-fg)' :
                      message.role === 'system' ? systemColor :
                      'var(--color-foreground)',
          color: '#fff'
        }"
      >
        <template v-if="message.role === 'assistant'">J</template>
        <template v-else-if="message.role === 'user' && message.source === 'wechat'">W</template>
        <template v-else-if="message.role === 'user'">U</template>
        <template v-else>S</template>
      </div>
      <span
        class="text-[11px] font-semibold"
        :style="{
          color: message.role === 'assistant' ? 'var(--color-primary)' :
                 message.role === 'user' && message.source === 'wechat' ? 'var(--color-info-fg)' :
                 message.role === 'system' ? systemColor :
                 'var(--color-foreground)'
        }"
      >
        {{ message.role === 'user' ? (message.source === 'wechat' ? 'WeChat' : 'You') : message.role === 'assistant' ? '[J]CODE' : systemLabel }}
      </span>

      <!-- Action buttons: visible on hover or keyboard focus-within -->
      <div class="flex items-center gap-0.5 ml-1 opacity-0 group-hover/msg:opacity-100 group-focus-within/msg:opacity-100 transition-opacity duration-150">
        <!-- Copy button -->
        <button
          class="w-5 h-5 flex items-center justify-center rounded transition-colors cursor-pointer"
          style="color: var(--color-muted-foreground)"
          :title="copied ? 'Copied!' : 'Copy'"
          @click="copyContent"
        >
          <svg v-if="!copied" class="w-3 h-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
            <rect x="9" y="9" width="13" height="13" rx="2" />
            <path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1" />
          </svg>
          <svg v-else class="w-3 h-3" style="color: var(--color-primary)" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round">
            <polyline points="20 6 9 17 4 12" />
          </svg>
        </button>

        <!-- Retry button (assistant messages) -->
        <button
          v-if="canRetry"
          class="w-5 h-5 flex items-center justify-center rounded transition-colors cursor-pointer"
          style="color: var(--color-muted-foreground)"
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
          class="w-5 h-5 flex items-center justify-center rounded transition-colors cursor-pointer"
          style="color: var(--color-muted-foreground)"
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
    <div v-if="message.images && message.images.length > 0" class="pl-9 mb-2 flex flex-wrap gap-2">
      <img
        v-for="(img, i) in message.images"
        :key="i"
        :src="`data:${img.media_type};base64,${img.data}`"
        class="max-w-64 max-h-48 object-contain cursor-pointer hover:opacity-90 transition-opacity"
        :style="{ borderRadius: 'var(--radius-lg)', border: '1px solid var(--color-border)' }"
        @click="($event.target as HTMLImageElement).classList.toggle('max-w-64'); ($event.target as HTMLImageElement).classList.toggle('max-w-full')"
      />
    </div>

    <!-- Content or Edit mode -->
    <div v-if="!editing" class="prose-chat pl-9" v-html="renderMarkdown(message.content)" />

    <!-- Inline edit mode -->
    <div v-else class="pl-9">
      <textarea
        ref="editTextarea"
        v-model="editText"
        class="w-full min-h-20 max-h-80 resize-y text-sm px-3 py-2 transition-colors"
        :style="{ borderRadius: 'var(--radius-lg)', border: '1px solid var(--color-border)', background: 'var(--color-surface)', color: 'var(--color-foreground)' }"
        @keydown="handleEditKeyDown"
      />
      <div class="flex items-center gap-2 mt-2">
        <button
          class="px-3 py-1 text-xs font-medium rounded text-white transition-colors cursor-pointer"
          :style="{ background: 'var(--color-primary)', borderRadius: 'var(--radius-md)' }"
          @click="confirmEdit"
        >
          Send
        </button>
        <button
          class="px-3 py-1 text-xs font-medium rounded transition-colors cursor-pointer"
          :style="{ background: 'var(--color-secondary)', color: 'var(--color-foreground)', borderRadius: 'var(--radius-md)' }"
          @click="cancelEdit"
        >
          Cancel
        </button>
        <span class="text-[10px]" style="color: var(--color-muted-foreground)">Enter to send · Shift+Enter for newline · Esc to cancel</span>
      </div>
    </div>

    <!-- Collapsible raw detail for errors -->
    <details
      v-if="message.role === 'system' && message.level === 'error' && message.detail"
      class="pl-9 mt-1"
    >
      <summary class="text-[11px] cursor-pointer select-none" style="color: var(--color-muted-foreground)">Details</summary>
      <pre class="text-[11px] font-mono whitespace-pre-wrap mt-1 px-2 py-1.5 overflow-x-auto" style="color: var(--color-muted-foreground); background: var(--color-muted); border-radius: var(--radius-md)">{{ message.detail }}</pre>
    </details>
  </div>
</template>
