/**
 * CloudBadge — cloud-relay status badge in the Sidebar footer.
 *
 * A `sb-footer-btn`-sized CloudIcon button with a status dot overlay (TopBar
 * idiom) that polls `api.cloudStatus()` every 5s and opens a small popover on
 * click: state label + server/device info, an auto-connect switch (POSTs
 * /api/cloud/config and re-reads the fresh status from the response), and a
 * `jcode login` hint when logged out.
 *
 * Dot colour mapping (unknown / fetch failure stays gray — older backends
 * without /api/cloud/* must not look broken):
 *   unknown / not logged in          → muted (gray)
 *   online                           → green   (--color-success)
 *   connecting                       → accent, pulsing
 *   error                            → red     (--color-destructive)
 *   offline while logged in          → amber   (--color-warning-fg)
 *     (auto-connect off or disconnected)
 *
 * Outside-click / Escape close mirrors TopBar's manual dropdown.
 */

import { useCallback, useEffect, useRef, useState } from 'react'
import { CloudIcon } from '@heroicons/react/24/outline'
import { useTranslation } from 'react-i18next'
import { api, type CloudStatusResponse } from '../lib/api'
import { Switch } from './SettingsDialog'

const POLL_MS = 5000

function dotColor(s: CloudStatusResponse | null): string {
  if (!s || !s.logged_in) return 'var(--color-muted-foreground)'
  switch (s.state) {
    case 'online':
      return 'var(--color-success)'
    case 'connecting':
      return 'var(--color-accent-neutral)'
    case 'error':
      return 'var(--color-destructive)'
    default: // offline while logged in
      return 'var(--color-warning-fg)'
  }
}

