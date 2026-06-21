<script setup lang="ts">
import { ref, reactive, computed, watch, nextTick, onUnmounted, inject, type Component } from 'vue'
import { useChatStore } from '@/stores/chat'
import { useTheme } from '@/composables/useTheme'
import { api } from '@/composables/api'
import type { MCPServerInfo, MCPServerRequest, SkillInfo, SSHAlias, SetupProvider, SetupModel, ProviderDetail, RemoteMeta } from '@/types/api'
import QRCode from 'qrcode'
import {
  Dialog,
  DialogPanel,
  DialogTitle,
  TransitionRoot,
  TransitionChild,
  Menu as HMenu,
  MenuButton,
  MenuItems,
  MenuItem,
} from '@headlessui/vue'
import {
  GlobeAltIcon,
  BoltIcon,
  ChatBubbleLeftIcon,
  ComputerDesktopIcon,
  KeyIcon,
  ServerIcon,
  ChevronRightIcon,
  PlusIcon,
  SignalIcon,
  ShieldCheckIcon,
  AdjustmentsHorizontalIcon,
  SwatchIcon,
  CpuChipIcon,
  ServerStackIcon,
  SparklesIcon,
  CommandLineIcon,
  BellAlertIcon,
  TrashIcon,
  ArrowLeftIcon,
  ChevronDownIcon,
  CheckIcon,
} from '@heroicons/vue/24/outline'
import { isTauri } from '@/composables/useDesktop'
import { useI18n } from 'vue-i18n'
import { SUPPORTED_LOCALES, LOCALE_LABELS, setLocale, i18n, type SupportedLocale } from '@/i18n'

const props = defineProps<{
  open: boolean
}>()

const emit = defineEmits<{
  close: []
}>()

const store = useChatStore()
const { t } = useI18n()

// Locale picker state. The active locale drives vue-i18n globally; setLocale()
// persists the choice to localStorage and updates <html lang>.
const currentLocale = computed(() => (i18n.global.locale as { value: string }).value as SupportedLocale)
const supportedLocales = SUPPORTED_LOCALES
async function chooseLocale(locale: SupportedLocale) {
  await setLocale(locale)
}

// Launch the Remote-connect wizard from the SSH tab (provided by App). We close
// Settings first so the wizard opens in the workspace context rather than
// stacking on top of the full-page settings overlay.
const openRemoteConnect = inject<(prefill?: RemoteMeta & { loadTaskUuid?: string }) => void>('openRemoteConnect')

// Open the wizard ON TOP of Settings (it stacks above via DOM order), rather
// than closing Settings first — that dumped the user back to the workspace
// (welcome) behind the wizard. Cancelling the wizard now returns to Settings;
// only a successful bind closes Settings (App handles @bound → go to the new
// remote workspace).
function launchWizard(prefill?: RemoteMeta) {
  openRemoteConnect?.(prefill)
}

function openRemoteWizard() {
  launchWizard()
}

function connectToAlias(alias: SSHAlias) {
  // addr is "user@host" where host may include ":port" (mirrors the wizard's
  // applyAlias parsing) → build a prefill the wizard jumps straight into.
  const at = alias.addr.indexOf('@')
  const user = at >= 0 ? alias.addr.slice(0, at) : 'root'
  let host = at >= 0 ? alias.addr.slice(at + 1) : alias.addr
  let port = 22
  const colon = host.lastIndexOf(':')
  if (colon >= 0) {
    port = parseInt(host.slice(colon + 1), 10) || 22
    host = host.slice(0, colon)
  }
  launchWizard({ host, port, user, remotePath: alias.path || '' })
}

const { themeChoice, setTheme, themes } = useTheme()
const darkThemes = computed(() => themes.filter((t) => t.appearance === 'dark'))
const lightThemes = computed(() => themes.filter((t) => t.appearance === 'light'))
const activeTab = ref<'general' | 'appearance' | 'providers' | 'mcp' | 'skills' | 'ssh' | 'channels' | 'shortcuts'>('general')
const mcpServers = ref<Record<string, MCPServerInfo>>({})
const sshAliases = ref<SSHAlias[]>([])
const sshCurrent = ref('local')
const mcpLoading = ref(false)

const channelAvailable = ref(false)
const channelState = ref('none')
const channelLoading = ref(false)
const channelQRContent = ref('')
const channelLoginReminder = ref(false)
const qrCanvas = ref<HTMLCanvasElement | null>(null)

// Bluetooth (BLE) status channel — desktop-only preference, persisted in config
// and applied on the next app launch.
const bleEnabled = ref(false)
const bleSaving = ref(false)

// Provider management state
const configuredProviders = ref<ProviderDetail[]>([])
const showAddProvider = ref(false)
const addProviderStep = ref<'select' | 'model' | 'apikey'>('select')
const addProviderList = ref<SetupProvider[]>([])
const addProviderModels = ref<SetupModel[]>([])
const addSelectedProvider = ref('')
const addSelectedModel = ref('')
const addApiKey = ref('')
const addBaseURL = ref('')
const addLoading = ref(false)
const addError = ref('')
const deleteConfirmId = ref('')

watch(() => props.open, async (isOpen) => {
  if (isOpen) {
    mcpEditing.value = null
    mcpLoginMessage.value = ''
    mcpLoginMessageFor.value = ''
    await loadMCP()
    loadSkills()

    try {
      const sshData = await api.sshList()
      sshAliases.value = sshData.aliases
      sshCurrent.value = sshData.current
    } catch { /* ignore */ }

    try {
      const ch = await api.channelStatus()
      channelAvailable.value = ch.available
      channelState.value = ch.state ?? 'none'
    } catch { /* ignore */ }

    // Bluetooth status channel is desktop-only; skip the request in the browser.
    if (isTauri) {
      try {
        bleEnabled.value = (await api.channelBLEStatus()).enabled
      } catch { /* ignore */ }
    }

    // Load configured providers
    try {
      configuredProviders.value = await api.listProviders()
    } catch { /* ignore */ }
  } else {
    channelQRContent.value = ''
    showAddProvider.value = false
    addError.value = ''
    deleteConfirmId.value = ''
    mcpEditing.value = null
    stopLoginPoll()
    stopChannelPoll()
  }
})

// Switching sections abandons any in-progress sub-flow on the previous tab —
// otherwise a half-filled MCP/provider form, an open delete confirmation, or a
// stale login/poll from one tab would linger when the user navigates to another.
watch(activeTab, () => {
  mcpEditing.value = null
  showAddProvider.value = false
  addError.value = ''
  deleteConfirmId.value = ''
  mcpLoginMessage.value = ''
  mcpLoginMessageFor.value = ''
  stopLoginPoll()
  stopChannelPoll()
})

async function loadMCP() {
  mcpLoading.value = true
  try {
    const result = await api.mcpList()
    mcpServers.value = result.servers
  } catch { /* ignore */ }
  mcpLoading.value = false
}

async function toggleMCP(name: string, enabled: boolean) {
  try {
    await api.mcpToggle(name, enabled)
    if (mcpServers.value[name]) {
      mcpServers.value[name].enabled = enabled
    }
    await loadMCP()
  } catch (err) {
    console.error('Failed to toggle MCP server:', err)
  }
}

function serverIcon(type: string) {
  return type === 'sse' || type === 'http' ? GlobeAltIcon : BoltIcon
}

function mcpStatusLabel(info: MCPServerInfo): string {
  if (!info.enabled) return t('settings.mcp.status.disabled')
  switch (info.status) {
    case 'connected': return t('settings.mcp.status.connected')
    case 'needs_auth': return t('settings.mcp.status.loginRequired')
    case 'error': return t('settings.mcp.status.error')
    default: return t('settings.mcp.status.configured')
  }
}

function mcpStatusColor(info: MCPServerInfo): string {
  if (!info.enabled) return 'var(--color-muted-foreground)'
  switch (info.status) {
    case 'connected': return 'var(--color-success-fg)'
    case 'needs_auth': return 'var(--color-warning-fg)'
    case 'error': return 'var(--color-error-fg)'
    default: return 'var(--color-muted-foreground)'
  }
}

// --- MCP server add/edit form ---
interface MCPHeaderRow { key: string; value: string }
interface MCPForm {
  name: string
  transport: 'local' | 'http' | 'sse'
  url: string
  command: string
  argsText: string
  headers: MCPHeaderRow[]
  timeout: string
  oauthEnabled: boolean
  clientId: string
  clientSecret: string
  scopesText: string
}

// mcpEditing: null = no form, '' = adding new, otherwise the server name being edited.
const mcpEditing = ref<string | null>(null)
const mcpSaving = ref(false)
const mcpFormError = ref('')
const mcpForm = reactive<MCPForm>({
  name: '', transport: 'http', url: '', command: '', argsText: '',
  headers: [], timeout: '', oauthEnabled: false, clientId: '', clientSecret: '', scopesText: '',
})

function resetMCPForm() {
  mcpForm.name = ''
  mcpForm.transport = 'http'
  mcpForm.url = ''
  mcpForm.command = ''
  mcpForm.argsText = ''
  mcpForm.headers = []
  mcpForm.timeout = ''
  mcpForm.oauthEnabled = false
  mcpForm.clientId = ''
  mcpForm.clientSecret = ''
  mcpForm.scopesText = ''
  mcpFormError.value = ''
}

function openAddMCP() {
  resetMCPForm()
  mcpEditing.value = ''
}

