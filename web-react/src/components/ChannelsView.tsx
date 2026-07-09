/**
 * ChannelsView — external messaging channels (WeChat).
 *
 * Faithful port of web/src/components/ChannelsView.vue. Two-column stage:
 *   LEFT  — a brand-tinted promo card (eyebrow, title, lede, three features,
 *           connect CTA). While scanning it shows the WeChat login QR; while
 *           connected it shows a status row + disconnect button.
 *   RIGHT — a static, purely-decorative phone mockup showing a fake WeChat
 *           conversation (approval card + done summary pill). No logic.
 *
 * No BLE here (BLE lives in SettingsDialog). The QR flow mirrors the Vue
 * original: login → render QR → poll channel status every 2s (capped at 3m)
 * → detect scan → online → logout. The view owns its own scan lifecycle.
 *
 * Strings are ported from web/src/i18n/locales/en.ts (channels.* + the
 * settings.channels.* keys the Vue file reuses).
 */

import { useCallback, useEffect, useRef, useState } from 'react'
import {
  SignalIcon,
  CursorArrowRaysIcon,
  BellAlertIcon,
  QrCodeIcon,
  DevicePhoneMobileIcon,
  CheckBadgeIcon,
} from '@heroicons/react/24/outline'
import QRCode from 'qrcode'
import { api } from '../lib/api'
import { useAppDispatch } from '../app/hooks'
import { uiActions } from '../app/store'

// Finer-grained string the QR flow drives.
type ChannelState = 'none' | 'scanning' | 'enabled' | 'disabled'

interface Feature {
  icon: typeof SignalIcon
  title: string
  desc: string
}

const FEATURES: Feature[] = [
  {
    icon: CursorArrowRaysIcon,
    title: 'One-tap approvals',
    desc: 'Approve or deny a tool call right from the WeChat message — no need to open the app.',
  },
  {
    icon: BellAlertIcon,
    title: 'Done notifications',
    desc: 'Get pinged the moment a scheduled automation or background task finishes.',
  },
  {
    icon: QrCodeIcon,
    title: 'Scan to connect',
    desc: 'Pair in seconds with a QR code. Disconnect any time from Settings → Channels.',
  },
]

