/**
 * ChatInput — the product composer (ported from web/src/components/ChatInput.vue).
 *
 * Layers jcode product UI on top of the runtime actions:
 *   - autosizing textarea, send / queue / stop, IME-safe Enter
 *   - slash-command menu (keyboard-navigable)
 *   - MODE picker (approval / plan / full_access)
 *   - MODEL picker (current / favorites / all-providers with capability dots,
 *     context-limit subline, and a Manage Models dialog)
 *   - EFFORT picker (per-model reasoning effort)
 *   - "+" menu (attach images, slash insert, Goal arming)
 *   - image attachments (paste + file picker + thumbnails)
 *   - type-ahead queue chips
 *   - keyboard shortcuts (⌘L focus, Esc to close dialogs)
 *   - click-outside handling
 *
 * Send/stop/queue come from the jcode-ui runtime actions; provider/model/mode
 * state + the model catalog come from the Redux model slice; the slash command
 * list comes from the chat slice (fetched at boot in App.tsx).
 */

import {
  useCallback,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
} from 'react'
import type { KeyboardEvent as ReactKeyboardEvent, MouseEvent as ReactMouseEvent } from 'react'
import { createPortal } from 'react-dom'
import { useTranslation } from 'react-i18next'
import {
  HandRaisedIcon,
  ShieldExclamationIcon,
  ClipboardDocumentListIcon,
  BoltIcon,
  PlusIcon,
  PaperClipIcon,
  XMarkIcon,
  ChevronDownIcon,
  StopIcon,
  PaperAirplaneIcon,
  MagnifyingGlassIcon,
  SquaresPlusIcon,
  PhotoIcon,
  WrenchScrewdriverIcon,
  CheckIcon,
  StarIcon,
  SparklesIcon,
} from '@heroicons/react/24/outline'
import { StarIcon as StarIconSolid, CheckCircleIcon } from '@heroicons/react/24/solid'
import { AttachmentList, useRuntimeActions, useRuntimeState } from 'jcode-ui'
import type { ChatImage as RuntimeChatImage } from 'jcode-ui-core'
import { useAppDispatch, useAppSelector } from '../app/hooks'
import { chatActions, modelActions } from '../app/store'
import { api } from '../lib/api'
import { BranchPicker } from './BranchPicker'
import { ProviderIcon } from './ProviderIcon'
import { WorkspacePicker } from './WorkspacePicker'
import type {
  AgentMode,
  ChatImage,
  ModelInfo,
  ProviderInfo,
  SlashCommandInfo,
  TaskStats,
} from '../lib/types'

// ─── Props ──────────────────────────────────────────────────────────────────

export interface ChatInputProps {
  /** Fired when the user dispatches a message (sent now or queued mid-turn). */
  onSent?: () => void
  /** Direction for workspace/branch panels. Welcome opens downward. */
  pickerPlacement?: 'top' | 'bottom'
  /**
   * Elevate the whole composer into a bordered, shadowed card (welcome /
   * new-task screen). Docked conversation composers stay recessed.
   * Mirrors Vue's `.composer-elevated` on `.chat-input-card`.
   */
  elevated?: boolean
}

// ─── Constants ──────────────────────────────────────────────────────────────

type ModeValue = AgentMode
interface ModeDef {
  value: ModeValue
  label: string
  sub: string
  risk: 'neutral' | 'plan' | 'danger'
  Icon: typeof HandRaisedIcon
}

const MODE_DEFS: ModeDef[] = [
  { value: 'approval', label: 'Ask for approval', sub: 'Agent asks before running tools', risk: 'neutral', Icon: HandRaisedIcon },
  { value: 'plan', label: 'Plan', sub: 'Agent plans, then waits for your go-ahead', risk: 'plan', Icon: ClipboardDocumentListIcon },
  { value: 'full_access', label: 'Full access', sub: 'Agent can act without asking', risk: 'danger', Icon: ShieldExclamationIcon },
]

const STANDARD_EFFORT_OPTIONS = ['minimal', 'low', 'medium', 'high', 'xhigh', 'max']

function modeLabel(m: string): string {
  const def = MODE_DEFS.find((d) => d.value === m)
  return def ? def.label : 'Ask for approval'
}

// ─── Format helpers ─────────────────────────────────────────────────────────

/** Compact context-limit label: 200000 → "200K", 1000000 → "1M". */
function formatContext(limit?: number): string | null {
  if (!limit || limit <= 0) return null
  if (limit >= 1_000_000) {
    const m = limit / 1_000_000
    return (Number.isInteger(m) ? String(m) : m.toFixed(1)) + 'M'
  }
  if (limit >= 1000) return Math.round(limit / 1000) + 'K'
  return String(limit)
}

/** "claude-sonnet-4-5 · 200K". */
function modelSubline(modelId: string, info?: { context_limit?: number }): string {
  const parts = [modelId]
  const ctx = formatContext(info?.context_limit)
  if (ctx) parts.push(ctx)
  return parts.join(' · ')
}

// ─── Component ──────────────────────────────────────────────────────────────