function openEditMCP(info: MCPServerInfo) {
  resetMCPForm()
  mcpForm.name = info.name
  mcpForm.transport = (info.type === 'stdio' || info.type === '') ? 'local' : (info.type as 'http' | 'sse')
  mcpForm.url = info.url ?? ''
  mcpForm.command = info.command ?? ''
  mcpForm.argsText = (info.args ?? []).join(' ')
  mcpForm.headers = Object.entries(info.headers ?? {}).map(([key, value]) => ({ key, value }))
  mcpForm.timeout = info.timeout ? String(info.timeout) : ''
  mcpForm.oauthEnabled = info.oauth
  mcpEditing.value = info.name
}

function cancelMCPEdit() {
  mcpEditing.value = null
  mcpFormError.value = ''
}

function addHeaderRow() {
  mcpForm.headers.push({ key: '', value: '' })
}

function removeHeaderRow(i: number) {
  mcpForm.headers.splice(i, 1)
}

function buildMCPRequest(): MCPServerRequest {
  const headers: Record<string, string> = {}
  for (const h of mcpForm.headers) {
    if (h.key.trim()) headers[h.key.trim()] = h.value
  }
  const req: MCPServerRequest = {
    name: mcpForm.name.trim(),
    type: mcpForm.transport,
  }
  if (mcpForm.transport === 'local') {
    req.command = mcpForm.command.trim()
    req.args = mcpForm.argsText.trim() ? mcpForm.argsText.trim().split(/\s+/) : undefined
  } else {
    req.url = mcpForm.url.trim()
    if (Object.keys(headers).length) req.headers = headers
    if (mcpForm.timeout.trim()) req.timeout = Number(mcpForm.timeout)
    if (mcpForm.oauthEnabled || mcpForm.clientId.trim()) {
      req.oauth = {
        enabled: true,
        client_id: mcpForm.clientId.trim() || undefined,
        client_secret: mcpForm.clientSecret.trim() || undefined,
        scopes: mcpForm.scopesText.trim() ? mcpForm.scopesText.trim().split(/\s+/) : undefined,
      }
    }
  }
  return req
}

async function saveMCP() {
  mcpFormError.value = ''
  if (!mcpForm.name.trim()) { mcpFormError.value = 'Server name is required'; return }
  if (mcpForm.transport === 'local' && !mcpForm.command.trim()) { mcpFormError.value = 'Command is required'; return }
  if (mcpForm.transport !== 'local' && !mcpForm.url.trim()) { mcpFormError.value = 'URL is required'; return }
  mcpSaving.value = true
  try {
    const req = buildMCPRequest()
    if (mcpEditing.value) {
      await api.mcpUpdate(mcpEditing.value, req)
    } else {
      await api.mcpCreate(req)
    }
    mcpEditing.value = null
    await loadMCP()
  } catch (err) {
    mcpFormError.value = err instanceof Error ? err.message : 'Failed to save'
  }
  mcpSaving.value = false
}

async function deleteMCP(name: string) {
  try {
    await api.mcpDelete(name)
    await loadMCP()
  } catch (err) {
    console.error('Failed to delete MCP server:', err)
  }
}

// --- MCP OAuth login ---
const mcpLoginBusy = ref('')      // server name currently logging in
const mcpLoginMessage = ref('')   // status text shown under the row
const mcpLoginMessageFor = ref('') // which server the message belongs to
let loginPollTimer: ReturnType<typeof setInterval> | null = null

function stopLoginPoll() {
  if (loginPollTimer) { clearInterval(loginPollTimer); loginPollTimer = null }
}

async function loginMCP(name: string) {
  mcpLoginBusy.value = name
  mcpLoginMessageFor.value = name
  mcpLoginMessage.value = 'Opening browser — complete authorization, then return here…'
  try {
    await api.mcpLogin(name)
  } catch (err) {
    mcpLoginMessage.value = err instanceof Error ? err.message : 'Login failed'
    mcpLoginMessageFor.value = name
    mcpLoginBusy.value = ''
    return
  }
  stopLoginPoll()
  loginPollTimer = setInterval(async () => {
    try {
      const st = await api.mcpLoginStatus(name)
      if (st.status === 'authorized') {
        stopLoginPoll()
        mcpLoginBusy.value = ''
        mcpLoginMessage.value = ''
        mcpLoginMessageFor.value = ''
        await loadMCP()
      } else if (st.status === 'error') {
        stopLoginPoll()
        mcpLoginBusy.value = ''
        mcpLoginMessageFor.value = name
        mcpLoginMessage.value = st.message || 'Login failed'
      } else if (st.status === 'needs_client_id') {
        stopLoginPoll()
        mcpLoginBusy.value = ''
        mcpLoginMessageFor.value = name
        mcpLoginMessage.value = 'This server does not support automatic registration. Edit it and set an OAuth Client ID, then log in again.'
      }
    } catch { /* keep polling */ }
  }, 1500)
}

onUnmounted(() => { stopLoginPoll(); stopChannelPoll() })

// --- Skills ---
const skills = ref<SkillInfo[]>([])
const skillsLoading = ref(false)
const skillFilter = ref<'all' | 'local' | 'builtin'>('all')
const skillSearch = ref('')

const filteredSkills = computed(() => {
  const q = skillSearch.value.trim().toLowerCase()
  return skills.value.filter((s) => {
    if (skillFilter.value === 'builtin' && !s.builtin) return false
    if (skillFilter.value === 'local' && s.builtin) return false
    if (q && !s.name.toLowerCase().includes(q) && !(s.description ?? '').toLowerCase().includes(q)) return false
    return true
  })
})

async function loadSkills() {
  skillsLoading.value = true
  try {
    skills.value = await api.skillsList()
  } catch { /* ignore */ }
  skillsLoading.value = false
}

async function toggleSkill(name: string, enabled: boolean) {
  try {
    await api.skillToggle(name, enabled)
    const sk = skills.value.find((s) => s.name === name)
    if (sk) sk.enabled = enabled
  } catch (err) {
    console.error('Failed to toggle skill:', err)
  }
}

const shortcuts = computed(() => [
  { keys: 'Enter', desc: t('settings.shortcuts.items.sendMessage') },
  { keys: 'Shift+Enter', desc: t('settings.shortcuts.items.newLine') },
  { keys: 'Escape', desc: t('settings.shortcuts.items.stopAgent') },
  { keys: '/', desc: t('settings.shortcuts.items.slashCommands') },
  { keys: 'Ctrl+L', desc: t('settings.shortcuts.items.focusInput') },
  { keys: 'Ctrl+Shift+N', desc: t('settings.shortcuts.items.newConversation') },
  { keys: 'Ctrl+,', desc: t('settings.shortcuts.items.openSettings') },
  { keys: 'Ctrl+`', desc: t('settings.shortcuts.items.toggleTerminal') },
])

// Draw the pending QR onto the canvas. The QR is rendered imperatively (not data-
// bound), and the Channels tab is a v-if block, so the <canvas> is destroyed and
// recreated whenever the user navigates away and back — leaving it blank. This
// helper is the single source of truth for drawing, called both right after login
// and whenever the canvas remounts (see the activeTab watcher below).
async function drawChannelQR() {
  await nextTick()
  if (!qrCanvas.value || !channelQRContent.value) return
  // Resolve colors from the design tokens so the QR follows the active theme —
  // the "dark" modules use the terminal foreground, the "light" modules the
  // surface color. Both read via getComputedStyle (QRCode needs resolved strings).
  const root = document.documentElement
  const fg = getComputedStyle(root).getPropertyValue('--term-fg').trim() || '#18181b'
  const bg = getComputedStyle(root).getPropertyValue('--color-surface').trim() || '#ffffff'
  await QRCode.toCanvas(qrCanvas.value, channelQRContent.value, {
    width: 200,
    margin: 2,
    color: { dark: fg, light: bg },
  })
}

// Re-draw the QR (and resume polling) when the user returns to the Channels tab
// mid-scan — the v-if remount would otherwise show an empty canvas.
watch(activeTab, (tab) => {
  if (tab === 'channels' && channelQRContent.value) {
    drawChannelQR()
    if (channelState.value === 'scanning') pollChannelState()
  }
})

// Flip the persisted default auto-approve preference (store handles the API +
// keeping the unified mode/flag in sync).
async function toggleAutoApprove() {
  await store.setAutoApprove(!store.autoApprove)
}

// Flip the persisted Bluetooth status-channel preference.
async function toggleBLE() {
  if (bleSaving.value) return
  bleSaving.value = true
  const next = !bleEnabled.value
  try {
    await api.setChannelBLE(next)
    bleEnabled.value = next
  } catch (err) {
    console.error('Failed to toggle Bluetooth notifications:', err)
  }
  bleSaving.value = false
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
        channelState.value = ch.state
        channelQRContent.value = ''
        store.channelAvailable = true
        store.channelEnabled = ch.state === 'enabled'
        // Show reminder when first connected via login flow
        if (ch.state === 'enabled' && previousState === 'scanning') {
          channelLoginReminder.value = true
        }
        stopChannelPoll()
      }
    } catch { /* ignore */ }
  }, 2000)
  // Safety cap: stop after 3 min even if the state never resolves.
  channelPollTimeout = setTimeout(stopChannelPoll, 180000)
}

