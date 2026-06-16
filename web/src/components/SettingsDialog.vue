<script setup lang="ts">
import { ref, reactive, computed, watch, nextTick, onUnmounted } from 'vue'
import { useChatStore } from '@/stores/chat'
import { useTheme } from '@/composables/useTheme'
import { api } from '@/composables/api'
import type { MCPServerInfo, MCPServerRequest, SkillInfo, SSHAlias, SetupProvider, SetupModel, ProviderDetail } from '@/types/api'
import QRCode from 'qrcode'
import {
  Dialog,
  DialogPanel,
  DialogTitle,
  TransitionRoot,
  TransitionChild,
} from '@headlessui/vue'

const props = defineProps<{
  open: boolean
}>()

const emit = defineEmits<{
  close: []
}>()

const store = useChatStore()
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
  }
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
  return type === 'sse' || type === 'http' ? '🌐' : '⚡'
}

function mcpStatusLabel(info: MCPServerInfo): string {
  if (!info.enabled) return 'Disabled'
  switch (info.status) {
    case 'connected': return 'Connected'
    case 'needs_auth': return 'Login required'
    case 'error': return 'Error'
    default: return 'Configured'
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
let loginPollTimer: ReturnType<typeof setInterval> | null = null

function stopLoginPoll() {
  if (loginPollTimer) { clearInterval(loginPollTimer); loginPollTimer = null }
}

async function loginMCP(name: string) {
  mcpLoginBusy.value = name
  mcpLoginMessage.value = 'Opening browser — complete authorization, then return here…'
  try {
    await api.mcpLogin(name)
  } catch (err) {
    mcpLoginMessage.value = err instanceof Error ? err.message : 'Login failed'
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
        await loadMCP()
      } else if (st.status === 'error') {
        stopLoginPoll()
        mcpLoginBusy.value = ''
        mcpLoginMessage.value = st.message || 'Login failed'
      } else if (st.status === 'needs_client_id') {
        stopLoginPoll()
        mcpLoginBusy.value = ''
        mcpLoginMessage.value = 'This server does not support automatic registration. Edit it and set an OAuth Client ID, then log in again.'
      }
    } catch { /* keep polling */ }
  }, 1500)
}

onUnmounted(stopLoginPoll)

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

const shortcuts = [
  { keys: 'Enter', desc: 'Send message' },
  { keys: 'Shift+Enter', desc: 'New line' },
  { keys: 'Escape', desc: 'Stop agent' },
  { keys: '/', desc: 'Slash commands' },
  { keys: 'Ctrl+L', desc: 'Focus input' },
  { keys: 'Ctrl+Shift+N', desc: 'New conversation' },
  { keys: 'Ctrl+,', desc: 'Open settings' },
  { keys: 'Ctrl+`', desc: 'Toggle terminal' },
]

async function channelLogin() {
  channelLoading.value = true
  try {
    const result = await api.channelLogin()
    channelQRContent.value = result.qr_content
    channelState.value = 'scanning'
    await nextTick()
    if (qrCanvas.value && channelQRContent.value) {
      const isDark = document.documentElement.classList.contains('dark')
      await QRCode.toCanvas(qrCanvas.value, channelQRContent.value, {
        width: 200,
        margin: 2,
        color: {
          dark: isDark ? '#e4e4e7' : '#18181b',
          light: isDark ? '#27272a' : '#ffffff',
        },
      })
    }
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

function pollChannelState() {
  const previousState = channelState.value
  const interval = setInterval(async () => {
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
        clearInterval(interval)
      }
    } catch { /* ignore */ }
  }, 2000)
  setTimeout(() => clearInterval(interval), 180000)
}

const tabLabel: Record<string, string> = {
  general: 'General',
  appearance: 'Appearance',
  providers: 'Providers',
  mcp: 'MCP Servers',
  skills: 'Skills',
  ssh: 'SSH',
  channels: 'Channels',
  shortcuts: 'Shortcuts',
}