export function ChatInput({ onSent, pickerPlacement = 'top', elevated = false }: ChatInputProps) {
  const { t } = useTranslation()
  const dispatch = useAppDispatch()

  // Runtime (timeline/queue + send/stop actions).
  const actions = useRuntimeActions()
  const { isRunning, queued, tokenSnapshot } = useRuntimeState()

  // Model slice state.
  const providerName = useAppSelector((s) => s.model.providerName)
  const modelName = useAppSelector((s) => s.model.modelName)
  const mode = useAppSelector((s) => s.model.mode)
  const providers = useAppSelector((s) => s.model.providers)
  const favoriteModels = useAppSelector((s) => s.model.favoriteModels)
  const recentModels = useAppSelector((s) => s.model.recentModels)
  const imageSupport = useAppSelector((s) => s.model.imageSupport)
  const effortOverrides = useAppSelector((s) => s.model.effortOverrides)
  const slashCommands = useAppSelector((s) => s.chat.slashCommands)
  const hasMessages = useAppSelector((s) => s.chat.timeline.length > 0)
  const goalArmed = useAppSelector((s) => s.chat.goalArmed)
  const currentSessionId = useAppSelector((s) => s.session.currentSessionId)

  // Composer-local state.
  const [input, setInput] = useState('')
  /** Pending vision images — same shape as jcode-ui `ChatImage` / AttachmentList. */
  const [pendingImages, setPendingImages] = useState<ChatImage[]>([])
  const [showSlashMenu, setShowSlashMenu] = useState(false)
  const [slashFilter, setSlashFilter] = useState('')
  const [selectedSlashIdx, setSelectedSlashIdx] = useState(0)
  const [showModelPicker, setShowModelPicker] = useState(false)
  const [showModePicker, setShowModePicker] = useState(false)
  const [showEffortPicker, setShowEffortPicker] = useState(false)
  const [showAddMenu, setShowAddMenu] = useState(false)
  const [showManageModels, setShowManageModels] = useState(false)
  const [showContextPopup, setShowContextPopup] = useState(false)
  const [taskStats, setTaskStats] = useState<TaskStats | null>(null)
  const [taskStatsLoading, setTaskStatsLoading] = useState(false)
  const [modelFilter, setModelFilter] = useState('')

  const textareaRef = useRef<HTMLTextAreaElement>(null)
  const fileInputRef = useRef<HTMLInputElement>(null)
  const containerRef = useRef<HTMLDivElement>(null)

  // ─── Derived model catalog ────────────────────────────────────────────────

  function modelInfoFor(provider: string, model: string): ModelInfo | undefined {
    const p = providers.find((x) => x.id === provider)
    return p?.models.find((m) => m.id === model)
  }

  function providerInfoFor(provider: string): ProviderInfo | undefined {
    return providers.find((x) => x.id === provider)
  }

  function getModelDisplayName(providerId: string, modelId: string): string {
    return modelInfoFor(providerId, modelId)?.name || modelId
  }

  // Filtered providers + models (search box). Used by both the picker and the
  // Manage dialog.
  const filteredProviders: ProviderInfo[] = useMemo(() => {
    const filter = modelFilter.trim().toLowerCase()
    if (!filter) return providers
    return providers
      .map((p) => ({
        ...p,
        models: p.models.filter(
          (m) =>
            (m.name || m.id).toLowerCase().includes(filter) ||
            p.name.toLowerCase().includes(filter),
        ),
      }))
      .filter((p) => p.models.length > 0)
  }, [providers, modelFilter])

  // The picker lists only enabled models; the Manage dialog shows all.
  const pickerProviders = useMemo(
    () =>
      filteredProviders
        .map((p) => ({ ...p, models: p.models.filter((m) => m.enabled !== false) }))
        .filter((p) => p.models.length > 0),
    [filteredProviders],
  )

  const favoriteModelRefs = useMemo(() => {
    const q = modelFilter.trim().toLowerCase()
    const favSet = new Set(favoriteModels)
    return recentModels.filter((r) => {
      if (!favSet.has(`${r.provider}/${r.model}`)) return false
      if (providerName === r.provider && modelName === r.model) return false
      if (!q) return true
      const name = getModelDisplayName(r.provider, r.model).toLowerCase()
      return name.includes(q) || r.provider.toLowerCase().includes(q)
    })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [favoriteModels, recentModels, modelFilter, providerName, modelName, providers])

  const currentModelInfo = useMemo(
    () => modelInfoFor(providerName, modelName),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [providerName, modelName, providers],
  )

  const effortKey = `${providerName}/${modelName}`
  const currentEffort = effortOverrides[effortKey] ?? ''

  // Reasoning-effort levels the current model advertises. Some custom and
  // OpenAI-compatible models only expose a broad `reasoning` flag, so fall back
  // to the backend-accepted standard tiers and keep an existing override visible.
  const currentEffortOptions: string[] = useMemo(() => {
    const values = new Set<string>()
    const info = currentModelInfo
    for (const o of info?.reasoning_options ?? []) {
      if (o.type === 'effort') {
        for (const v of o.values ?? []) values.add(v)
      }
    }
    if (values.size === 0 && (info?.reasoning || currentEffort)) {
      for (const v of STANDARD_EFFORT_OPTIONS) values.add(v)
    }
    if (currentEffort) values.add(currentEffort)
    return [...values]
  }, [currentEffort, currentModelInfo])
  const showEffortControl = currentEffortOptions.length > 0

  const manageVisibleCount = filteredProviders.reduce((n, p) => n + p.models.length, 0)
  const manageTotalCount = providers.reduce((n, p) => n + p.models.length, 0)

  const filteredSlashCommands = useMemo(() => {
    const filter = slashFilter.toLowerCase()
    return slashCommands.filter((s) => s.slash.toLowerCase().startsWith('/' + filter))
  }, [slashCommands, slashFilter])

  // Context-fill ring (token usage %).
  const ctxRingCirc = 2 * Math.PI * 6.4
  const tokenPct = tokenSnapshot && tokenSnapshot.model_context_limit > 0
    ? Math.min(100, Math.max(0, Math.round((tokenSnapshot.total_tokens / tokenSnapshot.model_context_limit) * 100)))
    : 0
  const ctxRingOffset = ctxRingCirc * (1 - tokenPct / 100)
  const ctxRingColor = tokenPct >= 90 ? 'var(--color-destructive)' : 'var(--color-primary)'
  const showTokenCount = hasMessages && !!tokenSnapshot && tokenSnapshot.total_tokens > 0

  async function toggleContextPopup() {
    setShowContextPopup((v) => !v)
    if (!showContextPopup && currentSessionId) {
      setTaskStatsLoading(true)
      try {
        const stats = await api.taskStats(currentSessionId)
        setTaskStats(stats)
      } catch {
        setTaskStats(null)
      } finally {
        setTaskStatsLoading(false)
      }
    }
  }

  // ─── Autosize ─────────────────────────────────────────────────────────────

  const autoResize = useCallback(() => {
    const el = textareaRef.current
    if (!el) return
    el.style.height = 'auto'
    el.style.height = `${Math.min(el.scrollHeight, 160)}px`
  }, [])

  useLayoutEffect(() => {
    autoResize()
  }, [input, autoResize])

  // ─── Send / queue ─────────────────────────────────────────────────────────

  const send = useCallback(() => {
    const text = input.trim()
    if (!text && pendingImages.length === 0) return
    const images: RuntimeChatImage[] | undefined =
      pendingImages.length > 0
        ? pendingImages.map((i) => ({ data: i.data, media_type: i.media_type, name: i.name }))
        : undefined
    const body = text || '(see attached images)'
    if (isRunning) {
      actions.enqueueMessage(body, images)
    } else {
      actions.sendMessage(body, images)
    }
    setInput('')
    setPendingImages([])
    setShowSlashMenu(false)
    onSent?.()
  }, [actions, input, isRunning, onSent, pendingImages])

  // ─── Model / mode selection ───────────────────────────────────────────────

  async function selectModel(provider: string, model: string) {
    setShowModelPicker(false)
    setShowEffortPicker(false)
    setModelFilter('')
    try {
      await api.switchModel(provider, model)
      dispatch(modelActions.setProvider(provider))
      dispatch(modelActions.setModel(model))
    } catch {
      /* ignore — the next status poll reconciles */
    }
  }

  async function selectMode(next: ModeValue) {
    setShowModePicker(false)
    try {
      await api.switchMode(next)
    } catch {
      /* ignore */
    }
    dispatch(modelActions.setMode(next))
  }

  async function pickEffort(effort: string) {
    setShowEffortPicker(false)
    dispatch(modelActions.setEffortOverride({ provider: providerName, model: modelName, effort }))
    try {
      await api.setModelEffort(providerName, modelName, effort)
    } catch {
      dispatch(modelActions.setEffortOverride({ provider: providerName, model: modelName, effort: currentEffort }))
    }
  }

  async function toggleFavorite(provider: string, model: string) {
    try {
      const result = await api.toggleFavorite(provider, model)
      dispatch(modelActions.setFavorite({ provider, model, favorite: result.favorite }))
    } catch {
      /* ignore */
    }
  }

  async function toggleModelEnabled(provider: string, model: string, enabled: boolean) {
    try {
      await api.toggleModelEnabled(provider, model, enabled)
      // Reflect locally so the Manage dialog toggles immediately.
      const nextProviders = providers.map((p) =>
        p.id === provider
          ? { ...p, models: p.models.map((m) => (m.id === model ? { ...m, enabled } : m)) }
          : p,
      )
      dispatch(modelActions.setProviders(nextProviders))
    } catch {
      /* ignore */
    }
  }

  // ─── Slash commands ───────────────────────────────────────────────────────

  function applySlashCommand(cmd: SlashCommandInfo) {
    setInput(cmd.slash + ' ')
    setShowSlashMenu(false)
    requestAnimationFrame(() => textareaRef.current?.focus())
  }

  function selectFirstFiltered() {
    const p = pickerProviders[0]
    const m = p?.models[0]
    if (p && m) void selectModel(p.id, m.id)
  }

  // ─── Image attachments ────────────────────────────────────────────────────

  function addImageFile(file: File) {
    const reader = new FileReader()
    reader.onload = () => {
      const result = reader.result as string
      const commaIdx = result.indexOf(',')
      if (commaIdx < 0) return
      const base64Data = result.substring(commaIdx + 1)
      setPendingImages((prev) => [
        ...prev,
        { data: base64Data, media_type: file.type, name: file.name || undefined },
      ])
    }
    reader.readAsDataURL(file)
  }

  function handlePaste(e: React.ClipboardEvent<HTMLTextAreaElement>) {
    if (!imageSupport) return
    const items = e.clipboardData?.items
    if (!items) return
    for (const item of Array.from(items)) {
      if (!item.type.startsWith('image/')) continue
      e.preventDefault()
      const file = item.getAsFile()
      if (file) addImageFile(file)
    }
  }

  function handleImageSelect(e: React.ChangeEvent<HTMLInputElement>) {
    const files = e.target.files
    if (!files) return
    for (const file of Array.from(files)) {
      if (!file.type.startsWith('image/')) continue
      if (file.size > 10 * 1024 * 1024) continue // 10MB limit
      addImageFile(file)
    }
    e.target.value = ''
  }

  function removeImage(index: number) {
    setPendingImages((prev) => prev.filter((_, i) => i !== index))
  }

  function triggerImageUpload() {
    fileInputRef.current?.click()
  }

  // ─── Input handling ───────────────────────────────────────────────────────

  function handleInput(e: React.ChangeEvent<HTMLTextAreaElement>) {
    autoResize()
    const text = e.target.value
    setInput(text)
    if (text.startsWith('/')) {
      setSlashFilter(text.slice(1))
      setShowSlashMenu(true)
      setSelectedSlashIdx(0)
    } else {
      setShowSlashMenu(false)
    }
  }

  function insertToken(char: string) {
    setShowAddMenu(false)
    const el = textareaRef.current
    const start = el ? el.selectionStart : input.length
    const end = el ? el.selectionEnd : input.length
    const next = input.slice(0, start) + char + input.slice(end)
    setInput(next)
    requestAnimationFrame(() => {
      el?.focus()
      const pos = start + char.length
      el?.setSelectionRange(pos, pos)
      // Re-run slash-menu logic if "/" was inserted.
      if (char === '/') {
        setSlashFilter(next.slice(1))
        setShowSlashMenu(next.startsWith('/'))
        setSelectedSlashIdx(0)
      }
    })
  }

  function handleKeyDown(e: ReactKeyboardEvent<HTMLTextAreaElement>) {
    // IME composition owns every key while active.
    if (e.nativeEvent.isComposing || e.keyCode === 229) return

    // Esc closes dialogs first.
    if (e.key === 'Escape') {
      if (showManageModels) {
        e.preventDefault()
        setShowManageModels(false)
        setModelFilter('')
        return
      }
      if (showModelPicker) {
        e.preventDefault()
        setShowModelPicker(false)
        return
      }
      if (showSlashMenu) {
        e.preventDefault()
        setShowSlashMenu(false)
        return
      }
    }

    if (showSlashMenu) {
      if (e.key === 'ArrowDown') {
        e.preventDefault()
        setSelectedSlashIdx((i) => Math.min(i + 1, filteredSlashCommands.length - 1))
        return
      }
      if (e.key === 'ArrowUp') {
        e.preventDefault()
        setSelectedSlashIdx((i) => Math.max(i - 1, 0))
        return
      }
      if (e.key === 'Enter' || e.key === 'Tab') {
        const cmd = filteredSlashCommands[selectedSlashIdx]
        if (cmd) {
          e.preventDefault()
          applySlashCommand(cmd)
          return
        }
      }
      if (e.key === 'Escape') {
        e.preventDefault()
        setShowSlashMenu(false)
        return
      }
    }

    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault()
      send()
    }
  }

  // ─── Click-outside + global keys ──────────────────────────────────────────

  useEffect(() => {
    function onClick(e: globalThis.MouseEvent) {
      if (containerRef.current && !containerRef.current.contains(e.target as Node)) {
        setShowModelPicker(false)
        setShowModePicker(false)
        setShowAddMenu(false)
        setShowSlashMenu(false)
        setShowEffortPicker(false)
        setShowContextPopup(false)
        if (showManageModels) {
          setShowManageModels(false)
          setModelFilter('')
        }
      }
    }
    function onKey(e: globalThis.KeyboardEvent) {
      if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 'l') {
        e.preventDefault()
        textareaRef.current?.focus()
      }
      if (e.key === 'Escape') {
        if (showManageModels) {
          e.preventDefault()
          setShowManageModels(false)
          setModelFilter('')
        } else if (showModelPicker) {
          e.preventDefault()
          setShowModelPicker(false)
        }
      }
    }
    document.addEventListener('click', onClick)
    document.addEventListener('keydown', onKey)
    return () => {
      document.removeEventListener('click', onClick)
      document.removeEventListener('keydown', onKey)
    }
  }, [showManageModels, showModelPicker])

  // ─── Re-focus when a turn ends; drop images when the model can't accept them.

  const wasRunning = useRef(false)
  useEffect(() => {
    if (wasRunning.current && !isRunning) {
      textareaRef.current?.focus()
    }
    wasRunning.current = isRunning
  }, [isRunning])

  useEffect(() => {
    if (!imageSupport && pendingImages.length > 0) {
      setPendingImages([])
    }
  }, [imageSupport, pendingImages.length])

  // ─── Trigger helpers (mutually-exclusive open states) ─────────────────────

  const openAdd = () => {
    setShowAddMenu((v) => !v)
    setShowModelPicker(false)
    setShowModePicker(false)
  }
  const openMode = () => {
    setShowModePicker((v) => !v)
    setShowModelPicker(false)
    setShowAddMenu(false)
  }
  const openModel = () => {
    setShowModelPicker((v) => !v)
    setShowModePicker(false)
    setShowAddMenu(false)
    setShowEffortPicker(false)
  }
  const openEffort = () => {
    setShowEffortPicker((v) => !v)
    setShowModelPicker(false)
    setShowModePicker(false)
    setShowAddMenu(false)
  }

  const canSend = input.trim().length > 0 || pendingImages.length > 0
  const showSend = !isRunning || input.trim().length > 0 || pendingImages.length > 0
  const currentModeDef = MODE_DEFS.find((m) => m.value === mode) ?? MODE_DEFS[0]

  // ─── Render ───────────────────────────────────────────────────────────────

  // Welcome/new-task elevates the whole card; docked conversation stays flat.
  const isElevated = elevated || !hasMessages

  // Horizontal inset comes from the parent (`.chat-col` or welcome `px-5`) so
  // the composer width matches the message column exactly.
  return (
    <div ref={containerRef} className="relative w-full bg-transparent" style={{ padding: '8px 0 14px' }}>
      {/* Type-ahead queue */}
      {queued.length > 0 && (
        <div className="mx-auto mb-1.5 flex flex-col gap-1" style={{ padding: '0 8px 6px' }}>
          {queued.map((q, i) => (
            <div
              key={q.id}
              className="flex items-center gap-2 rounded-[var(--radius-lg)] border border-[var(--color-border)] bg-[var(--color-muted)] px-2 py-1.5 text-xs text-[var(--color-foreground)]"
            >
              <span
                className="grid h-4 w-4 shrink-0 place-items-center rounded-[var(--radius-pill)] bg-[var(--neutral-wash)] font-mono text-[10px] font-semibold text-[var(--color-foreground)]"
              >
                {i + 1}
              </span>
              <span className="min-w-0 flex-1 truncate">{q.text}</span>
              {q.images && q.images.length > 0 && (
                <span className="inline-flex shrink-0 items-center gap-0.5 font-mono text-[11px] text-[var(--color-muted-foreground)]">
                  <PaperClipIcon className="h-3 w-3" />
                  {q.images.length}
                </span>
              )}
              <button
                type="button"
                title="Remove queued message"
                aria-label="Remove queued message"
                onClick={() => actions.removeQueuedMessage(q.id)}
                className="grid h-5 w-5 shrink-0 place-items-center rounded-[var(--radius-md)] border-none bg-transparent text-[var(--color-muted-foreground)] transition-colors hover:bg-[var(--color-secondary)] hover:text-[var(--color-foreground)]"
              >
                <XMarkIcon className="h-3 w-3" />
              </button>
            </div>
          ))}
        </div>
      )}

      <div
        className={`relative mx-auto flex flex-col ${
          isElevated
            ? 'rounded-[var(--radius-2xl)] border border-[var(--color-border)] bg-[var(--color-surface)] shadow-[var(--shadow-xl)]'
            : 'rounded-[var(--radius-xl)] bg-transparent'
        }`}
        style={{ padding: isElevated ? '6px 12px 12px' : '4px 6px 10px' }}
      >
        {/* Slash command menu */}
        {showSlashMenu && filteredSlashCommands.length > 0 && (
          <div className="absolute bottom-full left-0 right-0 z-30 mb-2 max-h-[200px] overflow-y-auto rounded-[var(--radius-lg)] border border-[var(--color-border)] bg-[var(--color-surface)] py-1.5 shadow-[var(--shadow-md)]">
            {filteredSlashCommands.map((cmd, i) => (
              <button
                key={cmd.slash}
                type="button"
                onMouseEnter={() => setSelectedSlashIdx(i)}
                onClick={() => applySlashCommand(cmd)}
                className={`flex w-full items-start gap-2.5 px-3.5 py-2 text-left transition-colors ${
                  i === selectedSlashIdx ? 'bg-[var(--color-muted)]' : ''
                }`}
              >
                <span className="shrink-0 font-mono text-xs text-[var(--color-primary)]">{cmd.slash}</span>
                {cmd.type === 'flow' && (
                  <span className="shrink-0 rounded-[var(--radius-pill)] border border-[var(--accent-wash)] bg-[var(--accent-wash)] px-1.5 py-0.5 text-[10px] font-semibold leading-none text-[var(--color-primary)]">
                    workflow
                  </span>
                )}
                <span className="truncate text-[11px] text-[var(--color-muted-foreground)]">{cmd.description}</span>
              </button>
            ))}
          </div>
        )}

        {!hasMessages && (
          <div
            className="flex min-w-0 items-center gap-1.5"
            style={{ padding: isElevated ? '2px 4px 9px' : '0 2px 5px' }}
          >
            <WorkspacePicker placement={pickerPlacement} />
            <BranchPicker placement={pickerPlacement} />
          </div>
        )}

        <div
          className={`border border-[var(--color-border)] bg-[color-mix(in_srgb,var(--color-surface)_90%,#000)] transition-colors focus-within:border-[color-mix(in_srgb,var(--color-foreground)_30%,transparent)] ${
            isElevated ? 'rounded-[var(--radius-xl)]' : 'rounded-[var(--radius-lg)]'
          }`}
          style={{ padding: '14px 16px 0' }}
        >
          {/* Textarea + image previews */}
          <div className="pb-2">
            <textarea
              ref={textareaRef}
              value={input}
              rows={1}
              onChange={handleInput}
              onKeyDown={handleKeyDown}
              onPaste={handlePaste}
              placeholder={
                isRunning
                  ? 'Queue a message…'
                    : goalArmed
                    ? 'What is your goal?'
                    : 'Send a message…'
              }
              className="block w-full resize-none border-none bg-transparent text-sm leading-relaxed text-[var(--color-foreground)] outline-none"
              style={{ minHeight: 28, maxHeight: 200, fontFamily: 'var(--font-sans)' }}
            />

            {pendingImages.length > 0 && (
              <div className="mt-2">
                <AttachmentList images={pendingImages} onRemove={removeImage} size={56} />
              </div>
            )}

            <input
              ref={fileInputRef}
              type="file"
              accept="image/*"
              multiple
              onChange={handleImageSelect}
              className="hidden"
            />
          </div>

          {/* Toolbar */}
          <div
            className="flex items-center justify-between gap-2 py-2"
            style={{ paddingBottom: 12 }}
          >
            {/* Toolbar-left: + menu, mode picker, goal chip */}
            <div className="flex items-center gap-1.5">
              {/* + menu */}
              <div className="relative">
                <button
                  type="button"
                  title="Add"
                  onClick={(e: ReactMouseEvent) => { e.stopPropagation(); openAdd() }}
                  className={`grid h-[30px] w-[30px] place-items-center rounded-[var(--radius-md)] border-none bg-transparent text-[var(--color-muted-foreground)] transition-colors hover:bg-[var(--color-secondary)] hover:text-[var(--color-foreground)] ${
                    showAddMenu ? 'bg-[var(--color-secondary)] text-[var(--color-foreground)]' : ''
                  }`}
                >
                  <PlusIcon className="h-4 w-4" />
                </button>
                {showAddMenu && (
                  <div
                    className="absolute bottom-full left-0 z-20 mb-1 min-w-[188px] rounded-[var(--radius-lg)] border border-[var(--color-border)] bg-[var(--color-surface)] py-1 shadow-[var(--shadow-md)]"
                  >
                    <button
                      type="button"
                      disabled={!imageSupport}
                      title={!imageSupport ? 'Current model does not accept images' : ''}
                      onClick={() => { triggerImageUpload(); setShowAddMenu(false) }}
                      className={`flex w-full items-center gap-2.5 px-3 py-1.5 text-left text-xs text-[var(--color-foreground)] transition-colors hover:bg-[var(--color-muted)] ${
                        !imageSupport ? 'cursor-default opacity-50' : ''
                      }`}
                    >
                      <PaperClipIcon className="h-3.5 w-3.5 shrink-0 text-[var(--color-muted-foreground)]" />
                      <span>Attach files</span>
                      {pendingImages.length > 0 && (
                        <span className="ml-auto font-mono text-[10px] text-[var(--color-primary)]">{pendingImages.length}</span>
                      )}
                    </button>
                    <button
                      type="button"
                      onClick={() => insertToken('/')}
                      className="flex w-full items-center gap-2.5 px-3 py-1.5 text-left text-xs text-[var(--color-foreground)] transition-colors hover:bg-[var(--color-muted)]"
                    >
                      <span className="w-[15px] shrink-0 text-center font-mono text-sm leading-none text-[var(--color-muted-foreground)]">/</span>
                      <span>Command</span>
                    </button>
                    <button
                      type="button"
                      title={goalArmed ? 'Goal is armed — your next message becomes the objective' : 'Arm Goal mode'}
                      onClick={() => { dispatch(chatActions.setGoalArmed(!goalArmed)); setShowAddMenu(false) }}
                      className={`flex w-full items-center gap-2.5 px-3 py-1.5 text-left text-xs text-[var(--color-foreground)] transition-colors hover:bg-[var(--color-muted)] ${
                        goalArmed ? 'bg-[var(--neutral-wash)] text-[var(--color-foreground)]' : ''
                      }`}
                    >
                      <BoltIcon className={`h-3.5 w-3.5 shrink-0 ${goalArmed ? 'text-[var(--color-primary)]' : 'text-[var(--color-muted-foreground)]'}`} />
                      <span>Goal</span>
                    </button>
                  </div>
                )}
              </div>

              {/* Mode picker */}
              <div className="relative">
                <button
                  type="button"
                  aria-expanded={showModePicker}
                  onClick={(e: ReactMouseEvent) => { e.stopPropagation(); openMode() }}
                  className="inline-flex h-7 items-center gap-[7px] rounded-[var(--radius-lg)] border border-transparent bg-transparent px-2 text-xs font-medium text-[var(--color-foreground)] transition-colors hover:bg-[var(--color-muted)]"
                  style={{ paddingLeft: 6 }}
                >
                  <span className="grid h-[18px] w-[18px] shrink-0 place-items-center text-[var(--color-primary)]">
                    <currentModeDef.Icon className="h-3.5 w-3.5" />
                  </span>
                  <span
                    className={
                      mode === 'full_access'
                        ? 'text-[color-mix(in_srgb,var(--color-error-fg)_72%,var(--color-background))]'
                        : mode === 'plan'
                          ? 'text-[var(--color-success)]'
                          : ''
                    }
                  >
                    {modeLabel(mode)}
                  </span>
                  <ChevronDownIcon
                    className={`h-3 w-3 opacity-55 transition-transform ${showModePicker ? 'rotate-180' : ''}`}
                  />
                </button>
                {showModePicker && (
                  <div className="absolute bottom-full left-0 z-[var(--z-dropdown)] mb-1 w-[264px] rounded-[var(--radius-xl)] border border-[var(--color-border)] bg-[var(--color-surface)] p-1 shadow-[var(--shadow-lg)]">
                    {MODE_DEFS.map((m) => {
                      const active = mode === m.value
                      return (
                        <button
                          key={m.value}
                          type="button"
                          onClick={() => selectMode(m.value)}
                          className={`flex w-full items-start gap-2.5 rounded-[var(--radius-md)] border-none bg-transparent px-2 py-2 text-left text-[var(--color-foreground)] transition-colors hover:bg-[var(--color-muted)] ${
                            active ? 'bg-[var(--neutral-wash)]' : ''
                          }`}
                        >
                          <span
                            className={`mt-px grid h-[26px] w-[26px] shrink-0 place-items-center ${
                              m.risk === 'danger'
                                ? 'text-[color-mix(in_srgb,var(--color-error-fg)_72%,var(--color-background))]'
                                : m.risk === 'plan'
                                  ? 'text-[var(--color-success)]'
                                  : 'text-[var(--color-primary)]'
                            }`}
                          >
                            <m.Icon className="h-4 w-4" />
                          </span>
                          <span className="min-w-0 flex-1 pt-px">
                            <span
                              className={`block text-[12.5px] font-medium ${
                                active ? 'font-semibold' : ''
                              } ${
                                m.risk === 'danger'
                                  ? 'text-[color-mix(in_srgb,var(--color-error-fg)_72%,var(--color-background))]'
                                  : m.risk === 'plan'
                                    ? 'text-[var(--color-success)]'
                                    : 'text-[var(--color-foreground)]'
                              }`}
                            >
                              {m.label}
                            </span>
                            <span className="mt-px block text-[10.5px] leading-[1.4] text-[var(--color-muted-foreground)]">
                              {m.sub}
                            </span>
                          </span>
                          {active && <CheckIcon className="mt-[3px] h-3.5 w-3.5 shrink-0 text-[var(--color-primary)]" />}
                        </button>
                      )
                    })}
                  </div>
                )}
              </div>

              {/* Goal chip (armed) */}
              {goalArmed && (
                <>
                  <span aria-hidden="true" className="mx-0.5 h-4 w-px shrink-0 bg-[var(--color-border)]" />
                  <div
                    title="Goal armed — your next message replaces the current objective"
                    className="inline-flex h-[26px] items-center gap-1.5 rounded-[var(--radius-md)] bg-[var(--neutral-wash)] px-2 text-xs font-medium text-[var(--color-primary)]"
                    style={{ paddingLeft: 4, paddingRight: 9 }}
                  >
                    <button
                      type="button"
                      title="Disarm Goal"
                      onClick={() => dispatch(chatActions.setGoalArmed(false))}
                      className="grid h-4 w-4 place-items-center rounded-[var(--radius-pill)] border-none bg-[color-mix(in_srgb,var(--color-primary)_18%,transparent)] text-[var(--color-primary)] transition-colors hover:bg-[color-mix(in_srgb,var(--color-primary)_34%,transparent)]"
                    >
                      <XMarkIcon className="h-2.5 w-2.5" />
                    </button>
                    <BoltIcon className="h-3 w-3" />
                    <span>Goal</span>
                  </div>
                </>
              )}
            </div>

            {/* Toolbar-right: token ring, model picker, effort picker, stop/send */}
            <div className="flex items-center gap-2">
              {/* Context-fill ring */}
              {showTokenCount && (
                <div className="relative">
                  <button
                    type="button"
                    title="Context capacity"
                    onClick={(e) => { e.stopPropagation(); void toggleContextPopup() }}
                    className="inline-flex items-center gap-[5px] rounded-[var(--radius-sm)] border-none bg-transparent px-[5px] py-0.5 font-mono text-[10px] text-[var(--color-muted-foreground)] transition-colors hover:bg-[var(--color-secondary)] hover:text-[var(--color-foreground)]"
                  >
                    <svg width="17" height="17" viewBox="0 0 16 16" aria-hidden="true">
                      <circle
                        cx="8"
                        cy="8"
                        r="6.4"
                        fill="none"
                        stroke="color-mix(in srgb, var(--color-foreground) 20%, transparent)"
                        strokeWidth="2.2"
                      />
                      <circle
                        cx="8"
                        cy="8"
                        r="6.4"
                        fill="none"
                        stroke={ctxRingColor}
                        strokeWidth="2.2"
                        strokeLinecap="round"
                        strokeDasharray={ctxRingCirc}
                        strokeDashoffset={ctxRingOffset}
                        transform="rotate(-90 8 8)"
                        style={{ transition: 'stroke-dashoffset 0.3s ease' }}
                      />
                    </svg>
                    <span className="tabular-nums">{tokenPct}%</span>
                  </button>
                  {showContextPopup && (
                    <ContextCapacityPopup
                      loading={taskStatsLoading}
                      stats={taskStats}
                      total={tokenSnapshot.total_tokens}
                      limit={tokenSnapshot.model_context_limit}
                      prompt={tokenSnapshot.prompt_tokens}
                      completion={tokenSnapshot.completion_tokens}
                      cached={tokenSnapshot.cached_tokens}
                      reasoning={tokenSnapshot.reasoning_tokens}
                    />
                  )}
                </div>
              )}

              {/* Model picker + effort picker */}
              <div className="relative flex items-center gap-1">
                <button
                  type="button"
                  aria-expanded={showModelPicker}
                  onClick={(e: ReactMouseEvent) => { e.stopPropagation(); openModel() }}
                  className="inline-flex h-7 items-center gap-1.5 rounded-[var(--radius-lg)] border border-transparent bg-transparent px-2 text-xs font-medium text-[var(--color-foreground)] transition-colors hover:bg-[var(--color-muted)]"
                >
                  {providerName && <ProviderIcon provider={providerName} custom={providerInfoFor(providerName)?.custom} size={16} />}
                  {modelName ? getModelDisplayName(providerName, modelName) : 'model'}
                  <ChevronDownIcon
                    className={`h-3 w-3 opacity-55 transition-transform ${showModelPicker ? 'rotate-180' : ''}`}
                  />
                </button>

                {/* Effort picker */}
                {showEffortControl && (
                  <button
                    type="button"
                    aria-expanded={showEffortPicker}
                    title={t('chat.model.effortTitle')}
                    onClick={(e: ReactMouseEvent) => { e.stopPropagation(); openEffort() }}
                    className={`inline-flex h-7 items-center gap-1 rounded-[var(--radius-lg)] border border-transparent bg-transparent px-2 text-xs font-medium transition-colors hover:bg-[var(--color-muted)] ${
                      currentEffort ? 'text-[var(--color-primary)]' : 'text-[var(--color-foreground)]'
                    }`}
                  >
                    <SparklesIcon className="h-3 w-3" />
                    <span>{currentEffort || t('chat.model.effort')}</span>
                    <ChevronDownIcon
                      className={`effort-chev h-3 w-3 opacity-55 transition-transform ${showEffortPicker ? 'rotate-180' : ''}`}
                    />
                  </button>
                )}
                {showEffortControl && showEffortPicker && (
                  <div className="absolute bottom-full right-0 z-[var(--z-dropdown)] mb-1 flex min-w-[140px] flex-col gap-px rounded-[var(--radius-md)] border border-[var(--color-border)] bg-[var(--color-surface)] p-1 shadow-[var(--shadow-md)]">
                    <button
                      type="button"
                      onClick={() => pickEffort('')}
                      className={`flex w-full items-center justify-between gap-2 rounded-[var(--radius-sm)] border-none bg-transparent px-2 py-1.5 text-xs text-[var(--color-foreground)] transition-colors hover:bg-[var(--color-muted)] ${
                        !currentEffort ? 'font-semibold text-[var(--color-primary)]' : ''
                      }`}
                    >
                      <span>{t('chat.model.effortDefault')}</span>
                      {!currentEffort && <CheckIcon className="h-3.5 w-3.5 text-[var(--color-primary)]" />}
                    </button>
                    {currentEffortOptions.map((opt) => (
                      <button
                        key={opt}
                        type="button"
                        onClick={() => pickEffort(opt)}
                        className={`flex w-full items-center justify-between gap-2 rounded-[var(--radius-sm)] border-none bg-transparent px-2 py-1.5 text-xs text-[var(--color-foreground)] transition-colors hover:bg-[var(--color-muted)] ${
                          currentEffort === opt ? 'font-semibold text-[var(--color-primary)]' : ''
                        }`}
                      >
                        <span className="font-mono">{opt}</span>
                        {currentEffort === opt && <CheckIcon className="h-3.5 w-3.5 text-[var(--color-primary)]" />}
                      </button>
                    ))}
                  </div>
                )}

                {/* Model picker panel */}
                {showModelPicker && (
                  <div className="absolute bottom-full right-0 z-[var(--z-dropdown)] mb-1 flex max-h-[540px] w-[290px] flex-col overflow-hidden rounded-[var(--radius-xl)] border border-[var(--color-border)] bg-[var(--color-surface)] shadow-[var(--shadow-lg)]">
                    {/* Search */}
                    <div className="flex items-center gap-2 border-b border-[var(--color-border)] px-3 py-2 text-[var(--color-foreground)]">
                      <MagnifyingGlassIcon className="h-3.5 w-3.5" />
                      <input
                        value={modelFilter}
                        onChange={(e) => setModelFilter(e.target.value)}
                        onKeyDown={(e) => {
                          if (e.key === 'Enter') {
                            e.preventDefault()
                            selectFirstFiltered()
                          }
                        }}
                        placeholder="Filter models…"
                        className="flex-1 border-none bg-transparent text-[13px] text-[var(--color-foreground)] outline-none"
                      />
                      <kbd className="rounded-[var(--radius-sm)] border border-[var(--color-border)] bg-[var(--color-muted)] px-[5px] py-px font-mono text-[10px] text-[var(--color-muted-foreground)]">/</kbd>
                    </div>

                    {/* Pinned current row */}
                    {providerName && modelName && (
                      <div className="flex items-center gap-2.5 border-b border-[var(--color-border)] px-3 py-2">
                        <CheckCircleIcon className="h-[17px] w-[17px] shrink-0 text-[var(--color-primary)]" title="Current model" />
                        <ProviderIcon provider={providerName} custom={providerInfoFor(providerName)?.custom} size={22} />
                        <span className="flex min-w-0 flex-1 flex-col">
                          <span className="truncate text-[12.5px] text-[var(--color-foreground)]">{getModelDisplayName(providerName, modelName)}</span>
                          <span className="mt-px truncate font-mono text-[10px] text-[var(--color-muted-foreground)]">{modelSubline(modelName, currentModelInfo)}</span>
                        </span>
                        <span className="inline-flex shrink-0 items-center gap-1.5">
                          {currentModelInfo?.reasoning && <SparklesIcon className="h-[15px] w-[15px]" title="Reasoning" strokeWidth={1.9} />}
                          {currentModelInfo?.tool_call && <WrenchScrewdriverIcon className="h-[15px] w-[15px]" title="Tool use" strokeWidth={1.9} />}
                          {currentModelInfo?.image_support && <PhotoIcon className="h-[15px] w-[15px]" title="Image input" strokeWidth={1.9} />}
                        </span>
                      </div>
                    )}

                    {/* List */}
                    <div className="flex-1 overflow-y-auto p-1">
                      {/* Favorites */}
                      {favoriteModelRefs.length > 0 && (
                        <>
                          <div className="flex items-center gap-2 px-2 pb-1 pt-2 text-[10px] font-semibold uppercase tracking-[0.06em] text-[var(--color-muted-foreground)]">
                            Favorites
                            <span className="font-mono font-normal normal-case tracking-normal opacity-70">{favoriteModelRefs.length}</span>
                          </div>
                          {favoriteModelRefs.map((r) => {
                            const info = modelInfoFor(r.provider, r.model)
                            return (
                              <button
                                key={`fav-${r.provider}-${r.model}`}
                                type="button"
                                onClick={() => selectModel(r.provider, r.model)}
                                className="group flex w-full items-center gap-2.5 rounded-[var(--radius-md)] border-none bg-transparent px-2 py-1.5 text-left transition-colors hover:bg-[var(--color-muted)]"
                              >
                                <ProviderIcon provider={r.provider} custom={providerInfoFor(r.provider)?.custom} size={22} />
                                <span className="flex min-w-0 flex-1 flex-col">
                                  <span className="truncate text-[12.5px] text-[var(--color-foreground)]">{getModelDisplayName(r.provider, r.model)}</span>
                                  <span className="mt-px truncate font-mono text-[10px] text-[var(--color-muted-foreground)]">{modelSubline(r.model, info)}</span>
                                </span>
                                <span className="inline-flex shrink-0 items-center gap-1.5">
                                  {info?.reasoning && <SparklesIcon className="h-[15px] w-[15px]" title="Reasoning" strokeWidth={1.9} />}
                                  {info?.tool_call && <WrenchScrewdriverIcon className="h-[15px] w-[15px]" title="Tool use" strokeWidth={1.9} />}
                                  {info?.image_support && <PhotoIcon className="h-[15px] w-[15px]" title="Image input" strokeWidth={1.9} />}
                                </span>
                                <StarIconSolid
                                  className="h-3.5 w-3.5 shrink-0 text-[var(--color-primary)]"
                                  onClick={(e: ReactMouseEvent) => { e.stopPropagation(); void toggleFavorite(r.provider, r.model) }}
                                />
                              </button>
                            )
                          })}
                        </>
                      )}

                      {/* All providers */}
                      {pickerProviders.map((p) => (
                        <div key={p.id}>
                          <div className="flex items-center gap-2 px-2 pb-1 pt-2 text-[10px] font-semibold uppercase tracking-[0.06em] text-[var(--color-muted-foreground)]">
                            {p.name}
                          </div>
                          {p.models.map((m) => {
                            const active = providerName === p.id && modelName === m.id
                            const isFav = favoriteModels.includes(`${p.id}/${m.id}`)
                            return (
                              <div
                                key={`${p.id}-${m.id}`}
                                role="button"
                                tabIndex={0}
                                onClick={() => selectModel(p.id, m.id)}
                                onKeyDown={(e) => {
                                  if (e.key === 'Enter' || e.key === ' ') {
                                    e.preventDefault()
                                    void selectModel(p.id, m.id)
                                  }
                                }}
                                className={`group flex w-full cursor-pointer items-center gap-2.5 rounded-[var(--radius-md)] border-none bg-transparent px-2 py-1.5 text-left transition-colors hover:bg-[var(--color-muted)] ${
                                  active ? 'bg-[var(--neutral-wash)]' : ''
                                }`}
                              >
                                <ProviderIcon provider={p.id} custom={p.custom} size={22} />
                                <span className="flex min-w-0 flex-1 flex-col">
                                  <span className={`truncate text-[12.5px] leading-snug text-[var(--color-foreground)] ${active ? 'font-semibold text-[var(--color-primary)]' : ''}`}>{m.name || m.id}</span>
                                  <span className="mt-px truncate font-mono text-[10px] text-[var(--color-muted-foreground)]">{modelSubline(m.id, m)}</span>
                                </span>
                                {m.recommended && (
                                  <span className="shrink-0 rounded-[var(--radius-xs)] border border-[var(--color-border)] bg-[var(--neutral-wash-strong)] px-1.5 py-px text-[10px] font-semibold text-[var(--color-foreground)]">Recommended</span>
                                )}
                                <span className="inline-flex shrink-0 items-center gap-1.5">
                                  {m.reasoning && <SparklesIcon className="h-[15px] w-[15px]" title="Reasoning" strokeWidth={1.9} />}
                                  {m.tool_call && <WrenchScrewdriverIcon className="h-[15px] w-[15px]" title="Tool use" strokeWidth={1.9} />}
                                  {m.image_support && <PhotoIcon className="h-[15px] w-[15px]" title="Image input" strokeWidth={1.9} />}
                                  {m.image_support === false && <PhotoIcon className="h-[15px] w-[15px] text-[var(--color-warning-fg)]" title="No image input" strokeWidth={1.9} />}
                                </span>
                                {active && <CheckIcon className="h-3.5 w-3.5 shrink-0 text-[var(--color-primary)]" strokeWidth={2} />}
                                <StarIcon
                                  className={`h-3.5 w-3.5 shrink-0 cursor-pointer border-none bg-transparent transition-opacity hover:text-[var(--color-primary)] ${
                                    isFav ? 'text-[var(--color-primary)] opacity-100' : 'text-[var(--color-muted-foreground)] opacity-0 group-hover:opacity-100'
                                  }`}
                                  onClick={(e: ReactMouseEvent) => { e.stopPropagation(); void toggleFavorite(p.id, m.id) }}
                                />
                              </div>
                            )
                          })}
                        </div>
                      ))}

                      {pickerProviders.length === 0 && (
                        <div className="px-2 py-5 text-center text-[13px] text-[var(--color-muted-foreground)]">
                          {modelFilter ? 'No matching models' : 'No models available'}
                        </div>
                      )}
                    </div>

                    {/* Footer → Manage models */}
                    <div className="border-t border-[var(--color-border)] p-1.5">
                      <button
                        type="button"
                        onClick={(e: ReactMouseEvent) => { e.stopPropagation(); setShowModelPicker(false); setShowManageModels(true) }}
                        className="flex w-full items-center gap-2 rounded-[var(--radius-md)] border-none bg-transparent px-2 py-1.5 text-left text-xs text-[var(--color-foreground)] transition-colors hover:bg-[var(--color-muted)]"
                      >
                        <SquaresPlusIcon className="h-3.5 w-3.5" />
                        Manage models
                      </button>
                    </div>
                  </div>
                )}
              </div>

              {/* Stop button */}
              {isRunning && (
                <button
                  type="button"
                  title={queued.length > 0 ? 'Stop and send next' : 'Stop agent'}
                  onClick={() => actions.stop()}
                  className="flex shrink-0 items-center gap-[5px] whitespace-nowrap rounded-[var(--radius-lg)] border-none bg-[var(--color-destructive)] px-3 py-[5px] text-xs font-medium text-[var(--color-on-destructive)]"
                >
                  <StopIcon className="h-3.5 w-3.5" />
                  Stop
                </button>
              )}

              {/* Send button */}
              {showSend && (
                <button
                  type="button"
                  disabled={!canSend}
                  aria-label={isRunning ? 'Queue message' : 'Send message'}
                  onClick={send}
                  className="flex shrink-0 items-center gap-[5px] whitespace-nowrap rounded-[var(--radius-lg)] border-none bg-[var(--color-primary)] px-3 py-[5px] text-xs font-medium text-[var(--color-on-primary)] transition-[opacity,transform] disabled:cursor-not-allowed disabled:opacity-45 enabled:hover:opacity-90"
                >
                  <PaperAirplaneIcon className="h-3.5 w-3.5" />
                  {isRunning ? 'Queue' : 'Send'}
                </button>
              )}
            </div>
          </div>
        </div>
      </div>

      {/* Manage Models dialog (portal to body, like Vue Teleport) */}
      {showManageModels && createPortal(
        <ManageModelsDialog
          providers={filteredProviders}
          filter={modelFilter}
          onFilter={setModelFilter}
          visibleCount={manageVisibleCount}
          totalCount={manageTotalCount}
          onClose={async () => {
            setShowManageModels(false)
            setModelFilter('')
            // Re-hydrate the catalog (matches Vue store.fetchModels()).
            try {
              const resp = await api.models()
              dispatch(modelActions.setProviders(resp.providers))
            } catch {
              /* ignore */
            }
          }}
          onToggle={toggleModelEnabled}
        />,
        document.body,
      )}
    </div>
  )
}

