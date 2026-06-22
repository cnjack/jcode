<script setup lang="ts">
import { computed, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { storeToRefs } from 'pinia'
import { useChatStore } from '@/stores/chat'
import { useUsageStore } from '@/stores/usage'

const { t, locale } = useI18n()
const chat = useChatStore()
const usage = useUsageStore()
const { taskStats, taskLoading } = storeToRefs(usage)

onMounted(() => {
  if (chat.currentSessionId) usage.fetchTaskStats(chat.currentSessionId)
})

function fmtCompact(n: number): string {
  try {
    return new Intl.NumberFormat(locale.value, { notation: 'compact', maximumFractionDigits: 1 }).format(n)
  } catch {
    return String(n)
  }
}
function fmtPct(frac: number): string {
  return `${(frac * 100).toFixed(1)}%`
}

// Bucket palette: an accent gradient for the model-driven parts, a neutral for
// the conversation-independent system prompt. Keeps the theme's orange identity.
const BUCKETS = [
  { key: 'messages', field: 'messages_tokens', color: 'color-mix(in srgb, var(--color-primary) 90%, transparent)' },
  { key: 'systemTools', field: 'system_tools_tokens', color: 'color-mix(in srgb, var(--color-primary) 60%, transparent)' },
  { key: 'mcpTools', field: 'mcp_tools_tokens', color: 'color-mix(in srgb, var(--color-primary) 38%, transparent)' },
  { key: 'skills', field: 'skills_tokens', color: 'color-mix(in srgb, var(--color-primary) 22%, transparent)' },
  { key: 'systemPrompt', field: 'system_prompt_tokens', color: 'color-mix(in srgb, var(--color-foreground) 30%, transparent)' },
] as const

const FREE_COLOR = 'color-mix(in srgb, var(--color-foreground) 7%, transparent)'

// model_context_limit comes live over the WS; fall back to the per-task fetch.
const limit = computed(() => chat.tokenInfo?.model_context_limit ?? taskStats.value?.context?.context_limit ?? 0)
const hasWindow = computed(() => limit.value > 0)

const rawRows = computed(() => {
  const ctx = taskStats.value?.context
  if (!ctx) return []
  return BUCKETS.map((b) => ({
    key: b.key,
    color: b.color,
    tokens: (ctx as unknown as Record<string, number>)[b.field] ?? 0,
  })).filter((r) => r.tokens > 0)
})

const usedTokens = computed(() => rawRows.value.reduce((s, r) => s + r.tokens, 0))
const freeTokens = computed(() => Math.max(0, limit.value - usedTokens.value))

// Fractions are of the FULL context window when the window is known (matching
// the canonical "context window" view), otherwise of the used total so the bar
// still reads on models with no published limit.
const denom = computed(() => (hasWindow.value ? limit.value : usedTokens.value || 1))
const rows = computed(() => rawRows.value.map((r) => ({ ...r, frac: r.tokens / denom.value })))
const freeFrac = computed(() => (hasWindow.value ? freeTokens.value / denom.value : 0))
const usedPct = computed(() => (hasWindow.value ? Math.round((usedTokens.value / limit.value) * 100) : 0))

// Cumulative tokens this conversation has consumed (input+output across turns).
const sessionTotal = computed(() => taskStats.value?.tokens?.total_tokens ?? 0)

const cachePct = computed<number | null>(() => {
  const live = chat.cacheHitPercentage
  if (live != null) return live
  const ts = taskStats.value
  if (ts && ts.cache_supported) return Math.round(ts.cache_hit_rate * 100)
  return null
})
</script>

<template>
  <div class="ctx-popup" @click.stop>
    <div class="flex items-baseline justify-between gap-3">
      <span class="text-[12px] font-semibold" style="color: var(--color-foreground)">{{ t('contextCapacity.title') }}</span>
      <span class="text-[11px] tabular-nums" style="color: var(--color-muted-foreground)">
        {{ fmtCompact(usedTokens) }}<span v-if="hasWindow"> / {{ fmtCompact(limit) }}</span>
        <span v-if="usedPct"> · {{ usedPct }}%</span>
      </span>
    </div>

    <!-- Stacked composition bar over the free-space track -->
    <div v-if="rows.length" class="ctx-bar mt-2.5" :style="{ background: FREE_COLOR }">
      <div
        v-for="seg in rows"
        :key="seg.key"
        class="ctx-seg"
        :style="{ width: `${seg.frac * 100}%`, background: seg.color }"
        :title="`${t('contextCapacity.' + seg.key)} · ${fmtCompact(seg.tokens)} · ${fmtPct(seg.frac)}`"
      />
    </div>

    <!-- Per-category rows: absolute tokens + share of the context window -->
    <div v-if="rows.length" class="mt-2.5 space-y-1">
      <div v-for="seg in rows" :key="seg.key" class="flex items-center gap-2 text-[11px]">
        <span class="inline-block w-2.5 h-2.5 rounded-[3px] shrink-0" :style="{ background: seg.color }" />
        <span class="flex-1 truncate" style="color: var(--color-foreground)">{{ t('contextCapacity.' + seg.key) }}</span>
        <span class="tabular-nums" style="color: var(--color-muted-foreground)">{{ fmtCompact(seg.tokens) }}</span>
        <span class="w-12 text-right tabular-nums" style="color: var(--color-muted-foreground)">{{ fmtPct(seg.frac) }}</span>
      </div>
      <!-- Free space, like the canonical context-window view -->
      <div v-if="hasWindow" class="flex items-center gap-2 text-[11px]">
        <span class="inline-block w-2.5 h-2.5 rounded-[3px] shrink-0" :style="{ background: FREE_COLOR }" />
        <span class="flex-1" style="color: var(--color-muted-foreground)">{{ t('contextCapacity.freeSpace') }}</span>
        <span class="tabular-nums" style="color: var(--color-muted-foreground)">{{ fmtCompact(freeTokens) }}</span>
        <span class="w-12 text-right tabular-nums" style="color: var(--color-muted-foreground)">{{ fmtPct(freeFrac) }}</span>
      </div>
    </div>

    <div v-else-if="taskLoading" class="mt-2 text-[11px]" style="color: var(--color-muted-foreground)">
      {{ t('common.loading') }}
    </div>

    <div class="ctx-divider" />

    <!-- Conversation-level totals -->
    <div class="flex items-center justify-between text-[11px]">
      <span style="color: var(--color-muted-foreground)">{{ t('contextCapacity.cacheHitRate') }}</span>
      <span class="font-semibold tabular-nums" :style="{ color: cachePct != null ? 'var(--color-primary)' : 'var(--color-muted-foreground)' }">
        {{ cachePct != null ? cachePct + '%' : '—' }}
      </span>
    </div>
    <div v-if="sessionTotal > 0" class="flex items-center justify-between text-[11px] mt-1.5">
      <span style="color: var(--color-muted-foreground)">{{ t('contextCapacity.sessionTotal') }}</span>
      <span class="tabular-nums" style="color: var(--color-foreground)">{{ fmtCompact(sessionTotal) }}</span>
    </div>

    <div class="mt-2 text-[10px]" style="color: var(--color-muted-foreground)">{{ t('contextCapacity.estimated') }}</div>
  </div>
</template>

<style scoped>
.ctx-popup {
  width: 290px;
  padding: 12px 14px;
  border-radius: var(--radius-md);
  background: var(--color-background);
  border: 1px solid var(--color-border);
  box-shadow: var(--elevation-popover, 0 8px 24px rgba(0, 0, 0, 0.16));
}
.ctx-bar {
  display: flex;
  height: 10px;
  width: 100%;
  border-radius: 5px;
  overflow: hidden;
}
.ctx-seg {
  height: 100%;
}
.ctx-seg + .ctx-seg {
  border-left: 1px solid var(--color-background);
}
.ctx-divider {
  height: 1px;
  background: var(--color-border);
  margin: 10px 0;
}
</style>
