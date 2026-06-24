<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted, nextTick, watch } from 'vue'
import {
  SignalIcon,
  CursorArrowRaysIcon,
  BellAlertIcon,
  QrCodeIcon,
  DevicePhoneMobileIcon,
  CheckBadgeIcon,
} from '@heroicons/vue/24/outline'
import QRCode from 'qrcode'
import { useI18n } from 'vue-i18n'
import { useChatStore } from '@/stores/chat'
import { api } from '@/composables/api'
import PageSurface from '@/components/PageSurface.vue'

// Channels is a page inside the shell (same inset-surface geometry as
// AutomationsView via the shared PageSurface), not an overlay. The parent
// (<main> in App.vue) mounts this via v-if, so the channel-status fetch fires
// on entry and the poll tears down on exit. The WeChat login/QR/poll logic
// mirrors SettingsDialog's Channels tab (kept local on purpose — a shared
// composable was deferred to avoid touching SettingsDialog).
const { t } = useI18n()
const store = useChatStore()

// Channel state. `channelState` is the finer-grained string the QR flow needs;
// store.channelEnabled is the derived boolean the rest of the app reads.
const channelAvailable = ref(false)
const channelState = ref<'none' | 'scanning' | 'enabled' | 'disabled'>('none')
const channelLoading = ref(false)
const channelQRContent = ref('')
const channelLoginReminder = ref(false)
const qrCanvas = ref<HTMLCanvasElement | null>(null)

async function fetchStatus() {
  try {
    const ch = await api.channelStatus()
    channelAvailable.value = ch.available
    channelState.value = (ch.state as typeof channelState.value) || 'none'
    store.channelEnabled = ch.state === 'enabled'
  } catch { /* ignore */ }
}

onMounted(() => { void fetchStatus() })

// ── WeChat connect (QR) ── mirrored from SettingsDialog so this page owns its
// own scan lifecycle without depending on the settings dialog being open.
async function drawChannelQR() {
  await nextTick()
  if (!qrCanvas.value || !channelQRContent.value) return
  // Resolve QR colors from design tokens so the code follows the active theme.
  const root = document.documentElement
  const fg = getComputedStyle(root).getPropertyValue('--term-fg').trim() || '#18181b'
  const bg = getComputedStyle(root).getPropertyValue('--color-surface').trim() || '#ffffff'
  await QRCode.toCanvas(qrCanvas.value, channelQRContent.value, {
    width: 200,
    margin: 2,
    color: { dark: fg, light: bg },
  })
}

async function channelLogin() {
  channelLoading.value = true
  try {
    const result = await api.channelLogin()
    channelQRContent.value = result.qr_content
    channelState.value = 'scanning'
    await drawChannelQR()
    pollChannelState()
  } catch (err) {
    console.error('Channel login failed:', err)
  }
  channelLoading.value = false
}

async function channelLogout() {
  channelLoading.value = true
  try {
    await api.channelLogout()
    channelState.value = 'none'
    channelQRContent.value = ''
    store.channelEnabled = false
    channelAvailable.value = false
  } catch (err) {
    console.error('Channel logout failed:', err)
  }
  channelLoading.value = false
}

let channelPollInterval: ReturnType<typeof setInterval> | null = null
let channelPollTimeout: ReturnType<typeof setTimeout> | null = null

function stopChannelPoll() {
  if (channelPollInterval) { clearInterval(channelPollInterval); channelPollInterval = null }
  if (channelPollTimeout) { clearTimeout(channelPollTimeout); channelPollTimeout = null }
}

function pollChannelState() {
  stopChannelPoll()
  const previousState = channelState.value
  channelPollInterval = setInterval(async () => {
    try {
      const ch = await api.channelStatus()
      if (ch.state === 'enabled' || ch.state === 'disabled') {
        channelState.value = ch.state as typeof channelState.value
        channelQRContent.value = ''
        channelAvailable.value = true
        store.channelEnabled = ch.state === 'enabled'
        if (ch.state === 'enabled' && previousState === 'scanning') {
          channelLoginReminder.value = true
        }
        stopChannelPoll()
      }
    } catch { /* ignore */ }
  }, 2000)
  channelPollTimeout = setTimeout(stopChannelPoll, 180000)
}

onUnmounted(() => { stopChannelPoll() })

const isConnected = computed(() => channelState.value === 'enabled')
const isScanning = computed(() => channelState.value === 'scanning')

// Re-draw the QR if it's mid-scan and the canvas remounts (e.g. after a theme
// switch re-evaluates the v-if). Mirrors the settings tab re-entry behavior.
watch(channelQRContent, (v) => { if (v) void drawChannelQR() })

