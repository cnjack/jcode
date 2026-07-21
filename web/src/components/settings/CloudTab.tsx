/**
 * settings/CloudTab — the Cloud section of the settings view (M18).
 *
 * The full cloud-relay lifecycle lives here now (moved out of the CloudBadge
 * popover, which keeps only a quick status indicator + a deep link to this
 * tab):
 *
 *   logged out  → in-app device-code login: user_code + verification URI (QR)
 *                 with a status poll until success / error / expired (retry)
 *   logged in   → connection state, server/device info, the auto-connect
 *                 switch, pairing approvals (approve / deny), a "scan to pair"
 *                 QR offer with an expiry countdown, and logout
 *
 * The tab polls `api.cloudStatus()` every 5s while mounted (same cadence as
 * the badge). Older backends without /api/cloud/* degrade to the logged-out
 * login entry instead of looking broken.
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
} from '../../lib/api'
import { BTN_GHOST, BTN_PRIMARY, BTN_SECONDARY, BTN_SM, ROW, SECTION_TITLE, Switch } from './atoms'

const POLL_MS = 5000
const LOGIN_POLL_MS = 2000

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

/** CopyButton — one-click clipboard copy with transient "copied" feedback (M17). */
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

export function CloudTab() {
  const { t } = useTranslation()
  // null = status unknown (request failed / older backend).
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

  // Inline error surface (this tab has no toast — the badge owns toasts).
  const [actionError, setActionError] = useState<string | null>(null)
  const mounted = useRef(true)
  useEffect(() => {
    mounted.current = true
    return () => {
      mounted.current = false
    }
  }, [])

  const refresh = useCallback(async () => {
    let st: CloudStatusResponse | null
    try {
      st = await api.cloudStatus()
    } catch {
      st = null
    }
    if (!mounted.current) return
    setStatus(st)
    if (!st?.logged_in) {
      setPairings([])
      return
    }
    try {
      const pr = await api.cloudPairings()
      if (mounted.current) setPairings(pr.pairings)
    } catch {
      // Pairing endpoints unavailable (older backend): ignore.
    }
  }, [])

  useEffect(() => {
    void refresh()
    const id = window.setInterval(() => void refresh(), POLL_MS)
    return () => window.clearInterval(id)
  }, [refresh])

  // Resume an in-flight login when the tab mounts while logged out (the flow
  // may have been started in a previous visit).
  useEffect(() => {
    if (status?.logged_in || loginPanel) return
    api
      .cloudLoginStatus()
      .then((st) => {
        if (st.state === 'pending') setLoginPanel(st)
      })
      .catch(() => {})
    // Only on mount / login-state transitions, not on every loginPanel change.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [status?.logged_in])

  // Poll the login flow while it is pending; success refreshes the tab.
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
  }, [loginPending, refresh])

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
    setActionError(null)
    try {
      const r = await api.cloudLogin()
      setLoginPanel({
        state: 'pending',
        user_code: r.user_code,
        verification_uri: r.verification_uri,
        expires_at: r.expires_at,
      })
    } catch (err) {
      setActionError(t('cloud.loginFailed', { message: err instanceof Error ? err.message : String(err) }))
    } finally {
      setLoginBusy(false)
    }
  }

  async function resolvePairing(id: string, approve: boolean) {
    if (pairingBusy) return
    setPairingBusy(id)
    setActionError(null)
    try {
      const pr = approve ? await api.cloudApprovePairing(id) : await api.cloudDenyPairing(id)
      setPairings(pr.pairings)
    } catch (err) {
      setActionError(err instanceof Error ? err.message : String(err))
    } finally {
      setPairingBusy(null)
    }
  }

  async function createOffer() {
    if (offerBusy) return
    setOfferBusy(true)
    setActionError(null)
    try {
      const o = await api.cloudPairingOffer()
      setOffer({ qr: o.qr, expiresAt: Date.parse(o.expires_at) })
    } catch (err) {
      setActionError(err instanceof Error ? err.message : String(err))
    } finally {
      setOfferBusy(false)
    }
  }

  async function logout() {
    if (logoutBusy) return
    setLogoutBusy(true)
    setActionError(null)
    try {
      setStatus(await api.cloudLogout())
      setPairings([])
      setOffer(null)
      setLoginPanel(null)
    } catch (err) {
      setActionError(err instanceof Error ? err.message : String(err))
    } finally {
      setLogoutBusy(false)
    }
  }

  const pulsing = !!status?.logged_in && status.state === 'connecting'
  const badgeColor = dotColor(status)
  const stateLabel = !status?.logged_in ? t('cloud.notLoggedIn') : t(`cloud.status.${status.state}`)
  const offerRemaining = offer ? offer.expiresAt - now : 0

  return (
    <div className="space-y-5">
      <h3 className={SECTION_TITLE}>{t('settings.tabs.cloud')}</h3>

      {/* Connection state. */}
      <div className={ROW}>
        <div className="grid h-7 w-7 shrink-0 place-items-center rounded-[var(--radius-md)] text-[var(--color-muted-foreground)]">
          <CloudIcon className="h-4 w-4" />
        </div>
        <div className="min-w-0 flex-1">
          <div className="text-[12px] font-medium text-[var(--color-foreground)]">{t('cloud.title')}</div>
          <div className="flex items-center gap-1.5 text-[11px] text-[var(--color-muted-foreground)]">
            <span
              aria-hidden="true"
              className={`h-2 w-2 shrink-0 rounded-full ${pulsing ? 'cb-dot-pulse' : ''}`}
              style={{ backgroundColor: badgeColor }}
            />
            {stateLabel}
          </div>
        </div>
      </div>

      {status?.logged_in && status.state === 'error' && status.error && (
        <div className="rounded-[var(--radius-md)] border border-[var(--color-error-fg)] bg-[var(--color-error-bg)] px-2.5 py-2 text-[11px] leading-tight text-[var(--color-error-fg)]">
          {status.error}
        </div>
      )}

      {status?.logged_in ? (
        <>
          {/* Account: server + device identity. */}
          {(status.cloud_url || status.device_name) && (
            <div className={ROW}>
              <div className="min-w-0 flex-1 space-y-1">
                {status.cloud_url && (
                  <div className="flex items-center justify-between gap-3 text-[12px]">
                    <span className="text-[var(--color-muted-foreground)]">{t('cloud.server')}</span>
                    <span className="truncate font-mono text-[var(--color-foreground)]" title={status.cloud_url}>
                      {status.cloud_url}
                    </span>
                  </div>
                )}
                {status.device_name && (
                  <div className="flex items-center justify-between gap-3 text-[12px]">
                    <span className="text-[var(--color-muted-foreground)]">{t('cloud.device')}</span>
                    <span className="truncate text-[var(--color-foreground)]" title={status.device_name}>
                      {status.device_name}
                    </span>
                  </div>
                )}
              </div>
            </div>
          )}

          {/* Auto-connect switch — disabled while a save is in flight. */}
          <div className={ROW}>
            <div className="min-w-0 flex-1">
              <div className="text-[12px] font-medium text-[var(--color-foreground)]">{t('cloud.autoConnect')}</div>
              <div className="text-[11px] text-[var(--color-muted-foreground)]">{t('cloud.autoConnectHint')}</div>
            </div>
            <Switch
              on={!!status.auto_connect}
              onClick={() => void toggleAutoConnect()}
              disabled={saving}
              ariaLabel={t('cloud.autoConnect')}
              title={status.auto_connect ? t('common.disable') : t('common.enable')}
            />
          </div>
          {saveError && (
            <div className="text-[11px] text-[var(--color-error-fg)]">{t('cloud.saveFailed', { message: saveError })}</div>
          )}

          {/* Pairing: pending approvals + scan-to-pair QR offer. */}
          <div className="text-[10px] font-medium uppercase tracking-wider text-[var(--color-muted-foreground)]">
            {t('cloud.pairingSection')}
          </div>

          {pairings.length > 0 && (
            <div className="space-y-1.5">
              <div className="text-[12px] font-medium text-[var(--color-foreground)]">{t('cloud.pairingRequests')}</div>
              {pairings.map((p) => (
                <div key={p.pairing_id} className={ROW}>
                  <div className="min-w-0 flex-1">
                    <div className="truncate text-[12px] font-medium text-[var(--color-foreground)]" title={p.label}>
                      {p.label}
                    </div>
                    <div className="text-[11px] text-[var(--color-muted-foreground)]">
                      {new Date(p.received_at).toLocaleString()}
                    </div>
                  </div>
                  <button
                    type="button"
                    disabled={pairingBusy !== null}
                    onClick={() => void resolvePairing(p.pairing_id, true)}
                    className={`${BTN_PRIMARY} ${BTN_SM}`}
                  >
                    <CheckIcon className="h-3 w-3" />
                    {t('cloud.approve')}
                  </button>
                  <button
                    type="button"
                    disabled={pairingBusy !== null}
                    onClick={() => void resolvePairing(p.pairing_id, false)}
                    className={`${BTN_SECONDARY} ${BTN_SM}`}
                  >
                    <XMarkIcon className="h-3 w-3" />
                    {t('cloud.deny')}
                  </button>
                </div>
              ))}
            </div>
          )}

          <div className={ROW}>
            {offer ? (
              <div className="flex w-full flex-col items-center gap-2 py-1">
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
                    className={`${BTN_SECONDARY} ${BTN_SM}`}
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
              <>
                <div className="grid h-7 w-7 shrink-0 place-items-center rounded-[var(--radius-md)] text-[var(--color-muted-foreground)]">
                  <QrCodeIcon className="h-4 w-4" />
                </div>
                <div className="min-w-0 flex-1">
                  <div className="text-[12px] font-medium text-[var(--color-foreground)]">{t('cloud.scanPair')}</div>
                  <div className="text-[11px] text-[var(--color-muted-foreground)]">{t('cloud.scanPairHint')}</div>
                </div>
                <button
                  type="button"
                  disabled={offerBusy}
                  onClick={() => void createOffer()}
                  className={`${BTN_SECONDARY} ${BTN_SM}`}
                >
                  <QrCodeIcon className={`h-3.5 w-3.5 ${offerBusy ? 'animate-spin' : ''}`} />
                  {t('cloud.scanPair')}
                </button>
              </>
            )}
          </div>

          {/* Logout. */}
          <div className="flex justify-end">
            <button
              type="button"
              disabled={logoutBusy}
              onClick={() => void logout()}
              className={`${BTN_GHOST} ${BTN_SM} text-[var(--color-muted-foreground)] hover:text-[var(--color-destructive)]`}
            >
              <ArrowRightOnRectangleIcon className="h-3.5 w-3.5" />
              {t('cloud.logout')}
            </button>
          </div>
        </>
      ) : loginPanel?.state === 'pending' && loginPanel.user_code ? (
        /* Device-code login in flight: code + verification URI + QR. */
        <div className={`${ROW} flex-col items-center gap-2 py-4`}>
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
          {loginPanel.verification_uri && <CopyButton text={loginPanel.verification_uri} label={t('cloud.copyLink')} />}
          <div className="inline-flex items-center gap-1.5 text-[11.5px] text-[var(--color-muted-foreground)]">
            <ArrowPathIcon className="h-3.5 w-3.5 animate-spin" />
            {t('cloud.loginWaiting')}
          </div>
        </div>
      ) : loginPanel && (loginPanel.state === 'error' || loginPanel.state === 'expired') ? (
        /* Terminal login failure: message + retry. */
        <div className={ROW}>
          <div className="min-w-0 flex-1 rounded-[var(--radius-md)] border border-[var(--color-error-fg)] bg-[var(--color-error-bg)] px-2 py-1.5 text-[11px] leading-tight text-[var(--color-error-fg)]">
            {loginPanel.state === 'expired'
              ? t('cloud.loginExpired')
              : t('cloud.loginFailed', { message: loginPanel.error ?? '' })}
          </div>
          <button
            type="button"
            disabled={loginBusy}
            onClick={() => void startLogin()}
            className={`${BTN_PRIMARY} ${BTN_SM}`}
          >
            <ArrowPathIcon className={`h-3.5 w-3.5 ${loginBusy ? 'animate-spin' : ''}`} />
            {t('cloud.retry')}
          </button>
        </div>
      ) : (
        /* Logged out: in-app device-code login entry point. */
        <div className={ROW}>
          <div className="min-w-0 flex-1">
            <div className="text-[12px] font-medium text-[var(--color-foreground)]">{t('cloud.login')}</div>
            <div className="text-[11px] leading-relaxed text-[var(--color-muted-foreground)]">{t('cloud.loginIntro')}</div>
          </div>
          <button
            type="button"
            disabled={loginBusy}
            onClick={() => void startLogin()}
            className={`${BTN_PRIMARY} ${BTN_SM}`}
          >
            <ArrowPathIcon className={`h-3.5 w-3.5 ${loginBusy ? 'animate-spin' : ''}`} />
            {t('cloud.login')}
          </button>
        </div>
      )}

      {actionError && (
        <div role="alert" className="text-[11px] text-[var(--color-error-fg)]">
          {actionError}
        </div>
      )}

      {/* Connecting pulse; disabled under prefers-reduced-motion (same keyframes
          the CloudBadge dot uses). */}
      <style>{`
        @keyframes cb-dot-pulse { 0%,100% { opacity:0.35; } 50% { opacity:1; } }
        .cb-dot-pulse { animation: cb-dot-pulse 1.2s ease-in-out infinite; }
        @media (prefers-reduced-motion: reduce) { .cb-dot-pulse { animation:none; opacity:1; } }
      `}</style>
    </div>
  )
}
