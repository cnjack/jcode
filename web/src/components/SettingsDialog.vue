<script setup lang="ts">
import { ref, watch, nextTick } from 'vue'
import { useChatStore } from '@/stores/chat'
import { api } from '@/composables/api'
import type { MCPServerInfo, SSHAlias } from '@/types/api'
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
const activeTab = ref<'general' | 'mcp' | 'ssh' | 'channels' | 'shortcuts'>('general')
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

watch(() => props.open, async (isOpen) => {
  if (isOpen) {
    mcpLoading.value = true
    try {
      const result = await api.mcpList()
      mcpServers.value = result.servers
    } catch { /* ignore */ }
    mcpLoading.value = false

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
  } else {
    channelQRContent.value = ''
  }
})

async function toggleMCP(name: string, enabled: boolean) {
  try {
    await api.mcpToggle(name, enabled)
    if (mcpServers.value[name]) {
      mcpServers.value[name].enabled = enabled
    }
  } catch (err) {
    console.error('Failed to toggle MCP server:', err)
  }
}

function serverIcon(type: string) {
  return type === 'sse' || type === 'http' ? '🌐' : '⚡'
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
  mcp: 'MCP Servers',
  ssh: 'SSH',
  channels: 'Channels',
  shortcuts: 'Shortcuts',
}
</script>