const tabLabel = computed<Record<string, string>>(() => ({
  general: t('settings.tabs.general'),
  appearance: t('settings.tabs.appearance'),
  providers: t('settings.tabs.providers'),
  mcp: t('settings.tabs.mcp'),
  skills: t('settings.tabs.skills'),
  ssh: t('settings.tabs.ssh'),
  channels: t('settings.tabs.channels'),
  shortcuts: t('settings.tabs.shortcuts'),
}))

// Nav-rail + empty-state icons. One heroicons component per section (was a
// v-html SVG-path map; switched to components to drop the v-html injection
// surface and keep icons consistent with the rest of the app).
const iconFor: Record<string, Component> = {
  general: AdjustmentsHorizontalIcon,
  appearance: SwatchIcon,
  providers: CpuChipIcon,
  mcp: ServerStackIcon,
  skills: SparklesIcon,
  ssh: CommandLineIcon,
  channels: BellAlertIcon,
  shortcuts: ComputerDesktopIcon,
}



async function startAddProvider() {
  showAddProvider.value = true
  addProviderStep.value = 'select'
  addSelectedProvider.value = ''
  addSelectedModel.value = ''
  addApiKey.value = ''
  addBaseURL.value = ''
  addError.value = ''
  addLoading.value = true
  try {
    addProviderList.value = await api.setupProviders()
  } catch { /* ignore */ }
  addLoading.value = false
}

async function selectAddProvider(id: string) {
  addSelectedProvider.value = id
  addLoading.value = true
  addError.value = ''
  try {
    addProviderModels.value = await api.setupProviderModels(id)
    addProviderStep.value = 'model'
  } catch {
    addError.value = 'Failed to load models'
  }
  addLoading.value = false
}

function selectAddModel(id: string) {
  addSelectedModel.value = id
  addProviderStep.value = 'apikey'
}

async function submitAddProvider() {
  addLoading.value = true
  addError.value = ''
  try {
    await api.addProvider({
      id: addSelectedProvider.value,
      api_key: addApiKey.value,
      base_url: addBaseURL.value || undefined,
    })
    // Refresh provider list
    configuredProviders.value = await api.listProviders()
    showAddProvider.value = false
    // Also refresh models in the chat store
    store.fetchModels()
  } catch (err: unknown) {
    addError.value = err instanceof Error ? err.message : 'Failed to add provider'
  }
  addLoading.value = false
}

async function deleteProvider(id: string) {
  try {
    await api.deleteProvider(id)
    configuredProviders.value = configuredProviders.value.filter(p => p.id !== id)
    deleteConfirmId.value = ''
    store.fetchModels()
  } catch (err: unknown) {
    console.error('Failed to delete provider:', err)
  }
}

const addProviderInfo = () => addProviderList.value.find(p => p.id === addSelectedProvider.value)
</script>

