/**
 * Sidebar — chat/session list + nav (chat / automations / channels).
 *
 * A closer port of the Vue Sidebar: the session list is enriched with task
 * metadata (pinned/archived/unread/running/updated_at) so it can be filtered
 * (status / time window / sort), grouped by recency (Today / Yesterday /
 * This Week / Older), and acted on via a right-click context menu
 * (Pin / Archive / Mark read / Delete) backed by `api.updateTask`.
 *
 * Kept from the prior React version: the nav buttons, the "New chat" button,
 * the footer (ThemeToggle + settings), and the `loadSession` thunk on click.
 */

import { useEffect, useMemo, useRef, useState } from 'react'
import {
  PlusIcon,
  ChatBubbleLeftIcon,
  BoltIcon,
  ChatBubbleOvalLeftIcon,
  Cog6ToothIcon,
  AdjustmentsHorizontalIcon,
  BookmarkIcon,
  ArchiveBoxIcon,
  ArchiveBoxArrowDownIcon,
  EnvelopeOpenIcon,
  TrashIcon,
  CheckIcon,
  EllipsisHorizontalIcon,
} from '@heroicons/react/24/outline'
import { useAppDispatch, useAppSelector } from '../app/hooks'
import { uiActions, sessionActions, chatActions, loadSession } from '../app/store'
import { api } from '../lib/api'
import type { SessionItem, TaskItem } from '../lib/types'
import { ThemeToggle } from './ThemeToggle'

// ─── Filter state (local; mirrors SidebarFilterMenu.vue's options) ───

type StatusFilter = 'all' | 'active' | 'archived'
type TimeFilter = 'all' | 'today' | 'week' | 'month'
type SortFilter = 'recent' | 'title'

interface FilterState {
  status: StatusFilter
  time: TimeFilter
  sort: SortFilter
}

const DEFAULT_FILTERS: FilterState = { status: 'all', time: 'all', sort: 'recent' }

const STATUS_OPTIONS: { value: StatusFilter; label: string }[] = [
  { value: 'all', label: 'All' },
  { value: 'active', label: 'Active' },
  { value: 'archived', label: 'Archived' },
]
const TIME_OPTIONS: { value: TimeFilter; label: string }[] = [
  { value: 'all', label: 'All time' },
  { value: 'today', label: 'Today' },
  { value: 'week', label: 'This week' },
  { value: 'month', label: 'This month' },
]
const SORT_OPTIONS: { value: SortFilter; label: string }[] = [
  { value: 'recent', label: 'Recent' },
  { value: 'title', label: 'Title' },
]

// ─── Enriched row: a session joined with its task metadata ───

interface SessionRow {
  uuid: string
  title: string
  created_at: string
  updated_at: string
  pinned: boolean
  archived: boolean
  unread: boolean
  running: boolean
}

interface DateGroup {
  key: string
  label: string
  items: SessionRow[]
}

const DAY = 86400000

