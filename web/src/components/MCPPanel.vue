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
    <div class="flex items-center justify-between px-4 py-3 border-b border-zinc-200 dark:border-zinc-800">
      <span class="text-sm font-medium text-zinc-700 dark:text-zinc-200">MCP Servers</span>
      <button
        class="text-[11px] text-zinc-400 dark:text-zinc-500 hover:text-zinc-600 dark:hover:text-zinc-300 cursor-pointer transition-colors font-medium"
        @click="fetchMCP"
      >
        ↻ Refresh
      </button>
    </div>

    <!-- Server list -->
    <div class="flex-1 overflow-y-auto p-3 space-y-2">
      <div v-if="loading" class="text-center text-xs text-zinc-400 dark:text-zinc-500 py-6 animate-pulse">
        Loading...
      </div>

      <div v-else-if="Object.keys(servers).length === 0" class="text-center py-8">
        <div class="text-zinc-400 dark:text-zinc-500 text-xs mb-1">No MCP servers configured</div>
        <div class="text-[10px] text-zinc-400 dark:text-zinc-600">
          Add servers to <span class="font-mono">~/.jcode/config.json</span>
        </div>
      </div>

      <div
        v-for="(info, name) in servers"
        :key="name"
        class="flex items-center gap-3 px-3 py-2.5 rounded-md border border-zinc-200 dark:border-zinc-700/60 bg-white dark:bg-zinc-800/60 hover:border-zinc-300 dark:hover:border-zinc-600 transition-colors"
      >
        <span class="text-base">{{ serverIcon(info.type) }}</span>
        <div class="flex-1 min-w-0">
          <div class="text-sm font-medium text-zinc-700 dark:text-zinc-200">{{ name }}</div>
          <div class="text-[10px] text-zinc-400 dark:text-zinc-500 font-mono truncate">
            {{ info.type === 'sse' || info.type === 'http' ? info.url : info.command }}
          </div>
        </div>
        <div class="flex items-center gap-2 shrink-0">
          <span
            class="text-[10px] px-1.5 py-0.5 rounded-full"
            :class="info.status === 'configured'
              ? 'bg-emerald-100 dark:bg-emerald-500/15 text-emerald-700 dark:text-emerald-400'
              : 'bg-zinc-100 dark:bg-zinc-800 text-zinc-500 dark:text-zinc-400'"
          >
            {{ info.status }}
          </span>
          <button
            class="w-8 h-4 rounded-full relative cursor-pointer transition-colors bg-emerald-500"
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
