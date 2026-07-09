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
  ChevronRightIcon,
  FolderIcon,
  FolderOpenIcon,
  ServerIcon,
  SparklesIcon,
  SignalIcon,
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
import { useTranslation } from 'react-i18next'
import { useAppDispatch, useAppSelector } from '../app/hooks'
import { uiActions, sessionActions, chatActions, loadSession, loadTasks, loadWorkspaceState } from '../app/store'
import { api } from '../lib/api'
import type { TaskItem } from '../lib/types'
import { ThemeToggle } from './ThemeToggle'

// ─── Filter state (local; mirrors SidebarFilterMenu.vue's options) ───

type StatusFilter = 'all' | 'active' | 'archived'
type LastActivityFilter = 'all' | 'today' | 'week' | 'month'
type GroupByMode = 'project' | 'date'
type SortFilter = 'recency' | 'name' | 'created'

interface FilterState {
  status: StatusFilter
  project: string
  lastActivity: LastActivityFilter
  groupBy: GroupByMode
  sort: SortFilter
}

const FILTERS_KEY = 'jcode_sidebar_filters'
const DEFAULT_FILTERS: FilterState = {
  status: 'active',
  project: '',
  lastActivity: 'all',
  groupBy: 'project',
  sort: 'recency',
}

// ─── Enriched row: a session joined with its task metadata ───

interface SessionRow {
  uuid: string
  project: string
  title: string
  created_at: string
  updated_at: string
  pinned: boolean
  archived: boolean
  unread: boolean
  running: boolean
}

interface SidebarGroup {
  key: string
  kind: 'project' | 'date'
  label: string
  path?: string
  items: SessionRow[]
}

const DAY = 86400000