export function Sidebar() {
  const dispatch = useAppDispatch()
  const sessions = useAppSelector((s) => s.session.sessions)
  const tasks = useAppSelector((s) => s.session.tasks)
  const currentSessionId = useAppSelector((s) => s.session.currentSessionId)
  const activeView = useAppSelector((s) => s.ui.activeView)

  // Filters live in local state (the Vue app keeps them in the project store;
  // here they're component-local, which is enough for a single sidebar).
  const [filters, setFilters] = useState<FilterState>(DEFAULT_FILTERS)
  const [filterOpen, setFilterOpen] = useState(false)
  const filterRef = useRef<HTMLDivElement | null>(null)

  // Right-click / ⋯ context menu.
  const [ctx, setCtx] = useState<{ x: number; y: number; row: SessionRow } | null>(null)
  const ctxRef = useRef<HTMLDivElement | null>(null)

  // A coarse clock so time-based filtering/grouping re-evaluates as the wall
  // clock advances (Date.now() isn't a reactive dep on its own).
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    const t = setInterval(() => setNow(Date.now()), 60000)
    return () => clearInterval(t)
  }, [])

  // Join sessions with task metadata by uuid. Sessions are the current-project
  // conversation list (guaranteed correct scope); tasks carry the pin/archive/
  // unread/running/updated_at fields the filter + context menu need.
  const taskMap = useMemo(() => new Map(tasks.map((t) => [t.uuid, t])), [tasks])
  const rows = useMemo<SessionRow[]>(() => {
    return sessions.map((s: SessionItem) => {
      const t: TaskItem | undefined = taskMap.get(s.uuid)
      return {
        uuid: s.uuid,
        title: s.title || t?.title || '',
        created_at: s.created_at || t?.created_at || '',
        updated_at: t?.updated_at || s.created_at || t?.created_at || '',
        pinned: t?.pinned ?? false,
        archived: t?.archived ?? false,
        unread: t?.unread ?? false,
        running: t?.running ?? false,
      }
    })
  }, [sessions, taskMap])

  // ── Filter pipeline ──

  const filtered = useMemo(() => {
    return rows.filter((r) => {
      // The open conversation always stays visible regardless of filters —
      // otherwise archiving or a narrowing window would strand it.
      if (r.uuid === currentSessionId) return true
      if (filters.status === 'active' && r.archived) return false
      if (filters.status === 'archived' && !r.archived) return false
      if (filters.time !== 'all') {
        const ts = r.updated_at || r.created_at || ''
        const then = new Date(ts).getTime()
        if (Number.isNaN(then)) return false
        const span = filters.time === 'today' ? DAY : filters.time === 'week' ? 7 * DAY : 30 * DAY
        if (now - then > span) return false
      }
      return true
    })
  }, [rows, filters, currentSessionId, now])

  // ── Sort: running first, then pinned, then the chosen key ──

  const sorted = useMemo(() => {
    const arr = [...filtered]
    arr.sort((a, b) => {
      if (a.running !== b.running) return a.running ? -1 : 1
      if (a.pinned !== b.pinned) return a.pinned ? -1 : 1
      if (filters.sort === 'title') return rowTitle(a).localeCompare(rowTitle(b))
      return (b.updated_at || '').localeCompare(a.updated_at || '')
    })
    return arr
  }, [filtered, filters.sort])

  // ── Group by recency (calendar-day based for today/yesterday) ──

  const groups = useMemo<DateGroup[]>(() => {
    const map = new Map<string, SessionRow[]>()
    for (const r of sorted) {
      const key = bucketFor(r.updated_at || r.created_at || '', now)
      const arr = map.get(key)
      if (arr) arr.push(r)
      else map.set(key, [r])
    }
    return BUCKET_ORDER.filter((k) => map.has(k)).map((k) => ({
      key: k,
      label: BUCKET_LABEL[k],
      items: map.get(k)!,
    }))
  }, [sorted, now])

  // ── Outside-click / Esc handling for the filter + context menus ──

  useEffect(() => {
    if (!filterOpen) return
    function onDown(e: MouseEvent) {
      if (filterRef.current && !filterRef.current.contains(e.target as Node)) setFilterOpen(false)
    }
    function onKey(e: KeyboardEvent) {
      if (e.key === 'Escape') setFilterOpen(false)
    }
    document.addEventListener('mousedown', onDown)
    document.addEventListener('keydown', onKey)
    return () => {
      document.removeEventListener('mousedown', onDown)
      document.removeEventListener('keydown', onKey)
    }
  }, [filterOpen])

  useEffect(() => {
    if (!ctx) return
    function onDown(e: MouseEvent) {
      if (ctxRef.current && !ctxRef.current.contains(e.target as Node)) setCtx(null)
    }
    function onKey(e: KeyboardEvent) {
      if (e.key === 'Escape') setCtx(null)
    }
    document.addEventListener('mousedown', onDown)
    document.addEventListener('keydown', onKey)
    window.addEventListener('blur', onKey as never)
    return () => {
      document.removeEventListener('mousedown', onDown)
      document.removeEventListener('keydown', onKey)
      window.removeEventListener('blur', onKey as never)
    }
  }, [ctx])

  const isDirty =
    filters.status !== DEFAULT_FILTERS.status ||
    filters.time !== DEFAULT_FILTERS.time ||
    filters.sort !== DEFAULT_FILTERS.sort

  // ── Actions ──

  async function newChat() {
    dispatch(chatActions.clearChat())
    dispatch(sessionActions.setCurrentSession(''))
    dispatch(uiActions.setView('chat'))
    try {
      const resp = await api.newSession()
      dispatch(sessionActions.setCurrentSession(resp.session_id))
      const fresh = await api.sessions()
      dispatch(sessionActions.setSessions(fresh))
    } catch {
      // surfaced via health/gate
    }
  }

  async function openItem(row: SessionRow) {
    dispatch(uiActions.setView('chat'))
    if (row.unread) await patchTask(row.uuid, { unread: false })
    await dispatch(loadSession(row.uuid))
  }

  // Apply a task metadata patch via the API and merge the result into the
  // store's task list so the UI reflects it immediately.
  async function patchTask(uuid: string, patch: Parameters<typeof api.updateTask>[1]) {
    try {
      const updated = await api.updateTask(uuid, patch)
      const next = tasks.map((t) => (t.uuid === uuid ? updated : t))
      dispatch(sessionActions.setTasks(next))
    } catch {
      // ignore — the next tasks refresh will reconcile
    }
  }

  async function deleteItem(row: SessionRow) {
    const wasActive = row.uuid === currentSessionId
    try {
      await api.deleteSession(row.uuid)
    } catch {
      return
    }
    dispatch(sessionActions.setSessions(sessions.filter((s) => s.uuid !== row.uuid)))
    dispatch(sessionActions.setTasks(tasks.filter((t) => t.uuid !== row.uuid)))
    if (wasActive) {
      dispatch(chatActions.clearChat())
      dispatch(sessionActions.setCurrentSession(''))
      dispatch(chatActions.setRunning(false))
    }
  }

  function openContext(e: React.MouseEvent, row: SessionRow) {
    e.preventDefault()
    e.stopPropagation()
    // Clamp so the menu never overflows the viewport.
    const x = Math.min(e.clientX, window.innerWidth - 180)
    const y = Math.min(e.clientY, window.innerHeight - 200)
    setCtx({ x, y, row })
  }

  function openContextFromButton(e: React.MouseEvent, row: SessionRow) {
    e.preventDefault()
    e.stopPropagation()
    const rect = (e.currentTarget as HTMLElement).getBoundingClientRect()
    const x = Math.min(rect.left, window.innerWidth - 180)
    // Flip up if there's no room below.
    const below = rect.bottom + 210 > window.innerHeight
    const y = below ? Math.max(8, rect.top - 200) : rect.bottom + 4
    setCtx({ x, y, row })
  }

  function setFilter<K extends keyof FilterState>(key: K, value: FilterState[K]) {
    setFilters((f) => ({ ...f, [key]: value }))
  }

  const ctxRow = ctx?.row

  return (
    <aside className="flex w-[var(--sidebar-width)] shrink-0 flex-col border-r border-[var(--color-border)] bg-[var(--color-sidebar-bg)]">
      {/* New chat */}
      <div className="p-2">
        <button
          type="button"
          onClick={newChat}
          className="sb-newchat flex w-full items-center gap-2 rounded-[var(--radius-lg)] bg-[var(--color-primary)] px-3 py-2 text-sm font-medium text-[var(--color-on-primary)] hover:bg-[var(--accent-wash-strong)]"
        >
          <PlusIcon className="h-4 w-4" />
          New chat
        </button>
      </div>

      {/* View nav */}
      <nav className="px-2 pb-1">
        <NavItem icon={ChatBubbleLeftIcon} label="Chat" active={activeView === 'chat'} onClick={() => dispatch(uiActions.setView('chat'))} />
        <NavItem icon={BoltIcon} label="Automations" active={activeView === 'automations'} onClick={() => dispatch(uiActions.setView('automations'))} />
        <NavItem icon={ChatBubbleOvalLeftIcon} label="Channels" active={activeView === 'channels'} onClick={() => dispatch(uiActions.setView('channels'))} />
      </nav>

      {/* Chat heading + filter menu */}
      <div className="flex items-center justify-between px-3 pb-1 pt-2">
        <span className="text-[0.65rem] font-semibold uppercase tracking-[0.06em] text-[var(--color-muted-foreground)]">
          Chat
        </span>
        <div ref={filterRef} className="relative inline-flex">
          <button
            type="button"
            onClick={() => setFilterOpen((v) => !v)}
            title="Filter conversations"
            aria-label="Filter conversations"
            aria-expanded={filterOpen}
            className={`relative grid h-[22px] w-[22px] place-items-center rounded-[var(--radius-sm)] text-[var(--color-muted-foreground)] transition-colors hover:bg-[var(--color-muted)] hover:text-[var(--color-foreground)] ${
              filterOpen || isDirty ? 'text-[var(--color-foreground)]' : ''
            } ${filterOpen ? 'bg-[var(--color-muted)]' : ''}`}
          >
            <AdjustmentsHorizontalIcon className="h-4 w-4" />
            {isDirty && (
              <span className="absolute right-[1px] top-[1px] h-[5px] w-[5px] rounded-full bg-[var(--color-accent-neutral)]" aria-hidden="true" />
            )}
          </button>
          {filterOpen && (
            <div
              className="sb-pop absolute right-0 top-full z-[var(--z-dropdown)] mt-1 w-[232px] rounded-[var(--radius-lg)] border border-[var(--color-border)] bg-[var(--color-surface)] p-1 shadow-[var(--shadow-md)]"
              role="menu"
            >
              <FilterSection label="Status" options={STATUS_OPTIONS} value={filters.status} onSelect={(v) => setFilter('status', v)} />
              <div className="my-1 h-px bg-[var(--color-border)]" />
              <FilterSection label="Time" options={TIME_OPTIONS} value={filters.time} onSelect={(v) => setFilter('time', v)} />
              <div className="my-1 h-px bg-[var(--color-border)]" />
              <FilterSection label="Sort" options={SORT_OPTIONS} value={filters.sort} onSelect={(v) => setFilter('sort', v)} />
              {isDirty && (
                <>
                  <div className="my-1 h-px bg-[var(--color-border)]" />
                  <button
                    type="button"
                    onClick={() => setFilters(DEFAULT_FILTERS)}
                    className="flex w-full items-center gap-2 rounded-[var(--radius-md)] px-2 py-1.5 text-left text-[12.5px] text-[var(--color-muted-foreground)] transition-colors hover:bg-[var(--color-muted)]"
                  >
                    Reset
                  </button>
                </>
              )}
            </div>
          )}
        </div>
      </div>

      {/* Session list, grouped by recency */}
      <div className="min-h-0 flex-1 overflow-y-auto px-1 pb-2">
        {groups.length === 0 ? (
          <div className="px-3 py-6 text-center text-xs text-[var(--color-muted-foreground)]">No conversations yet</div>
        ) : (
          groups.map((g) => (
            <div key={g.key} className="mb-0.5">
              <div className="flex items-center gap-1.5 px-2 pb-1 pt-2">
                <span className="flex-1 text-[0.65rem] font-semibold uppercase tracking-[0.06em] text-[var(--color-muted-foreground)]">{g.label}</span>
                <span className="font-mono text-[0.625rem] text-[var(--color-muted-foreground)]">{g.items.length}</span>
              </div>
              {g.items.map((row) => {
                const active = row.uuid === currentSessionId
                return (
                  <div
                    key={row.uuid}
                    role="button"
                    tabIndex={0}
                    onClick={() => openItem(row)}
                    onKeyDown={(e) => {
                      if (e.key === 'Enter' || e.key === ' ') {
                        e.preventDefault()
                        openItem(row)
                      }
                    }}
                    onContextMenu={(e) => openContext(e, row)}
                    className={`sb-row group flex w-full cursor-pointer items-center gap-2 rounded-[var(--radius-md)] border-l border-transparent px-2.5 py-1.5 text-left text-sm transition-colors ${
                      active
                        ? 'border-l-[var(--color-accent-neutral)] bg-[var(--accent-wash)] text-[var(--color-foreground)]'
                        : row.archived
                          ? 'text-[var(--color-muted-foreground)] hover:bg-[var(--neutral-wash-soft)]'
                          : 'text-[var(--color-foreground)] hover:bg-[var(--neutral-wash-soft)]'
                    } ${row.running ? 'border-l-[color-mix(in_srgb,var(--color-accent)_50%,transparent)]' : ''}`}
                  >
                    {row.running ? (
                      <span className="sb-ring h-[11px] w-[11px] shrink-0 rounded-full border-[1.6px] border-[var(--color-accent)]" aria-hidden="true" />
                    ) : (
                      <span
                        className={`h-[6px] w-[6px] shrink-0 rounded-full ${row.unread ? 'bg-[var(--color-accent-neutral)]' : 'bg-transparent'}`}
                        aria-hidden="true"
                      />
                    )}
                    {row.pinned && <BookmarkIcon className="h-2.5 w-2.5 shrink-0 text-[var(--color-accent-neutral)]" />}
                    <span className="min-w-0 flex-1 truncate">{rowTitle(row)}</span>
                    <span className={`shrink-0 font-mono text-[0.625rem] ${row.running ? 'text-[var(--color-accent)]' : 'text-[var(--color-muted-foreground)]'}`}>
                      {row.running ? 'running' : relativeTime(row.updated_at || row.created_at, now)}
                    </span>
                    <button
                      type="button"
                      title="Actions"
                      aria-label="Actions"
                      onClick={(e) => openContextFromButton(e, row)}
                      className="grid h-5 w-5 shrink-0 place-items-center rounded-[var(--radius-sm)] text-[var(--color-muted-foreground)] opacity-0 transition-all hover:bg-[var(--color-secondary)] hover:text-[var(--color-foreground)] group-hover:opacity-100"
                    >
                      <EllipsisHorizontalIcon className="h-3.5 w-3.5" />
                    </button>
                  </div>
                )
              })}
            </div>
          ))
        )}
      </div>

      {/* Footer: theme toggle + settings (matches Vue sidebar footer) */}
      <div className="flex shrink-0 items-center justify-end gap-1 px-3 py-2">
        <ThemeToggle compact />
        <button
          type="button"
          onClick={() => dispatch(uiActions.setSettingsOpen(true))}
          className="rounded-[var(--radius-md)] p-1.5 text-[var(--color-muted-foreground)] transition-colors hover:bg-[var(--neutral-wash-soft)] hover:text-[var(--color-foreground)]"
          aria-label="Settings"
          title="Settings (⌘,)"
        >
          <Cog6ToothIcon className="h-3.5 w-3.5" />
        </button>
      </div>

      {/* Right-click / ⋯ context menu (fixed-positioned at the cursor) */}
      {ctx && ctxRow && (
        <div
          ref={ctxRef}
          role="menu"
          className="sb-pop fixed z-[var(--z-dropdown)] min-w-[170px] rounded-[var(--radius-lg)] border border-[var(--color-border)] bg-[var(--color-surface)] p-1 shadow-[var(--shadow-md)]"
          style={{ left: ctx.x, top: ctx.y }}
        >
          <CtxItem icon={BookmarkIcon} label={ctxRow.pinned ? 'Unpin' : 'Pin'} onClick={() => { void patchTask(ctxRow.uuid, { pinned: !ctxRow.pinned }); setCtx(null) }} />
          <CtxItem
            icon={ctxRow.archived ? ArchiveBoxArrowDownIcon : ArchiveBoxIcon}
            label={ctxRow.archived ? 'Unarchive' : 'Archive'}
            onClick={() => { void patchTask(ctxRow.uuid, { archived: !ctxRow.archived }); setCtx(null) }}
          />
          <CtxItem icon={EnvelopeOpenIcon} label={ctxRow.unread ? 'Mark read' : 'Mark unread'} onClick={() => { void patchTask(ctxRow.uuid, { unread: !ctxRow.unread }); setCtx(null) }} />
          <div className="my-1 h-px bg-[var(--color-border)]" />
          <CtxItem icon={TrashIcon} label="Delete" danger onClick={() => { void deleteItem(ctxRow); setCtx(null) }} />
        </div>
      )}

      {/* Animations: running ring breathe + pop-in for menus. Scoped via sb-* names. */}
      <style>{`
        @keyframes sb-ring-breathe { 0%,100% { opacity:0.35; transform:scale(0.78); } 50% { opacity:1; transform:scale(1); } }
        @keyframes sb-pop-in { from { opacity:0; transform:translateY(-4px); } to { opacity:1; transform:none; } }
        .sb-ring { animation: sb-ring-breathe 1.6s ease-in-out infinite; }
        .sb-pop { animation: sb-pop-in 0.12s ease; }
        .sb-newchat { transition: background 0.15s, transform 0.1s; }
        .sb-newchat:active { transform: scale(0.98); }
        .sb-row { transition: background 0.15s, border-color 0.15s; }
        @media (prefers-reduced-motion: reduce) {
          .sb-ring { animation: none; opacity: 1; transform: none; }
          .sb-pop { animation: none; }
        }
      `}</style>
    </aside>
  )
}

