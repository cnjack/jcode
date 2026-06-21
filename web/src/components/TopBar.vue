<script setup lang="ts">
import { computed, ref, onMounted, watch } from 'vue'
import { CommandLineIcon, ArrowsRightLeftIcon, FolderOpenIcon, ClipboardDocumentCheckIcon, ChevronDownIcon, RectangleStackIcon } from '@heroicons/vue/24/outline'
import { Menu as HMenu, MenuButton, MenuItems, MenuItem } from '@headlessui/vue'
import { api } from '@/composables/api'

type PanelType = 'terminal' | 'files' | 'changes' | 'plan'

const props = defineProps<{
  isRunning: boolean
  wsConnected: boolean
  activePanel: 'none' | PanelType
  terminalOpen: boolean
}>()

const emit = defineEmits<{
  'toggle-panel': [panel: PanelType]
}>()

// Terminal is a bottom panel and can be open alongside a right-panel tab, so it
// is tracked separately from activePanel (which reflects the right panel only).
function isCurrent(panel: PanelType): boolean {
  if (panel === 'terminal') return props.terminalOpen
  return props.activePanel === panel
}

// Dot colour and label share the same priority order (running > connected >
// disconnected) so they never disagree — previously the colour led with
// isRunning while the label led with wsConnected, so a running+connected agent
// showed an orange "Running" dot but a "Connected" tooltip, and the 'Running'
// label was effectively dead (running always implies connected).
const statusColor = computed(() => {
  if (props.isRunning) return 'var(--color-primary)'
  if (props.wsConnected) return 'var(--color-success)'
  return 'var(--color-muted-foreground)'
})

const statusLabel = computed(() => {
  if (props.isRunning) return 'Running'
  if (props.wsConnected) return 'Connected'
  return 'Disconnected'
})

const panelButtons = [
  { panel: 'plan' as PanelType, icon: ClipboardDocumentCheckIcon, label: 'Plan', shortcut: '⇧⌘P' },
  { panel: 'files' as PanelType, icon: FolderOpenIcon, label: 'Files', shortcut: '⇧⌘E' },
  { panel: 'changes' as PanelType, icon: ArrowsRightLeftIcon, label: 'Changes', shortcut: '⇧⌘G' },
  { panel: 'terminal' as PanelType, icon: CommandLineIcon, label: 'Terminal', shortcut: '⌘`' },
]

// Working-tree diff stat, shown inline on the Changes item. Fetched from the
// real /api/diff endpoint; never fabricated (null on failure / clean tree).
const diffStat = ref<{ additions: number; deletions: number } | null>(null)

async function loadDiffStat() {
  try {
    const result = await api.diff('working')
    const additions = result.entries.reduce((sum, e) => sum + e.additions, 0)
    const deletions = result.entries.reduce((sum, e) => sum + e.deletions, 0)
    diffStat.value = result.entries.length > 0 ? { additions, deletions } : null
  } catch (err) {
    console.error('Failed to fetch diff stat:', err)
    diffStat.value = null
  }
}

onMounted(loadDiffStat)
watch(
  () => props.isRunning,
  (running, was) => {
    if (was && !running) loadDiffStat()
  },
)
</script>