export function Sidebar() {
  const { t } = useTranslation()
  const dispatch = useAppDispatch()
  const sessions = useAppSelector((s) => s.session.sessions)
  const tasks = useAppSelector((s) => s.session.tasks)
  const currentSessionId = useAppSelector((s) => s.session.currentSessionId)
  const activePath = useAppSelector((s) => s.session.projectPath)
  const activeView = useAppSelector((s) => s.ui.activeView)

  const [filters, setFilters] = useState<FilterState>(() => loadFilters())
  const [filterOpen, setFilterOpen] = useState(false)
  const filterRef = useRef<HTMLDivElement | null>(null)
  const [expanded, setExpanded] = useState<Set<string>>(() => new Set(activePath ? [activePath] : []))

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

  useEffect(() => {
    try {
      localStorage.setItem(FILTERS_KEY, JSON.stringify(filters))
    } catch {
      // localStorage may be unavailable in hardened webviews.
    }
  }, [filters])

  useEffect(() => {
    if (!activePath) return
    setExpanded((prev) => {
      if (prev.has(activePath)) return prev
      const next = new Set(prev)
      next.add(activePath)
      return next
    })
  }, [activePath])

  // Vue's sidebar is task-first: /api/tasks is the cross-project source of
  // truth, while /api/sessions only describes the active project. Keep a small
  // sessions fallback for a freshly-created local task before /api/tasks catches
  // up.
  const rows = useMemo<SessionRow[]>(() => {
    const out = tasks.map(taskToRow)
    const seen = new Set(out.map((r) => r.uuid))
    for (const s of sessions) {
      if (seen.has(s.uuid)) continue
      out.push({
        uuid: s.uuid,
        project: activePath,
        title: s.title || '',
        created_at: s.created_at || '',
        updated_at: s.created_at || '',
        pinned: false,
        archived: false,
        unread: false,
        running: false,
      })
    }
    return out
  }, [tasks, sessions, activePath])

  const projects = useMemo(() => {
    const map = new Map<string, string>()
    if (activePath) map.set(activePath, projectName(activePath))
    for (const r of rows) {
      if (r.project) map.set(r.project, projectName(r.project))
    }
    return [...map].map(([path, name]) => ({ path, name })).sort((a, b) => {
      if (a.path === activePath) return -1
      if (b.path === activePath) return 1
      return a.name.localeCompare(b.name)
    })
  }, [rows, activePath])

  const projectFilter = useMemo(() => {
    if (!filters.project) return ''
    return projects.some((p) => p.path === filters.project) ? filters.project : ''
  }, [filters.project, projects])

  // ── Filter pipeline ──

  const filtered = useMemo(() => {
    return rows.filter((r) => {
      // The open conversation always stays visible regardless of filters —
      // otherwise archiving or a narrowing window would strand it.
      if (r.uuid === currentSessionId) return true
      if (filters.status === 'active' && r.archived) return false
      if (filters.status === 'archived' && !r.archived) return false
      if (projectFilter && r.project !== projectFilter) return false
      if (filters.lastActivity !== 'all') {
        const ts = r.updated_at || r.created_at || ''
        const then = new Date(ts).getTime()
        if (Number.isNaN(then)) return false
        const span = filters.lastActivity === 'today' ? DAY : filters.lastActivity === 'week' ? 7 * DAY : 30 * DAY
        if (now - then > span) return false
      }
      return true
    })
  }, [rows, filters, projectFilter, currentSessionId, now])

  // ── Sort: running first, then pinned, then the chosen key ──

  const sorted = useMemo(() => {
    const arr = [...filtered]
    arr.sort((a, b) => {
      if (a.running !== b.running) return a.running ? -1 : 1
      if (a.pinned !== b.pinned) return a.pinned ? -1 : 1
      if (filters.sort === 'name') return rowTitle(a).localeCompare(rowTitle(b))
      if (filters.sort === 'created') return (b.created_at || '').localeCompare(a.created_at || '')
      return (b.updated_at || '').localeCompare(a.updated_at || '')
    })
    return arr
  }, [filtered, filters.sort])

  // ── Group by project or recency ──

  const groups = useMemo<SidebarGroup[]>(() => {
    if (filters.groupBy === 'project') {
      const map = new Map<string, SessionRow[]>()
      for (const r of sorted) {
        const key = r.project || activePath || ''
        const arr = map.get(key)
        if (arr) arr.push(r)
        else map.set(key, [r])
      }
      const paths = projectFilter ? new Set([projectFilter]) : new Set(map.keys())
      const narrowing = !!projectFilter || filters.status === 'archived' || filters.lastActivity !== 'all'
      if (activePath && (map.has(activePath) || !narrowing)) paths.add(activePath)
      const projectGroups = [...paths].map((path) => ({
        kind: 'project' as const,
        key: path || 'current',
        path,
        label: projectName(path),
        items: map.get(path) || [],
      }))
      return projectGroups.sort((a, b) => compareProjectGroups(a, b, activePath))
    }

    const map = new Map<string, SessionRow[]>()
    for (const r of sorted) {
      const key = bucketFor(r.updated_at || r.created_at || '', now)
      const arr = map.get(key)
      if (arr) arr.push(r)
      else map.set(key, [r])
    }
    return BUCKET_ORDER.filter((k) => map.has(k)).map((k) => ({
      kind: 'date' as const,
      key: k,
      label: t(`sidebar.dateBucket.${k}`),
      items: map.get(k)!,
    }))
  }, [sorted, filters.groupBy, filters.status, filters.lastActivity, projectFilter, activePath, now, t])

  const duplicateProjectNames = useMemo(() => {
    const counts = new Map<string, number>()
    for (const group of groups) {
      if (group.kind === 'project') counts.set(group.label, (counts.get(group.label) || 0) + 1)
    }
    return new Set([...counts].filter(([, count]) => count > 1).map(([name]) => name))
  }, [groups])

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
    filters.project !== DEFAULT_FILTERS.project ||
    filters.lastActivity !== DEFAULT_FILTERS.lastActivity ||
    filters.groupBy !== DEFAULT_FILTERS.groupBy ||
    filters.sort !== DEFAULT_FILTERS.sort

  const statusOptions = useMemo(() => [
    { value: 'all' as StatusFilter, label: t('sidebar.filter.statusAll') },
    { value: 'active' as StatusFilter, label: t('sidebar.filter.statusActive') },
    { value: 'archived' as StatusFilter, label: t('sidebar.filter.statusArchived') },
  ], [t])
  const projectOptions = useMemo(() => [
    { value: '', label: t('sidebar.filter.projectAll') },
    ...projects.map((p) => ({ value: p.path, label: p.name })),
  ], [projects, t])
  const lastActivityOptions = useMemo(() => [
    { value: 'all' as LastActivityFilter, label: t('sidebar.filter.activityAll') },
    { value: 'today' as LastActivityFilter, label: t('sidebar.filter.activityToday') },
    { value: 'week' as LastActivityFilter, label: t('sidebar.filter.activityWeek') },
    { value: 'month' as LastActivityFilter, label: t('sidebar.filter.activityMonth') },
  ], [t])
  const groupOptions = useMemo(() => [
    { value: 'project' as GroupByMode, label: t('sidebar.filter.groupProject') },
    { value: 'date' as GroupByMode, label: t('sidebar.filter.groupDate') },
  ], [t])
  const sortOptions = useMemo(() => [
    { value: 'recency' as SortFilter, label: t('sidebar.filter.sortRecency') },
    { value: 'name' as SortFilter, label: t('sidebar.filter.sortName') },
    { value: 'created' as SortFilter, label: t('sidebar.filter.sortCreated') },
  ], [t])

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
      dispatch(loadTasks())
    } catch {
      // surfaced via health/gate
    }
  }

  async function openItem(row: SessionRow) {
    dispatch(uiActions.setView('chat'))
    if (row.unread) await patchTask(row.uuid, { unread: false })
    if (row.project && activePath && row.project !== activePath) {
      if (isRemotePath(row.project)) {
        dispatch(chatActions.addMessage({
          role: 'system',
          content: t('sidebar.remoteReconnectRequired'),
          level: 'error',
        }))
        return
      }
      try {
        const resp = await api.switchProject(row.project)
        dispatch(sessionActions.setProjectPath(resp.pwd || row.project))
        await dispatch(loadWorkspaceState())
      } catch {
        dispatch(chatActions.addMessage({
          role: 'system',
          content: t('sidebar.switchProjectFailed'),
          level: 'error',
        }))
        return
      }
    }
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

  function toggleGroup(key: string) {
    setExpanded((prev) => {
      const next = new Set(prev)
      if (next.has(key)) next.delete(key)
      else next.add(key)
      return next
    })
  }

  async function newTaskInProject(path: string) {
    dispatch(uiActions.setView('chat'))
    if (isRemotePath(path)) {
      dispatch(chatActions.addMessage({
        role: 'system',
        content: t('sidebar.remoteReconnectRequired'),
        level: 'error',
      }))
      return
    }
    if (path && activePath && path !== activePath) {
      try {
        const resp = await api.switchProject(path)
        dispatch(sessionActions.setProjectPath(resp.pwd || path))
        await dispatch(loadWorkspaceState())
      } catch {
        dispatch(chatActions.addMessage({
          role: 'system',
          content: t('sidebar.switchProjectFailed'),
          level: 'error',
        }))
        return
      }
    }
    await newChat()
    setExpanded((prev) => new Set(prev).add(path))
  }

  const ctxRow = ctx?.row

  return (
    <aside className="sb-root flex w-[var(--sidebar-width,20rem)] shrink-0 flex-col border-r border-[var(--color-border)] bg-[var(--color-background)]">
      <div className="sb-header">
        <div className="sb-nav-list">
          <button
            type="button"
            onClick={newChat}
            className="sb-nav-row"
          >
            <PlusIcon className="sb-nav-ic" />
            <span className="sb-nav-name">{t('nav.newTask')}</span>
            <span className="sb-nav-kbd">⌘ N</span>
          </button>
          <button
            type="button"
            onClick={() => dispatch(uiActions.setView('automations'))}
            className={`sb-nav-row ${activeView === 'automations' ? 'active' : ''}`}
          >
            <SparklesIcon className="sb-nav-ic" />
            <span className="sb-nav-name">{t('nav.automations')}</span>
          </button>
          <button
            type="button"
            onClick={() => dispatch(uiActions.setView('channels'))}
            className={`sb-nav-row ${activeView === 'channels' ? 'active' : ''}`}
          >
            <SignalIcon className="sb-nav-ic" />
            <span className="sb-nav-name">{t('nav.channels')}</span>
          </button>
        </div>
      </div>

      <div className="sb-tree min-h-0 flex-1 overflow-y-auto">
        <div className="sb-tree-head">
          <span className="sb-tree-label">{t('nav.workspace')}</span>
          <div ref={filterRef} className="relative inline-flex">
            <button
              type="button"
              onClick={() => setFilterOpen((v) => !v)}
              title={t('sidebar.filter.title')}
              aria-label={t('sidebar.filter.title')}
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
                <FilterSection label={t('sidebar.filter.status')} options={statusOptions} value={filters.status} onSelect={(v) => setFilter('status', v)} />
                <div className="my-1 h-px bg-[var(--color-border)]" />
                <FilterSection label={t('sidebar.filter.project')} options={projectOptions} value={filters.project} onSelect={(v) => setFilter('project', v)} />
                <div className="my-1 h-px bg-[var(--color-border)]" />
                <FilterSection label={t('sidebar.filter.lastActivity')} options={lastActivityOptions} value={filters.lastActivity} onSelect={(v) => setFilter('lastActivity', v)} />
                <div className="my-1 h-px bg-[var(--color-border)]" />
                <FilterSection label={t('sidebar.filter.groupBy')} options={groupOptions} value={filters.groupBy} onSelect={(v) => setFilter('groupBy', v)} />
                <div className="my-1 h-px bg-[var(--color-border)]" />
                <FilterSection label={t('sidebar.filter.sort')} options={sortOptions} value={filters.sort} onSelect={(v) => setFilter('sort', v)} />
                {isDirty && (
                  <>
                    <div className="my-1 h-px bg-[var(--color-border)]" />
                    <button
                      type="button"
                      onClick={() => setFilters({ ...DEFAULT_FILTERS })}
                      className="flex w-full items-center gap-2 rounded-[var(--radius-md)] px-2 py-1.5 text-left text-[12.5px] text-[var(--color-muted-foreground)] transition-colors hover:bg-[var(--color-muted)]"
                    >
                      {t('common.reset')}
                    </button>
                  </>
                )}
              </div>
            )}
          </div>
        </div>

        {groups.length === 0 ? (
          <div className="px-3 py-6 text-center text-xs text-[var(--color-muted-foreground)]">{t('sidebar.noConversations')}</div>
        ) : (
          groups.map((g) => {
            const isProject = g.kind === 'project'
            const open = !isProject || expanded.has(g.key)
            const activeProject = isProject && g.path === activePath
            const ProjectIcon = g.path && isRemotePath(g.path) ? ServerIcon : activeProject ? FolderOpenIcon : FolderIcon
            return (
              <div key={g.key} className="sb-project-group">
                {isProject ? (
                  <div
                    role="button"
                    tabIndex={0}
                    title={g.path}
                    onClick={() => toggleGroup(g.key)}
                    onKeyDown={(e) => {
                      if (e.key === 'Enter' || e.key === ' ') {
                        e.preventDefault()
                        toggleGroup(g.key)
                      }
                    }}
                    className={`sb-project-row ${activeProject ? 'active' : ''}`}
                  >
                    <ChevronRightIcon className={`h-3.5 w-3.5 shrink-0 transition-transform ${open ? 'rotate-90' : ''}`} />
                    <ProjectIcon className="sb-project-icon h-3.5 w-3.5 shrink-0" />
                    <span className="sb-project-name">{g.label}</span>
                    {g.path && duplicateProjectNames.has(g.label) && !isRemotePath(g.path) && (
                      <span className="sb-project-hint">{projectParentHint(g.path)}</span>
                    )}
                    {!open && g.items.some((row) => row.running) && (
                      <span className="sb-ring sb-project-ring" title={t('sidebar.running')} aria-hidden="true" />
                    )}
                    {g.path && (
                      <button
                        type="button"
                        title={t('sidebar.newTaskHere', { defaultValue: t('nav.newTask') })}
                        aria-label={t('sidebar.newTaskHere', { defaultValue: t('nav.newTask') })}
                        onClick={(e) => {
                          e.stopPropagation()
                          void newTaskInProject(g.path!)
                        }}
                        className="sb-project-add"
                      >
                        <PlusIcon className="h-3.5 w-3.5" />
                      </button>
                    )}
                    {g.items.length > 0 && <span className="sb-project-count">{g.items.length}</span>}
                  </div>
                ) : (
                  <div className="sb-date-row">
                    <span className="sb-date-label">{g.label}</span>
                    <span className="sb-project-count">{g.items.length}</span>
                  </div>
                )}

                {open && (
                  <div className={`sb-task-list ${isProject ? '' : 'date-list'}`}>
                    {isProject && g.items.length === 0 && (
                      <div className="px-2 py-1.5 text-xs text-[var(--color-muted-foreground)]">{t('sidebar.noTasks')}</div>
                    )}
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
                          className={`sb-task-row group ${active ? 'active' : ''} ${row.archived ? 'archived' : ''} ${row.running ? 'running' : ''}`}
                        >
                          {row.running ? (
                            <span className="sb-ring h-[11px] w-[11px] shrink-0" aria-hidden="true" />
                          ) : (
                            <span
                              className={`h-[6px] w-[6px] shrink-0 rounded-full ${row.unread ? 'bg-[var(--color-accent-neutral)]' : 'bg-transparent'}`}
                              aria-hidden="true"
                            />
                          )}
                          {row.pinned && <BookmarkIcon className="h-2.5 w-2.5 shrink-0 text-[var(--color-accent-neutral)]" />}
                          <span className="min-w-0 flex-1 truncate">{rowTitle(row)}</span>
                          {!isProject && <span className="sb-task-project">{projectName(row.project)}</span>}
                          <span className={`shrink-0 font-mono text-[0.625rem] ${row.running ? 'text-[var(--color-accent)]' : 'text-[var(--color-muted-foreground)]'}`}>
                            {row.running ? t('sidebar.running') : relativeTime(row.updated_at || row.created_at, now, t)}
                          </span>
                          <button
                            type="button"
                            title={t('sidebar.actions.taskActions')}
                            aria-label={t('sidebar.actions.taskActions')}
                            onClick={(e) => openContextFromButton(e, row)}
                            className="grid h-5 w-5 shrink-0 place-items-center rounded-[var(--radius-sm)] text-[var(--color-muted-foreground)] opacity-0 transition-all hover:bg-[var(--color-secondary)] hover:text-[var(--color-foreground)] group-hover:opacity-100"
                          >
                            <EllipsisHorizontalIcon className="h-3.5 w-3.5" />
                          </button>
                        </div>
                      )
                    })}
                  </div>
                )}
              </div>
            )
          })
        )}
      </div>

      <div className="flex shrink-0 items-center justify-end gap-1 px-3 py-2">
        <ThemeToggle compact />
        <button
          type="button"
          onClick={() => dispatch(uiActions.setSettingsOpen(true))}
          className="rounded-[var(--radius-md)] p-1.5 text-[var(--color-muted-foreground)] transition-colors hover:bg-[var(--neutral-wash-soft)] hover:text-[var(--color-foreground)]"
          aria-label="Settings"
          title={t('nav.settingsWithShortcut')}
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
          <CtxItem icon={BookmarkIcon} label={ctxRow.pinned ? t('sidebar.actions.unpin') : t('sidebar.actions.pin')} onClick={() => { void patchTask(ctxRow.uuid, { pinned: !ctxRow.pinned }); setCtx(null) }} />
          <CtxItem
            icon={ctxRow.archived ? ArchiveBoxArrowDownIcon : ArchiveBoxIcon}
            label={ctxRow.archived ? t('sidebar.actions.unarchive') : t('sidebar.actions.archive')}
            onClick={() => { void patchTask(ctxRow.uuid, { archived: !ctxRow.archived }); setCtx(null) }}
          />
          <CtxItem icon={EnvelopeOpenIcon} label={ctxRow.unread ? t('sidebar.actions.markRead') : t('sidebar.actions.markUnread')} onClick={() => { void patchTask(ctxRow.uuid, { unread: !ctxRow.unread }); setCtx(null) }} />
          <div className="my-1 h-px bg-[var(--color-border)]" />
          <CtxItem icon={TrashIcon} label={t('sidebar.actions.delete')} danger onClick={() => { void deleteItem(ctxRow); setCtx(null) }} />
        </div>
      )}

      {/* Animations: running ring breathe + pop-in for menus. Scoped via sb-* names. */}
      <style>{`
        @keyframes sb-ring-breathe { 0%,100% { opacity:0.35; transform:scale(0.78); } 50% { opacity:1; transform:scale(1); } }
        @keyframes sb-pop-in { from { opacity:0; transform:translateY(-4px); } to { opacity:1; transform:none; } }
        .sb-header { padding: 48px 12px 6px; }
        html.is-tauri-macos .sb-header { padding-top: 20px; }
        .sb-nav-list { display:flex; flex-direction:column; gap:6px; }
        .sb-nav-row {
          display:flex; align-items:center; gap:12px; width:100%; padding:9px 12px;
          border:1px solid transparent; border-radius:var(--radius-lg); background:transparent;
          color:var(--color-foreground); text-align:left; cursor:pointer;
          transition:background 0.15s, color 0.15s, border-color 0.15s;
        }
        .sb-nav-row:hover, .sb-nav-row.active { background:var(--color-muted); }
        .sb-nav-row:hover .sb-nav-ic, .sb-nav-row.active .sb-nav-ic { color:var(--color-foreground); }
        .sb-nav-row.active .sb-nav-name { font-weight:600; }
        .sb-nav-ic { width:18px; height:18px; flex-shrink:0; color:var(--color-muted-foreground); transition:color 0.15s; }
        .sb-nav-name { flex:1; min-width:0; overflow:hidden; text-overflow:ellipsis; white-space:nowrap; font-size:13.5px; font-weight:500; }
        .sb-nav-kbd { flex-shrink:0; color:var(--color-foreground); font-family:var(--font-sans); font-size:11.5px; line-height:1; white-space:nowrap; }
        .sb-tree { padding:4px 8px 8px; }
        .sb-tree-head { display:flex; align-items:center; justify-content:space-between; padding:6px 6px 4px; }
        .sb-tree-label, .sb-date-label {
          color:var(--color-muted-foreground); font-size:10px; font-weight:600;
          letter-spacing:0.06em; text-transform:uppercase;
        }
        .sb-project-group { margin-bottom:2px; }
        .sb-project-row {
          display:flex; align-items:center; gap:6px; width:100%; padding:8px 6px;
          border-radius:var(--radius-md); color:var(--color-muted-foreground); cursor:pointer;
          transition:background 0.15s;
        }
        .sb-project-row:hover { background:var(--color-muted); }
        .sb-project-row.active .sb-project-icon { color:var(--color-accent-neutral); }
        .sb-project-name {
          flex:1; min-width:0; overflow:hidden; text-overflow:ellipsis; white-space:nowrap;
          color:var(--color-foreground); font-size:13px; font-weight:500;
        }
        .sb-project-hint {
          max-width:80px; flex-shrink:0; overflow:hidden; text-overflow:ellipsis; white-space:nowrap;
          color:var(--color-muted-foreground); font-size:10px; opacity:0.7;
        }
        .sb-project-count {
          flex-shrink:0; color:var(--color-muted-foreground); font-family:var(--font-mono); font-size:10px;
        }
        .sb-project-add {
          display:grid; place-items:center; width:20px; height:20px; flex-shrink:0;
          border:none; border-radius:var(--radius-sm); background:transparent;
          color:var(--color-muted-foreground); opacity:0; cursor:pointer;
          transition:opacity 0.15s, background 0.15s, color 0.15s;
        }
        .sb-project-row:hover .sb-project-add, .sb-project-row:focus-within .sb-project-add { opacity:1; }
        .sb-project-add:hover { background:var(--color-secondary); color:var(--color-foreground); }
        .sb-project-ring { width:9px; height:9px; margin-left:auto; }
        .sb-task-list { padding-left:14px; }
        .sb-task-list.date-list { padding-left:0; }
        .sb-date-row { display:flex; align-items:center; gap:6px; padding:10px 6px 4px; }
        .sb-date-label { flex:1; }
        .sb-task-row {
          display:flex; align-items:center; gap:6px; margin-left:6px; padding:8px 8px;
          border-left:1px solid var(--color-border); color:var(--color-foreground);
          cursor:pointer; position:relative; transition:background 0.15s, border-color 0.15s;
        }
        .sb-task-row:hover { background:var(--color-muted); }
        .sb-task-row.active { background:var(--neutral-wash-soft); border-left-color:var(--color-accent-neutral); }
        .sb-task-row.archived { opacity:0.55; }
        .sb-task-row.running { border-left-color:color-mix(in srgb, var(--color-accent) 50%, var(--color-border)); }
        .date-list .sb-task-row { margin-left:0; border-left:none; padding-left:6px; }
        .sb-task-project {
          max-width:90px; flex-shrink:0; overflow:hidden; text-overflow:ellipsis; white-space:nowrap;
          color:var(--color-muted-foreground); font-size:10px; opacity:0.75;
        }
        .sb-ring {
          border:1.6px solid var(--color-accent);
          border-radius:var(--radius-pill);
          animation: sb-ring-breathe 1.6s ease-in-out infinite;
        }
        .sb-pop { animation: sb-pop-in 0.12s ease; }
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

// ─── Helpers ───

function rowTitle(r: SessionRow): string {
  return r.title || r.uuid.slice(0, 8) + '…'
}

function taskToRow(t: TaskItem): SessionRow {
  return {
    uuid: t.uuid,
    project: t.project,
    title: t.title || '',
    created_at: t.created_at || '',
    updated_at: t.updated_at || t.created_at || '',
    pinned: t.pinned,
    archived: t.archived,
    unread: t.unread,
    running: !!t.running,
  }
}

const BUCKET_ORDER = ['today', 'yesterday', 'week', 'month', 'older'] as const
function bucketFor(ts: string, now: number): (typeof BUCKET_ORDER)[number] {
  const then = new Date(ts).getTime()
  if (Number.isNaN(then)) return 'older'
  const today = new Date(now)
  const startToday = new Date(today.getFullYear(), today.getMonth(), today.getDate()).getTime()
  if (then >= startToday) return 'today'
  if (then >= startToday - DAY) return 'yesterday'
  if (then >= startToday - 7 * DAY) return 'week'
  if (then >= startToday - 30 * DAY) return 'month'
  return 'older'
}

function relativeTime(ts: string, now: number, t: (key: string, values?: Record<string, number>) => string): string {
  if (!ts) return ''
  const then = new Date(ts).getTime()
  if (Number.isNaN(then)) return ''
  const mins = Math.floor((now - then) / 60000)
  if (mins < 1) return t('sidebar.relativeTime.now')
  if (mins < 60) return t('sidebar.relativeTime.minutes', { n: mins })
  const hrs = Math.floor(mins / 60)
  if (hrs < 24) return t('sidebar.relativeTime.hours', { n: hrs })
  const days = Math.floor(hrs / 24)
  if (days < 30) return t('sidebar.relativeTime.days', { n: days })
  return new Date(ts).toLocaleDateString([], { month: 'short', day: 'numeric' })
}

function loadFilters(): FilterState {
  try {
    const raw = localStorage.getItem(FILTERS_KEY)
    if (!raw) return { ...DEFAULT_FILTERS }
    const parsed = JSON.parse(raw) as Partial<FilterState> & { time?: LastActivityFilter; sortBy?: SortFilter }
    return {
      ...DEFAULT_FILTERS,
      ...parsed,
      lastActivity: parsed.lastActivity || parsed.time || DEFAULT_FILTERS.lastActivity,
      sort: parsed.sort || parsed.sortBy || DEFAULT_FILTERS.sort,
    }
  } catch {
    return { ...DEFAULT_FILTERS }
  }
}

function isRemotePath(path: string): boolean {
  return path.startsWith('ssh://') || path.startsWith('docker://')
}

function projectName(path: string): string {
  if (!path) return ''
  if (path.startsWith('docker://')) {
    const rest = path.slice('docker://'.length)
    return rest.split('/')[0] || path
  }
  if (path.startsWith('ssh://')) {
    const rest = path.slice('ssh://'.length)
    const slash = rest.indexOf('/')
    const host = slash >= 0 ? rest.slice(0, slash) : rest
    const tail = slash >= 0 ? rest.slice(slash).split('/').filter(Boolean).at(-1) : ''
    return tail ? `${tail} (${host})` : host
  }
  const parts = path.split('/').filter(Boolean)
  return parts.at(-1) || path
}

function projectParentHint(path: string): string {
  const parts = path.split('/').filter(Boolean)
  if (parts.length < 2) return ''
  return parts[parts.length - 2] || ''
}

function compareProjectGroups(a: SidebarGroup, b: SidebarGroup, activePath: string): number {
  if (a.path === activePath) return -1
  if (b.path === activePath) return 1
  const A = aggregate(a.items)
  const B = aggregate(b.items)
  if (A.running !== B.running) return A.running ? -1 : 1
  if (A.unread !== B.unread) return A.unread ? -1 : 1
  if (A.lastTs !== B.lastTs) return B.lastTs.localeCompare(A.lastTs)
  return a.label.localeCompare(b.label)
}

function aggregate(items: SessionRow[]) {
  let running = false
  let unread = false
  let lastTs = ''
  for (const item of items) {
    if (item.running) running = true
    if (item.unread) unread = true
    const ts = item.updated_at || item.created_at || ''
    if (ts > lastTs) lastTs = ts
  }
  return { running, unread, lastTs }
}
