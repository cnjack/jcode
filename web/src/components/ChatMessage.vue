<script setup lang="ts">
import { renderMarkdown } from '@/composables/markdown'
import type { ChatMessage } from '@/types/api'
import { ref, nextTick, computed } from 'vue'
import { Square2StackIcon, CheckIcon, PencilSquareIcon } from '@heroicons/vue/24/outline'

const props = defineProps<{
  message: ChatMessage
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

// Human-readable elapsed time for the turn (assistant messages only). "45s" for
// under a minute, "1m 23s" beyond. Empty when the message carries no duration.
const durationLabel = computed(() => {
  const ms = props.message.durationMs
  if (!ms || ms < 0) return ''
  const totalSec = Math.round(ms / 1000)
  if (totalSec < 60) return `${totalSec}s`
  const m = Math.floor(totalSec / 60)
  const s = totalSec % 60
  return s ? `${m}m ${s}s` : `${m}m`
})

const emit = defineEmits<{
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
          color: 'var(--color-on-primary)'
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
          class="px-3 py-1 text-xs font-semibold transition-all cursor-pointer active:scale-95"
          :style="{ background: 'var(--color-primary)', color: 'var(--color-on-primary)', borderRadius: 'var(--radius-md)' }"
          @click="confirmEdit"
        >
          Send
        </button>
        <button
          class="px-3 py-1 text-xs font-medium transition-all cursor-pointer active:scale-95"
          :style="{ background: 'var(--color-secondary)', color: 'var(--color-foreground)', borderRadius: 'var(--radius-md)' }"
          @click="cancelEdit"
        >
          Cancel
        </button>
        <span class="text-[10px]" style="color: var(--color-muted-foreground)">Enter to send · Shift+Enter for newline · Esc to cancel</span>
      </div>
    </div>

    <!-- Action footer (below the content): turn time persists; copy/edit reveal on
         hover or keyboard focus. Moved here from the role-label row. -->
    <div v-if="!editing" class="flex items-center gap-2 mt-1.5 pl-9">
      <!-- Turn elapsed time (assistant only) — persists after the live timer. -->
      <span
        v-if="durationLabel"
        class="text-[10px] tabular-nums"
        style="font-family: var(--font-mono); color: var(--color-muted-foreground); opacity: 0.7"
        title="Time this turn took"
      >{{ durationLabel }}</span>

      <div class="flex items-center gap-0.5 opacity-0 group-hover/msg:opacity-100 group-focus-within/msg:opacity-100 transition-opacity duration-150">
        <!-- Copy button -->
        <button
          class="w-5 h-5 flex items-center justify-center rounded-[var(--radius-sm)] transition-all cursor-pointer hover:bg-[var(--color-secondary)] active:scale-90"
          style="color: var(--color-muted-foreground)"
          :title="copied ? 'Copied!' : 'Copy'"
          @click="copyContent"
        >
          <Square2StackIcon v-if="!copied" class="w-3 h-3" />
          <CheckIcon v-else class="w-3 h-3" style="color: var(--color-primary)" />
        </button>

        <!-- Edit button (user messages) -->
        <button
          v-if="canEdit"
          class="w-5 h-5 flex items-center justify-center rounded-[var(--radius-sm)] transition-all cursor-pointer hover:bg-[var(--color-secondary)] active:scale-90"
          style="color: var(--color-muted-foreground)"
          title="Edit"
          @click="startEdit"
        >
          <PencilSquareIcon class="w-3 h-3" />
        </button>
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
