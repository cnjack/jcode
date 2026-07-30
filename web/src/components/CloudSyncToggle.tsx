/**
 * CloudSyncToggle — per-session cloud sync switch (M19), floated at the
 * top-right of the session page just left of the TopBar.
 *
 * Three states:
 *   logged out          → gray cloud, disabled, tooltip "log in to sync"
 *   logged in + synced  → lit (accent) cloud, click to stop syncing
 *   logged in + unsynced → gray cloud, click to start syncing
 *
 * It polls /api/cloud/status + /api/cloud/sync every 5s (same cadence and
 * failure tolerance as the CloudBadge: an older backend without the endpoints
 * just renders the gray disabled state). Hidden when there is no current
 * session (welcome screen).
 *
 * Semantics (mirror of the backend): turning sync off stops future uploads
 * but keeps the cloud-side history; turning it on uploads from that point on
 * (no backfill).
 */

import { useCallback, useEffect, useRef, useState } from 'react'
import { CloudIcon } from '@heroicons/react/24/outline'
import { useTranslation } from 'react-i18next'
import { api } from '../lib/api'
import { useAppSelector } from '../app/hooks'

const POLL_MS = 5000

export interface CloudSessionSyncState {
  loggedIn: boolean
  synced: boolean
  busy: boolean
  toggle: () => Promise<void>
}

/** Shared per-session cloud state used by both browser chrome and Desktop. */
export function useCloudSessionSync(sessionId: string): CloudSessionSyncState {
  const [loggedIn, setLoggedIn] = useState(false)
  const [synced, setSynced] = useState(false)
  const [busy, setBusy] = useState(false)
  const refreshSequence = useRef(0)
  const busyRef = useRef(false)
  const mountedRef = useRef(true)

  useEffect(() => {
    mountedRef.current = true
    return () => {
      mountedRef.current = false
    }
  }, [])

  const refresh = useCallback(async () => {
    if (busyRef.current) return
    const sequence = ++refreshSequence.current
    try {
      const st = await api.cloudStatus()
      if (sequence !== refreshSequence.current) return
      setLoggedIn(st.logged_in)
      if (!st.logged_in) {
        setSynced(false)
        return
      }
      const sync = await api.cloudSync()
      if (sequence !== refreshSequence.current) return
      setSynced(sessionId ? !!sync.sessions[sessionId] : false)
    } catch {
      // Tolerate failure (older backend / transient): keep the last state.
    }
  }, [sessionId])

  useEffect(() => {
    refreshSequence.current++
    setSynced(false)
    void refresh()
    const id = window.setInterval(() => void refresh(), POLL_MS)
    return () => {
      refreshSequence.current++
      window.clearInterval(id)
    }
  }, [refresh])

  async function toggle() {
    if (!sessionId || !loggedIn || busyRef.current) return
    busyRef.current = true
    const sequence = ++refreshSequence.current
    setBusy(true)
    const next = !synced
    setSynced(next) // optimistic; the next poll reconciles
    try {
      await api.cloudSetSessionSync(sessionId, next)
    } catch {
      if (mountedRef.current && sequence === refreshSequence.current) setSynced(!next)
    } finally {
      busyRef.current = false
      if (mountedRef.current) {
        setBusy(false)
        if (sequence === refreshSequence.current) void refresh()
      }
    }
  }

  return { loggedIn, synced, busy, toggle }
}

export function CloudSyncToggle() {
  const { t } = useTranslation()
  const sessionId = useAppSelector((s) => s.session.currentSessionId)
  const { loggedIn, synced, busy, toggle } = useCloudSessionSync(sessionId)

  if (!sessionId) return null

  const title = !loggedIn
    ? t('cloud.syncNeedLogin')
    : synced
      ? t('cloud.syncOn')
      : t('cloud.syncOff')

  return (
    <div className="absolute right-[64px] top-[6px] z-[46]">
      <button
        type="button"
        onClick={() => void toggle()}
        disabled={!loggedIn || busy}
        aria-label={t('cloud.syncSession')}
        aria-pressed={synced}
        title={title}
        className="inline-flex h-[34px] w-[34px] items-center justify-center rounded-[var(--radius-lg)] bg-transparent transition-[color,transform] duration-150 active:translate-y-px disabled:cursor-not-allowed disabled:opacity-50"
        style={{
          color: synced ? 'var(--color-accent-neutral)' : 'var(--color-muted-foreground)',
        }}
      >
        <span className="relative">
          <CloudIcon className="h-4 w-4" />
          {!synced && loggedIn && <span aria-hidden="true" className="absolute left-0 top-1/2 h-px w-4 -rotate-45 bg-current" />}
        </span>
      </button>
    </div>
  )
}