<template>
  <TransitionRoot :show="open" as="template">
    <Dialog @close="emit('close')" class="relative" style="z-index: var(--z-modal)">
      <!-- Opaque page background: settings is a full page, not a floating modal. -->
      <TransitionChild
        enter="ease-out duration-150"
        enter-from="opacity-0"
        enter-to="opacity-100"
        leave="ease-in duration-100"
        leave-from="opacity-100"
        leave-to="opacity-0">
        <div class="fixed inset-0" style="background: var(--color-background)" />
      </TransitionChild>

      <!-- Edge-to-edge full page. -->
      <div class="fixed inset-0 flex">
        <TransitionChild
          class="w-full h-full"
          enter="ease-out duration-200"
          enter-from="opacity-0 scale-[0.995]"
          enter-to="opacity-100 scale-100"
          leave="ease-in duration-100"
          leave-from="opacity-100 scale-100"
          leave-to="opacity-0 scale-[0.995]">
          <!-- Mirrors the chat page shell: full-height left rail + right column
               with a transparent top bar and an inset surface content panel. -->
          <DialogPanel
            class="settings-shell relative flex w-full h-full overflow-hidden"
            style="background-color: var(--color-background)"
          >
            <!-- Native macOS title-bar drag strip, matching the workspace shell
                 so Settings has the same top inset / draggable region. -->
            <div class="titlebar-drag" data-tauri-drag-region aria-hidden="true" />

            <!-- Left rail (shell tone, like the sidebar): back-to-workspace at
                 the top, then the section nav. -->
            <nav class="settings-rail shrink-0 flex flex-col">
              <div class="flex flex-col gap-0.5">
                <button
                  v-for="tab in (['general', 'appearance', 'providers', 'mcp', 'skills', 'ssh', 'channels', 'shortcuts'] as const)"
                  :key="tab"
                  class="group relative w-full flex items-center gap-2.5 h-8 pl-2.5 pr-2 text-left text-[13px] cursor-pointer transition-colors duration-[var(--duration-fast)] hover:bg-[var(--color-secondary)]"
                  :style="activeTab === tab
                    ? { borderRadius: 'var(--radius-md)', color: 'var(--color-foreground)', backgroundColor: 'var(--color-secondary)', fontWeight: '500' }
                    : { borderRadius: 'var(--radius-md)', color: 'var(--color-muted-foreground)', backgroundColor: 'transparent' }"
                  @click="activeTab = tab"
                >
                  <span v-if="activeTab === tab" class="absolute left-0 top-1/2 -translate-y-1/2 w-[3px] h-4 rounded-full" style="background-color: var(--color-accent-neutral)" />
                  <component :is="iconFor[tab]" class="w-3.5 h-3.5 shrink-0" />
                  <span class="truncate">{{ tabLabel[tab] }}</span>
                </button>
              </div>
              <!-- A second "Back to workspace" pinned to the bottom of the rail:
                   settings is opened from the sidebar's bottom gear, so returning
                   shouldn't require traveling all the way back to the top. -->
              <button
                class="settings-back group mt-auto flex items-center gap-1.5 h-9 px-2.5 rounded-md text-[13px] font-medium transition-colors cursor-pointer"
                @click="emit('close')"
              >
                <ArrowLeftIcon class="w-4 h-4 transition-transform group-hover:-translate-x-0.5" />
                {{ t('settings.backToWorkspace') }}
              </button>
            </nav>

            <!-- Right column: just the inset surface content panel — no header
                 bar. The active section is shown by the rail; close via the
                 rail's "Back to workspace" or Esc. A visually-hidden title keeps
                 the dialog accessible. -->
            <div class="flex flex-col flex-1 min-w-0">
              <DialogTitle class="sr-only">Settings · {{ tabLabel[activeTab] }}</DialogTitle>

              <!-- Inset content panel — matches .chat-panel. Only the inner div
                   scrolls; each tab block is centered and width-capped. -->
              <div class="settings-panel flex flex-col flex-1 min-h-0">
                <div class="flex-1 min-h-0 overflow-y-auto px-8 py-7 [&>div]:max-w-3xl [&>div]:mx-auto">
                <!-- General tab -->
                <div v-if="activeTab === 'general'" class="space-y-5">
                  <!-- Connection status -->
                  <div class="s-row">
                    <div class="s-row-icon">
                      <span
                        class="w-2 h-2 rounded-full"
                        :style="{ backgroundColor: store.wsConnected ? 'var(--color-success)' : 'var(--color-border)' }"
                      />
                    </div>
                    <div class="s-row-body">
                      <div class="s-row-title">{{ t('settings.general.serverState') }}</div>
                      <div class="s-row-sub" :style="{ color: store.wsConnected ? 'var(--color-success)' : 'var(--color-muted-foreground)' }">
                        {{ store.wsConnected ? t('settings.general.serverOnline') : t('settings.general.serverOffline') }}
                      </div>
                    </div>
                  </div>

                  <!-- Token usage -->
                  <div v-if="store.tokenInfo" class="s-row">
                    <div class="s-row-body">
                      <div class="s-row-title">{{ t('settings.general.tokenUsage') }}</div>
                      <div class="flex items-center gap-2 mt-1.5">
                        <div class="flex-1 h-1.5 rounded-full overflow-hidden" style="background-color: var(--color-muted)">
                          <div
                            class="h-full rounded-full transition-all"
                            :style="{ width: store.tokenPercentage + '%', backgroundColor: store.tokenPercentage > 80 ? 'var(--color-destructive)' : store.tokenPercentage > 50 ? 'var(--color-warning-fg)' : 'var(--color-accent-neutral)' }"
                          />
                        </div>
                        <span class="text-[10px] font-mono" style="color: var(--color-muted-foreground)">
                          {{ store.tokenInfo.total_tokens.toLocaleString() }}
                          <span v-if="store.tokenInfo.model_context_limit"> / {{ store.tokenInfo.model_context_limit.toLocaleString() }}</span>
                        </span>
                      </div>
                    </div>
                  </div>

                  <!-- Preferences — configurable toggles with explanations. -->
                  <div>
                    <div class="text-[10px] uppercase tracking-wider font-medium mb-2" style="color: var(--color-muted-foreground)">{{ t('settings.general.preferences') }}</div>

                    <!-- Default auto-approve -->
                    <div class="s-row">
                      <div class="s-row-icon"><ShieldCheckIcon class="w-4 h-4" /></div>
                      <div class="s-row-body">
                        <div class="s-row-title">{{ t('settings.general.autoApproveTitle') }}</div>
                        <div class="s-row-sub">{{ t('settings.general.autoApproveDesc') }}</div>
                      </div>
                      <div class="s-row-actions">
                        <button
                          class="s-switch"
                          :data-on="store.autoApprove ? 'true' : 'false'"
                          :title="store.autoApprove ? t('common.disable') : t('common.enable')"
                          :aria-pressed="store.autoApprove"
                          @click="toggleAutoApprove"
                        />
                      </div>
                    </div>

                    <!-- Bluetooth status notifications (desktop only) -->
                    <div v-if="isTauri" class="s-row">
                      <div class="s-row-icon"><SignalIcon class="w-4 h-4" /></div>
                      <div class="s-row-body">
                        <div class="s-row-title">{{ t('settings.general.bleTitle') }}</div>
                        <div class="s-row-sub">{{ t('settings.general.bleDesc') }}</div>
                      </div>
                      <div class="s-row-actions">
                        <button
                          class="s-switch"
                          :data-on="bleEnabled ? 'true' : 'false'"
                          :disabled="bleSaving"
                          :title="bleEnabled ? t('common.disable') : t('common.enable')"
                          :aria-pressed="bleEnabled"
                          @click="toggleBLE"
                        />
                      </div>
                    </div>

                    <!-- Language -->
                    <div class="s-row">
                      <div class="s-row-icon"><GlobeAltIcon class="w-4 h-4" /></div>
                      <div class="s-row-body">
                        <div class="s-row-title">{{ t('settings.general.languageTitle') }}</div>
                        <div class="s-row-sub">{{ t('settings.general.languageDesc') }}</div>
                      </div>
                      <div class="s-row-actions">
                        <HMenu as="div" class="relative lang-menu" v-slot="{ open }">
                          <MenuButton
                            class="s-btn s-btn-secondary s-btn-sm"
                            :aria-label="t('settings.general.languageTitle')"
                            :aria-expanded="open"
                          >
                            <span class="lang-trigger-label">{{ LOCALE_LABELS[currentLocale] }}</span>
                            <ChevronDownIcon class="w-3 h-3 lang-trigger-caret" :class="{ open }" />
                          </MenuButton>
                          <transition
                            enter-active-class="pop-enter-active"
                            enter-from-class="pop-enter-from"
                            leave-active-class="pop-leave-active"
                            leave-to-class="pop-leave-to"
                          >
                            <MenuItems class="lang-menu-items">
                              <MenuItem v-for="loc in supportedLocales" :key="loc" v-slot="{ active }">
                                <button
                                  class="lang-menu-item"
                                  :class="{ highlight: active, current: loc === currentLocale }"
                                  :aria-current="loc === currentLocale ? 'true' : undefined"
                                  @click="chooseLocale(loc)"
                                >
                                  <span class="lang-item-label">{{ LOCALE_LABELS[loc] }}</span>
                                  <CheckIcon v-if="loc === currentLocale" class="w-3.5 h-3.5 lang-item-check" />
                                </button>
                              </MenuItem>
                            </MenuItems>
                          </transition>
                        </HMenu>
                      </div>
                    </div>
                  </div>
                </div>

                <!-- Appearance tab -->
                <div v-if="activeTab === 'appearance'" class="space-y-5">
                  <h3 class="text-[13px] font-semibold tracking-tight" style="color: var(--color-foreground)">{{ t('settings.appearance.theme') }}</h3>

                  <!-- System (follow OS) -->
                  <button
                    class="w-full flex items-center gap-3 px-3 py-2.5 rounded-md cursor-pointer transition-colors text-left"
                    :style="themeChoice === 'system'
                      ? { border: '1px solid var(--color-accent-neutral)', backgroundColor: 'var(--neutral-wash-soft)' }
                      : { border: '1px solid var(--color-border)', backgroundColor: 'var(--color-surface)' }"
                    @click="setTheme('system')"
                  >
                      <ComputerDesktopIcon class="w-3.5 h-3.5" style="color: var(--color-muted-foreground)" />
                    <div class="flex-1 min-w-0">
                      <div class="text-xs font-medium" style="color: var(--color-foreground)">{{ t('settings.appearance.system') }}</div>
                      <div class="text-[10px]" style="color: var(--color-muted-foreground)">{{ t('settings.appearance.systemDesc') }}</div>
                    </div>
                    <span v-if="themeChoice === 'system'" class="text-[10px] px-1.5 py-0.5 rounded-full shrink-0" style="background-color: var(--neutral-wash); color: var(--color-accent-neutral)">{{ t('settings.appearance.active') }}</span>
                  </button>

                  <!-- Dark themes -->
                  <div>
                    <div class="text-[10px] mb-2 font-medium" style="color: var(--color-muted-foreground)">{{ t('settings.appearance.dark') }}</div>
                    <div class="grid grid-cols-2 gap-2">
                      <button
                        v-for="t in darkThemes"
                        :key="t.id"
                        :data-theme="t.id"
                        class="rounded-md overflow-hidden cursor-pointer text-left transition-transform active:scale-[0.98]"
                        :style="{ border: themeChoice === t.id ? '2px solid var(--color-accent-neutral)' : '1px solid var(--color-border)', backgroundColor: 'var(--color-background)' }"
                        @click="setTheme(t.id)"
                      >
                        <div class="px-2.5 pt-2 pb-1.5">
                          <div class="flex items-center gap-1.5 mb-1.5">
                            <span class="w-2.5 h-2.5 rounded-full" style="background-color: var(--color-primary)" />
                            <span class="w-2.5 h-2.5 rounded-full" style="background-color: var(--color-accent)" />
                            <span class="w-2.5 h-2.5 rounded-full" style="background-color: var(--color-success-fg)" />
                            <span class="w-2.5 h-2.5 rounded-full" style="background-color: var(--color-error-fg)" />
                          </div>
                          <div class="rounded px-1.5 py-1 text-[10px] font-mono truncate" style="background-color: var(--color-surface); color: var(--color-foreground)">
                            <span style="color: var(--color-primary)">&gt;</span> jcode
                          </div>
                        </div>
                        <div class="px-2.5 py-1.5 flex items-center justify-between" style="background-color: var(--color-surface); border-top: 1px solid var(--color-border)">
                          <span class="text-[11px] font-medium truncate" style="color: var(--color-foreground)">{{ t.label }}</span>
                          <span v-if="themeChoice === t.id" class="text-[10px] shrink-0" style="color: var(--color-accent-neutral)">●</span>
                        </div>
                      </button>
                    </div>
                  </div>

                  <!-- Light themes -->
                  <div>
                    <div class="text-[10px] mb-2 font-medium" style="color: var(--color-muted-foreground)">{{ t('settings.appearance.light') }}</div>
                    <div class="grid grid-cols-2 gap-2">
                      <button
                        v-for="t in lightThemes"
                        :key="t.id"
                        :data-theme="t.id"
                        class="rounded-md overflow-hidden cursor-pointer text-left transition-transform active:scale-[0.98]"
                        :style="{ border: themeChoice === t.id ? '2px solid var(--color-accent-neutral)' : '1px solid var(--color-border)', backgroundColor: 'var(--color-background)' }"
                        @click="setTheme(t.id)"
                      >
                        <div class="px-2.5 pt-2 pb-1.5">
                          <div class="flex items-center gap-1.5 mb-1.5">
                            <span class="w-2.5 h-2.5 rounded-full" style="background-color: var(--color-primary)" />
                            <span class="w-2.5 h-2.5 rounded-full" style="background-color: var(--color-accent)" />
                            <span class="w-2.5 h-2.5 rounded-full" style="background-color: var(--color-success-fg)" />
                            <span class="w-2.5 h-2.5 rounded-full" style="background-color: var(--color-error-fg)" />
                          </div>
                          <div class="rounded px-1.5 py-1 text-[10px] font-mono truncate" style="background-color: var(--color-surface); color: var(--color-foreground)">
                            <span style="color: var(--color-primary)">&gt;</span> jcode
                          </div>
                        </div>
                        <div class="px-2.5 py-1.5 flex items-center justify-between" style="background-color: var(--color-surface); border-top: 1px solid var(--color-border)">
                          <span class="text-[11px] font-medium truncate" style="color: var(--color-foreground)">{{ t.label }}</span>
                          <span v-if="themeChoice === t.id" class="text-[10px] shrink-0" style="color: var(--color-accent-neutral)">●</span>
                        </div>
                      </button>
                    </div>
                  </div>

                  <div class="text-[10px] leading-relaxed" style="color: var(--color-muted-foreground)">
                    {{ t('settings.appearance.terminalHint', { cmd: '/theme' }) }}
                  </div>
                </div>

                <!-- Providers tab -->
                <div v-if="activeTab === 'providers'">
                  <div class="flex items-center justify-between mb-4">
                    <div class="flex items-baseline gap-2">
                      <h3 class="text-[13px] font-semibold tracking-tight" style="color: var(--color-foreground)">{{ t('settings.providers.title') }}</h3>
                      <span class="s-section-count">{{ configuredProviders.length }}</span>
                    </div>
                    <button class="s-btn s-btn-secondary s-btn-sm" @click="startAddProvider">
                      {{ t('settings.providers.add') }}
                    </button>
                  </div>

                  <!-- Add provider flow -->
                  <div v-if="showAddProvider" class="s-form mb-4">
                    <div class="s-form-head">
                      <span class="s-form-head-title">
                        {{ addProviderStep === 'select' ? t('settings.providers.selectProvider') : addProviderStep === 'model' ? t('settings.providers.selectModel') : t('settings.providers.enterApiKey') }}
                      </span>
                      <button class="s-btn s-btn-ghost s-btn-xs" @click="showAddProvider = false">✕</button>
                    </div>
                    <div class="s-form-body" style="max-height: 220px; overflow-y: auto; padding: 12px">
                      <!-- Select provider -->
                      <div v-if="addProviderStep === 'select'">
                        <div v-if="addLoading" class="text-center py-4 text-xs animate-pulse" style="color: var(--color-muted-foreground)">{{ t('settings.providers.loadingHint') }}</div>
                        <div v-else class="space-y-1">
                          <button
                            v-for="p in addProviderList.filter(x => !configuredProviders.some(c => c.id === x.id))"
                            :key="p.id"
                            class="w-full px-2.5 py-2 text-left rounded-md text-xs cursor-pointer transition-colors hover:bg-[var(--color-secondary)]"
                            style="color: var(--color-foreground)"
                            @click="selectAddProvider(p.id)"
                          >
                            <span class="font-medium">{{ p.name }}</span>
                            <span class="ml-1.5 font-mono" style="color: var(--color-muted-foreground)">{{ p.id }}</span>
                          </button>
                          <div v-if="addProviderList.filter(x => !configuredProviders.some(c => c.id === x.id)).length === 0" class="text-center py-3 text-[10px]" style="color: var(--color-muted-foreground)">
                            {{ t('settings.providers.allConfigured') }}
                          </div>
                        </div>
                      </div>
                      <!-- Select model -->
                      <div v-if="addProviderStep === 'model'">
                        <div class="flex items-center gap-1 mb-2">
                          <button class="s-btn s-btn-ghost s-btn-xs" @click="addProviderStep = 'select'">‹</button>
                          <span class="text-[10px]" style="color: var(--color-muted-foreground)">{{ addProviderInfo()?.name }}</span>
                        </div>
                        <div v-if="addLoading" class="text-center py-4 text-xs animate-pulse" style="color: var(--color-muted-foreground)">{{ t('settings.providers.loadingHint') }}</div>
                        <div v-else class="space-y-1">
                          <button
                            v-for="m in addProviderModels"
                            :key="m.id"
                            class="w-full px-2.5 py-1.5 text-left rounded-md text-xs cursor-pointer transition-colors hover:bg-[var(--color-secondary)] font-mono"
                            style="color: var(--color-foreground)"
                            @click="selectAddModel(m.id)"
                          >
                            {{ m.id }}
                          </button>
                        </div>
                      </div>
                      <!-- Enter API key -->
                      <div v-if="addProviderStep === 'apikey'" class="space-y-2">
                        <div class="flex items-center gap-1 mb-1">
                          <button class="s-btn s-btn-ghost s-btn-xs" @click="addProviderStep = 'model'">‹</button>
                          <span class="text-[10px] font-mono" style="color: var(--color-muted-foreground)">{{ addSelectedProvider }} / {{ addSelectedModel }}</span>
                        </div>
                        <input v-model="addApiKey" type="password" :placeholder="t('settings.providers.apiKey')" class="s-input mono" @keydown.enter="submitAddProvider" />
                        <input v-model="addBaseURL" type="text" :placeholder="t('settings.providers.baseUrl')" class="s-input mono" @keydown.enter="submitAddProvider" />
                        <div v-if="addError" class="s-error">{{ addError }}</div>
                        <button :disabled="addLoading || !addApiKey" class="s-btn s-btn-primary w-full" @click="submitAddProvider">
                          {{ addLoading ? t('settings.providers.saving') : t('settings.providers.addBtn') }}
                        </button>
                      </div>
                    </div>
                  </div>

                  <!-- Provider list -->
                  <div v-if="configuredProviders.length === 0" class="flex flex-col items-center justify-center text-center py-12 gap-2.5">
                    <div class="w-9 h-9 grid place-items-center rounded-lg" style="background-color: var(--color-secondary); color: var(--color-muted-foreground)">
                      <component :is="iconFor.providers" class="w-4 h-4" />
                    </div>
                    <div class="text-[13px] font-medium" style="color: var(--color-foreground)">{{ t('settings.providers.noneConfigured') }}</div>
                    <div class="text-[11px] leading-relaxed max-w-[240px]" style="color: var(--color-muted-foreground)">
                      {{ t('settings.providers.noneHint', { btn: t('settings.providers.add') }) }}
                    </div>
                  </div>
                  <div v-else>
                    <div v-for="p in configuredProviders" :key="p.id" class="s-row">
                      <div class="s-row-icon"><KeyIcon class="w-3.5 h-3.5" /></div>
                      <div class="s-row-body">
                        <div class="s-row-title" style="font-family: var(--font-mono)">{{ p.id }}</div>
                        <div class="s-row-sub" style="font-family: var(--font-mono)">
                          {{ p.api_key || '—' }}
                          <template v-if="p.base_url"> · {{ p.base_url }}</template>
                        </div>
                      </div>
                      <div class="s-row-actions">
                        <span v-if="store.providerName === p.id" class="s-chip s-chip-accent">{{ t('common.active') }}</span>
                        <button
                          v-if="deleteConfirmId !== p.id"
                          class="s-btn s-btn-ghost s-btn-xs"
                          :title="t('settings.providers.remove')"
                          @click="deleteConfirmId = p.id"
                        >
                          <TrashIcon class="w-3.5 h-3.5" />
                        </button>
                        <template v-else>
                          <button class="s-btn s-btn-danger s-btn-xs" @click="deleteProvider(p.id)">{{ t('common.delete') }}</button>
                          <button class="s-btn s-btn-ghost s-btn-xs" @click="deleteConfirmId = ''">{{ t('common.cancel') }}</button>
                        </template>
                      </div>
                    </div>
                  </div>
                </div>

                <!-- MCP Servers tab -->
                <div v-if="activeTab === 'mcp'">
                  <div class="flex items-center justify-between mb-4">
                    <div class="flex items-baseline gap-2">
                      <h3 class="text-[13px] font-semibold tracking-tight" style="color: var(--color-foreground)">{{ t('settings.mcp.title') }}</h3>
                      <span class="s-section-count">{{ Object.keys(mcpServers).length }}</span>
                    </div>
                    <button
                      v-if="mcpEditing === null"
                      class="s-btn s-btn-secondary s-btn-sm"
                      @click="openAddMCP"
                    >{{ t('settings.mcp.add') }}</button>
                  </div>

                  <!-- Add / Edit form -->
                  <div v-if="mcpEditing !== null" class="s-form mb-4">
                    <div class="s-form-head">
                      <span class="s-form-head-title">{{ mcpEditing ? t('settings.mcp.editServer') : t('settings.mcp.addServer') }}</span>
                      <button class="s-btn s-btn-ghost s-btn-xs" @click="cancelMCPEdit">✕</button>
                    </div>
                    <div class="s-form-body">
                      <div class="s-field">
                        <label class="s-label">{{ t('settings.mcp.serverName') }}</label>
                        <input
                          v-model="mcpForm.name"
                          :disabled="!!mcpEditing"
                          type="text"
                          placeholder="my-server"
                          class="s-input"
                        />
                      </div>

                      <div class="s-field">
                        <label class="s-label">{{ t('settings.mcp.transport') }}</label>
                        <div class="s-seg">
                          <button
                            v-for="transport in (['local', 'http', 'sse'] as const)"
                            :key="transport"
                            class="s-seg-btn"
                            :aria-pressed="mcpForm.transport === transport"
                            @click="mcpForm.transport = transport"
                          >{{ transport === 'local' ? t('settings.mcp.transportLocal') : transport.toUpperCase() }}</button>
                        </div>
                      </div>

                      <!-- HTTP / SSE fields -->
                      <template v-if="mcpForm.transport !== 'local'">
                        <div class="s-field">
                          <label class="s-label">{{ t('settings.mcp.url') }}</label>
                          <input
                            v-model="mcpForm.url"
                            type="text"
                            placeholder="https://api.example.com/mcp"
                            class="s-input mono"
                          />
                        </div>

                        <div class="s-field">
                          <div class="flex items-center justify-between mb-1">
                            <label class="s-label" style="margin: 0">{{ t('settings.mcp.headers') }}</label>
                            <button class="s-btn s-btn-ghost s-btn-xs" @click="addHeaderRow">+ {{ t('settings.mcp.addHeader') }}</button>
                          </div>
                          <div v-for="(h, i) in mcpForm.headers" :key="i" class="s-kv">
                            <input v-model="h.key" type="text" :placeholder="t('settings.mcp.headerKey')" class="s-input mono" />
                            <input v-model="h.value" type="text" :placeholder="t('settings.mcp.headerValue')" class="s-input mono" />
                            <button class="s-kv-rm" @click="removeHeaderRow(i)">✕</button>
                          </div>
                        </div>

                        <div class="s-field">
                          <div class="s-row" style="padding: 8px 12px">
                            <div class="s-row-body">
                              <div class="s-row-title">{{ t('settings.mcp.useOauth') }}</div>
                            </div>
                            <button
                              class="s-switch"
                              :data-on="mcpForm.oauthEnabled ? 'true' : 'false'"
                              :aria-pressed="mcpForm.oauthEnabled"
                              @click="mcpForm.oauthEnabled = !mcpForm.oauthEnabled"
                            />
                          </div>
                        </div>

                        <div v-if="mcpForm.oauthEnabled" class="space-y-3 pl-1 mb-3">
                          <div class="s-field">
                            <label class="s-label">{{ t('settings.mcp.oauthClientId') }}</label>
                            <input
                              v-model="mcpForm.clientId"
                              type="text"
                              placeholder="Optional — leave blank to auto-register"
                              class="s-input mono"
                            />
                          </div>
                          <div class="s-field">
                            <label class="s-label">{{ t('settings.mcp.oauthClientSecret') }}</label>
                            <input
                              v-model="mcpForm.clientSecret"
                              type="password"
                              placeholder="Optional (confidential clients)"
                              class="s-input mono"
                            />
                          </div>
                          <div class="s-field">
                            <label class="s-label">{{ t('settings.mcp.oauthScopes') }}</label>
                            <input
                              v-model="mcpForm.scopesText"
                              type="text"
                              placeholder="space-separated, optional"
                              class="s-input mono"
                            />
                          </div>
                        </div>

                        <div class="s-field">
                          <label class="s-label">{{ t('settings.mcp.timeout') }}</label>
                          <input
                            v-model="mcpForm.timeout"
                            type="number"
                            placeholder="180"
                            class="s-input"
                            style="max-width: 120px"
                          />
                        </div>
                      </template>

                      <!-- Local fields -->
                      <template v-else>
                        <div class="s-field">
                          <label class="s-label">{{ t('settings.mcp.command') }}</label>
                          <input
                            v-model="mcpForm.command"
                            type="text"
                            placeholder="npx"
                            class="s-input mono"
                          />
                        </div>
                        <div class="s-field">
                          <label class="s-label">{{ t('settings.mcp.arguments') }}</label>
                          <input
                            v-model="mcpForm.argsText"
                            type="text"
                            placeholder="-y @some/mcp-server"
                            class="s-input mono"
                          />
                        </div>
                      </template>

                      <div v-if="mcpFormError" class="s-error">{{ mcpFormError }}</div>
                    </div>
                    <div class="s-form-foot">
                      <button class="s-btn s-btn-secondary" @click="cancelMCPEdit">{{ t('common.cancel') }}</button>
                      <button
                        class="s-btn s-btn-primary"
                        :disabled="mcpSaving"
                        @click="saveMCP"
                      >{{ mcpSaving ? t('settings.providers.saving') : t('settings.mcp.save') }}</button>
                    </div>
                  </div>

                  <div v-if="mcpLoading" class="text-center text-xs py-6 animate-pulse" style="color: var(--color-muted-foreground)">
                    {{ t('settings.providers.loadingHint') }}
                  </div>
                  <div v-else-if="mcpEditing === null && Object.keys(mcpServers).length === 0" class="flex flex-col items-center justify-center text-center py-12 gap-2.5">
                    <div class="w-9 h-9 grid place-items-center rounded-lg" style="background-color: var(--color-secondary); color: var(--color-muted-foreground)">
                      <component :is="iconFor.mcp" class="w-4 h-4" />
                    </div>
                    <div class="text-[13px] font-medium" style="color: var(--color-foreground)">{{ t('settings.mcp.noneConfigured') }}</div>
                    <div class="text-[11px] leading-relaxed max-w-[240px]" style="color: var(--color-muted-foreground)">{{ t('settings.mcp.noneHint', { btn: t('settings.mcp.add') }) }}</div>
                  </div>
                  <div v-else-if="mcpEditing === null">
                    <div
                      v-for="(info, name) in mcpServers"
                      :key="name"
                      class="s-row items-start"
                      :data-muted="!info.enabled ? 'true' : 'false'"
                      style="flex-direction: column; align-items: stretch"
                    >
                      <div class="flex items-center gap-3 w-full">
                        <div class="s-row-icon"><component :is="serverIcon(info.type)" class="w-3.5 h-3.5" /></div>
                        <div class="s-row-body">
                          <div class="s-row-title" style="display: flex; align-items: center; gap: 8px; flex-wrap: wrap">
                            {{ name }}
                            <span class="s-chip">{{ info.type || 'stdio' }}</span>
                            <span class="text-[10px]" :style="{ color: mcpStatusColor(info) }">● {{ mcpStatusLabel(info) }}</span>
                          </div>
                          <div class="s-row-sub" style="font-family: var(--font-mono)">
                            {{ info.type === 'sse' || info.type === 'http' ? info.url : info.command }}
                          </div>
                        </div>
                        <div class="s-row-actions">
                          <button class="s-btn s-btn-ghost s-btn-xs" :title="t('common.edit')" @click="openEditMCP(info)">{{ t('common.edit') }}</button>
                          <button class="s-btn s-btn-ghost s-btn-xs" style="color: var(--color-destructive)" :title="t('common.delete')" @click="deleteMCP(String(name))">{{ t('common.delete') }}</button>
                          <button
                            class="s-switch"
                            :data-on="info.enabled ? 'true' : 'false'"
                            :title="info.enabled ? t('common.disable') : t('common.enable')"
                            :aria-pressed="info.enabled"
                            @click="toggleMCP(String(name), !info.enabled)"
                          />
                        </div>
                      </div>
                      <!-- OAuth login row -->
                      <div v-if="(info.type === 'http' || info.type === 'sse') && (info.oauth || info.status === 'needs_auth')" class="mt-2 flex items-center gap-2 pl-10">
                        <button
                          class="s-btn s-btn-secondary s-btn-xs"
                          :disabled="mcpLoginBusy === name"
                          @click="loginMCP(String(name))"
                        >{{ mcpLoginBusy === name ? t('settings.mcp.waitingBrowser') : (info.has_auth ? t('settings.mcp.reauth') : t('settings.mcp.login')) }}</button>
                        <span v-if="info.has_auth" class="s-chip s-chip-success">{{ t('settings.mcp.authenticated') }}</span>
                      </div>
                      <div v-if="mcpLoginBusy === name && mcpLoginMessage" class="mt-1 text-[10px] pl-10" style="color: var(--color-muted-foreground)">{{ mcpLoginMessage }}</div>
                      <div v-else-if="mcpLoginMessageFor === name && mcpLoginMessage && !mcpLoginBusy" class="mt-1 text-[10px] pl-10" style="color: var(--color-warning-fg)">{{ mcpLoginMessage }}</div>
                      <div v-if="info.error" class="mt-1 text-[10px] font-mono pl-10" style="color: var(--color-error-fg)">{{ info.error }}</div>
                    </div>
                  </div>
                </div>

                <!-- Skills tab -->
                <div v-if="activeTab === 'skills'">
                  <div class="flex items-baseline gap-2 mb-4">
                    <h3 class="text-[13px] font-semibold tracking-tight" style="color: var(--color-foreground)">{{ t('settings.skills.title') }}</h3>
                    <span class="s-section-count">{{ skills.length }}</span>
                  </div>
                  <div class="flex items-center gap-2 mb-3">
                    <div class="s-seg">
                      <button
                        v-for="f in (['all', 'local', 'builtin'] as const)"
                        :key="f"
                        class="s-seg-btn"
                        :aria-pressed="skillFilter === f"
                        @click="skillFilter = f"
                      >{{ f === 'all' ? t('settings.skills.filterAll') : f === 'local' ? t('settings.skills.filterLocal') : t('settings.skills.filterBuiltin') }}</button>
                    </div>
                    <input
                      v-model="skillSearch"
                      type="text"
                      :placeholder="t('settings.skills.search')"
                      class="s-input"
                      style="flex: 1"
                    />
                  </div>

                  <div v-if="skillsLoading" class="text-center text-xs py-6 animate-pulse" style="color: var(--color-muted-foreground)">{{ t('settings.skills.loadingHint') }}</div>
                  <div v-else-if="filteredSkills.length === 0" class="flex flex-col items-center justify-center text-center py-12 gap-2.5">
                    <div class="w-9 h-9 grid place-items-center rounded-lg" style="background-color: var(--color-secondary); color: var(--color-muted-foreground)">
                      <component :is="iconFor.skills" class="w-4 h-4" />
                    </div>
                    <div class="text-[13px] font-medium" style="color: var(--color-foreground)">{{ t('settings.skills.none') }}</div>
                    <div class="text-[11px] leading-relaxed max-w-[240px]" style="color: var(--color-muted-foreground)">{{ t('settings.skills.noneHint') }}</div>
                  </div>
                  <div v-else>
                    <div
                      v-for="sk in filteredSkills"
                      :key="sk.name"
                      class="s-row"
                      :data-muted="!sk.enabled ? 'true' : 'false'"
                    >
                      <div class="s-row-body">
                        <div class="s-row-title" style="display: flex; align-items: center; gap: 8px">
                          {{ sk.name }}
                          <span v-if="sk.builtin" class="s-chip">{{ t('settings.skills.builtin') }}</span>
                        </div>
                        <div v-if="sk.description" class="s-row-sub">{{ sk.description }}</div>
                      </div>
                      <div class="s-row-actions">
                        <button
                          class="s-switch"
                          :data-on="sk.enabled ? 'true' : 'false'"
                          @click="toggleSkill(sk.name, !sk.enabled)"
                          :title="sk.enabled ? t('common.disable') : t('common.enable')"
                          :aria-pressed="sk.enabled"
                        />
                      </div>
                    </div>
                  </div>
                </div>

                <!-- SSH tab -->
                <div v-if="activeTab === 'ssh'">
                  <div class="flex items-center justify-between mb-4">
                    <h3 class="text-[13px] font-semibold tracking-tight" style="color: var(--color-foreground)">{{ t('settings.ssh.title') }}</h3>
                    <button class="s-btn s-btn-secondary s-btn-sm" @click="openRemoteWizard">
                      <PlusIcon class="w-3.5 h-3.5" /> {{ t('settings.ssh.connect') }}
                    </button>
                  </div>

                  <div class="mb-3">
                    <div class="text-[11px] font-medium mb-1" style="color: var(--color-muted-foreground)">{{ t('settings.ssh.currentEnv') }}</div>
                    <span class="s-chip s-chip-accent">
                      <span class="w-1.5 h-1.5 rounded-full" style="background-color: var(--color-accent-neutral)" />
                      {{ sshCurrent }}
                    </span>
                  </div>

                  <div v-if="sshAliases.length === 0" class="flex flex-col items-center justify-center text-center py-12 gap-2.5">
                    <div class="w-9 h-9 grid place-items-center rounded-lg" style="background-color: var(--color-secondary); color: var(--color-muted-foreground)">
                      <component :is="iconFor.ssh" class="w-4 h-4" />
                    </div>
                    <div class="text-[13px] font-medium" style="color: var(--color-foreground)">{{ t('settings.ssh.noneConfigured') }}</div>
                    <div class="text-[11px] leading-relaxed max-w-[270px]" style="color: var(--color-muted-foreground)">
                      {{ t('settings.ssh.noneHint', { btn: t('settings.ssh.connect'), config: '~/.jcode/config.json' }) }}
                    </div>
                  </div>
                  <div v-else>
                    <button
                      v-for="alias in sshAliases"
                      :key="alias.name"
                      class="group s-row w-full text-left cursor-pointer transition-colors hover:bg-[var(--color-secondary)]"
                      :title="t('settings.ssh.connectTo', { name: alias.name })"
                      @click="connectToAlias(alias)"
                    >
                      <div class="s-row-icon"><ServerIcon class="w-3.5 h-3.5" /></div>
                      <div class="s-row-body">
                        <div class="s-row-title">{{ alias.name }}</div>
                        <div class="s-row-sub" style="font-family: var(--font-mono)">
                          {{ alias.addr }}
                          <template v-if="alias.path"> · {{ alias.path }}</template>
                        </div>
                      </div>
                      <div class="s-row-actions">
                        <span v-if="sshCurrent === alias.name" class="s-chip s-chip-accent">{{ t('common.active') }}</span>
                        <ChevronRightIcon class="w-3.5 h-3.5 opacity-0 group-hover:opacity-100 transition-opacity" style="color: var(--color-muted-foreground)" />
                      </div>
                    </button>
                  </div>
                </div>

                <!-- Channels tab -->
                <div v-if="activeTab === 'channels'">
                  <h3 class="text-[13px] font-semibold tracking-tight mb-4" style="color: var(--color-foreground)">{{ t('settings.channels.title') }}</h3>

                  <div v-if="!channelAvailable" class="flex flex-col items-center justify-center text-center py-12 gap-2.5">
                    <div class="w-9 h-9 grid place-items-center rounded-lg" style="background-color: var(--color-secondary); color: var(--color-muted-foreground)">
                      <component :is="iconFor.channels" class="w-4 h-4" />
                    </div>
                    <div class="text-[13px] font-medium" style="color: var(--color-foreground)">{{ t('settings.channels.noneConfigured') }}</div>
                    <div class="text-[11px] leading-relaxed max-w-[260px]" style="color: var(--color-muted-foreground)">
                      {{ t('settings.channels.noneHint', { flag: 'channel.web_enabled: true', config: '~/.jcode/config.json' }) }}
                    </div>
                  </div>

                  <div v-else class="space-y-4">
                    <div class="s-row items-start" style="flex-direction: column; align-items: stretch; padding: 14px 16px">
                      <div class="flex items-center justify-between mb-3 w-full">
                        <div class="flex items-center gap-3">
                          <div class="s-row-icon"><ChatBubbleLeftIcon class="w-4 h-4" /></div>
                          <div class="s-row-body">
                            <div class="s-row-title">{{ t('settings.channels.wechat') }}</div>
                            <div class="s-row-sub">{{ t('settings.channels.integration') }}</div>
                          </div>
                        </div>
                        <span
                          class="s-chip"
                          :class="{
                            's-chip-accent': channelState === 'enabled',
                            's-chip-warning': channelState === 'disabled' || channelState === 'scanning',
                          }"
                        >
                          <span
                            class="w-1.5 h-1.5 rounded-full"
                            :style="{ backgroundColor: 'currentColor' }"
                          />
                          {{ channelState === 'enabled' ? t('common.connected') : channelState === 'disabled' ? t('common.disconnected') : channelState === 'scanning' ? t('common.scanning') : t('common.notConfigured') }}
                        </span>
                      </div>

                      <div v-if="channelQRContent" class="flex flex-col items-center py-3">
                        <canvas ref="qrCanvas" class="rounded-md" style="border: 1px solid var(--color-border)" />
                        <div class="text-[10px] mt-2" style="color: var(--color-muted-foreground)">{{ t('settings.channels.scanQr') }}</div>
                      </div>

                      <div class="flex gap-2 mt-2">
                        <button
                          v-if="channelState === 'none'"
                          :disabled="channelLoading"
                          class="s-btn s-btn-primary flex-1"
                          @click="channelLogin"
                        >
                          {{ channelLoading ? t('settings.channels.loadingHint') : t('settings.channels.connect') }}
                        </button>
                        <button
                          v-if="channelState === 'enabled' || channelState === 'disabled'"
                          :disabled="channelLoading"
                          class="s-btn s-btn-ghost flex-1"
                          style="color: var(--color-destructive)"
                          @click="channelLogout"
                        >
                          {{ t('settings.channels.disconnect') }}
                        </button>
                      </div>
                    </div>

                    <!-- Login reminder banner -->
                    <div
                      v-if="channelLoginReminder"
                      class="px-4 py-3 rounded-md flex items-start gap-2.5"
                      style="border: 1px solid var(--color-warning-fg); background-color: var(--color-warning-bg)"
                    >
                      <span class="text-sm shrink-0 mt-0.5">⚠️</span>
                      <div class="flex-1 min-w-0">
                        <div class="text-xs font-medium" style="color: var(--color-warning-fg)">{{ t('settings.channels.activate') }}</div>
                        <div class="text-[10px] mt-0.5 leading-relaxed" style="color: var(--color.warning-fg); opacity: 0.8">
                          {{ t('settings.channels.activateBody') }}
                        </div>
                      </div>
                      <button
                        class="s-btn s-btn-ghost s-btn-xs shrink-0"
                        style="color: var(--color-warning-fg)"
                        @click="channelLoginReminder = false"
                      >✕</button>
                    </div>

                    <div class="text-[10px] leading-relaxed" style="color: var(--color-muted-foreground)">
                      {{ t('settings.channels.whenConnected') }}
                    </div>
                  </div>
                </div>

                <!-- Shortcuts tab -->
                <div v-if="activeTab === 'shortcuts'">
                  <h3 class="text-[13px] font-semibold tracking-tight mb-4" style="color: var(--color-foreground)">{{ t('settings.shortcuts.title') }}</h3>
                  <div>
                    <div
                      v-for="s in shortcuts"
                      :key="s.keys"
                      class="s-row"
                      style="padding: 8px 12px"
                    >
                      <div class="s-row-body">
                        <div class="s-row-title">{{ s.desc }}</div>
                      </div>
                      <kbd class="px-2 py-0.5 text-[10px] font-mono rounded" style="background-color: var(--color-secondary); border: 1px solid var(--color-border); color: var(--color-muted-foreground)">{{ s.keys }}</kbd>
                    </div>
                  </div>
                </div>
                </div>
              </div>
            </div>
          </DialogPanel>
        </TransitionChild>
      </div>
    </Dialog>
  </TransitionRoot>