// ─── Context capacity popup ─────────────────────────────────────────────────

function ContextCapacityPopup({
  loading,
  stats,
  total,
  limit,
  prompt,
  completion,
  cached,
  reasoning,
}: {
  loading: boolean
  stats: TaskStats | null
  total: number
  limit: number
  prompt: number
  completion: number
  cached?: number
  reasoning?: number
}) {
  const context = stats?.context
  const effectiveLimit = context?.context_limit || limit
  const rows = context
    ? [
        { label: 'System prompt', value: context.system_prompt_tokens },
        { label: 'System tools', value: context.system_tools_tokens },
        { label: 'MCP tools', value: context.mcp_tools_tokens },
        { label: 'Skills', value: context.skills_tokens },
        { label: 'Messages', value: context.messages_tokens },
      ]
    : [
        { label: 'Input', value: prompt },
        { label: 'Output', value: completion },
        { label: 'Cached', value: cached || 0 },
        { label: 'Reasoning', value: reasoning || 0 },
      ]
  const max = Math.max(1, ...rows.map((r) => r.value))
  const percent = effectiveLimit > 0 ? Math.min(100, Math.round((total / effectiveLimit) * 100)) : 0

  return (
    <div className="absolute bottom-full right-0 z-[var(--z-dropdown)] mb-2 w-[280px] rounded-[var(--radius-xl)] border border-[var(--color-border)] bg-[var(--color-surface)] p-3 shadow-[var(--shadow-lg)]">
      <div className="mb-2 flex items-center justify-between gap-2">
        <div>
          <div className="text-[12px] font-semibold text-[var(--color-foreground)]">Context capacity</div>
          <div className="mt-0.5 font-mono text-[10.5px] text-[var(--color-muted-foreground)]">
            {formatCompact(total)} / {effectiveLimit > 0 ? formatCompact(effectiveLimit) : '-'} tokens
          </div>
        </div>
        <span className={`font-mono text-[13px] font-semibold ${percent >= 90 ? 'text-[var(--color-destructive)]' : 'text-[var(--color-primary)]'}`}>
          {percent}%
        </span>
      </div>
      <div className="mb-2 h-1.5 overflow-hidden rounded-full bg-[var(--color-muted)]">
        <div className="h-full rounded-full bg-[var(--color-primary)]" style={{ width: `${percent}%` }} />
      </div>
      {loading ? (
        <div className="py-4 text-center text-xs text-[var(--color-muted-foreground)]">Loading...</div>
      ) : (
        <div className="space-y-1.5">
          {rows.map((row) => (
            <div key={row.label}>
              <div className="mb-0.5 flex items-center justify-between gap-2 text-[10.5px]">
                <span className="text-[var(--color-muted-foreground)]">{row.label}</span>
                <span className="font-mono text-[var(--color-foreground)]">{formatCompact(row.value)}</span>
              </div>
              <div className="h-1 overflow-hidden rounded-full bg-[var(--color-muted)]">
                <div className="h-full rounded-full bg-[var(--accent-fill)]" style={{ width: `${Math.max(2, (row.value / max) * 100)}%` }} />
              </div>
            </div>
          ))}
        </div>
      )}
      {stats?.cache_supported && (
        <div className="mt-2 rounded-[var(--radius-md)] bg-[var(--color-muted)] px-2 py-1.5 text-[10.5px] text-[var(--color-muted-foreground)]">
          Cache hit rate: <span className="font-mono text-[var(--color-foreground)]">{Math.round(stats.cache_hit_rate * 100)}%</span>
        </div>
      )}
    </div>
  )
}