<template>
  <TransitionRoot :show="open" as="template">
    <Dialog @close="emit('close')" class="relative z-50">
      <TransitionChild
        enter="ease-out duration-150"
        enter-from="opacity-0"
        enter-to="opacity-100"
        leave="ease-in duration-100"
        leave-from="opacity-100"
        leave-to="opacity-0">
        <div class="fixed inset-0 bg-black/40 dark:bg-black/60 backdrop-blur-sm" />
      </TransitionChild>

      <div class="fixed inset-0 flex items-start justify-center pt-16 px-4">
        <TransitionChild
          enter="ease-out duration-150"
          enter-from="opacity-0 translate-y-2"
          enter-to="opacity-100 translate-y-0"
          leave="ease-in duration-100"
          leave-from="opacity-100 translate-y-0"
          leave-to="opacity-0 translate-y-2">
          <DialogPanel class="w-2xl bg-white dark:bg-zinc-900 border border-zinc-200 dark:border-zinc-700 rounded-2xl shadow-2xl overflow-hidden">
            <div class="px-5 pt-5 pb-3 border-b border-zinc-200 dark:border-zinc-800">
              <DialogTitle class="text-sm font-semibold text-zinc-800 dark:text-zinc-100" style="font-family: var(--font-sans)">Settings</DialogTitle>
            </div>

            <div class="flex h-105">
              <!-- Left sidebar -->
              <nav class="w-40 border-r border-zinc-200 dark:border-zinc-800 py-2 shrink-0">
                <button
                  v-for="tab in (['general', 'mcp', 'ssh', 'channels', 'shortcuts'] as const)"
                  :key="tab"
                  class="w-full px-4 py-2 text-left text-xs transition-colors cursor-pointer"
                  :class="activeTab === tab
                    ? 'text-emerald-700 dark:text-emerald-400 bg-emerald-50 dark:bg-emerald-500/10 font-medium'
                    : 'text-zinc-500 dark:text-zinc-400 hover:text-zinc-700 dark:hover:text-zinc-200 hover:bg-zinc-50 dark:hover:bg-zinc-800'"
                  @click="activeTab = tab"
                >
                  {{ tabLabel[tab] }}
                </button>
              </nav>

              <!-- Right content -->
              <div class="flex-1 overflow-y-auto p-5">
                <!-- General tab -->
                <div v-if="activeTab === 'general'" class="space-y-4">
                  <div class="flex items-center gap-2">
                    <span
                      class="w-2 h-2 rounded-full"
                      :class="store.wsConnected ? 'bg-emerald-400' : 'bg-zinc-300 dark:bg-zinc-600'"
                    />
                    <span class="text-xs font-medium" :class="store.wsConnected ? 'text-emerald-600 dark:text-emerald-400' : 'text-zinc-400 dark:text-zinc-500'">
                      Server {{ store.wsConnected ? 'Online' : 'Offline' }}
                    </span>
                  </div>

                  <div class="grid grid-cols-2 gap-4">
                    <div>
                      <div class="text-[10px] text-zinc-400 dark:text-zinc-500 uppercase tracking-wider mb-0.5 font-medium">Provider</div>
                      <div class="text-xs font-mono text-zinc-600 dark:text-zinc-300">{{ store.providerName || '—' }}</div>
                    </div>
                    <div>
                      <div class="text-[10px] text-zinc-400 dark:text-zinc-500 uppercase tracking-wider mb-0.5 font-medium">Model</div>
                      <div class="text-xs font-mono text-zinc-600 dark:text-zinc-300">{{ store.modelName || '—' }}</div>
                    </div>
                    <div>
                      <div class="text-[10px] text-zinc-400 dark:text-zinc-500 uppercase tracking-wider mb-0.5 font-medium">Mode</div>
                      <div class="text-xs font-mono text-zinc-600 dark:text-zinc-300">{{ store.mode === 'agent' ? 'Agent' : 'Plan' }}</div>
                    </div>
                    <div>
                      <div class="text-[10px] text-zinc-400 dark:text-zinc-500 uppercase tracking-wider mb-0.5 font-medium">Auto-approve</div>
                      <div class="text-xs font-mono text-zinc-600 dark:text-zinc-300">{{ store.autoApprove ? 'On' : 'Off' }}</div>
                    </div>
                  </div>

                  <div>
                    <div class="text-[10px] text-zinc-400 dark:text-zinc-500 uppercase tracking-wider mb-0.5 font-medium">Workspace</div>
                    <div class="text-xs font-mono text-zinc-500 dark:text-zinc-400 break-all">{{ store.pwd || '—' }}</div>
                  </div>

                  <div v-if="store.tokenInfo">
                    <div class="text-[10px] text-zinc-400 dark:text-zinc-500 uppercase tracking-wider mb-1 font-medium">Token Usage</div>
                    <div class="flex items-center gap-2">
                      <div class="flex-1 h-1.5 bg-zinc-100 dark:bg-zinc-800 rounded-full overflow-hidden">
                        <div
                          class="h-full rounded-full transition-all"
                          :class="store.tokenPercentage > 80 ? 'bg-red-400' : store.tokenPercentage > 50 ? 'bg-amber-400' : 'bg-emerald-400'"
                          :style="{ width: store.tokenPercentage + '%' }"
                        />
                      </div>
                      <span class="text-[10px] text-zinc-500 dark:text-zinc-400 font-mono">
                        {{ store.tokenInfo.total_tokens.toLocaleString() }}
                        <span v-if="store.tokenInfo.model_context_limit"> / {{ store.tokenInfo.model_context_limit.toLocaleString() }}</span>
                      </span>
                    </div>
                  </div>
                </div>

                <!-- MCP Servers tab -->
                <div v-if="activeTab === 'mcp'">
                  <div class="text-[10px] text-zinc-400 dark:text-zinc-500 uppercase tracking-wider mb-3 font-medium">MCP Servers</div>
                  <div v-if="mcpLoading" class="text-center text-xs text-zinc-400 dark:text-zinc-500 py-6 animate-pulse">
                    Loading...
                  </div>
                  <div v-else-if="Object.keys(mcpServers).length === 0" class="text-center py-8">
                    <div class="text-xs text-zinc-400 dark:text-zinc-500 mb-1">No MCP servers configured</div>
                    <div class="text-[10px] text-zinc-400 dark:text-zinc-600">
                      Edit <span class="font-mono">~/.jcode/config.json</span>
                    </div>
                  </div>
                  <div v-else class="space-y-2">
                    <div
                      v-for="(info, name) in mcpServers"
                      :key="name"
                      class="flex items-center gap-3 px-3 py-2.5 rounded-xl border border-zinc-200 dark:border-zinc-700/60"
                      :class="info.enabled
                        ? 'bg-white dark:bg-zinc-800/60'
                        : 'bg-zinc-50 dark:bg-zinc-800/30 opacity-60'"
                    >
                      <span class="text-sm">{{ serverIcon(info.type) }}</span>
                      <div class="flex-1 min-w-0">
                        <div class="text-xs font-medium text-zinc-700 dark:text-zinc-200">{{ name }}</div>
                        <div class="text-[10px] text-zinc-400 dark:text-zinc-500 font-mono truncate">
                          {{ info.type === 'sse' || info.type === 'http' ? info.url : info.command }}
                        </div>
                      </div>
                      <button
                        class="relative inline-flex h-5 w-9 items-center rounded-full cursor-pointer transition-colors shrink-0"
                        :class="info.enabled ? 'bg-emerald-500' : 'bg-zinc-300 dark:bg-zinc-600'"
                        @click="toggleMCP(String(name), !info.enabled)"
                        :title="info.enabled ? 'Disable' : 'Enable'"
                      >
                        <span
                          class="inline-block h-3.5 w-3.5 rounded-full bg-white shadow-sm transition-transform"
                          :class="info.enabled ? 'translate-x-4.5' : 'translate-x-0.76'"
                        />
                      </button>
                    </div>
                  </div>
                </div>

                <!-- SSH tab -->
                <div v-if="activeTab === 'ssh'">
                  <div class="text-[10px] text-zinc-400 dark:text-zinc-500 uppercase tracking-wider mb-3 font-medium">SSH Environments</div>

                  <div class="mb-3">
                    <div class="text-[10px] text-zinc-400 dark:text-zinc-500 mb-1">Current Environment</div>
                    <div class="inline-flex items-center gap-1.5 px-2 py-1 rounded-lg bg-emerald-50 dark:bg-emerald-500/10 text-xs text-emerald-700 dark:text-emerald-400 font-medium">
                      <span class="w-1.5 h-1.5 rounded-full bg-emerald-500 dark:bg-emerald-400" />
                      {{ sshCurrent }}
                    </div>
                  </div>

                  <div v-if="sshAliases.length === 0" class="text-center py-6">
                    <div class="text-xs text-zinc-400 dark:text-zinc-500 mb-1">No SSH aliases configured</div>
                    <div class="text-[10px] text-zinc-400 dark:text-zinc-600">
                      Add aliases to <span class="font-mono">~/.jcode/config.json</span>
                    </div>
                  </div>
                  <div v-else class="space-y-2">
                    <div
                      v-for="alias in sshAliases"
                      :key="alias.name"
                      class="flex items-center gap-3 px-3 py-2.5 rounded-xl border border-zinc-200 dark:border-zinc-700/60 bg-white dark:bg-zinc-800/60"
                    >
                      <span class="text-sm">🖥</span>
                      <div class="flex-1 min-w-0">
                        <div class="text-xs font-medium text-zinc-700 dark:text-zinc-200">{{ alias.name }}</div>
                        <div class="text-[10px] text-zinc-400 dark:text-zinc-500 font-mono truncate">
                          {{ alias.addr }}
                          <template v-if="alias.path"> · {{ alias.path }}</template>
                        </div>
                      </div>
                      <span
                        v-if="sshCurrent === alias.name"
                        class="text-[10px] px-1.5 py-0.5 rounded-full bg-emerald-100 dark:bg-emerald-500/15 text-emerald-700 dark:text-emerald-400"
                      >
                        active
                      </span>
                    </div>
                  </div>
                </div>

                <!-- Channels tab -->
                <div v-if="activeTab === 'channels'">
                  <div class="text-[10px] text-zinc-400 dark:text-zinc-500 uppercase tracking-wider mb-3 font-medium">Notification Channels</div>

                  <div v-if="!channelAvailable" class="text-center py-8">
                    <div class="text-xs text-zinc-400 dark:text-zinc-500 mb-1">No channels configured</div>
                    <div class="text-[10px] text-zinc-400 dark:text-zinc-600">
                      Set <span class="font-mono">channel.web_enabled: true</span> in <span class="font-mono">~/.jcode/config.json</span>
                    </div>
                  </div>

                  <div v-else class="space-y-4">
                    <div class="px-4 py-3 rounded-xl border border-zinc-200 dark:border-zinc-700/60 bg-white dark:bg-zinc-800/60">
                      <div class="flex items-center justify-between mb-3">
                        <div class="flex items-center gap-2">
                          <span class="text-base">💬</span>
                          <div>
                            <div class="text-xs font-medium text-zinc-700 dark:text-zinc-200">WeChat</div>
                            <div class="text-[10px] text-zinc-400 dark:text-zinc-500">iLink Bot integration</div>
                          </div>
                        </div>
                        <div class="flex items-center gap-1.5">
                          <span
                            class="w-1.5 h-1.5 rounded-full"
                            :class="{
                              'bg-emerald-400': channelState === 'enabled',
                              'bg-amber-400': channelState === 'disabled' || channelState === 'scanning',
                              'bg-zinc-300 dark:bg-zinc-600': channelState === 'none',
                            }"
                          />
                          <span class="text-[10px] font-medium" :class="{
                            'text-emerald-600 dark:text-emerald-400': channelState === 'enabled',
                            'text-amber-600 dark:text-amber-400': channelState === 'disabled' || channelState === 'scanning',
                            'text-zinc-400 dark:text-zinc-500': channelState === 'none',
                          }">
                            {{ channelState === 'enabled' ? 'Connected' : channelState === 'disabled' ? 'Disconnected' : channelState === 'scanning' ? 'Scanning...' : 'Not configured' }}
                          </span>
                        </div>
                      </div>

                      <div v-if="channelQRContent" class="flex flex-col items-center py-3">
                        <canvas ref="qrCanvas" class="rounded-xl border border-zinc-200 dark:border-zinc-700" />
                        <div class="text-[10px] text-zinc-400 dark:text-zinc-500 mt-2">Scan with WeChat to connect</div>
                      </div>

                      <div class="flex gap-2 mt-2">
                        <button
                          v-if="channelState === 'none'"
                          :disabled="channelLoading"
                          class="flex-1 px-3 py-1.5 text-xs rounded-xl bg-emerald-500 text-white hover:bg-emerald-600 disabled:opacity-50 cursor-pointer transition-colors font-medium"
                          @click="channelLogin"
                        >
                          {{ channelLoading ? 'Loading...' : 'Connect' }}
                        </button>
                        <button
                          v-if="channelState === 'enabled' || channelState === 'disabled'"
                          :disabled="channelLoading"
                          class="flex-1 px-3 py-1.5 text-xs rounded-xl text-red-500 dark:text-red-400 hover:bg-red-50 dark:hover:bg-red-500/10 disabled:opacity-50 cursor-pointer transition-colors font-medium"
                          @click="channelLogout"
                        >
                          Disconnect
                        </button>
                      </div>
                    </div>

                    <!-- Login reminder banner -->
                    <div
                      v-if="channelLoginReminder"
                      class="px-4 py-3 rounded-xl border border-amber-200 dark:border-amber-500/30 bg-amber-50 dark:bg-amber-500/10 flex items-start gap-2.5"
                    >
                      <span class="text-sm shrink-0 mt-0.5">⚠️</span>
                      <div class="flex-1 min-w-0">
                        <div class="text-xs font-medium text-amber-700 dark:text-amber-400">Send a message to activate</div>
                        <div class="text-[10px] text-amber-600 dark:text-amber-400/80 mt-0.5 leading-relaxed">
                          Please send any message to the WeChat bot now to activate notifications. Once activated, you can receive notifications for 24 hours.
                        </div>
                      </div>
                      <button
                        class="text-amber-400 hover:text-amber-600 dark:text-amber-500 dark:hover:text-amber-300 shrink-0 cursor-pointer"
                        @click="channelLoginReminder = false"
                      >✕</button>
                    </div>

                    <div class="text-[10px] text-zinc-400 dark:text-zinc-500 leading-relaxed">
                      When connected, jcode sends approval requests and task completion notifications to your WeChat.
                    </div>
                  </div>
                </div>

                <!-- Shortcuts tab -->
                <div v-if="activeTab === 'shortcuts'">
                  <div class="text-[10px] text-zinc-400 dark:text-zinc-500 uppercase tracking-wider mb-3 font-medium">Keyboard Shortcuts</div>
                  <div class="space-y-1.5">
                    <div
                      v-for="s in shortcuts"
                      :key="s.keys"
                      class="flex items-center justify-between py-1.5 px-2 rounded-lg hover:bg-zinc-50 dark:hover:bg-zinc-800"
                    >
                      <span class="text-xs text-zinc-600 dark:text-zinc-300">{{ s.desc }}</span>
                      <kbd class="px-2 py-0.5 text-[10px] font-mono bg-zinc-100 dark:bg-zinc-800 border border-zinc-200 dark:border-zinc-700 rounded-lg text-zinc-500 dark:text-zinc-400">{{ s.keys }}</kbd>
                    </div>
                  </div>
                </div>
              </div>
            </div>

            <div class="px-5 py-3 border-t border-zinc-200 dark:border-zinc-800 flex justify-end">
              <button
                class="px-3 py-1.5 text-xs text-zinc-500 dark:text-zinc-400 hover:text-zinc-700 dark:hover:text-zinc-200 cursor-pointer transition-colors font-medium"
                @click="emit('close')">
                Done
              </button>
            </div>
          </DialogPanel>
        </TransitionChild>
      </div>
    </Dialog>
  </TransitionRoot>
</template>