</template>

<style scoped>
/* Left rail mirrors the chat sidebar: same width + shell tone, no border. */
.settings-rail {
  width: var(--sidebar-width);
  padding: 12px;
  overflow-y: auto;
  background: var(--color-background);
}

.settings-back {
  color: var(--color-muted-foreground);
}
.settings-back:hover {
  background: var(--color-secondary);
  color: var(--color-foreground);
}

/* Inset content panel — identical treatment to App.vue's .chat-panel so the two
   pages read as the same layout. */
.settings-panel {
  background: var(--color-surface);
  border: 1px solid var(--color-border);
  border-radius: var(--radius-2xl);
  margin: 4px 14px 14px;
  overflow: hidden;
  box-shadow: var(--shadow-sm);
}

@media (max-width: 640px) {
  .settings-rail {
    width: 100%;
  }
  .settings-panel {
    margin: 2px 8px 8px;
  }
}

/* Language picker (General → Preferences). Mirrors the headlessui Menu pattern
   used by TopBar's panel switcher, but sized to fit inside a preference row. */
.lang-menu {
  position: relative;
  flex-shrink: 0;
}
.lang-trigger {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  height: 28px;
  padding: 0 9px;
  border-radius: var(--radius-md);
  border: 1px solid var(--color-border);
  background-color: var(--color-muted);
  color: var(--color-foreground);
  font-size: 11px;
  font-weight: 500;
  cursor: pointer;
  transition: background-color 0.12s ease, border-color 0.12s ease;
}
.lang-trigger:hover {
  background-color: var(--color-surface);
  border-color: var(--color-accent-neutral);
}
.lang-trigger-label {
  white-space: nowrap;
}
.lang-trigger-caret {
  color: var(--color-muted-foreground);
  transition: transform 0.12s ease;
}
.lang-trigger-caret.open {
  transform: rotate(180deg);
}
.lang-menu-items {
  position: absolute;
  top: calc(100% + 6px);
  right: 0;
  min-width: 160px;
  padding: 4px;
  background: var(--color-surface);
  border: 1px solid var(--color-border);
  border-radius: var(--radius-lg);
  box-shadow: var(--shadow-md);
  z-index: var(--z-dropdown);
  outline: none;
}
.lang-menu-item {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  width: 100%;
  padding: 7px 9px;
  border: none;
  background: transparent;
  border-radius: var(--radius-md);
  color: var(--color-foreground);
  font-size: 12px;
  text-align: left;
  cursor: pointer;
}
.lang-menu-item.highlight {
  background: var(--color-muted);
}
.lang-menu-item.current {
  color: var(--color-accent-neutral);
  font-weight: 600;
}
.lang-item-check {
  color: var(--color-accent-neutral);
  flex-shrink: 0;
}
.pop-enter-active,
.pop-leave-active {
  transition: opacity 0.12s ease, transform 0.12s ease;
}
.pop-enter-from,
.pop-leave-to {
  opacity: 0;
  transform: translateY(-4px);
}

