// Project management store using localStorage
import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import type { Project, TaskItem, TaskMetaPatch } from '@/types/api'
import { api } from '@/composables/api'

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
