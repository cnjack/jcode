/**
 * CloudBadge — cloud-relay status badge in the Sidebar footer.
 *
 * M18: the badge is now a pure status indicator. The full cloud lifecycle
 * (device-code login, pairing approvals, QR pairing offer, auto-connect,
 * logout) moved to the Cloud tab of the settings view; the popover keeps only
 * a quick readout (connection state, server/device, errors, pending pairing
 * count) plus an "Open settings" deep link that routes to that tab.
 *
 * A `sb-footer-btn`-sized CloudIcon button with a status dot overlay (TopBar
 * idiom) that polls `api.cloudStatus()` every 5s. Newly arrived pairing
 * requests and approvals still surface as transient toasts, and pending
 * requests turn the dot accent-pulsing so they are visible with the popover
 * closed.
 *
 * Dot colour mapping (unknown / fetch failure stays gray — older backends
 * without /api/cloud/* must not look broken):
 *   unknown / not logged in          → muted (gray)
 *   online                           → green   (--color-success)
 *   connecting                       → accent, pulsing
 *   error                            → red     (--color-destructive)
 *   offline while logged in          → amber   (--color-warning-fg)
 *   pending pairing requests         → accent, pulsing (overrides the above)
 *
 * Outside-click / Escape close mirrors TopBar's manual dropdown.
 */

import { useCallback, useEffect, useRef, useState } from 'react'
import { ArrowRightIcon, CloudIcon } from '@heroicons/react/24/outline'
import { useTranslation } from 'react-i18next'
import { api, type CloudPendingPairing, type CloudStatusResponse } from '../lib/api'
import { useAppDispatch } from '../app/hooks'
import { uiActions } from '../app/store'