/* ============================================================
   Unified design atoms — collapses 20+ inline styles into 8
   groups so every Settings tab reads as one product. Every value
   references a design token; no hardcoded colors.
   ============================================================ */

/* Input — replaces ~20 inline <input> styles across tabs. */
.s-input {
  width: 100%;
  height: 32px;
  padding: 0 10px;
  background: var(--color-surface);
  border: 1px solid var(--color-border);
  border-radius: var(--radius-md);
  color: var(--color-foreground);
  font-size: 12px;
  font-family: var(--font-sans);
  outline: none;
  transition: border-color var(--duration-fast), box-shadow var(--duration-fast);
}
.s-input::placeholder {
  color: var(--color-muted-foreground);
  opacity: 0.7;
}
.s-input:focus {
  border-color: var(--color-primary);
  box-shadow: 0 0 0 3px var(--accent-wash-soft);
}
.s-input:disabled {
  opacity: 0.55;
  cursor: not-allowed;
}
.s-input.mono {
  font-family: var(--font-mono);
}

/* Buttons — 4 variants × 3 sizes. primary = the one main action per row. */
.s-btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 6px;
  height: 32px;
  padding: 0 12px;
  border-radius: var(--radius-md);
  font-size: 12px;
  font-weight: 500;
  font-family: var(--font-sans);
  cursor: pointer;
  border: 1px solid transparent;
  white-space: nowrap;
  transition: background var(--duration-fast), border-color var(--duration-fast), opacity var(--duration-fast);
}
.s-btn:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}
.s-btn-primary {
  background: var(--color-primary);
  color: var(--color-on-primary);
}
.s-btn-primary:hover:not(:disabled) {
  background: color-mix(in srgb, var(--color-primary) 88%, #000);
}
.s-btn-secondary {
  background: var(--color-surface);
  border-color: var(--color-border);
  color: var(--color-foreground);
}
.s-btn-secondary:hover:not(:disabled) {
  background: var(--color-secondary);
}
.s-btn-ghost {
  background: transparent;
  color: var(--color-foreground);
}
.s-btn-ghost:hover:not(:disabled) {
  background: var(--color-secondary);
}
.s-btn-danger {
  background: var(--color-destructive);
  color: var(--color-on-destructive);
}
.s-btn-danger:hover:not(:disabled) {
  filter: brightness(0.92);
}
.s-btn-sm {
  height: 26px;
  padding: 0 9px;
  font-size: 11px;
  border-radius: var(--radius-sm);
}
.s-btn-xs {
  height: 22px;
  padding: 0 7px;
  font-size: 10px;
  border-radius: var(--radius-sm);
}

/* Switch — single boolean toggle. Replaces the per-call inline toggles
 * (autoApprove / BLE / MCP / Skill) AND the raw <input checkbox> in the
 * MCP OAuth field, so a screen never shows two switch shapes. */
.s-switch {
  position: relative;
  display: inline-block;
  width: 34px;
  height: 20px;
  flex-shrink: 0;
  background: var(--color-border);
  border-radius: var(--radius-pill);
  border: none;
  cursor: pointer;
  padding: 0;
  transition: background var(--duration-fast);
}
/* Kept on --color-accent-neutral (not --color-primary) to match the
 * existing de-emphasis policy: primary orange is reserved for the hero +
 * send button only. */
.s-switch[data-on='true'] {
  background: var(--color-accent-neutral);
}
.s-switch::after {
  content: '';
  position: absolute;
  top: 2px;
  left: 2px;
  width: 16px;
  height: 16px;
  border-radius: 50%;
  background: var(--color-surface);
  box-shadow: var(--shadow-sm);
  transition: transform var(--duration-fast) var(--ease-out);
}
.s-switch[data-on='true']::after {
  transform: translateX(14px);
}
.s-switch:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

/* Row — unified list row for prefs / provider / mcp / skill / ssh /
 * shortcuts. Collapses 5 structurally-similar but differently-written
 * row containers into one. */
.s-row {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 10px 14px;
  background: var(--color-surface);
  border: 1px solid var(--color-border);
  border-radius: var(--radius-lg);
}
.s-row + .s-row {
  margin-top: 8px;
}
.s-row[data-muted='true'] {
  background: var(--color-muted);
  opacity: 0.75;
}
.s-row-icon {
  width: 28px;
  height: 28px;
  display: grid;
  place-items: center;
  background: transparent;
  border-radius: var(--radius-md);
  color: var(--color-muted-foreground);
  flex-shrink: 0;
}
.s-row-body {
  flex: 1;
  min-width: 0;
}
.s-row-title {
  font-size: 12px;
  font-weight: 500;
  color: var(--color-foreground);
}
.s-row-sub {
  font-size: 11px;
  color: var(--color-muted-foreground);
  margin-top: 1px;
  line-height: 1.4;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.s-row-actions {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-shrink: 0;
}

/* Chips / badges — status + active markers. */
.s-chip {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  height: 18px;
  padding: 0 7px;
  border-radius: var(--radius-pill);
  font-size: 10px;
  font-weight: 500;
  background: var(--color-muted);
  color: var(--color-muted-foreground);
  white-space: nowrap;
}
.s-chip-accent {
  background: var(--neutral-wash);
  color: var(--color-accent-neutral);
}
.s-chip-success {
  background: var(--color-success-bg);
  color: var(--color-success-fg);
}
.s-chip-warning {
  background: var(--color-warning-bg);
  color: var(--color-warning-fg);
}
.s-chip-error {
  background: var(--color-error-bg);
  color: var(--color-error-fg);
}

/* Segmented control — transport picker + skill filter. */
.s-seg {
  display: inline-flex;
  background: var(--color-muted);
  border-radius: var(--radius-md);
  padding: 2px;
  gap: 2px;
}
.s-seg-btn {
  height: 24px;
  padding: 0 10px;
  border: none;
  background: transparent;
  border-radius: var(--radius-sm);
  font-size: 11px;
  font-weight: 500;
  color: var(--color-muted-foreground);
  cursor: pointer;
  transition: background var(--duration-fast), color var(--duration-fast);
}
.s-seg-btn[aria-pressed='true'] {
  background: var(--color-surface);
  color: var(--color-foreground);
  box-shadow: var(--shadow-sm);
}

/* Section count — small monospace number beside a section title. */
.s-section-count {
  font-size: 11px;
  font-family: var(--font-mono);
  color: var(--color-muted-foreground);
}

/* Field group: label → control → helper/error. Gives MCP / Provider
 * forms a consistent vertical rhythm instead of ad-hoc label markup. */
.s-field {
  margin-bottom: 14px;
}
.s-field:last-child {
  margin-bottom: 0;
}
.s-label {
  display: block;
  font-size: 11px;
  font-weight: 500;
  color: var(--color-foreground);
  margin-bottom: 5px;
}
.s-helper {
  font-size: 11px;
  line-height: 1.45;
  color: var(--color-muted-foreground);
  margin-top: 5px;
}
.s-error {
  font-size: 11px;
  line-height: 1.45;
  color: var(--color-destructive);
  margin-top: 5px;
}

/* Form card — MCP add/edit + Provider add. */
.s-form {
  background: var(--color-surface);
  border: 1px solid var(--color-border);
  border-radius: var(--radius-lg);
  overflow: hidden;
}
.s-form-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 10px 14px;
  background: var(--color-muted);
  border-bottom: 1px solid var(--color-border);
}
.s-form-head-title {
  font-size: 11px;
  font-weight: 600;
  color: var(--color-muted-foreground);
  text-transform: uppercase;
  letter-spacing: 0.06em;
}
.s-form-body {
  padding: 16px;
}
.s-form-foot {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
  padding: 12px 14px;
  background: var(--color-muted);
  border-top: 1px solid var(--color-border);
}

/* Key-value rows — MCP headers. */
.s-kv {
  display: flex;
  gap: 8px;
  margin-bottom: 8px;
}
.s-kv .s-input {
  height: 28px;
  font-size: 11px;
}
.s-kv-rm {
  width: 28px;
  height: 28px;
  flex-shrink: 0;
  display: grid;
  place-items: center;
  background: transparent;
  border: 1px solid var(--color-border);
  border-radius: var(--radius-sm);
  cursor: pointer;
  color: var(--color-muted-foreground);
  transition: background var(--duration-fast), color var(--duration-fast);
}
.s-kv-rm:hover {
  background: var(--color-secondary);
  color: var(--color-foreground);
}
</style>
