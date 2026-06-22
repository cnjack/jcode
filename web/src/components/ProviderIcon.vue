<script setup lang="ts">
import { computed } from 'vue'
import { iconForProvider } from '@/utils/providerIcons'

const props = withDefaults(defineProps<{ provider: string; size?: number }>(), {
  size: 18,
})

// Trusted, build-time bundled SVG strings (not user input), so v-html is safe.
const svg = computed(() => iconForProvider(props.provider))

// First alphanumeric character, shown when the provider has no brand icon.
const initial = computed(
  () => (props.provider || '').replace(/[^a-z0-9]/i, '').charAt(0).toUpperCase() || '?',
)
</script>

<template>
  <span
    v-if="svg"
    class="provider-icon"
    :style="{ fontSize: size + 'px', width: size + 'px', height: size + 'px' }"
    aria-hidden="true"
    v-html="svg"
  />
  <span
    v-else
    class="provider-icon provider-icon-fallback"
    :style="{ width: size + 'px', height: size + 'px', fontSize: Math.round(size * 0.52) + 'px' }"
    aria-hidden="true"
    >{{ initial }}</span
  >
</template>

<style scoped>
.provider-icon {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  flex: none;
  line-height: 0;
}
.provider-icon :deep(svg) {
  width: 1em;
  height: 1em;
  display: block;
}
.provider-icon-fallback {
  border-radius: 6px;
  background: var(--color-secondary);
  color: var(--color-muted-foreground);
  font-weight: 600;
  line-height: 1;
}
</style>
