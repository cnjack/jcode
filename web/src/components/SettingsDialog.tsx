/**
 * SettingsDialog — React port of web/src/components/SettingsDialog.vue.
 *
 * Full-screen app-like overlay (mirrors the Vue .settings-shell): a `fixed
 * inset-0` surface with an opaque background and its own left nav rail + an
 * inset surface content panel — the same geometry as the chat page, NOT a small
 * centered dialog. Tabs: Providers (full CRUD + catalog + advanced config),
 * Models (state/favorites/effort), MCP (servers CRUD + OAuth login), Skills
 * (enable/disable), Appearance (theme picker), Browser (config + site
 * permissions), Remote (SSH aliases), Usage (stats).
 *
 * The Providers tab is the most complete port: list of provider cards, inline
 * add/edit form with advanced fields (base_url, headers, vision, thinking,
 * reasoning_effort), browsable model catalog with add/remove/toggle, and an
 * inline custom-model authoring form. Other tabs are functional CRUD ports of
 * the Vue logic.
 *
 * State is per-tab (each tab remounts on activation, so switching tabs naturally
 * abandons in-progress sub-flows — mirroring the Vue `watch(activeTab)` reset).
 */

import { useEffect, useRef, useState } from 'react'
import {
  Cog6ToothIcon,
  ArrowLeftIcon,
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
  ShieldCheckIcon,
  KeyIcon,
  ArrowRightIcon,
  ChatBubbleOvalLeftIcon,
} from '@heroicons/react/24/outline'
import { useTranslation } from 'react-i18next'
import { useAppDispatch, useAppSelector } from '../app/hooks'
import { uiActions, modelActions, loadConfig } from '../app/store'
import { ProviderIcon } from './ProviderIcon'
import { api } from '../lib/api'
import { openRemoteConnect } from '../lib/remote'
import { LOCALE_LABELS, SUPPORTED_LOCALES, setLocale, type SupportedLocale } from '../i18n'
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
  SetupProvider,
} from '../lib/types'

// ─── tab config ────────────────────────────────────────────────────────────

// Matches the Vue SettingsDialog tab list exactly (line 106):
//   general, appearance, providers, mcp, skills, browser, ssh, channels,
//   shortcuts, usage. Note: there is NO standalone Models tab — models live
//   inside the Providers tab (catalog + custom models), mirroring the Vue app.
type TabId = 'general' | 'appearance' | 'providers' | 'mcp' | 'skills' | 'browser' | 'ssh' | 'channels' | 'shortcuts' | 'usage'

