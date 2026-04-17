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
  return parts.length > 1 ? (parts[parts.length - 1] ?? '') : ''
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
  <div class="fixed inset-0 bg-black/40 dark:bg-black/60 z-50 flex items-center justify-center p-8 backdrop-blur-sm animate-fade-in" @click.self="emit('close')">
    <div class="bg-white dark:bg-zinc-900 border border-zinc-200 dark:border-zinc-700 rounded-2xl flex flex-col w-full max-w-5xl max-h-[85vh] overflow-hidden shadow-2xl">
      <div class="flex items-center justify-between px-4 py-2.5 border-b border-zinc-200 dark:border-zinc-800 bg-zinc-50 dark:bg-zinc-800/80">
        <span class="font-mono text-xs text-zinc-500 dark:text-zinc-400 truncate">{{ path }}</span>
        <button
          class="text-xs text-zinc-400 dark:text-zinc-500 hover:text-zinc-600 dark:hover:text-zinc-300 cursor-pointer transition-colors font-medium px-2 py-0.5 rounded-lg hover:bg-zinc-100 dark:hover:bg-zinc-700"
          @click="emit('close')">
          Close
        </button>
      </div>
      <div class="flex-1 overflow-auto p-4 bg-white dark:bg-zinc-900">
        <pre class="text-xs leading-relaxed"><code class="hljs" v-html="highlighted()" /></pre>
      </div>
    </div>
  </div>
</template>