function formatCompact(n: number): string {
  if (!Number.isFinite(n)) return '0'
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(n >= 10_000_000 ? 0 : 1)}M`
  if (n >= 1000) return `${Math.round(n / 1000)}K`
  return String(n)
}

// ─── Manage Models dialog ────────────────────────────────────────────────────

interface ManageModelsDialogProps {
  providers: ProviderInfo[]
  filter: string
  onFilter: (v: string) => void
  visibleCount: number
  totalCount: number
  onClose: () => void
  onToggle: (provider: string, model: string, enabled: boolean) => void
}

function ManageModelsDialog({
  providers,
  filter,
  onFilter,
  visibleCount,
  totalCount,
  onClose,
  onToggle,
}: ManageModelsDialogProps) {
  return (
    <div
      className="fixed inset-0 z-[var(--z-modal)] flex items-center justify-center bg-[var(--backdrop)]"
      style={{ backdropFilter: 'blur(6px)', WebkitBackdropFilter: 'blur(6px)' }}
      onClick={onClose}
    >
      <div
        role="dialog"
        aria-modal="true"
        aria-label="Manage models"
        onClick={(e) => e.stopPropagation()}
        className="m-4 flex max-h-[70vh] min-w-0 flex-col overflow-hidden rounded-[var(--radius-xl)] border border-[var(--color-border)] bg-[var(--color-surface)] shadow-[var(--shadow-lg)]"
        style={{ width: 'min(560px, 94vw)' }}
      >
        {/* Header */}
        <div className="flex items-start gap-3 border-b border-[var(--color-border)] px-[18px] py-4">
          <div className="grid h-[30px] w-[30px] shrink-0 place-items-center rounded-[var(--radius-md)] bg-[var(--neutral-wash)] text-[var(--color-primary)]">
            <SquaresPlusIcon className="h-4 w-4" />
          </div>
          <div className="min-w-0 flex-1">
            <h3 className="m-0 text-sm font-semibold tracking-[-0.01em] text-[var(--color-foreground)]">Manage models</h3>
            <p className="mt-0.5 text-[11.5px] leading-[1.45] text-[var(--color-muted-foreground)]">
              Toggle which models appear in the picker.
            </p>
          </div>
          <button
            type="button"
            aria-label="Close"
            onClick={onClose}
            className="ml-auto grid h-7 w-7 shrink-0 place-items-center rounded-[var(--radius-md)] border border-transparent bg-transparent text-[var(--color-muted-foreground)] transition-colors hover:border-[var(--color-border)] hover:bg-[var(--color-muted)] hover:text-[var(--color-foreground)]"
          >
            <XMarkIcon className="h-4 w-4" />
          </button>
        </div>

        {/* Filter row */}
        <div className="flex items-center gap-2 border-b border-[var(--color-border)] px-[18px] py-2.5">
          <div className="flex h-[30px] flex-1 items-center gap-2 rounded-[var(--radius-md)] border border-[var(--color-border)] bg-[var(--color-muted)] px-2.5 text-[var(--color-muted-foreground)] transition-colors focus-within:border-[var(--color-primary)]">
            <MagnifyingGlassIcon className="h-3.5 w-3.5" />
            <input
              value={filter}
              onChange={(e) => onFilter(e.target.value)}
              placeholder="Filter models…"
              className="flex-1 border-none bg-transparent text-[12.5px] text-[var(--color-foreground)] outline-none"
            />
          </div>
          <span className="shrink-0 font-mono text-[11px] text-[var(--color-muted-foreground)]">{visibleCount} / {totalCount}</span>
        </div>

        {/* Body */}
        <div className="flex-1 overflow-y-auto px-2.5 pb-2.5 pt-1.5">
          {providers.map((p) => (
            <div key={`mgr-${p.id}`}>
              <div className="sticky top-0 z-[1] flex items-center gap-2 bg-[var(--color-surface)] px-2 pb-1.5 pt-3">
                <ProviderIcon provider={p.id} custom={p.custom} size={18} />
                <span className="text-[11px] font-semibold text-[var(--color-foreground)]">{p.name}</span>
                <span className="font-mono text-[10px] text-[var(--color-muted-foreground)]">{p.id}</span>
                <span className="ml-auto font-mono text-[10px] text-[var(--color-muted-foreground)]">{p.models.length}</span>
              </div>
              {p.models.map((m) => {
                const off = m.enabled === false
                return (
                  <div
                    key={`mgr-${p.id}-${m.id}`}
                    className={`flex items-center gap-2.5 rounded-[var(--radius-md)] px-2 py-2 transition-colors hover:bg-[var(--color-muted)] ${off ? 'opacity-50' : ''}`}
                  >
                    <ProviderIcon provider={p.id} custom={p.custom} size={18} />
                    <span className="flex min-w-0 flex-1 flex-col">
                      <span className={`text-[12.5px] leading-snug ${off ? 'text-[var(--color-muted-foreground)]' : 'text-[var(--color-foreground)]'}`}>{m.name || m.id}</span>
                      <span className="mt-px font-mono text-[10px] text-[var(--color-muted-foreground)]">{modelSubline(m.id, m)}</span>
                    </span>
                    {m.recommended && (
                      <span className="shrink-0 rounded-[var(--radius-xs)] border border-[var(--color-border)] bg-[var(--neutral-wash-strong)] px-1.5 py-px text-[10px] font-semibold text-[var(--color-foreground)]">Recommended</span>
                    )}
                    <button
                      type="button"
                      role="switch"
                      aria-checked={off ? 'false' : 'true'}
                      aria-label={off ? 'Enable model' : 'Disable model'}
                      onClick={() => onToggle(p.id, m.id, off)}
                      className="relative h-[18px] w-8 shrink-0 cursor-pointer rounded-[var(--radius-pill)] border-none bg-[var(--color-border)] transition-colors"
                      style={off ? undefined : { background: 'var(--color-primary)' }}
                    >
                      <span
                        className="absolute top-0.5 h-3.5 w-3.5 rounded-full bg-[var(--color-surface)] shadow-[var(--shadow-sm)] transition-transform"
                        style={{ left: 2, transform: off ? 'none' : 'translateX(14px)' }}
                      />
                    </button>
                  </div>
                )
              })}
            </div>
          ))}
          {providers.length === 0 && (
            <div className="py-8 text-center text-[13px] text-[var(--color-muted-foreground)]">
              {filter ? 'No matching models' : 'No models available'}
            </div>
          )}
        </div>

        {/* Footer */}
        <div className="flex items-center justify-between border-t border-[var(--color-border)] bg-[var(--color-muted)] px-[18px] py-3">
          <span className="text-[11px] text-[var(--color-muted-foreground)]">
            {visibleCount} visible of {totalCount} models
          </span>
          <div className="flex gap-2">
            <button
              type="button"
              onClick={onClose}
              className="inline-flex h-[30px] items-center gap-1.5 rounded-[var(--radius-md)] border border-transparent bg-[var(--color-primary)] px-3.5 text-xs font-medium text-[var(--color-on-primary)] transition-colors hover:bg-[color-mix(in_srgb,var(--color-primary)_88%,var(--color-background))]"
            >
              Done
            </button>
          </div>
        </div>
      </div>
    </div>
  )
}
