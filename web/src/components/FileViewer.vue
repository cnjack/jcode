<script setup lang="ts">
import hljs from 'highlight.js'
import { useI18n } from 'vue-i18n'

const props = defineProps<{
  path: string
  content: string
}>()

const emit = defineEmits<{
  close: []
}>()

const { t } = useI18n()

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
  <div class="fv-overlay animate-fade-in" @click.self="emit('close')">
    <div class="fv-modal">
      <div class="fv-head">
        <span class="fv-path">{{ path }}</span>
        <button class="fv-close" @click="emit('close')">{{ t('fileViewer.close') }}</button>
      </div>
      <div class="fv-body">
        <pre class="fv-pre"><code class="hljs" v-html="highlighted()" /></pre>
      </div>
    </div>
  </div>
</template>

<style scoped>
.fv-overlay {
  position: fixed;
  inset: 0;
  z-index: var(--z-modal);
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 32px;
  background: var(--backdrop);
  backdrop-filter: blur(6px);
  -webkit-backdrop-filter: blur(6px);
}
.fv-modal {
  display: flex;
  flex-direction: column;
  width: 100%;
  max-width: 64rem;
  max-height: 85vh;
  overflow: hidden;
  background: var(--color-surface);
  border: 1px solid var(--color-border);
  border-radius: var(--radius-xl);
  box-shadow: var(--shadow-lg);
}
.fv-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 10px 14px;
  border-bottom: 1px solid var(--color-border);
  background: var(--color-muted);
}
.fv-path {
  font-family: var(--font-mono);
  font-size: 12px;
  color: var(--color-muted-foreground);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.fv-close {
  flex-shrink: 0;
  padding: 3px 8px;
  font-size: 12px;
  font-weight: 500;
  color: var(--color-muted-foreground);
  background: transparent;
  border: none;
  border-radius: var(--radius-sm);
  cursor: pointer;
  transition: background 0.15s, color 0.15s;
}
.fv-close:hover {
  color: var(--color-foreground);
  background: var(--color-secondary);
}
.fv-body {
  flex: 1;
  overflow: auto;
  padding: 18px;
  background: var(--color-surface);
}
.fv-pre {
  font-size: 12px;
  line-height: 1.65;
  margin: 0;
}
</style>
