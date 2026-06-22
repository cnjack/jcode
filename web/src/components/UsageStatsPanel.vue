<script setup lang="ts">
import { computed, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { storeToRefs } from 'pinia'
import { useUsageStore } from '@/stores/usage'
import type { UsageDayBucket } from '@/types/api'

const { t, locale } = useI18n()
const usage = useUsageStore()
const { stats, loading, error, rangeDays } = storeToRefs(usage)

onMounted(() => {
  if (!stats.value) usage.fetchStats()
})

// --- formatting --------------------------------------------------------------

function fmtCompact(n: number): string {
  try {
    return new Intl.NumberFormat(locale.value, { notation: 'compact', maximumFractionDigits: 1 }).format(n)
  } catch {
    return String(n)
  }
}
function fmtFull(n: number): string {
  try {
    return new Intl.NumberFormat(locale.value).format(n)
  } catch {
    return String(n)
  }
}
function fmtPct(frac: number): string {
  return `${Math.round(frac * 100)}%`
}

// Parse a local YYYY-MM-DD without UTC shifting.
function parseLocal(s: string): Date {
  const [y = 1970, m = 1, d = 1] = s.split('-').map(Number)
  return new Date(y, m - 1, d)
}
function toKey(d: Date): string {
  const m = String(d.getMonth() + 1).padStart(2, '0')
  const day = String(d.getDate()).padStart(2, '0')
  return `${d.getFullYear()}-${m}-${day}`
}
function fmtDayLabel(s: string): string {
  try {
    return new Intl.DateTimeFormat(locale.value, { month: 'short', day: 'numeric' }).format(parseLocal(s))
  } catch {
    return s
  }
}

// --- derived cards -----------------------------------------------------------

const cacheLabel = computed(() => {
  const s = stats.value
  if (!s || !s.cache_supported) return '—'
  return fmtPct(s.cache_hit_rate)
})
const modelShare = computed(() => {
  const s = stats.value
  if (!s || !s.by_model.length) return null
  return s.by_model[0]?.share ?? null
})

// --- heatmap (53 weeks x 7 days, ending today) -------------------------------

const HEAT_WEEKS = 53
const CELL = 11
const GAP = 3
const STEP = CELL + GAP

interface HeatCell {
  date: string
  tokens: number
  turns: number
  level: number // 0-4
  future: boolean
}

const heatmap = computed(() => {
  const buckets = new Map<string, UsageDayBucket>()
  let max = 0
  for (const b of stats.value?.heatmap ?? []) {
    buckets.set(b.date, b)
    if (b.tokens > max) max = b.tokens
  }
  const today = new Date()
  today.setHours(0, 0, 0, 0)
  // Sunday of the current week is the top of the final column.
  const lastSunday = new Date(today)
  lastSunday.setDate(today.getDate() - today.getDay())
  const start = new Date(lastSunday)
  start.setDate(lastSunday.getDate() - (HEAT_WEEKS - 1) * 7)

  const cols: HeatCell[][] = []
  for (let w = 0; w < HEAT_WEEKS; w++) {
    const col: HeatCell[] = []
    for (let d = 0; d < 7; d++) {
      const cur = new Date(start)
      cur.setDate(start.getDate() + w * 7 + d)
      const key = toKey(cur)
      const b = buckets.get(key)
      const tokens = b?.tokens ?? 0
      col.push({
        date: key,
        tokens,
        turns: b?.turns ?? 0,
        level: levelFor(tokens, max),
        future: cur.getTime() > today.getTime(),
      })
    }
    cols.push(col)
  }
  return cols
})

function levelFor(tokens: number, max: number): number {
  if (tokens <= 0 || max <= 0) return 0
  // Log scale so a few huge days don't flatten everything else.
  const r = Math.log(tokens + 1) / Math.log(max + 1)
  return Math.min(4, Math.max(1, Math.ceil(r * 4)))
}

const HEAT_FILL = [
  'color-mix(in srgb, var(--color-foreground) 7%, transparent)',
  'color-mix(in srgb, var(--color-primary) 28%, transparent)',
  'color-mix(in srgb, var(--color-primary) 48%, transparent)',
  'color-mix(in srgb, var(--color-primary) 72%, transparent)',
  'var(--color-primary)',
]
const heatWidth = HEAT_WEEKS * STEP - GAP
const heatHeight = 7 * STEP - GAP

function fillFor(c: HeatCell): string {
  if (c.future) return 'transparent'
  return HEAT_FILL[c.level] ?? 'transparent'
}

function cellTitle(c: HeatCell): string {
  if (c.future) return ''
  if (c.tokens <= 0) return `${fmtDayLabel(c.date)} · ${t('settings.usageStats.noActivity')}`
  return `${fmtDayLabel(c.date)} · ${fmtCompact(c.tokens)} tokens · ${c.turns} ${t('settings.usageStats.turnsUnit')}`
}

// --- daily trend -------------------------------------------------------------

const trend = computed(() => stats.value?.daily_trend ?? [])
const trendMax = computed(() => Math.max(1, ...trend.value.map((d) => d.tokens)))
function barTitle(d: UsageDayBucket): string {
  return `${fmtDayLabel(d.date)} · ${fmtCompact(d.tokens)} tokens · ${d.turns} ${t('settings.usageStats.turnsUnit')}`
}

function setRange(days: number) {
  if (rangeDays.value === days && stats.value) return
  usage.fetchStats(days)
}
function shortName(path: string): string {
  const parts = path.replace(/\/+$/, '').split('/')
  return parts[parts.length - 1] || path
}
</script>

<template>
  <div class="space-y-5">
    <div class="flex items-center justify-between gap-3 flex-wrap">
      <div>
        <h3 class="text-[13px] font-semibold tracking-tight" style="color: var(--color-foreground)">
          {{ t('settings.usageStats.title') }}
        </h3>
        <p class="text-[11px] mt-0.5" style="color: var(--color-muted-foreground)">
          {{ t('settings.usageStats.subtitle') }}
        </p>
      </div>
      <!-- Range toggle -->
      <div class="inline-flex p-0.5 rounded-md" style="background: var(--color-secondary)">
        <button
          v-for="d in [7, 30]"
          :key="d"
          class="px-2.5 h-7 text-[12px] rounded transition-colors cursor-pointer"
          :style="rangeDays === d
            ? { background: 'var(--color-background)', color: 'var(--color-foreground)', fontWeight: '500' }
            : { background: 'transparent', color: 'var(--color-muted-foreground)' }"
          @click="setRange(d)"
        >
          {{ t('settings.usageStats.lastNDays', { n: d }) }}
        </button>
      </div>
    </div>

    <div v-if="error" class="text-[12px] px-3 py-2 rounded-md" style="background: var(--color-warning-bg); color: var(--color-warning-fg)">
      {{ error }}
    </div>

    <div v-else-if="loading && !stats" class="text-[12px] py-8 text-center" style="color: var(--color-muted-foreground)">
      {{ t('common.loading') }}
    </div>

    <template v-else-if="stats">
      <!-- Stat cards -->
      <div class="grid grid-cols-2 sm:grid-cols-3 gap-2.5">
        <div class="us-card us-card-lg">
          <div class="us-label">{{ t('settings.usageStats.totalTokens') }}</div>
          <div class="us-value" :title="fmtFull(stats.totals.total_tokens)">{{ fmtCompact(stats.totals.total_tokens) }}</div>
        </div>
        <div class="us-card us-card-lg">
          <div class="us-label">{{ t('settings.usageStats.cacheHitRate') }}</div>
          <div class="us-value" style="color: var(--color-primary)">{{ cacheLabel }}</div>
        </div>
        <div class="us-card us-card-lg">
          <div class="us-label">{{ t('settings.usageStats.mostUsedModel') }}</div>
          <div class="us-value us-value-sm" :title="stats.most_used_model">{{ stats.most_used_model || '—' }}</div>
          <div v-if="modelShare != null" class="us-sub">{{ t('settings.usageStats.share', { pct: fmtPct(modelShare) }) }}</div>
        </div>
        <div class="us-card">
          <div class="us-label">{{ t('settings.usageStats.sessions') }}</div>
          <div class="us-value">{{ fmtFull(stats.totals.sessions) }}</div>
        </div>
        <div class="us-card">
          <div class="us-label">{{ t('settings.usageStats.turns') }}</div>
          <div class="us-value">{{ fmtFull(stats.totals.turns) }}</div>
        </div>
        <div class="us-card">
          <div class="us-label">{{ t('settings.usageStats.activeDays') }}</div>
          <div class="us-value">{{ stats.active_days }}</div>
          <div class="us-sub">{{ t('settings.usageStats.streak', { n: stats.current_streak }) }}</div>
        </div>
      </div>

      <!-- Token composition strip -->
      <div class="us-panel">
        <div class="us-panel-title">{{ t('settings.usageStats.tokenBreakdown') }}</div>
        <div class="grid grid-cols-2 sm:grid-cols-4 gap-2.5 mt-2">
          <div>
            <div class="us-mini-label">{{ t('settings.usageStats.promptTokens') }}</div>
            <div class="us-mini-value">{{ fmtCompact(stats.totals.prompt_tokens) }}</div>
          </div>
          <div>
            <div class="us-mini-label">{{ t('settings.usageStats.cachedTokens') }}</div>
            <div class="us-mini-value">{{ fmtCompact(stats.totals.cached_tokens) }}</div>
          </div>
          <div>
            <div class="us-mini-label">{{ t('settings.usageStats.completionTokens') }}</div>
            <div class="us-mini-value">{{ fmtCompact(stats.totals.completion_tokens) }}</div>
          </div>
          <div>
            <div class="us-mini-label">{{ t('settings.usageStats.reasoningTokens') }}</div>
            <div class="us-mini-value">{{ fmtCompact(stats.totals.reasoning_tokens) }}</div>
          </div>
        </div>
        <div class="us-mini-hint">{{ t('settings.usageStats.tokenBreakdownHint') }}</div>
      </div>

      <!-- Activity heatmap -->
      <div class="us-panel">
        <div class="flex items-center justify-between">
          <div class="us-panel-title">{{ t('settings.usageStats.heatmap') }}</div>
          <div class="flex items-center gap-1 text-[10px]" style="color: var(--color-muted-foreground)">
            <span>{{ t('settings.usageStats.less') }}</span>
            <span v-for="(f, i) in HEAT_FILL" :key="i" class="inline-block rounded-[2px]" :style="{ width: '10px', height: '10px', background: f }" />
            <span>{{ t('settings.usageStats.more') }}</span>
          </div>
        </div>
        <div class="overflow-x-auto mt-2 pb-1">
          <svg :viewBox="`0 0 ${heatWidth} ${heatHeight}`" :width="heatWidth" :height="heatHeight" role="img" :aria-label="t('settings.usageStats.heatmap')">
            <template v-for="(col, w) in heatmap" :key="w">
              <rect
                v-for="(c, d) in col"
                :key="d"
                :x="w * STEP"
                :y="d * STEP"
                :width="CELL"
                :height="CELL"
                rx="2"
                :fill="fillFor(c)"
              >
                <title>{{ cellTitle(c) }}</title>
              </rect>
            </template>
          </svg>
        </div>
      </div>

      <!-- Daily trend -->
      <div class="us-panel">
        <div class="us-panel-title">{{ t('settings.usageStats.dailyTrend') }}</div>
        <div v-if="trend.length" class="flex items-end gap-[3px] mt-3" style="height: 120px">
          <div
            v-for="d in trend"
            :key="d.date"
            class="flex-1 rounded-t-[2px] transition-[height] min-w-[2px]"
            :style="{ height: `${Math.max(2, (d.tokens / trendMax) * 100)}%`, background: 'var(--accent-fill)' }"
            :title="barTitle(d)"
          />
        </div>
        <div v-else class="text-[11px] py-6 text-center" style="color: var(--color-muted-foreground)">
          {{ t('settings.usageStats.noData') }}
        </div>
      </div>

      <!-- Breakdown bars -->
      <div class="grid grid-cols-1 sm:grid-cols-2 gap-3">
        <div class="us-panel">
          <div class="us-panel-title">{{ t('settings.usageStats.byModel') }}</div>
          <div class="space-y-2 mt-2">
            <div v-for="m in stats.by_model.slice(0, 6)" :key="m.name" class="us-bar-row">
              <div class="flex items-center justify-between text-[11px] mb-1">
                <span class="truncate" style="color: var(--color-foreground)">{{ m.name }}</span>
                <span style="color: var(--color-muted-foreground)">{{ fmtCompact(m.tokens) }}</span>
              </div>
              <div class="us-bar-track"><div class="us-bar-fill" :style="{ width: fmtPct(m.share) }" /></div>
            </div>
            <div v-if="!stats.by_model.length" class="us-empty">{{ t('settings.usageStats.noData') }}</div>
          </div>
        </div>
        <div class="us-panel">
          <div class="us-panel-title">{{ t('settings.usageStats.byProject') }}</div>
          <div class="space-y-2 mt-2">
            <div v-for="p in stats.by_project.slice(0, 6)" :key="p.name" class="us-bar-row">
              <div class="flex items-center justify-between text-[11px] mb-1">
                <span class="truncate" style="color: var(--color-foreground)" :title="p.name">{{ shortName(p.name) }}</span>
                <span style="color: var(--color-muted-foreground)">{{ fmtCompact(p.tokens) }}</span>
              </div>
              <div class="us-bar-track"><div class="us-bar-fill" :style="{ width: fmtPct(p.share) }" /></div>
            </div>
            <div v-if="!stats.by_project.length" class="us-empty">{{ t('settings.usageStats.noData') }}</div>
          </div>
        </div>
      </div>
    </template>
  </div>
</template>

<style scoped>
.us-card {
  border-radius: var(--radius-md);
  background: var(--color-secondary);
  padding: 12px 14px;
}
.us-label {
  font-size: 11px;
  color: var(--color-muted-foreground);
  margin-bottom: 4px;
}
.us-value {
  font-size: 24px;
  font-weight: 600;
  line-height: 1.1;
  color: var(--color-foreground);
}
.us-value-sm {
  font-size: 16px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.us-sub {
  font-size: 10px;
  color: var(--color-muted-foreground);
  margin-top: 4px;
}
.us-panel {
  border-radius: var(--radius-md);
  background: var(--color-secondary);
  padding: 14px;
}
.us-panel-title {
  font-size: 12px;
  font-weight: 600;
  color: var(--color-foreground);
}
.us-mini-label {
  font-size: 10px;
  color: var(--color-muted-foreground);
}
.us-mini-value {
  font-size: 15px;
  font-weight: 600;
  color: var(--color-foreground);
  margin-top: 2px;
}
.us-mini-hint {
  margin-top: 10px;
  font-size: 10px;
  line-height: 1.4;
  color: var(--color-muted-foreground);
}
.us-bar-track {
  height: 6px;
  border-radius: 3px;
  background: color-mix(in srgb, var(--color-foreground) 8%, transparent);
  overflow: hidden;
}
.us-bar-fill {
  height: 100%;
  border-radius: 3px;
  background: var(--color-primary);
}
.us-empty {
  font-size: 11px;
  color: var(--color-muted-foreground);
  padding: 8px 0;
}
</style>
