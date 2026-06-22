import { defineStore } from 'pinia'
import { ref } from 'vue'
import { api } from '@/composables/api'
import type { UsageStats, TaskStats } from '@/types/api'

// Global usage-statistics store. Kept separate from chat.ts so the stats page
// (a lazily-rendered Settings tab) doesn't bloat the hot chat store.
export const useUsageStore = defineStore('usage', () => {
  const stats = ref<UsageStats | null>(null)
  const loading = ref(false)
  const error = ref<string | null>(null)
  const rangeDays = ref(30)

  // Per-task context-capacity stats, keyed by session UUID.
  const taskStats = ref<TaskStats | null>(null)
  const taskLoading = ref(false)

  async function fetchTaskStats(uuid: string) {
    if (!uuid) return
    taskLoading.value = true
    try {
      taskStats.value = await api.taskStats(uuid)
    } catch {
      taskStats.value = null
    } finally {
      taskLoading.value = false
    }
  }

  async function fetchStats(days = rangeDays.value) {
    loading.value = true
    error.value = null
    rangeDays.value = days
    try {
      stats.value = await api.usageStats(days)
    } catch (e) {
      error.value = e instanceof Error ? e.message : String(e)
    } finally {
      loading.value = false
    }
  }

  return { stats, loading, error, rangeDays, fetchStats, taskStats, taskLoading, fetchTaskStats }
})
