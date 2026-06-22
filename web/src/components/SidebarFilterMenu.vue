<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  AdjustmentsHorizontalIcon,
  ChevronRightIcon,
  ChevronLeftIcon,
  CheckIcon,
  ArrowPathIcon,
} from '@heroicons/vue/24/outline'
import { useProjectStore, DEFAULT_FILTERS } from '@/stores/project'
import type { SidebarFilters } from '@/stores/project'

const { t } = useI18n()
const projectStore = useProjectStore()
const filters = computed(() => projectStore.filters)

type RowKey = keyof SidebarFilters
interface Opt { value: string; label: string }
interface Row { key: RowKey; label: string; options: Opt[] }

// Open state + which row's options are showing ('' = the root list). Drill-in
// keeps the panel anchored and avoids a side-flyout overflowing the narrow rail.
const open = ref(false)
const openSub = ref<RowKey | ''>('')
const root = ref<HTMLElement | null>(null)

function toggle() {
  open.value = !open.value
  if (open.value) openSub.value = ''
}
function close() {
  open.value = false
}
// Manual outside-click / Esc close (mirrors ChatInput) — more reliable here than
// a headlessui Popover, which didn't toggle in this slot.
function onDocClick(e: MouseEvent) {
  if (root.value && !root.value.contains(e.target as Node)) close()
}
function onKey(e: KeyboardEvent) {
  if (e.key === 'Escape' && open.value) {
    e.stopPropagation()
    close()
  }
}
onMounted(() => {
  document.addEventListener('click', onDocClick)
  document.addEventListener('keydown', onKey)
})
onUnmounted(() => {
  document.removeEventListener('click', onDocClick)
  document.removeEventListener('keydown', onKey)
})

const filterRows = computed<Row[]>(() => [
  {
    key: 'status',
    label: t('sidebar.filter.status'),
    options: [
      { value: 'active', label: t('sidebar.filter.statusActive') },
      { value: 'archived', label: t('sidebar.filter.statusArchived') },
      { value: 'all', label: t('sidebar.filter.statusAll') },
    ],
  },
  {
    key: 'project',
    label: t('sidebar.filter.project'),
    options: [
      { value: '', label: t('sidebar.filter.projectAll') },
      ...projectStore.projectsForTree.map((p) => ({ value: p.path, label: p.name })),
    ],
  },
  {
    key: 'lastActivity',
    label: t('sidebar.filter.lastActivity'),
    options: [
      { value: 'all', label: t('sidebar.filter.activityAll') },
      { value: 'today', label: t('sidebar.filter.activityToday') },
      { value: 'week', label: t('sidebar.filter.activityWeek') },
      { value: 'month', label: t('sidebar.filter.activityMonth') },
    ],
  },
])

const viewRows = computed<Row[]>(() => [
  {
    key: 'groupBy',
    label: t('sidebar.filter.groupBy'),
    options: [
      { value: 'project', label: t('sidebar.filter.groupProject') },
      { value: 'date', label: t('sidebar.filter.groupDate') },
    ],
  },
  {
    key: 'sortBy',
    label: t('sidebar.filter.sortBy'),
    options: [
      { value: 'recency', label: t('sidebar.filter.sortRecency') },
      { value: 'name', label: t('sidebar.filter.sortName') },
      { value: 'created', label: t('sidebar.filter.sortCreated') },
    ],
  },
])

const allRows = computed(() => [...filterRows.value, ...viewRows.value])
const activeRow = computed(() => allRows.value.find((r) => r.key === openSub.value) || null)

// The right-aligned current value on a row. A project filter pointing at a
// deleted project falls back to "All" (its first option).
function currentLabel(row: Row): string {
  const cur = String(filters.value[row.key])
  const match = row.options.find((o) => o.value === cur)
  return match ? match.label : row.options[0]?.label ?? ''
}

function isSelected(row: Row, value: string): boolean {
  return String(filters.value[row.key]) === value
}

function apply(key: RowKey, value: string) {
  projectStore.setFilters({ [key]: value } as Partial<SidebarFilters>)
  openSub.value = ''
}

// Non-default filters get a dot on the trigger so an applied filter shows
// without opening the menu.
const isDirty = computed(() =>
  (Object.keys(DEFAULT_FILTERS) as RowKey[]).some((k) => filters.value[k] !== DEFAULT_FILTERS[k]),
)

function reset() {
  projectStore.resetFilters()
  openSub.value = ''
}
</script>

