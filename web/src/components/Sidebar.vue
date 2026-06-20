<!-- eslint-disable vue/multi-word-component-names -->
<script setup lang="ts">
import { ref, computed, onMounted, watch, inject } from 'vue'
import {
  Menu as HMenu,
  MenuButton,
  MenuItems,
  MenuItem,
} from '@headlessui/vue'
import {
  ChevronRight,
  Folder,
  FolderOpen,
  Server,
  Plus,
  MoreHorizontal,
  Pin,
  Archive,
  ArchiveRestore,
  Pencil,
  Trash2,
  MailOpen,
} from 'lucide-vue-next'
import { useChatStore } from '@/stores/chat'
import { useProjectStore, isRemotePath, parseRemoteLabel } from '@/stores/project'
import type { TaskItem, RemoteMeta } from '@/types/api'

const store = useChatStore()
const projectStore = useProjectStore()
const openRemoteConnect = inject<(prefill?: RemoteMeta & { loadTaskUuid?: string }) => void>('openRemoteConnect')

defineProps<{
  resolvedTheme: 'light' | 'dark'
}>()

const emit = defineEmits<{
  openFile: [path: string, content: string]
  openSettings: []
  openProjects: []
  toggleTheme: []
}>()

// Expanded project paths. The active project is auto-expanded.
const expanded = ref<Set<string>>(new Set())
const showArchived = ref(false)

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

// Project nodes sorted with the active project first, then alphabetically.
const projectNodes = computed(() => {
  const nodes = [...projectStore.projectsForTree]
  return nodes.sort((a, b) => {
    if (a.path === activePath.value) return -1
    if (b.path === activePath.value) return 1
    return a.name.localeCompare(b.name)
  })
})

function tasksFor(path: string): TaskItem[] {
  const list = projectStore.tasksByProject[path] || []
  return showArchived.value ? list : list.filter((t) => !t.archived)
}

function visibleCount(path: string): number {
  return tasksFor(path).length
}

async function refresh() {
  await projectStore.fetchAllTasks()
}