const TABS: { id: TabId; Icon: React.ComponentType<{ className?: string }> }[] = [
  { id: 'general', Icon: Cog6ToothIcon },
  { id: 'appearance', Icon: SwatchIcon },
  { id: 'providers', Icon: CpuChipIcon },
  { id: 'mcp', Icon: ServerStackIcon },
  { id: 'skills', Icon: SparklesIcon },
  { id: 'browser', Icon: GlobeAltIcon },
  { id: 'ssh', Icon: CommandLineIcon },
  { id: 'channels', Icon: ChatBubbleOvalLeftIcon },
  { id: 'shortcuts', Icon: KeyIcon },
  { id: 'usage', Icon: ChartBarIcon },
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

function mcpStatusLabel(info: MCPServerInfo, t: (k: string) => string): string {
  if (!info.enabled) return t('settings.mcp.status.disabled')
  switch (info.status) {
    case 'connected':
      return t('settings.mcp.status.connected')
    case 'needs_auth':
      return t('settings.mcp.status.loginRequired')
    case 'error':
      return t('settings.mcp.status.error')
    default:
      return t('settings.mcp.status.configured')
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
  const { t } = useTranslation()
  const [tab, setTab] = useState<TabId>('general')

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
    // Full-screen app-like overlay (mirrors the Vue .settings-shell): opaque
    // background covering the entire window, with its own left rail + inset
    // content panel — NOT a small centered dialog. z-modal covers the TopBar.
    <div
      className="settings-shell fixed inset-0 box-border flex overflow-hidden"
      style={{ backgroundColor: 'var(--color-background)', zIndex: 'var(--z-modal)' }}
    >
      <div className="titlebar-drag" data-tauri-drag-region aria-hidden="true" />
      {/* Left rail: shell tone, same width as the workspace sidebar, no border.
          Holds the vertical section nav (full-width buttons, active one
          highlighted) with a "Close" action pinned to the bottom. */}
      <nav
        className="flex w-[var(--sidebar-width,20rem)] shrink-0 flex-col gap-0.5 overflow-y-auto p-3"
        style={{ backgroundColor: 'var(--color-background)' }}
      >
        {TABS.map((tabItem) => {
          const active = tab === tabItem.id
          return (
            <button
              key={tabItem.id}
              type="button"
              onClick={() => setTab(tabItem.id)}
              className="relative flex h-8 w-full items-center gap-2.5 rounded-[var(--radius-md)] px-2.5 text-left text-[13px] transition-colors duration-[var(--duration-fast,150ms)] hover:bg-[var(--color-secondary)]"
              style={
                active
                  ? { color: 'var(--color-foreground)', backgroundColor: 'var(--color-secondary)', fontWeight: 500 }
                  : { color: 'var(--color-muted-foreground)', backgroundColor: 'transparent' }
              }
            >
              {active && (
                <span
                  className="absolute left-0 top-1/2 h-4 w-[3px] -translate-y-1/2 rounded-full"
                  style={{ backgroundColor: 'var(--color-accent-neutral)' }}
                />
              )}
              <tabItem.Icon className="h-3.5 w-3.5 shrink-0" />
              <span className="truncate">{t(`settings.tabs.${tabItem.id}`)}</span>
            </button>
          )
        })}

        {/* Close action pinned to the bottom of the rail — settings is opened
            from the sidebar's bottom gear, so returning shouldn't require
            traveling all the way back to the top. */}
        <button
          type="button"
          onClick={close}
          title={`${t('settings.backToWorkspace')} (Esc)`}
          className="group mt-auto flex h-9 items-center gap-1.5 rounded-[var(--radius-md)] px-2.5 text-[13px] font-medium text-[var(--color-muted-foreground)] transition-colors hover:bg-[var(--color-secondary)] hover:text-[var(--color-foreground)]"
        >
          <ArrowLeftIcon className="h-4 w-4 transition-transform group-hover:-translate-x-0.5" />
          {t('settings.backToWorkspace')}
        </button>
      </nav>

      {/* Right column: inset surface content panel (mirrors .settings-panel /
          .chat-panel). Only this panel scrolls; each section is centered and
          width-capped (max-w-3xl). No header bar — the rail conveys navigation. */}
      <div className="flex min-w-0 flex-1 flex-col">
        <div
          className="flex min-h-0 flex-1 flex-col overflow-hidden rounded-[var(--radius-2xl)] border border-[var(--color-border)] bg-[var(--color-surface)] shadow-[var(--shadow-sm)]"
          style={{ margin: '4px 14px 14px' }}
        >
          <div className="min-h-0 flex-1 overflow-y-auto px-8 py-7 [&>*]:mx-auto [&>*]:max-w-3xl">
            {tab === 'general' && <GeneralTab />}
            {tab === 'appearance' && <AppearanceTab />}
            {tab === 'providers' && <ProvidersTab />}
            {tab === 'mcp' && <MCPTab />}
            {tab === 'skills' && <SkillsTab />}
            {tab === 'browser' && <BrowserTab />}
            {tab === 'ssh' && <SSHTab />}
            {tab === 'channels' && <ChannelsTab />}
            {tab === 'shortcuts' && <ShortcutsTab />}
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
  const { t } = useTranslation()
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

  // Model roles: config.small_model ("provider/model", '' = unset). Options
  // come from the chat-picker payload in redux (enabled models only).
  const smallModel = useAppSelector((s) => s.model.smallModel)
  const pickerProviders = useAppSelector((s) => s.model.providers)
  const [smallSaving, setSmallSaving] = useState(false)
  const [smallError, setSmallError] = useState('')
  const [smallSaved, setSmallSaved] = useState(false)

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
    // Fresh enabled-model list + small_model value for the roles section (the
    // dialog can open long after boot; another client may have changed both).
    void refreshModels()
    void dispatch(loadConfig())
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  // Set or clear config.small_model. The select is controlled by redux state,
  // so a failed save simply never moves it — no manual revert needed.
  async function changeSmallModel(value: string) {
    setSmallError('')
    setSmallSaved(false)
    let provider = ''
    let model = ''
    if (value) {
      // Provider ids never contain '/'; model ids may (custom endpoints).
      const i = value.indexOf('/')
      if (i >= 0) {
        provider = value.slice(0, i)
        model = value.slice(i + 1)
      } else {
        // Malformed ref without a separator (hand-edited config surfaced via
        // the "unavailable" option): pass it as model-only so the API rejects
        // it intact instead of us mangling the provider name.
        model = value
      }
    }
    setSmallSaving(true)
    try {
      await api.setSmallModel(provider, model)
      dispatch(modelActions.setSmallModel(value))
      setSmallSaved(true)
      window.setTimeout(() => setSmallSaved(false), 2000)
    } catch (err) {
      setSmallError(err instanceof Error ? err.message : String(err))
    } finally {
      setSmallSaving(false)
    }
  }

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

  // Enabled models grouped by provider for the small-model picker; providers
  // with nothing enabled disappear entirely.
  const roleOptions = pickerProviders
    .map((p) => ({ ...p, models: p.models.filter((m) => m.enabled) }))
    .filter((p) => p.models.length > 0)
  // A configured ref whose model was since disabled/removed still renders (as
  // its raw ref, marked unavailable) so it can be seen and cleared.
  const smallModelListed =
    smallModel === '' || roleOptions.some((p) => p.models.some((m) => `${p.id}/${m.id}` === smallModel))

  return (
    <div>
      <div className="mb-5">
        <h3 className={`${SECTION_TITLE} mb-2`}>{t('settings.providers.roles.title')}</h3>
        <div className={ROW}>
          <div className="grid h-7 w-7 shrink-0 place-items-center rounded-[var(--radius-md)] text-[var(--color-muted-foreground)]">
            <BoltIcon className="h-4 w-4" />
          </div>
          <div className="min-w-0 flex-1">
            <div className="text-[12px] font-medium text-[var(--color-foreground)]">
              {t('settings.providers.roles.smallName')}
            </div>
            <div className="text-[11px] text-[var(--color-muted-foreground)]">
              {roleOptions.length === 0
                ? t('settings.providers.roles.smallNoProviders')
                : t('settings.providers.roles.smallDesc')}
            </div>
            {smallError && (
              <div className="mt-1 text-[11px] text-[var(--color-destructive)]">
                {t('settings.providers.roles.smallSaveFailed', { reason: smallError })}
              </div>
            )}
            {smallSaved && (
              <div className="mt-1 text-[11px] text-[var(--color-success)]">
                {t('settings.providers.roles.smallSaved')}
              </div>
            )}
          </div>
          <select
            value={smallModel}
            disabled={smallSaving || (roleOptions.length === 0 && smallModel === '')}
            onChange={(e) => void changeSmallModel(e.target.value)}
            className={INPUT_SM}
            style={{ width: '15rem' }}
          >
            <option value="">{t('settings.providers.roles.smallUnset')}</option>
            {!smallModelListed && (
              <option value={smallModel}>
                {smallModel} — {t('settings.providers.roles.smallUnavailable')}
              </option>
            )}
            {roleOptions.map((p) => (
              <optgroup key={p.id} label={p.name || p.id}>
                {p.models.map((m) => (
                  <option key={m.id} value={`${p.id}/${m.id}`}>
                    {m.name || m.id}
                  </option>
                ))}
              </optgroup>
            ))}
          </select>
        </div>
      </div>

      <div className="mb-4 flex items-center justify-between">
        <div className="flex items-baseline gap-2">
          <h3 className={SECTION_TITLE}>{t('settings.providers.title')}</h3>
          <span className="font-mono text-[11px] text-[var(--color-muted-foreground)]">{providers.length}</span>
        </div>
        <button type="button" className={`${BTN_SECONDARY} ${BTN_SM}`} onClick={() => setAdding(true)}>
          <PlusIcon className="h-3.5 w-3.5" /> {t('settings.providers.addBtn')}
        </button>
      </div>

      {loading ? (
        <div className="animate-pulse py-6 text-center text-xs text-[var(--color-muted-foreground)]">{t('settings.providers.loadingHint')}</div>
      ) : providers.length === 0 ? (
        <EmptyState
          Icon={CpuChipIcon}
          title={t('settings.providers.noneConfigured')}
          hint={t('settings.providers.noneHint', { btn: t('settings.providers.addBtn') })}
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
  const { t } = useTranslation()
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
          <ProviderIcon provider={provider.id} custom={provider.custom} size={18} />
        </div>
        <div className="min-w-0 flex-1">
          <div className="flex items-center gap-2 text-[13px] font-semibold text-[var(--color-foreground)]">
            <span className="truncate">{provider.name || provider.id}</span>
            {activeProvider === provider.id && <span className={CHIP_ACCENT}>{t('settings.appearance.active')}</span>}
            {provider.custom && <span className={CHIP}>{t('settings.providers.custom')}</span>}
          </div>
          <div className="mt-0.5 truncate font-mono text-[11px] text-[var(--color-muted-foreground)]">
            {provider.base_url || (provider.api_key_set ? t('settings.providers.apiKey') : '—')}
          </div>
        </div>
        <div className="flex shrink-0 items-center gap-1.5">
          {deleteConfirm ? (
            activeModelName ? (
              <div className="flex max-w-[280px] flex-col gap-2 rounded-[var(--radius-md)] border border-[var(--color-warning-fg)] bg-[var(--color-warning-bg)] p-2.5 text-right">
                <div>
                  <div className="text-[12px] font-semibold text-[var(--color-warning-fg)]">{t('settings.providers.deleteInUseTitle')}</div>
                  <div className="mt-0.5 text-[10.5px] text-[var(--color-muted-foreground)]">
                    {t('settings.providers.deleteInUseBody', { model: activeModelName })}
                  </div>
                </div>
                <div className="flex flex-wrap justify-end gap-1.5">
                  <button type="button" className={`${BTN_GHOST} ${BTN_XS}`} onClick={() => setDeleteConfirm(false)}>
                    {t('common.cancel')}
                  </button>
                </div>
              </div>
            ) : (
              <>
                <button type="button" className={`${BTN_DANGER} ${BTN_XS}`} onClick={onDelete}>
                  {t('common.delete')}
                </button>
                <button type="button" className={`${BTN_GHOST} ${BTN_XS}`} onClick={() => setDeleteConfirm(false)}>
                  {t('common.cancel')}
                </button>
              </>
            )
          ) : (
            <>
              <button type="button" title={t('settings.providers.edit')} className={`${BTN_GHOST} ${BTN_XS}`} onClick={onEdit}>
                <PencilSquareIcon className="h-3.5 w-3.5" />
              </button>
              <button type="button" title={t('settings.providers.remove')} className={`${BTN_GHOST} ${BTN_XS}`} onClick={() => setDeleteConfirm(true)}>
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
            <b className="font-semibold text-[var(--color-foreground)]">{addedCount}</b>/{catalog.length} {t('settings.providers.models')}
          </span>
          <div className="ml-auto flex items-center gap-1.5">
            <button
              type="button"
              className={`${BTN_SECONDARY} ${BTN_XS}`}
              onClick={onRefreshCatalog}
              title={t('settings.providers.refreshCatalog')}
            >
              ↻
            </button>
            <button type="button" className={`${BTN_SECONDARY} ${BTN_XS}`} onClick={onAddCustomModel}>
              <PlusIcon className="h-3.5 w-3.5" /> {t('settings.providers.addCustomModel')}
            </button>
          </div>
        </div>

        {/* Inline catalog (open by default) */}
        <div className="mt-1.5 border-t border-dashed border-[var(--color-border)] pt-2">
          {catalogLoading && catalog.length === 0 ? (
            <div className="animate-pulse py-3 text-center text-[10px] text-[var(--color-muted-foreground)]">
              {t('settings.providers.loadingHint')}
            </div>
          ) : (
            <>
              <input
                value={search}
                onChange={(e) => setSearch(e.target.value)}
                type="text"
                placeholder={t('settings.providers.catalogSearch')}
                className={INPUT_SM + ' mb-1.5'}
              />
              {filtered.length === 0 ? (
                <div className="py-3 text-center text-[10px] text-[var(--color-muted-foreground)]">
                  {provider.custom ? t('settings.providers.noCatalog') : t('settings.providers.allConfigured')}
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
                          {cm.reasoning ? ` · ${t('settings.providers.customReasoning')}` : ''}
                          {cm.attachment ? ` · ${t('settings.providers.supportImage')}` : ''}
                        </div>
                      </div>
                      {cm.added && cm.custom ? (
                        <div className="flex items-center gap-1">
                          <button
                            type="button"
                            title={t('settings.providers.editModel')}
                            className={`${BTN_GHOST} ${BTN_XS}`}
                            onClick={() => onEditCustomModel(cmToDetail(cm))}
                          >
                            <PencilSquareIcon className="h-3.5 w-3.5" />
                          </button>
                          <button
                            type="button"
                            title={t('settings.providers.remove')}
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
                          title={cm.added ? t('settings.providers.hideModel') : t('settings.providers.showModel')}
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
  const { t } = useTranslation()
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
      setError(t('settings.providers.customIdRequired'))
      return
    }
    if (!isEdit && !apiKey.trim()) {
      setError(t('settings.providers.enterApiKey'))
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
      setError(err instanceof Error ? err.message : t('settings.providers.connectFailed'))
    } finally {
      setSaving(false)
    }
  }

  return (
    <form onSubmit={save}>
      <div className="mb-4 flex items-center justify-between">
        <h3 className={SECTION_TITLE}>{isEdit ? t('settings.providers.editProvider') : t('settings.providers.add')}</h3>
        <button type="button" className={`${BTN_GHOST} ${BTN_SM}`} onClick={onCancel}>
          ← {t('common.back')}
        </button>
      </div>

      <div className="rounded-[var(--radius-lg)] border border-[var(--color-border)] bg-[var(--color-surface)]">
        <div className="border-b border-[var(--color-border)] bg-[var(--color-muted)] px-3.5 py-2.5 text-[11px] font-semibold uppercase tracking-wide text-[var(--color-muted-foreground)]">
          {isEdit ? editing!.name || editing!.id : t('settings.providers.add')}
        </div>
        <div className="p-4">
          {/* Provider selection (add mode only) */}
          {!isEdit && (
            <Field label={t('settings.providers.selectProvider')}>
              <Segmented
                value={mode}
                onChange={(m) => setMode(m)}
                options={[
                  { value: 'registry', label: t('settings.providers.selectProvider') },
                  { value: 'custom', label: t('settings.providers.custom') },
                ]}
              />
              <div className="mt-2">
                {mode === 'registry' ? (
                  <select
                    value={selId}
                    onChange={(e) => setSelId(e.target.value)}
                    className={INPUT}
                  >
                    <option value="">{t('settings.providers.selectProvider')}</option>
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
                    placeholder={t('settings.providers.customIdPlaceholder')}
                    className={INPUT_MONO}
                  />
                )}
              </div>
            </Field>
          )}

          <Field label={t('settings.providers.apiKey')}>
            <input
              value={apiKey}
              onChange={(e) => setApiKey(e.target.value)}
              type="password"
              placeholder={isEdit ? t('settings.providers.apiKeyUnchanged') : 'sk-…'}
              className={INPUT_MONO}
            />
          </Field>

          {(mode === 'custom' || isEdit) && (
            <Field label={t('settings.providers.customName')}>
              <input
                value={name}
                onChange={(e) => setName(e.target.value)}
                type="text"
                placeholder={t('settings.providers.customNamePlaceholder')}
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
            {t('settings.providers.advanced')}
          </button>

          {advancedOpen && (
            <div className="mb-3 rounded-[var(--radius-md)] border border-[var(--color-border)] bg-[var(--color-muted)] p-3">
              <Field label={t('settings.providers.endpoint')}>
                <input
                  value={baseUrl}
                  onChange={(e) => setBaseUrl(e.target.value)}
                  type="text"
                  placeholder={t('settings.providers.endpointPlaceholder')}
                  className={INPUT_MONO}
                />
              </Field>

              <div className="mb-3.5">
                <div className="mb-1.5 flex items-center justify-between">
                  <label className={LABEL + ' !mb-0'}>{t('settings.providers.headers')}</label>
                  <button
                    type="button"
                    className={`${BTN_GHOST} ${BTN_XS}`}
                    onClick={() => setHeaders((h) => [...h, { key: '', value: '' }])}
                  >
                    + {t('settings.providers.addHeader')}
                  </button>
                </div>
                {headers.length === 0 && (
                  <div className="text-[11px] text-[var(--color-muted-foreground)]">{t('settings.providers.headersHint')}</div>
                )}
                {headers.map((h, i) => (
                  <div key={i} className="mb-2 flex gap-2">
                    <input
                      value={h.key}
                      onChange={(e) => setHeaders((prev) => prev.map((x, j) => (j === i ? { ...x, key: e.target.value } : x)))}
                      type="text"
                      placeholder={t('settings.providers.headerKey')}
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

              <Field label={t('settings.providers.advanced')}>
                <div className="space-y-2">
                  <div className="flex items-center justify-between rounded-[var(--radius-md)] border border-[var(--color-border)] bg-[var(--color-surface)] px-3 py-2">
                    <div>
                      <div className="text-[12px] font-medium text-[var(--color-foreground)]">{t('settings.providers.supportImage')}</div>
                      <div className="text-[10px] text-[var(--color-muted-foreground)]">{t('settings.providers.supportImageDesc')}</div>
                    </div>
                    <Switch on={vision} onClick={() => setVision((v) => !v)} />
                  </div>
                  <div className="flex items-center justify-between rounded-[var(--radius-md)] border border-[var(--color-border)] bg-[var(--color-surface)] px-3 py-2">
                    <div>
                      <div className="text-[12px] font-medium text-[var(--color-foreground)]">{t('settings.providers.customReasoning')}</div>
                      <div className="text-[10px] text-[var(--color-muted-foreground)]">{t('settings.providers.customReasoningDesc')}</div>
                    </div>
                    <Switch on={thinking} onClick={() => setThinking((v) => !v)} />
                  </div>
                  <div className="flex items-center justify-between rounded-[var(--radius-md)] border border-[var(--color-border)] bg-[var(--color-surface)] px-3 py-2">
                    <div className="text-[12px] font-medium text-[var(--color-foreground)]">{t('settings.providers.supportReasoning')}</div>
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
            {t('common.cancel')}
          </button>
          <button type="submit" disabled={saving} className={BTN_PRIMARY}>
            {saving ? t('settings.providers.saving') : isEdit ? t('settings.mcp.save') : t('settings.providers.addBtn')}
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
  const { t } = useTranslation()
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
      setError(t('settings.providers.customModelRequired'))
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
      setError(err instanceof Error ? err.message : t('settings.providers.connectFailed'))
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
        {isEdit ? t('settings.providers.editModel') : t('settings.providers.addModel')}
      </div>
      <div className="grid grid-cols-2 gap-2.5">
        <Field label={t('settings.providers.customModelId')}>
          <input value={id} onChange={(e) => setId(e.target.value)} type="text" placeholder="gpt-4o" className={INPUT_MONO} />
        </Field>
        <Field label={t('settings.providers.customName')}>
          <input value={name} onChange={(e) => setName(e.target.value)} type="text" placeholder="GPT-4o" className={INPUT} />
        </Field>
        <Field label={t('settings.providers.contextWindow')}>
          <input
            value={context}
            onChange={(e) => setContext(e.target.value)}
            type="number"
            placeholder="128000"
            className={INPUT}
          />
        </Field>
        <Field label={t('settings.providers.effortTiers')}>
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
          <Switch on={reasoning} onClick={() => setReasoning((v) => !v)} /> {t('settings.providers.customReasoning')}
        </label>
        <label className="flex items-center gap-2 text-[11px] text-[var(--color-foreground)]">
          <Switch on={attachment} onClick={() => setAttachment((v) => !v)} /> {t('settings.providers.supportImage')}
        </label>
      </div>
      {error && <div className="mb-2 text-[11px] text-[var(--color-destructive)]">{error}</div>}
      <div className="flex justify-end gap-2">
        <button type="button" className={`${BTN_GHOST} ${BTN_SM}`} onClick={onCancel}>
          {t('common.cancel')}
        </button>
        <button type="submit" disabled={saving} className={`${BTN_PRIMARY} ${BTN_SM}`}>
          {saving ? t('settings.providers.saving') : t('settings.mcp.save')}
        </button>
      </div>
    </form>
  )
}

// ════════════════════════════════════════════════════════════════════════════
// General tab — default mode, auto-approve, max iterations, language
// (matches the Vue SettingsDialog 'general' tab; a functional port.)
// ════════════════════════════════════════════════════════════════════════════

function GeneralTab() {
  const dispatch = useAppDispatch()
  const { t, i18n } = useTranslation()
  const autoApprove = useAppSelector((s) => s.model.autoApprove)
  const wsConnected = useAppSelector((s) => s.session.wsConnected)
  const tokenSnapshot = useAppSelector((s) => s.chat.tokenSnapshot)
  const bleAvailable = useAppSelector((s) => s.ui.bleAvailable)
  const bleEnabled = useAppSelector((s) => s.ui.bleEnabled)
  const [bleSaving, setBLESaving] = useState(false)

  const tokenPct = tokenSnapshot?.model_context_limit
    ? Math.round((tokenSnapshot.total_tokens / tokenSnapshot.model_context_limit) * 100)
    : 0

  async function toggleAutoApprove() {
    const next = !autoApprove
    try {
      await api.setApprovalMode(next)
      dispatch(modelActions.setAutoApprove(next))
      dispatch(modelActions.setMode(next ? 'full_access' : 'approval'))
    } catch (err) {
      console.error('Failed to toggle auto-approve:', err)
    }
  }

  async function toggleBLE() {
    if (bleSaving) return
    setBLESaving(true)
    const next = !bleEnabled
    try {
      const res = await api.setChannelBLE(next)
      dispatch(uiActions.setBLEState({ available: bleAvailable, enabled: res.enabled }))
    } catch (err) {
      console.error('Failed to toggle BLE:', err)
    } finally {
      setBLESaving(false)
    }
  }

  async function changeLanguage(code: string) {
    await setLocale(code as SupportedLocale)
  }

  return (
    <div className="space-y-5">
      <h3 className={SECTION_TITLE}>{t('settings.tabs.general')}</h3>

      <div className={ROW}>
        <div className="grid h-7 w-7 shrink-0 place-items-center rounded-[var(--radius-md)] text-[var(--color-muted-foreground)]">
          <span
            className="h-2 w-2 rounded-full"
            style={{ backgroundColor: wsConnected ? 'var(--color-success)' : 'var(--color-border)' }}
          />
        </div>
        <div className="min-w-0 flex-1">
          <div className="text-[12px] font-medium text-[var(--color-foreground)]">{t('settings.general.serverState')}</div>
          <div
            className="text-[11px]"
            style={{ color: wsConnected ? 'var(--color-success)' : 'var(--color-muted-foreground)' }}
          >
            {wsConnected ? t('settings.general.serverOnline') : t('settings.general.serverOffline')}
          </div>
        </div>
      </div>

      {tokenSnapshot && (
        <div className={ROW}>
          <div className="min-w-0 flex-1">
            <div className="text-[12px] font-medium text-[var(--color-foreground)]">{t('settings.general.tokenUsage')}</div>
            <div className="mt-1.5 flex items-center gap-2">
              <div className="h-1.5 flex-1 overflow-hidden rounded-full bg-[var(--color-muted)]">
                <div
                  className="h-full rounded-full transition-all"
                  style={{
                    width: `${tokenPct}%`,
                    backgroundColor: tokenPct > 80 ? 'var(--color-destructive)' : tokenPct > 50 ? 'var(--color-warning-fg)' : 'var(--color-accent-neutral)',
                  }}
                />
              </div>
              <span className="font-mono text-[10px] text-[var(--color-muted-foreground)]">
                {tokenSnapshot.total_tokens.toLocaleString()}
                {tokenSnapshot.model_context_limit ? ` / ${tokenSnapshot.model_context_limit.toLocaleString()}` : ''}
              </span>
            </div>
          </div>
        </div>
      )}

      <div className="text-[10px] font-medium uppercase tracking-wider text-[var(--color-muted-foreground)]">
        {t('settings.general.preferences')}
      </div>

      {/* Auto-approve */}
      <div className={ROW}>
        <div className="grid h-7 w-7 shrink-0 place-items-center rounded-[var(--radius-md)] text-[var(--color-muted-foreground)]">
          <ShieldCheckIcon className="h-4 w-4" />
        </div>
        <div className="min-w-0 flex-1">
          <div className="text-[12px] font-medium text-[var(--color-foreground)]">{t('settings.general.autoApproveTitle')}</div>
          <div className="text-[11px] text-[var(--color-muted-foreground)]">
            {t('settings.general.autoApproveDesc')}
          </div>
        </div>
        <Switch on={autoApprove} onClick={toggleAutoApprove} title={autoApprove ? t('common.disable') : t('common.enable')} />
      </div>

      {bleAvailable && (
        <div className={ROW}>
          <div className="grid h-7 w-7 shrink-0 place-items-center rounded-[var(--radius-md)] text-[var(--color-muted-foreground)]">
            <BoltIcon className="h-4 w-4" />
          </div>
          <div className="min-w-0 flex-1">
            <div className="text-[12px] font-medium text-[var(--color-foreground)]">{t('settings.general.bleTitle')}</div>
            <div className="text-[11px] text-[var(--color-muted-foreground)]">{t('settings.general.bleDesc')}</div>
          </div>
          <Switch
            on={bleEnabled}
            onClick={toggleBLE}
            disabled={bleSaving}
            title={bleEnabled ? t('common.disable') : t('common.enable')}
          />
        </div>
      )}

      {/* Language */}
      <div className={ROW}>
        <div className="grid h-7 w-7 shrink-0 place-items-center rounded-[var(--radius-md)] text-[var(--color-muted-foreground)]">
          <GlobeAltIcon className="h-4 w-4" />
        </div>
        <div className="min-w-0 flex-1">
          <div className="text-[12px] font-medium text-[var(--color-foreground)]">{t('settings.general.languageTitle')}</div>
          <div className="text-[11px] text-[var(--color-muted-foreground)]">{t('settings.general.languageDesc')}</div>
        </div>
        <select
          value={i18n.language}
          onChange={(e) => changeLanguage(e.target.value)}
          className={INPUT_SM}
          style={{ width: '9rem' }}
        >
          {SUPPORTED_LOCALES.map((locale) => (
            <option key={locale} value={locale}>
              {LOCALE_LABELS[locale]}
            </option>
          ))}
        </select>
      </div>
    </div>
  )
}

// ════════════════════════════════════════════════════════════════════════════
// Shortcuts tab — static keyboard shortcuts reference
// ════════════════════════════════════════════════════════════════════════════

const SHORTCUTS: { keys: string; labelKey: string }[] = [
  { keys: '⌘K', labelKey: 'commandPalette' },
  { keys: '⌘N', labelKey: 'newChat' },
  { keys: '⌘,', labelKey: 'openSettings' },
  { keys: '⇧⌘P', labelKey: 'planMode' },
  { keys: '⇧⌘E', labelKey: 'filesPanel' },
  { keys: '⇧⌘G', labelKey: 'changesPanel' },
  { keys: '⌘`', labelKey: 'toggleTerminal' },
  { keys: '⌘L', labelKey: 'focusInput' },
]

function ShortcutsTab() {
  const { t } = useTranslation()
  return (
    <div className="space-y-4">
      <h3 className={SECTION_TITLE}>{t('settings.shortcuts.title')}</h3>
      <div className="overflow-hidden rounded-[var(--radius-lg)] border border-[var(--color-border)] bg-[var(--color-surface)]">
        {SHORTCUTS.map((s, i) => (
          <div
            key={s.keys}
            className="flex items-center justify-between px-3.5 py-2.5"
            style={{ borderTop: i === 0 ? 'none' : '1px solid var(--color-border)' }}
          >
            <span className="text-[12px] text-[var(--color-foreground)]">
              {t(`settings.shortcuts.items.${s.labelKey}`)}
            </span>
            <kbd
              className="inline-flex h-6 min-w-[28px] items-center justify-center rounded-[var(--radius-sm)] border border-[var(--color-border)] bg-[var(--color-muted)] px-2 font-mono text-[11px] text-[var(--color-foreground)]"
            >
              {s.keys}
            </kbd>
          </div>
        ))}
      </div>
      <div className="text-[10.5px] leading-relaxed text-[var(--color-muted-foreground)]">
        {t('settings.shortcuts.windowsHint')}
      </div>
    </div>
  )
}

// ════════════════════════════════════════════════════════════════════════════
// Channels tab (in settings) — minimal: links to the full Channels page.
// ════════════════════════════════════════════════════════════════════════════

function ChannelsTab() {
  const { t } = useTranslation()
  const dispatch = useAppDispatch()
  return (
    <div className="space-y-4">
      <h3 className={SECTION_TITLE}>{t('settings.channels.title')}</h3>
      <div className={ROW}>
        <div className="grid h-7 w-7 shrink-0 place-items-center rounded-[var(--radius-md)] text-[var(--color-muted-foreground)]">
          <ChatBubbleOvalLeftIcon className="h-4 w-4" />
        </div>
        <div className="min-w-0 flex-1">
          <div className="text-[12px] font-medium text-[var(--color-foreground)]">{t('settings.channels.manageTitle')}</div>
          <div className="text-[11px] text-[var(--color-muted-foreground)]">
            {t('settings.channels.manageDesc')}
          </div>
        </div>
        <button
          type="button"
          className={`${BTN_SECONDARY} ${BTN_SM}`}
          onClick={() => {
            dispatch(uiActions.setView('channels'))
            dispatch(uiActions.setSettingsOpen(false))
          }}
        >
          {t('settings.channels.openPage')} <ArrowRightIcon className="h-3.5 w-3.5" />
        </button>
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
  const { t } = useTranslation()
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
      setFormError(t('settings.mcp.serverName') + ' *')
      return
    }
    if (form.transport === 'local' && !form.command.trim()) {
      setFormError(t('settings.mcp.command') + ' *')
      return
    }
    if (form.transport !== 'local' && !form.url.trim()) {
      setFormError(t('settings.mcp.url') + ' *')
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
      setFormError(err instanceof Error ? err.message : t('settings.mcp.status.error'))
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
    setLoginMsg(t('settings.mcp.waitingBrowser'))
    try {
      await api.mcpLogin(name)
    } catch (err) {
      setLoginMsg(err instanceof Error ? err.message : t('settings.mcp.status.error'))
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
          setLoginMsg(st.message || t('settings.mcp.status.error'))
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
          <h3 className={SECTION_TITLE}>{editing ? t('settings.mcp.editServer') : t('settings.mcp.addServer')}</h3>
          <button type="button" className={`${BTN_GHOST} ${BTN_SM}`} onClick={() => setEditing(null)}>
            ← {t('common.back')}
          </button>
        </div>

        <div className="overflow-hidden rounded-[var(--radius-lg)] border border-[var(--color-border)] bg-[var(--color-surface)]">
          <div className="border-b border-[var(--color-border)] bg-[var(--color-muted)] px-3.5 py-2.5 text-[11px] font-semibold uppercase tracking-wide text-[var(--color-muted-foreground)]">
            {editing ? t('settings.mcp.editServer') : t('settings.mcp.addServer')}
          </div>
          <div className="p-4">
            <Field label={t('settings.mcp.serverName')}>
              <input
                value={form.name}
                onChange={(e) => setForm((f) => ({ ...f, name: e.target.value }))}
                disabled={!!editing}
                type="text"
                placeholder="my-server"
                className={INPUT}
              />
            </Field>

            <Field label={t('settings.mcp.transport')}>
              <Segmented
                value={form.transport}
                onChange={(t) => setForm((f) => ({ ...f, transport: t }))}
                options={[
                  { value: 'local', label: t('settings.mcp.transportLocal') },
                  { value: 'http', label: 'HTTP' },
                  { value: 'sse', label: 'SSE' },
                ]}
              />
            </Field>

            {form.transport !== 'local' ? (
              <>
                <Field label={t('settings.mcp.url')}>
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
                    <label className={LABEL + ' !mb-0'}>{t('settings.providers.headers')}</label>
                    <button
                      type="button"
                      className={`${BTN_GHOST} ${BTN_XS}`}
                      onClick={() => setForm((f) => ({ ...f, headers: [...f.headers, { key: '', value: '' }] }))}
                    >
                      + {t('settings.providers.addHeader')}
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
                        placeholder={t('settings.providers.headerKey')}
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
                  <div className="text-[12px] font-medium text-[var(--color-foreground)]">{t('settings.mcp.useOauth')}</div>
                  <Switch
                    on={form.oauthEnabled}
                    onClick={() => setForm((f) => ({ ...f, oauthEnabled: !f.oauthEnabled }))}
                  />
                </div>

                {form.oauthEnabled && (
                  <div className="mb-3.5 space-y-3">
                    <Field label={t('settings.mcp.oauthClientId')}>
                      <input
                        value={form.clientId}
                        onChange={(e) => setForm((f) => ({ ...f, clientId: e.target.value }))}
                        type="text"
                        placeholder="Optional — leave blank to auto-register"
                        className={INPUT_MONO}
                      />
                    </Field>
                    <Field label={t('settings.mcp.oauthClientSecret')}>
                      <input
                        value={form.clientSecret}
                        onChange={(e) => setForm((f) => ({ ...f, clientSecret: e.target.value }))}
                        type="password"
                        placeholder="Optional (confidential clients)"
                        className={INPUT_MONO}
                      />
                    </Field>
                    <Field label={t('settings.mcp.oauthScopes')}>
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

                <Field label={t('settings.mcp.timeout')}>
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
                <Field label={t('settings.mcp.command')}>
                  <input
                    value={form.command}
                    onChange={(e) => setForm((f) => ({ ...f, command: e.target.value }))}
                    type="text"
                    placeholder="npx"
                    className={INPUT_MONO}
                  />
                </Field>
                <Field label={t('settings.mcp.arguments')}>
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
              {t('common.cancel')}
            </button>
            <button type="submit" disabled={saving} className={BTN_PRIMARY}>
              {saving ? t('settings.providers.saving') : t('settings.mcp.save')}
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
          <h3 className={SECTION_TITLE}>{t('settings.mcp.title')}</h3>
          <span className="font-mono text-[11px] text-[var(--color-muted-foreground)]">{serverList.length}</span>
        </div>
        <button type="button" className={`${BTN_SECONDARY} ${BTN_SM}`} onClick={openAdd}>
          <PlusIcon className="h-3.5 w-3.5" /> {t('settings.mcp.addServer')}
        </button>
      </div>

      {loading ? (
        <div className="animate-pulse py-6 text-center text-xs text-[var(--color-muted-foreground)]">{t('common.loading')}</div>
      ) : serverList.length === 0 ? (
        <EmptyState
          Icon={ServerStackIcon}
          title={t('settings.mcp.noneConfigured')}
          hint={t('settings.mcp.noneHint', { btn: t('settings.mcp.add') })}
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
                        ● {mcpStatusLabel(info, t)}
                      </span>
                    </div>
                    <div className="truncate font-mono text-[11px] text-[var(--color-muted-foreground)]">
                      {info.type === 'sse' || info.type === 'http' ? info.url : info.command}
                    </div>
                  </div>
                  <div className="flex shrink-0 items-center gap-1.5">
                    <button type="button" className={`${BTN_GHOST} ${BTN_XS}`} onClick={() => openEdit(info)}>
                      {t('common.edit')}
                    </button>
                    <button
                      type="button"
                      className={`${BTN_GHOST} ${BTN_XS} text-[var(--color-destructive)]`}
                      onClick={() => remove(name)}
                    >
                      {t('common.delete')}
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
                        ? t('settings.mcp.waitingBrowser')
                        : info.has_auth
                          ? t('settings.mcp.reauth')
                          : t('settings.mcp.login')}
                    </button>
                    {info.has_auth && <span className={CHIP + ' !bg-[var(--color-success-bg)] !text-[var(--color-success-fg)]'}>{t('settings.mcp.authenticated')}</span>}
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
  const { t } = useTranslation()
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
        <h3 className={SECTION_TITLE}>{t('settings.skills.title')}</h3>
        <span className="font-mono text-[11px] text-[var(--color-muted-foreground)]">{skills.length}</span>
      </div>
      <div className="mb-3 flex items-center gap-2">
        <Segmented
          value={filter}
          onChange={(f) => setFilter(f)}
          options={[
            { value: 'all', label: t('settings.skills.filterAll') },
            { value: 'local', label: t('settings.skills.filterLocal') },
            { value: 'builtin', label: t('settings.skills.filterBuiltin') },
          ]}
        />
        <input
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          type="text"
          placeholder={t('settings.skills.search')}
          className={INPUT}
          style={{ flex: 1 }}
        />
      </div>

      {loading ? (
        <div className="animate-pulse py-6 text-center text-xs text-[var(--color-muted-foreground)]">{t('settings.skills.loadingHint')}</div>
      ) : filtered.length === 0 ? (
        <EmptyState Icon={SparklesIcon} title={t('settings.skills.none')} hint={t('settings.skills.noneHint')} />
      ) : (
        <div className="space-y-2">
          {filtered.map((s) => (
            <div key={s.name} className={ROW} data-muted={!s.enabled ? 'true' : 'false'}>
              <div className="min-w-0 flex-1">
                <div className="flex items-center gap-2 text-[12px] font-medium text-[var(--color-foreground)]">
                  {s.name}
                  {s.builtin && <span className={CHIP}>{t('settings.skills.builtin')}</span>}
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
  const { t } = useTranslation()
  const dispatch = useAppDispatch()
  const storeTheme = useAppSelector((s) => s.ui.theme)
  // Resolve the active theme: prefer store, fall back to localStorage, then default.
  const [active, setActive] = useState(() => {
    let th = storeTheme
    if (!th || th === 'system' || !THEMES.some((x) => x.id === th)) {
      try {
        th = localStorage.getItem('jcode-theme') || 'jcode-dark'
      } catch {
        th = 'jcode-dark'
      }
    }
    return th
  })

  function choose(name: string) {
    setActive(name)
    dispatch(uiActions.setTheme(name))
    applyTheme(name)
  }

  const darkThemes = THEMES.filter((th) => th.appearance === 'dark')
  const lightThemes = THEMES.filter((th) => th.appearance === 'light')

  const ThemeButton = ({ theme }: { theme: { id: string; label: string; appearance: string } }) => (
    <button
      type="button"
      data-theme={theme.id}
      onClick={() => choose(theme.id)}
      className="cursor-pointer overflow-hidden rounded-[var(--radius-md)] text-left transition-transform active:scale-[0.98]"
      style={{
        border: active === theme.id ? '2px solid var(--color-accent-neutral)' : '1px solid var(--color-border)',
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
        <span className="truncate text-[11px] font-medium text-[var(--color-foreground)]">{theme.label}</span>
        {active === theme.id && (
          <CheckIcon className="h-3.5 w-3.5 shrink-0" style={{ color: 'var(--color-accent-neutral)' }} />
        )}
      </div>
    </button>
  )

  return (
    <div className="space-y-5">
      <h3 className={SECTION_TITLE}>{t('settings.appearance.theme')}</h3>
      <div>
        <div className="mb-2 text-[10px] font-medium text-[var(--color-muted-foreground)]">{t('settings.appearance.dark')}</div>
        <div className="grid grid-cols-2 gap-2">
          {darkThemes.map((th) => (
            <ThemeButton key={th.id} theme={th} />
          ))}
        </div>
      </div>
      <div>
        <div className="mb-2 text-[10px] font-medium text-[var(--color-muted-foreground)]">{t('settings.appearance.light')}</div>
        <div className="grid grid-cols-2 gap-2">
          {lightThemes.map((th) => (
            <ThemeButton key={th.id} theme={th} />
          ))}
        </div>
      </div>
      <div className="text-[10px] leading-relaxed text-[var(--color-muted-foreground)]">
        {t('settings.appearance.terminalHint', { cmd: '/theme' })}
      </div>
    </div>
  )
}

// ════════════════════════════════════════════════════════════════════════════
// Browser tab — config + site permissions
// ════════════════════════════════════════════════════════════════════════════

function BrowserTab() {
  const { t } = useTranslation()
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
        <h3 className={SECTION_TITLE}>{t('settings.browser.title')}</h3>
        <p className="mt-0.5 text-[12px] text-[var(--color-muted-foreground)]">{t('settings.browser.subtitle')}</p>
      </div>

      <div className={ROW}>
        <div className="grid h-7 w-7 shrink-0 place-items-center rounded-[var(--radius-md)] text-[var(--color-muted-foreground)]">
          <GlobeAltIcon className="h-4 w-4" />
        </div>
        <div className="min-w-0 flex-1">
          <div className="text-[12px] font-medium text-[var(--color-foreground)]">{t('settings.browser.enableTitle')}</div>
          <div className="text-[11px] text-[var(--color-muted-foreground)]">{t('settings.browser.enableDesc')}</div>
        </div>
        <Switch on={cfg.enabled} onClick={() => patch({ enabled: !cfg.enabled })} />
      </div>

      {cfg.enabled && (
        <>
          <div className="mb-2 mt-5 text-[11px] font-medium uppercase tracking-wide text-[var(--color-muted-foreground)]">
            {t('settings.browser.control')}
          </div>
          <div className="space-y-2">
            <div className={ROW}>
              <div className="grid h-7 w-7 shrink-0 place-items-center rounded-[var(--radius-md)] text-[var(--color-muted-foreground)]">
                <ComputerDesktopIcon className="h-4 w-4" />
              </div>
              <div className="min-w-0 flex-1">
                <div className="text-[12px] font-medium text-[var(--color-foreground)]">{t('settings.browser.managedChrome')}</div>
                <div className="text-[11px] text-[var(--color-muted-foreground)]">
                  {st?.chrome_found ? st.chrome_version || st.chrome_path : t('settings.browser.noChromeDetected')}
                </div>
              </div>
              <select
                value={cfg.backend}
                onChange={(e) => patch({ backend: e.target.value })}
                className={INPUT_SM}
                style={{ width: '8rem' }}
              >
                <option value="auto">{t('settings.browser.backendAuto')}</option>
                <option value="managed">{t('settings.browser.backendManaged')}</option>
                <option value="extension">{t('settings.browser.backendExtension')}</option>
              </select>
            </div>
            <div className={ROW}>
              <div className="grid h-7 w-7 shrink-0 place-items-center rounded-[var(--radius-md)] text-[var(--color-muted-foreground)]">
                <GlobeAltIcon className="h-4 w-4" />
              </div>
              <div className="min-w-0 flex-1">
                <div className="text-[12px] font-medium text-[var(--color-foreground)]">{t('settings.browser.extension')}</div>
                <div className="flex items-center gap-1.5 text-[11px] text-[var(--color-muted-foreground)]">
                  <span
                    className="h-1.5 w-1.5 shrink-0 rounded-full"
                    style={{ backgroundColor: st?.extension_online ? 'var(--color-success-fg)' : 'var(--color-border)' }}
                  />
                  {st?.extension_online ? t('settings.browser.connected') : t('settings.browser.notConnected')}
                </div>
              </div>
              {st?.extension_online && (
                <span className={CHIP + ' !bg-[var(--color-success-bg)] !text-[var(--color-success-fg)]'}>{t('settings.browser.online')}</span>
              )}
            </div>
          </div>

          {!st?.chrome_found && (
            <div className="mt-3">
              <label className="text-[11px] font-medium text-[var(--color-muted-foreground)]">{t('settings.browser.chromePath')}</label>
              <input
                value={cfg.chrome_path ?? ''}
                onChange={(e) => patch({ chrome_path: e.target.value })}
                className={INPUT + ' mt-1'}
                placeholder="/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
              />
            </div>
          )}

          <div className="mb-2 mt-5 text-[11px] font-medium uppercase tracking-wide text-[var(--color-muted-foreground)]">
            {t('settings.browser.approval')}
          </div>
          <div className="space-y-2">
            <div className={ROW}>
              <div className="min-w-0 flex-1">
                <div className="text-[12px] font-medium text-[var(--color-foreground)]">{t('settings.browser.navigate')}</div>
              </div>
              <select
                value={cfg.approval?.['navigate'] ?? 'ask'}
                onChange={(e) => setApproval('navigate', e.target.value)}
                className={INPUT_SM}
                style={{ width: '10rem' }}
              >
                <option value="ask">{t('settings.browser.askEachSite')}</option>
                <option value="always_allow">{t('settings.browser.alwaysAllow')}</option>
              </select>
            </div>
            <div className={ROW}>
              <div className="min-w-0 flex-1">
                <div className="text-[12px] font-medium text-[var(--color-foreground)]">{t('settings.browser.interact')}</div>
              </div>
              <select
                value={cfg.approval?.['interact'] ?? 'ask'}
                onChange={(e) => setApproval('interact', e.target.value)}
                className={INPUT_SM}
                style={{ width: '10rem' }}
              >
                <option value="ask">{t('settings.browser.askEachSite')}</option>
                <option value="always_allow">{t('settings.browser.alwaysAllow')}</option>
              </select>
            </div>
          </div>

          <div className="mb-2 mt-5 flex items-center justify-between">
            <div className="text-[11px] font-medium uppercase tracking-wide text-[var(--color-muted-foreground)]">
              {t('settings.browser.sitePermissions')}
            </div>
            <button type="button" className={`${BTN_SECONDARY} ${BTN_SM}`} onClick={addSitePerm}>
              <PlusIcon className="h-3.5 w-3.5" /> {t('settings.browser.add')}
            </button>
          </div>
          <div className="space-y-2">
            {!(cfg.site_permissions?.length ?? 0) && (
              <div className={ROW}>
                <div className="text-[11px] text-[var(--color-muted-foreground)]">{t('settings.browser.noSitePermissions')}</div>
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
                  <option value="ask">{t('settings.browser.navAsk')}</option>
                  <option value="allow">{t('settings.browser.navAllow')}</option>
                </select>
                <select
                  value={sp.interact ?? 'ask'}
                  onChange={(e) => updateSitePerm(i, { interact: e.target.value })}
                  className={INPUT_SM}
                  style={{ width: '7rem' }}
                >
                  <option value="ask">{t('settings.browser.actAsk')}</option>
                  <option value="allow">{t('settings.browser.actAllow')}</option>
                </select>
                <button type="button" className={`${BTN_GHOST} ${BTN_SM}`} onClick={() => removeSitePerm(i)}>
                  <TrashIcon className="h-3.5 w-3.5" />
                </button>
              </div>
            ))}
          </div>

          <div className="mb-2 mt-5 text-[11px] font-medium uppercase tracking-wide text-[var(--color-muted-foreground)]">
            {t('settings.browser.developerMode')}
          </div>
          <div className={ROW}>
            <div className="min-w-0 flex-1">
              <div className="mb-0.5 text-[11px] font-semibold text-[var(--color-warning-fg)]">⚠ {t('settings.browser.elevatedRisk')}</div>
              <div className="text-[12px] font-medium text-[var(--color-foreground)]">{t('settings.browser.devModeTitle')}</div>
              <div className="text-[11px] text-[var(--color-muted-foreground)]">
                {t('settings.browser.devModeDesc')}
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
// SSH tab — SSH aliases + remote-connect wizard entrypoint
// ════════════════════════════════════════════════════════════════════════════

function SSHTab() {
  const { t } = useTranslation()
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
        <h3 className={SECTION_TITLE}>{t('settings.ssh.title')}</h3>
        <button type="button" className={`${BTN_SECONDARY} ${BTN_SM}`} onClick={() => openRemoteConnect()}>
          <PlusIcon className="h-3.5 w-3.5" /> {t('settings.ssh.connect')}
        </button>
      </div>

      <div className="mb-3">
        <div className="mb-1 text-[11px] font-medium text-[var(--color-muted-foreground)]">{t('settings.ssh.currentEnv')}</div>
        <span className={CHIP_ACCENT}>
          <span className="h-1.5 w-1.5 rounded-full" style={{ backgroundColor: 'var(--color-accent-neutral)' }} />
          {current}
        </span>
      </div>

      {loading ? (
        <div className="animate-pulse py-6 text-center text-xs text-[var(--color-muted-foreground)]">{t('common.loading')}</div>
      ) : aliases.length === 0 ? (
        <EmptyState
          Icon={CommandLineIcon}
          title={t('settings.ssh.noneAliases')}
          hint={t('settings.ssh.noneAliasesHint')}
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
              {current === alias.name && <span className={CHIP_ACCENT}>{t('settings.ssh.active')}</span>}
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
  const { t, i18n } = useTranslation()
  const [stats, setStats] = useState<UsageStats | null>(null)
  const [days, setDays] = useState(30)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')

  useEffect(() => {
    setLoading(true)
    setError('')
    api
      .usageStats(days)
      .then(setStats)
      .catch((e) => setError(e instanceof Error ? e.message : String(e)))
      .finally(() => setLoading(false))
  }, [days])

  const fmtCompact = (n: number) => new Intl.NumberFormat(i18n.language, { notation: 'compact', maximumFractionDigits: 1 }).format(n)
  const fmtFull = (n: number) => new Intl.NumberFormat(i18n.language).format(n)
  const fmtPct = (frac: number) => `${Math.round(frac * 100)}%`

  if (loading) {
    return <div className="animate-pulse py-6 text-center text-xs text-[var(--color-muted-foreground)]">{t('common.loading')}</div>
  }
  if (!stats) {
    return <EmptyState Icon={ChartBarIcon} title={t('settings.usageStats.noData')} hint={error || t('settings.usageStats.subtitle')} />
  }

  const totals = stats.totals
  const trend = stats.daily_trend
  const maxTokens = Math.max(1, ...trend.map((d) => d.tokens))
  const heat = buildHeatmap(stats.heatmap)

  return (
    <div className="space-y-5">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h3 className={SECTION_TITLE}>{t('settings.usageStats.title')}</h3>
          <p className="mt-0.5 text-[11px] text-[var(--color-muted-foreground)]">{t('settings.usageStats.subtitle')}</p>
        </div>
        <div className="inline-flex rounded-[var(--radius-md)] bg-[var(--color-secondary)] p-0.5">
          {[7, 30].map((d) => (
            <button
              key={d}
              type="button"
              onClick={() => setDays(d)}
              className={`h-7 rounded-[var(--radius-sm)] px-2.5 text-xs transition-colors ${
                days === d
                  ? 'bg-[var(--color-background)] font-medium text-[var(--color-foreground)] shadow-[var(--shadow-sm)]'
                  : 'text-[var(--color-muted-foreground)] hover:text-[var(--color-foreground)]'
              }`}
            >
              {t('settings.usageStats.lastNDays').replace('{n}', String(d))}
            </button>
          ))}
        </div>
      </div>

      {error && <div className="rounded-[var(--radius-md)] bg-[var(--color-warning-bg)] px-3 py-2 text-xs text-[var(--color-warning-fg)]">{error}</div>}

      <div className="grid grid-cols-2 gap-2.5 sm:grid-cols-3">
        <StatCard label={t('settings.usageStats.totalTokens')} value={fmtCompact(totals.total_tokens)} title={fmtFull(totals.total_tokens)} large />
        <StatCard label={t('settings.usageStats.cacheHitRate')} value={stats.cache_supported ? fmtPct(stats.cache_hit_rate) : '—'} large accent />
        <StatCard label={t('settings.usageStats.mostUsedModel')} value={stats.most_used_model || '—'} title={stats.most_used_model} large />
        <StatCard label={t('settings.usageStats.sessions')} value={fmtFull(totals.sessions)} />
        <StatCard label={t('settings.usageStats.turns')} value={fmtFull(totals.turns)} />
        <StatCard label={t('settings.usageStats.activeDays')} value={fmtFull(stats.active_days)} sub={t('settings.usageStats.streak').replace('{n}', String(stats.current_streak))} />
      </div>

      <section className={US_PANEL}>
        <div className={US_PANEL_TITLE}>{t('settings.usageStats.tokenBreakdown')}</div>
        <div className="mt-2 grid grid-cols-2 gap-2.5 sm:grid-cols-4">
          <MiniStat label={t('settings.usageStats.promptTokens')} value={fmtCompact(totals.prompt_tokens)} />
          <MiniStat label={t('settings.usageStats.cachedTokens')} value={fmtCompact(totals.cached_tokens)} />
          <MiniStat label={t('settings.usageStats.completionTokens')} value={fmtCompact(totals.completion_tokens)} />
          <MiniStat label={t('settings.usageStats.reasoningTokens')} value={fmtCompact(totals.reasoning_tokens)} />
        </div>
        <div className="mt-2 text-[10.5px] text-[var(--color-muted-foreground)]">{t('settings.usageStats.tokenBreakdownHint')}</div>
      </section>

      <section className={US_PANEL}>
        <div className="flex items-center justify-between">
          <div className={US_PANEL_TITLE}>{t('settings.usageStats.heatmap')}</div>
          <div className="flex items-center gap-1 text-[10px] text-[var(--color-muted-foreground)]">
            <span>{t('settings.usageStats.less')}</span>
            {HEAT_FILL.map((fill) => <span key={fill} className="inline-block h-2.5 w-2.5 rounded-[2px]" style={{ background: fill }} />)}
            <span>{t('settings.usageStats.more')}</span>
          </div>
        </div>
        <div className="mt-2 overflow-x-auto pb-1">
          <div className="grid w-max grid-flow-col grid-rows-7 gap-[3px]">
            {heat.map((cell) => (
              <span
                key={cell.date}
                title={cell.future ? '' : cell.tokens > 0 ? `${cell.date} · ${fmtCompact(cell.tokens)} tokens · ${cell.turns} ${t('settings.usageStats.turnsUnit')}` : `${cell.date} · ${t('settings.usageStats.noActivity')}`}
                className="h-[11px] w-[11px] rounded-[2px]"
                style={{ background: cell.future ? 'transparent' : HEAT_FILL[cell.level] }}
              />
            ))}
          </div>
        </div>
      </section>

      <section className={US_PANEL}>
        <div className={US_PANEL_TITLE}>{t('settings.usageStats.dailyTrend')}</div>
        {trend.length > 0 ? (
          <div className="mt-3 flex h-[120px] items-end gap-[3px]">
            {trend.map((d) => (
              <div
                key={d.date}
                title={`${d.date} · ${fmtCompact(d.tokens)} tokens · ${d.turns} ${t('settings.usageStats.turnsUnit')}`}
                className="min-w-[2px] flex-1 rounded-t-[2px] bg-[var(--accent-fill)] transition-[height]"
                style={{ height: `${Math.max(2, (d.tokens / maxTokens) * 100)}%`, opacity: d.tokens === 0 ? 0.25 : 0.9 }}
              />
            ))}
          </div>
        ) : (
          <div className="py-6 text-center text-[11px] text-[var(--color-muted-foreground)]">{t('settings.usageStats.noData')}</div>
        )}
      </section>

      <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
        <Breakdown title={t('settings.usageStats.byModel')} shares={stats.by_model} compact={fmtCompact} />
        <Breakdown title={t('settings.usageStats.byProject')} shares={stats.by_project} compact={fmtCompact} project />
      </div>
    </div>
  )
}

function StatCard({ label, value, title, sub, large, accent }: { label: string; value: string; title?: string; sub?: string; large?: boolean; accent?: boolean }) {
  return (
    <div className={`rounded-[var(--radius-md)] bg-[var(--color-secondary)] px-3 py-2 ${large ? 'py-3' : ''}`}>
      <div className="text-[10px] uppercase tracking-wide text-[var(--color-muted-foreground)]">{label}</div>
      <div title={title} className={`mt-0.5 truncate font-mono font-semibold ${large ? 'text-[18px]' : 'text-[14px]'} ${accent ? 'text-[var(--color-primary)]' : 'text-[var(--color-foreground)]'}`}>{value}</div>
      {sub && <div className="mt-px text-[10.5px] text-[var(--color-muted-foreground)]">{sub}</div>}
    </div>
  )
}

function MiniStat({ label, value }: { label: string; value: string }) {
  return (
    <div>
      <div className="text-[10px] uppercase tracking-[0.05em] text-[var(--color-muted-foreground)]">{label}</div>
      <div className="mt-0.5 font-mono text-sm font-semibold text-[var(--color-foreground)]">{value}</div>
    </div>
  )
}

function Breakdown({
  title,
  shares,
  compact,
  project,
}: {
  title: string
  shares: { name: string; tokens: number; share: number }[]
  compact: (n: number) => string
  project?: boolean
}) {
  const sorted = [...shares].sort((a, b) => b.tokens - a.tokens).slice(0, 8)
  return (
    <section className={US_PANEL}>
      <div className={US_PANEL_TITLE}>{title}</div>
      <div className="space-y-1.5">
        {sorted.length === 0 && <div className="text-[11px] text-[var(--color-muted-foreground)]">No data.</div>}
        {sorted.map((s) => (
          <div key={s.name}>
            <div className="flex items-center justify-between text-[11px]">
              <span className="truncate text-[var(--color-foreground)]" title={s.name}>{project ? shortProjectName(s.name) : s.name}</span>
              <span className="ml-2 shrink-0 font-mono text-[var(--color-muted-foreground)]">
                {compact(s.tokens)}
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
    </section>
  )
}

const US_PANEL = 'rounded-[var(--radius-md)] bg-[var(--color-secondary)] p-3'
const US_PANEL_TITLE = 'text-[11px] font-semibold uppercase tracking-[0.06em] text-[var(--color-muted-foreground)]'
const HEAT_FILL = [
  'color-mix(in srgb, var(--color-foreground) 7%, transparent)',
  'color-mix(in srgb, var(--color-primary) 28%, transparent)',
  'color-mix(in srgb, var(--color-primary) 48%, transparent)',
  'color-mix(in srgb, var(--color-primary) 72%, transparent)',
  'var(--color-primary)',
]

function buildHeatmap(buckets: { date: string; tokens: number; turns: number }[]) {
  const map = new Map(buckets.map((b) => [b.date, b]))
  const max = Math.max(0, ...buckets.map((b) => b.tokens))
  const today = new Date()
  today.setHours(0, 0, 0, 0)
  const lastSunday = new Date(today)
  lastSunday.setDate(today.getDate() - today.getDay())
  const start = new Date(lastSunday)
  start.setDate(lastSunday.getDate() - 52 * 7)
  const cells: { date: string; tokens: number; turns: number; level: number; future: boolean }[] = []
  for (let w = 0; w < 53; w++) {
    for (let d = 0; d < 7; d++) {
      const cur = new Date(start)
      cur.setDate(start.getDate() + w * 7 + d)
      const key = dateKey(cur)
      const bucket = map.get(key)
      const tokens = bucket?.tokens || 0
      cells.push({
        date: key,
        tokens,
        turns: bucket?.turns || 0,
        level: levelFor(tokens, max),
        future: cur.getTime() > today.getTime(),
      })
    }
  }
  return cells
}

function levelFor(tokens: number, max: number): number {
  if (tokens <= 0 || max <= 0) return 0
  const ratio = Math.log(tokens + 1) / Math.log(max + 1)
  return Math.min(4, Math.max(1, Math.ceil(ratio * 4)))
}

function dateKey(d: Date): string {
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
}

function shortProjectName(path: string): string {
  const parts = path.replace(/\/+$/, '').split('/').filter(Boolean)
  return parts.at(-1) || path
}
