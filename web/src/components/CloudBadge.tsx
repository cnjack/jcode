/**
 * CloudBadge — cloud-relay status badge in the Sidebar footer.
 *
 * A `sb-footer-btn`-sized CloudIcon button with a status dot overlay (TopBar
 * idiom) that polls `api.cloudStatus()` every 5s and opens a popover on click.
 * The popover covers the whole cloud lifecycle without touching the CLI
 * (M11-W1):
 *
 *   logged out  → in-app device-code login: user_code + verification URI (QR)
 *                 with a status poll until success / error / expired (retry)
 *   logged in   → state + server/device info, pairing approval cards (approve
 *                 / deny, polled with the status), a "scan to pair" QR offer
 *                 with an expiry countdown, the auto-connect switch, logout
 *
 * Approvals (manual or QR auto-approve) and newly arrived pairing requests
 * surface as transient toasts; pending requests turn the dot accent-pulsing
 * so they are visible with the popover closed.
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
import QRCode from 'qrcode'
import {
  ArrowPathIcon,
  ArrowRightOnRectangleIcon,
  CheckIcon,
  ClipboardDocumentIcon,
  CloudIcon,
  QrCodeIcon,
  XMarkIcon,
} from '@heroicons/react/24/outline'
import { useTranslation } from 'react-i18next'
import {
  api,
  type CloudLoginStatusResponse,
  type CloudPendingPairing,
  type CloudStatusResponse,
} from '../lib/api'
import { Switch } from './SettingsDialog'

const POLL_MS = 5000
const LOGIN_POLL_MS = 2000
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

/** QRImage renders text as a QR code data-URL image (white pad for contrast). */
function QRImage({ text, size }: { text: string; size: number }) {
  const [src, setSrc] = useState<string | null>(null)
  useEffect(() => {
    let alive = true
    QRCode.toDataURL(text, { margin: 1, width: size })
      .then((url) => {
        if (alive) setSrc(url)
      })
      .catch(() => {})
    return () => {
      alive = false
    }
  }, [text, size])
  return (
    <div
      className="inline-flex shrink-0 items-center justify-center rounded-[var(--radius-md)] bg-white p-1.5"
      style={{ width: size + 12, height: size + 12 }}
    >
      {src && <img src={src} width={size} height={size} alt="" aria-hidden="true" />}
    </div>
  )
}

function fmtCountdown(ms: number): string {
  const total = Math.max(0, Math.ceil(ms / 1000))
  const m = Math.floor(total / 60)
  const s = total % 60
  return `${m}:${String(s).padStart(2, '0')}`
}

/**
 * CopyButton — one-click clipboard copy with transient "copied" feedback
 * (M17). With `label` it renders as a subtle bordered text button; without,
 * as a compact icon button. Clipboard failures degrade silently — the text
 * stays selectable.
 */
function CopyButton({ text, label }: { text: string; label?: string }) {
  const { t } = useTranslation()
  const [copied, setCopied] = useState(false)
  const caption = copied ? t('cloud.copied') : (label ?? t('cloud.copy'))
  return (
    <button
      type="button"
      aria-label={caption}
      title={caption}
      onClick={() => {
        navigator.clipboard
          .writeText(text)
          .then(() => {
            setCopied(true)
            window.setTimeout(() => setCopied(false), 1500)
          })
          .catch(() => {})
      }}
      className={
        label !== undefined
          ? 'inline-flex items-center gap-1 rounded-[var(--radius-md)] border border-[var(--color-border)] px-2 py-1 text-[11.5px] text-[var(--color-foreground)] transition-colors hover:bg-[var(--color-muted)]'
          : 'inline-flex h-6 w-6 shrink-0 items-center justify-center rounded-[var(--radius-md)] text-[var(--color-muted-foreground)] transition-colors hover:bg-[var(--color-muted)] hover:text-[var(--color-foreground)]'
      }
    >
      {copied ? <CheckIcon className="h-3 w-3" /> : <ClipboardDocumentIcon className="h-3 w-3" />}
      {label !== undefined && <span>{caption}</span>}
    </button>
  )
}

