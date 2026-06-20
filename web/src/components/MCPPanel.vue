<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { Globe, Zap, RefreshCw } from 'lucide-vue-next'
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

function isNetworked(type: string) {
  return type === 'sse' || type === 'http'
}

onMounted(fetchMCP)
</script>

<template>
  <div class="mcp-panel">
    <!-- Header -->
    <div class="mcp-head">
      <span class="mcp-title">MCP Servers</span>
      <button class="mcp-refresh" @click="fetchMCP">
        <RefreshCw :size="13" /> Refresh
      </button>
    </div>

    <!-- Server list -->
    <div class="mcp-list">
      <div v-if="loading" class="mcp-state animate-pulse">Loading…</div>

      <div v-else-if="Object.keys(servers).length === 0" class="mcp-empty">
        <div class="mcp-empty-title">No MCP servers configured</div>
        <div class="mcp-empty-hint">
          Add servers to <span class="font-mono">~/.jcode/config.json</span>
        </div>
      </div>

      <div v-for="(info, name) in servers" :key="name" class="mcp-card">
        <component :is="isNetworked(info.type) ? Globe : Zap" :size="16" class="mcp-icon" />
        <div class="flex-1 min-w-0">
          <div class="mcp-name">{{ name }}</div>
          <div class="mcp-detail">
            {{ isNetworked(info.type) ? info.url : info.command }}
          </div>
        </div>
        <div class="flex items-center gap-2 shrink-0">
          <span class="mcp-badge" :class="info.status === 'configured' ? 'ok' : 'muted'">
            {{ info.status }}
          </span>
          <button class="mcp-toggle" @click="toggleServer(String(name), false)" title="Disable server">
            <span class="mcp-knob" />
          </button>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.mcp-panel {
  display: flex;
  flex-direction: column;
  height: 100%;
}
.mcp-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 12px 16px;
  border-bottom: 1px solid var(--color-border);
}
.mcp-title {
  font-size: 13px;
  font-weight: 600;
  color: var(--color-foreground);
}
.mcp-refresh {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  font-size: 11px;
  font-weight: 500;
  color: var(--color-muted-foreground);
  background: transparent;
  border: none;
  cursor: pointer;
  transition: color 0.15s;
}
.mcp-refresh:hover {
  color: var(--color-foreground);
}
.mcp-list {
  flex: 1;
  overflow-y: auto;
  padding: 12px;
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.mcp-state,
.mcp-empty {
  text-align: center;
  font-size: 12px;
  color: var(--color-muted-foreground);
  padding: 24px 0;
}
.mcp-empty-title {
  margin-bottom: 4px;
}
.mcp-empty-hint {
  font-size: 10px;
  color: var(--color-text-muted, var(--color-muted-foreground));
}
.mcp-card {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 10px 12px;
  border-radius: var(--radius-lg);
  border: 1px solid var(--color-border);
  background: var(--color-surface);
  transition: border-color 0.15s;
}
.mcp-card:hover {
  border-color: color-mix(in srgb, var(--color-foreground) 24%, transparent);
}
.mcp-icon {
  color: var(--color-muted-foreground);
  flex-shrink: 0;
}
.mcp-name {
  font-size: 13px;
  font-weight: 500;
  color: var(--color-foreground);
}
.mcp-detail {
  font-size: 10px;
  font-family: var(--font-mono);
  color: var(--color-muted-foreground);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.mcp-badge {
  font-size: 10px;
  padding: 2px 7px;
  border-radius: var(--radius-pill);
}
.mcp-badge.ok {
  background: var(--color-success-bg);
  color: var(--color-success-fg);
}
.mcp-badge.muted {
  background: var(--color-muted);
  color: var(--color-muted-foreground);
}
.mcp-toggle {
  width: 32px;
  height: 18px;
  border-radius: var(--radius-pill);
  position: relative;
  cursor: pointer;
  border: none;
  background: var(--color-success);
  transition: background 0.15s;
}
.mcp-knob {
  position: absolute;
  top: 2px;
  right: 2px;
  width: 14px;
  height: 14px;
  border-radius: 50%;
  background: #fff;
  box-shadow: var(--shadow-sm);
}
</style>
