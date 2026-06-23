<!-- eslint-disable vue/multi-word-component-names -->
<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted, watch, nextTick, inject } from 'vue'
import {
  Menu as HMenu,
  MenuButton,
  MenuItems,
  MenuItem,
} from '@headlessui/vue'
import {
  ChevronRightIcon,
  FolderIcon,
  FolderOpenIcon,
  ServerIcon,
  PlusIcon,
  EllipsisHorizontalIcon,
  BookmarkIcon,
  ArchiveBoxIcon,
  ArchiveBoxArrowDownIcon,
  PencilIcon,
  TrashIcon,
  EnvelopeOpenIcon,
  SunIcon,
  MoonIcon,
  Cog6ToothIcon,
  BoltIcon,
} from '@heroicons/vue/24/outline'
import { useI18n } from 'vue-i18n'
import { useChatStore } from '@/stores/chat'
import { useProjectStore, isRemotePath, parseRemoteLabel } from '@/stores/project'
import type { TaskItem, RemoteMeta } from '@/types/api'
import SidebarFilterMenu from '@/components/SidebarFilterMenu.vue'

const store = useChatStore()
const { t } = useI18n()
const projectStore = useProjectStore()
const openRemoteConnect = inject<(prefill?: RemoteMeta & { loadTaskUuid?: string }) => void>('openRemoteConnect')
const startNewTaskInProject = inject<(path: string) => Promise<boolean>>('onNewTaskInProject')

defineProps<{
  resolvedTheme: 'light' | 'dark'
}>()

const emit = defineEmits<{
  openFile: [path: string, content: string]
  openSettings: []
  openProjects: []
  openAutomations: []
  toggleTheme: []
}>()

// Expanded project paths. The active project is auto-expanded.
const expanded = ref<Set<string>>(new Set())

// A coarse reactive clock so time-based filtering/grouping (last-activity window,
// date buckets) re-evaluate as the wall clock advances — Date.now()/new Date()
// alone aren't reactive deps, so without this rows would go stale until an
// unrelated refresh.
const now = ref(Date.now())
let clockTimer: ReturnType<typeof setInterval> | null = null

// The task "⋯" menu opens downward by default; for rows near the bottom of the
// sidebar that clips it, so flip it upward when there isn't room below.
const flipUpMenus = ref<Set<string>>(new Set())
function onTaskMenuClick(e: MouseEvent, uuid: string) {
  const rect = (e.currentTarget as HTMLElement).getBoundingClientRect()
  const next = new Set(flipUpMenus.value)
  if (rect.bottom + 200 > window.innerHeight - 12) next.add(uuid)
  else next.delete(uuid)
  flipUpMenus.value = next
}

const activePath = computed(() => projectStore.activeProject?.path || store.pwd)

function isExpanded(path: string): boolean {
  return expanded.value.has(path)
}
function toggle(path: string) {
  const next = new Set(expanded.value)
  if (next.has(path)) next.delete(path)
  else next.add(path)
  expanded.value = next
}

// ── Filter → group → sort pipeline (driven by projectStore.filters) ──

// Status: active hides archived, archived shows only archived, all = both.
function passesStatus(task: TaskItem): boolean {
  const s = projectStore.filters.status
  if (s === 'active') return !task.archived
  if (s === 'archived') return task.archived
  return true
}

// Last-activity window against updated_at (fallback created_at).
function withinActivity(ts: string): boolean {
  const f = projectStore.filters.lastActivity
  if (f === 'all') return true
  const then = new Date(ts).getTime()
  if (Number.isNaN(then)) return false
  const day = 86400000
  const span = f === 'today' ? day : f === 'week' ? 7 * day : 30 * day
  return now.value - then <= span
}

// The single-project filter, ignored if it points at a project that no longer
// exists (e.g. it was just deleted) so the list is never stranded empty.
const projectFilter = computed(() => {
  const p = projectStore.filters.project
  if (!p) return ''
  return projectStore.projectsForTree.some((n) => n.path === p) ? p : ''
})