export function ChannelsView() {
  const dispatch = useAppDispatch()
  // ── WeChat channel state ──
  const [channelAvailable, setChannelAvailable] = useState(false)
  const [channelState, setChannelState] = useState<ChannelState>('none')
  const [channelLoading, setChannelLoading] = useState(false)
  const [qrDataUrl, setQrDataUrl] = useState('')
  const [loginReminder, setLoginReminder] = useState(false)

  // Polling handles, on refs so teardown and a fresh poll share the same slots.
  const pollIntervalRef = useRef<ReturnType<typeof setInterval> | null>(null)
  const pollTimeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  const fetchStatus = useCallback(async () => {
    try {
      const ch = await api.channelStatus()
      setChannelAvailable(ch.available)
      setChannelState((ch.state as ChannelState) || 'none')
      dispatch(uiActions.setChannelState({ available: ch.available, enabled: ch.state === 'enabled' }))
    } catch {
      /* ignore */
    }
  }, [dispatch])

  // Initial load.
  useEffect(() => {
    void fetchStatus()
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
            dispatch(uiActions.setChannelState({ available: true, enabled: next === 'enabled' }))
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
    [dispatch, stopPoll],
  )

  const channelLogin = useCallback(async () => {
    setChannelLoading(true)
    try {
      const result = await api.channelLogin()
      // Render the QR as a data URL (img src). Colors resolve at call time so a
      // theme switch mid-scan re-renders cleanly.
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
      setChannelAvailable(false)
      dispatch(uiActions.setChannelState({ available: false, enabled: false }))
      setLoginReminder(false)
    } catch (err) {
      console.error('Channel logout failed:', err)
    }
    setChannelLoading(false)
  }, [dispatch])

  const channelEnable = useCallback(async () => {
    setChannelLoading(true)
    try {
      const res = await api.channelEnable()
      setChannelAvailable(true)
      setChannelState((res.state as ChannelState) || 'enabled')
      dispatch(uiActions.setChannelState({ available: true, enabled: res.state === 'enabled' }))
    } catch (err) {
      console.error('Channel enable failed:', err)
    } finally {
      setChannelLoading(false)
    }
  }, [dispatch])

  const channelDisable = useCallback(async () => {
    setChannelLoading(true)
    try {
      const res = await api.channelDisable()
      setChannelAvailable(true)
      setChannelState((res.state as ChannelState) || 'disabled')
      dispatch(uiActions.setChannelState({ available: true, enabled: false }))
    } catch (err) {
      console.error('Channel disable failed:', err)
    } finally {
      setChannelLoading(false)
    }
  }, [dispatch])

  const isConnected = channelState === 'enabled'
  const isScanning = channelState === 'scanning'

  return (
    <div className="page-surface flex min-h-0 flex-1 flex-col">
      <header className="flex h-[var(--header-height)] shrink-0 items-center gap-2 border-b border-[var(--color-border)] bg-[var(--color-surface)] px-4">
        <SignalIcon className="h-4 w-4 text-[var(--color-primary)]" />
        <h1 className="text-sm font-medium">Channels</h1>
      </header>

      <div className="min-h-0 flex-1 overflow-y-auto">
        <div className="mx-auto w-full max-w-[56rem] px-5 pb-8">
          {/* Two-column stage: promo card (left) + phone mock (right). Stacks
              below 860px, mirroring the Vue media query. */}
          <div className="chan-stage">
            {/* ── LEFT: promo / connection card ── */}
            <div className="promo-card">
              <div className="promo-glow" aria-hidden />
              <div className="promo-inner">
                <span className="promo-eyebrow">
                  <SignalIcon className="h-3.5 w-3.5" />
                  Channels
                </span>
                <h2 className="promo-title">
                  Approve and monitor from <span className="accent">your phone</span>
                </h2>
                <p className="promo-lede">
                  Link WeChat once. jcode forwards every approval request and task-completion notice
                  straight to your chat — so a long-running agent never waits on you being at the
                  desk.
                </p>

                {/* Connect flow: QR while scanning */}
                {isScanning && (
                  <div className="qr-block">
                    {qrDataUrl && (
                      /* eslint-disable-next-line @next/next/no-img-element -- local data URL */
                      <img
                        src={qrDataUrl}
                        alt="WeChat login QR code"
                        className="qr-canvas"
                        width={200}
                        height={200}
                      />
                    )}
                    <p className="qr-hint">Scan with WeChat to connect</p>
                  </div>
                )}

                {/* Connected state */}
                {!isScanning && isConnected && (
                  <div className="connected">
                    <div className="conn-status">
                      <span className="conn-dot" />
                      <span>Connected</span>
                    </div>
                    {loginReminder && (
                      <p className="conn-reminder">
                        Please send any message to the WeChat bot now to activate notifications.
                        Once activated, you can receive notifications for 24 hours.
                      </p>
                    )}
                    <div className="flex flex-wrap gap-2">
                      <button
                        type="button"
                        onClick={channelDisable}
                        disabled={channelLoading}
                        className="btn-outline"
                      >
                        Disable
                      </button>
                      <button
                        type="button"
                        onClick={channelLogout}
                        disabled={channelLoading}
                        className="btn-outline"
                      >
                        Disconnect
                      </button>
                    </div>
                  </div>
                )}

                {/* Feature list + CTA (disconnected) */}
                {!isScanning && !isConnected && (
                  <>
                    <div className="promo-features">
                      {FEATURES.map((f) => {
                        const Icon = f.icon
                        return (
                          <div key={f.title} className="promo-feat">
                            <span className="feat-ic">
                              <Icon className="h-4 w-4" />
                            </span>
                            <div>
                              <h4>{f.title}</h4>
                              <p>{f.desc}</p>
                            </div>
                          </div>
                        )
                      })}
                    </div>

                    <div className="promo-cta">
                      {channelAvailable && channelState === 'disabled' ? (
                        <button
                          type="button"
                          onClick={channelEnable}
                          disabled={channelLoading}
                          className="btn-primary"
                        >
                          <DevicePhoneMobileIcon className="h-4 w-4" />
                          {channelLoading ? 'Loading…' : 'Enable'}
                        </button>
                      ) : (
                        <button
                          type="button"
                          onClick={channelLogin}
                          disabled={channelLoading}
                          className="btn-primary"
                        >
                          <DevicePhoneMobileIcon className="h-4 w-4" />
                          {channelLoading ? 'Loading…' : 'Connect'}
                        </button>
                      )}
                      {!channelAvailable && <span className="cta-hint">No channels configured</span>}
                    </div>
                  </>
                )}
              </div>
            </div>

            {/* ── RIGHT: phone mock — what the remote experience looks like.
                  Static decorative HTML/CSS, no logic. Ported verbatim from
                  the Vue template. ── */}
            <div className="phone-col">
              <div className="phone">
                <div className="phone-notch" />
                <div className="phone-screen">
                  <div className="phone-status">
                    <span>9:41</span>
                    <span className="bat" />
                  </div>

                  <div className="wc-msg">
                    <div className="wc-head">
                      <span className="wc-avatar">j</span>
                      <span className="wc-name">jcode</span>
                      <span className="wc-time">now</span>
                    </div>
                    <div className="wc-body">
                      <b>Approval needed</b>
                      <br />
                      <span className="muted">Run</span> <code>git push origin</code>{' '}
                      <span className="muted">?</span>
                    </div>
                    <div className="wc-actions">
                      <button type="button" className="wc-btn approve">
                        Approve
                      </button>
                      <button type="button" className="wc-btn deny">
                        Deny
                      </button>
                    </div>
                  </div>

                  <div className="wc-msg-2">
                    <div className="wc-sub">jcode · now</div>
                    <div className="wc-line">✓ Nightly automations finished</div>
                    <span className="wc-pill">
                      <CheckBadgeIcon className="h-2.5 w-2.5" /> 3 tasks · 0 failed
                    </span>
                  </div>
                </div>
              </div>
            </div>
          </div>
        </div>
      </div>

      {/* ── Component-local styles ── Ports the Vue <style scoped> block.
            Tailwind v4 @layer prevents these from being purged and keeps them
            low-specificity so utilities can still override. */}
      <style>{`
.chan-stage { display: flex; gap: 40px; align-items: flex-start; padding-top: 16px; }
@media (max-width: 860px) { .chan-stage { flex-direction: column; } }

/* ── Promo card (left-anchored) ── */
.promo-card {
  flex: 1;
  min-width: 0;
  position: relative;
  background:
    radial-gradient(120% 80% at 0% 0%, color-mix(in srgb, var(--color-primary) 8%, transparent), transparent 60%),
    var(--color-background);
  border: 1px solid var(--color-border);
  border-radius: var(--radius-2xl);
  overflow: hidden;
}
.promo-glow {
  position: absolute;
  top: -90px; left: -60px;
  width: 280px; height: 280px;
  border-radius: 50%;
  background: color-mix(in srgb, var(--color-primary) 13%, transparent);
  filter: blur(46px);
  opacity: 0.8;
  pointer-events: none;
}
.promo-inner { position: relative; padding: 32px; display: flex; flex-direction: column; }
.promo-eyebrow {
  align-self: flex-start;
  display: inline-flex;
  align-items: center;
  gap: 7px;
  font-size: 11px;
  font-weight: 600;
  letter-spacing: 0.04em;
  text-transform: uppercase;
  color: var(--color-primary);
  padding: 5px 11px;
  border-radius: var(--radius-pill);
  background: color-mix(in srgb, var(--color-primary) 8%, transparent);
  border: 1px solid color-mix(in srgb, var(--color-primary) 35%, transparent);
}
.promo-title {
  font-size: 26px;
  font-weight: 600;
  letter-spacing: -0.02em;
  margin: 18px 0 0;
  line-height: 1.15;
}
.promo-title .accent { color: var(--color-primary); }
.promo-lede {
  font-size: 13.5px;
  color: var(--color-muted-foreground);
  line-height: 1.6;
  margin: 14px 0 0;
  max-width: 44ch;
}

.promo-features { display: flex; flex-direction: column; gap: 16px; margin: 24px 0 0; }
.promo-feat { display: flex; gap: 13px; align-items: flex-start; }
.feat-ic {
  display: grid; place-items: center;
  width: 36px; height: 36px; flex-shrink: 0;
  border-radius: var(--radius-lg);
  background: color-mix(in srgb, var(--color-foreground) 8%, transparent);
  border: 1px solid color-mix(in srgb, var(--color-foreground) 30%, transparent);
  color: var(--color-foreground);
}
.promo-feat h4 { font-size: 13.5px; font-weight: 600; margin: 3px 0 3px; }
.promo-feat p { font-size: 12.5px; color: var(--color-muted-foreground); line-height: 1.5; margin: 0; max-width: 42ch; }

.promo-cta { display: flex; align-items: center; gap: 14px; margin-top: 26px; flex-wrap: wrap; }
.btn-primary {
  display: inline-flex; align-items: center; gap: 8px;
  padding: 10px 18px;
  background: var(--color-primary);
  color: var(--color-on-primary, #fff);
  border: none;
  border-radius: var(--radius-pill);
  font-size: 13px;
  font-weight: 600;
  cursor: pointer;
  box-shadow: 0 4px 16px -4px color-mix(in srgb, var(--color-primary) 60%, transparent);
  transition: box-shadow 0.15s, opacity 0.15s;
}
.btn-primary:hover:not(:disabled) { box-shadow: 0 6px 22px -5px color-mix(in srgb, var(--color-primary) 70%, transparent); }
.btn-primary:disabled { opacity: 0.6; cursor: not-allowed; }
.cta-hint { font-size: 11.5px; color: var(--color-muted-foreground); }

/* ── Connect (QR) ── */
.qr-block { display: flex; flex-direction: column; align-items: center; gap: 10px; margin-top: 22px; }
.qr-canvas { border: 1px solid var(--color-border); border-radius: var(--radius-md); }
.qr-hint { font-size: 12px; color: var(--color-muted-foreground); }

/* ── Connected ── */
.connected { margin-top: 22px; display: flex; flex-direction: column; gap: 14px; }
.conn-status { display: inline-flex; align-items: center; gap: 8px; font-size: 13.5px; font-weight: 600; color: var(--color-success); }
.conn-dot { width: 8px; height: 8px; border-radius: var(--radius-pill); background: var(--color-success); }
.conn-reminder {
  font-size: 12px; line-height: 1.5; color: var(--color-foreground);
  background: color-mix(in srgb, var(--color-primary) 10%, transparent);
  border: 1px solid color-mix(in srgb, var(--color-primary) 30%, transparent);
  border-radius: var(--radius-lg); padding: 10px 12px;
  margin: 0;
}
.btn-outline {
  align-self: flex-start;
  padding: 7px 14px;
  font-size: 12.5px;
  font-weight: 500;
  border-radius: var(--radius-lg);
  border: 1px solid var(--color-border);
  background: var(--color-surface);
  color: var(--color-foreground);
  cursor: pointer;
  transition: background 0.15s;
}
.btn-outline:hover:not(:disabled) { background: var(--color-muted); }
.btn-outline:disabled { opacity: 0.6; cursor: not-allowed; }

/* ── Phone mock (right) ── */
.phone-col { flex: 0 0 auto; display: flex; padding-top: 24px; }
.phone {
  width: 240px;
  border: 2px solid var(--color-foreground);
  border-radius: 32px;
  padding: 10px;
  background: var(--color-background);
  box-shadow: var(--shadow-xl);
  position: relative;
}
.phone-notch {
  position: absolute; top: 10px; left: 50%; transform: translateX(-50%);
  width: 72px; height: 18px;
  background: var(--color-foreground);
  border-radius: 0 0 12px 12px;
}
.phone-screen {
  border-radius: 24px;
  overflow: hidden;
  background: var(--color-muted);
  height: 400px;
  padding: 32px 12px 14px;
  display: flex;
  flex-direction: column;
  gap: 11px;
}
.phone-status { display: flex; align-items: center; justify-content: space-between; font-size: 9px; color: var(--color-muted-foreground); padding: 0 6px; }
.bat { width: 16px; height: 8px; border: 1px solid currentColor; border-radius: 2px; position: relative; }
.bat::after { content: ''; position: absolute; inset: 1px; background: currentColor; border-radius: 1px; }

.wc-msg, .wc-msg-2 { background: var(--color-surface); border: 1px solid var(--color-border); border-radius: 12px; }
.wc-msg { padding: 12px 13px; box-shadow: var(--shadow-sm); }
.wc-head { display: flex; align-items: center; gap: 7px; margin-bottom: 8px; }
.wc-avatar { width: 22px; height: 22px; border-radius: 6px; background: var(--color-primary); display: grid; place-items: center; color: #fff; font-size: 10px; font-weight: 700; font-family: var(--font-mono); }
.wc-name { font-size: 11.5px; font-weight: 600; }
.wc-time { margin-left: auto; font-size: 9px; color: var(--color-muted-foreground); }
.wc-body { font-size: 11.5px; line-height: 1.5; color: var(--color-foreground); }
.wc-body .muted { color: var(--color-muted-foreground); }
.wc-body code { font-family: var(--font-mono); font-size: 10px; background: var(--color-muted); padding: 1px 4px; border-radius: 3px; }
.wc-actions { display: flex; gap: 8px; margin-top: 10px; }
.wc-btn { flex: 1; padding: 8px 0; border-radius: 8px; font-size: 11px; font-weight: 600; border: none; cursor: default; }
.wc-btn.approve { background: #07C160; color: #fff; }
.wc-btn.deny { background: var(--color-muted); color: var(--color-foreground); border: 1px solid var(--color-border); }
.wc-msg-2 { padding: 10px 13px; }
.wc-sub { font-size: 10px; color: var(--color-muted-foreground); margin-bottom: 3px; }
.wc-line { font-size: 11.5px; font-weight: 500; }
.wc-pill { display: inline-flex; align-items: center; gap: 4px; font-size: 10px; font-weight: 600; color: #07C160; background: color-mix(in srgb, #07C160 12%, transparent); padding: 2px 8px; border-radius: var(--radius-pill); margin-top: 7px; }
`}</style>
    </div>
  )
}
