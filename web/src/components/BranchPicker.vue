<script setup lang="ts">
import { ref, computed, nextTick } from 'vue'
import { Popover, PopoverButton, PopoverPanel } from '@headlessui/vue'
import { CodeBracketIcon, MagnifyingGlassIcon, ChevronDownIcon, CheckIcon, PlusIcon } from '@heroicons/vue/24/outline'
import { useI18n } from 'vue-i18n'
import { useBranch } from '@/composables/useBranch'

withDefaults(defineProps<{
  // Open upward by default; the composer sits low in the viewport.
  placement?: 'top' | 'bottom'
}>(), { placement: 'top' })

const { current, branches, switching, error, checkout } = useBranch()
const { t } = useI18n()

const query = ref('')
const creating = ref(false)
const newName = ref('')
const newInput = ref<HTMLInputElement | null>(null)

const filtered = computed(() => {
  const q = query.value.trim().toLowerCase()
  if (!q) return branches.value
  return branches.value.filter((b) => b.toLowerCase().includes(q))
})

async function pick(branch: string, close: () => void) {
  if (branch === current.value) {
    reset()
    close()
    return
  }
  const ok = await checkout(branch, false)
  if (ok) {
    reset()
    close()
  }
  // On failure keep the panel open so the error message stays visible.
}

function startCreate() {
  creating.value = true
  newName.value = query.value.trim()
  nextTick(() => newInput.value?.focus())
}

async function confirmCreate(close: () => void) {
  const name = newName.value.trim()
  if (!name) return
  const ok = await checkout(name, true)
  if (ok) {
    reset()
    close()
  }
}

function reset() {
  query.value = ''
  creating.value = false
  newName.value = ''
  error.value = ''
}
</script>

<template>
  <!-- Only shown for git workspaces (no current branch ⇒ not a repo). -->
  <div v-if="current" class="bp-bar">
    <Popover class="bp-popover" style="position: relative">
      <PopoverButton as="template" :disabled="switching">
        <button class="bp-pill" :disabled="switching" :title="t('branches.current', { name: current })">
          <CodeBracketIcon class="w-3 h-3 bp-pill-icon" />
          <span class="bp-name">{{ current }}</span>
          <ChevronDownIcon class="w-3 h-3 bp-caret" />
        </button>
      </PopoverButton>

      <transition
        enter-active-class="pop-enter-active"
        enter-from-class="pop-enter-from"
        leave-active-class="pop-leave-active"
        leave-to-class="pop-leave-to"
      >
        <PopoverPanel
          v-slot="{ close }"
          class="bp-panel"
          :class="placement === 'top' ? 'place-top' : 'place-bottom'"
        >
          <div class="bp-search">
            <MagnifyingGlassIcon class="w-3 h-3 bp-search-icon" />
            <input v-model="query" class="bp-search-input" :placeholder="t('branches.search')" />
          </div>

          <div class="bp-section">{{ t('branches.title') }}</div>
          <div class="bp-list">
            <div v-if="filtered.length === 0" class="bp-hint">{{ t('branches.none') }}</div>
            <button
              v-for="b in filtered"
              :key="b"
              class="bp-row"
              :class="{ active: b === current }"
              :disabled="switching"
              @click="pick(b, close)"
            >
              <CodeBracketIcon class="w-3 h-3 bp-row-icon" />
              <span class="bp-row-name">{{ b }}</span>
              <CheckIcon v-if="b === current" class="w-3.5 h-3.5 bp-check" />
            </button>
          </div>

          <div v-if="error" class="bp-error">{{ error }}</div>

          <div class="bp-actions">
            <button v-if="!creating" class="bp-action" @click="startCreate">
              <PlusIcon class="w-3.5 h-3.5" /> <span>{{ t('branches.create') }}</span>
            </button>
            <div v-else class="bp-create">
              <input
                ref="newInput"
                v-model="newName"
                class="bp-create-input"
                :placeholder="t('branches.newName')"
                @keydown.enter="confirmCreate(close)"
                @keydown.esc="creating = false"
              />
              <button
                class="bp-create-btn"
                :disabled="!newName.trim() || switching"
                @click="confirmCreate(close)"
              >
                {{ t('branches.createBtn') }}
              </button>
            </div>
          </div>
        </PopoverPanel>
      </transition>
    </Popover>
  </div>
</template>

<style scoped>
.bp-bar {
  display: inline-flex;
  align-items: center;
  min-width: 0;
}
.bp-popover {
  display: inline-flex;
  min-width: 0;
}