const filteredTasks = computed(() =>
  projectStore.allTasks.filter((task) => {
    // The conversation you currently have open always stays visible & selectable,
    // regardless of filters — otherwise archiving (or a narrowing window) would
    // strand the open task with no row anywhere in the tree.
    if (task.uuid === store.currentSessionId && task.project === activePath.value) return true
    if (!passesStatus(task)) return false
    if (projectFilter.value && task.project !== projectFilter.value) return false
    if (!withinActivity(task.updated_at || task.created_at || '')) return false
    return true
  }),
)

// Within a group: live work first, then pinned, then the chosen sort key.
function taskComparator(a: TaskItem, b: TaskItem): number {
  if (!!a.running !== !!b.running) return a.running ? -1 : 1
  if (a.pinned !== b.pinned) return a.pinned ? -1 : 1
  const f = projectStore.filters.sortBy
  if (f === 'name') return taskTitle(a).localeCompare(taskTitle(b))
  if (f === 'created') return (b.created_at || '').localeCompare(a.created_at || '')
  const at = a.updated_at || a.created_at || ''
  const bt = b.updated_at || b.created_at || ''
  return bt.localeCompare(at)
}

// Date-bucket assignment for "group by date" (calendar-day based for today /
// yesterday, then rolling windows).
const DATE_BUCKETS = ['today', 'yesterday', 'week', 'month', 'older'] as const
function bucketFor(ts: string): string {
  const then = new Date(ts).getTime()
  if (Number.isNaN(then)) return 'older'
  const today = new Date(now.value)
  const startToday = new Date(today.getFullYear(), today.getMonth(), today.getDate()).getTime()
  const day = 86400000
  if (then >= startToday) return 'today'
  if (then >= startToday - day) return 'yesterday'
  if (then >= startToday - 7 * day) return 'week'
  if (then >= startToday - 30 * day) return 'month'
  return 'older'
}

function pushTask(map: Map<string, TaskItem[]>, key: string, task: TaskItem) {
  const arr = map.get(key)
  if (arr) arr.push(task)
  else map.set(key, [task])
}

// "+" on a workspace row: switch to that workspace and open a fresh welcome
// screen, so the next message starts a new task there. Remote workspaces must
// reconnect through the SSH wizard first (it lands on a fresh session too).
async function newTaskInProject(proj: { path: string }) {
  if (isRemotePath(proj.path)) {
    const meta = parseRemoteLabel(proj.path)
    if (meta) openRemoteConnect?.(meta)
    return
  }
  const ok = await startNewTaskInProject?.(proj.path)
  if (ok === false) return
  if (!expanded.value.has(proj.path)) toggle(proj.path)
}

interface SidebarGroup {
  kind: 'project' | 'date'
  key: string
  label: string
  path?: string // project kind only
  tasks: TaskItem[]
}

// Per-group ordering signals: surface live work, unread, then recency.
function aggregate(tasks: TaskItem[]) {
  let running = false
  let unread = false
  let lastTs = ''
  for (const task of tasks) {
    if (task.running) running = true
    if (task.unread) unread = true
    const ts = task.updated_at || task.created_at || ''
    if (ts > lastTs) lastTs = ts
  }
  return { running, unread, lastTs }
}

