<script setup lang="ts">
// A compact headless single-select built on HeadlessUI's Popover.
//
// Why Popover and not Listbox? MenuSelect is used inside the automation editor
// <Dialog>. A Listbox's value-driven auto-close fights the dialog: selecting an
// option updates :model-value, which re-renders the panel mid-open, and the
// Listbox then fails to dismiss (it flickers and stays open). The Popover hands
// us an explicit `close` slot callback, so a click dismisses the panel
// deterministically — the same reason ProjectPickerPanel uses a Popover. This
// used to be a Listbox; switching here fixes "select item doesn't close".
//
// Keyboard parity with the old Listbox is kept manually: ArrowUp/Down move the
// highlighted option, Home/End jump to the ends, Enter selects, Esc closes, and
// typing the first letter of an option jumps to it (type-ahead).
//
// Designed for small, known option sets (trigger cadence, weekday, hour, mode,
// run filter). Values are emitted verbatim (numbers stay numbers via
// v-model.number).
import { ref, computed } from 'vue'
import { Popover, PopoverButton, PopoverPanel } from '@headlessui/vue'
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

// ── Keyboard navigation (replaces what Listbox gave us for free) ──
// `activeIndex` is the highlighted option; -1 means nothing highlighted. It is
// reset every time the panel opens (afterOpen) so a stale highlight from a
// previous open never lands on the wrong row.
const activeIndex = ref(-1)
const optionRefs = ref<Array<HTMLLIElement | null>>([])

function selectedIndex(): number {
  return props.options.findIndex((o) => o.value === props.modelValue)
}

function clamp(i: number): number {
  if (!props.options.length) return -1
  return (i + props.options.length) % props.options.length
}

function move(delta: number) {
  if (!props.options.length) return
  const start = activeIndex.value >= 0 ? activeIndex.value : (delta > 0 ? -1 : 0)
  activeIndex.value = clamp(start + delta)
  scrollActiveIntoView()
}

function jumpTo(edge: 'home' | 'end') {
  if (!props.options.length) return
  activeIndex.value = edge === 'home' ? 0 : props.options.length - 1
  scrollActiveIntoView()
}

function choose(i: number, close: () => void) {
  const opt = props.options[i]
  if (opt) emit('update:modelValue', opt.value)
  close()
}

function scrollActiveIntoView() {
  const el = optionRefs.value[activeIndex.value]
  el?.scrollIntoView({ block: 'nearest' })
}

// Type-ahead: jump to the first option whose label starts with the typed key
// (case-insensitive). Matches the Listbox feel for small option sets.
function typeAhead(key: string, close: () => void) {
  if (!props.options.length) return
  const lower = key.toLowerCase()
  const from = activeIndex.value >= 0 ? activeIndex.value + 1 : 0
  let hit = -1
  for (let i = 0; i < props.options.length; i++) {
    const idx = (from + i) % props.options.length
    if (props.options[idx].label.toLowerCase().startsWith(lower)) { hit = idx; break }
  }
  if (hit >= 0) activeIndex.value = hit
  // Space/Enter on a type-ahead hit selects; a printable char only moves focus.
  if (key === 'Enter' && activeIndex.value >= 0) choose(activeIndex.value, close)
}

// `afterOpen` is wired to the transition's @before-enter so the highlight is
// seeded exactly when the panel appears — keeping it in a headless-slot scope
// would be cleaner, but HeadlessUI's Popover doesn't expose a lifecycle hook,
// so the transition hook is the reliable anchor.
function afterOpen() {
  const i = selectedIndex()
  activeIndex.value = i >= 0 ? i : (props.options.length ? 0 : -1)
  optionRefs.value = []
}

function onKeydown(e: KeyboardEvent, close: () => void) {
  switch (e.key) {
    case 'ArrowDown': e.preventDefault(); move(1); break
    case 'ArrowUp': e.preventDefault(); move(-1); break
    case 'Home': e.preventDefault(); jumpTo('home'); break
    case 'End': e.preventDefault(); jumpTo('end'); break
    case 'Enter': e.preventDefault(); if (activeIndex.value >= 0) choose(activeIndex.value, close); break
    case 'Escape': e.preventDefault(); close(); break
    case 'Tab': close(); break
    default:
      if (e.key.length === 1 && /\S/.test(e.key)) { e.preventDefault(); typeAhead(e.key, close) }
  }
}
</script>

<template>
  <!-- Inline position:relative on the root — headlessui's component root doesn't
       receive the SFC scoped attribute, so the absolute options panel needs an
       anchor. A Popover (not a Listbox) drives the dropdown so we control
       dismissal explicitly via the `close` slot callback. -->
  <Popover class="ms-root" :class="{ block }" style="position: relative">
    <PopoverButton as="template">
      <button
        type="button"
        class="ms-trigger"
        :class="{ block }"
        :title="title"
      >
        <span class="ms-value">{{ selectedLabel }}</span>
        <ChevronUpDownIcon class="w-4 h-4 ms-caret" />
      </button>
    </PopoverButton>

    <transition
      enter-active-class="pop-enter-active"
      enter-from-class="pop-enter-from"
      leave-active-class="pop-leave-active"
      leave-to-class="pop-leave-to"
      @before-enter="afterOpen"
    >
      <PopoverPanel
        v-slot="{ close }"
        class="ms-panel"
        :class="placement === 'top' ? 'place-top' : 'place-bottom'"
      >
        <!-- keydown is captured on the panel so it works whether the panel or a
             child has focus; HeadlessUI keeps focus on the trigger after open,
             so we also let key events bubble from the PopoverButton. -->
        <ul role="listbox" @keydown="(e) => onKeydown(e, close)">
          <li
            v-for="(opt, i) in options"
            :key="opt.value"
            ref="optionRefs"
            role="option"
            :aria-selected="opt.value === modelValue"
            class="ms-option"
            :class="{ focus: i === activeIndex, selected: opt.value === modelValue }"
            @click="choose(i, close)"
            @mousemove="activeIndex = i"
          >
            <span class="ms-opt-label">{{ opt.label }}</span>
            <CheckIcon v-if="opt.value === modelValue" class="w-3.5 h-3.5 ms-check" />
          </li>
        </ul>
      </PopoverPanel>
    </transition>
  </Popover>
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
/* `focus` = the keyboard-highlighted option (replaces HeadlessUI Listbox's
   `active` slot prop). */
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
