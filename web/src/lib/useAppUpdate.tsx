/**
 * useAppUpdate — desktop auto-update flow (Tauri updater plugin), ported from
 * jtype's useAppUpdate. Mirrors the useDesktop.ts bridge rules: everything is
 * feature-detected via `isTauri` and the plugin packages are dynamically
 * imported so the browser bundle never executes Tauri updater code.
 *
 * Flow: passive check ~4s after mount (silent on failure) → "available" →
 * user consents via the UpdateBanner → downloadAndInstall with progress →
 * relaunch (process plugin) swaps in the staged bundle.
 *
 * The state lives in a single provider (mounted once by App) and is shared
 * through a context so the Settings "version & updates" row can trigger a
 * manual check and reuse the same banner.
 */

import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useRef,
  useState,
  type ReactNode,
} from 'react'
import type { Update } from '@tauri-apps/plugin-updater'
import { isTauri } from './useDesktop'

/** Delay before the passive startup check — past first paint and sidecar boot. */
const AUTO_CHECK_DELAY_MS = 4000

export type AppUpdateStatus =
  | 'idle'
  | 'checking'
  | 'available'
  | 'up-to-date'
  | 'downloading'
  | 'restarting'
  | 'error'

export interface AppUpdateState {
  status: AppUpdateStatus
  /** Version string of the pending update ('' when none). */
  version: string
  /** Release notes body of the pending update ('' when none). */
  notes: string
  /** Download progress ratio 0..1 (stays 0 when the size is unknown). */
  progress: number
  /** Error message when status === 'error'. */
  error: string
}

export interface AppUpdateApi extends AppUpdateState {
  /** True once the user closed the banner (hidden until the next check). */
  dismissed: boolean
  check: (opts?: { manual?: boolean }) => Promise<void>
  install: () => Promise<void>
  dismiss: () => void
}

const initialState: AppUpdateState = {
  status: 'idle',
  version: '',
  notes: '',
  progress: 0,
  error: '',
}

const AppUpdateContext = createContext<AppUpdateApi | null>(null)

export function AppUpdateProvider({
  autoCheck = true,
  children,
}: {
  autoCheck?: boolean
  children: ReactNode
}) {
  const [state, setState] = useState<AppUpdateState>(initialState)
  const [dismissed, setDismissed] = useState(false)
  // The Update handle is imperative (downloadAndInstall); keep it out of state.
  const updateRef = useRef<Update | null>(null)

  const check = useCallback(async (opts?: { manual?: boolean }) => {
    if (!isTauri) return
    setState((s) => ({ ...s, status: 'checking', error: '' }))
    try {
      const { check: tauriCheck } = await import('@tauri-apps/plugin-updater')
      const update = await tauriCheck()
      if (update) {
        updateRef.current = update
        // A check that finds an update re-arms the banner: the user dismissed
        // the previous notice, but this is a fresh discovery — and the
        // Settings row has no install button of its own.
        setDismissed(false)
        setState((s) => ({
          ...s,
          status: 'available',
          version: update.version,
          notes: update.body || '',
          progress: 0,
          error: '',
        }))
      } else {
        setState((s) => ({ ...s, status: 'up-to-date' }))
      }
    } catch (err) {
      // A failed passive startup check must never nag the user; only a manual
      // check (Settings button) surfaces the error.
      const message = err instanceof Error ? err.message : String(err)
      setState((s) => ({ ...s, status: opts?.manual ? 'error' : 'idle', error: opts?.manual ? message : '' }))
    }
  }, [])

  const install = useCallback(async () => {
    const update = updateRef.current
    if (!update) return
    setState((s) => ({ ...s, status: 'downloading', progress: 0, error: '' }))
    let contentLength = 0
    let downloaded = 0
    try {
      await update.downloadAndInstall((event) => {
        switch (event.event) {
          case 'Started':
            contentLength = event.data.contentLength ?? 0
            break
          case 'Progress':
            downloaded += event.data.chunkLength
            if (contentLength > 0) {
              setState((s) => ({ ...s, progress: Math.min(1, downloaded / contentLength) }))
            }
            break
          case 'Finished':
            setState((s) => ({ ...s, progress: 1 }))
            break
        }
      })
      setState((s) => ({ ...s, status: 'restarting' }))
      const { relaunch } = await import('@tauri-apps/plugin-process')
      await relaunch()
    } catch (err) {
      const message = err instanceof Error ? err.message : String(err)
      setState((s) => ({ ...s, status: 'error', error: message }))
    }
  }, [])

  const dismiss = useCallback(() => setDismissed(true), [])

  // Passive startup check. Skipped entirely in dev mode — the dev app version
  // (from tauri.conf.json) would otherwise "update" over the dev session.
  useEffect(() => {
    if (!autoCheck || !isTauri || import.meta.env.DEV) return
    const timer = window.setTimeout(() => {
      void check()
    }, AUTO_CHECK_DELAY_MS)
    return () => window.clearTimeout(timer)
  }, [autoCheck, check])

  return (
    <AppUpdateContext.Provider value={{ ...state, dismissed, check, install, dismiss }}>
      {children}
    </AppUpdateContext.Provider>
  )
}

export function useAppUpdate(): AppUpdateApi {
  const ctx = useContext(AppUpdateContext)
  if (!ctx) throw new Error('useAppUpdate must be used within <AppUpdateProvider>')
  return ctx
}
