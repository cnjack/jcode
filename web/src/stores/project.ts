// Project management store using localStorage
import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import { i18n } from '@/i18n'
import type { Project, RemoteMeta, TaskItem, TaskMetaPatch } from '@/types/api'
import { api } from '@/composables/api'

// A remote workspace is identified by a scheme-qualified label:
//   ssh://user@host:port/remote/path   or   docker://container/path
export function isRemotePath(path: string): boolean {
  return path.startsWith('ssh://') || path.startsWith('docker://')
}

// parseRemoteLabel decomposes a remote project label into the pieces the
// wizard needs to reconnect. Returns null for non-remote paths.
export function parseRemoteLabel(label: string): RemoteMeta | null {
  if (label.startsWith('docker://')) {
    const rest = label.slice('docker://'.length)
    const slash = rest.indexOf('/')
    const container = slash < 0 ? rest : rest.slice(0, slash)
    const remotePath = slash < 0 ? '/' : rest.slice(slash)
    return { kind: 'docker', host: '', user: '', port: 0, remotePath, container }
  }
  if (!isRemotePath(label)) return null
  const rest = label.slice('ssh://'.length)
  const at = rest.indexOf('@')
  if (at < 0) return null
  const user = rest.slice(0, at)
  const afterUser = rest.slice(at + 1)
  const slash = afterUser.indexOf('/')
  const hostPort = slash < 0 ? afterUser : afterUser.slice(0, slash)
  const remotePath = slash < 0 ? '/' : afterUser.slice(slash)
  const colon = hostPort.lastIndexOf(':')
  const port = colon >= 0 ? parseInt(hostPort.slice(colon + 1), 10) || 22 : 22
  return { kind: 'ssh', host: hostPort, user, port, remotePath }
}

const STORAGE_KEY = 'jcode_projects'
const ACTIVE_KEY = 'jcode_active_project'
const FILTERS_KEY = 'jcode_sidebar_filters'

// Sidebar filter / grouping / sorting preferences (driven by the filter menu).
export type StatusFilter = 'active' | 'archived' | 'all'
export type LastActivityFilter = 'all' | 'today' | 'week' | 'month'
export type GroupByMode = 'project' | 'date'
export type SortByMode = 'recency' | 'name' | 'created'

export interface SidebarFilters {
  status: StatusFilter
  project: string // '' = all projects, otherwise a project path
  lastActivity: LastActivityFilter
  groupBy: GroupByMode
  sortBy: SortByMode
}

export const DEFAULT_FILTERS: SidebarFilters = {
  status: 'active',
  project: '',
  lastActivity: 'all',
  groupBy: 'project',
  sortBy: 'recency',
}

function loadFilters(): SidebarFilters {
  try {
    const raw = localStorage.getItem(FILTERS_KEY)
    // Spread over defaults so a newly-added key is filled in for old installs.
    return raw ? { ...DEFAULT_FILTERS, ...JSON.parse(raw) } : { ...DEFAULT_FILTERS }
  } catch {
    return { ...DEFAULT_FILTERS }
  }
}

function loadProjects(): Project[] {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    return raw ? JSON.parse(raw) : []
  } catch {
    return []
  }
}

function saveProjects(projects: Project[]) {
  localStorage.setItem(STORAGE_KEY, JSON.stringify(projects))
}

