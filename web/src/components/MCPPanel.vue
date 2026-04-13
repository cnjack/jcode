<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { api } from '@/composables/api'
import type { MCPServerInfo } from '@/types/api'

const servers = ref<Record<string, MCPServerInfo>>({})
const loading = ref(false)

async function fetchMCP() {
  loading.value = true
  try {
    const result = await api.mcpList()
    servers.value = result.servers
  } catch (err) {
    console.error('Failed to fetch MCP servers:', err)
  } finally {
    loading.value = false
  }
}

async function toggleServer(name: string, enabled: boolean) {
  try {
    await api.mcpToggle(name, enabled)
    if (!enabled) {
      delete servers.value[name]
    }
  } catch (err) {
    console.error('Failed to toggle MCP server:', err)
  }
}

function serverIcon(type: string) {
  return type === 'sse' || type === 'http' ? '🌐' : '⚡'
}

onMounted(fetchMCP)
</script>

<template>
  <div class="flex flex-col h-full">
    <!-- Header -->
    <div class="flex items-center justify-between px-4 py-3 border-b border-stone-200">
      <span class="text-sm font-medium text-stone-700">MCP Servers</span>
      <button
        class="text-[11px] text-stone-400 hover:text-stone-600 cursor-pointer transition-colors"
        @click="fetchMCP"
      >
        ↻ Refresh
      </button>
    </div>

    <!-- Server list -->
    <div class="flex-1 overflow-y-auto p-3 space-y-2">
      <div v-if="loading" class="text-center text-xs text-stone-400 py-6 animate-pulse">
        Loading...
      </div>

      <div v-else-if="Object.keys(servers).length === 0" class="text-center py-8">
        <div class="text-stone-400 text-xs mb-1">No MCP servers configured</div>
        <div class="text-[10px] text-stone-400">
          Add servers to <span class="font-mono">~/.jcode/config.json</span>
        </div>
      </div>

      <div
        v-for="(info, name) in servers"
        :key="name"
        class="flex items-center gap-3 px-3 py-2.5 rounded-lg border border-stone-200 bg-white hover:border-stone-300 transition-colors"
      >
        <span class="text-base">{{ serverIcon(info.type) }}</span>
        <div class="flex-1 min-w-0">
          <div class="text-sm font-medium text-stone-700">{{ name }}</div>
          <div class="text-[10px] text-stone-400 font-mono truncate">
            {{ info.type === 'sse' || info.type === 'http' ? info.url : info.command }}
          </div>
        </div>
        <div class="flex items-center gap-2 shrink-0">
          <span
            class="text-[10px] px-1.5 py-0.5 rounded-full"
            :class="info.status === 'configured'
              ? 'bg-emerald-100 text-emerald-700'
              : 'bg-stone-100 text-stone-500'"
          >
            {{ info.status }}
          </span>
          <button
            class="w-8 h-4 rounded-full relative cursor-pointer transition-colors bg-teal-500"
            @click="toggleServer(String(name), false)"
            title="Disable server"
          >
            <span class="absolute top-0.5 right-0.5 w-3 h-3 rounded-full bg-white shadow transition-transform" />
          </button>
        </div>
      </div>
    </div>
  </div>
</template>