// The rendered list: filtered tasks bucketed by project or date, each group
// internally sorted. Project groups keep the active-first → running → unread →
// recency → name order; date groups run newest bucket first.
const sidebarGroups = computed<SidebarGroup[]>(() => {
  const tasks = filteredTasks.value

  if (projectStore.filters.groupBy === 'date') {
    const map = new Map<string, TaskItem[]>()
    for (const task of tasks) pushTask(map, bucketFor(task.updated_at || task.created_at || ''), task)
    return DATE_BUCKETS.filter((k) => map.has(k)).map((k) => ({
      kind: 'date' as const,
      key: k,
      label: t(`sidebar.dateBucket.${k}`),
      tasks: map.get(k)!.sort(taskComparator),
    }))
  }

  // Group by project.
  const map = new Map<string, TaskItem[]>()
  for (const task of tasks) pushTask(map, task.project, task)
  // Under a single-project filter the set is just that folder; otherwise every
  // project with a (filtered) task. The active project is added below so the
  // open conversation is never hidden.
  const paths = projectFilter.value ? new Set([projectFilter.value]) : new Set(map.keys())
  // Keep the active project's folder so the place you're working never vanishes.
  // Under a narrowing filter only keep it when it has a visible task (which now
  // includes the always-kept open conversation), so we don't synthesize a
  // misleading empty "No tasks" folder.
  const narrowing =
    !!projectFilter.value ||
    projectStore.filters.status === 'archived' ||
    projectStore.filters.lastActivity !== 'all'
  if (activePath.value && (map.has(activePath.value) || !narrowing)) {
    paths.add(activePath.value)
  }
  const groups: SidebarGroup[] = [...paths].map((path) => ({
    kind: 'project',
    key: path,
    path,
    label: projectStore.nameForPath(path),
    tasks: (map.get(path) || []).sort(taskComparator),
  }))
  return groups.sort((a, b) => {
    if (a.path === activePath.value) return -1
    if (b.path === activePath.value) return 1
    const A = aggregate(a.tasks)
    const B = aggregate(b.tasks)
    if (A.running !== B.running) return A.running ? -1 : 1
    if (A.unread !== B.unread) return A.unread ? -1 : 1
    if (A.lastTs !== B.lastTs) return B.lastTs.localeCompare(A.lastTs)
    return a.label.localeCompare(b.label)
  })
})

// When two project groups share a basename (e.g. ~/work/jack and ~/srv/jack),
// show the parent directory so they're tellable apart at a glance.
const duplicateNames = computed(() => {
  const counts = new Map<string, number>()
  for (const g of sidebarGroups.value) {
    if (g.kind === 'project') counts.set(g.label, (counts.get(g.label) || 0) + 1)
  }
  return new Set([...counts].filter(([, c]) => c > 1).map(([name]) => name))
})
function projHint(group: SidebarGroup): string {
  if (group.kind !== 'project' || !group.path) return ''
  if (!duplicateNames.value.has(group.label) || isRemotePath(group.path)) return ''
  const segs = group.path.split('/').filter(Boolean)
  return segs.length >= 2 ? segs[segs.length - 2] ?? '' : ''
}

async function refresh() {
  await projectStore.fetchAllTasks()
}

onMounted(() => {
  refresh()
  if (activePath.value) expanded.value = new Set([activePath.value])
  // Tick the clock once a minute so last-activity windows and date buckets
  // re-evaluate as time passes (incl. the midnight today→yesterday rollover).
  clockTimer = setInterval(() => { now.value = Date.now() }, 60000)
})

onUnmounted(() => {
  if (clockTimer) clearInterval(clockTimer)
})

// Keep the active project expanded and the tree fresh as the active session /
// session list changes (new task, send, delete).
watch(activePath, (p) => {
  if (p && !expanded.value.has(p)) toggle(p)
})
watch(() => store.sessions.length, refresh)
watch(() => store.currentSessionId, refresh)

async function openTask(task: TaskItem) {
  if (task.unread) projectStore.updateTaskMeta(task.uuid, { unread: false })

  // Remote tasks: if their workspace is already the active connection just load
  // the transcript; otherwise reconnect via the SSH wizard (it loads the task
  // after binding). We never persist the SSH secret, so a fresh connect is
  // required to continue the conversation.
  if (isRemotePath(task.project)) {
    if (activePath.value === task.project) {
      await store.loadSession(task.uuid)
    } else {
      const meta = parseRemoteLabel(task.project)
      if (meta) openRemoteConnect?.({ ...meta, loadTaskUuid: task.uuid })
    }
    return
  }

  if (activePath.value !== task.project) {
    const ok = await projectStore.openProject(task.project)
    if (!ok) return
    await store.fetchHealth()
  }
  await store.loadSession(task.uuid)
}