export function CloudBadge() {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  // null = status unknown (request failed / older backend) — dot stays gray.
  const [status, setStatus] = useState<CloudStatusResponse | null>(null)
  const [saving, setSaving] = useState(false)
  const [saveError, setSaveError] = useState<string | null>(null)
  const rootRef = useRef<HTMLDivElement | null>(null)

  const loadStatus = useCallback(async () => {
    try {
      setStatus(await api.cloudStatus())
    } catch {
      // Tolerate failure (backend older than this UI): report "unknown".
      setStatus(null)
    }
  }, [])

  useEffect(() => {
    void loadStatus()
    const id = window.setInterval(() => void loadStatus(), POLL_MS)
    return () => window.clearInterval(id)
  }, [loadStatus])

  // Close on outside click / Escape (TopBar dropdown idiom).
  useEffect(() => {
    if (!open) return
    function onPointerDown(e: MouseEvent) {
      if (!rootRef.current?.contains(e.target as Node)) setOpen(false)
    }
    function onKey(e: KeyboardEvent) {
      if (e.key === 'Escape') setOpen(false)
    }
    document.addEventListener('mousedown', onPointerDown)
    document.addEventListener('keydown', onKey)
    return () => {
      document.removeEventListener('mousedown', onPointerDown)
      document.removeEventListener('keydown', onKey)
    }
  }, [open])

  async function toggleAutoConnect() {
    if (!status || saving) return
    const prev = status
    // Optimistic flip; the POST response carries the authoritative fresh status.
    setStatus({ ...prev, auto_connect: !prev.auto_connect })
    setSaveError(null)
    setSaving(true)
    try {
      setStatus(await api.cloudSaveConfig(!prev.auto_connect))
    } catch (err) {
      setStatus(prev)
      setSaveError(err instanceof Error ? err.message : String(err))
    } finally {
      setSaving(false)
    }
  }

  const pulsing = !!status?.logged_in && status.state === 'connecting'
  const stateLabel = !status?.logged_in
    ? t('cloud.notLoggedIn')
    : t(`cloud.status.${status.state}`)

  return (
    <div ref={rootRef} className="relative inline-flex">
      <button
        type="button"
        onClick={() => {
          setSaveError(null)
          // Refresh on open so the panel shows fresh state.
          void loadStatus()
          setOpen((v) => !v)
        }}
        aria-label={t('cloud.badge')}
        aria-expanded={open}
        title={`${t('cloud.title')} · ${stateLabel}`}
        className="sb-footer-btn flex h-9 w-9 items-center justify-center rounded-[var(--radius-md)] text-[var(--color-muted-foreground)] transition-colors hover:bg-[var(--color-muted)] hover:text-[var(--color-foreground)]"
      >
        <CloudIcon className="h-[18px] w-[18px]" />
        {/* Status dot pinned to the button corner (same treatment as TopBar). */}
        <span
          aria-hidden="true"
          className={`absolute right-1 top-1 h-[7px] w-[7px] rounded-[var(--radius-pill)] ${pulsing ? 'cb-dot-pulse' : ''}`}
          style={{ backgroundColor: dotColor(status), border: '1.5px solid var(--color-background)' }}
        />
      </button>

      {open && (
        <div
          role="menu"
          aria-label={t('cloud.title')}
          className="absolute bottom-[calc(100%+6px)] left-0 z-[var(--z-dropdown)] min-w-[240px] rounded-[var(--radius-lg)] border border-[var(--color-border)] bg-[var(--color-surface)] p-3 shadow-[var(--shadow-md)]"
        >
          {/* Status line: dot + label. */}
          <div className="flex items-center gap-2 text-[13px] font-medium text-[var(--color-foreground)]">
            <span
              aria-hidden="true"
              className={`h-2 w-2 shrink-0 rounded-full ${pulsing ? 'cb-dot-pulse' : ''}`}
              style={{ backgroundColor: dotColor(status) }}
            />
            <span className="flex-1">{stateLabel}</span>
          </div>

          {status?.logged_in ? (
            <>
              {(status.cloud_url || status.device_name) && (
                <div className="mt-2 space-y-1 text-[11.5px] text-[var(--color-muted-foreground)]">
                  {status.cloud_url && (
                    <div className="flex items-center justify-between gap-3">
                      <span>{t('cloud.server')}</span>
                      <span className="truncate" title={status.cloud_url}>
                        {status.cloud_url}
                      </span>
                    </div>
                  )}
                  {status.device_name && (
                    <div className="flex items-center justify-between gap-3">
                      <span>{t('cloud.device')}</span>
                      <span className="truncate" title={status.device_name}>
                        {status.device_name}
                      </span>
                    </div>
                  )}
                </div>
              )}
              {status.state === 'error' && status.error && (
                <div className="mt-2 rounded-[var(--radius-md)] border border-[var(--color-error-fg)] bg-[var(--color-error-bg)] px-2 py-1.5 text-[11px] leading-tight text-[var(--color-error-fg)]">
                  {status.error}
                </div>
              )}
            </>
          ) : (
            /* Logged out: point at the CLI — there is no in-app login flow. */
            <div className="mt-2 text-[11.5px] leading-relaxed text-[var(--color-muted-foreground)]">
              {t('cloud.loginHint')}
              <code
                className="mt-1 block rounded-[var(--radius-md)] bg-[var(--color-muted)] px-2 py-1 text-[11.5px] text-[var(--color-foreground)]"
                style={{ fontFamily: 'var(--font-mono)' }}
              >
                jcode login
              </code>
            </div>
          )}

          {/* Auto-connect switch — disabled until we have a status, while a
              save is in flight, or when logged out (nothing to connect). */}
          <div className="mt-3 flex items-center gap-2 border-t border-[var(--color-border)] pt-3">
            <div className="flex-1">
              <div className="text-[12.5px] text-[var(--color-foreground)]">{t('cloud.autoConnect')}</div>
              <div className="text-[11px] text-[var(--color-muted-foreground)]">{t('cloud.autoConnectHint')}</div>
            </div>
            <Switch
              on={!!status?.auto_connect}
              onClick={() => void toggleAutoConnect()}
              disabled={!status?.logged_in || saving}
              ariaLabel={t('cloud.autoConnect')}
            />
          </div>
          {saveError && (
            <div className="mt-2 text-[11px] text-[var(--color-error-fg)]">
              {t('cloud.saveFailed', { message: saveError })}
            </div>
          )}
        </div>
      )}

      {/* Connecting pulse; disabled under prefers-reduced-motion like the
          Sidebar's sb-ring. */}
      <style>{`
        @keyframes cb-dot-pulse { 0%,100% { opacity:0.35; } 50% { opacity:1; } }
        .cb-dot-pulse { animation: cb-dot-pulse 1.2s ease-in-out infinite; }
        @media (prefers-reduced-motion: reduce) { .cb-dot-pulse { animation:none; opacity:1; } }
      `}</style>
    </div>
  )
}
