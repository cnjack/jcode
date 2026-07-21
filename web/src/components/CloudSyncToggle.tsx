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

import { useCallback, useEffect, useState } from 'react'
import { CloudIcon } from '@heroicons/react/24/outline'
import { useTranslation } from 'react-i18next'
import { api } from '../lib/api'
import { useAppSelector } from '../app/hooks'

const POLL_MS = 5000

export function CloudSyncToggle() {
  const { t } = useTranslation()
  const sessionId = useAppSelector((s) => s.session.currentSessionId)
  const [loggedIn, setLoggedIn] = useState(false)
  const [synced, setSynced] = useState(false)
  const [busy, setBusy] = useState(false)

  const refresh = useCallback(async () => {
    try {
      const st = await api.cloudStatus()
      setLoggedIn(st.logged_in)
      if (!st.logged_in) {
        setSynced(false)
        return
      }
      const sync = await api.cloudSync()
      setSynced(sessionId ? !!sync.sessions[sessionId] : false)
    } catch {
      // Tolerate failure (older backend / transient): keep the last state.
    }
  }, [sessionId])

  useEffect(() => {
    void refresh()
    const id = window.setInterval(() => void refresh(), POLL_MS)
    return () => window.clearInterval(id)
  }, [refresh])

  async function toggle() {
    if (!sessionId || !loggedIn || busy) return
    setBusy(true)
    const next = !synced
    setSynced(next) // optimistic; the next poll reconciles
    try {
      await api.cloudSetSessionSync(sessionId, next)
    } catch {
      setSynced(!next)
    } finally {
      setBusy(false)
    }
  }

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
        className="inline-flex h-[34px] w-[34px] items-center justify-center rounded-[var(--radius-lg)] border transition-[background,color,border-color] duration-150 disabled:cursor-not-allowed disabled:opacity-50"
        style={{
          color: synced ? 'var(--color-accent-neutral)' : 'var(--color-muted-foreground)',
          borderColor: 'var(--color-border)',
          backgroundColor: 'var(--color-background)',
        }}
      >
        <CloudIcon className="h-4 w-4" />
      </button>
    </div>
  )
}