onMounted(() => {
  refresh()
  if (activePath.value) expanded.value = new Set([activePath.value])
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
  if (isRemotePath(path)) return Server
  return path === activePath.value ? FolderOpen : Folder
}

function isActiveTask(task: TaskItem): boolean {
  return task.uuid === store.currentSessionId && activePath.value === task.project
}

async function handleDelete(task: TaskItem) {
  await store.deleteSession(task.uuid)
  await refresh()
}

async function renameTask(task: TaskItem) {
  const title = window.prompt('Rename task', task.title || '')
  if (title != null && title.trim()) {
    await projectStore.updateTaskMeta(task.uuid, { title: title.trim() })
  }
}

function taskTitle(t: TaskItem): string {
  return t.title || t.uuid.slice(0, 8) + '…'
}

function relativeTime(ts: string): string {
  if (!ts) return ''
  const then = new Date(ts).getTime()
  if (Number.isNaN(then)) return ''
  const mins = Math.floor((Date.now() - then) / 60000)
  if (mins < 1) return 'now'
  if (mins < 60) return `${mins}m`
  const hrs = Math.floor(mins / 60)
  if (hrs < 24) return `${hrs}h`
  const days = Math.floor(hrs / 24)
  if (days < 30) return `${days}d`
  return new Date(ts).toLocaleDateString([], { month: 'short', day: 'numeric' })
}
</script>

<template>
  <aside class="sidebar">
    <!-- New task -->
    <div class="sidebar-header">
      <button class="new-task-btn" @click="store.newSession()">
        <Plus :size="16" />
        <span>New task</span>
      </button>
    </div>

    <!-- Workspace tree -->
    <div class="tree">
      <div class="tree-head">
        <span class="tree-label">Workspace</span>
      </div>

      <div v-if="projectNodes.length === 0" class="empty-state">No projects yet</div>

      <div v-for="proj in projectNodes" :key="proj.path" class="project-group">
        <button class="project-row" :class="{ active: proj.path === activePath }" @click="toggle(proj.path)">
          <ChevronRight :size="14" class="proj-chevron" :class="{ open: isExpanded(proj.path) }" />
          <component :is="projIcon(proj.path)" :size="15" class="proj-icon" />
          <span class="proj-name">{{ proj.name }}</span>
          <span v-if="visibleCount(proj.path) > 0" class="proj-count">{{ visibleCount(proj.path) }}</span>
        </button>

        <div v-show="isExpanded(proj.path)" class="task-list">
          <div v-if="visibleCount(proj.path) === 0" class="task-empty">No tasks</div>
          <div
            v-for="task in tasksFor(proj.path)"
            :key="task.uuid"
            class="task-row"
            :class="{ active: isActiveTask(task), archived: task.archived }"
            @click="openTask(task)"
          >
            <span class="task-dot" :class="{ unread: task.unread }" aria-hidden="true" />
            <Pin v-if="task.pinned" :size="11" class="task-pin" />
            <span class="task-title">{{ taskTitle(task) }}</span>
            <span class="task-time">{{ relativeTime(task.created_at) }}</span>

            <HMenu as="div" class="task-menu" @click.stop>
              <MenuButton class="task-menu-btn" title="Task actions" @click.stop="onTaskMenuClick($event, task.uuid)">
                <MoreHorizontal :size="14" />
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
                      <Pin :size="14" /> {{ task.pinned ? 'Unpin' : 'Pin' }}
                    </button>
                  </MenuItem>
                  <MenuItem v-slot="{ active }">
                    <button class="tmi" :class="{ hl: active }" @click.stop="renameTask(task)">
                      <Pencil :size="14" /> Rename
                    </button>
                  </MenuItem>
                  <MenuItem v-slot="{ active }">
                    <button class="tmi" :class="{ hl: active }" @click.stop="projectStore.updateTaskMeta(task.uuid, { archived: !task.archived })">
                      <component :is="task.archived ? ArchiveRestore : Archive" :size="14" /> {{ task.archived ? 'Unarchive' : 'Archive' }}
                    </button>
                  </MenuItem>
                  <MenuItem v-slot="{ active }">
                    <button class="tmi" :class="{ hl: active }" @click.stop="projectStore.updateTaskMeta(task.uuid, { unread: !task.unread })">
                      <MailOpen :size="14" /> {{ task.unread ? 'Mark read' : 'Mark unread' }}
                    </button>
                  </MenuItem>
                  <div class="tmi-sep" />
                  <MenuItem v-slot="{ active }">
                    <button class="tmi danger" :class="{ hl: active }" @click.stop="handleDelete(task)">
                      <Trash2 :size="14" /> Delete
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
        <button class="footer-btn" @click="emit('toggleTheme')" :title="resolvedTheme === 'dark' ? 'Switch to light mode' : 'Switch to dark mode'">
          <svg v-if="resolvedTheme === 'dark'" class="w-3.5 h-3.5" viewBox="0 0 20 20" fill="currentColor">
            <path d="M10 2a.75.75 0 01.75.75v1.5a.75.75 0 01-1.5 0v-1.5A.75.75 0 0110 2zM10 15a.75.75 0 01.75.75v1.5a.75.75 0 01-1.5 0v-1.5A.75.75 0 0110 15zM10 7a3 3 0 100 6 3 3 0 000-6zM15.657 5.404a.75.75 0 10-1.06-1.06l-1.061 1.06a.75.75 0 001.06 1.06l1.06-1.06zM6.464 14.596a.75.75 0 10-1.06-1.06l-1.06 1.06a.75.75 0 001.06 1.06l1.06-1.06zM18 10a.75.75 0 01-.75.75h-1.5a.75.75 0 010-1.5h1.5A.75.75 0 0118 10zM5 10a.75.75 0 01-.75.75h-1.5a.75.75 0 010-1.5h1.5A.75.75 0 015 10zM14.596 15.657a.75.75 0 001.06-1.06l-1.06-1.061a.75.75 0 10-1.06 1.06l1.06 1.06zM5.404 6.464a.75.75 0 001.06-1.06l-1.06-1.06a.75.75 0 10-1.06 1.06l1.06 1.06z" />
          </svg>
          <svg v-else class="w-3.5 h-3.5" viewBox="0 0 20 20" fill="currentColor">
            <path fill-rule="evenodd" d="M7.455 2.004a.75.75 0 01.26.77 7 7 0 009.958 7.967.75.75 0 011.067.853A8.5 8.5 0 116.647 1.921a.75.75 0 01.808.083z" clip-rule="evenodd" />
          </svg>
        </button>
        <button class="footer-btn" @click="emit('openSettings')" title="Settings">
          <svg class="w-3.5 h-3.5" viewBox="0 0 20 20" fill="currentColor">
            <path fill-rule="evenodd" d="M7.84 1.804A1 1 0 018.82 1h2.36a1 1 0 01.98.804l.331 1.652a6.993 6.993 0 011.929 1.115l1.598-.54a1 1 0 011.186.447l1.18 2.044a1 1 0 01-.205 1.251l-1.267 1.113a7.047 7.047 0 010 2.228l1.267 1.113a1 1 0 01.206 1.25l-1.18 2.045a1 1 0 01-1.187.447l-1.598-.54a6.993 6.993 0 01-1.929 1.115l-.33 1.652a1 1 0 01-.98.804H8.82a1 1 0 01-.98-.804l-.331-1.652a6.993 6.993 0 01-1.929-1.115l-1.598.54a1 1 0 01-1.186-.447l-1.18-2.044a1 1 0 01.205-1.251l1.267-1.114a7.05 7.05 0 010-2.227L1.821 7.773a1 1 0 01-.206-1.25l1.18-2.045a1 1 0 011.187-.447l1.598.54A6.993 6.993 0 017.51 3.456l.33-1.652zM10 13a3 3 0 100-6 3 3 0 000 6z" clip-rule="evenodd" />
          </svg>
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
.tree-head-actions {
  display: flex;
  gap: 2px;
}
.tree-icon-btn {
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
.tree-icon-btn:hover {
  background: var(--color-muted);
  color: var(--color-foreground);
}
.tree-icon-btn.on {
  color: var(--color-primary);
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
  color: var(--color-primary);
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
.proj-count {
  font-size: 10px;
  font-family: var(--font-mono);
  color: var(--color-muted-foreground);
  flex-shrink: 0;
}

.task-list {
  padding-left: 14px;
}
.task-empty {
  font-size: 11px;
  color: var(--color-muted-foreground);
  padding: 5px 8px;
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
  background: var(--accent-wash-soft);
  border-left-color: var(--color-primary);
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
  background: var(--color-primary);
}
.task-pin {
  color: var(--color-primary);
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
.task-time {
  font-size: 10px;
  font-family: var(--font-mono);
  color: var(--color-muted-foreground);
  flex-shrink: 0;
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
}
.footer-btn svg {
  width: 18px;
  height: 18px;
}
.footer-btn:hover {
  color: var(--color-foreground);
}
</style>