// Monochrome 20×20 stroke icons (inner SVG only) for the nav rail + empty
// states. Rendered via v-html into an <svg stroke="currentColor" fill="none">
// so they inherit the element's color/weight.
const iconFor: Record<string, string> = {
  general: '<line x1="3" y1="6.5" x2="17" y2="6.5" stroke-linecap="round"/><line x1="3" y1="13.5" x2="17" y2="13.5" stroke-linecap="round"/><circle cx="13" cy="6.5" r="2.1"/><circle cx="7" cy="13.5" r="2.1"/>',
  appearance: '<circle cx="10" cy="10" r="3.2"/><path d="M10 2.5v2M10 15.5v2M2.5 10h2M15.5 10h2M4.9 4.9l1.4 1.4M13.7 13.7l1.4 1.4M15.1 4.9l-1.4 1.4M6.3 13.7l-1.4 1.4" stroke-linecap="round"/>',
  providers: '<rect x="6" y="6" width="8" height="8" rx="1.5"/><path d="M8.5 3v3M11.5 3v3M8.5 14v3M11.5 14v3M3 8.5h3M3 11.5h3M14 8.5h3M14 11.5h3" stroke-linecap="round"/>',
  mcp: '<rect x="3.5" y="4" width="13" height="5" rx="1.5"/><rect x="3.5" y="11" width="13" height="5" rx="1.5"/><path d="M6 6.5h0M6 13.5h0" stroke-linecap="round" stroke-width="2"/>',
  skills: '<path d="M10 3l1.6 4.4L16 9l-4.4 1.6L10 15l-1.6-4.4L4 9l4.4-1.6z" stroke-linejoin="round"/>',
  ssh: '<rect x="3" y="4.5" width="14" height="11" rx="1.8"/><path d="M6.5 8.5l2 2-2 2M10.8 12.5h3" stroke-linecap="round" stroke-linejoin="round"/>',
  channels: '<path d="M7 8.5a3 3 0 016 0c0 2.6 1.2 3.6 1.8 4.2H5.2C5.8 12.1 7 11.1 7 8.5z" stroke-linejoin="round"/><path d="M8.6 15a1.6 1.6 0 002.8 0" stroke-linecap="round"/>',
  shortcuts: '<rect x="3" y="6" width="14" height="8" rx="1.8"/><path d="M6 9.2h0M9 9.2h0M12 9.2h0M6 11.6h6" stroke-linecap="round" stroke-width="2"/>',
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
            class="flex w-full h-full overflow-hidden"
            style="background-color: var(--color-background)"
          >
            <!-- Left rail (shell tone, like the sidebar): back-to-workspace at
                 the top, then the section nav. -->
            <nav class="settings-rail shrink-0 flex flex-col">
              <button
                class="settings-back group flex items-center gap-1.5 h-9 px-2.5 mb-1.5 rounded-md text-[13px] font-medium transition-colors cursor-pointer"
                @click="emit('close')"
              >
                <svg class="w-4 h-4 transition-transform group-hover:-translate-x-0.5" viewBox="0 0 20 20" fill="none" stroke="currentColor" stroke-width="1.8">
                  <path d="M11.5 5L6.5 10l5 5M6.5 10H16" stroke-linecap="round" stroke-linejoin="round" />
                </svg>
                Back to workspace
              </button>
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
                  <span v-if="activeTab === tab" class="absolute left-0 top-1/2 -translate-y-1/2 w-[3px] h-4 rounded-full" style="background-color: var(--color-primary)" />
                  <svg class="w-3.5 h-3.5 shrink-0" viewBox="0 0 20 20" fill="none" stroke="currentColor" stroke-width="1.6" v-html="iconFor[tab]" />
                  <span class="truncate">{{ tabLabel[tab] }}</span>
                </button>
              </div>
            </nav>

            <!-- Right column: top bar (shell tone) + inset surface content panel. -->
            <div class="flex flex-col flex-1 min-w-0">
              <div class="flex items-center gap-2.5 h-[52px] px-4 shrink-0">
                <span class="w-[5px] h-[5px] rounded-[1px]" style="background-color: var(--color-primary)" />
                <div class="flex flex-col min-w-0 leading-tight">
                  <DialogTitle class="text-[13px] font-semibold tracking-tight" style="font-family: var(--font-sans); color: var(--color-foreground)">Settings</DialogTitle>
                  <span class="text-[11px]" style="color: var(--color-muted-foreground)">{{ tabLabel[activeTab] }}</span>
                </div>
                <button
                  class="ml-auto grid place-items-center w-7 h-7 rounded-md transition-colors cursor-pointer hover:bg-[var(--color-secondary)]"
                  style="color: var(--color-muted-foreground)"
                  aria-label="Close"
                  @click="emit('close')"
                >
                  <svg class="w-3.5 h-3.5" viewBox="0 0 20 20" fill="none" stroke="currentColor" stroke-width="1.8">
                    <path d="M5 5l10 10M15 5L5 15" stroke-linecap="round" />
                  </svg>
                </button>
              </div>

              <!-- Inset content panel — matches .chat-panel. Only the inner div
                   scrolls; each tab block is centered and width-capped. -->
              <div class="settings-panel flex flex-col flex-1 min-h-0">
                <div class="flex-1 min-h-0 overflow-y-auto px-8 py-7 [&>div]:max-w-3xl [&>div]:mx-auto">
                <!-- General tab -->
                <div v-if="activeTab === 'general'" class="space-y-5">
                  <div class="flex items-center gap-2">
                    <span
                      class="w-2 h-2 rounded-full"
                      :style="{ backgroundColor: store.wsConnected ? 'var(--color-primary)' : 'var(--color-border)' }"
                    />
                    <span class="text-xs font-medium" :style="{ color: store.wsConnected ? 'var(--color-primary)' : 'var(--color-muted-foreground)' }">
                      Server {{ store.wsConnected ? 'Online' : 'Offline' }}
                    </span>
                  </div>

                  <div class="grid grid-cols-2 gap-4">
                    <div>
                      <div class="text-[10px] uppercase tracking-wider mb-0.5 font-medium" style="color: var(--color-muted-foreground)">Provider</div>
                      <div class="text-xs font-mono" style="color: var(--color-foreground)">{{ store.providerName || '—' }}</div>
                    </div>
                    <div>
                      <div class="text-[10px] uppercase tracking-wider mb-0.5 font-medium" style="color: var(--color-muted-foreground)">Model</div>
                      <div class="text-xs font-mono" style="color: var(--color-foreground)">{{ store.modelName || '—' }}</div>
                    </div>
                    <div>
                      <div class="text-[10px] uppercase tracking-wider mb-0.5 font-medium" style="color: var(--color-muted-foreground)">Mode</div>
                      <div class="text-xs font-mono" style="color: var(--color-foreground)">{{ store.mode.charAt(0).toUpperCase() + store.mode.slice(1) }}</div>
                    </div>
                    <div>
                      <div class="text-[10px] uppercase tracking-wider mb-0.5 font-medium" style="color: var(--color-muted-foreground)">Auto-approve</div>
                      <div class="text-xs font-mono" style="color: var(--color-foreground)">{{ store.autoApprove ? 'On' : 'Off' }}</div>
                    </div>
                  </div>

                  <div>
                    <div class="text-[10px] uppercase tracking-wider mb-0.5 font-medium" style="color: var(--color-muted-foreground)">Workspace</div>
                    <div class="text-xs font-mono break-all" style="color: var(--color-muted-foreground)">{{ store.pwd || '—' }}</div>
                  </div>

                  <div v-if="store.tokenInfo">
                    <div class="text-[10px] uppercase tracking-wider mb-1 font-medium" style="color: var(--color-muted-foreground)">Token Usage</div>
                    <div class="flex items-center gap-2">
                      <div class="flex-1 h-1.5 rounded-full overflow-hidden" style="background-color: var(--color-muted)">
                        <div
                          class="h-full rounded-full transition-all"
                          :style="{ width: store.tokenPercentage + '%', backgroundColor: store.tokenPercentage > 80 ? 'var(--color-destructive)' : store.tokenPercentage > 50 ? 'var(--color-warning-fg)' : 'var(--color-primary)' }"
                        />
                      </div>
                      <span class="text-[10px] font-mono" style="color: var(--color-muted-foreground)">
                        {{ store.tokenInfo.total_tokens.toLocaleString() }}
                        <span v-if="store.tokenInfo.model_context_limit"> / {{ store.tokenInfo.model_context_limit.toLocaleString() }}</span>
                      </span>
                    </div>
                  </div>
                </div>

                <!-- Appearance tab -->
                <div v-if="activeTab === 'appearance'" class="space-y-5">
                  <h3 class="text-[13px] font-semibold tracking-tight" style="color: var(--color-foreground)">Theme</h3>

                  <!-- System (follow OS) -->
                  <button
                    class="w-full flex items-center gap-3 px-3 py-2.5 rounded-md cursor-pointer transition-colors text-left"
                    :style="themeChoice === 'system'
                      ? { border: '1px solid var(--color-primary)', backgroundColor: 'rgba(255,132,0,0.08)' }
                      : { border: '1px solid var(--color-border)', backgroundColor: 'var(--color-surface)' }"
                    @click="setTheme('system')"
                  >
                    <span class="text-sm">🖥</span>
                    <div class="flex-1 min-w-0">
                      <div class="text-xs font-medium" style="color: var(--color-foreground)">System</div>
                      <div class="text-[10px]" style="color: var(--color-muted-foreground)">Follow your OS light / dark setting</div>
                    </div>
                    <span v-if="themeChoice === 'system'" class="text-[10px] px-1.5 py-0.5 rounded-full shrink-0" style="background-color: rgba(255,132,0,0.12); color: var(--color-primary)">active</span>
                  </button>

                  <!-- Dark themes -->
                  <div>
                    <div class="text-[10px] mb-2 font-medium" style="color: var(--color-muted-foreground)">Dark</div>
                    <div class="grid grid-cols-2 gap-2">
                      <button
                        v-for="t in darkThemes"
                        :key="t.id"
                        :data-theme="t.id"
                        class="rounded-md overflow-hidden cursor-pointer text-left transition-transform active:scale-[0.98]"
                        :style="{ border: themeChoice === t.id ? '2px solid var(--color-primary)' : '1px solid var(--color-border)', backgroundColor: 'var(--color-background)' }"
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
                          <span v-if="themeChoice === t.id" class="text-[10px] shrink-0" style="color: var(--color-primary)">●</span>
                        </div>
                      </button>
                    </div>
                  </div>

                  <!-- Light themes -->
                  <div>
                    <div class="text-[10px] mb-2 font-medium" style="color: var(--color-muted-foreground)">Light</div>
                    <div class="grid grid-cols-2 gap-2">
                      <button
                        v-for="t in lightThemes"
                        :key="t.id"
                        :data-theme="t.id"
                        class="rounded-md overflow-hidden cursor-pointer text-left transition-transform active:scale-[0.98]"
                        :style="{ border: themeChoice === t.id ? '2px solid var(--color-primary)' : '1px solid var(--color-border)', backgroundColor: 'var(--color-background)' }"
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
                          <span v-if="themeChoice === t.id" class="text-[10px] shrink-0" style="color: var(--color-primary)">●</span>
                        </div>
                      </button>
                    </div>
                  </div>

                  <div class="text-[10px] leading-relaxed" style="color: var(--color-muted-foreground)">
                    The terminal UI has its own themes — switch them there with the <span class="font-mono">/theme</span> command.
                  </div>
                </div>

                <!-- Providers tab -->
                <div v-if="activeTab === 'providers'">
                  <div class="flex items-center justify-between mb-4">
                    <div class="flex items-baseline gap-2">
                      <h3 class="text-[13px] font-semibold tracking-tight" style="color: var(--color-foreground)">Providers</h3>
                      <span class="text-[11px] font-mono" style="color: var(--color-muted-foreground)">{{ configuredProviders.length }}</span>
                    </div>
                    <button
                      class="h-7 px-2.5 text-[11px] font-medium rounded-md cursor-pointer transition-colors hover:bg-[rgba(255,132,0,0.16)]"
                      style="color: var(--color-primary); background-color: rgba(255,132,0,0.1)"
                      @click="startAddProvider"
                    >
                      + Add provider
                    </button>
                  </div>

                  <!-- Add provider flow -->
                  <div v-if="showAddProvider" class="mb-4 rounded-md overflow-hidden" style="border: 1px solid var(--color-border)">
                    <div class="px-3 py-2 flex items-center justify-between" style="background-color: var(--color-muted); border-bottom: 1px solid var(--color-border)">
                      <span class="text-[10px] font-medium uppercase tracking-wider" style="color: var(--color-muted-foreground)">
                        {{ addProviderStep === 'select' ? 'Select Provider' : addProviderStep === 'model' ? 'Select Model' : 'Enter API Key' }}
                      </span>
                      <button class="cursor-pointer text-xs" style="color: var(--color-muted-foreground)" @click="showAddProvider = false">✕</button>
                    </div>
                    <div class="p-3 max-h-48 overflow-y-auto">
                      <!-- Select provider -->
                      <div v-if="addProviderStep === 'select'">
                        <div v-if="addLoading" class="text-center py-4 text-xs animate-pulse" style="color: var(--color-muted-foreground)">Loading...</div>
                        <div v-else class="space-y-1">
                          <button
                            v-for="p in addProviderList.filter(x => !configuredProviders.some(c => c.id === x.id))"
                            :key="p.id"
                            class="w-full px-2.5 py-2 text-left rounded-md text-xs cursor-pointer transition-colors"
                            style="color: var(--color-foreground)"
                            @click="selectAddProvider(p.id)"
                          >
                            <span class="font-medium">{{ p.name }}</span>
                            <span class="ml-1.5 font-mono" style="color: var(--color-muted-foreground)">{{ p.id }}</span>
                          </button>
                          <div v-if="addProviderList.filter(x => !configuredProviders.some(c => c.id === x.id)).length === 0" class="text-center py-3 text-[10px]" style="color: var(--color-muted-foreground)">
                            All providers configured
                          </div>
                        </div>
                      </div>
                      <!-- Select model -->
                      <div v-if="addProviderStep === 'model'">
                        <div class="flex items-center gap-1 mb-2">
                          <button class="cursor-pointer" style="color: var(--color-muted-foreground)" @click="addProviderStep = 'select'">
                            <svg class="w-3.5 h-3.5" viewBox="0 0 20 20" fill="currentColor"><path fill-rule="evenodd" d="M12.79 5.23a.75.75 0 01-.02 1.06L8.832 10l3.938 3.71a.75.75 0 11-1.04 1.08l-4.5-4.25a.75.75 0 010-1.08l4.5-4.25a.75.75 0 011.06.02z" clip-rule="evenodd" /></svg>
                          </button>
                          <span class="text-[10px]" style="color: var(--color-muted-foreground)">{{ addProviderInfo()?.name }}</span>
                        </div>
                        <div v-if="addLoading" class="text-center py-4 text-xs animate-pulse" style="color: var(--color-muted-foreground)">Loading...</div>
                        <div v-else class="space-y-1">
                          <button
                            v-for="m in addProviderModels"
                            :key="m.id"
                            class="w-full px-2.5 py-1.5 text-left rounded-md text-xs cursor-pointer transition-colors font-mono"
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
                          <button class="cursor-pointer" style="color: var(--color-muted-foreground)" @click="addProviderStep = 'model'">
                            <svg class="w-3.5 h-3.5" viewBox="0 0 20 20" fill="currentColor"><path fill-rule="evenodd" d="M12.79 5.23a.75.75 0 01-.02 1.06L8.832 10l3.938 3.71a.75.75 0 11-1.04 1.08l-4.5-4.25a.75.75 0 010-1.08l4.5-4.25a.75.75 0 011.06.02z" clip-rule="evenodd" /></svg>
                          </button>
                          <span class="text-[10px] font-mono" style="color: var(--color-muted-foreground)">{{ addSelectedProvider }} / {{ addSelectedModel }}</span>
                        </div>
                        <input v-model="addApiKey" type="password" placeholder="API Key" class="w-full px-2.5 py-1.5 text-xs font-mono rounded-md outline-none" :style="{ backgroundColor: 'var(--color-surface)', border: '1px solid var(--color-border)', color: 'var(--color-foreground)' }" @keydown.enter="submitAddProvider" />
                        <input v-model="addBaseURL" type="text" placeholder="Base URL (optional)" class="w-full px-2.5 py-1.5 text-xs font-mono rounded-md outline-none" :style="{ backgroundColor: 'var(--color-surface)', border: '1px solid var(--color-border)', color: 'var(--color-foreground)' }" @keydown.enter="submitAddProvider" />
                        <div v-if="addError" class="text-[10px]" style="color: var(--color-destructive)">{{ addError }}</div>
                        <button :disabled="addLoading || !addApiKey" class="w-full px-2.5 py-1.5 text-xs text-white rounded-md cursor-pointer transition-colors font-medium disabled:opacity-50" style="background-color: var(--color-primary)" @click="submitAddProvider">
                          {{ addLoading ? 'Saving...' : 'Add' }}
                        </button>
                      </div>
                    </div>
                  </div>

                  <!-- Provider list -->
                  <div v-if="configuredProviders.length === 0" class="flex flex-col items-center justify-center text-center py-12 gap-2.5">
                    <div class="w-9 h-9 grid place-items-center rounded-lg" style="background-color: var(--color-secondary); color: var(--color-muted-foreground)">
                      <svg class="w-4 h-4" viewBox="0 0 20 20" fill="none" stroke="currentColor" stroke-width="1.6" v-html="iconFor.providers" />
                    </div>
                    <div class="text-[13px] font-medium" style="color: var(--color-foreground)">No providers configured</div>
                    <div class="text-[11px] leading-relaxed max-w-[240px]" style="color: var(--color-muted-foreground)">
                      Add one with the <span class="font-mono">Add&nbsp;provider</span> button above to get started.
                    </div>
                  </div>
                  <div v-else class="space-y-2">
                    <div
                      v-for="p in configuredProviders"
                      :key="p.id"
                      class="flex items-center gap-3 px-3 py-2.5 rounded-md"
                      style="border: 1px solid var(--color-border); background-color: var(--color-surface)"
                    >
                      <span class="text-sm">🔑</span>
                      <div class="flex-1 min-w-0">
                        <div class="text-xs font-medium font-mono" style="color: var(--color-foreground)">{{ p.id }}</div>
                        <div class="text-[10px] font-mono truncate" style="color: var(--color-muted-foreground)">
                          {{ p.api_key || '—' }}
                          <template v-if="p.base_url"> · {{ p.base_url }}</template>
                        </div>
                      </div>
                      <span
                        v-if="store.providerName === p.id"
                        class="text-[10px] px-1.5 py-0.5 rounded-full"
                        style="background-color: rgba(255,132,0,0.1); color: var(--color-primary)"
                      >
                        active
                      </span>
                      <button
                        v-if="deleteConfirmId !== p.id"
                        class="cursor-pointer transition-colors"
                        style="color: var(--color-muted-foreground)"
                        title="Remove provider"
                        @click="deleteConfirmId = p.id"
                      >
                        <svg class="w-3.5 h-3.5" viewBox="0 0 20 20" fill="currentColor"><path fill-rule="evenodd" d="M8.75 1A2.75 2.75 0 006 3.75v.443c-.795.077-1.584.176-2.365.298a.75.75 0 10.23 1.482l.149-.022.841 10.518A2.75 2.75 0 007.596 19h4.807a2.75 2.75 0 002.742-2.53l.841-10.519.149.023a.75.75 0 00.23-1.482A41.03 41.03 0 0014 4.193V3.75A2.75 2.75 0 0011.25 1h-2.5zM10 4c.84 0 1.673.025 2.5.075V3.75c0-.69-.56-1.25-1.25-1.25h-2.5c-.69 0-1.25.56-1.25 1.25v.325C8.327 4.025 9.16 4 10 4zM8.58 7.72a.75.75 0 00-1.5.06l.3 7.5a.75.75 0 101.5-.06l-.3-7.5zm4.34.06a.75.75 0 10-1.5-.06l-.3 7.5a.75.75 0 101.5.06l.3-7.5z" clip-rule="evenodd" /></svg>
                      </button>
                      <div v-else class="flex items-center gap-1">
                        <button class="text-[10px] px-1.5 py-0.5 text-white rounded cursor-pointer" style="background-color: var(--color-destructive)" @click="deleteProvider(p.id)">Delete</button>
                        <button class="text-[10px] px-1.5 py-0.5 cursor-pointer" style="color: var(--color-muted-foreground)" @click="deleteConfirmId = ''">Cancel</button>
                      </div>
                    </div>
                  </div>
                </div>

                <!-- MCP Servers tab -->
                <div v-if="activeTab === 'mcp'">
                  <div class="flex items-center justify-between mb-4">
                    <div class="flex items-baseline gap-2">
                      <h3 class="text-[13px] font-semibold tracking-tight" style="color: var(--color-foreground)">MCP Servers</h3>
                      <span class="text-[11px] font-mono" style="color: var(--color-muted-foreground)">{{ Object.keys(mcpServers).length }}</span>
                    </div>
                    <button
                      v-if="mcpEditing === null"
                      class="h-7 px-2.5 text-[11px] font-medium rounded-md cursor-pointer transition-colors hover:bg-[rgba(255,132,0,0.16)]"
                      style="color: var(--color-primary); background-color: rgba(255,132,0,0.1)"
                      @click="openAddMCP"
                    >+ Add server</button>
                  </div>

                  <!-- Add / Edit form -->
                  <div v-if="mcpEditing !== null" class="space-y-3 mb-2 p-3 rounded-md" style="border: 1px solid var(--color-border)">
                    <div class="text-xs font-medium" style="color: var(--color-foreground)">{{ mcpEditing ? 'Edit server' : 'Add server' }}</div>

                    <div>
                      <div class="text-[10px] uppercase tracking-wider mb-1 font-medium" style="color: var(--color-muted-foreground)">Server name</div>
                      <input
                        v-model="mcpForm.name"
                        :disabled="!!mcpEditing"
                        type="text"
                        placeholder="my-server"
                        class="w-full px-2.5 py-1.5 text-xs rounded-md outline-none"
                        style="background-color: var(--color-background); border: 1px solid var(--color-border); color: var(--color-foreground)"
                      />
                    </div>

                    <div>
                      <div class="text-[10px] uppercase tracking-wider mb-1 font-medium" style="color: var(--color-muted-foreground)">Transport</div>
                      <div class="inline-flex rounded-md overflow-hidden" style="border: 1px solid var(--color-border)">
                        <button
                          v-for="t in (['local', 'http', 'sse'] as const)"
                          :key="t"
                          class="px-3 py-1 text-[11px] cursor-pointer transition-colors"
                          :style="mcpForm.transport === t
                            ? { backgroundColor: 'var(--color-primary)', color: '#fff' }
                            : { color: 'var(--color-muted-foreground)' }"
                          @click="mcpForm.transport = t"
                        >{{ t === 'local' ? 'Local' : t.toUpperCase() }}</button>
                      </div>
                    </div>

                    <!-- HTTP / SSE fields -->
                    <template v-if="mcpForm.transport !== 'local'">
                      <div>
                        <div class="text-[10px] uppercase tracking-wider mb-1 font-medium" style="color: var(--color-muted-foreground)">URL</div>
                        <input
                          v-model="mcpForm.url"
                          type="text"
                          placeholder="https://api.example.com/mcp"
                          class="w-full px-2.5 py-1.5 text-xs font-mono rounded-md outline-none"
                          style="background-color: var(--color-background); border: 1px solid var(--color-border); color: var(--color-foreground)"
                        />
                      </div>

                      <div>
                        <div class="flex items-center justify-between mb-1">
                          <div class="text-[10px] uppercase tracking-wider font-medium" style="color: var(--color-muted-foreground)">Headers</div>
                          <button class="text-[11px] cursor-pointer" style="color: var(--color-primary)" @click="addHeaderRow">+ Add header</button>
                        </div>
                        <div v-for="(h, i) in mcpForm.headers" :key="i" class="flex gap-2 mb-1.5">
                          <input v-model="h.key" type="text" placeholder="Key" class="flex-1 px-2 py-1 text-[11px] font-mono rounded-md outline-none" style="background-color: var(--color-background); border: 1px solid var(--color-border); color: var(--color-foreground)" />
                          <input v-model="h.value" type="text" placeholder="Value" class="flex-1 px-2 py-1 text-[11px] font-mono rounded-md outline-none" style="background-color: var(--color-background); border: 1px solid var(--color-border); color: var(--color-foreground)" />
                          <button class="px-2 text-xs cursor-pointer" style="color: var(--color-muted-foreground)" @click="removeHeaderRow(i)">✕</button>
                        </div>
                      </div>

                      <label class="flex items-center gap-2 cursor-pointer">
                        <input v-model="mcpForm.oauthEnabled" type="checkbox" />
                        <span class="text-xs" style="color: var(--color-foreground)">Use OAuth (log in after saving)</span>
                      </label>

                      <div v-if="mcpForm.oauthEnabled" class="space-y-2 pl-1">
                        <div>
                          <div class="text-[10px] uppercase tracking-wider mb-1 font-medium" style="color: var(--color-muted-foreground)">OAuth Client ID</div>
                          <input
                            v-model="mcpForm.clientId"
                            type="text"
                            placeholder="Optional — leave blank to auto-register"
                            class="w-full px-2.5 py-1.5 text-xs font-mono rounded-md outline-none"
                            style="background-color: var(--color-background); border: 1px solid var(--color-border); color: var(--color-foreground)"
                          />
                        </div>
                        <div>
                          <div class="text-[10px] uppercase tracking-wider mb-1 font-medium" style="color: var(--color-muted-foreground)">OAuth Client Secret</div>
                          <input
                            v-model="mcpForm.clientSecret"
                            type="password"
                            placeholder="Optional (confidential clients)"
                            class="w-full px-2.5 py-1.5 text-xs font-mono rounded-md outline-none"
                            style="background-color: var(--color-background); border: 1px solid var(--color-border); color: var(--color-foreground)"
                          />
                        </div>
                        <div>
                          <div class="text-[10px] uppercase tracking-wider mb-1 font-medium" style="color: var(--color-muted-foreground)">Scopes</div>
                          <input
                            v-model="mcpForm.scopesText"
                            type="text"
                            placeholder="space-separated, optional"
                            class="w-full px-2.5 py-1.5 text-xs font-mono rounded-md outline-none"
                            style="background-color: var(--color-background); border: 1px solid var(--color-border); color: var(--color-foreground)"
                          />
                        </div>
                      </div>

                      <div>
                        <div class="text-[10px] uppercase tracking-wider mb-1 font-medium" style="color: var(--color-muted-foreground)">Timeout (seconds)</div>
                        <input
                          v-model="mcpForm.timeout"
                          type="number"
                          placeholder="180"
                          class="w-32 px-2.5 py-1.5 text-xs rounded-md outline-none"
                          style="background-color: var(--color-background); border: 1px solid var(--color-border); color: var(--color-foreground)"
                        />
                      </div>
                    </template>

                    <!-- Local fields -->
                    <template v-else>
                      <div>
                        <div class="text-[10px] uppercase tracking-wider mb-1 font-medium" style="color: var(--color-muted-foreground)">Command</div>
                        <input
                          v-model="mcpForm.command"
                          type="text"
                          placeholder="npx"
                          class="w-full px-2.5 py-1.5 text-xs font-mono rounded-md outline-none"
                          style="background-color: var(--color-background); border: 1px solid var(--color-border); color: var(--color-foreground)"
                        />
                      </div>
                      <div>
                        <div class="text-[10px] uppercase tracking-wider mb-1 font-medium" style="color: var(--color-muted-foreground)">Arguments</div>
                        <input
                          v-model="mcpForm.argsText"
                          type="text"
                          placeholder="-y @some/mcp-server"
                          class="w-full px-2.5 py-1.5 text-xs font-mono rounded-md outline-none"
                          style="background-color: var(--color-background); border: 1px solid var(--color-border); color: var(--color-foreground)"
                        />
                      </div>
                    </template>

                    <div v-if="mcpFormError" class="text-[11px]" style="color: var(--color-error-fg)">{{ mcpFormError }}</div>

                    <div class="flex justify-end gap-2 pt-1">
                      <button class="text-[11px] px-3 py-1.5 rounded-md cursor-pointer" style="border: 1px solid var(--color-border); color: var(--color-foreground)" @click="cancelMCPEdit">Cancel</button>
                      <button
                        class="text-[11px] px-3 py-1.5 rounded-md cursor-pointer"
                        style="background-color: var(--color-primary); color: #fff"
                        :disabled="mcpSaving"
                        @click="saveMCP"
                      >{{ mcpSaving ? 'Saving…' : 'Save' }}</button>
                    </div>
                  </div>

                  <div v-if="mcpLoading" class="text-center text-xs py-6 animate-pulse" style="color: var(--color-muted-foreground)">
                    Loading...
                  </div>
                  <div v-else-if="mcpEditing === null && Object.keys(mcpServers).length === 0" class="flex flex-col items-center justify-center text-center py-12 gap-2.5">
                    <div class="w-9 h-9 grid place-items-center rounded-lg" style="background-color: var(--color-secondary); color: var(--color-muted-foreground)">
                      <svg class="w-4 h-4" viewBox="0 0 20 20" fill="none" stroke="currentColor" stroke-width="1.6" v-html="iconFor.mcp" />
                    </div>
                    <div class="text-[13px] font-medium" style="color: var(--color-foreground)">No MCP servers configured</div>
                    <div class="text-[11px] leading-relaxed max-w-[240px]" style="color: var(--color-muted-foreground)">Connect one with the <span class="font-mono">Add&nbsp;server</span> button above.</div>
                  </div>
                  <div v-else-if="mcpEditing === null" class="space-y-2">
                    <div
                      v-for="(info, name) in mcpServers"
                      :key="name"
                      class="px-3 py-2.5 rounded-md"
                      :style="{
                        border: '1px solid var(--color-border)',
                        backgroundColor: info.enabled ? 'var(--color-surface)' : 'var(--color-muted)',
                        opacity: info.enabled ? 1 : 0.6,
                      }"
                    >
                      <div class="flex items-center gap-3">
                        <span class="text-sm">{{ serverIcon(info.type) }}</span>
                        <div class="flex-1 min-w-0">
                          <div class="flex items-center gap-2">
                            <span class="text-xs font-medium" style="color: var(--color-foreground)">{{ name }}</span>
                            <span class="text-[9px] px-1.5 py-0.5 rounded uppercase tracking-wide" style="background-color: var(--color-muted); color: var(--color-muted-foreground)">{{ info.type || 'stdio' }}</span>
                            <span class="text-[10px]" :style="{ color: mcpStatusColor(info) }">● {{ mcpStatusLabel(info) }}</span>
                          </div>
                          <div class="text-[10px] font-mono truncate" style="color: var(--color-muted-foreground)">
                            {{ info.type === 'sse' || info.type === 'http' ? info.url : info.command }}
                          </div>
                        </div>
                        <button class="text-[11px] cursor-pointer px-1.5" style="color: var(--color-muted-foreground)" title="Edit" @click="openEditMCP(info)">Edit</button>
                        <button class="text-[11px] cursor-pointer px-1.5" style="color: var(--color-error-fg)" title="Delete" @click="deleteMCP(String(name))">Delete</button>
                        <button
                          class="relative inline-flex h-5 w-9 items-center rounded-full cursor-pointer transition-colors shrink-0"
                          :style="{ backgroundColor: info.enabled ? 'var(--color-primary)' : 'var(--color-border)' }"
                          @click="toggleMCP(String(name), !info.enabled)"
                          :title="info.enabled ? 'Disable' : 'Enable'"
                        >
                          <span
                            class="inline-block h-3.5 w-3.5 rounded-full bg-white shadow-sm transition-transform"
                            :class="info.enabled ? 'translate-x-4.5' : 'translate-x-0.76'"
                          />
                        </button>
                      </div>
                      <!-- OAuth login row -->
                      <div v-if="(info.type === 'http' || info.type === 'sse') && (info.oauth || info.status === 'needs_auth')" class="mt-2 flex items-center gap-2">
                        <button
                          class="text-[11px] px-2.5 py-1 rounded-md cursor-pointer"
                          style="border: 1px solid var(--color-primary); color: var(--color-primary)"
                          :disabled="mcpLoginBusy === name"
                          @click="loginMCP(String(name))"
                        >{{ mcpLoginBusy === name ? 'Waiting for browser…' : (info.has_auth ? 'Re-authenticate' : 'Log in') }}</button>
                        <span v-if="info.has_auth" class="text-[10px]" style="color: var(--color-success-fg)">Authenticated</span>
                      </div>
                      <div v-if="mcpLoginBusy === name && mcpLoginMessage" class="mt-1 text-[10px]" style="color: var(--color-muted-foreground)">{{ mcpLoginMessage }}</div>
                      <div v-else-if="mcpLoginMessage && !mcpLoginBusy" class="mt-1 text-[10px]" style="color: var(--color-warning-fg)">{{ mcpLoginMessage }}</div>
                      <div v-if="info.error" class="mt-1 text-[10px] font-mono" style="color: var(--color-error-fg)">{{ info.error }}</div>
                    </div>
                  </div>
                </div>

                <!-- Skills tab -->
                <div v-if="activeTab === 'skills'">
                  <div class="flex items-baseline gap-2 mb-4">
                    <h3 class="text-[13px] font-semibold tracking-tight" style="color: var(--color-foreground)">Skills</h3>
                    <span class="text-[11px] font-mono" style="color: var(--color-muted-foreground)">{{ skills.length }}</span>
                  </div>
                  <div class="flex items-center gap-2 mb-3">
                    <div class="inline-flex rounded-md overflow-hidden" style="border: 1px solid var(--color-border)">
                      <button
                        v-for="f in (['all', 'local', 'builtin'] as const)"
                        :key="f"
                        class="px-2.5 py-1 text-[11px] cursor-pointer transition-colors"
                        :style="skillFilter === f
                          ? { backgroundColor: 'var(--color-primary)', color: '#fff' }
                          : { color: 'var(--color-muted-foreground)' }"
                        @click="skillFilter = f"
                      >{{ f === 'all' ? 'All' : f === 'local' ? 'On this device' : 'Built-in' }}</button>
                    </div>
                    <input
                      v-model="skillSearch"
                      type="text"
                      placeholder="Search skills…"
                      class="flex-1 px-2.5 py-1 text-xs rounded-md outline-none"
                      style="background-color: var(--color-background); border: 1px solid var(--color-border); color: var(--color-foreground)"
                    />
                  </div>

                  <div v-if="skillsLoading" class="text-center text-xs py-6 animate-pulse" style="color: var(--color-muted-foreground)">Loading...</div>
                  <div v-else-if="filteredSkills.length === 0" class="flex flex-col items-center justify-center text-center py-12 gap-2.5">
                    <div class="w-9 h-9 grid place-items-center rounded-lg" style="background-color: var(--color-secondary); color: var(--color-muted-foreground)">
                      <svg class="w-4 h-4" viewBox="0 0 20 20" fill="none" stroke="currentColor" stroke-width="1.6" v-html="iconFor.skills" />
                    </div>
                    <div class="text-[13px] font-medium" style="color: var(--color-foreground)">No skills found</div>
                    <div class="text-[11px] leading-relaxed max-w-[240px]" style="color: var(--color-muted-foreground)">Try a different filter or search term.</div>
                  </div>
                  <div v-else class="space-y-2">
                    <div
                      v-for="sk in filteredSkills"
                      :key="sk.name"
                      class="flex items-center gap-3 px-3 py-2.5 rounded-md"
                      :style="{
                        border: '1px solid var(--color-border)',
                        backgroundColor: sk.enabled ? 'var(--color-surface)' : 'var(--color-muted)',
                        opacity: sk.enabled ? 1 : 0.6,
                      }"
                    >
                      <div class="flex-1 min-w-0">
                        <div class="flex items-center gap-2">
                          <span class="text-xs font-medium" style="color: var(--color-foreground)">{{ sk.name }}</span>
                          <span v-if="sk.builtin" class="text-[9px] px-1.5 py-0.5 rounded uppercase tracking-wide" style="background-color: var(--color-muted); color: var(--color-muted-foreground)">Built-in</span>
                        </div>
                        <div v-if="sk.description" class="text-[10px] truncate" style="color: var(--color-muted-foreground)">{{ sk.description }}</div>
                      </div>
                      <button
                        class="relative inline-flex h-5 w-9 items-center rounded-full cursor-pointer transition-colors shrink-0"
                        :style="{ backgroundColor: sk.enabled ? 'var(--color-primary)' : 'var(--color-border)' }"
                        @click="toggleSkill(sk.name, !sk.enabled)"
                        :title="sk.enabled ? 'Disable' : 'Enable'"
                      >
                        <span
                          class="inline-block h-3.5 w-3.5 rounded-full bg-white shadow-sm transition-transform"
                          :class="sk.enabled ? 'translate-x-4.5' : 'translate-x-0.76'"
                        />
                      </button>
                    </div>
                  </div>
                </div>

                <!-- SSH tab -->
                <div v-if="activeTab === 'ssh'">
                  <h3 class="text-[13px] font-semibold tracking-tight mb-4" style="color: var(--color-foreground)">SSH Environments</h3>

                  <div class="mb-3">
                    <div class="text-[11px] font-medium mb-1" style="color: var(--color-muted-foreground)">Current Environment</div>
                    <div class="inline-flex items-center gap-1.5 px-2 py-1 rounded text-xs font-medium" style="background-color: rgba(255,132,0,0.1); color: var(--color-primary)">
                      <span class="w-1.5 h-1.5 rounded-full" style="background-color: var(--color-primary)" />
                      {{ sshCurrent }}
                    </div>
                  </div>

                  <div v-if="sshAliases.length === 0" class="flex flex-col items-center justify-center text-center py-12 gap-2.5">
                    <div class="w-9 h-9 grid place-items-center rounded-lg" style="background-color: var(--color-secondary); color: var(--color-muted-foreground)">
                      <svg class="w-4 h-4" viewBox="0 0 20 20" fill="none" stroke="currentColor" stroke-width="1.6" v-html="iconFor.ssh" />
                    </div>
                    <div class="text-[13px] font-medium" style="color: var(--color-foreground)">No SSH aliases configured</div>
                    <div class="text-[11px] leading-relaxed max-w-[240px]" style="color: var(--color-muted-foreground)">
                      Add aliases to <span class="font-mono">~/.jcode/config.json</span>.
                    </div>
                  </div>
                  <div v-else class="space-y-2">
                    <div
                      v-for="alias in sshAliases"
                      :key="alias.name"
                      class="flex items-center gap-3 px-3 py-2.5 rounded-md"
                      style="border: 1px solid var(--color-border); background-color: var(--color-surface)"
                    >
                      <span class="text-sm">🖥</span>
                      <div class="flex-1 min-w-0">
                        <div class="text-xs font-medium" style="color: var(--color-foreground)">{{ alias.name }}</div>
                        <div class="text-[10px] font-mono truncate" style="color: var(--color-muted-foreground)">
                          {{ alias.addr }}
                          <template v-if="alias.path"> · {{ alias.path }}</template>
                        </div>
                      </div>
                      <span
                        v-if="sshCurrent === alias.name"
                        class="text-[10px] px-1.5 py-0.5 rounded-full"
                        style="background-color: rgba(255,132,0,0.1); color: var(--color-primary)"
                      >
                        active
                      </span>
                    </div>
                  </div>
                </div>

                <!-- Channels tab -->
                <div v-if="activeTab === 'channels'">
                  <h3 class="text-[13px] font-semibold tracking-tight mb-4" style="color: var(--color-foreground)">Notification Channels</h3>

                  <div v-if="!channelAvailable" class="flex flex-col items-center justify-center text-center py-12 gap-2.5">
                    <div class="w-9 h-9 grid place-items-center rounded-lg" style="background-color: var(--color-secondary); color: var(--color-muted-foreground)">
                      <svg class="w-4 h-4" viewBox="0 0 20 20" fill="none" stroke="currentColor" stroke-width="1.6" v-html="iconFor.channels" />
                    </div>
                    <div class="text-[13px] font-medium" style="color: var(--color-foreground)">No channels configured</div>
                    <div class="text-[11px] leading-relaxed max-w-[260px]" style="color: var(--color-muted-foreground)">
                      Set <span class="font-mono">channel.web_enabled: true</span> in <span class="font-mono">~/.jcode/config.json</span>.
                    </div>
                  </div>

                  <div v-else class="space-y-4">
                    <div class="px-4 py-3 rounded-md" style="border: 1px solid var(--color-border); background-color: var(--color-surface)">
                      <div class="flex items-center justify-between mb-3">
                        <div class="flex items-center gap-2">
                          <span class="text-base">💬</span>
                          <div>
                            <div class="text-xs font-medium" style="color: var(--color-foreground)">WeChat</div>
                            <div class="text-[10px]" style="color: var(--color-muted-foreground)">iLink Bot integration</div>
                          </div>
                        </div>
                        <div class="flex items-center gap-1.5">
                          <span
                            class="w-1.5 h-1.5 rounded-full"
                            :style="{
                              backgroundColor: channelState === 'enabled' ? 'var(--color-primary)'
                                : (channelState === 'disabled' || channelState === 'scanning') ? 'var(--color-warning-fg)'
                                : 'var(--color-border)',
                            }"
                          />
                          <span class="text-[10px] font-medium" :style="{
                            color: channelState === 'enabled' ? 'var(--color-primary)'
                              : (channelState === 'disabled' || channelState === 'scanning') ? 'var(--color-warning-fg)'
                              : 'var(--color-muted-foreground)',
                          }">
                            {{ channelState === 'enabled' ? 'Connected' : channelState === 'disabled' ? 'Disconnected' : channelState === 'scanning' ? 'Scanning...' : 'Not configured' }}
                          </span>
                        </div>
                      </div>

                      <div v-if="channelQRContent" class="flex flex-col items-center py-3">
                        <canvas ref="qrCanvas" class="rounded-md" style="border: 1px solid var(--color-border)" />
                        <div class="text-[10px] mt-2" style="color: var(--color-muted-foreground)">Scan with WeChat to connect</div>
                      </div>

                      <div class="flex gap-2 mt-2">
                        <button
                          v-if="channelState === 'none'"
                          :disabled="channelLoading"
                          class="flex-1 px-3 py-1.5 text-xs rounded-md text-white disabled:opacity-50 cursor-pointer transition-colors font-medium"
                          style="background-color: var(--color-primary)"
                          @click="channelLogin"
                        >
                          {{ channelLoading ? 'Loading...' : 'Connect' }}
                        </button>
                        <button
                          v-if="channelState === 'enabled' || channelState === 'disabled'"
                          :disabled="channelLoading"
                          class="flex-1 px-3 py-1.5 text-xs rounded-md disabled:opacity-50 cursor-pointer transition-colors font-medium"
                          style="color: var(--color-destructive)"
                          @click="channelLogout"
                        >
                          Disconnect
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
                        <div class="text-xs font-medium" style="color: var(--color-warning-fg)">Send a message to activate</div>
                        <div class="text-[10px] mt-0.5 leading-relaxed" style="color: var(--color-warning-fg); opacity: 0.8">
                          Please send any message to the WeChat bot now to activate notifications. Once activated, you can receive notifications for 24 hours.
                        </div>
                      </div>
                      <button
                        class="shrink-0 cursor-pointer"
                        style="color: var(--color-warning-fg)"
                        @click="channelLoginReminder = false"
                      >✕</button>
                    </div>

                    <div class="text-[10px] leading-relaxed" style="color: var(--color-muted-foreground)">
                      When connected, jcode sends approval requests and task completion notifications to your WeChat.
                    </div>
                  </div>
                </div>

                <!-- Shortcuts tab -->
                <div v-if="activeTab === 'shortcuts'">
                  <h3 class="text-[13px] font-semibold tracking-tight mb-4" style="color: var(--color-foreground)">Keyboard Shortcuts</h3>
                  <div class="space-y-1.5">
                    <div
                      v-for="s in shortcuts"
                      :key="s.keys"
                      class="flex items-center justify-between py-1.5 px-2 rounded"
                    >
                      <span class="text-xs" style="color: var(--color-foreground)">{{ s.desc }}</span>
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
  border-radius: 14px;
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
</style>