export const useProjectStore = defineStore('project', () => {
  const projects = ref<Project[]>(loadProjects())
  const activeId = ref<string>(localStorage.getItem(ACTIVE_KEY) || '')

  // Sidebar filter/sort/group prefs — persisted so they survive reloads.
  const filters = ref<SidebarFilters>(loadFilters())
  function setFilters(patch: Partial<SidebarFilters>) {
    filters.value = { ...filters.value, ...patch }
    localStorage.setItem(FILTERS_KEY, JSON.stringify(filters.value))
  }
  function resetFilters() {
    setFilters({ ...DEFAULT_FILTERS })
  }

  const activeProject = computed(() =>
    projects.value.find((p) => p.id === activeId.value) || null,
  )

  function addProject(path: string): Project {
    // Deduplicate by path
    const existing = projects.value.find((p) => p.path === path)
    if (existing) return existing
    const project: Project = {
      id: `proj_${Date.now()}_${Math.random().toString(36).slice(2, 8)}`,
      path,
      createdAt: Date.now(),
    }
    projects.value.push(project)
    saveProjects(projects.value)
    return project
  }

  // upsertRemoteProject records a bound remote workspace (keyed by its
  // host-qualified label) and returns it. Unlike addProject it carries remote
  // metadata so the tree can render it distinctly and offer reconnect.
  function upsertRemoteProject(label: string, remote: RemoteMeta): Project {
    const existing = projects.value.find((p) => p.path === label)
    if (existing) {
      existing.remote = remote
      saveProjects(projects.value)
      return existing
    }
    const project: Project = {
      id: `proj_${Date.now()}_${Math.random().toString(36).slice(2, 8)}`,
      path: label,
      createdAt: Date.now(),
      remote,
    }
    projects.value.push(project)
    saveProjects(projects.value)
    return project
  }

  function removeProject(id: string) {
    const removed = projects.value.find((p) => p.id === id)
    projects.value = projects.value.filter((p) => p.id !== id)
    saveProjects(projects.value)
    if (activeId.value === id) {
      activeId.value = ''
      localStorage.removeItem(ACTIVE_KEY)
    }
    // Drop a single-project filter pointing at the project we just removed, so a
    // stale path can't silently reactivate when that path later reappears.
    if (removed && filters.value.project === removed.path) {
      setFilters({ project: '' })
    }
  }

  // removeProjectByPath drops a workspace by its path (used when its last
  // conversation is deleted). No-op if the path isn't a tracked project — paths
  // that only existed because they had tasks fall out of the tree on their own.
  function removeProjectByPath(path: string) {
    const proj = projects.value.find((p) => p.path === path)
    if (proj) removeProject(proj.id)
  }

  function setActive(id: string) {
    activeId.value = id
    localStorage.setItem(ACTIVE_KEY, id)
  }

  const switching = ref(false)
  const switchError = ref('')

  async function switchToProject(id: string): Promise<boolean> {
    const project = projects.value.find((p) => p.id === id)
    if (!project) return false
    // If already active, no-op.
    if (activeId.value === id) return true

    // Remote workspaces cannot be re-activated by a local path switch (the
    // backend would `stat` a path that only exists on the remote host, and we
    // never persist the SSH secret). Callers must route these through the SSH
    // wizard instead. Guard on the label too (not just the .remote metadata):
    // a project added from a task whose path is an ssh:// label may lack the
    // metadata, and must still not fall through to a doomed local switch.
    if (project.remote || isRemotePath(project.path)) {
      switchError.value = i18n.global.t('errors.remoteReconnect')
      return false
    }

    switching.value = true
    switchError.value = ''
    try {
      await api.switchProject(project.path)
      setActive(id)
      return true
    } catch (err: unknown) {
      switchError.value = err instanceof Error ? err.message : i18n.global.t('errors.switchFailed')
      return false
    } finally {
      switching.value = false
    }
  }

  async function openProject(path: string): Promise<boolean> {
    // A remote (ssh://) label can't be opened as a local workspace — don't even
    // persist it as a bare local project; signal the caller to use the SSH
    // wizard instead (matches ProjectSwitcher.selectProject's remote handling).
    if (isRemotePath(path)) {
      switchError.value = i18n.global.t('errors.remoteReconnect')
      return false
    }
    const proj = addProject(path)
    return switchToProject(proj.id)
  }

  function ensureCurrentProject(pwd: string) {
    // Auto-create project for current workspace if none exists
    const existing = projects.value.find((p) => p.path === pwd)
    if (existing) {
      if (!activeId.value) setActive(existing.id)
      return
    }
    const proj = addProject(pwd)
    setActive(proj.id)
  }

  function projectName(p: Project): string {
    return p.path.split('/').filter(Boolean).pop() || p.path
  }

  function nameForPath(path: string): string {
    return path.split('/').filter(Boolean).pop() || path
  }

  // ─── Cross-project tasks (for the sidebar tree) ───
  const allTasks = ref<TaskItem[]>([])

  async function fetchAllTasks() {
    try {
      allTasks.value = await api.tasks()
    } catch {
      allTasks.value = []
    }
  }

  // Local workspace paths that no longer exist on disk. Populated by
  // validateProjectPaths and used to hide dead entries from the picker, so the
  // user never clicks one and hits "path does not exist or is not a directory".
  const missingPaths = ref<Set<string>>(new Set())

  async function validateProjectPaths() {
    // Gather every known local workspace path (localStorage projects + paths that
    // have tasks). Remote (ssh://) labels can't be stat'd locally, so they're
    // never validated and never marked missing.
    const paths = new Set<string>()
    for (const p of projects.value) if (!isRemotePath(p.path)) paths.add(p.path)
    for (const t of allTasks.value) if (!isRemotePath(t.project)) paths.add(t.project)
    if (paths.size === 0) {
      missingPaths.value = new Set()
      return
    }
    try {
      const { missing } = await api.validatePaths([...paths])
      missingPaths.value = new Set(missing)
    } catch {
      // If the check itself fails, hide nothing — a stale-but-visible workspace
      // is far less surprising than an entire list vanishing on a network blip.
      missingPaths.value = new Set()
    }
  }

  // Tasks grouped by project path, ordered: live work first, then pinned, then
  // newest activity. Putting running tasks above pinned ones means whatever is
  // actively generating is always the top row of its workspace.
  const tasksByProject = computed(() => {
    const map: Record<string, TaskItem[]> = {}
    for (const t of allTasks.value) {
      ;(map[t.project] ??= []).push(t)
    }
    for (const path in map) {
      const list = map[path]
      if (!list) continue
      list.sort((a, b) => {
        if (!!a.running !== !!b.running) return a.running ? -1 : 1
        if (a.pinned !== b.pinned) return a.pinned ? -1 : 1
        // Newest activity first.
        const at = a.updated_at || a.created_at || ''
        const bt = b.updated_at || b.created_at || ''
        return bt.localeCompare(at)
      })
    }
    return map
  })

  // The project nodes to render: known (localStorage) projects unioned with any
  // project path that has tasks, so nothing is hidden just because it wasn't
  // explicitly opened.
  const projectsForTree = computed(() => {
    const paths = new Set<string>(projects.value.map((p) => p.path))
    for (const t of allTasks.value) paths.add(t.project)
    return [...paths].map((path) => {
      const known = projects.value.find((p) => p.path === path)
      return { id: known?.id ?? '', path, name: nameForPath(path) }
    })
  })

  // setTaskRunning optimistically marks a task running/idle in the sidebar from a
  // task_status WS event (the authoritative live state is re-synced by a
  // following fetchAllTasks).
  function setTaskRunning(uuid: string, running: boolean) {
    const t = allTasks.value.find((x) => x.uuid === uuid)
    if (t) {
      t.running = running
      if (running) t.updated_at = new Date().toISOString()
    }
  }

  async function updateTaskMeta(uuid: string, patch: TaskMetaPatch) {
    // Optimistic local update, then persist.
    const t = allTasks.value.find((x) => x.uuid === uuid)
    if (t) Object.assign(t, patch)
    try {
      await api.updateTask(uuid, patch)
    } catch {
      await fetchAllTasks() // resync on failure
    }
  }

  return {
    projects,
    activeId,
    activeProject,
    switching,
    switchError,
    addProject,
    upsertRemoteProject,
    removeProject,
    removeProjectByPath,
    setActive,
    switchToProject,
    openProject,
    ensureCurrentProject,
    projectName,
    nameForPath,
    allTasks,
    fetchAllTasks,
    missingPaths,
    validateProjectPaths,
    setTaskRunning,
    tasksByProject,
    projectsForTree,
    updateTaskMeta,
    filters,
    setFilters,
    resetFilters,
  }
})
