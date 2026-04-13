// Project management store using localStorage
import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import type { Project } from '@/types/api'

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

  function addProject(name: string, path: string): Project {
    const project: Project = {
      id: `proj_${Date.now()}_${Math.random().toString(36).slice(2, 8)}`,
      name,
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

  function ensureCurrentProject(pwd: string) {
    // Auto-create project for current workspace if none exists
    const existing = projects.value.find((p) => p.path === pwd)
    if (existing) {
      if (!activeId.value) setActive(existing.id)
      return
    }
    const name = pwd.split('/').filter(Boolean).pop() || 'workspace'
    const proj = addProject(name, pwd)
    setActive(proj.id)
  }

  return {
    projects,
    activeId,
    activeProject,
    addProject,
    removeProject,
    setActive,
    ensureCurrentProject,
  }
})
