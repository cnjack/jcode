/**
 * SettingsDialog — React port of web/src/components/SettingsDialog.vue.
 *
 * Modal overlay with a left tab nav + right scrollable content panel. Tabs:
 * Providers (full CRUD + catalog + advanced config), Models (state/favorites/
 * effort), MCP (servers CRUD + OAuth login), Skills (enable/disable), Appearance
 * (theme picker), Browser (config + site permissions), Remote (SSH aliases),
 * Usage (stats).
 *
 * The Providers tab is the most complete port: list of provider cards, inline
 * add/edit form with advanced fields (base_url, headers, vision, thinking,
 * reasoning_effort), browsable model catalog with add/remove/toggle, and an
 * inline custom-model authoring form. Other tabs are functional CRUD ports of
 * the Vue logic; a few polish items are marked TODO.
 *
 * State is per-tab (each tab remounts on activation, so switching tabs naturally
 * abandons in-progress sub-flows — mirroring the Vue `watch(activeTab)` reset).
 */

import { useEffect, useRef, useState } from 'react'
import {
  Cog6ToothIcon,
  XMarkIcon,
  PlusIcon,
  TrashIcon,
  ChevronDownIcon,
  PencilSquareIcon,
  CpuChipIcon,
  ServerStackIcon,
  SparklesIcon,
  GlobeAltIcon,
  CommandLineIcon,
  SwatchIcon,
  ChartBarIcon,
  CheckIcon,
  ServerIcon,
  BoltIcon,
  ComputerDesktopIcon,
} from '@heroicons/react/24/outline'
import { useAppDispatch, useAppSelector } from '../app/hooks'
import { uiActions, modelActions } from '../app/store'
import { api } from '../lib/api'
import type { BrowserConfig, BrowserStatusResponse, BrowserSitePermission } from '../lib/api'
import type {
  ProviderDetail,
  CustomModelDetail,
  CatalogModel,
  MCPServerInfo,
  MCPServerRequest,
  SkillInfo,
  SSHListResponse,
  UsageStats,
  ModelStateResponse,
  SetupProvider,
  ModelInfo,
} from '../lib/types'

// ─── tab config ────────────────────────────────────────────────────────────

type TabId = 'providers' | 'models' | 'mcp' | 'skills' | 'appearance' | 'browser' | 'remote' | 'usage'

const TABS: { id: TabId; label: string; Icon: React.ComponentType<{ className?: string }> }[] = [
  { id: 'providers', label: 'Providers', Icon: CpuChipIcon },
  { id: 'models', label: 'Models', Icon: Cog6ToothIcon },
  { id: 'mcp', label: 'MCP', Icon: ServerStackIcon },
  { id: 'skills', label: 'Skills', Icon: SparklesIcon },
  { id: 'appearance', label: 'Appearance', Icon: SwatchIcon },
  { id: 'browser', label: 'Browser', Icon: GlobeAltIcon },
  { id: 'remote', label: 'Remote', Icon: CommandLineIcon },
  { id: 'usage', label: 'Usage', Icon: ChartBarIcon },
]

const THEMES: { id: string; label: string; appearance: 'dark' | 'light' }[] = [
  { id: 'jcode-dark', label: 'jcode Dark', appearance: 'dark' },
  { id: 'jcode-light', label: 'jcode Light', appearance: 'light' },
  { id: 'midnight', label: 'Midnight', appearance: 'dark' },
  { id: 'dracula', label: 'Dracula', appearance: 'dark' },
  { id: 'nord-dark', label: 'Nord Dark', appearance: 'dark' },
  { id: 'github-light', label: 'GitHub Light', appearance: 'light' },
  { id: 'solarized-light', label: 'Solarized Light', appearance: 'light' },
]

// ─── shared class atoms (mirror the Vue .s-* design tokens) ─────────────────

const INPUT =
  'w-full h-8 px-2.5 rounded-[var(--radius-md)] border border-[var(--color-border)] bg-[var(--color-surface)] text-xs text-[var(--color-foreground)] outline-none focus:border-[var(--color-primary)] placeholder:text-[var(--color-muted-foreground)]'
const INPUT_SM = INPUT + ' !h-7 text-[11px]'
const INPUT_MONO = INPUT + ' font-mono'
const BTN =
  'inline-flex items-center justify-center gap-1.5 h-8 px-3 rounded-[var(--radius-md)] text-xs font-medium cursor-pointer border border-transparent transition-colors disabled:opacity-50 disabled:cursor-not-allowed'
const BTN_PRIMARY = BTN + ' bg-[var(--color-primary)] text-[var(--color-on-primary)] hover:opacity-90'
const BTN_SECONDARY =
  BTN + ' bg-[var(--color-surface)] border-[var(--color-border)] text-[var(--color-foreground)] hover:bg-[var(--color-secondary)]'
const BTN_GHOST = BTN + ' bg-transparent text-[var(--color-foreground)] hover:bg-[var(--color-secondary)]'
const BTN_DANGER = BTN + ' bg-[var(--color-destructive)] text-[var(--color-on-destructive)] hover:opacity-90'
const BTN_SM = '!h-7 !px-2.5 !text-[11px] !rounded-[var(--radius-sm)]'
const BTN_XS = '!h-[22px] !px-2 !text-[10px] !rounded-[var(--radius-sm)]'
const ROW =
  'flex items-center gap-3 px-3.5 py-2.5 bg-[var(--color-surface)] border border-[var(--color-border)] rounded-[var(--radius-lg)]'
const LABEL = 'block text-[11px] font-medium text-[var(--color-foreground)] mb-1.5'
const CHIP =
  'inline-flex items-center gap-1 h-[18px] px-2 rounded-full text-[10px] font-medium bg-[var(--color-muted)] text-[var(--color-muted-foreground)] whitespace-nowrap'
const CHIP_ACCENT = CHIP + ' !bg-[var(--neutral-wash)] !text-[var(--color-accent-neutral)]'
const SECTION_TITLE = 'text-[13px] font-semibold tracking-tight text-[var(--color-foreground)]'

// ─── small shared components ────────────────────────────────────────────────

function Switch({
  on,
  onClick,
  title,
  disabled,
}: {
  on: boolean
  onClick: () => void
  title?: string
  disabled?: boolean
}) {
  return (
    <button
      type="button"
      role="switch"
      aria-checked={on}
      disabled={disabled}
      onClick={onClick}
      title={title}
      className="relative h-5 w-[34px] shrink-0 rounded-full border-none p-0 transition-colors disabled:opacity-50"
      style={{ backgroundColor: on ? 'var(--color-accent-neutral)' : 'var(--color-border)' }}
    >
      <span
        className="absolute left-0.5 top-0.5 h-4 w-4 rounded-full bg-[var(--color-surface)] shadow-[var(--shadow-sm)] transition-transform"
        style={{ transform: on ? 'translateX(14px)' : 'translateX(0)' }}
      />
    </button>
  )
}

function Segmented<T extends string>({
  value,
  options,
  onChange,
}: {
  value: T
  options: { value: T; label: string }[]
  onChange: (v: T) => void
}) {
  return (
    <div className="inline-flex gap-0.5 rounded-[var(--radius-md)] bg-[var(--color-muted)] p-0.5">
      {options.map((o) => (
        <button
          key={o.value}
          type="button"
          aria-pressed={value === o.value}
          onClick={() => onChange(o.value)}
          className="h-6 cursor-pointer rounded-[var(--radius-sm)] px-2.5 text-[11px] font-medium transition-colors"
          style={
            value === o.value
              ? { background: 'var(--color-surface)', color: 'var(--color-foreground)' }
              : { background: 'transparent', color: 'var(--color-muted-foreground)' }
          }
        >
          {o.label}
        </button>
      ))}
    </div>
  )
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="mb-3.5 last:mb-0">
      <label className={LABEL}>{label}</label>
      {children}
    </div>
  )
}

function EmptyState({
  Icon,
  title,
  hint,
}: {
  Icon: React.ComponentType<{ className?: string }>
  title: string
  hint: string
}) {
  return (
    <div className="flex flex-col items-center justify-center gap-2.5 py-12 text-center">
      <div className="grid h-9 w-9 place-items-center rounded-lg bg-[var(--color-secondary)] text-[var(--color-muted-foreground)]">
        <Icon className="h-4 w-4" />
      </div>
      <div className="text-[13px] font-medium text-[var(--color-foreground)]">{title}</div>
      <div className="max-w-[240px] text-[11px] leading-relaxed text-[var(--color-muted-foreground)]">{hint}</div>
    </div>
  )
}

// ─── helpers ────────────────────────────────────────────────────────────────

/** Compact context-window size: <1000 raw, >=1000 as K (200000 → "200K"). */
function formatContext(ctx?: number): string {
  if (!ctx || ctx <= 0) return ''
  if (ctx < 1000) return String(ctx)
  return Math.round(ctx / 1000) + 'K'
}

function parseTiers(text: string): string[] {
  return text
    .split(/[\s,]+/)
    .map((s) => s.trim())
    .filter(Boolean)
}

function buildHeaders(rows: { key: string; value: string }[]): Record<string, string> {
  const out: Record<string, string> = {}
  for (const r of rows) {
    const k = r.key.trim()
    if (k) out[k] = r.value
  }
  return out
}

function cmToDetail(cm: CatalogModel): CustomModelDetail {
  return {
    id: cm.id,
    name: cm.name,
    reasoning: cm.reasoning,
    context: cm.context,
    attachment: cm.attachment,
    effort_tiers: cm.effort_tiers ? [...cm.effort_tiers] : [],
    custom: true,
  }
}

/** Reasoning-effort options for a model: prefer models.dev effort values, else default tiers. */
function modelEfforts(model: ModelInfo): string[] {
  const opt = (model.reasoning_options ?? []).find((o) => o.type === 'effort')
  if (opt?.values?.length) return opt.values
  return ['low', 'medium', 'high']
}

function mcpStatusLabel(info: MCPServerInfo): string {
  if (!info.enabled) return 'Disabled'
  switch (info.status) {
    case 'connected':
      return 'Connected'
    case 'needs_auth':
      return 'Login required'
    case 'error':
      return 'Error'
    default:
      return 'Configured'
  }
}

function mcpStatusColor(info: MCPServerInfo): string {
  if (!info.enabled) return 'var(--color-muted-foreground)'
  switch (info.status) {
    case 'connected':
      return 'var(--color-success-fg)'
    case 'needs_auth':
      return 'var(--color-warning-fg)'
    case 'error':
      return 'var(--color-error-fg)'
    default:
      return 'var(--color-muted-foreground)'
  }
}

// ─── main dialog ────────────────────────────────────────────────────────────