function projIcon(path: string) {
  if (isRemotePath(path)) return ServerIcon
  return path === activePath.value ? FolderOpenIcon : FolderIcon
}

function isActiveTask(task: TaskItem): boolean {
  return task.uuid === store.currentSessionId && activePath.value === task.project
}

async function handleDelete(task: TaskItem) {
  const path = task.project
  // Capture before mutating: deleting the conversation you're currently viewing
  // must also reset the chat view, otherwise the timeline stays rendered and
  // currentSessionId keeps pointing at the now-dead session (the next message
  // would be sent to it). Guarded so deleting a background task never disturbs
  // the open chat.
  const wasActive = isActiveTask(task)
  await store.deleteSession(task.uuid)
  await refresh()
  if (wasActive) {
    store.clearChat()
    store.currentSessionId = ''
    store.isRunning = false
  }
  // If that was the workspace's last conversation, drop the now-empty folder from
  // the tree too. Archived chats still count (tasksByProject keeps them), so a
  // folder with only archived conversations is preserved.
  if (!projectStore.tasksByProject[path]?.length) {
    projectStore.removeProjectByPath(path)
  }
}

// Inline rename — window.prompt is unreliable in the Tauri webview (can return
// null, silently no-op) and looks non-native, so edit the title in place.
const renamingUuid = ref('')
const renameValue = ref('')
const renameInput = ref<HTMLInputElement | null>(null)

async function renameTask(task: TaskItem) {
  renamingUuid.value = task.uuid
  renameValue.value = task.title || ''
  await nextTick()
  renameInput.value?.focus()
  renameInput.value?.select()
}

async function commitRename(task: TaskItem) {
  if (renamingUuid.value !== task.uuid) return
  const title = renameValue.value.trim()
  renamingUuid.value = ''
  if (title && title !== (task.title || '')) {
    await projectStore.updateTaskMeta(task.uuid, { title })
  }
}

function cancelRename() {
  renamingUuid.value = ''
}

function taskTitle(t: TaskItem): string {
  return t.title || t.uuid.slice(0, 8) + '…'
}

function relativeTime(ts: string): string {
  if (!ts) return ''
  const then = new Date(ts).getTime()
  if (Number.isNaN(then)) return ''
  const mins = Math.floor((Date.now() - then) / 60000)
  if (mins < 1) return t('sidebar.relativeTime.now')
  if (mins < 60) return t('sidebar.relativeTime.minutes', { n: mins })
  const hrs = Math.floor(mins / 60)
  if (hrs < 24) return t('sidebar.relativeTime.hours', { n: hrs })
  const days = Math.floor(hrs / 24)
  if (days < 30) return t('sidebar.relativeTime.days', { n: days })
  return new Date(ts).toLocaleDateString([], { month: 'short', day: 'numeric' })
}
</script>

