<script setup lang="ts">
import hljs from 'highlight.js'

const props = defineProps<{
  path: string
  content: string
}>()

const emit = defineEmits<{
  close: []
}>()

function ext(path: string): string {
  const parts = path.split('.')
  return parts.length > 1 ? parts[parts.length - 1] : ''
}

function highlighted(): string {
  const language = ext(props.path)
  if (language && hljs.getLanguage(language)) {
    return hljs.highlight(props.content, { language }).value
  }
  return hljs.highlightAuto(props.content).value
}
</script>

<template>
  <div class="fixed inset-0 bg-black/20 z-50 flex items-center justify-center p-8" @click.self="emit('close')">
    <div class="bg-white border border-stone-200 rounded-xl flex flex-col w-full max-w-5xl max-h-[85vh] overflow-hidden shadow-xl">
      <div class="flex items-center justify-between px-4 py-2.5 border-b border-stone-200 bg-stone-50">
        <span class="font-mono text-xs text-stone-500 truncate">{{ path }}</span>
        <button
          class="text-xs text-stone-400 hover:text-stone-600 cursor-pointer transition-colors"
          @click="emit('close')">
          Close
        </button>
      </div>
      <div class="flex-1 overflow-auto p-4 bg-white">
        <pre class="text-xs leading-relaxed"><code class="hljs" v-html="highlighted()" /></pre>
      </div>
    </div>
  </div>
</template>