export function SettingsDialog() {
  const open = useAppSelector((s) => s.ui.settingsOpen)
  const dispatch = useAppDispatch()
  const [tab, setTab] = useState<TabId>('providers')

  // Esc closes (App.tsx also binds a global Esc, but this is self-contained).
  useEffect(() => {
    if (!open) return
    function onKey(e: KeyboardEvent) {
      if (e.key === 'Escape') dispatch(uiActions.setSettingsOpen(false))
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [open, dispatch])

  if (!open) return null

  const close = () => dispatch(uiActions.setSettingsOpen(false))

  return (
    <div
      className="fixed inset-0 z-[var(--z-modal)] flex items-center justify-center bg-[var(--backdrop)] p-4"
      onClick={close}
    >
      <div
        className="flex max-h-[80vh] w-full max-w-3xl flex-col overflow-hidden rounded-[var(--radius-2xl)] border border-[var(--color-border)] bg-[var(--color-surface)] shadow-[var(--shadow-lg)]"
        onClick={(e) => e.stopPropagation()}
      >
        {/* Header */}
        <div className="flex items-center justify-between border-b border-[var(--color-border)] px-4 py-3">
          <div className="flex items-center gap-2 text-sm font-semibold text-[var(--color-foreground)]">
            <Cog6ToothIcon className="h-4 w-4" />
            Settings
          </div>
          <button
            type="button"
            onClick={close}
            title="Close"
            className="rounded-[var(--radius-md)] p-1 text-[var(--color-muted-foreground)] hover:bg-[var(--color-secondary)]"
          >
            <XMarkIcon className="h-4 w-4" />
          </button>
        </div>

        {/* Body: left nav + right content */}
        <div className="flex min-h-0 flex-1">
          <nav className="w-44 shrink-0 overflow-y-auto border-r border-[var(--color-border)] p-2">
            {TABS.map((t) => {
              const active = tab === t.id
              return (
                <button
                  key={t.id}
                  type="button"
                  onClick={() => setTab(t.id)}
                  className="mb-0.5 flex w-full items-center gap-2.5 rounded-[var(--radius-md)] px-2.5 py-1.5 text-left text-[13px] transition-colors hover:bg-[var(--color-secondary)]"
                  style={
                    active
                      ? { background: 'var(--color-secondary)', color: 'var(--color-foreground)', fontWeight: 500 }
                      : { color: 'var(--color-muted-foreground)' }
                  }
                >
                  <t.Icon className="h-3.5 w-3.5 shrink-0" />
                  <span className="truncate">{t.label}</span>
                </button>
              )
            })}
          </nav>

          <div className="min-w-0 flex-1 overflow-y-auto px-6 py-5">
            {tab === 'providers' && <ProvidersTab />}
            {tab === 'models' && <ModelsTab />}
            {tab === 'mcp' && <MCPTab />}
            {tab === 'skills' && <SkillsTab />}
            {tab === 'appearance' && <AppearanceTab />}
            {tab === 'browser' && <BrowserTab />}
            {tab === 'remote' && <RemoteTab />}
            {tab === 'usage' && <UsageTab />}
          </div>
        </div>
      </div>
    </div>
  )
}

// ════════════════════════════════════════════════════════════════════════════
// Providers tab — full port: list + add/edit form + catalog + advanced config
// ════════════════════════════════════════════════════════════════════════════

function ProvidersTab() {
  const dispatch = useAppDispatch()
  const activeProvider = useAppSelector((s) => s.model.providerName)
  const activeModel = useAppSelector((s) => s.model.modelName)

  const [providers, setProviders] = useState<ProviderDetail[]>([])
  const [loading, setLoading] = useState(true)
  const [setupList, setSetupList] = useState<SetupProvider[]>([])
  const [catalogs, setCatalogs] = useState<Record<string, CatalogModel[]>>({})
  const [catalogLoading, setCatalogLoading] = useState('')
  const [adding, setAdding] = useState(false)
  const [editing, setEditing] = useState<ProviderDetail | null>(null)
  const [modelForm, setModelForm] = useState<{ providerId: string; target: CustomModelDetail | null } | null>(null)

  // Refresh the chat model picker after provider/model mutations.
  async function refreshModels() {
    try {
      const resp = await api.models()
      dispatch(modelActions.setProviders(resp.providers))
    } catch {
      /* ignore */
    }
  }

  async function load() {
    setLoading(true)
    try {
      const list = await api.listProviders()
      setProviders(list)
      // Pre-fetch each provider's catalog so the browse panel shows models
      // immediately (the card's catalog is open by default).
      await Promise.all(
        list.map(async (p) => {
          if (!catalogs[p.id]) {
            try {
              const cat = await api.providerCatalog(p.id)
              setCatalogs((prev) => ({ ...prev, [p.id]: cat }))
            } catch {
              setCatalogs((prev) => ({ ...prev, [p.id]: [] }))
            }
          }
        }),
      )
    } catch {
      /* ignore */
    }
    setLoading(false)
  }

  useEffect(() => {
    void load()
    // Fetch the registry list for the add-provider picker.
    api.setupProviders().then(setSetupList).catch(() => {})
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  async function refreshCatalog(providerId: string) {
    setCatalogLoading(providerId)
    try {
      const cat = await api.providerCatalog(providerId)
      setCatalogs((prev) => ({ ...prev, [providerId]: cat }))
    } catch {
      setCatalogs((prev) => ({ ...prev, [providerId]: [] }))
    }
    setCatalogLoading('')
  }

  async function onProviderSaved() {
    setProviders(await api.listProviders())
    await refreshModels()
    setEditing(null)
    setAdding(false)
  }

  async function deleteProvider(id: string) {
    try {
      await api.deleteProvider(id)
      setProviders((prev) => prev.filter((p) => p.id !== id))
      await refreshModels()
    } catch (err) {
      console.error('Failed to delete provider:', err)
    }
  }

  // Toggle a catalog row's enabled state (registry models live in model_state;
  // toggling adds/removes them from the chat picker). Optimistic, revert on
  // failure.
  async function toggleCatalogModel(providerId: string, modelId: string) {
    const list = catalogs[providerId]
    if (!list) return
    const m = list.find((x) => x.id === modelId)
    if (!m) return
    const next = !m.added
    setCatalogs((prev) => ({
      ...prev,
      [providerId]: prev[providerId].map((x) => (x.id === modelId ? { ...x, added: next } : x)),
    }))
    try {
      await api.toggleModelEnabled(providerId, modelId, next)
      await refreshModels()
    } catch (err) {
      setCatalogs((prev) => ({
        ...prev,
        [providerId]: prev[providerId].map((x) => (x.id === modelId ? { ...x, added: !next } : x)),
      }))
      console.error('Failed to toggle model:', err)
    }
  }

  // Add/edit a custom model: rebuild the provider's custom_models (only
  // user-defined models are persisted) and save via updateProvider.
  async function saveCustomModel(payload: {
    providerId: string
    id: string
    name?: string
    reasoning: boolean
    context: number
    attachment: boolean
    effortTiers: string[]
    isEdit: boolean
    originalId?: string
  }) {
    const p = providers.find((x) => x.id === payload.providerId)
    if (!p) return
    const newId = payload.id.trim()
    const isSelf = payload.isEdit && payload.originalId === newId
    // Reject duplicate ids (collisions with built-in registry models included).
    if (!isSelf) {
      const clash = (p.custom_models ?? []).some((m) => m.id === newId)
      if (clash) {
        // Surface via alert-style: the inline form reads its own error from
        // the parent through a callback would be cleaner; for now throw to be
        // caught by the form's try/catch.
        throw new Error('A model with this ID already exists for this provider.')
      }
    }
    const next: CustomModelDetail[] = (p.custom_models ?? [])
      .filter((m) => m.custom)
      .map((m) => ({
        id: m.id,
        name: m.name,
        reasoning: m.reasoning,
        context: m.context,
        attachment: m.attachment,
        effort_tiers: m.effort_tiers,
      }))
    const built: CustomModelDetail = {
      id: newId,
      name: payload.name,
      reasoning: payload.reasoning,
      context: payload.context || undefined,
      attachment: payload.attachment || undefined,
      effort_tiers: payload.effortTiers.length ? payload.effortTiers : undefined,
    }
    if (payload.isEdit && payload.originalId) {
      const i = next.findIndex((m) => m.id === payload.originalId)
      if (i >= 0) next[i] = built
      else next.push(built)
    } else {
      next.push(built)
    }
    await api.updateProvider(payload.providerId, { custom_models: next })
    setProviders(await api.listProviders())
    await refreshCatalog(payload.providerId)
    await refreshModels()
    setModelForm(null)
  }

  async function removeCustomModel(providerId: string, modelId: string) {
    const p = providers.find((x) => x.id === providerId)
    if (!p) return
    const next: CustomModelDetail[] = (p.custom_models ?? [])
      .filter((m) => m.custom && m.id !== modelId)
      .map((m) => ({
        id: m.id,
        name: m.name,
        reasoning: m.reasoning,
        context: m.context,
        attachment: m.attachment,
        effort_tiers: m.effort_tiers,
      }))
    try {
      await api.updateProvider(providerId, { custom_models: next })
      setProviders(await api.listProviders())
      await refreshCatalog(providerId)
      await refreshModels()
    } catch (err) {
      console.error('Failed to remove model:', err)
    }
  }

  // Add form overrides the list; edit form renders in place of the list too.
  if (adding || editing) {
    return (
      <ProviderForm
        key={editing?.id ?? 'new'}
        editing={editing}
        setupList={setupList}
        configuredIds={providers.map((p) => p.id)}
        onCancel={() => {
          setAdding(false)
          setEditing(null)
        }}
        onSaved={onProviderSaved}
      />
    )
  }

  return (
    <div>
      <div className="mb-4 flex items-center justify-between">
        <div className="flex items-baseline gap-2">
          <h3 className={SECTION_TITLE}>Providers</h3>
          <span className="font-mono text-[11px] text-[var(--color-muted-foreground)]">{providers.length}</span>
        </div>
        <button type="button" className={`${BTN_SECONDARY} ${BTN_SM}`} onClick={() => setAdding(true)}>
          <PlusIcon className="h-3.5 w-3.5" /> Add
        </button>
      </div>

      {loading ? (
        <div className="animate-pulse py-6 text-center text-xs text-[var(--color-muted-foreground)]">Loading…</div>
      ) : providers.length === 0 ? (
        <EmptyState
          Icon={CpuChipIcon}
          title="No providers configured"
          hint="Click Add to configure your first model provider."
        />
      ) : (
        <div className="space-y-2.5">
          {providers.map((p) => (
            <ProviderCard
              key={p.id}
              provider={p}
              catalog={catalogs[p.id] ?? []}
              catalogLoading={catalogLoading === p.id}
              activeProvider={activeProvider}
              activeModelName={activeProvider === p.id ? activeModel : ''}
              modelForm={modelForm?.providerId === p.id ? modelForm : null}
              onRefreshCatalog={() => refreshCatalog(p.id)}
              onToggleModel={(mid) => toggleCatalogModel(p.id, mid)}
              onAddCustomModel={() => setModelForm({ providerId: p.id, target: null })}
              onEditCustomModel={(m) => setModelForm({ providerId: p.id, target: m })}
              onRemoveCustomModel={(mid) => removeCustomModel(p.id, mid)}
              onCancelModelForm={() => setModelForm(null)}
              onSaveCustomModel={saveCustomModel}
              onEdit={() => setEditing(p)}
              onDelete={() => deleteProvider(p.id)}
            />
          ))}
        </div>
      )}
    </div>
  )
}

/** A single provider card: head (brand/name/url/actions) + model catalog. */
function ProviderCard({
  provider,
  catalog,
  catalogLoading,
  activeProvider,
  activeModelName,
  modelForm,
  onRefreshCatalog,
  onToggleModel,
  onAddCustomModel,
  onEditCustomModel,
  onRemoveCustomModel,
  onCancelModelForm,
  onSaveCustomModel,
  onEdit,
  onDelete,
}: {
  provider: ProviderDetail
  catalog: CatalogModel[]
  catalogLoading: boolean
  activeProvider: string
  activeModelName: string
  modelForm: { providerId: string; target: CustomModelDetail | null } | null
  onRefreshCatalog: () => void
  onToggleModel: (modelId: string) => void
  onAddCustomModel: () => void
  onEditCustomModel: (m: CustomModelDetail) => void
  onRemoveCustomModel: (modelId: string) => void
  onCancelModelForm: () => void
  onSaveCustomModel: (payload: {
    providerId: string
    id: string
    name?: string
    reasoning: boolean
    context: number
    attachment: boolean
    effortTiers: string[]
    isEdit: boolean
    originalId?: string
  }) => Promise<void>
  onEdit: () => void
  onDelete: () => void
}) {
  const [deleteConfirm, setDeleteConfirm] = useState(false)
  const [search, setSearch] = useState('')

  const addedCount = catalog.filter((m) => m.added).length
  const filtered = (() => {
    const q = search.trim().toLowerCase()
    if (!q) return catalog
    return catalog.filter(
      (m) => m.id.toLowerCase().includes(q) || (m.name ?? '').toLowerCase().includes(q),
    )
  })()

  return (
    <div className="overflow-hidden rounded-[var(--radius-lg)] border border-[var(--color-border)] bg-[var(--color-surface)]">
      {/* Head */}
      <div className="flex items-center gap-3 border-b border-[var(--color-border)] px-3 py-2.5">
        <div className="grid h-[30px] w-[30px] shrink-0 place-items-center rounded-[var(--radius-md)] bg-[var(--color-secondary)] text-[var(--color-foreground)]">
          <CpuChipIcon className="h-[18px] w-[18px]" />
        </div>
        <div className="min-w-0 flex-1">
          <div className="flex items-center gap-2 text-[13px] font-semibold text-[var(--color-foreground)]">
            <span className="truncate">{provider.name || provider.id}</span>
            {activeProvider === provider.id && <span className={CHIP_ACCENT}>Active</span>}
            {provider.custom && <span className={CHIP}>Custom</span>}
          </div>
          <div className="mt-0.5 truncate font-mono text-[11px] text-[var(--color-muted-foreground)]">
            {provider.base_url || (provider.api_key_set ? 'API key set' : '—')}
          </div>
        </div>
        <div className="flex shrink-0 items-center gap-1.5">
          {deleteConfirm ? (
            activeModelName ? (
              <div className="flex max-w-[280px] flex-col gap-2 rounded-[var(--radius-md)] border border-[var(--color-warning-fg)] bg-[var(--color-warning-bg)] p-2.5 text-right">
                <div>
                  <div className="text-[12px] font-semibold text-[var(--color-warning-fg)]">Model in use</div>
                  <div className="mt-0.5 text-[10.5px] text-[var(--color-muted-foreground)]">
                    Switch away from “{activeModelName}” before deleting this provider.
                  </div>
                </div>
                <div className="flex flex-wrap justify-end gap-1.5">
                  <button type="button" className={`${BTN_GHOST} ${BTN_XS}`} onClick={() => setDeleteConfirm(false)}>
                    Cancel
                  </button>
                </div>
              </div>
            ) : (
              <>
                <button type="button" className={`${BTN_DANGER} ${BTN_XS}`} onClick={onDelete}>
                  Delete
                </button>
                <button type="button" className={`${BTN_GHOST} ${BTN_XS}`} onClick={() => setDeleteConfirm(false)}>
                  Cancel
                </button>
              </>
            )
          ) : (
            <>
              <button type="button" title="Edit" className={`${BTN_GHOST} ${BTN_XS}`} onClick={onEdit}>
                <PencilSquareIcon className="h-3.5 w-3.5" />
              </button>
              <button type="button" title="Remove" className={`${BTN_GHOST} ${BTN_XS}`} onClick={() => setDeleteConfirm(true)}>
                <TrashIcon className="h-3.5 w-3.5" />
              </button>
            </>
          )}
        </div>
      </div>

      {/* Models sub-area */}
      <div className="px-3 pb-3 pt-2">
        <div className="flex flex-wrap items-center justify-between gap-2 pb-2">
          <span className="text-[10.5px] text-[var(--color-muted-foreground)]">
            <b className="font-semibold text-[var(--color-foreground)]">{addedCount}</b>/{catalog.length} models
          </span>
          <div className="ml-auto flex items-center gap-1.5">
            <button
              type="button"
              className={`${BTN_SECONDARY} ${BTN_XS}`}
              onClick={onRefreshCatalog}
              title="Refresh catalog"
            >
              ↻
            </button>
            <button type="button" className={`${BTN_SECONDARY} ${BTN_XS}`} onClick={onAddCustomModel}>
              <PlusIcon className="h-3.5 w-3.5" /> Custom model
            </button>
          </div>
        </div>

        {/* Inline catalog (open by default) */}
        <div className="mt-1.5 border-t border-dashed border-[var(--color-border)] pt-2">
          {catalogLoading && catalog.length === 0 ? (
            <div className="animate-pulse py-3 text-center text-[10px] text-[var(--color-muted-foreground)]">
              Loading models…
            </div>
          ) : (
            <>
              <input
                value={search}
                onChange={(e) => setSearch(e.target.value)}
                type="text"
                placeholder="Search models…"
                className={INPUT_SM + ' mb-1.5'}
              />
              {filtered.length === 0 ? (
                <div className="py-3 text-center text-[10px] text-[var(--color-muted-foreground)]">
                  {provider.custom ? 'No models in catalog' : 'All models configured'}
                </div>
              ) : (
                <div className="max-h-[220px] overflow-y-auto rounded-[var(--radius-md)] border border-[var(--color-border)] bg-[var(--color-background)] p-0.5">
                  {filtered.map((cm) => (
                    <div
                      key={cm.id}
                      className="flex items-center gap-2.5 rounded-[var(--radius-sm)] px-2 py-1.5 hover:bg-[var(--color-secondary)]"
                    >
                      <div className="min-w-0 flex-1">
                        <div className="font-mono text-[11px] text-[var(--color-foreground)]">{cm.id}</div>
                        <div className="truncate text-[10px] text-[var(--color-muted-foreground)]">
                          {cm.name || cm.id}
                          {cm.context ? ` · ${formatContext(cm.context)}` : ''}
                          {cm.reasoning ? ' · reasoning' : ''}
                          {cm.attachment ? ' · vision' : ''}
                        </div>
                      </div>
                      {cm.added && cm.custom ? (
                        <div className="flex items-center gap-1">
                          <button
                            type="button"
                            title="Edit model"
                            className={`${BTN_GHOST} ${BTN_XS}`}
                            onClick={() => onEditCustomModel(cmToDetail(cm))}
                          >
                            <PencilSquareIcon className="h-3.5 w-3.5" />
                          </button>
                          <button
                            type="button"
                            title="Remove"
                            className={`${BTN_GHOST} ${BTN_XS} text-[var(--color-muted-foreground)] hover:text-[var(--color-destructive)]`}
                            onClick={() => onRemoveCustomModel(cm.id)}
                          >
                            <TrashIcon className="h-3.5 w-3.5" />
                          </button>
                        </div>
                      ) : (
                        <Switch
                          on={!!cm.added}
                          onClick={() => onToggleModel(cm.id)}
                          title={cm.added ? 'Hide from picker' : 'Show in picker'}
                        />
                      )}
                    </div>
                  ))}
                </div>
              )}
            </>
          )}
        </div>

        {/* Inline custom-model form */}
        {modelForm && (
          <CustomModelForm
            target={modelForm.target}
            onCancel={onCancelModelForm}
            onSave={(payload) =>
              onSaveCustomModel({ ...payload, providerId: provider.id, originalId: payload.originalId })
            }
          />
        )}
      </div>
    </div>
  )
}

/** Add/edit provider form with advanced config (base_url, headers, vision, thinking, reasoning_effort). */
function ProviderForm({
  editing,
  setupList,
  configuredIds,
  onCancel,
  onSaved,
}: {
  editing: ProviderDetail | null
  setupList: SetupProvider[]
  configuredIds: string[]
  onCancel: () => void
  onSaved: () => void
}) {
  const isEdit = !!editing
  const [mode, setMode] = useState<'registry' | 'custom'>(editing?.custom ? 'custom' : 'registry')
  const [selId, setSelId] = useState('')
  const [customId, setCustomId] = useState(editing?.id ?? '')
  const [name, setName] = useState(editing?.name ?? '')
  const [apiKey, setApiKey] = useState('')
  const [baseUrl, setBaseUrl] = useState(editing?.base_url ?? '')
  const [headers, setHeaders] = useState<{ key: string; value: string }[]>(
    Object.entries(editing?.headers ?? {}).map(([key, value]) => ({ key, value })),
  )
  const [vision, setVision] = useState(!!editing?.vision)
  const [thinking, setThinking] = useState(!!editing?.thinking)
  const [reasoningEffort, setReasoningEffort] = useState(editing?.reasoning_effort ?? '')
  const [advancedOpen, setAdvancedOpen] = useState(
    !!(editing?.base_url || (editing?.headers && Object.keys(editing.headers).length)),
  )
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')

  // Filter out already-configured providers from the registry picker (add mode).
  const availableSetup = setupList.filter((s) => !configuredIds.includes(s.id))

  const providerId = isEdit ? editing!.id : mode === 'custom' ? customId.trim() : selId

  async function save(e: React.FormEvent) {
    e.preventDefault()
    setError('')
    if (!providerId) {
      setError('Provider is required.')
      return
    }
    if (!isEdit && !apiKey.trim()) {
      setError('API key is required.')
      return
    }
    setSaving(true)
    try {
      const builtHeaders = buildHeaders(headers)
      if (isEdit) {
        const data: Parameters<typeof api.updateProvider>[1] = {
          name: name || undefined,
          base_url: baseUrl || undefined,
          headers: Object.keys(builtHeaders).length ? builtHeaders : undefined,
          vision,
          thinking,
          reasoning_effort: reasoningEffort || undefined,
        }
        if (apiKey.trim()) data.api_key = apiKey.trim()
        await api.updateProvider(editing!.id, data)
      } else {
        await api.addProvider({
          id: providerId,
          api_key: apiKey.trim(),
          name: name || undefined,
          vision,
          thinking,
          reasoning_effort: reasoningEffort || undefined,
          base_url: baseUrl || undefined,
          headers: Object.keys(builtHeaders).length ? builtHeaders : undefined,
        })
      }
      onSaved()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to save')
    } finally {
      setSaving(false)
    }
  }

  return (
    <form onSubmit={save}>
      <div className="mb-4 flex items-center justify-between">
        <h3 className={SECTION_TITLE}>{isEdit ? 'Edit provider' : 'Add provider'}</h3>
        <button type="button" className={`${BTN_GHOST} ${BTN_SM}`} onClick={onCancel}>
          ← Back
        </button>
      </div>

      <div className="rounded-[var(--radius-lg)] border border-[var(--color-border)] bg-[var(--color-surface)]">
        <div className="border-b border-[var(--color-border)] bg-[var(--color-muted)] px-3.5 py-2.5 text-[11px] font-semibold uppercase tracking-wide text-[var(--color-muted-foreground)]">
          {isEdit ? editing!.name || editing!.id : 'New provider'}
        </div>
        <div className="p-4">
          {/* Provider selection (add mode only) */}
          {!isEdit && (
            <Field label="Provider">
              <Segmented
                value={mode}
                onChange={(m) => setMode(m)}
                options={[
                  { value: 'registry', label: 'Registry' },
                  { value: 'custom', label: 'Custom' },
                ]}
              />
              <div className="mt-2">
                {mode === 'registry' ? (
                  <select
                    value={selId}
                    onChange={(e) => setSelId(e.target.value)}
                    className={INPUT}
                  >
                    <option value="">Select a provider…</option>
                    {availableSetup.map((s) => (
                      <option key={s.id} value={s.id}>
                        {s.name}
                      </option>
                    ))}
                  </select>
                ) : (
                  <input
                    value={customId}
                    onChange={(e) => setCustomId(e.target.value)}
                    type="text"
                    placeholder="my-provider"
                    className={INPUT_MONO}
                  />
                )}
              </div>
            </Field>
          )}

          <Field label="API key">
            <input
              value={apiKey}
              onChange={(e) => setApiKey(e.target.value)}
              type="password"
              placeholder={isEdit ? 'Leave blank to keep current' : 'sk-…'}
              className={INPUT_MONO}
            />
          </Field>

          {(mode === 'custom' || isEdit) && (
            <Field label="Display name">
              <input
                value={name}
                onChange={(e) => setName(e.target.value)}
                type="text"
                placeholder="My Provider"
                className={INPUT}
              />
            </Field>
          )}

          {/* Advanced disclosure */}
          <button
            type="button"
            onClick={() => setAdvancedOpen((v) => !v)}
            className="mb-3 flex items-center gap-1 text-[11px] font-medium text-[var(--color-muted-foreground)] hover:text-[var(--color-foreground)]"
          >
            <ChevronDownIcon
              className="h-3.5 w-3.5 transition-transform"
              style={{ transform: advancedOpen ? 'rotate(180deg)' : 'none' }}
            />
            Advanced configuration
          </button>

          {advancedOpen && (
            <div className="mb-3 rounded-[var(--radius-md)] border border-[var(--color-border)] bg-[var(--color-muted)] p-3">
              <Field label="Base URL">
                <input
                  value={baseUrl}
                  onChange={(e) => setBaseUrl(e.target.value)}
                  type="text"
                  placeholder="https://api.example.com/v1"
                  className={INPUT_MONO}
                />
              </Field>

              <div className="mb-3.5">
                <div className="mb-1.5 flex items-center justify-between">
                  <label className={LABEL + ' !mb-0'}>Headers</label>
                  <button
                    type="button"
                    className={`${BTN_GHOST} ${BTN_XS}`}
                    onClick={() => setHeaders((h) => [...h, { key: '', value: '' }])}
                  >
                    + Add header
                  </button>
                </div>
                {headers.length === 0 && (
                  <div className="text-[11px] text-[var(--color-muted-foreground)]">No custom headers.</div>
                )}
                {headers.map((h, i) => (
                  <div key={i} className="mb-2 flex gap-2">
                    <input
                      value={h.key}
                      onChange={(e) => setHeaders((prev) => prev.map((x, j) => (j === i ? { ...x, key: e.target.value } : x)))}
                      type="text"
                      placeholder="Header-Name"
                      className={INPUT_MONO}
                    />
                    <input
                      value={h.value}
                      onChange={(e) => setHeaders((prev) => prev.map((x, j) => (j === i ? { ...x, value: e.target.value } : x)))}
                      type="text"
                      placeholder="value"
                      className={INPUT_MONO}
                    />
                    <button
                      type="button"
                      onClick={() => setHeaders((prev) => prev.filter((_, j) => j !== i))}
                      className="grid h-7 w-7 shrink-0 place-items-center rounded-[var(--radius-sm)] border border-[var(--color-border)] text-[var(--color-muted-foreground)] hover:bg-[var(--color-secondary)]"
                    >
                      ✕
                    </button>
                  </div>
                ))}
              </div>

              <Field label="Capabilities (provider-level overrides)">
                <div className="space-y-2">
                  <div className="flex items-center justify-between rounded-[var(--radius-md)] border border-[var(--color-border)] bg-[var(--color-surface)] px-3 py-2">
                    <div>
                      <div className="text-[12px] font-medium text-[var(--color-foreground)]">Vision</div>
                      <div className="text-[10px] text-[var(--color-muted-foreground)]">Accept image inputs</div>
                    </div>
                    <Switch on={vision} onClick={() => setVision((v) => !v)} />
                  </div>
                  <div className="flex items-center justify-between rounded-[var(--radius-md)] border border-[var(--color-border)] bg-[var(--color-surface)] px-3 py-2">
                    <div>
                      <div className="text-[12px] font-medium text-[var(--color-foreground)]">Thinking</div>
                      <div className="text-[10px] text-[var(--color-muted-foreground)]">Extended reasoning</div>
                    </div>
                    <Switch on={thinking} onClick={() => setThinking((v) => !v)} />
                  </div>
                  <div className="flex items-center justify-between rounded-[var(--radius-md)] border border-[var(--color-border)] bg-[var(--color-surface)] px-3 py-2">
                    <div className="text-[12px] font-medium text-[var(--color-foreground)]">Reasoning effort</div>
                    <select
                      value={reasoningEffort}
                      onChange={(e) => setReasoningEffort(e.target.value)}
                      className={INPUT_SM}
                      style={{ width: '8rem' }}
                    >
                      <option value="">Default</option>
                      <option value="low">Low</option>
                      <option value="medium">Medium</option>
                      <option value="high">High</option>
                    </select>
                  </div>
                </div>
              </Field>
            </div>
          )}

          {error && <div className="mb-3 text-[11px] text-[var(--color-destructive)]">{error}</div>}
        </div>
        <div className="flex justify-end gap-2 border-t border-[var(--color-border)] bg-[var(--color-muted)] px-3.5 py-3">
          <button type="button" className={BTN_SECONDARY} onClick={onCancel}>
            Cancel
          </button>
          <button type="submit" disabled={saving} className={BTN_PRIMARY}>
            {saving ? 'Saving…' : isEdit ? 'Save changes' : 'Add provider'}
          </button>
        </div>
      </div>
    </form>
  )
}

/** Inline add/edit form for a single custom model. */
function CustomModelForm({
  target,
  onCancel,
  onSave,
}: {
  target: CustomModelDetail | null
  onCancel: () => void
  onSave: (payload: {
    id: string
    name?: string
    reasoning: boolean
    context: number
    attachment: boolean
    effortTiers: string[]
    isEdit: boolean
    originalId?: string
  }) => Promise<void>
}) {
  const isEdit = !!target
  const [id, setId] = useState(target?.id ?? '')
  const [name, setName] = useState(target?.name ?? '')
  const [context, setContext] = useState(target?.context ? String(target.context) : '')
  const [reasoning, setReasoning] = useState(!!target?.reasoning)
  const [attachment, setAttachment] = useState(!!target?.attachment)
  const [tiersText, setTiersText] = useState((target?.effort_tiers ?? []).join(', '))
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')

  async function save(e: React.FormEvent) {
    e.preventDefault()
    setError('')
    if (!id.trim()) {
      setError('Model ID is required.')
      return
    }
    setSaving(true)
    try {
      await onSave({
        id: id.trim(),
        name: name.trim() || undefined,
        reasoning,
        context: context ? Number(context) : 0,
        attachment,
        effortTiers: parseTiers(tiersText),
        isEdit,
        originalId: target?.id,
      })
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to save model')
    } finally {
      setSaving(false)
    }
  }

  return (
    <form
      onSubmit={save}
      className="mt-2 rounded-[var(--radius-md)] border border-[var(--color-border)] bg-[var(--color-muted)] p-3"
    >
      <div className="mb-2 text-[11.5px] font-semibold text-[var(--color-foreground)]">
        {isEdit ? 'Edit custom model' : 'Add custom model'}
      </div>
      <div className="grid grid-cols-2 gap-2.5">
        <Field label="Model ID">
          <input value={id} onChange={(e) => setId(e.target.value)} type="text" placeholder="gpt-4o" className={INPUT_MONO} />
        </Field>
        <Field label="Display name">
          <input value={name} onChange={(e) => setName(e.target.value)} type="text" placeholder="GPT-4o" className={INPUT} />
        </Field>
        <Field label="Context (tokens)">
          <input
            value={context}
            onChange={(e) => setContext(e.target.value)}
            type="number"
            placeholder="128000"
            className={INPUT}
          />
        </Field>
        <Field label="Effort tiers">
          <input
            value={tiersText}
            onChange={(e) => setTiersText(e.target.value)}
            type="text"
            placeholder="low, medium, high"
            className={INPUT_MONO}
          />
        </Field>
      </div>
      <div className="mb-2 flex items-center gap-4">
        <label className="flex items-center gap-2 text-[11px] text-[var(--color-foreground)]">
          <Switch on={reasoning} onClick={() => setReasoning((v) => !v)} /> Reasoning
        </label>
        <label className="flex items-center gap-2 text-[11px] text-[var(--color-foreground)]">
          <Switch on={attachment} onClick={() => setAttachment((v) => !v)} /> Vision
        </label>
      </div>
      {error && <div className="mb-2 text-[11px] text-[var(--color-destructive)]">{error}</div>}
      <div className="flex justify-end gap-2">
        <button type="button" className={`${BTN_GHOST} ${BTN_SM}`} onClick={onCancel}>
          Cancel
        </button>
        <button type="submit" disabled={saving} className={`${BTN_PRIMARY} ${BTN_SM}`}>
          {saving ? 'Saving…' : 'Save'}
        </button>
      </div>
    </form>
  )
}

// ════════════════════════════════════════════════════════════════════════════
// Models tab — model state: favorites, enable/disable, effort overrides
// ════════════════════════════════════════════════════════════════════════════

function ModelsTab() {
  const providers = useAppSelector((s) => s.model.providers)
  const [state, setState] = useState<ModelStateResponse | null>(null)
  const [loading, setLoading] = useState(true)
  const [busy, setBusy] = useState('')

  async function load() {
    try {
      setState(await api.modelState())
    } catch {
      /* ignore */
    }
    setLoading(false)
  }

  useEffect(() => {
    void load()
  }, [])

  const favSet = new Set((state?.favorite ?? []).map((r) => `${r.provider}/${r.model}`))
  const disSet = new Set((state?.disabled_models ?? []).map((r) => `${r.provider}/${r.model}`))
  const overrides = state?.effort_overrides ?? {}

  async function toggleFavorite(p: string, m: string) {
    setBusy(`fav:${p}/${m}`)
    try {
      await api.toggleFavorite(p, m)
      await load()
    } catch (err) {
      console.error('Failed to toggle favorite:', err)
    }
    setBusy('')
  }

  async function toggleEnabled(p: string, m: string, enabled: boolean) {
    setBusy(`en:${p}/${m}`)
    try {
      await api.toggleModelEnabled(p, m, enabled)
      await load()
    } catch (err) {
      console.error('Failed to toggle model:', err)
    }
    setBusy('')
  }

  async function setEffort(p: string, m: string, effort: string) {
    setBusy(`ef:${p}/${m}`)
    try {
      await api.setModelEffort(p, m, effort)
      await load()
    } catch (err) {
      console.error('Failed to set effort:', err)
    }
    setBusy('')
  }

  if (loading) {
    return <div className="animate-pulse py-6 text-center text-xs text-[var(--color-muted-foreground)]">Loading…</div>
  }

  const totalModels = providers.reduce((n, p) => n + p.models.length, 0)
  if (totalModels === 0) {
    return (
      <EmptyState Icon={Cog6ToothIcon} title="No models available" hint="Configure a provider to see its models here." />
    )
  }

  return (
    <div>
      <div className="mb-4 flex items-baseline gap-2">
        <h3 className={SECTION_TITLE}>Models</h3>
        <span className="font-mono text-[11px] text-[var(--color-muted-foreground)]">{totalModels}</span>
      </div>
      <p className="mb-3 text-[11px] leading-relaxed text-[var(--color-muted-foreground)]">
        Toggle which models appear in the chat picker, mark favorites, and set a per-model reasoning effort.
      </p>
      <div className="space-y-3">
        {providers.map((p) => {
          if (p.models.length === 0) return null
          return (
            <div key={p.id}>
              <div className="mb-1.5 text-[11px] font-semibold uppercase tracking-wide text-[var(--color-muted-foreground)]">
                {p.id}
              </div>
              <div className="space-y-1.5">
                {p.models.map((m) => {
                  const key = `${p.id}/${m.id}`
                  const isFav = favSet.has(key)
                  const isDisabled = disSet.has(key)
                  const efforts = modelEfforts(m)
                  const currentEffort = overrides[key] ?? ''
                  const showEffort = !!(m.reasoning || m.reasoning_options?.length)
                  return (
                    <div key={m.id} className={ROW} data-muted={isDisabled ? 'true' : 'false'}>
                      <button
                        type="button"
                        title={isFav ? 'Remove favorite' : 'Add favorite'}
                        onClick={() => toggleFavorite(p.id, m.id)}
                        className="shrink-0 text-[14px] leading-none"
                        style={{ color: isFav ? 'var(--color-warning-fg)' : 'var(--color-muted-foreground)' }}
                      >
                        {isFav ? '★' : '☆'}
                      </button>
                      <div className="min-w-0 flex-1">
                        <div className="truncate text-[12px] font-medium text-[var(--color-foreground)]">{m.name}</div>
                        <div className="truncate font-mono text-[10.5px] text-[var(--color-muted-foreground)]">
                          {m.id}
                          {m.context_limit ? ` · ${formatContext(m.context_limit)}` : ''}
                          {m.reasoning ? ' · reasoning' : ''}
                          {m.image_support ? ' · vision' : ''}
                        </div>
                      </div>
                      {showEffort && (
                        <select
                          value={currentEffort}
                          onChange={(e) => e.target.value && setEffort(p.id, m.id, e.target.value)}
                          disabled={busy.startsWith(`ef:${key}`)}
                          className={INPUT_SM}
                          style={{ width: '7rem' }}
                          title="Reasoning effort"
                        >
                          <option value="">Effort: auto</option>
                          {efforts.map((ef) => (
                            <option key={ef} value={ef}>
                              {ef}
                            </option>
                          ))}
                        </select>
                      )}
                      <Switch
                        on={!isDisabled}
                        onClick={() => toggleEnabled(p.id, m.id, isDisabled)}
                        title={isDisabled ? 'Show in picker' : 'Hide from picker'}
                      />
                    </div>
                  )
                })}
              </div>
            </div>
          )
        })}
      </div>
    </div>
  )
}

// ════════════════════════════════════════════════════════════════════════════
// MCP tab — servers CRUD (local/http/sse) + OAuth login polling
// ════════════════════════════════════════════════════════════════════════════

interface MCPHeaderRow {
  key: string
  value: string
}
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

function emptyMCPForm(): MCPForm {
  return {
    name: '',
    transport: 'http',
    url: '',
    command: '',
    argsText: '',
    headers: [],
    timeout: '',
    oauthEnabled: false,
    clientId: '',
    clientSecret: '',
    scopesText: '',
  }
}

function MCPTab() {
  const [servers, setServers] = useState<Record<string, MCPServerInfo>>({})
  const [loading, setLoading] = useState(true)
  const [editing, setEditing] = useState<string | null>(null) // null=list, ''=add, name=edit
  const [form, setForm] = useState<MCPForm>(emptyMCPForm())
  const [saving, setSaving] = useState(false)
  const [formError, setFormError] = useState('')
  const [loginBusy, setLoginBusy] = useState('')
  const [loginMsg, setLoginMsg] = useState('')
  const [loginMsgFor, setLoginMsgFor] = useState('')
  const pollRef = useRef<number | null>(null)

  async function load() {
    setLoading(true)
    try {
      const r = await api.mcpList()
      setServers(r.servers)
    } catch {
      /* ignore */
    }
    setLoading(false)
  }

  useEffect(() => {
    void load()
    return () => stopPoll()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  function stopPoll() {
    if (pollRef.current) {
      window.clearInterval(pollRef.current)
      pollRef.current = null
    }
  }

  function openAdd() {
    setForm(emptyMCPForm())
    setFormError('')
    setEditing('')
  }

  function openEdit(info: MCPServerInfo) {
    setForm({
      name: info.name,
      transport: info.type === 'stdio' || info.type === '' ? 'local' : (info.type as 'http' | 'sse'),
      url: info.url ?? '',
      command: info.command ?? '',
      argsText: (info.args ?? []).join(' '),
      headers: Object.entries(info.headers ?? {}).map(([key, value]) => ({ key, value })),
      timeout: info.timeout ? String(info.timeout) : '',
      oauthEnabled: info.oauth,
      clientId: '',
      clientSecret: '',
      scopesText: '',
    })
    setFormError('')
    setEditing(info.name)
  }

  function buildRequest(): MCPServerRequest {
    const hdrs = buildHeaders(form.headers)
    const req: MCPServerRequest = { name: form.name.trim(), type: form.transport }
    if (form.transport === 'local') {
      req.command = form.command.trim()
      req.args = form.argsText.trim() ? form.argsText.trim().split(/\s+/) : undefined
    } else {
      req.url = form.url.trim()
      if (Object.keys(hdrs).length) req.headers = hdrs
      if (form.timeout.trim()) req.timeout = Number(form.timeout)
      if (form.oauthEnabled || form.clientId.trim()) {
        req.oauth = {
          enabled: true,
          client_id: form.clientId.trim() || undefined,
          client_secret: form.clientSecret.trim() || undefined,
          scopes: form.scopesText.trim() ? form.scopesText.trim().split(/\s+/) : undefined,
        }
      }
    }
    return req
  }

  async function save(e: React.FormEvent) {
    e.preventDefault()
    setFormError('')
    if (!form.name.trim()) {
      setFormError('Server name is required')
      return
    }
    if (form.transport === 'local' && !form.command.trim()) {
      setFormError('Command is required')
      return
    }
    if (form.transport !== 'local' && !form.url.trim()) {
      setFormError('URL is required')
      return
    }
    setSaving(true)
    try {
      const req = buildRequest()
      if (editing) await api.mcpUpdate(editing, req)
      else await api.mcpCreate(req)
      setEditing(null)
      await load()
    } catch (err) {
      setFormError(err instanceof Error ? err.message : 'Failed to save')
    }
    setSaving(false)
  }

  async function remove(name: string) {
    try {
      await api.mcpDelete(name)
      await load()
    } catch (err) {
      console.error('Failed to delete MCP server:', err)
    }
  }

  async function toggle(name: string, enabled: boolean) {
    try {
      await api.mcpToggle(name, enabled)
      await load()
    } catch (err) {
      console.error('Failed to toggle MCP server:', err)
    }
  }

  async function login(name: string) {
    setLoginBusy(name)
    setLoginMsgFor(name)
    setLoginMsg('Opening browser — complete authorization, then return here…')
    try {
      await api.mcpLogin(name)
    } catch (err) {
      setLoginMsg(err instanceof Error ? err.message : 'Login failed')
      setLoginBusy('')
      return
    }
    stopPoll()
    pollRef.current = window.setInterval(async () => {
      try {
        const st = await api.mcpLoginStatus(name)
        if (st.status === 'authorized') {
          stopPoll()
          setLoginBusy('')
          setLoginMsg('')
          setLoginMsgFor('')
          await load()
        } else if (st.status === 'error') {
          stopPoll()
          setLoginBusy('')
          setLoginMsgFor(name)
          setLoginMsg(st.message || 'Login failed')
        } else if (st.status === 'needs_client_id') {
          stopPoll()
          setLoginBusy('')
          setLoginMsgFor(name)
          setLoginMsg('This server does not support automatic registration. Edit it and set an OAuth Client ID, then log in again.')
        }
      } catch {
        /* keep polling */
      }
    }, 1500)
  }

  const serverList = Object.entries(servers)

  // Add/edit form view
  if (editing !== null) {
    return (
      <form onSubmit={save}>
        <div className="mb-4 flex items-center justify-between">
          <h3 className={SECTION_TITLE}>{editing ? 'Edit server' : 'Add server'}</h3>
          <button type="button" className={`${BTN_GHOST} ${BTN_SM}`} onClick={() => setEditing(null)}>
            ← Back
          </button>
        </div>

        <div className="overflow-hidden rounded-[var(--radius-lg)] border border-[var(--color-border)] bg-[var(--color-surface)]">
          <div className="border-b border-[var(--color-border)] bg-[var(--color-muted)] px-3.5 py-2.5 text-[11px] font-semibold uppercase tracking-wide text-[var(--color-muted-foreground)]">
            {editing ? 'Edit' : 'New'} server
          </div>
          <div className="p-4">
            <Field label="Server name">
              <input
                value={form.name}
                onChange={(e) => setForm((f) => ({ ...f, name: e.target.value }))}
                disabled={!!editing}
                type="text"
                placeholder="my-server"
                className={INPUT}
              />
            </Field>

            <Field label="Transport">
              <Segmented
                value={form.transport}
                onChange={(t) => setForm((f) => ({ ...f, transport: t }))}
                options={[
                  { value: 'local', label: 'Local' },
                  { value: 'http', label: 'HTTP' },
                  { value: 'sse', label: 'SSE' },
                ]}
              />
            </Field>

            {form.transport !== 'local' ? (
              <>
                <Field label="URL">
                  <input
                    value={form.url}
                    onChange={(e) => setForm((f) => ({ ...f, url: e.target.value }))}
                    type="text"
                    placeholder="https://api.example.com/mcp"
                    className={INPUT_MONO}
                  />
                </Field>

                <div className="mb-3.5">
                  <div className="mb-1.5 flex items-center justify-between">
                    <label className={LABEL + ' !mb-0'}>Headers</label>
                    <button
                      type="button"
                      className={`${BTN_GHOST} ${BTN_XS}`}
                      onClick={() => setForm((f) => ({ ...f, headers: [...f.headers, { key: '', value: '' }] }))}
                    >
                      + Add header
                    </button>
                  </div>
                  {form.headers.map((h, i) => (
                    <div key={i} className="mb-2 flex gap-2">
                      <input
                        value={h.key}
                        onChange={(e) =>
                          setForm((f) => ({
                            ...f,
                            headers: f.headers.map((x, j) => (j === i ? { ...x, key: e.target.value } : x)),
                          }))
                        }
                        type="text"
                        placeholder="Header-Name"
                        className={INPUT_MONO}
                      />
                      <input
                        value={h.value}
                        onChange={(e) =>
                          setForm((f) => ({
                            ...f,
                            headers: f.headers.map((x, j) => (j === i ? { ...x, value: e.target.value } : x)),
                          }))
                        }
                        type="text"
                        placeholder="value"
                        className={INPUT_MONO}
                      />
                      <button
                        type="button"
                        onClick={() => setForm((f) => ({ ...f, headers: f.headers.filter((_, j) => j !== i) }))}
                        className="grid h-7 w-7 shrink-0 place-items-center rounded-[var(--radius-sm)] border border-[var(--color-border)] text-[var(--color-muted-foreground)] hover:bg-[var(--color-secondary)]"
                      >
                        ✕
                      </button>
                    </div>
                  ))}
                </div>

                <div className="mb-3.5 flex items-center justify-between rounded-[var(--radius-md)] border border-[var(--color-border)] bg-[var(--color-muted)] px-3 py-2">
                  <div className="text-[12px] font-medium text-[var(--color-foreground)]">Use OAuth</div>
                  <Switch
                    on={form.oauthEnabled}
                    onClick={() => setForm((f) => ({ ...f, oauthEnabled: !f.oauthEnabled }))}
                  />
                </div>

                {form.oauthEnabled && (
                  <div className="mb-3.5 space-y-3">
                    <Field label="OAuth client ID">
                      <input
                        value={form.clientId}
                        onChange={(e) => setForm((f) => ({ ...f, clientId: e.target.value }))}
                        type="text"
                        placeholder="Optional — leave blank to auto-register"
                        className={INPUT_MONO}
                      />
                    </Field>
                    <Field label="OAuth client secret">
                      <input
                        value={form.clientSecret}
                        onChange={(e) => setForm((f) => ({ ...f, clientSecret: e.target.value }))}
                        type="password"
                        placeholder="Optional (confidential clients)"
                        className={INPUT_MONO}
                      />
                    </Field>
                    <Field label="Scopes">
                      <input
                        value={form.scopesText}
                        onChange={(e) => setForm((f) => ({ ...f, scopesText: e.target.value }))}
                        type="text"
                        placeholder="space-separated, optional"
                        className={INPUT_MONO}
                      />
                    </Field>
                  </div>
                )}

                <Field label="Timeout (seconds)">
                  <input
                    value={form.timeout}
                    onChange={(e) => setForm((f) => ({ ...f, timeout: e.target.value }))}
                    type="number"
                    placeholder="180"
                    className={INPUT}
                    style={{ maxWidth: '120px' }}
                  />
                </Field>
              </>
            ) : (
              <>
                <Field label="Command">
                  <input
                    value={form.command}
                    onChange={(e) => setForm((f) => ({ ...f, command: e.target.value }))}
                    type="text"
                    placeholder="npx"
                    className={INPUT_MONO}
                  />
                </Field>
                <Field label="Arguments">
                  <input
                    value={form.argsText}
                    onChange={(e) => setForm((f) => ({ ...f, argsText: e.target.value }))}
                    type="text"
                    placeholder="-y @some/mcp-server"
                    className={INPUT_MONO}
                  />
                </Field>
              </>
            )}

            {formError && <div className="text-[11px] text-[var(--color-destructive)]">{formError}</div>}
          </div>
          <div className="flex justify-end gap-2 border-t border-[var(--color-border)] bg-[var(--color-muted)] px-3.5 py-3">
            <button type="button" className={BTN_SECONDARY} onClick={() => setEditing(null)}>
              Cancel
            </button>
            <button type="submit" disabled={saving} className={BTN_PRIMARY}>
              {saving ? 'Saving…' : 'Save'}
            </button>
          </div>
        </div>
      </form>
    )
  }

  return (
    <div>
      <div className="mb-4 flex items-center justify-between">
        <div className="flex items-baseline gap-2">
          <h3 className={SECTION_TITLE}>MCP Servers</h3>
          <span className="font-mono text-[11px] text-[var(--color-muted-foreground)]">{serverList.length}</span>
        </div>
        <button type="button" className={`${BTN_SECONDARY} ${BTN_SM}`} onClick={openAdd}>
          <PlusIcon className="h-3.5 w-3.5" /> Add
        </button>
      </div>

      {loading ? (
        <div className="animate-pulse py-6 text-center text-xs text-[var(--color-muted-foreground)]">Loading…</div>
      ) : serverList.length === 0 ? (
        <EmptyState
          Icon={ServerStackIcon}
          title="No MCP servers"
          hint="Add a server to extend the agent with external tools."
        />
      ) : (
        <div className="space-y-2">
          {serverList.map(([name, info]) => {
            const Icon = info.type === 'sse' || info.type === 'http' ? GlobeAltIcon : BoltIcon
            return (
              <div
                key={name}
                className="flex flex-col rounded-[var(--radius-lg)] border border-[var(--color-border)] bg-[var(--color-surface)] px-3.5 py-2.5"
                data-muted={!info.enabled ? 'true' : 'false'}
              >
                <div className="flex w-full items-center gap-3">
                  <div className="grid h-7 w-7 shrink-0 place-items-center rounded-[var(--radius-md)] text-[var(--color-muted-foreground)]">
                    <Icon className="h-3.5 w-3.5" />
                  </div>
                  <div className="min-w-0 flex-1">
                    <div className="flex flex-wrap items-center gap-2 text-[12px] font-medium text-[var(--color-foreground)]">
                      {name}
                      <span className={CHIP}>{info.type || 'stdio'}</span>
                      <span className="text-[10px]" style={{ color: mcpStatusColor(info) }}>
                        ● {mcpStatusLabel(info)}
                      </span>
                    </div>
                    <div className="truncate font-mono text-[11px] text-[var(--color-muted-foreground)]">
                      {info.type === 'sse' || info.type === 'http' ? info.url : info.command}
                    </div>
                  </div>
                  <div className="flex shrink-0 items-center gap-1.5">
                    <button type="button" className={`${BTN_GHOST} ${BTN_XS}`} onClick={() => openEdit(info)}>
                      Edit
                    </button>
                    <button
                      type="button"
                      className={`${BTN_GHOST} ${BTN_XS} text-[var(--color-destructive)]`}
                      onClick={() => remove(name)}
                    >
                      Delete
                    </button>
                    <Switch on={info.enabled} onClick={() => toggle(name, !info.enabled)} />
                  </div>
                </div>
                {/* OAuth login row */}
                {(info.type === 'http' || info.type === 'sse') && (info.oauth || info.status === 'needs_auth') && (
                  <div className="mt-2 flex items-center gap-2 pl-10">
                    <button
                      type="button"
                      className={`${BTN_SECONDARY} ${BTN_XS}`}
                      disabled={loginBusy === name}
                      onClick={() => login(name)}
                    >
                      {loginBusy === name
                        ? 'Waiting for browser…'
                        : info.has_auth
                          ? 'Re-authorize'
                          : 'Log in'}
                    </button>
                    {info.has_auth && <span className={CHIP + ' !bg-[var(--color-success-bg)] !text-[var(--color-success-fg)]'}>Authenticated</span>}
                  </div>
                )}
                {loginBusy === name && loginMsg && (
                  <div className="mt-1 pl-10 text-[10px] text-[var(--color-muted-foreground)]">{loginMsg}</div>
                )}
                {loginMsgFor === name && loginMsg && !loginBusy && (
                  <div className="mt-1 pl-10 text-[10px] text-[var(--color-warning-fg)]">{loginMsg}</div>
                )}
                {info.error && (
                  <div className="mt-1 pl-10 font-mono text-[10px] text-[var(--color-error-fg)]">{info.error}</div>
                )}
              </div>
            )
          })}
        </div>
      )}
    </div>
  )
}

// ════════════════════════════════════════════════════════════════════════════
// Skills tab — list + filter + search + enable/disable
// ════════════════════════════════════════════════════════════════════════════

function SkillsTab() {
  const [skills, setSkills] = useState<SkillInfo[]>([])
  const [loading, setLoading] = useState(true)
  const [filter, setFilter] = useState<'all' | 'local' | 'builtin'>('all')
  const [search, setSearch] = useState('')

  async function load() {
    try {
      setSkills(await api.skillsList())
    } catch {
      /* ignore */
    }
    setLoading(false)
  }

  useEffect(() => {
    void load()
  }, [])

  async function toggle(name: string, enabled: boolean) {
    try {
      await api.skillToggle(name, enabled)
      setSkills((prev) => prev.map((s) => (s.name === name ? { ...s, enabled } : s)))
    } catch (err) {
      console.error('Failed to toggle skill:', err)
    }
  }

  const q = search.trim().toLowerCase()
  const filtered = skills.filter((s) => {
    if (filter === 'builtin' && !s.builtin) return false
    if (filter === 'local' && s.builtin) return false
    if (q && !s.name.toLowerCase().includes(q) && !(s.description ?? '').toLowerCase().includes(q)) return false
    return true
  })

  return (
    <div>
      <div className="mb-4 flex items-baseline gap-2">
        <h3 className={SECTION_TITLE}>Skills</h3>
        <span className="font-mono text-[11px] text-[var(--color-muted-foreground)]">{skills.length}</span>
      </div>
      <div className="mb-3 flex items-center gap-2">
        <Segmented
          value={filter}
          onChange={(f) => setFilter(f)}
          options={[
            { value: 'all', label: 'All' },
            { value: 'local', label: 'Local' },
            { value: 'builtin', label: 'Built-in' },
          ]}
        />
        <input
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          type="text"
          placeholder="Search skills…"
          className={INPUT}
          style={{ flex: 1 }}
        />
      </div>

      {loading ? (
        <div className="animate-pulse py-6 text-center text-xs text-[var(--color-muted-foreground)]">Loading…</div>
      ) : filtered.length === 0 ? (
        <EmptyState Icon={SparklesIcon} title="No skills found" hint="Skills extend the agent with slash commands." />
      ) : (
        <div className="space-y-2">
          {filtered.map((s) => (
            <div key={s.name} className={ROW} data-muted={!s.enabled ? 'true' : 'false'}>
              <div className="min-w-0 flex-1">
                <div className="flex items-center gap-2 text-[12px] font-medium text-[var(--color-foreground)]">
                  {s.name}
                  {s.builtin && <span className={CHIP}>Built-in</span>}
                </div>
                {s.description && (
                  <div className="text-[11px] text-[var(--color-muted-foreground)]">{s.description}</div>
                )}
              </div>
              <Switch on={!!s.enabled} onClick={() => toggle(s.name, !s.enabled)} />
            </div>
          ))}
        </div>
      )}
    </div>
  )
}

// ════════════════════════════════════════════════════════════════════════════
// Appearance tab — theme picker
// ════════════════════════════════════════════════════════════════════════════

function applyTheme(name: string) {
  const t = THEMES.find((x) => x.id === name)
  const dark = t ? t.appearance === 'dark' : true
  document.documentElement.setAttribute('data-theme', name)
  document.documentElement.classList.toggle('dark', dark)
  try {
    localStorage.setItem('jcode-theme', name)
  } catch {
    /* ignore */
  }
}

function AppearanceTab() {
  const dispatch = useAppDispatch()
  const storeTheme = useAppSelector((s) => s.ui.theme)
  // Resolve the active theme: prefer store, fall back to localStorage, then default.
  const [active, setActive] = useState(() => {
    let t = storeTheme
    if (!t || t === 'system' || !THEMES.some((x) => x.id === t)) {
      try {
        t = localStorage.getItem('jcode-theme') || 'jcode-dark'
      } catch {
        t = 'jcode-dark'
      }
    }
    return t
  })

  function choose(name: string) {
    setActive(name)
    dispatch(uiActions.setTheme(name))
    applyTheme(name)
  }

  const darkThemes = THEMES.filter((t) => t.appearance === 'dark')
  const lightThemes = THEMES.filter((t) => t.appearance === 'light')

  const ThemeButton = ({ t }: { t: { id: string; label: string; appearance: string } }) => (
    <button
      type="button"
      data-theme={t.id}
      onClick={() => choose(t.id)}
      className="cursor-pointer overflow-hidden rounded-[var(--radius-md)] text-left transition-transform active:scale-[0.98]"
      style={{
        border: active === t.id ? '2px solid var(--color-accent-neutral)' : '1px solid var(--color-border)',
        backgroundColor: 'var(--color-background)',
      }}
    >
      <div className="px-2.5 pb-1.5 pt-2">
        <div className="mb-1.5 flex items-center gap-1.5">
          <span className="h-2.5 w-2.5 rounded-full" style={{ backgroundColor: 'var(--color-primary)' }} />
          <span className="h-2.5 w-2.5 rounded-full" style={{ backgroundColor: 'var(--color-accent-neutral)' }} />
          <span className="h-2.5 w-2.5 rounded-full" style={{ backgroundColor: 'var(--color-success-fg)' }} />
          <span className="h-2.5 w-2.5 rounded-full" style={{ backgroundColor: 'var(--color-error-fg)' }} />
        </div>
        <div
          className="truncate rounded px-1.5 py-1 font-mono text-[10px]"
          style={{ backgroundColor: 'var(--color-surface)', color: 'var(--color-foreground)' }}
        >
          <span style={{ color: 'var(--color-primary)' }}>&gt;</span> jcode
        </div>
      </div>
      <div
        className="flex items-center justify-between px-2.5 py-1.5"
        style={{ backgroundColor: 'var(--color-surface)', borderTop: '1px solid var(--color-border)' }}
      >
        <span className="truncate text-[11px] font-medium text-[var(--color-foreground)]">{t.label}</span>
        {active === t.id && (
          <CheckIcon className="h-3.5 w-3.5 shrink-0" style={{ color: 'var(--color-accent-neutral)' }} />
        )}
      </div>
    </button>
  )

  return (
    <div className="space-y-5">
      <h3 className={SECTION_TITLE}>Theme</h3>
      <div>
        <div className="mb-2 text-[10px] font-medium text-[var(--color-muted-foreground)]">Dark</div>
        <div className="grid grid-cols-2 gap-2">
          {darkThemes.map((t) => (
            <ThemeButton key={t.id} t={t} />
          ))}
        </div>
      </div>
      <div>
        <div className="mb-2 text-[10px] font-medium text-[var(--color-muted-foreground)]">Light</div>
        <div className="grid grid-cols-2 gap-2">
          {lightThemes.map((t) => (
            <ThemeButton key={t.id} t={t} />
          ))}
        </div>
      </div>
      <div className="text-[10px] leading-relaxed text-[var(--color-muted-foreground)]">
        Tip: run <span className="font-mono">/theme</span> in the chat to switch from the terminal.
      </div>
    </div>
  )
}

// ════════════════════════════════════════════════════════════════════════════
// Browser tab — config + site permissions
// ════════════════════════════════════════════════════════════════════════════

function BrowserTab() {
  const [status, setStatus] = useState<BrowserStatusResponse | null>(null)
  const [cfg, setCfg] = useState<BrowserConfig>({
    enabled: false,
    backend: 'auto',
    site_permissions: [],
    approval: {},
    dev_mode: false,
  })
  const saveTimer = useRef<number | null>(null)
  const pollRef = useRef<number | null>(null)

  async function load() {
    try {
      const st = await api.browserStatus()
      setStatus(st)
      if (st.status) {
        setCfg({
          enabled: st.status.enabled,
          backend: st.status.backend || 'auto',
          chrome_path: st.status.chrome_path,
          dev_mode: st.status.dev_mode,
          approval: st.approval || {},
          site_permissions: st.site_permissions || [],
        })
      }
    } catch (err) {
      console.error('Failed to load browser status:', err)
    }
  }

  useEffect(() => {
    void load()
    pollRef.current = window.setInterval(() => void load(), 3000)
    return () => {
      if (pollRef.current) window.clearInterval(pollRef.current)
      if (saveTimer.current) window.clearTimeout(saveTimer.current)
    }
  }, [])

  function save(next: BrowserConfig) {
    if (saveTimer.current) window.clearTimeout(saveTimer.current)
    saveTimer.current = window.setTimeout(async () => {
      try {
        await api.browserSaveConfig(next)
        await load()
      } catch (err) {
        console.error('Failed to save browser config:', err)
      }
    }, 250)
  }

  function patch(p: Partial<BrowserConfig>) {
    setCfg((prev) => {
      const next = { ...prev, ...p }
      save(next)
      return next
    })
  }

  function setApproval(cls: string, val: string) {
    const approval = { ...(cfg.approval ?? {}), [cls]: val }
    patch({ approval })
  }

  function addSitePerm() {
    const perms = [...(cfg.site_permissions ?? [])]
    perms.push({ origin: '', navigate: 'allow', interact: 'allow' })
    patch({ site_permissions: perms })
  }

  function removeSitePerm(i: number) {
    const perms = (cfg.site_permissions ?? []).filter((_, j) => j !== i)
    patch({ site_permissions: perms })
  }

  function updateSitePerm(i: number, p: Partial<BrowserSitePermission>) {
    const perms = (cfg.site_permissions ?? []).map((sp, j) => (j === i ? { ...sp, ...p } : sp))
    patch({ site_permissions: perms })
  }

  const st = status?.status

  return (
    <div>
      <div className="mb-4">
        <h3 className={SECTION_TITLE}>Browser</h3>
        <p className="mt-0.5 text-[12px] text-[var(--color-muted-foreground)]">Control browser-use tool access and permissions.</p>
      </div>

      <div className={ROW}>
        <div className="grid h-7 w-7 shrink-0 place-items-center rounded-[var(--radius-md)] text-[var(--color-muted-foreground)]">
          <GlobeAltIcon className="h-4 w-4" />
        </div>
        <div className="min-w-0 flex-1">
          <div className="text-[12px] font-medium text-[var(--color-foreground)]">Enable browser use</div>
          <div className="text-[11px] text-[var(--color-muted-foreground)]">Allow the agent to drive a browser.</div>
        </div>
        <Switch on={cfg.enabled} onClick={() => patch({ enabled: !cfg.enabled })} />
      </div>

      {cfg.enabled && (
        <>
          <div className="mb-2 mt-5 text-[11px] font-medium uppercase tracking-wide text-[var(--color-muted-foreground)]">
            Control
          </div>
          <div className={ROW}>
            <div className="grid h-7 w-7 shrink-0 place-items-center rounded-[var(--radius-md)] text-[var(--color-muted-foreground)]">
              <ComputerDesktopIcon className="h-4 w-4" />
            </div>
            <div className="min-w-0 flex-1">
              <div className="text-[12px] font-medium text-[var(--color-foreground)]">Managed Chrome</div>
              <div className="text-[11px] text-[var(--color-muted-foreground)]">
                {st?.chrome_found ? st.chrome_version || st.chrome_path : 'No Chrome detected'}
              </div>
            </div>
            <select
              value={cfg.backend}
              onChange={(e) => patch({ backend: e.target.value })}
              className={INPUT_SM}
              style={{ width: '8rem' }}
            >
              <option value="auto">Auto</option>
              <option value="managed">Managed</option>
              <option value="extension">Extension</option>
            </select>
          </div>
          <div className={ROW}>
            <div className="grid h-7 w-7 shrink-0 place-items-center rounded-[var(--radius-md)] text-[var(--color-muted-foreground)]">
              <GlobeAltIcon className="h-4 w-4" />
            </div>
            <div className="min-w-0 flex-1">
              <div className="text-[12px] font-medium text-[var(--color-foreground)]">Extension</div>
              <div className="flex items-center gap-1.5 text-[11px] text-[var(--color-muted-foreground)]">
                <span
                  className="h-1.5 w-1.5 shrink-0 rounded-full"
                  style={{ backgroundColor: st?.extension_online ? 'var(--color-success-fg)' : 'var(--color-border)' }}
                />
                {st?.extension_online ? 'Connected' : 'Not connected'}
              </div>
            </div>
            {st?.extension_online && (
              <span className={CHIP + ' !bg-[var(--color-success-bg)] !text-[var(--color-success-fg)]'}>Online</span>
            )}
          </div>

          {!st?.chrome_found && (
            <div className="mt-3">
              <label className="text-[11px] font-medium text-[var(--color-muted-foreground)]">Chrome path</label>
              <input
                value={cfg.chrome_path ?? ''}
                onChange={(e) => patch({ chrome_path: e.target.value })}
                className={INPUT + ' mt-1'}
                placeholder="/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
              />
            </div>
          )}

          <div className="mb-2 mt-5 text-[11px] font-medium uppercase tracking-wide text-[var(--color-muted-foreground)]">
            Approval
          </div>
          <div className={ROW}>
            <div className="min-w-0 flex-1">
              <div className="text-[12px] font-medium text-[var(--color-foreground)]">Navigation</div>
            </div>
            <select
              value={cfg.approval?.['navigate'] ?? 'ask'}
              onChange={(e) => setApproval('navigate', e.target.value)}
              className={INPUT_SM}
              style={{ width: '10rem' }}
            >
              <option value="ask">Ask each site</option>
              <option value="always_allow">Always allow</option>
            </select>
          </div>
          <div className={ROW}>
            <div className="min-w-0 flex-1">
              <div className="text-[12px] font-medium text-[var(--color-foreground)]">Interaction</div>
            </div>
            <select
              value={cfg.approval?.['interact'] ?? 'ask'}
              onChange={(e) => setApproval('interact', e.target.value)}
              className={INPUT_SM}
              style={{ width: '10rem' }}
            >
              <option value="ask">Ask each site</option>
              <option value="always_allow">Always allow</option>
            </select>
          </div>

          <div className="mb-2 mt-5 flex items-center justify-between">
            <div className="text-[11px] font-medium uppercase tracking-wide text-[var(--color-muted-foreground)]">
              Site permissions
            </div>
            <button type="button" className={`${BTN_SECONDARY} ${BTN_SM}`} onClick={addSitePerm}>
              <PlusIcon className="h-3.5 w-3.5" /> Add
            </button>
          </div>
          {!(cfg.site_permissions?.length ?? 0) && (
            <div className={ROW}>
              <div className="text-[11px] text-[var(--color-muted-foreground)]">No site permissions configured.</div>
            </div>
          )}
          {(cfg.site_permissions ?? []).map((sp, i) => (
            <div key={i} className={ROW}>
              <input
                value={sp.origin}
                onChange={(e) => updateSitePerm(i, { origin: e.target.value })}
                className={INPUT_SM}
                style={{ flex: 1 }}
                placeholder="https://github.com"
              />
              <select
                value={sp.navigate ?? 'ask'}
                onChange={(e) => updateSitePerm(i, { navigate: e.target.value })}
                className={INPUT_SM}
                style={{ width: '7rem' }}
              >
                <option value="ask">nav: ask</option>
                <option value="allow">nav: allow</option>
              </select>
              <select
                value={sp.interact ?? 'ask'}
                onChange={(e) => updateSitePerm(i, { interact: e.target.value })}
                className={INPUT_SM}
                style={{ width: '7rem' }}
              >
                <option value="ask">act: ask</option>
                <option value="allow">act: allow</option>
              </select>
              <button type="button" className={`${BTN_GHOST} ${BTN_SM}`} onClick={() => removeSitePerm(i)}>
                <TrashIcon className="h-3.5 w-3.5" />
              </button>
            </div>
          ))}

          <div className="mb-2 mt-5 text-[11px] font-medium uppercase tracking-wide text-[var(--color-muted-foreground)]">
            Developer mode
          </div>
          <div className={ROW}>
            <div className="min-w-0 flex-1">
              <div className="mb-0.5 text-[11px] font-semibold text-[var(--color-warning-fg)]">⚠ Elevated risk</div>
              <div className="text-[12px] font-medium text-[var(--color-foreground)]">Developer mode</div>
              <div className="text-[11px] text-[var(--color-muted-foreground)]">
                Auto-approve browser actions without confirmation.
              </div>
            </div>
            <Switch on={!!cfg.dev_mode} onClick={() => patch({ dev_mode: !cfg.dev_mode })} />
          </div>
        </>
      )}
    </div>
  )
}

// ════════════════════════════════════════════════════════════════════════════
// Remote tab — SSH aliases (read + connect via wizard is a follow-up)
// ════════════════════════════════════════════════════════════════════════════

function RemoteTab() {
  const [data, setData] = useState<SSHListResponse | null>(null)
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    api
      .sshList()
      .then(setData)
      .catch(() => {})
      .finally(() => setLoading(false))
  }, [])

  const aliases = data?.aliases ?? []
  const current = data?.current ?? 'local'

  return (
    <div>
      <div className="mb-4 flex items-center justify-between">
        <h3 className={SECTION_TITLE}>Remote / SSH</h3>
        {/* TODO: wire the remote-connect wizard (Vue inject('openRemoteConnect')). */}
        <button type="button" className={`${BTN_SECONDARY} ${BTN_SM}`} disabled title="Coming soon">
          <PlusIcon className="h-3.5 w-3.5" /> Connect
        </button>
      </div>

      <div className="mb-3">
        <div className="mb-1 text-[11px] font-medium text-[var(--color-muted-foreground)]">Current environment</div>
        <span className={CHIP_ACCENT}>
          <span className="h-1.5 w-1.5 rounded-full" style={{ backgroundColor: 'var(--color-accent-neutral)' }} />
          {current}
        </span>
      </div>

      {loading ? (
        <div className="animate-pulse py-6 text-center text-xs text-[var(--color-muted-foreground)]">Loading…</div>
      ) : aliases.length === 0 ? (
        <EmptyState
          Icon={CommandLineIcon}
          title="No SSH aliases"
          hint="Add aliases to ~/.jcode/config.json to connect to remote workspaces."
        />
      ) : (
        <div className="space-y-2">
          {aliases.map((alias) => (
            <div key={alias.name} className={ROW} style={{ cursor: 'default' }}>
              <div className="grid h-7 w-7 shrink-0 place-items-center rounded-[var(--radius-md)] text-[var(--color-muted-foreground)]">
                <ServerIcon className="h-3.5 w-3.5" />
              </div>
              <div className="min-w-0 flex-1">
                <div className="text-[12px] font-medium text-[var(--color-foreground)]">{alias.name}</div>
                <div className="truncate font-mono text-[11px] text-[var(--color-muted-foreground)]">
                  {alias.addr}
                  {alias.path ? ` · ${alias.path}` : ''}
                </div>
              </div>
              {current === alias.name && <span className={CHIP_ACCENT}>Active</span>}
            </div>
          ))}
        </div>
      )}
    </div>
  )
}

// ════════════════════════════════════════════════════════════════════════════
// Usage tab — stats with range selector + trend + breakdowns
// ════════════════════════════════════════════════════════════════════════════

function UsageTab() {
  const [stats, setStats] = useState<UsageStats | null>(null)
  const [days, setDays] = useState(30)
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    setLoading(true)
    api
      .usageStats(days)
      .then(setStats)
      .catch(() => {})
      .finally(() => setLoading(false))
  }, [days])

  if (loading) {
    return <div className="animate-pulse py-6 text-center text-xs text-[var(--color-muted-foreground)]">Loading usage…</div>
  }
  if (!stats) {
    return <EmptyState Icon={ChartBarIcon} title="No usage data" hint="Usage statistics will appear here once available." />
  }

  const t = stats.totals
  const trend = stats.daily_trend
  const maxTokens = Math.max(1, ...trend.map((d) => d.tokens))
  const fmt = (n: number) => n.toLocaleString()

  return (
    <div>
      <div className="mb-4 flex items-center justify-between">
        <h3 className={SECTION_TITLE}>Usage</h3>
        <select value={days} onChange={(e) => setDays(Number(e.target.value))} className={INPUT_SM} style={{ width: '7rem' }}>
          <option value={7}>7 days</option>
          <option value={30}>30 days</option>
          <option value={90}>90 days</option>
          <option value={365}>1 year</option>
        </select>
      </div>

      {/* Totals */}
      <div className="mb-5 grid grid-cols-2 gap-2 lg:grid-cols-4">
        <StatCard label="Total tokens" value={fmt(t.total_tokens)} />
        <StatCard label="Calls" value={fmt(t.calls)} />
        <StatCard label="Turns" value={fmt(t.turns)} />
        <StatCard label="Sessions" value={fmt(t.sessions)} />
        <StatCard label="Prompt" value={fmt(t.prompt_tokens)} />
        <StatCard label="Completion" value={fmt(t.completion_tokens)} />
        <StatCard label="Cached" value={fmt(t.cached_tokens)} />
        <StatCard label="Reasoning" value={fmt(t.reasoning_tokens)} />
      </div>

      {/* Streak + cache */}
      <div className="mb-5 flex flex-wrap gap-2">
        <span className={CHIP}>Active days: {stats.active_days}</span>
        <span className={CHIP}>Current streak: {stats.current_streak}</span>
        <span className={CHIP}>Longest streak: {stats.longest_streak}</span>
        {stats.cache_supported && (
          <span className={CHIP}>Cache hit: {Math.round(stats.cache_hit_rate * 100)}%</span>
        )}
        {stats.most_used_model && <span className={CHIP}>Top model: {stats.most_used_model}</span>}
      </div>

      {/* Daily trend (mini bar chart) */}
      {trend.length > 0 && (
        <div className="mb-5">
          <div className="mb-1.5 text-[11px] font-medium text-[var(--color-muted-foreground)]">Daily token trend</div>
          <div className="flex h-20 items-end gap-px overflow-hidden rounded-[var(--radius-md)] border border-[var(--color-border)] bg-[var(--color-muted)] p-1.5">
            {trend.map((d) => (
              <div
                key={d.date}
                title={`${d.date}: ${fmt(d.tokens)} tokens`}
                className="flex-1 rounded-sm"
                style={{
                  height: `${Math.max(2, (d.tokens / maxTokens) * 100)}%`,
                  backgroundColor: 'var(--color-accent-neutral)',
                  opacity: d.tokens === 0 ? 0.2 : 0.85,
                }}
              />
            ))}
          </div>
        </div>
      )}

      {/* Breakdowns */}
      <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
        <Breakdown title="By model" shares={stats.by_model} />
        <Breakdown title="By project" shares={stats.by_project} />
      </div>

      {/* TODO: port the 365-day heatmap from UsageStatsPanel.vue. */}
    </div>
  )
}

function StatCard({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-[var(--radius-md)] border border-[var(--color-border)] bg-[var(--color-surface)] px-3 py-2">
      <div className="text-[10px] uppercase tracking-wide text-[var(--color-muted-foreground)]">{label}</div>
      <div className="mt-0.5 font-mono text-[14px] font-semibold text-[var(--color-foreground)]">{value}</div>
    </div>
  )
}

function Breakdown({
  title,
  shares,
}: {
  title: string
  shares: { name: string; tokens: number; share: number }[]
}) {
  const sorted = [...shares].sort((a, b) => b.tokens - a.tokens).slice(0, 8)
  return (
    <div>
      <div className="mb-1.5 text-[11px] font-medium text-[var(--color-muted-foreground)]">{title}</div>
      <div className="space-y-1.5">
        {sorted.length === 0 && <div className="text-[11px] text-[var(--color-muted-foreground)]">No data.</div>}
        {sorted.map((s) => (
          <div key={s.name}>
            <div className="flex items-center justify-between text-[11px]">
              <span className="truncate text-[var(--color-foreground)]">{s.name}</span>
              <span className="ml-2 shrink-0 font-mono text-[var(--color-muted-foreground)]">
                {Math.round(s.share * 100)}%
              </span>
            </div>
            <div className="mt-0.5 h-1.5 overflow-hidden rounded-full bg-[var(--color-muted)]">
              <div
                className="h-full rounded-full"
                style={{ width: `${Math.max(1, s.share * 100)}%`, backgroundColor: 'var(--color-accent-neutral)' }}
              />
            </div>
          </div>
        ))}
      </div>
    </div>
  )
}
