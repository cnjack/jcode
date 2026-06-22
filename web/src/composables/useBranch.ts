// Shared git-branch state for the composer's BranchPicker and the TopBar chip.
// Module-level singletons so every consumer reflects the same current branch,
// and so a switch — whether done in the UI or with `git checkout` in a terminal
// — shows up everywhere.
import { ref } from 'vue'
import { api } from '@/composables/api'
import { i18n } from '@/i18n'

// A switch git refused because it would overwrite uncommitted work. Held so the
// UI can confirm how to resolve it (stash / discard) before retrying.
export interface PendingSwitch {
  branch: string
  create: boolean
  files: string[] // files that would be overwritten
}

const current = ref('')
const branches = ref<string[]>([])
const loading = ref(false)
const switching = ref(false)
const error = ref('')
const pending = ref<PendingSwitch | null>(null)

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

// checkout switches branch (create=true → `git checkout -b`). strategy picks how
// to handle a dirty tree: '' lets git decide (and abort if it would clobber
// changes), 'stash' tucks changes away first, 'force' discards them.
//
// Returns true on success. A plain switch git refuses because it would overwrite
// uncommitted work is NOT an error: it parks the conflict in `pending` (returning
// false) so the caller can confirm a strategy and retry. Any other failure sets
// `error`. Always re-reads afterwards so the UI matches reality.
async function checkout(
  branch: string,
  create = false,
  strategy: '' | 'stash' | 'force' = '',
): Promise<boolean> {
  switching.value = true
  error.value = ''
  try {
    const res = await api.gitCheckout(branch, create, strategy)
    if (res.blocked) {
      // Non-destructive: git aborted before touching the tree. Surface the
      // conflict so the caller can confirm how to proceed.
      pending.value = { branch, create, files: res.files || [] }
      return false
    }
    pending.value = null
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

// resolvePending retries the parked switch with a chosen strategy: 'stash' saves
// the working changes first (recoverable via `git stash pop`), 'force' discards
// them. On success `pending` is cleared by checkout().
async function resolvePending(strategy: 'stash' | 'force'): Promise<boolean> {
  const p = pending.value
  if (!p) return false
  return checkout(p.branch, p.create, strategy)
}

// cancelPending dismisses the confirmation without switching.
function cancelPending() {
  pending.value = null
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
  return {
    current,
    branches,
    loading,
    switching,
    error,
    pending,
    refresh,
    checkout,
    resolvePending,
    cancelPending,
  }
}
