// Project management store using localStorage
import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import type { Project, RemoteMeta, TaskItem, TaskMetaPatch } from '@/types/api'
import { api } from '@/composables/api'

// A remote workspace is identified by a host-qualified label:
// ssh://user@host:port/remote/path
export function isRemotePath(path: string): boolean {
  return path.startsWith('ssh://')
}

// parseRemoteLabel decomposes a remote project label into the pieces the SSH
// wizard needs to reconnect. Returns null for non-remote paths.
export function parseRemoteLabel(label: string): RemoteMeta | null {
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
  return { host: hostPort, user, port, remotePath }
}

const STORAGE_KEY = 'jcode_projects'
const ACTIVE_KEY = 'jcode_active_project'

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
    projects.value = projects.value.filter((p) => p.id !== id)
    saveProjects(projects.value)
    if (activeId.value === id) {
      activeId.value = ''
      localStorage.removeItem(ACTIVE_KEY)
    }
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
    // wizard instead.
    if (project.remote) {
      switchError.value = 'Remote workspace — reconnect via the SSH wizard'
      return false
    }

    switching.value = true
    switchError.value = ''
    try {
      await api.switchProject(project.path)
      setActive(id)
      return true
    } catch (err: unknown) {
      switchError.value = err instanceof Error ? err.message : 'Failed to switch project'
      return false
    } finally {
      switching.value = false
    }
  }

  async function openProject(path: string): Promise<boolean> {
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

  // Tasks grouped by project path, sorted newest-first, pinned on top.
  const tasksByProject = computed(() => {
    const map: Record<string, TaskItem[]> = {}
    for (const t of allTasks.value) {
      ;(map[t.project] ??= []).push(t)
    }
    for (const path in map) {
      const list = map[path]
      if (!list) continue
      list.sort((a, b) => {
        if (a.pinned !== b.pinned) return a.pinned ? -1 : 1
        return (b.created_at || '').localeCompare(a.created_at || '')
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
    setActive,
    switchToProject,
    openProject,
    ensureCurrentProject,
    projectName,
    nameForPath,
    allTasks,
    fetchAllTasks,
    tasksByProject,
    projectsForTree,
    updateTaskMeta,
  }
})
