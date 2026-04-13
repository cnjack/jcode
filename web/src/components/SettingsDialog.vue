<script setup lang="ts">
import { ref, watch } from 'vue'
import { useChatStore } from '@/stores/chat'
import { api } from '@/composables/api'
import type { MCPServerInfo } from '@/types/api'
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
const activeTab = ref<'general' | 'mcp'>('general')
const mcpServers = ref<Record<string, MCPServerInfo>>({})

watch(() => props.open, async (isOpen) => {
  if (isOpen) {
    try {
      const result = await api.mcpList()
      mcpServers.value = result.servers
    } catch {
      // ignore
    }
  }
})

function serverIcon(type: string) {
  return type === 'sse' || type === 'http' ? '🌐' : '⚡'
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
        <div class="fixed inset-0 bg-black/20" />
      </TransitionChild>

      <div class="fixed inset-0 flex items-start justify-center pt-20 px-4">
        <TransitionChild
          enter="ease-out duration-150"
          enter-from="opacity-0 translate-y-2"
          enter-to="opacity-100 translate-y-0"
          leave="ease-in duration-100"
          leave-from="opacity-100 translate-y-0"
          leave-to="opacity-0 translate-y-2">
          <DialogPanel class="w-full max-w-md bg-white border border-stone-200 rounded-xl shadow-xl overflow-hidden">
            <div class="px-5 pt-5 pb-3">
              <DialogTitle class="text-sm font-semibold text-stone-800">Settings</DialogTitle>
            </div>

            <!-- Tabs -->
            <div class="flex mx-5 border-b border-stone-200">
              <button
                v-for="tab in (['general', 'mcp'] as const)"
                :key="tab"
                class="pb-2 px-3 text-[11px] capitalize transition-colors cursor-pointer"
                :class="activeTab === tab
                  ? 'text-stone-800 border-b-2 border-teal-500'
                  : 'text-stone-400 hover:text-stone-600'"
                @click="activeTab = tab"
              >
                {{ tab === 'mcp' ? 'MCP Servers' : 'General' }}
              </button>
            </div>

            <!-- General tab -->
            <div v-if="activeTab === 'general'" class="p-5 space-y-3">
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
                <div class="text-xs font-mono text-stone-600">{{ store.mode }}</div>
              </div>
              <div>
                <div class="text-[10px] text-stone-400 uppercase tracking-wider mb-0.5">Workspace</div>
                <div class="text-xs font-mono text-stone-500 break-all">{{ store.pwd || '—' }}</div>
              </div>
              <div v-if="store.tokenInfo">
                <div class="text-[10px] text-stone-400 uppercase tracking-wider mb-0.5">Token Usage</div>
                <div class="text-xs font-mono text-stone-600">
                  {{ store.tokenInfo.total_tokens.toLocaleString() }}
                  <span v-if="store.tokenInfo.model_context_limit"> / {{ store.tokenInfo.model_context_limit.toLocaleString() }}</span>
                </div>
              </div>
            </div>

            <!-- MCP tab -->
            <div v-if="activeTab === 'mcp'" class="p-5">
              <div v-if="Object.keys(mcpServers).length === 0" class="text-center py-4">
                <div class="text-xs text-stone-400">No MCP servers configured</div>
                <div class="text-[10px] text-stone-400 mt-1">Edit <span class="font-mono">~/.jcode/config.json</span></div>
              </div>
              <div v-else class="space-y-2">
                <div
                  v-for="(info, name) in mcpServers"
                  :key="name"
                  class="flex items-center gap-3 px-3 py-2 rounded-lg border border-stone-200 bg-stone-50"
                >
                  <span class="text-sm">{{ serverIcon(info.type) }}</span>
                  <div class="flex-1 min-w-0">
                    <div class="text-xs font-medium text-stone-700">{{ name }}</div>
                    <div class="text-[10px] text-stone-400 font-mono truncate">
                      {{ info.type === 'sse' || info.type === 'http' ? info.url : info.command }}
                    </div>
                  </div>
                  <span
                    class="text-[10px] px-1.5 py-0.5 rounded-full bg-emerald-100 text-emerald-700"
                  >
                    {{ info.status }}
                  </span>
                </div>
              </div>
            </div>

            <div class="px-5 pb-4 flex justify-end">
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
