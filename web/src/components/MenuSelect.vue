<script setup lang="ts">
// A compact headless single-select built on HeadlessUI's Listbox — a real
// <select>-semantics control (arrow-key navigation, type-ahead, Home/End,
// Enter/Esc, ARIA listbox roles), styled to match the app's form fields. This
// replaces the earlier Popover-based fake-select so the cadence/time/mode
// pickers read and behave as ordinary dropdowns.
//
// Designed for small, known option sets (trigger cadence, weekday, hour, mode,
// run filter). It is NOT a search box — use ProjectPickerPanel for the project
// field. Values are emitted verbatim (numbers stay numbers via v-model.number).
import { computed } from 'vue'
import { Listbox, ListboxButton, ListboxOptions, ListboxOption } from '@headlessui/vue'
import { ChevronUpDownIcon, CheckIcon } from '@heroicons/vue/24/outline'

export interface MenuSelectOption<V extends string | number = string | number> {
  value: V
  label: string
}

const props = withDefaults(defineProps<{
  modelValue: string | number
  options: MenuSelectOption[]
  // Which way the panel opens relative to the trigger. Pickers near the bottom
  // of a viewport (e.g. inside a dialog) open upward.
  placement?: 'top' | 'bottom'
  // Optional accessible label / title for the trigger button.
  title?: string
  // Match the field-input width of sibling inputs inside a form row.
  block?: boolean
}>(), {
  placement: 'bottom',
  title: undefined,
  block: false,
})

const emit = defineEmits<{ (e: 'update:modelValue', value: typeof props.modelValue): void }>()

// Display text for the trigger = the selected option's label (falls back to the
// raw value so a transient/out-of-range model never blanks the control).
const selectedLabel = computed(() => {
  const hit = props.options.find((o) => o.value === props.modelValue)
  return hit ? hit.label : String(props.modelValue)
})
</script>

<template>
  <!-- Inline position:relative on the root — headlessui's component root doesn't
       receive the SFC scoped attribute, so a scoped `.ms-root { position:
       relative }` wouldn't anchor the absolute panel. -->
  <Listbox
    as="div"
    class="ms-root"
    :class="{ block }"
    style="position: relative"
    :model-value="modelValue"
    @update:model-value="(v) => emit('update:modelValue', v as string | number)"
  >
    <ListboxButton class="ms-trigger" :class="{ block }" :title="title">
      <span class="ms-value">{{ selectedLabel }}</span>
      <ChevronUpDownIcon class="w-4 h-4 ms-caret" />
    </ListboxButton>

    <transition
      enter-active-class="pop-enter-active"
      enter-from-class="pop-enter-from"
      leave-active-class="pop-leave-active"
      leave-to-class="pop-leave-to"
    >
      <ListboxOptions
        class="ms-panel"
        :class="placement === 'top' ? 'place-top' : 'place-bottom'"
      >
        <ListboxOption
          v-for="opt in options"
          :key="opt.value"
          v-slot="{ active, selected }"
          :value="opt.value"
          as="template"
        >
          <li class="ms-option" :class="{ focus: active, selected }">
            <span class="ms-opt-label">{{ opt.label }}</span>
            <CheckIcon v-if="selected" class="w-3.5 h-3.5 ms-check" />
          </li>
        </ListboxOption>
      </ListboxOptions>
    </transition>
  </Listbox>
</template>

<style scoped>
.ms-root {
  display: inline-flex;
  min-width: 0;
}
.ms-root.block {
  display: flex;
  width: 100%;
}

.ms-trigger {
  display: inline-flex;
  align-items: center;
  justify-content: space-between;
  gap: 6px;
  width: auto;
  min-width: 0;
  height: 32px;
  padding: 0 9px;
  font-size: 13px;
  color: var(--color-foreground);
  background: var(--color-background);
  border: 1px solid var(--color-border);
  border-radius: var(--radius-lg);
  outline: none;
  cursor: pointer;
  transition: border-color 0.15s;
}
.ms-trigger.block {
  width: 100%;
}
.ms-trigger:hover {
  border-color: color-mix(in srgb, var(--color-foreground) 32%, var(--color-border));
}
.ms-trigger:focus-visible {
  border-color: var(--color-primary);
}
.ms-value {
  flex: 1;
  min-width: 0;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  text-align: left;
}
.ms-caret {
  color: var(--color-muted-foreground);
  flex-shrink: 0;
  opacity: 0.75;
}

.ms-panel {
  position: absolute;
  left: 0;
  z-index: var(--z-dropdown, 40);
  min-width: 100%;
  max-height: min(60vh, 320px);
  overflow-y: auto;
  margin: 0;
  padding: 4px;
  list-style: none;
  background: var(--color-surface);
  border: 1px solid var(--color-border);
  border-radius: var(--radius-xl);
  box-shadow: var(--shadow-md);
  outline: none;
}
.ms-panel.place-top {
  bottom: calc(100% + 6px);
}
.ms-panel.place-bottom {
  top: calc(100% + 4px);
}

.ms-option {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  width: 100%;
  padding: 7px 9px;
  border: none;
  background: transparent;
  border-radius: var(--radius-md);
  color: var(--color-foreground);
  font-size: 13px;
  text-align: left;
  cursor: pointer;
  transition: background 0.12s;
}
/* `focus` = HeadlessUI's `active` (pointer-hover or keyboard-focused option). */
.ms-option.focus {
  background: var(--color-muted);
}
/* `selected` = the currently chosen value. */
.ms-option.selected {
  color: var(--color-accent-neutral);
}
.ms-opt-label {
  flex: 1;
  min-width: 0;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.ms-check {
  color: var(--color-accent-neutral);
  flex-shrink: 0;
}

.pop-enter-active,
.pop-leave-active {
  transition: opacity 0.12s ease, transform 0.12s ease;
}
.pop-enter-from,
.pop-leave-to {
  opacity: 0;
  transform: translateY(-4px);
}
.ms-panel.place-top.pop-enter-from,
.ms-panel.place-top.pop-leave-to {
  transform: translateY(4px);
}
</style>