// ─── Sub-components ───

function FilterSection<T extends string>({
  label,
  options,
  value,
  onSelect,
}: {
  label: string
  options: { value: T; label: string }[]
  value: T
  onSelect: (v: T) => void
}) {
  return (
    <div>
      <div className="px-2 py-1 text-[0.625rem] font-semibold uppercase tracking-wide text-[var(--color-muted-foreground)]">{label}</div>
      {options.map((o) => {
        const selected = o.value === value
        return (
          <button
            key={o.value}
            type="button"
            onClick={() => onSelect(o.value)}
            className={`flex w-full items-center gap-2 rounded-[var(--radius-md)] px-2 py-1.5 text-left text-[12.5px] transition-colors hover:bg-[var(--color-muted)] ${
              selected ? 'text-[var(--color-accent-neutral)]' : 'text-[var(--color-foreground)]'
            }`}
          >
            <span className="flex-1 truncate">{o.label}</span>
            {selected && <CheckIcon className="h-3.5 w-3.5 shrink-0 text-[var(--color-accent-neutral)]" />}
          </button>
        )
      })}
    </div>
  )
}

function CtxItem({
  icon: Icon,
  label,
  onClick,
  danger,
}: {
  icon: React.ComponentType<{ className?: string }>
  label: string
  onClick: () => void
  danger?: boolean
}) {
  return (
    <button
      type="button"
      role="menuitem"
      onClick={onClick}
      className={`flex w-full items-center gap-2 rounded-[var(--radius-md)] px-2 py-1.5 text-left text-[12.5px] transition-colors hover:bg-[var(--color-muted)] ${
        danger ? 'text-[var(--color-destructive)]' : 'text-[var(--color-foreground)]'
      }`}
    >
      <Icon className="h-3.5 w-3.5" />
      {label}
    </button>
  )
}