<template>
  <aside class="sidebar">
    <!-- New task -->
    <div class="sidebar-header">
      <button class="new-task-btn" @click="store.newSession()">
        <PlusIcon class="w-4 h-4" />
        <span>{{ t('nav.newTask') }}</span>
      </button>
      <button class="nav-link-btn" @click="emit('openAutomations')" :title="t('nav.automations')">
        <BoltIcon class="w-4 h-4" />
        <span>{{ t('nav.automations') }}</span>
      </button>
    </div>

    <!-- Workspace tree -->
    <div class="tree">
      <div class="tree-head">
        <span class="tree-label">{{ t('nav.workspace') }}</span>
        <SidebarFilterMenu />
      </div>

      <div v-if="sidebarGroups.length === 0" class="empty-state">{{ t('sidebar.noProjects') }}</div>

      <div v-for="group in sidebarGroups" :key="group.kind + ':' + group.key" class="project-group">
        <!-- Project group → folder row (collapsible, with new-task + running cue). -->
        <div
          v-if="group.kind === 'project'"
          class="project-row"
          :class="{ active: group.path === activePath }"
          :title="group.path"
          role="button"
          tabindex="0"
          @click="toggle(group.key)"
          @keydown.enter.prevent="toggle(group.key)"
          @keydown.space.prevent="toggle(group.key)"
        >
          <ChevronRightIcon class="w-3.5 h-3.5 proj-chevron" :class="{ open: isExpanded(group.key) }" />
          <component :is="projIcon(group.path!)" class="w-3.5 h-3.5 proj-icon" />
          <span class="proj-name">{{ group.label }}</span>
          <span v-if="projHint(group)" class="proj-hint">{{ projHint(group) }}</span>
          <span
            v-if="!isExpanded(group.key) && group.tasks.some((tk) => tk.running)"
            class="task-running-ring proj-running-ring"
            :title="t('sidebar.running')"
            aria-hidden="true"
          />
          <button
            class="proj-add"
            :title="t('sidebar.newTaskHere')"
            :aria-label="t('sidebar.newTaskHere')"
            @click.stop="newTaskInProject({ path: group.path! })"
            @keydown.stop
          >
            <PlusIcon class="w-3.5 h-3.5" />
          </button>
          <span v-if="group.tasks.length > 0" class="proj-count">{{ group.tasks.length }}</span>
        </div>

        <!-- Date group → static bucket header (no folder affordances). -->
        <div v-else class="date-row">
          <span class="date-label">{{ group.label }}</span>
          <span class="proj-count">{{ group.tasks.length }}</span>
        </div>

        <div
          v-show="group.kind === 'date' || isExpanded(group.key)"
          class="task-list"
          :class="{ 'date-list': group.kind === 'date' }"
        >
          <div v-if="group.kind === 'project' && group.tasks.length === 0" class="task-empty">{{ t('sidebar.noTasks') }}</div>
          <div
            v-for="task in group.tasks"
            :key="task.uuid"
            class="task-row"
            :class="{ active: isActiveTask(task), archived: task.archived, running: task.running }"
            @click="openTask(task)"
          >
            <span
              v-if="task.running"
              class="task-running-ring"
              :title="t('sidebar.running')"
              aria-hidden="true"
            />
            <span v-else class="task-dot" :class="{ unread: task.unread }" aria-hidden="true" />
            <BookmarkIcon v-if="task.pinned" class="w-2.5 h-2.5 task-pin" />
            <input
              v-if="renamingUuid === task.uuid"
              :ref="el => { renameInput = el as HTMLInputElement | null }"
              v-model="renameValue"
              class="task-rename"
              @click.stop
              @keydown.enter.stop.prevent="commitRename(task)"
              @keydown.esc.stop.prevent="cancelRename"
              @blur="commitRename(task)"
            />
            <span v-else class="task-title">{{ taskTitle(task) }}</span>
            <span
              v-if="group.kind === 'date'"
              class="task-proj"
              :title="task.project"
            >{{ projectStore.nameForPath(task.project) }}</span>
            <span class="task-time" :class="{ running: task.running }">
              {{ task.running ? t('sidebar.running') : relativeTime(task.updated_at || task.created_at) }}
            </span>

            <HMenu as="div" class="task-menu" @click.stop>
              <MenuButton class="task-menu-btn" :title="t('sidebar.actions.taskActions')" @click.stop="onTaskMenuClick($event, task.uuid)">
                <EllipsisHorizontalIcon class="w-3.5 h-3.5" />
              </MenuButton>
              <transition
                enter-active-class="pop-enter-active"
                enter-from-class="pop-enter-from"
                leave-active-class="pop-leave-active"
                leave-to-class="pop-leave-to"
              >
                <MenuItems class="task-menu-items" :class="{ 'flip-up': flipUpMenus.has(task.uuid) }">
                  <MenuItem v-slot="{ active }">
                    <button class="tmi" :class="{ hl: active }" @click.stop="projectStore.updateTaskMeta(task.uuid, { pinned: !task.pinned })">
                      <BookmarkIcon class="w-3.5 h-3.5" /> {{ task.pinned ? t('sidebar.actions.unpin') : t('sidebar.actions.pin') }}
                    </button>
                  </MenuItem>
                  <MenuItem v-slot="{ active }">
                    <button class="tmi" :class="{ hl: active }" @click.stop="renameTask(task)">
                      <PencilIcon class="w-3.5 h-3.5" /> {{ t('sidebar.actions.rename') }}
                    </button>
                  </MenuItem>
                  <MenuItem v-slot="{ active }">
                    <button class="tmi" :class="{ hl: active }" @click.stop="projectStore.updateTaskMeta(task.uuid, { archived: !task.archived })">
                      <component :is="task.archived ? ArchiveBoxArrowDownIcon : ArchiveBoxIcon" class="w-3.5 h-3.5" /> {{ task.archived ? t('sidebar.actions.unarchive') : t('sidebar.actions.archive') }}
                    </button>
                  </MenuItem>
                  <MenuItem v-slot="{ active }">
                    <button class="tmi" :class="{ hl: active }" @click.stop="projectStore.updateTaskMeta(task.uuid, { unread: !task.unread })">
                      <EnvelopeOpenIcon class="w-3.5 h-3.5" /> {{ task.unread ? t('sidebar.actions.markRead') : t('sidebar.actions.markUnread') }}
                    </button>
                  </MenuItem>
                  <div class="tmi-sep" />
                  <MenuItem v-slot="{ active }">
                    <button class="tmi danger" :class="{ hl: active }" @click.stop="handleDelete(task)">
                      <TrashIcon class="w-3.5 h-3.5" /> {{ t('sidebar.actions.delete') }}
                    </button>
                  </MenuItem>
                </MenuItems>
              </transition>
            </HMenu>
          </div>
        </div>
      </div>
    </div>

    <!-- Footer -->
    <div class="sidebar-footer">
      <div class="footer-actions">
        <button class="footer-btn" @click="emit('toggleTheme')" :title="resolvedTheme === 'dark' ? t('nav.switchToLight') : t('nav.switchToDark')">
          <SunIcon v-if="resolvedTheme === 'dark'" class="w-3.5 h-3.5" />
          <MoonIcon v-else class="w-3.5 h-3.5" />
        </button>
        <button class="footer-btn" @click="emit('openSettings')" :title="t('nav.settingsWithShortcut')">
          <Cog6ToothIcon class="w-3.5 h-3.5" />
        </button>
      </div>
    </div>
  </aside>