const features = computed(() => [
  { icon: CursorArrowRaysIcon, title: t('channels.features.approvals.title'), desc: t('channels.features.approvals.desc') },
  { icon: BellAlertIcon, title: t('channels.features.notifications.title'), desc: t('channels.features.notifications.desc') },
  { icon: QrCodeIcon, title: t('channels.features.scan.title'), desc: t('channels.features.scan.desc') },
])
</script>

<template>
  <PageSurface :title="t('nav.channels')">
      <div class="chan-stage">
        <!-- LEFT: promo / connection card -->
        <div class="promo-card">
          <div class="promo-glow" aria-hidden="true" />
          <div class="promo-inner">
            <span class="promo-eyebrow">
              <SignalIcon class="w-3.5 h-3.5" /> {{ t('nav.channels') }}
            </span>
            <h2 class="promo-title" v-html="t('channels.promo.title')" />
            <p class="promo-lede">{{ t('channels.promo.lede') }}</p>

            <!-- Connect flow: QR while scanning -->
            <div v-if="isScanning" class="qr-block">
              <canvas ref="qrCanvas" class="qr-canvas" />
              <p class="qr-hint">{{ t('settings.channels.scanQr') }}</p>
            </div>

            <!-- Connected state -->
            <div v-else-if="isConnected" class="connected">
              <div class="conn-status">
                <span class="conn-dot" />
                <span>{{ t('channels.connected') }}</span>
              </div>
              <p v-if="channelLoginReminder" class="conn-reminder">{{ t('settings.channels.activateBody') }}</p>
              <button class="btn-outline" :disabled="channelLoading" @click="channelLogout">
                {{ t('settings.channels.disconnect') }}
              </button>
            </div>

            <!-- Feature list + CTA (disconnected) -->
            <template v-else>
              <div class="promo-features">
                <div v-for="f in features" :key="f.title" class="promo-feat">
                  <span class="feat-ic"><component :is="f.icon" class="w-4 h-4" /></span>
                  <div>
                    <h4>{{ f.title }}</h4>
                    <p>{{ f.desc }}</p>
                  </div>
                </div>
              </div>

              <div class="promo-cta">
                <button class="btn-primary" :disabled="channelLoading" @click="channelLogin">
                  <DevicePhoneMobileIcon class="w-4 h-4" />
                  {{ channelLoading ? t('settings.channels.loadingHint') : t('settings.channels.connect') }}
                </button>
                <span v-if="!channelAvailable" class="cta-hint">{{ t('settings.channels.noneConfigured') }}</span>
              </div>
            </template>
          </div>
        </div>

        <!-- RIGHT: phone mock — what the remote experience looks like -->
        <div class="phone-col">
          <div class="phone">
            <div class="phone-notch" />
            <div class="phone-screen">
              <div class="phone-status"><span>9:41</span><span class="bat" /></div>

              <div class="wc-msg">
                <div class="wc-head">
                  <span class="wc-avatar">j</span>
                  <span class="wc-name">jcode</span>
                  <span class="wc-time">{{ t('channels.mock.now') }}</span>
                </div>
                <div class="wc-body">
                  <b>{{ t('channels.mock.approvalTitle') }}</b><br>
                  <span class="muted">{{ t('channels.mock.run') }}</span>
                  <code>git push origin</code>
                  <span class="muted">?</span>
                </div>
                <div class="wc-actions">
                  <button class="wc-btn approve">{{ t('channels.mock.approve') }}</button>
                  <button class="wc-btn deny">{{ t('channels.mock.deny') }}</button>
                </div>
              </div>

              <div class="wc-msg-2">
                <div class="wc-sub">jcode · {{ t('channels.mock.now') }}</div>
                <div class="wc-line">✓ {{ t('channels.mock.done') }}</div>
                <span class="wc-pill">
                  <CheckBadgeIcon class="w-2.5 h-2.5" /> {{ t('channels.mock.summary') }}
                </span>
              </div>
            </div>
          </div>
        </div>
      </div>
  </PageSurface>
</template>

<style scoped>
/* The page surface (inset chrome + title head + scroll body) is owned by
 * PageSurface. This component styles only its own content. Content column is
 * centered + inset to match the chat timeline, scoped to PageSurface's body. */
:deep(.page-body) > * { max-width: 56rem; margin-left: auto; margin-right: auto; padding: 0 20px 32px; }

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
.promo-title :deep(.accent) { color: var(--color-primary); }
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
</style>