function NavItem({
  icon: Icon,
  label,
  active,
  onClick,
}: {
  icon: React.ComponentType<{ className?: string }>
  label: string
  active: boolean
  onClick: () => void
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={`flex w-full items-center gap-2 rounded-[var(--radius-md)] px-2.5 py-1.5 text-sm transition-colors ${
        active
          ? 'bg-[var(--accent-wash)] font-medium text-[var(--color-foreground)]'
          : 'text-[var(--color-foreground)] hover:bg-[var(--neutral-wash-soft)]'
      }`}
    >
      <Icon className="h-4 w-4" />
      {label}
    </button>
  )
}

// ─── Helpers ───

function rowTitle(r: SessionRow): string {
  return r.title || r.uuid.slice(0, 8) + '…'
}

const BUCKET_ORDER = ['today', 'yesterday', 'week', 'older'] as const
const BUCKET_LABEL: Record<(typeof BUCKET_ORDER)[number], string> = {
  today: 'Today',
  yesterday: 'Yesterday',
  week: 'This Week',
  older: 'Older',
}

function bucketFor(ts: string, now: number): (typeof BUCKET_ORDER)[number] {
  const then = new Date(ts).getTime()
  if (Number.isNaN(then)) return 'older'
  const today = new Date(now)
  const startToday = new Date(today.getFullYear(), today.getMonth(), today.getDate()).getTime()
  if (then >= startToday) return 'today'
  if (then >= startToday - DAY) return 'yesterday'
  if (then >= startToday - 7 * DAY) return 'week'
  return 'older'
}

function relativeTime(ts: string, now: number): string {
  if (!ts) return ''
  const then = new Date(ts).getTime()
  if (Number.isNaN(then)) return ''
  const mins = Math.floor((now - then) / 60000)
  if (mins < 1) return 'now'
  if (mins < 60) return `${mins}m`
  const hrs = Math.floor(mins / 60)
  if (hrs < 24) return `${hrs}h`
  const days = Math.floor(hrs / 24)
  if (days < 30) return `${days}d`
  return new Date(ts).toLocaleDateString([], { month: 'short', day: 'numeric' })
}
