import { defineStore } from 'pinia'
import { ref } from 'vue'
import { api } from '@/composables/api'
import type { AutomationItem, AutomationRun, AutomationTemplate, AutomationCreate, Automation } from '@/types/automation'

export const useAutomationStore = defineStore('automation', () => {
  const items = ref<AutomationItem[]>([])
  const runs = ref<AutomationRun[]>([])
  const templates = ref<AutomationTemplate[]>([])
  const loading = ref(false)
  const error = ref<string | null>(null)

  async function fetchAll() {
    loading.value = true
    error.value = null
    try {
      const [its, rns] = await Promise.all([api.automations(), api.automationRuns()])
      items.value = its
      runs.value = rns
    } catch (e) {
      error.value = e instanceof Error ? e.message : String(e)
    } finally {
      loading.value = false
    }
  }

  async function fetchTemplates() {
    if (templates.value.length) return
    try {
      templates.value = await api.automationTemplates()
    } catch {
      // non-fatal
    }
  }

  async function create(data: AutomationCreate): Promise<AutomationItem | null> {
    error.value = null
    try {
      const created = await api.automationCreate(data)
      await fetchAll()
      return created
    } catch (e) {
      error.value = e instanceof Error ? e.message : String(e)
      return null
    }
  }

  async function update(id: string, data: Partial<Automation>): Promise<boolean> {
    error.value = null
    try {
      await api.automationUpdate(id, data)
      await fetchAll()
      return true
    } catch (e) {
      error.value = e instanceof Error ? e.message : String(e)
      return false
    }
  }

  async function setEnabled(item: AutomationItem, enabled: boolean) {
    await update(item.id, { ...stripDerived(item), enabled })
  }

  async function remove(id: string) {
    try {
      await api.automationDelete(id)
      await fetchAll()
    } catch (e) {
      error.value = e instanceof Error ? e.message : String(e)
    }
  }

  async function runNow(id: string) {
    try {
      await api.automationRunNow(id)
      // Give the run a moment to register, then refresh recent runs.
      setTimeout(() => { void fetchAll() }, 1200)
    } catch (e) {
      error.value = e instanceof Error ? e.message : String(e)
    }
  }

  return { items, runs, templates, loading, error, fetchAll, fetchTemplates, create, update, setEnabled, remove, runNow }
})

// stripDerived returns just the editable automation fields (drops the derived
// human_schedule/badge/state) so a PUT round-trips cleanly.
function stripDerived(item: AutomationItem): Partial<Automation> {
  return {
    name: item.name,
    prompt: item.prompt,
    trigger: item.trigger,
    project_path: item.project_path,
    mode: item.mode,
    provider: item.provider,
    model: item.model,
    enabled: item.enabled,
  }
}