</template>

<style scoped>
.sidebar {
  width: var(--sidebar-width);
  display: flex;
  flex-direction: column;
  flex-shrink: 0;
  position: relative;
  background: var(--color-background);
}

.sidebar-header {
  padding: 8px 12px 6px;
}

.new-task-btn {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 6px;
  width: 100%;
  padding: 9px 0;
  border: 1px solid var(--color-border);
  background: var(--color-surface);
  border-radius: var(--radius-lg);
  font-size: 13px;
  font-weight: 500;
  color: var(--color-foreground);
  cursor: pointer;
  transition: border-color 0.15s, box-shadow 0.15s, transform 0.08s var(--ease-out);
}
.new-task-btn:hover {
  border-color: color-mix(in srgb, var(--color-foreground) 32%, var(--color-border));
  box-shadow: var(--shadow-sm);
}
.new-task-btn:active {
  transform: translateY(0.5px);
}
.nav-link-btn {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 100%;
  margin-top: 6px;
  padding: 7px 10px;
  border: none;
  background: transparent;
  border-radius: var(--radius-lg);
  font-size: 13px;
  font-weight: 500;
  color: var(--color-muted-foreground);
  cursor: pointer;
  transition: background 0.15s, color 0.15s;
}
.nav-link-btn:hover {
  background: var(--color-muted);
  color: var(--color-foreground);
}

/* ─── Tree ─── */
.tree {
  flex: 1;
  overflow-y: auto;
  padding: 4px 8px 8px;
}

