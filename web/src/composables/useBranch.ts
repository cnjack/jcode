// Shared git-branch state for the composer's BranchPicker and the TopBar chip.
// Module-level singletons so every consumer reflects the same current branch,
// and so a switch — whether done in the UI or with `git checkout` in a terminal
// — shows up everywhere.
import { ref } from 'vue'
import { api } from '@/composables/api'
import { i18n } from '@/i18n'

const current = ref('')
const branches = ref<string[]>([])
const loading = ref(false)
const switching = ref(false)
const error = ref('')

// refresh re-reads the current branch + local branch list. Any error (not a git
// repo, git missing) collapses to an empty state rather than throwing.
async function refresh() {
  loading.value = true
  try {
    const res = await api.gitBranches()
    current.value = res.current || ''
    branches.value = res.branches || []
  } catch {
    current.value = ''
    branches.value = []
  } finally {
    loading.value = false
  }
}

// checkout switches branch (create=true → `git checkout -b`). Returns true on
// success; on failure (e.g. a dirty tree git refuses to overwrite) `error` holds
// git's own message for the caller to surface. Always re-reads afterwards so the
// UI matches reality even on partial failures.
async function checkout(branch: string, create = false): Promise<boolean> {
  switching.value = true
  error.value = ''
  try {
    const res = await api.gitCheckout(branch, create)
    current.value = res.branch || branch
    return true
  } catch (e) {
    error.value = (e as Error).message || i18n.global.t('errors.branchSwitch')
    return false
  } finally {
    switching.value = false
    await refresh()
  }
}

// Keep the UI in sync when the branch changes outside the app (e.g. the user or
// the agent runs `git checkout` in a terminal): re-read whenever the window
// regains focus or the tab becomes visible. Wired once.
let wired = false
function wireExternalSync() {
  if (wired || typeof window === 'undefined') return
  wired = true
  window.addEventListener('focus', () => { void refresh() })
  document.addEventListener('visibilitychange', () => {
    if (document.visibilityState === 'visible') void refresh()
  })
}

export function useBranch() {
  wireExternalSync()
  return { current, branches, loading, switching, error, refresh, checkout }
}
