/**
 * ChannelsView — external messaging channels (WeChat + Bluetooth/BLE).
 *
 * Ports the WeChat QR-login flow + enable/disable + the BLE status toggle. The
 * QR flow mirrors SettingsDialog's Channels tab (login → display QR → poll
 * channel status → detect scan → online → logout), kept local so this page owns
 * its own scan lifecycle. The BLE card is only rendered when the backend reports
 * BLE support (web builds report `available: false`, hiding it).
 */

import { useCallback, useEffect, useRef, useState } from 'react'
import {
  ChatBubbleOvalLeftIcon,
  QrCodeIcon,
  PowerIcon,
  SignalIcon,
} from '@heroicons/react/24/outline'
import QRCode from 'qrcode'
import { api } from '../lib/api'

// Finer-grained string the QR flow drives; `enabled` is the boolean the rest of
// the app derives from it.
type ChannelState = 'none' | 'scanning' | 'enabled' | 'disabled'

function stateLabel(state: ChannelState): string {
  switch (state) {
    case 'enabled':
      return 'Connected'
    case 'disabled':
      return 'Disconnected'
    case 'scanning':
      return 'Scanning…'
    default:
      return 'Not configured'
  }
}

export function ChannelsView() {
  // ── WeChat channel ──
  const [channelAvailable, setChannelAvailable] = useState(false)
  const [channelState, setChannelState] = useState<ChannelState>('none')
  const [channelLoading, setChannelLoading] = useState(false)
  const [qrDataUrl, setQrDataUrl] = useState('')
  const [loginReminder, setLoginReminder] = useState(false)

  // ── Bluetooth (BLE) ── desktop-only status channel. `available` is a
  // compile-time feature flag from the backend; web builds report false.
  const [bleAvailable, setBleAvailable] = useState(false)
  const [bleEnabled, setBleEnabled] = useState(false)
  const [bleSaving, setBleSaving] = useState(false)

  // Polling handles. Stored on refs so the teardown path and a fresh poll share
  // the same slots without re-triggering the effect.
  const pollIntervalRef = useRef<ReturnType<typeof setInterval> | null>(null)
  const pollTimeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  const fetchStatus = useCallback(async () => {
    try {
      const ch = await api.channelStatus()
      setChannelAvailable(ch.available)
      setChannelState((ch.state as ChannelState) || 'none')
    } catch {
      /* ignore */
    }
  }, [])

  // Initial load: channel status + BLE status.
  useEffect(() => {
    void fetchStatus()
    api
      .channelBLEStatus()
      .then((ble) => {
        setBleAvailable(ble.available)
        setBleEnabled(ble.enabled)
      })
      .catch(() => {})
  }, [fetchStatus])

  const stopPoll = useCallback(() => {
    if (pollIntervalRef.current) {
      clearInterval(pollIntervalRef.current)
      pollIntervalRef.current = null
    }
    if (pollTimeoutRef.current) {
      clearTimeout(pollTimeoutRef.current)
      pollTimeoutRef.current = null
    }
  }, [])

  // Tear down the poll on unmount.
  useEffect(() => stopPoll, [stopPoll])

  // Poll channel status every 2s while a QR is pending, to detect when the user
  // has scanned it. Safety-capped at 3 minutes regardless of outcome.
  const pollChannelState = useCallback(
    (previousState: ChannelState) => {
      stopPoll()
      pollIntervalRef.current = setInterval(async () => {
        try {
          const ch = await api.channelStatus()
          if (ch.state === 'enabled' || ch.state === 'disabled') {
            const next = ch.state as ChannelState
            setChannelState(next)
            setQrDataUrl('')
            setChannelAvailable(true)
            // Show the "activate" reminder the first time we go online via the
            // login flow (scanning → enabled).
            if (next === 'enabled' && previousState === 'scanning') {
              setLoginReminder(true)
            }
            stopPoll()
          }
        } catch {
          /* keep polling */
        }
      }, 2000)
      pollTimeoutRef.current = setTimeout(stopPoll, 180000)
    },
    [stopPoll],
  )

  const channelLogin = useCallback(async () => {
    setChannelLoading(true)
    try {
      const result = await api.channelLogin()
      // Render the QR as a data URL (img src). QRCode.toDataURL resolves theme
      // colors at call time so a theme switch mid-scan re-renders cleanly.
      const fg =
        getComputedStyle(document.documentElement).getPropertyValue('--color-foreground').trim() ||
        '#18181b'
      const bg =
        getComputedStyle(document.documentElement).getPropertyValue('--color-surface').trim() ||
        '#ffffff'
      const url = await QRCode.toDataURL(result.qr_content, {
        width: 200,
        margin: 2,
        color: { dark: fg, light: bg },
      })
      setQrDataUrl(url)
      setChannelState('scanning')
      pollChannelState('scanning')
    } catch (err) {
      console.error('Channel login failed:', err)
    }
    setChannelLoading(false)
  }, [pollChannelState])

  const channelLogout = useCallback(async () => {
    setChannelLoading(true)
    try {
      await api.channelLogout()
      setChannelState('none')
      setQrDataUrl('')
    } catch (err) {
      console.error('Channel logout failed:', err)
    }
    setChannelLoading(false)
  }, [])

  const channelEnable = useCallback(async () => {
    setChannelLoading(true)
    try {
      const res = await api.channelEnable()
      setChannelState((res.state as ChannelState) || 'enabled')
      setChannelAvailable(true)
    } catch (err) {
      console.error('Channel enable failed:', err)
    }
    setChannelLoading(false)
  }, [])

  const channelDisable = useCallback(async () => {
    setChannelLoading(true)
    try {
      const res = await api.channelDisable()
      setChannelState((res.state as ChannelState) || 'disabled')
      setChannelAvailable(true)
    } catch (err) {
      console.error('Channel disable failed:', err)
    }
    setChannelLoading(false)
  }, [])

  const toggleBLE = useCallback(async () => {
    if (bleSaving) return
    setBleSaving(true)
    const next = !bleEnabled
    try {
      await api.setChannelBLE(next)
      setBleEnabled(next)
    } catch (err) {
      console.error('Failed to toggle Bluetooth notifications:', err)
    }
    setBleSaving(false)
  }, [bleEnabled, bleSaving])

  // Chip color follows the channel's finer-grained state.
  const chipClass =
    channelState === 'enabled'
      ? 'text-[var(--color-success)] bg-[var(--color-success-bg)] border-[color-mix(in_srgb,var(--color-success)_35%,transparent)]'
      : channelState === 'disabled' || channelState === 'scanning'
        ? 'text-[var(--color-warning-fg)] bg-[var(--color-warning-bg)] border-[color-mix(in_srgb,var(--color-warning-fg)_35%,transparent)]'
        : 'text-[var(--color-muted-foreground)] bg-[var(--color-muted)] border-[var(--color-border)]'

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <header className="flex h-[var(--header-height)] shrink-0 items-center gap-2 border-b border-[var(--color-border)] bg-[var(--color-surface)] px-4">
        <ChatBubbleOvalLeftIcon className="h-4 w-4 text-[var(--color-primary)]" />
        <h1 className="text-sm font-medium">Channels</h1>
      </header>

      <div className="min-h-0 flex-1 overflow-y-auto p-4">
        <div className="mx-auto flex max-w-2xl flex-col gap-4">
          {/* ── WeChat card ── */}
          <section className="rounded-[var(--radius-lg)] border border-[var(--color-border)] bg-[var(--color-surface)] p-4">
            <div className="mb-3 flex items-center justify-between">
              <div className="flex items-center gap-3">
                <div className="grid h-8 w-8 place-items-center rounded-[var(--radius-md)] bg-[var(--color-muted)] text-[var(--color-foreground)]">
                  <ChatBubbleOvalLeftIcon className="h-4 w-4" />
                </div>
                <div>
                  <div className="text-sm font-medium">WeChat</div>
                  <div className="text-xs text-[var(--color-muted-foreground)]">
                    Approvals &amp; notifications channel
                  </div>
                </div>
              </div>
              <span
                className={`inline-flex items-center gap-1.5 rounded-full border px-2 py-0.5 text-[11px] font-medium ${chipClass}`}
              >
                <span
                  className="inline-block h-1.5 w-1.5 rounded-full"
                  style={{ backgroundColor: 'currentColor' }}
                />
                {stateLabel(channelState)}
              </span>
            </div>

            {/* QR while scanning */}
            {qrDataUrl && (
              <div className="flex flex-col items-center py-3">
                {/* eslint-disable-next-line @next/next/no-img-element -- local data URL, no remote loader needed */}
                <img
                  src={qrDataUrl}
                  alt="WeChat login QR code"
                  className="rounded-[var(--radius-md)] border border-[var(--color-border)]"
                  width={200}
                  height={200}
                />
                <div className="mt-2 text-[10px] text-[var(--color-muted-foreground)]">
                  Scan with WeChat to connect
                </div>
              </div>
            )}

            {/* Login-reminder banner (shown once after a scan-driven login) */}
            {loginReminder && (
              <div
                className="mt-2 flex items-start gap-2.5 rounded-[var(--radius-md)] px-4 py-3"
                style={{
                  border: '1px solid var(--color-warning-fg)',
                  backgroundColor: 'var(--color-warning-bg)',
                }}
              >
                <span className="mt-0.5 shrink-0 text-sm">⚠️</span>
                <div className="min-w-0 flex-1">
                  <div className="text-xs font-medium text-[var(--color-warning-fg)]">
                    Activate on your phone
                  </div>
                  <div
                    className="mt-0.5 text-[10px] leading-relaxed"
                    style={{ color: 'var(--color-warning-fg)', opacity: 0.8 }}
                  >
                    Open the WeChat conversation with jcode and tap the menu to start receiving
                    messages, otherwise the channel stays idle.
                  </div>
                </div>
                <button
                  type="button"
                  onClick={() => setLoginReminder(false)}
                  className="shrink-0 text-xs text-[var(--color-warning-fg)] hover:opacity-70"
                  aria-label="Dismiss reminder"
                >
                  ✕
                </button>
              </div>
            )}

            {/* Not configured — backend reports no channel available */}
            {!channelAvailable ? (
              <div className="flex flex-col items-center justify-center gap-2.5 py-8 text-center">
                <div className="grid h-9 w-9 place-items-center rounded-[var(--radius-md)] bg-[var(--color-muted)] text-[var(--color-muted-foreground)]">
                  <QrCodeIcon className="h-4 w-4" />
                </div>
                <div className="text-[13px] font-medium">No channel configured</div>
                <div className="max-w-[260px] text-[11px] leading-relaxed text-[var(--color-muted-foreground)]">
                  Set <code className="font-mono">channel.web_enabled: true</code> in
                  ~/.jcode/config.json and restart to enable the WeChat channel.
                </div>
              </div>
            ) : (
              <div className="mt-3 flex flex-wrap gap-2">
                {/* Connect (start QR flow) — only when idle */}
                {channelState === 'none' && (
                  <button
                    type="button"
                    onClick={channelLogin}
                    disabled={channelLoading}
                    className="inline-flex flex-1 items-center justify-center gap-1.5 rounded-[var(--radius-md)] bg-[var(--color-primary)] px-3 py-1.5 text-xs font-medium text-[var(--color-on-primary)] disabled:cursor-not-allowed disabled:opacity-60"
                  >
                    <QrCodeIcon className="h-3.5 w-3.5" />
                    {channelLoading ? 'Starting…' : 'Connect'}
                  </button>
                )}

                {/* Disconnect — available once logged in or disabled */}
                {(channelState === 'enabled' || channelState === 'disabled') && (
                  <button
                    type="button"
                    onClick={channelLogout}
                    disabled={channelLoading}
                    className="inline-flex flex-1 items-center justify-center gap-1.5 rounded-[var(--radius-md)] border border-[var(--color-border)] bg-transparent px-3 py-1.5 text-xs font-medium text-[var(--color-destructive)] hover:bg-[var(--color-muted)] disabled:cursor-not-allowed disabled:opacity-60"
                  >
                    <PowerIcon className="h-3.5 w-3.5" />
                    Disconnect
                  </button>
                )}

                {/* Enable / disable — independent of the QR login flow. Enable is
                    only meaningful when not already enabled; disable when on. */}
                {channelState !== 'enabled' && (
                  <button
                    type="button"
                    onClick={channelEnable}
                    disabled={channelLoading}
                    className="inline-flex items-center justify-center gap-1.5 rounded-[var(--radius-md)] bg-[var(--color-muted)] px-3 py-1.5 text-xs font-medium hover:bg-[var(--neutral-wash-soft)] disabled:cursor-not-allowed disabled:opacity-60"
                  >
                    Enable
                  </button>
                )}
                {channelState === 'enabled' && (
                  <button
                    type="button"
                    onClick={channelDisable}
                    disabled={channelLoading}
                    className="inline-flex items-center justify-center gap-1.5 rounded-[var(--radius-md)] bg-[var(--color-muted)] px-3 py-1.5 text-xs font-medium hover:bg-[var(--neutral-wash-soft)] disabled:cursor-not-allowed disabled:opacity-60"
                  >
                    Disable
                  </button>
                )}
              </div>
            )}
          </section>

          {/* ── BLE card — only when the backend advertises BLE support ── */}
          {bleAvailable && (
            <section className="flex items-center gap-3 rounded-[var(--radius-lg)] border border-[var(--color-border)] bg-[var(--color-surface)] px-4 py-3">
              <div className="grid h-8 w-8 place-items-center rounded-[var(--radius-md)] bg-[var(--color-muted)] text-[var(--color-foreground)]">
                <SignalIcon className="h-4 w-4" />
              </div>
              <div className="min-w-0 flex-1">
                <div className="text-sm font-medium">Bluetooth notifications</div>
                <div className="text-xs text-[var(--color-muted-foreground)]">
                  Receive status updates over BLE (applies on next launch).
                </div>
              </div>
              <button
                type="button"
                role="switch"
                aria-checked={bleEnabled}
                aria-label={bleEnabled ? 'Disable Bluetooth notifications' : 'Enable Bluetooth notifications'}
                title={bleEnabled ? 'Disable' : 'Enable'}
                disabled={bleSaving}
                onClick={toggleBLE}
                data-on={bleEnabled ? 'true' : 'false'}
                className="relative inline-flex h-5 w-9 shrink-0 items-center rounded-full border border-transparent transition-colors disabled:cursor-not-allowed disabled:opacity-60"
                style={{
                  backgroundColor: bleEnabled
                    ? 'var(--color-primary)'
                    : 'var(--color-muted)',
                }}
              >
                <span
                  className="inline-block h-4 w-4 transform rounded-full bg-white shadow transition-transform"
                  style={{ transform: bleEnabled ? 'translateX(18px)' : 'translateX(2px)' }}
                />
              </button>
            </section>
          )}
        </div>
      </div>
    </div>
  )
}
