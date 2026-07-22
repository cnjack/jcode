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
 *                 switch, pairing approvals (approve / deny), and logout
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
const DEFAULT_CLOUD_URL = 'https://cloud.j-code.net'

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
  const [statusLoading, setStatusLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [saveError, setSaveError] = useState<string | null>(null)

  // Login flow panel: pending (code + QR), or terminal error/expired (retry).
  const [loginPanel, setLoginPanel] = useState<CloudLoginStatusResponse | null>(null)
  const [loginBusy, setLoginBusy] = useState(false)
  const [loginCloudURL, setLoginCloudURL] = useState(DEFAULT_CLOUD_URL)
  const [customServer, setCustomServer] = useState(false)

  // Pairing requests are reviewed here in the desktop UI.
  const [pairings, setPairings] = useState<CloudPendingPairing[]>([])
  const [pairingBusy, setPairingBusy] = useState<string | null>(null)
  const [logoutBusy, setLogoutBusy] = useState(false)
  const [syncDefault, setSyncDefault] = useState<boolean | null>(null)
  const [syncSaving, setSyncSaving] = useState(false)

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
    setStatusLoading(false)
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
    api.cloudSync().then((res) => setSyncDefault(res.sync_default)).catch(() => setSyncDefault(null))
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

  async function toggleSyncDefault() {
    if (syncDefault === null || syncSaving) return
    setSyncSaving(true)
    setActionError(null)
    try {
      const res = await api.cloudSetSyncDefault(!syncDefault)
      setSyncDefault(res.sync_default)
    } catch (err) {
      setActionError(t('cloud.saveFailed', { message: err instanceof Error ? err.message : String(err) }))
    } finally {
      setSyncSaving(false)
    }
  }

  async function startLogin() {
    if (loginBusy) return
    setLoginBusy(true)
    setActionError(null)
    try {
      const r = await api.cloudLogin(loginCloudURL.trim() || DEFAULT_CLOUD_URL)
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

  async function logout(forget = false) {
    if (logoutBusy) return
    if (forget && !window.confirm(t('cloud.forgetConfirm'))) return
    setLogoutBusy(true)
    setActionError(null)
    try {
      const previousCloudURL = status?.cloud_url
      setStatus(forget ? await api.cloudForget() : await api.cloudLogout())
      setPairings([])
      setLoginPanel(null)
      setLoginCloudURL(previousCloudURL || DEFAULT_CLOUD_URL)
      setCustomServer(!!previousCloudURL && previousCloudURL !== DEFAULT_CLOUD_URL)
    } catch (err) {
      setActionError(err instanceof Error ? err.message : String(err))
    } finally {
      setLogoutBusy(false)
    }
  }

  const pulsing = !!status?.logged_in && status.state === 'connecting'
  const badgeColor = dotColor(status)
  const stateLabel = statusLoading ? t('common.loading') : !status?.logged_in ? t('cloud.notLoggedIn') : t(`cloud.status.${status.state}`)
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

          {/* New local sessions follow this default; existing sessions retain
              their per-session switch. Kept beside the relay settings so the
              cloud behaviour is configured in one place. */}
          <div className={ROW}>
            <div className="min-w-0 flex-1">
              <div className="text-[12px] font-medium text-[var(--color-foreground)]">{t('cloud.syncDefaultTitle')}</div>
              <div className="text-[11px] text-[var(--color-muted-foreground)]">{t('cloud.syncDefaultDesc')}</div>
            </div>
            <Switch
              on={!!syncDefault}
              onClick={() => void toggleSyncDefault()}
              disabled={syncDefault === null || syncSaving}
              ariaLabel={t('cloud.syncDefaultTitle')}
              title={syncDefault ? t('common.disable') : t('common.enable')}
            />
          </div>

          {/* Pairing requests are approved or denied directly in Desktop. */}
          <div className="text-[10px] font-medium uppercase tracking-wider text-[var(--color-muted-foreground)]">
            {t('cloud.pairingSection')}
          </div>

          <div className="text-[11px] leading-relaxed text-[var(--color-muted-foreground)]">
            {t('cloud.pairingReviewHint')}
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

          {pairings.length === 0 && <div className={ROW}>{t('cloud.noPairingRequests')}</div>}

          {/* Logout. */}
          <div className="flex justify-end gap-2">
            <button
              type="button"
              disabled={logoutBusy}
              onClick={() => void logout(true)}
              className={`${BTN_GHOST} ${BTN_SM} text-[var(--color-muted-foreground)] hover:text-[var(--color-destructive)]`}
            >
              {t('cloud.forgetDevice')}
            </button>
            <button
              type="button"
              disabled={logoutBusy}
              onClick={() => void logout(false)}
              className={`${BTN_GHOST} ${BTN_SM} text-[var(--color-muted-foreground)] hover:text-[var(--color-destructive)]`}
            >
              <ArrowRightOnRectangleIcon className="h-3.5 w-3.5" />
              {t('cloud.logout')}
            </button>
          </div>
        </>
      ) : statusLoading ? (
        <div className={`${ROW} justify-center text-[11px] text-[var(--color-muted-foreground)]`}>{t('common.loading')}</div>
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
        <div className={`${ROW} flex-col items-stretch gap-3`}>
          <div className="min-w-0 flex-1">
            <div className="text-[12px] font-medium text-[var(--color-foreground)]">{t('cloud.login')}</div>
            <div className="text-[11px] leading-relaxed text-[var(--color-muted-foreground)]">{t('cloud.loginIntro')}</div>
          </div>
          <label className="grid gap-1 text-[11px] text-[var(--color-muted-foreground)]">
            <span>{t('cloud.server')}</span>
            {customServer ? (
              <input
                type="url"
                value={loginCloudURL}
                onChange={(event) => setLoginCloudURL(event.target.value)}
                placeholder={DEFAULT_CLOUD_URL}
                className="h-8 rounded-[var(--radius-md)] border border-[var(--color-border)] bg-[var(--color-background)] px-2.5 font-mono text-[11px] text-[var(--color-foreground)] outline-none focus:border-[var(--color-accent-neutral)]"
              />
            ) : (
              <span className="font-mono text-[var(--color-foreground)]">{DEFAULT_CLOUD_URL}</span>
            )}
          </label>
          <div className="flex items-center justify-between gap-3">
            <button
              type="button"
              className="text-[11px] text-[var(--color-accent-neutral)] underline-offset-2 hover:underline"
              onClick={() => {
                setCustomServer((value) => !value)
                setLoginCloudURL(DEFAULT_CLOUD_URL)
              }}
            >
              {customServer ? t('common.cancel') : t('cloud.useCustomServer')}
            </button>
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
