import type { WorkspaceKind } from '../lib/types'

const PAGE_BOOT_MARKER = 'jcode_page_booted'

/** sessionStorage survives reloads but not a genuinely new tab/process. The
 * marker is read before boot and written only after a successful landing. */
export function pageBootCompleted(storage: Pick<Storage, 'getItem'> | undefined = safeBootMarkerStore()): boolean {
  if (!storage) return false
  try {
    return storage.getItem(PAGE_BOOT_MARKER) === '1'
  } catch {
    return false
  }
}

export function markPageBootComplete(storage: Pick<Storage, 'setItem'> | undefined = safeBootMarkerStore()): void {
  if (!storage) return
  try {
    storage.setItem(PAGE_BOOT_MARKER, '1')
  } catch {
    // A storage failure keeps subsequent loads fail-closed as cold opens.
  }
}

export type StartupLanding = 'restore' | 'reuse_bootstrap' | 'provision'

/** Choose the startup session action after health + sidebar indexes load. */
export function startupLanding(
  cold: boolean,
  sessionID: string,
  indexed: boolean,
  running: boolean,
  freshSession?: boolean,
): StartupLanding {
  const durable = freshSession === false || (freshSession === undefined && indexed)
  if (!cold && sessionID !== '' && durable) return 'restore'
  if (sessionID !== '' && freshSession === true && !running) return 'reuse_bootstrap'
  return 'provision'
}

/** Hidden Desktop windows start fresh when reopened only if no task is active. */
export function shouldStartFreshOnWindowReopen(running: boolean): boolean {
  return !running
}

export interface FreshTaskTarget {
  projectPath?: string
  workspaceKind?: WorkspaceKind
  expectedSessionId?: string
  requireIdle?: boolean
}

/** Desktop sidecars boot from HOME; a cold New Task should retain the last
 * durable workspace authority without reopening its conversation. Scratch is
 * special: JCode allocates a new managed directory instead of reusing a path. */
export function coldDesktopFreshTarget(
  cold: boolean,
  desktop: boolean,
  recentProject: string | undefined,
  recentKind: WorkspaceKind | undefined,
): FreshTaskTarget | undefined {
  if (!cold || !desktop || !recentProject) return undefined
  if (recentKind === 'scratch') return { workspaceKind: 'scratch' }
  return { projectPath: recentProject, workspaceKind: 'project' }
}

function safeBootMarkerStore(): Pick<Storage, 'getItem' | 'setItem'> | undefined {
  if (typeof sessionStorage === 'undefined') return undefined
  try {
    // Some embedded browsers expose the property but reject all operations.
    sessionStorage.getItem(PAGE_BOOT_MARKER)
    return sessionStorage
  } catch {
    return undefined
  }
}