const POLL_MS = 5000
const TOAST_MS = 5000

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
  const dispatch = useAppDispatch()
  const [open, setOpen] = useState(false)
  // null = status unknown (request failed / older backend) — dot stays gray.
  const [status, setStatus] = useState<CloudStatusResponse | null>(null)

  // Pending pairing requests drive the pulsing dot (approvals happen in the
  // settings view's Cloud tab now).
  const [pairings, setPairings] = useState<CloudPendingPairing[]>([])

  // Transient toast (approvals, new requests).
  const [notice, setNotice] = useState<string | null>(null)
  const noticeTimer = useRef<number | null>(null)
  const rootRef = useRef<HTMLDivElement | null>(null)
  // Baselines for change detection: the first snapshot never toasts.
  const seenPairingIds = useRef<Set<string> | null>(null)
  const lastPairedAt = useRef<string | null>(null)

  const toast = useCallback((msg: string) => {
    setNotice(msg)
    if (noticeTimer.current) window.clearTimeout(noticeTimer.current)
    noticeTimer.current = window.setTimeout(() => setNotice(null), TOAST_MS)
  }, [])

  const refresh = useCallback(async () => {
    let st: CloudStatusResponse | null
    try {
      st = await api.cloudStatus()
    } catch {
      // Tolerate failure (backend older than this UI): report "unknown".
      st = null
    }
    setStatus(st)
    if (!st?.logged_in) {
      setPairings([])
      return
    }
    try {
      const pr = await api.cloudPairings()
      setPairings(pr.pairings)
      // Toast on newly arrived pairing requests (the first poll only baselines).
      const current = new Set(pr.pairings.map((p) => p.pairing_id))
      if (seenPairingIds.current === null) {
        seenPairingIds.current = current
      } else {
        for (const p of pr.pairings) {
          if (!seenPairingIds.current.has(p.pairing_id)) {
            toast(t('cloud.pairingRequest', { label: p.label }))
          }
        }
        seenPairingIds.current = current
      }
      // Toast on a fresh approval (manual or QR auto-approve).
      if (pr.last_paired) {
        if (lastPairedAt.current === null) {
          lastPairedAt.current = pr.last_paired.paired_at
        } else if (lastPairedAt.current !== pr.last_paired.paired_at) {
          lastPairedAt.current = pr.last_paired.paired_at
          toast(
            pr.last_paired.auto
              ? t('cloud.pairedViaQR', { label: pr.last_paired.label })
              : t('cloud.pairingApproved', { label: pr.last_paired.label }),
          )
        }
      }
    } catch {
      // Pairing endpoints unavailable (older backend): ignore.
    }
  }, [t, toast])

  useEffect(() => {
    void refresh()
    const id = window.setInterval(() => void refresh(), POLL_MS)
    return () => window.clearInterval(id)
  }, [refresh])

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

  // Deep link: route to the settings view's Cloud tab (M18).
  function openCloudSettings() {
    setOpen(false)
    dispatch(uiActions.setSettingsTab('cloud'))
    dispatch(uiActions.setView('settings'))
  }

  const pendingPairings = pairings.length > 0
  const pulsing = pendingPairings || (!!status?.logged_in && status.state === 'connecting')
  const badgeColor = pendingPairings ? 'var(--color-accent-neutral)' : dotColor(status)
  const stateLabel = !status?.logged_in
    ? t('cloud.notLoggedIn')
    : t(`cloud.status.${status.state}`)

  return (
    <div ref={rootRef} className="relative inline-flex">
      <button
        type="button"
        onClick={() => {
          // Refresh on open so the panel shows fresh state.
          void refresh()
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
          style={{ backgroundColor: badgeColor, border: '1.5px solid var(--color-background)' }}
        />
      </button>

      {open && (
        <div
          role="menu"
          aria-label={t('cloud.title')}
          className="absolute bottom-[calc(100%+6px)] left-0 z-[var(--z-dropdown)] min-w-[264px] rounded-[var(--radius-lg)] border border-[var(--color-border)] bg-[var(--color-surface)] p-3 shadow-[var(--shadow-md)]"
        >
          {/* Status line: dot + label. */}
          <div className="flex items-center gap-2 text-[13px] font-medium text-[var(--color-foreground)]">
            <span
              aria-hidden="true"
              className={`h-2 w-2 shrink-0 rounded-full ${pulsing ? 'cb-dot-pulse' : ''}`}
              style={{ backgroundColor: badgeColor }}
            />
            <span className="flex-1">{stateLabel}</span>
          </div>

          {status?.logged_in && (status.cloud_url || status.device_name) && (
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
          {status?.logged_in && status.state === 'error' && status.error && (
            <div className="mt-2 rounded-[var(--radius-md)] border border-[var(--color-error-fg)] bg-[var(--color-error-bg)] px-2 py-1.5 text-[11px] leading-tight text-[var(--color-error-fg)]">
              {status.error}
            </div>
          )}

          {pendingPairings && (
            <div className="mt-2 text-[11.5px] text-[var(--color-muted-foreground)]">
              {t('cloud.pairingRequests')}: {pairings.length}
            </div>
          )}

          {/* All configuration lives in the settings view now (M18). */}
          <button
            type="button"
            onClick={openCloudSettings}
            className="mt-3 flex w-full items-center justify-between gap-2 rounded-[var(--radius-md)] border border-[var(--color-border)] px-2.5 py-1.5 text-[12px] font-medium text-[var(--color-foreground)] transition-colors hover:bg-[var(--color-muted)]"
          >
            {t('nav.openSettings')}
            <ArrowRightIcon className="h-3.5 w-3.5 text-[var(--color-muted-foreground)]" />
          </button>
        </div>
      )}

      {/* Transient toast (pairing events, approvals). */}
      {notice && (
        <div
          role="status"
          className="fixed bottom-4 right-4 z-[var(--z-dropdown)] max-w-[320px] rounded-[var(--radius-lg)] border border-[var(--color-border)] bg-[var(--color-surface)] px-3 py-2 text-[12.5px] text-[var(--color-foreground)] shadow-[var(--shadow-md)]"
        >
          {notice}
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