export function CloudBadge() {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  // null = status unknown (request failed / older backend) — dot stays gray.
  const [status, setStatus] = useState<CloudStatusResponse | null>(null)
  const [saving, setSaving] = useState(false)
  const [saveError, setSaveError] = useState<string | null>(null)

  // Login flow panel: pending (code + QR), or terminal error/expired (retry).
  const [loginPanel, setLoginPanel] = useState<CloudLoginStatusResponse | null>(null)
  const [loginBusy, setLoginBusy] = useState(false)

  // Pairing approvals + QR pairing offer.
  const [pairings, setPairings] = useState<CloudPendingPairing[]>([])
  const [pairingBusy, setPairingBusy] = useState<string | null>(null)
  const [offer, setOffer] = useState<{ qr: string; expiresAt: number } | null>(null)
  const [offerBusy, setOfferBusy] = useState(false)
  const [logoutBusy, setLogoutBusy] = useState(false)
  const [now, setNow] = useState(Date.now())

  // Transient toast (approvals, new requests, errors).
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

  // Resume an in-flight login when the popover opens while logged out (the
  // flow may have been started in a previous popover session).
  useEffect(() => {
    if (!open || status?.logged_in || loginPanel) return
    api
      .cloudLoginStatus()
      .then((st) => {
        if (st.state === 'pending') setLoginPanel(st)
      })
      .catch(() => {})
  }, [open, status?.logged_in, loginPanel])

  // Poll the login flow while it is pending; success refreshes the badge.
  const loginPending = loginPanel?.state === 'pending'
  useEffect(() => {
    if (!loginPending) return
    let cancelled = false
    async function poll() {
      try {
        const st = await api.cloudLoginStatus()
        if (cancelled) return
        if (st.state === 'success') {
          setLoginPanel(null)
          toast(t('cloud.loginSuccess'))
          void refresh()
        } else if (st.state === 'error' || st.state === 'expired') {
          setLoginPanel(st)
        }
      } catch {
        // Transient failure: keep polling.
      }
    }
    const id = window.setInterval(() => void poll(), LOGIN_POLL_MS)
    void poll()
    return () => {
      cancelled = true
      window.clearInterval(id)
    }
  }, [loginPending, t, toast, refresh])

  // Countdown tick for the pairing-offer QR.
  useEffect(() => {
    if (!offer) return
    const id = window.setInterval(() => setNow(Date.now()), 1000)
    return () => window.clearInterval(id)
  }, [offer])

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

  async function startLogin() {
    if (loginBusy) return
    setLoginBusy(true)
    try {
      const r = await api.cloudLogin()
      setLoginPanel({
        state: 'pending',
        user_code: r.user_code,
        verification_uri: r.verification_uri,
        expires_at: r.expires_at,
      })
    } catch (err) {
      toast(t('cloud.loginFailed', { message: err instanceof Error ? err.message : String(err) }))
    } finally {
      setLoginBusy(false)
    }
  }

  async function resolvePairing(id: string, approve: boolean) {
    if (pairingBusy) return
    setPairingBusy(id)
    try {
      const pr = approve ? await api.cloudApprovePairing(id) : await api.cloudDenyPairing(id)
      setPairings(pr.pairings)
      seenPairingIds.current = new Set(pr.pairings.map((p) => p.pairing_id))
      // The approval is recorded as last_paired; baseline it so the next
      // poll does not toast it a second time.
      if (pr.last_paired) {
        lastPairedAt.current = pr.last_paired.paired_at
        if (approve) toast(t('cloud.pairingApproved', { label: pr.last_paired.label }))
      }
      if (!approve) toast(t('cloud.pairingDenied', { label: id }))
    } catch (err) {
      toast(err instanceof Error ? err.message : String(err))
    } finally {
      setPairingBusy(null)
    }
  }

  async function createOffer() {
    if (offerBusy) return
    setOfferBusy(true)
    try {
      const o = await api.cloudPairingOffer()
      setOffer({ qr: o.qr, expiresAt: Date.parse(o.expires_at) })
    } catch (err) {
      toast(err instanceof Error ? err.message : String(err))
    } finally {
      setOfferBusy(false)
    }
  }

  async function logout() {
    if (logoutBusy) return
    setLogoutBusy(true)
    try {
      setStatus(await api.cloudLogout())
      setPairings([])
      setOffer(null)
      setLoginPanel(null)
      seenPairingIds.current = null
      lastPairedAt.current = null
    } catch (err) {
      toast(err instanceof Error ? err.message : String(err))
    } finally {
      setLogoutBusy(false)
    }
  }

  const pendingPairings = pairings.length > 0
  const pulsing = pendingPairings || (!!status?.logged_in && status.state === 'connecting')
  const badgeColor = pendingPairings ? 'var(--color-accent-neutral)' : dotColor(status)
  const stateLabel = !status?.logged_in
    ? t('cloud.notLoggedIn')
    : t(`cloud.status.${status.state}`)
  const offerRemaining = offer ? offer.expiresAt - now : 0

  return (
    <div ref={rootRef} className="relative inline-flex">
      <button
        type="button"
        onClick={() => {
          setSaveError(null)
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

              {/* Pending pairing approvals. */}
              {pendingPairings && (
                <div className="mt-3 border-t border-[var(--color-border)] pt-3">
                  <div className="mb-1.5 text-[12.5px] font-medium text-[var(--color-foreground)]">
                    {t('cloud.pairingRequests')}
                  </div>
                  <div className="space-y-1.5">
                    {pairings.map((p) => (
                      <div
                        key={p.pairing_id}
                        className="rounded-[var(--radius-md)] border border-[var(--color-border)] bg-[var(--color-muted)] px-2 py-1.5"
                      >
                        <div className="truncate text-[12.5px] text-[var(--color-foreground)]" title={p.label}>
                          {p.label}
                        </div>
                        <div className="mt-0.5 text-[11px] text-[var(--color-muted-foreground)]">
                          {new Date(p.received_at).toLocaleString()}
                        </div>
                        <div className="mt-1.5 flex gap-1.5">
                          <button
                            type="button"
                            disabled={pairingBusy !== null}
                            onClick={() => void resolvePairing(p.pairing_id, true)}
                            className="inline-flex items-center gap-1 rounded-[var(--radius-md)] bg-[var(--color-primary)] px-2 py-1 text-[11.5px] font-medium text-[var(--color-on-primary)] disabled:opacity-50"
                          >
                            <CheckIcon className="h-3 w-3" />
                            {t('cloud.approve')}
                          </button>
                          <button
                            type="button"
                            disabled={pairingBusy !== null}
                            onClick={() => void resolvePairing(p.pairing_id, false)}
                            className="inline-flex items-center gap-1 rounded-[var(--radius-md)] border border-[var(--color-border)] px-2 py-1 text-[11.5px] text-[var(--color-foreground)] transition-colors hover:bg-[var(--color-muted)] disabled:opacity-50"
                          >
                            <XMarkIcon className="h-3 w-3" />
                            {t('cloud.deny')}
                          </button>
                        </div>
                      </div>
                    ))}
                  </div>
                </div>
              )}

              {/* Scan-to-pair QR offer. */}
              <div className="mt-3 border-t border-[var(--color-border)] pt-3">
                {offer ? (
                  <div className="flex flex-col items-center gap-2">
                    <QRImage text={offer.qr} size={168} />
                    {offerRemaining > 0 ? (
                      <div className="text-[11px] text-[var(--color-muted-foreground)]">
                        {t('cloud.offerExpiresIn', { time: fmtCountdown(offerRemaining) })}
                      </div>
                    ) : (
                      <button
                        type="button"
                        disabled={offerBusy}
                        onClick={() => void createOffer()}
                        className="inline-flex items-center gap-1 rounded-[var(--radius-md)] border border-[var(--color-border)] px-2 py-1 text-[11.5px] text-[var(--color-foreground)] transition-colors hover:bg-[var(--color-muted)] disabled:opacity-50"
                      >
                        <ArrowPathIcon className={`h-3 w-3 ${offerBusy ? 'animate-spin' : ''}`} />
                        {t('cloud.regenerate')}
                      </button>
                    )}
                    <CopyButton text={offer.qr} label={t('cloud.copyPairLink')} />
                    <div className="text-center text-[11px] leading-relaxed text-[var(--color-muted-foreground)]">
                      {t('cloud.scanPairHint')}
                    </div>
                    <button
                      type="button"
                      onClick={() => setOffer(null)}
                      className="text-[11px] text-[var(--color-muted-foreground)] underline-offset-2 hover:underline"
                    >
                      {t('cloud.close')}
                    </button>
                  </div>
                ) : (
                  <button
                    type="button"
                    disabled={offerBusy}
                    onClick={() => void createOffer()}
                    className="inline-flex items-center gap-1.5 rounded-[var(--radius-md)] border border-[var(--color-border)] px-2.5 py-1.5 text-[12px] text-[var(--color-foreground)] transition-colors hover:bg-[var(--color-muted)] disabled:opacity-50"
                  >
                    <QrCodeIcon className={`h-3.5 w-3.5 ${offerBusy ? 'animate-spin' : ''}`} />
                    {t('cloud.scanPair')}
                  </button>
                )}
              </div>
            </>
          ) : loginPanel?.state === 'pending' && loginPanel.user_code ? (
            /* Device-code login in flight: code + verification URI + QR. */
            <div className="mt-3 flex flex-col items-center gap-2 border-t border-[var(--color-border)] pt-3">
              <div className="self-start text-[11.5px] leading-relaxed text-[var(--color-muted-foreground)]">
                {t('cloud.loginPrompt')}
              </div>
              {loginPanel.verification_uri && (
                <a
                  href={loginPanel.verification_uri}
                  target="_blank"
                  rel="noreferrer"
                  className="max-w-full truncate text-[12px] text-[var(--color-accent-neutral)] underline-offset-2 hover:underline"
                  title={loginPanel.verification_uri}
                >
                  {loginPanel.verification_uri}
                </a>
              )}
              <div className="flex items-center gap-1.5">
                <div
                  className="rounded-[var(--radius-md)] bg-[var(--color-muted)] px-3 py-1.5 text-[18px] font-semibold tracking-[0.2em] text-[var(--color-foreground)]"
                  style={{ fontFamily: 'var(--font-mono)' }}
                >
                  {loginPanel.user_code}
                </div>
                <CopyButton text={loginPanel.user_code} />
              </div>
              {loginPanel.verification_uri && <QRImage text={loginPanel.verification_uri} size={140} />}
              {loginPanel.verification_uri && (
                <CopyButton text={loginPanel.verification_uri} label={t('cloud.copyLink')} />
              )}
              <div className="inline-flex items-center gap-1.5 text-[11.5px] text-[var(--color-muted-foreground)]">
                <ArrowPathIcon className="h-3.5 w-3.5 animate-spin" />
                {t('cloud.loginWaiting')}
              </div>
            </div>
          ) : loginPanel && (loginPanel.state === 'error' || loginPanel.state === 'expired') ? (
            /* Terminal login failure: message + retry. */
            <div className="mt-3 border-t border-[var(--color-border)] pt-3">
              <div className="rounded-[var(--radius-md)] border border-[var(--color-error-fg)] bg-[var(--color-error-bg)] px-2 py-1.5 text-[11px] leading-tight text-[var(--color-error-fg)]">
                {loginPanel.state === 'expired'
                  ? t('cloud.loginExpired')
                  : t('cloud.loginFailed', { message: loginPanel.error ?? '' })}
              </div>
              <button
                type="button"
                disabled={loginBusy}
                onClick={() => void startLogin()}
                className="mt-2 inline-flex items-center gap-1.5 rounded-[var(--radius-md)] bg-[var(--color-primary)] px-2.5 py-1.5 text-[12px] font-medium text-[var(--color-on-primary)] disabled:opacity-50"
              >
                <ArrowPathIcon className={`h-3.5 w-3.5 ${loginBusy ? 'animate-spin' : ''}`} />
                {t('cloud.retry')}
              </button>
            </div>
          ) : (
            /* Logged out: in-app device-code login entry point. */
            <div className="mt-3 border-t border-[var(--color-border)] pt-3">
              <div className="text-[11.5px] leading-relaxed text-[var(--color-muted-foreground)]">
                {t('cloud.loginIntro')}
              </div>
              <button
                type="button"
                disabled={loginBusy}
                onClick={() => void startLogin()}
                className="mt-2 inline-flex items-center gap-1.5 rounded-[var(--radius-md)] bg-[var(--color-primary)] px-2.5 py-1.5 text-[12px] font-medium text-[var(--color-on-primary)] disabled:opacity-50"
              >
                <ArrowPathIcon className={`h-3.5 w-3.5 ${loginBusy ? 'animate-spin' : ''}`} />
                {t('cloud.login')}
              </button>
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

          {status?.logged_in && (
            <div className="mt-3 flex justify-end border-t border-[var(--color-border)] pt-3">
              <button
                type="button"
                disabled={logoutBusy}
                onClick={() => void logout()}
                className="inline-flex items-center gap-1 text-[11.5px] text-[var(--color-muted-foreground)] transition-colors hover:text-[var(--color-destructive)] disabled:opacity-50"
              >
                <ArrowRightOnRectangleIcon className="h-3.5 w-3.5" />
                {t('cloud.logout')}
              </button>
            </div>
          )}
        </div>
      )}

      {/* Transient toast (pairing events, approvals, failures). */}
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