<template>
  <div ref="root" class="filter-menu">
    <button
      class="filter-btn"
      :class="{ dirty: isDirty, on: open }"
      :title="t('sidebar.filter.title')"
      :aria-label="t('sidebar.filter.title')"
      @click.stop="toggle"
    >
      <AdjustmentsHorizontalIcon class="w-4 h-4" />
      <span v-if="isDirty" class="filter-dot" aria-hidden="true" />
    </button>

    <transition
      enter-active-class="pop-enter-active"
      enter-from-class="pop-enter-from"
      leave-active-class="pop-leave-active"
      leave-to-class="pop-leave-to"
    >
      <div v-if="open" class="filter-panel" @click.stop>
        <!-- Root list -->
        <template v-if="!activeRow">
          <button v-for="row in filterRows" :key="row.key" class="fm-row" @click="openSub = row.key">
            <span class="fm-row-label">{{ row.label }}</span>
            <span class="fm-row-value">{{ currentLabel(row) }}</span>
            <ChevronRightIcon class="w-3.5 h-3.5 fm-row-chev" />
          </button>
          <div class="fm-sep" />
          <button v-for="row in viewRows" :key="row.key" class="fm-row" @click="openSub = row.key">
            <span class="fm-row-label">{{ row.label }}</span>
            <span class="fm-row-value">{{ currentLabel(row) }}</span>
            <ChevronRightIcon class="w-3.5 h-3.5 fm-row-chev" />
          </button>
          <template v-if="isDirty">
            <div class="fm-sep" />
            <button class="fm-row fm-reset" @click="reset">
              <ArrowPathIcon class="w-3.5 h-3.5" />
              <span class="fm-row-label">{{ t('sidebar.filter.reset') }}</span>
            </button>
          </template>
        </template>

        <!-- A filter's options -->
        <template v-else>
          <button class="fm-back" @click="openSub = ''">
            <ChevronLeftIcon class="w-3.5 h-3.5" />
            <span>{{ activeRow.label }}</span>
          </button>
          <div class="fm-sep" />
          <div class="fm-opts">
            <button
              v-for="opt in activeRow.options"
              :key="opt.value"
              class="fm-opt"
              :class="{ on: isSelected(activeRow, opt.value) }"
              @click="apply(activeRow.key, opt.value)"
            >
              <span class="fm-opt-label">{{ opt.label }}</span>
              <CheckIcon v-if="isSelected(activeRow, opt.value)" class="w-3.5 h-3.5 fm-opt-check" />
            </button>
          </div>
        </template>
      </div>
    </transition>
  </div>
</template>

<style scoped>
.filter-menu {
  position: relative;
  display: inline-flex;
}
.filter-btn {
  position: relative;
  display: grid;
  place-items: center;
  width: 22px;
  height: 22px;
  border: none;
  background: transparent;
  border-radius: var(--radius-sm);
  color: var(--color-muted-foreground);
  cursor: pointer;
  transition: background 0.15s, color 0.15s;
}
.filter-btn:hover,
.filter-btn.dirty,
.filter-btn.on {
  color: var(--color-foreground);
}
.filter-btn:hover,
.filter-btn.on {
  background: var(--color-muted);
}
.filter-dot {
  position: absolute;
  top: 1px;
  right: 1px;
  width: 5px;
  height: 5px;
  border-radius: var(--radius-pill);
  background: var(--color-accent-neutral);
}

.filter-panel {
  position: absolute;
  top: calc(100% + 4px);
  right: 0;
  z-index: var(--z-dropdown);
  width: 232px;
  max-height: 380px;
  overflow-y: auto;
  padding: 4px;
  background: var(--color-surface);
  border: 1px solid var(--color-border);
  border-radius: var(--radius-lg);
  box-shadow: var(--shadow-md);
  outline: none;
}

.fm-row {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 100%;
  padding: 7px 8px;
  border: none;
  background: transparent;
  border-radius: var(--radius-md);
  color: var(--color-foreground);
  font-size: 12.5px;
  text-align: left;
  cursor: pointer;
  transition: background 0.12s;
}
.fm-row:hover {
  background: var(--color-muted);
}
.fm-row-label {
  flex-shrink: 0;
}
.fm-row-value {
  flex: 1;
  min-width: 0;
  text-align: right;
  color: var(--color-muted-foreground);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.fm-row-chev {
  flex-shrink: 0;
  color: var(--color-muted-foreground);
}
.fm-reset {
  color: var(--color-muted-foreground);
}

.fm-sep {
  height: 1px;
  margin: 4px 0;
  background: var(--color-border);
}

.fm-back {
  display: flex;
  align-items: center;
  gap: 6px;
  width: 100%;
  padding: 6px 8px;
  border: none;
  background: transparent;
  border-radius: var(--radius-md);
  color: var(--color-foreground);
  font-size: 12.5px;
  font-weight: 500;
  text-align: left;
  cursor: pointer;
}
.fm-back:hover {
  background: var(--color-muted);
}

.fm-opts {
  display: flex;
  flex-direction: column;
}
.fm-opt {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 100%;
  padding: 7px 8px;
  border: none;
  background: transparent;
  border-radius: var(--radius-md);
  color: var(--color-foreground);
  font-size: 12.5px;
  text-align: left;
  cursor: pointer;
  transition: background 0.12s;
}
.fm-opt:hover {
  background: var(--color-muted);
}
.fm-opt.on {
  color: var(--color-accent-neutral);
}
.fm-opt-label {
  flex: 1;
  min-width: 0;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.fm-opt-check {
  flex-shrink: 0;
  color: var(--color-accent-neutral);
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
</style>
