// Project management store using localStorage
import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import type { Project } from '@/types/api'
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
  }
})
