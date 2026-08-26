import type { WorkspaceKind } from '../lib/types'

const PAGE_BOOT_MARKER = 'jcode_page_booted'

export type PageNavigationType = PerformanceNavigationTiming['type']

type BootMarkerStorage = Pick<Storage, 'getItem' | 'setItem' | 'removeItem'>

/** sessionStorage survives reloads but can also be cloned into a duplicated
 * tab. Restore is therefore allowed only when BOTH the marker exists and the
 * Navigation Timing entry proves this document is a genuine reload. */
export function pageBootCompleted(
  storage: Pick<Storage, 'getItem'> | undefined = safeBootMarkerStore(),
  navigationType: PageNavigationType | undefined = currentNavigationType(),
): boolean {
  if (navigationType !== 'reload') return false
  if (!storage) return false
  try {
    return storage.getItem(PAGE_BOOT_MARKER) === '1'
  } catch {
    return false
  }
}

function currentNavigationType(): PageNavigationType | undefined {
  if (typeof performance === 'undefined' || typeof performance.getEntriesByType !== 'function') return undefined
  try {
    const entry = performance.getEntriesByType('navigation')[0] as PerformanceNavigationTiming | undefined
    return entry?.type
  } catch {
    return undefined
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

/** A duplicated tab inherits sessionStorage. Remove that copied marker on every
 * non-reload document so a failed cold landing followed by Retry remains cold. */
export function clearPageBootMarker(storage: Pick<Storage, 'removeItem'> | undefined = safeBootMarkerStore()): void {
  if (!storage) return
  try {
    storage.removeItem(PAGE_BOOT_MARKER)
  } catch {
    // Storage failures already make pageBootCompleted fail closed.
  }
}

/** Read and normalize the marker exactly once at module boot. Genuine reloads
 * preserve it; navigations/duplicated tabs clear their inherited copy. */
export function initializePageBootState(
  storage: BootMarkerStorage | undefined = safeBootMarkerStore(),
  navigationType: PageNavigationType | undefined = currentNavigationType(),
): boolean {
  const completed = pageBootCompleted(storage, navigationType)
  if (navigationType !== 'reload') clearPageBootMarker(storage)
  return completed
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

/** The product shell stays unmounted until initial landing or a fresh-task
 * handoff commits, so no composer/navigation action can target the outgoing
 * task during the guarded request window. */
export function shouldShowBootScreen(bootPending: boolean, freshTaskPending: boolean): boolean {
  return bootPending || freshTaskPending
}

/** Prevent browser/native defaults for the app shortcuts while the Shell is
 * gated. Other keys keep their platform behavior. */
export function shouldBlockAppShortcut(
  pending: boolean,
  meta: boolean,
  key: string,
  shift: boolean,
): boolean {
  if (!pending || !meta) return false
  const normalized = key.toLowerCase()
  return (!shift && (normalized === 'k' || normalized === 'n' || key === ',')) ||
    (shift && normalized === 'o')
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

function safeBootMarkerStore(): BootMarkerStorage | undefined {
  if (typeof sessionStorage === 'undefined') return undefined
  try {
    // Some embedded browsers expose the property but reject all operations.
    sessionStorage.getItem(PAGE_BOOT_MARKER)
    return sessionStorage
  } catch {
    return undefined
  }
}