.tree-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 6px 6px 4px;
}
.tree-label {
  font-size: 10px;
  font-weight: 600;
  letter-spacing: 0.06em;
  text-transform: uppercase;
  color: var(--color-muted-foreground);
}

.empty-state {
  text-align: center;
  font-size: 11px;
  padding: 24px 0;
  color: var(--color-muted-foreground);
}

.project-group {
  margin-bottom: 2px;
}

.project-row {
  display: flex;
  align-items: center;
  gap: 6px;
  width: 100%;
  padding: 8px 6px;
  border: none;
  background: transparent;
  border-radius: var(--radius-md);
  cursor: pointer;
  text-align: left;
  transition: background 0.15s;
}
.project-row:hover {
  background: var(--color-muted);
}
.proj-chevron {
  color: var(--color-muted-foreground);
  flex-shrink: 0;
  transition: transform 0.15s;
}
.proj-chevron.open {
  transform: rotate(90deg);
}
.proj-icon {
  color: var(--color-muted-foreground);
  flex-shrink: 0;
}
.project-row.active .proj-icon {
  color: var(--color-accent-neutral);
}
.proj-name {
  flex: 1;
  min-width: 0;
  font-size: 13px;
  font-weight: 500;
  color: var(--color-foreground);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.proj-hint {
  font-size: 10px;
  color: var(--color-muted-foreground);
  flex-shrink: 0;
  opacity: 0.7;
  max-width: 80px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.proj-count {
  font-size: 10px;
  font-family: var(--font-mono);
  color: var(--color-muted-foreground);
  flex-shrink: 0;
}

/* "+" new-task affordance — hidden until the row is hovered/focused, mirroring
   the task row's ⋯ menu button so the tree stays calm at rest. */
.proj-add {
  display: grid;
  place-items: center;
  width: 20px;
  height: 20px;
  flex-shrink: 0;
  border: none;
  background: transparent;
  border-radius: var(--radius-sm);
  color: var(--color-muted-foreground);
  cursor: pointer;
  opacity: 0;
  transition: opacity 0.15s, background 0.15s, color 0.15s;
}
.project-row:hover .proj-add,
.project-row:focus-within .proj-add {
  opacity: 1;
}
.proj-add:hover {
  background: var(--color-secondary);
  color: var(--color-foreground);
}

.task-list {
  padding-left: 14px;
}
/* Date grouping is a flat chronological feed — drop the folder indent + rail. */
.task-list.date-list {
  padding-left: 0;
}
.task-empty {
  font-size: 11px;
  color: var(--color-muted-foreground);
  padding: 5px 8px;
}

/* Date-bucket header (Today / Yesterday / …) — a quiet label, not a folder. */
.date-row {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 10px 6px 4px;
}
.date-label {
  flex: 1;
  font-size: 10px;
  font-weight: 600;
  letter-spacing: 0.06em;
  text-transform: uppercase;
  color: var(--color-muted-foreground);
}

.task-row {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 8px 8px;
  margin-left: 6px;
  border-left: 1px solid var(--color-border);
  cursor: pointer;
  transition: background 0.15s;
  position: relative;
}
.task-row:hover {
  background: var(--color-muted);
}
.task-row.active {
  background: var(--neutral-wash-soft);
  border-left-color: var(--color-accent-neutral);
}
.task-row.archived {
  opacity: 0.55;
}

.task-dot {
  width: 6px;
  height: 6px;
  border-radius: var(--radius-pill);
  flex-shrink: 0;
  background: transparent;
}
.task-dot.unread {
  background: var(--color-accent-neutral);
}
/* Running indicator: a thin accent ring that breathes (scale + opacity) — a calm
   "working" cue matched to a clean ring aesthetic. */
.task-running-ring {
  width: 11px;
  height: 11px;
  flex-shrink: 0;
  border-radius: var(--radius-pill);
  border: 1.6px solid var(--color-accent);
  animation: task-ring-breathe 1.6s ease-in-out infinite;
}
.proj-running-ring {
  width: 9px;
  height: 9px;
  margin-left: auto;
}
@keyframes task-ring-breathe {
  0%,
  100% {
    opacity: 0.35;
    transform: scale(0.78);
  }
  50% {
    opacity: 1;
    transform: scale(1);
  }
}
/* A running task row gets a faint accent rail so live work reads at a glance. */
.task-row.running {
  border-left-color: color-mix(in srgb, var(--color-accent) 50%, var(--color-border));
}
/* Without motion the ring sits static at full opacity so it still clearly
   means "running". */
@media (prefers-reduced-motion: reduce) {
  .task-running-ring {
    animation: none;
    opacity: 1;
    transform: none;
  }
}
.task-pin {
  color: var(--color-accent-neutral);
  flex-shrink: 0;
}
.task-title {
  flex: 1;
  min-width: 0;
  font-size: 13px;
  color: var(--color-foreground);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.task-rename {
  flex: 1;
  min-width: 0;
  font-size: 13px;
  font-family: inherit;
  color: var(--color-foreground);
  background: var(--color-background);
  border: 1px solid var(--color-accent-neutral);
  border-radius: var(--radius-sm);
  padding: 1px 5px;
  outline: none;
}
.task-time {
  font-size: 10px;
  font-family: var(--font-mono);
  color: var(--color-muted-foreground);
  flex-shrink: 0;
}
.task-time.running {
  color: var(--color-accent);
}

/* In date grouping the rows aren't under a folder, so drop the tree rail/indent
   and show which project each task belongs to. */
.date-list .task-row {
  margin-left: 0;
  border-left: none;
  padding-left: 6px;
}
.task-proj {
  flex-shrink: 0;
  max-width: 90px;
  font-size: 10px;
  color: var(--color-muted-foreground);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  opacity: 0.75;
}

/* ─── Task action menu ─── */
.task-menu {
  position: relative;
  display: inline-flex;
  flex-shrink: 0;
}
.task-menu-btn {
  display: grid;
  place-items: center;
  width: 20px;
  height: 20px;
  border: none;
  background: transparent;
  border-radius: var(--radius-sm);
  color: var(--color-muted-foreground);
  cursor: pointer;
  opacity: 0;
  transition: opacity 0.15s, background 0.15s, color 0.15s;
}
.task-row:hover .task-menu-btn {
  opacity: 1;
}
.task-menu-btn:hover {
  background: var(--color-secondary);
  color: var(--color-foreground);
}
.task-menu-items.flip-up {
  top: auto;
  bottom: calc(100% + 4px);
}
.task-menu-items {
  position: absolute;
  top: calc(100% + 4px);
  right: 0;
  min-width: 160px;
  padding: 4px;
  background: var(--color-surface);
  border: 1px solid var(--color-border);
  border-radius: var(--radius-lg);
  box-shadow: var(--shadow-md);
  z-index: var(--z-dropdown);
  outline: none;
}
.tmi {
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
}
.tmi.hl {
  background: var(--color-muted);
}
.tmi.danger {
  color: var(--color-destructive);
}
.tmi-sep {
  height: 1px;
  margin: 4px 0;
  background: var(--color-border);
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

/* ─── Footer ─── */
.sidebar-footer {
  padding: 10px 12px;
  display: flex;
  align-items: center;
  justify-content: flex-end;
}
.footer-actions {
  display: flex;
  align-items: center;
  gap: 2px;
}
.footer-btn {
  width: 34px;
  height: 34px;
  display: flex;
  align-items: center;
  justify-content: center;
  border-radius: var(--radius-md);
  border: none;
  background: transparent;
  color: var(--color-muted-foreground);
  cursor: pointer;
  transition: background 0.15s, color 0.15s;
}
.footer-btn:hover {
  background: var(--color-muted);
  color: var(--color-foreground);
}
.footer-btn svg {
  width: 18px;
  height: 18px;
}
</style>