.bp-pill {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  height: 28px;
  max-width: 200px;
  padding: 0 8px;
  border: 1px solid transparent;
  border-radius: var(--radius-lg);
  background: transparent;
  color: var(--color-muted-foreground);
  cursor: pointer;
  transition: background 0.15s, transform 0.06s ease;
}
.bp-pill:hover:not(:disabled) {
  background: var(--color-muted);
}
.bp-pill:active:not(:disabled) {
  transform: translateY(0.5px);
}
.bp-pill:disabled {
  opacity: 0.55;
  cursor: not-allowed;
}
.bp-pill-icon {
  flex-shrink: 0;
}
.bp-name {
  font-family: var(--font-mono);
  font-size: 11.5px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.bp-caret {
  flex-shrink: 0;
  margin-left: 1px;
  opacity: 0.7;
}

.bp-panel {
  position: absolute;
  left: 0;
  z-index: 40;
  width: 300px;
  max-width: 84vw;
  padding: 6px;
  background: var(--color-surface);
  border: 1px solid var(--color-border);
  border-radius: var(--radius-xl);
  box-shadow: var(--shadow-lg);
}
.bp-panel.place-top {
  bottom: calc(100% + 6px);
}
.bp-panel.place-bottom {
  top: calc(100% + 6px);
}

.bp-search {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 5px 8px;
  margin-bottom: 4px;
  border: 1px solid var(--color-border);
  border-radius: var(--radius-lg);
  background: var(--color-background);
}
.bp-search-icon {
  color: var(--color-muted-foreground);
  flex-shrink: 0;
}
.bp-search-input {
  flex: 1;
  min-width: 0;
  border: none;
  outline: none;
  background: transparent;
  font-size: 12.5px;
  color: var(--color-foreground);
}
.bp-search-input::placeholder {
  color: var(--color-muted-foreground);
}

.bp-section {
  padding: 4px 8px 2px;
  font-size: 10px;
  font-weight: 600;
  letter-spacing: 0.06em;
  text-transform: uppercase;
  color: var(--color-muted-foreground);
}

.bp-list {
  max-height: 240px;
  overflow-y: auto;
  padding: 2px;
}
.bp-row {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 100%;
  padding: 7px 8px;
  border: none;
  background: transparent;
  border-radius: var(--radius-md);
  cursor: pointer;
  text-align: left;
  color: var(--color-foreground);
  font-size: 12.5px;
  font-family: var(--font-mono);
  transition: background 0.12s;
}
.bp-row:hover:not(:disabled) {
  background: var(--color-muted);
}
.bp-row.active {
  background: var(--accent-wash-soft);
}
.bp-row:disabled {
  opacity: 0.6;
  cursor: default;
}
.bp-row-icon {
  color: var(--color-muted-foreground);
  flex-shrink: 0;
}
.bp-row.active .bp-row-icon {
  color: var(--color-primary);
}
.bp-row-name {
  flex: 1;
  min-width: 0;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.bp-check {
  color: var(--color-primary);
  flex-shrink: 0;
}
.bp-hint {
  padding: 14px 8px;
  text-align: center;
  font-size: 11.5px;
  color: var(--color-muted-foreground);
}

.bp-error {
  margin: 4px 2px 2px;
  padding: 7px 9px;
  border-radius: var(--radius-md);
  background: var(--color-error-bg);
  color: var(--color-error-fg);
  font-size: 11.5px;
  line-height: 1.45;
  white-space: pre-wrap;
  word-break: break-word;
}

.bp-actions {
  margin-top: 4px;
  padding-top: 4px;
  border-top: 1px solid var(--color-border);
}
.bp-action {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 100%;
  padding: 8px;
  border: none;
  background: transparent;
  border-radius: var(--radius-md);
  cursor: pointer;
  font-size: 12.5px;
  color: var(--color-foreground);
  transition: background 0.12s;
}
.bp-action:hover {
  background: var(--color-muted);
}
.bp-action svg {
  color: var(--color-muted-foreground);
  flex-shrink: 0;
}

.bp-create {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 4px;
}
.bp-create-input {
  flex: 1;
  min-width: 0;
  height: 30px;
  padding: 0 9px;
  border: 1px solid var(--color-border);
  border-radius: var(--radius-md);
  background: var(--color-background);
  color: var(--color-foreground);
  font-family: var(--font-mono);
  font-size: 12px;
  outline: none;
}
.bp-create-input:focus {
  border-color: var(--color-primary);
}
.bp-create-btn {
  flex-shrink: 0;
  height: 30px;
  padding: 0 12px;
  border: none;
  border-radius: var(--radius-md);
  background: var(--color-primary);
  color: var(--color-on-primary);
  font-size: 12px;
  font-weight: 500;
  cursor: pointer;
  transition: opacity 0.15s;
}
.bp-create-btn:disabled {
  opacity: 0.4;
  cursor: not-allowed;
}

.pop-enter-active,
.pop-leave-active {
  transition: opacity 0.12s ease, transform 0.12s ease;
}
.pop-enter-from,
.pop-leave-to {
  opacity: 0;
  transform: translateY(4px);
}
.bp-panel.place-bottom.pop-enter-from,
.bp-panel.place-bottom.pop-leave-to {
  transform: translateY(-4px);
}
</style>