<template>
  <!-- The single top-right control, floated into the title-bar zone. The panel
       menu carries Plan/Files/Changes/Terminal; the dot on the button reflects
       live connection status. Inline position:relative — headlessui's root
       doesn't get the SFC scoped attribute, so the absolute menu would otherwise
       anchor to the wrong ancestor. -->
  <div class="topbar-control">
    <HMenu as="div" class="panel-menu" style="position: relative" v-slot="{ open }">
      <MenuButton
        class="panel-menu-btn"
        :class="{ open }"
        :aria-label="`Panels menu · ${statusLabel}`"
        :aria-expanded="open"
        :title="`Panels · ${statusLabel}  (⇧⌘P plan · ⇧⌘E files · ⇧⌘G changes · ⌘\` terminal)`"
        @click="loadDiffStat"
      >
        <RectangleStackIcon class="w-3.5 h-3.5" />
        <ChevronDownIcon class="w-3 h-3 panel-menu-caret" />
        <span class="status-dot panel-status-dot" :style="{ backgroundColor: statusColor }" />
      </MenuButton>
      <transition
        enter-active-class="pop-enter-active"
        enter-from-class="pop-enter-from"
        leave-active-class="pop-leave-active"
        leave-to-class="pop-leave-to"
      >
        <MenuItems class="panel-menu-items">
          <MenuItem v-for="btn in panelButtons" :key="btn.panel" v-slot="{ active }">
            <button
              class="panel-menu-item"
              :class="{ highlight: active, current: isCurrent(btn.panel) }"
              :aria-current="isCurrent(btn.panel) ? 'true' : undefined"
              @click="emit('toggle-panel', btn.panel)"
            >
              <component :is="btn.icon" class="w-4 h-4 pmi-icon" />
              <span class="pmi-label">{{ btn.label }}</span>
              <span v-if="btn.panel === 'changes' && diffStat" class="pmi-stat">
                <span style="color: var(--color-success-fg)">+{{ diffStat.additions }}</span>
                <span style="color: var(--color-error-fg)">-{{ diffStat.deletions }}</span>
              </span>
              <span class="pmi-key">{{ btn.shortcut }}</span>
            </button>
          </MenuItem>
        </MenuItems>
      </transition>
    </HMenu>
  </div>
</template>

<style scoped>
/* Floated into the top-right of the window's title-bar zone (anchored to the
   position:relative .app-shell). z above the drag strip so it stays clickable. */
.topbar-control {
  position: absolute;
  top: 7px;
  right: 14px;
  /* Above the title-bar drag strip (45) so it stays clickable, but below
     --z-modal (50) so the Settings overlay covers it. */
  z-index: 46;
  font-family: var(--font-sans);
}

.panel-menu {
  position: relative;
  display: inline-flex;
}

.panel-menu-btn {
  position: relative;
  display: inline-flex;
  align-items: center;
  gap: 2px;
  height: 28px;
  padding: 0 7px;
  border: 1px solid var(--color-border);
  border-radius: var(--radius-lg);
  background: var(--color-background);
  color: var(--color-muted-foreground);
  cursor: pointer;
  transition: background 0.15s, color 0.15s, border-color 0.15s;
}

.panel-menu-btn:hover {
  color: var(--color-foreground);
  border-color: var(--color-foreground);
}

.panel-menu-btn.open {
  background: var(--color-muted);
  color: var(--color-foreground);
}

.panel-menu-caret {
  opacity: 0.6;
}

/* Small live-status dot on the corner of the button. The border matches the
   shell tone so the dot reads as a separate indicator. */
.status-dot {
  width: 8px;
  height: 8px;
  border-radius: var(--radius-pill);
  flex-shrink: 0;
}
.panel-status-dot {
  position: absolute;
  top: 4px;
  right: 4px;
  width: 7px;
  height: 7px;
  border: 1.5px solid var(--color-background);
}

.panel-menu-items {
  position: absolute;
  top: calc(100% + 6px);
  right: 0;
  min-width: 224px;
  padding: 4px;
  background: var(--color-surface);
  border: 1px solid var(--color-border);
  border-radius: var(--radius-lg);
  box-shadow: var(--shadow-md);
  z-index: var(--z-dropdown);
  outline: none;
}

.panel-menu-item {
  display: flex;
  align-items: center;
  gap: 10px;
  width: 100%;
  padding: 7px 8px;
  border: none;
  background: transparent;
  border-radius: var(--radius-md);
  color: var(--color-foreground);
  font-size: 13px;
  text-align: left;
  cursor: pointer;
}

.panel-menu-item.highlight {
  background: var(--color-muted);
}

.pmi-icon {
  color: var(--color-muted-foreground);
  flex-shrink: 0;
}

.panel-menu-item.current .pmi-icon {
  color: var(--color-primary);
}

.pmi-label {
  flex: 1;
  white-space: nowrap;
}

.pmi-key {
  color: var(--color-muted-foreground);
  font-size: 11px;
  letter-spacing: 0.04em;
  flex-shrink: 0;
}

/* Working-tree diff stat shown inline on the Changes menu item. */
.pmi-stat {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  font-family: var(--font-mono);
  font-size: 11px;
  flex-shrink: 0;
}

/* Menu transition */
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
