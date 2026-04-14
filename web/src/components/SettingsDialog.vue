<script setup lang="ts">
import { ref, watch } from 'vue'
import { useChatStore } from '@/stores/chat'
import { api } from '@/composables/api'
import type { MCPServerInfo, SSHAlias } from '@/types/api'
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
const activeTab = ref<'general' | 'mcp' | 'ssh' | 'shortcuts'>('general')
const mcpServers = ref<Record<string, MCPServerInfo>>({})
const sshAliases = ref<SSHAlias[]>([])
const sshCurrent = ref('local')
const mcpLoading = ref(false)

watch(() => props.open, async (isOpen) => {
  if (isOpen) {
    // Load MCP servers
    mcpLoading.value = true
    try {
      const result = await api.mcpList()
      mcpServers.value = result.servers
    } catch { /* ignore */ }
    mcpLoading.value = false

    // Load SSH aliases
    try {
      const sshData = await api.sshList()
      sshAliases.value = sshData.aliases
      sshCurrent.value = sshData.current
    } catch { /* ignore */ }
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
        <div class="fixed inset-0 bg-black/20" />
      </TransitionChild>

      <div class="fixed inset-0 flex items-start justify-center pt-16 px-4">
        <TransitionChild
          enter="ease-out duration-150"
          enter-from="opacity-0 translate-y-2"
          enter-to="opacity-100 translate-y-0"
          leave="ease-in duration-100"
          leave-from="opacity-100 translate-y-0"
          leave-to="opacity-0 translate-y-2">
          <DialogPanel class="w-2xl bg-white border border-stone-200 rounded-xl shadow-xl overflow-hidden">
            <div class="px-5 pt-5 pb-3 border-b border-stone-200">
              <DialogTitle class="text-sm font-semibold text-stone-800">Settings</DialogTitle>
            </div>

            <div class="flex h-105">
              <!-- Left sidebar -->
              <nav class="w-40 border-r border-stone-200 py-2 shrink-0">
                <button
                  v-for="tab in (['general', 'mcp', 'ssh', 'shortcuts'] as const)"
                  :key="tab"
                  class="w-full px-4 py-2 text-left text-xs transition-colors cursor-pointer"
                  :class="activeTab === tab
                    ? 'text-teal-700 bg-teal-50 font-medium'
                    : 'text-stone-500 hover:text-stone-700 hover:bg-stone-50'"
                  @click="activeTab = tab"
                >
                  {{ tab === 'mcp' ? 'MCP Servers' : tab === 'ssh' ? 'SSH' : tab === 'shortcuts' ? 'Shortcuts' : 'General' }}
                </button>
              </nav>

              <!-- Right content -->
              <div class="flex-1 overflow-y-auto p-5">
                <!-- General tab -->
                <div v-if="activeTab === 'general'" class="space-y-4">
                  <!-- Server status -->
                  <div class="flex items-center gap-2">
                    <span
                      class="w-2 h-2 rounded-full"
                      :class="store.wsConnected ? 'bg-emerald-400' : 'bg-stone-300'"
                    />
                    <span class="text-xs font-medium" :class="store.wsConnected ? 'text-emerald-600' : 'text-stone-400'">
                      Server {{ store.wsConnected ? 'Online' : 'Offline' }}
                    </span>
                  </div>

                  <div class="grid grid-cols-2 gap-4">
                    <div>
                      <div class="text-[10px] text-stone-400 uppercase tracking-wider mb-0.5">Provider</div>
                      <div class="text-xs font-mono text-stone-600">{{ store.providerName || '—' }}</div>
                    </div>
                    <div>
                      <div class="text-[10px] text-stone-400 uppercase tracking-wider mb-0.5">Model</div>
                      <div class="text-xs font-mono text-stone-600">{{ store.modelName || '—' }}</div>
                    </div>
                    <div>
                      <div class="text-[10px] text-stone-400 uppercase tracking-wider mb-0.5">Mode</div>
                      <div class="text-xs font-mono text-stone-600">{{ store.mode === 'agent' ? 'Agent' : 'Plan' }}</div>
                    </div>
                    <div>
                      <div class="text-[10px] text-stone-400 uppercase tracking-wider mb-0.5">Auto-approve</div>
                      <div class="text-xs font-mono text-stone-600">{{ store.autoApprove ? 'On' : 'Off' }}</div>
                    </div>
                  </div>

                  <div>
                    <div class="text-[10px] text-stone-400 uppercase tracking-wider mb-0.5">Workspace</div>
                    <div class="text-xs font-mono text-stone-500 break-all">{{ store.pwd || '—' }}</div>
                  </div>

                  <div v-if="store.tokenInfo">
                    <div class="text-[10px] text-stone-400 uppercase tracking-wider mb-1">Token Usage</div>
                    <div class="flex items-center gap-2">
                      <div class="flex-1 h-1.5 bg-stone-100 rounded-full overflow-hidden">
                        <div
                          class="h-full rounded-full transition-all"
                          :class="store.tokenPercentage > 80 ? 'bg-red-400' : store.tokenPercentage > 50 ? 'bg-amber-400' : 'bg-teal-400'"
                          :style="{ width: store.tokenPercentage + '%' }"
                        />
                      </div>
                      <span class="text-[10px] text-stone-500 font-mono">
                        {{ store.tokenInfo.total_tokens.toLocaleString() }}
                        <span v-if="store.tokenInfo.model_context_limit"> / {{ store.tokenInfo.model_context_limit.toLocaleString() }}</span>
                      </span>
                    </div>
                  </div>
                </div>

                <!-- MCP Servers tab -->
                <div v-if="activeTab === 'mcp'">
                  <div class="text-[10px] text-stone-400 uppercase tracking-wider mb-3">MCP Servers</div>
                  <div v-if="mcpLoading" class="text-center text-xs text-stone-400 py-6 animate-pulse">
                    Loading...
                  </div>
                  <div v-else-if="Object.keys(mcpServers).length === 0" class="text-center py-8">
                    <div class="text-xs text-stone-400 mb-1">No MCP servers configured</div>
                    <div class="text-[10px] text-stone-400">
                      Edit <span class="font-mono">~/.jcode/config.json</span>
                    </div>
                  </div>
                  <div v-else class="space-y-2">
                    <div
                      v-for="(info, name) in mcpServers"
                      :key="name"
                      class="flex items-center gap-3 px-3 py-2.5 rounded-lg border border-stone-200"
                      :class="info.enabled ? 'bg-white' : 'bg-stone-50 opacity-60'"
                    >
                      <span class="text-sm">{{ serverIcon(info.type) }}</span>
                      <div class="flex-1 min-w-0">
                        <div class="text-xs font-medium text-stone-700">{{ name }}</div>
                        <div class="text-[10px] text-stone-400 font-mono truncate">
                          {{ info.type === 'sse' || info.type === 'http' ? info.url : info.command }}
                        </div>
                      </div>
                      <!-- Toggle switch -->
                      <button
                        class="relative inline-flex h-5 w-9 items-center rounded-full cursor-pointer transition-colors shrink-0"
                        :class="info.enabled ? 'bg-teal-500' : 'bg-stone-300'"
                        @click="toggleMCP(String(name), !info.enabled)"
                        :title="info.enabled ? 'Disable' : 'Enable'"
                      >
                        <span
                          class="inline-block h-3.5 w-3.5 rounded-full bg-white shadow-sm transition-transform"
                          :class="info.enabled ? 'translate-x-4.5' : 'translate-x-0.75'"
                        />
                      </button>
                    </div>
                  </div>
                </div>

                <!-- SSH tab -->
                <div v-if="activeTab === 'ssh'">
                  <div class="text-[10px] text-stone-400 uppercase tracking-wider mb-3">SSH Environments</div>

                  <div class="mb-3">
                    <div class="text-[10px] text-stone-400 mb-1">Current Environment</div>
                    <div class="inline-flex items-center gap-1.5 px-2 py-1 rounded-md bg-teal-50 text-xs text-teal-700 font-medium">
                      <span class="w-1.5 h-1.5 rounded-full bg-teal-500" />
                      {{ sshCurrent }}
                    </div>
                  </div>

                  <div v-if="sshAliases.length === 0" class="text-center py-6">
                    <div class="text-xs text-stone-400 mb-1">No SSH aliases configured</div>
                    <div class="text-[10px] text-stone-400">
                      Add aliases to <span class="font-mono">~/.jcode/config.json</span>
                    </div>
                  </div>
                  <div v-else class="space-y-2">
                    <div
                      v-for="alias in sshAliases"
                      :key="alias.name"
                      class="flex items-center gap-3 px-3 py-2.5 rounded-lg border border-stone-200 bg-white"
                    >
                      <span class="text-sm">🖥</span>
                      <div class="flex-1 min-w-0">
                        <div class="text-xs font-medium text-stone-700">{{ alias.name }}</div>
                        <div class="text-[10px] text-stone-400 font-mono truncate">
                          {{ alias.addr }}
                          <template v-if="alias.path"> · {{ alias.path }}</template>
                        </div>
                      </div>
                      <span
                        v-if="sshCurrent === alias.name"
                        class="text-[10px] px-1.5 py-0.5 rounded-full bg-teal-100 text-teal-700"
                      >
                        active
                      </span>
                    </div>
                  </div>
                </div>

                <!-- Shortcuts tab -->
                <div v-if="activeTab === 'shortcuts'">
                  <div class="text-[10px] text-stone-400 uppercase tracking-wider mb-3">Keyboard Shortcuts</div>
                  <div class="space-y-1.5">
                    <div
                      v-for="s in shortcuts"
                      :key="s.keys"
                      class="flex items-center justify-between py-1.5 px-2 rounded hover:bg-stone-50"
                    >
                      <span class="text-xs text-stone-600">{{ s.desc }}</span>
                      <kbd class="px-2 py-0.5 text-[10px] font-mono bg-stone-100 border border-stone-200 rounded text-stone-500">{{ s.keys }}</kbd>
                    </div>
                  </div>
                </div>
              </div>
            </div>

            <div class="px-5 py-3 border-t border-stone-200 flex justify-end">
              <button
                class="px-3 py-1.5 text-xs text-stone-500 hover:text-stone-700 cursor-pointer transition-colors"
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
