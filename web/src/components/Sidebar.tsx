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
import { createPortal } from 'react-dom'
import {
  PlusIcon,
  ChevronRightIcon,
  FolderIcon,
  FolderOpenIcon,
  ServerIcon,
  SparklesIcon,
  SignalIcon,
  Cog6ToothIcon,
  BookmarkIcon,
  ArchiveBoxIcon,
  ArchiveBoxArrowDownIcon,
  EnvelopeOpenIcon,
  TrashIcon,
  EllipsisHorizontalIcon,
} from '@heroicons/react/24/outline'
import { useTranslation } from 'react-i18next'
import { useAppDispatch, useAppSelector } from '../app/hooks'
import { uiActions, sessionActions, chatActions, loadSession, loadWorkspaceState, startNewChat } from '../app/store'
import { api } from '../lib/api'
import type { TaskItem } from '../lib/types'
import { ThemeToggle } from './ThemeToggle'
import {
  DEFAULT_FILTERS,
  SidebarFilterMenu,
  type FilterState,
  type LastActivityFilter,
  type SortFilter,
} from './SidebarFilterMenu'
import { isRemotePath, openRemoteConnect, parseRemoteLabel } from '../lib/remote'

const FILTERS_KEY = 'jcode_sidebar_filters'

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

/** Matches Vue .task-menu-items min-width. */
const CTX_MENU_WIDTH = 160
const CTX_MENU_EST_H = 180

/**
 * Context menu — left-click ⋯ opens to the RIGHT of the button
 * (into the main column), not left over the row title.
 * 靠上 → 右下 (below + right of ⋯); 靠下 → 右上 (above + right of ⋯).
 */
type CtxMenu = {
  row: SessionRow
  left: number
  top: number
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
  const projectTimes = useAppSelector((s) => s.session.projectTimes)
  const currentSessionId = useAppSelector((s) => s.session.currentSessionId)
  // Latest currentSessionId readable from async handlers (deleteItem) after an
  // await, when the render-scope value is stale: if the user navigated away
  // while the DELETE was in flight, the delete must not yank the foreground.
  const currentSessionRef = useRef(currentSessionId)
  currentSessionRef.current = currentSessionId
  const activePath = useAppSelector((s) => s.session.projectPath)
  const activeView = useAppSelector((s) => s.ui.activeView)

  const [filters, setFilters] = useState<FilterState>(() => loadFilters())
  const [expanded, setExpanded] = useState<Set<string>>(() => new Set(activePath ? [activePath] : []))

  // ⋯ left-click / right-click menu — fixed, portaled to body.
  // ⋯: menu's LEFT edge starts just to the RIGHT of the button.
  const [ctx, setCtx] = useState<CtxMenu | null>(null)
  const ctxRef = useRef<HTMLDivElement | null>(null)

  // Track rows that just appeared so we can play a one-shot enter animation
  // (avoids a hard pop-in when a new session is revealed optimistically).
  const knownUuids = useRef<Set<string>>(new Set())
  const [entering, setEntering] = useState<Set<string>>(() => new Set())
  const seededList = useRef(false)

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

  // After the first non-empty paint, animate only newly-added rows (not the
  // initial hydrate of the whole list).
  useEffect(() => {
    const ids = rows.map((r) => r.uuid)
    if (!seededList.current) {
      if (ids.length === 0) return
      knownUuids.current = new Set(ids)
      seededList.current = true
      return
    }
    const added: string[] = []
    for (const id of ids) {
      if (!knownUuids.current.has(id)) added.push(id)
    }
    knownUuids.current = new Set(ids)
    if (added.length === 0) return
    setEntering((prev) => {
      const next = new Set(prev)
      for (const id of added) next.add(id)
      return next
    })
    const t = window.setTimeout(() => {
      setEntering((prev) => {
        const next = new Set(prev)
        for (const id of added) next.delete(id)
        return next
      })
    }, 220)
    return () => window.clearTimeout(t)
  }, [rows])

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
      // Untitled rows are empty sessions that never recorded a message (or
      // optimistic placeholders). Keep them out of the list — welcome stays
      // clean until the first user turn materializes a real title.
      if (!r.title?.trim() && !r.running) return false
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

  const untitledLabel = t('sidebar.untitled')

  const sorted = useMemo(() => {
    const arr = [...filtered]
    arr.sort((a, b) => {
      if (a.running !== b.running) return a.running ? -1 : 1
      if (a.pinned !== b.pinned) return a.pinned ? -1 : 1
      let cmp = 0
      if (filters.sort === 'name') cmp = rowTitle(a, untitledLabel).localeCompare(rowTitle(b, untitledLabel))
      else if (filters.sort === 'created') cmp = (b.created_at || '').localeCompare(a.created_at || '')
      else cmp = (b.updated_at || '').localeCompare(a.updated_at || '')
      // Deterministic final tiebreaker. /api/tasks returns rows in Go map order
      // (randomized per request), so without a stable key, any re-fetch would
      // reshuffle rows whose sort key ties — making the list "jump" on actions
      // that trigger a refresh (e.g. opening a task in another project). uuid is
      // unique and stable, so equal-key rows keep a fixed relative order.
      return cmp !== 0 ? cmp : a.uuid.localeCompare(b.uuid)
    })
    return arr
  }, [filtered, filters.sort, untitledLabel])

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
      return projectGroups.sort((a, b) => compareProjectGroups(a, b, activePath, projectTimes))
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
  }, [sorted, filters.groupBy, filters.status, filters.lastActivity, projectFilter, activePath, projectTimes, now, t])

  const duplicateProjectNames = useMemo(() => {
    const counts = new Map<string, number>()
    for (const group of groups) {
      if (group.kind === 'project') counts.set(group.label, (counts.get(group.label) || 0) + 1)
    }
    return new Set([...counts].filter(([, count]) => count > 1).map(([name]) => name))
  }, [groups])

  // ── Outside-click / Esc handling for the context menu ──
  // (Filter menu owns its own outside-click / Esc handling.)

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

  // ── Actions ──

  // Shared with the ⌘N / ⇧⌘O shortcuts in App.tsx (see startNewChat in the store).
  async function newChat() {
    await dispatch(startNewChat())
  }

  async function openItem(row: SessionRow) {
    dispatch(uiActions.setView('chat'))
    if (row.unread) await patchTask(row.uuid, { unread: false })
    if (row.project && activePath && row.project !== activePath) {
      // Remote workspaces need the wizard (prefill + optional load task).
      if (isRemotePath(row.project)) {
        const meta = parseRemoteLabel(row.project)
        openRemoteConnect(meta ? { ...meta, loadTaskUuid: row.uuid } : undefined)
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

  // Apply a task metadata patch via the API and reflect it in the store.
  //
  // Merge ONLY the fields we changed — never the timestamps. A metadata edit
  // (pin / archive / mark-read / rename) must not touch created_at/updated_at, so
  // opening a session (which marks it read) can't move it in the recency sort.
  // We intentionally ignore the endpoint's echoed row so the sidebar's order is
  // fully decoupled from this round-trip: the list only ever reorders when a new
  // prompt is sent, never on selection.
  async function patchTask(uuid: string, patch: Parameters<typeof api.updateTask>[1]) {
    try {
      await api.updateTask(uuid, patch)
      dispatch(sessionActions.patchTask({ uuid, ...patch }))
    } catch {
      // ignore — the next tasks refresh will reconcile
    }
  }

  async function deleteItem(row: SessionRow) {
    // Running tasks must not be deleted — backend also returns 409. Stop first.
    if (row.running) return
    const wasActive = row.uuid === currentSessionId
    if (wasActive) {
      // Drop queued type-ahead for this session BEFORE the delete round-trip:
      // a late agent_done (from a prior cancel/stop) can arrive before the
      // DELETE response and would otherwise drain the queue back into the
      // deleted session — resurrecting its file + index entry on disk.
      dispatch(chatActions.dropSessionQueue(row.uuid))
    }
    try {
      await api.deleteSession(row.uuid)
    } catch {
      return
    }
    // Filter inside the reducer: the render-scope sessions/tasks copies are
    // stale after the await (a WS update or refresh may have landed since),
    // and a whole-list setTasks would clobber it.
    dispatch(sessionActions.removeSession(row.uuid))
    if (!wasActive) {
      dispatch(chatActions.dropSessionQueue(row.uuid))
      return
    }
    // The user may have navigated to another session while the DELETE was in
    // flight (their click's loadSession wins) — only take over the foreground
    // if we're still on the deleted session.
    if (currentSessionRef.current !== row.uuid) return
    // Land on the welcome page with a FRESH session. Merely clearing the UI
    // would strand the backend on the deleted task's engine: the next
    // message would run on that stale engine, whose events are stamped with
    // the deleted task id and dropped by the WS filter (a conversation that
    // appears dead). startNewChat provisions a consistent new engine/task —
    // and reclaims the stale one (its recorder was reset on delete).
    await dispatch(startNewChat())
  }

  function openContext(e: React.MouseEvent, row: SessionRow) {
    e.preventDefault()
    e.stopPropagation()
    setCtx(placeMenu(e.clientX, e.clientY, row))
  }

  function openContextFromButton(e: React.MouseEvent, row: SessionRow) {
    e.preventDefault()
    e.stopPropagation()
    // Toggle closed if already open for this row.
    if (ctx && ctx.row.uuid === row.uuid) {
      setCtx(null)
      return
    }
    const rect = (e.currentTarget as HTMLElement).getBoundingClientRect()
    // Open to the RIGHT of the ⋯ button (not left over the title).
    const gap = 4
    const flipUp = rect.bottom + 200 > window.innerHeight - 12
    const left = rect.right + gap
    const top = flipUp
      ? Math.max(8, rect.top - CTX_MENU_EST_H - gap)
      : rect.bottom + gap
    setCtx(placeMenu(left, top, row))
  }

  /** Clamp fixed menu coords so the panel stays on-screen. */
  function placeMenu(left: number, top: number, row: SessionRow): CtxMenu {
    const pad = 8
    const vw = window.innerWidth
    const vh = window.innerHeight
    let x = left
    let y = top
    if (x + CTX_MENU_WIDTH > vw - pad) x = Math.max(pad, vw - CTX_MENU_WIDTH - pad)
    if (y + CTX_MENU_EST_H > vh - pad) y = Math.max(pad, vh - CTX_MENU_EST_H - pad)
    if (x < pad) x = pad
    if (y < pad) y = pad
    return { row, left: x, top: y }
  }

  function closeCtx() {
    setCtx(null)
  }

  function renderCtxItems(row: SessionRow) {
    return (
      <>
        <CtxItem
          icon={BookmarkIcon}
          label={row.pinned ? t('sidebar.actions.unpin') : t('sidebar.actions.pin')}
          onClick={() => { void patchTask(row.uuid, { pinned: !row.pinned }); closeCtx() }}
        />
        <CtxItem
          icon={row.archived ? ArchiveBoxArrowDownIcon : ArchiveBoxIcon}
          label={row.archived ? t('sidebar.actions.unarchive') : t('sidebar.actions.archive')}
          onClick={() => { void patchTask(row.uuid, { archived: !row.archived }); closeCtx() }}
        />
        <CtxItem
          icon={EnvelopeOpenIcon}
          label={row.unread ? t('sidebar.actions.markRead') : t('sidebar.actions.markUnread')}
          onClick={() => { void patchTask(row.uuid, { unread: !row.unread }); closeCtx() }}
        />
        <div className="my-1 h-px bg-[var(--color-border)]" />
        <CtxItem
          icon={TrashIcon}
          label={t('sidebar.actions.delete')}
          danger
          disabled={row.running}
          title={row.running ? t('sidebar.actions.deleteWhileRunning') : undefined}
          onClick={() => { void deleteItem(row); closeCtx() }}
        />
      </>
    )
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
      const meta = parseRemoteLabel(path)
      openRemoteConnect(meta ?? undefined)
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
    <aside className="sb-root flex w-[var(--sidebar-width,20rem)] shrink-0 flex-col bg-[var(--color-background)]">
      <div className="sb-header">
        <div className="sb-nav-list">
          <button
            type="button"
            onClick={newChat}
            className="sb-nav-row"
          >
            <PlusIcon className="sb-nav-ic" />
            <span className="sb-nav-name">{t('nav.newTask')}</span>
            <span className="sb-nav-kbd">⇧⌘ O</span>
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

      {/* Pinned workspace + filter header — stays put while the list scrolls. */}
      <div className="sb-tree-head-fixed shrink-0">
        <div className="sb-tree-head">
          <span className="sb-tree-label">{t('nav.workspace')}</span>
          <SidebarFilterMenu filters={filters} projects={projects} onChange={setFilters} />
        </div>
      </div>

      <div className="sb-tree sb-tree-feather min-h-0 flex-1 overflow-y-auto">
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
                    <span className="sb-project-time">
                      {relativeTime((g.path && projectTimes[g.path]) || aggregate(g.items).lastTs, now, t)}
                    </span>
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
                      <div className="sb-task-empty">{t('sidebar.noTasks')}</div>
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
                          className={`sb-task-row group ${active ? 'active' : ''} ${row.archived ? 'archived' : ''} ${row.running ? 'running' : ''} ${entering.has(row.uuid) ? 'sb-task-enter' : ''}`}
                        >
                          {row.running ? (
                            <span className="sb-ring h-[11px] w-[11px] shrink-0" aria-hidden="true" />
                          ) : (
                            <span
                              className={`h-[6px] w-[6px] shrink-0 rounded-full ${row.unread ? 'bg-[var(--color-accent-neutral)]' : 'bg-transparent'}`}
                              aria-hidden="true"
                            />
                          )}
                          {row.pinned && <BookmarkIcon className="sb-task-pin h-2.5 w-2.5 shrink-0" />}
                          <span className="sb-task-title">{rowTitle(row, untitledLabel)}</span>
                          {!isProject && <span className="sb-task-project">{projectName(row.project)}</span>}
                          <span className={`sb-task-time ${row.running ? 'running' : ''}`}>
                            {row.running ? t('sidebar.running') : relativeTime(row.updated_at || row.created_at, now, t)}
                          </span>
                          <button
                            type="button"
                            title={t('sidebar.actions.taskActions')}
                            aria-label={t('sidebar.actions.taskActions')}
                            onClick={(e) => openContextFromButton(e, row)}
                            className="sb-task-menu-btn"
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

      <div className="sb-footer relative flex shrink-0 items-center justify-end gap-1.5 px-3">
        <ThemeToggle compact />
        <button
          type="button"
          onClick={() => dispatch(uiActions.setSettingsOpen(true))}
          className="sb-footer-btn flex h-9 w-9 items-center justify-center rounded-[var(--radius-md)] text-[var(--color-muted-foreground)] transition-colors hover:bg-[var(--color-muted)] hover:text-[var(--color-foreground)]"
          aria-label="Settings"
          title={t('nav.settingsWithShortcut')}
        >
          <Cog6ToothIcon className="h-[18px] w-[18px]" />
        </button>
      </div>

      {/* ⋯ / right-click menu — fixed to the RIGHT of ⋯ (into the main column). */}
      {ctx && ctxRow && typeof document !== 'undefined'
        ? createPortal(
            <div
              ref={ctxRef}
              role="menu"
              className="sb-task-menu-items sb-pop"
              style={{
                position: 'fixed',
                top: ctx.top,
                left: ctx.left,
                right: 'auto',
                bottom: 'auto',
                zIndex: 1000,
              }}
            >
              {renderCtxItems(ctxRow)}
            </div>,
            document.body,
          )
        : null}

      {/* Animations: running ring breathe + pop-in for menus / new rows. Scoped via sb-* names. */}
      <style>{`
        @keyframes sb-ring-breathe { 0%,100% { opacity:0.35; transform:scale(0.78); } 50% { opacity:1; transform:scale(1); } }
        @keyframes sb-pop-in { from { opacity:0; transform:translateY(-4px); } to { opacity:1; transform:none; } }
        @keyframes sb-task-enter { from { opacity:0; transform:translateY(-6px); } to { opacity:1; transform:none; } }
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
        .sb-tree { padding:10px 8px 20px; }
        /* Pinned Workspace + filter header (moved out of the scroll area). Left
           padding (8px) + the head's own 6px keeps the label aligned with the
           list rows as before. */
        .sb-tree-head-fixed { padding:4px 8px 2px; }
        /* Feather both edges of the scrolling session list: a soft top edge so
           rows dissolve under the pinned header (the 10px fade sits over the
           list's 10px top padding, so the first row is crisp at rest and only
           softens once it scrolls up), plus the existing bottom fade into the
           footer (same idea as messages-feather above the composer). */
        .sb-tree-feather {
          -webkit-mask-image: linear-gradient(to bottom, transparent 0, #000 10px, #000 calc(100% - 28px), transparent 100%);
          mask-image: linear-gradient(to bottom, transparent 0, #000 10px, #000 calc(100% - 28px), transparent 100%);
        }
        .sb-footer {
          padding-top: 14px;
          padding-bottom: 14px;
        }
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
          color:var(--color-foreground); font-size:13px; font-weight:500; line-height:1.25;
        }
        .sb-project-hint {
          max-width:80px; flex-shrink:0; overflow:hidden; text-overflow:ellipsis; white-space:nowrap;
          color:var(--color-muted-foreground); font-size:10px; opacity:0.7;
        }
        .sb-project-count {
          flex-shrink:0; color:var(--color-muted-foreground); font-family:var(--font-mono); font-size:10px;
        }
        .sb-project-time {
          flex-shrink:0; color:var(--color-muted-foreground); font-family:var(--font-mono); font-size:10px;
          line-height:1; opacity:0.75;
        }
        .sb-project-time:empty { display:none; }
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
        .sb-task-empty {
          font-size:11px; color:var(--color-muted-foreground); padding:5px 8px;
        }
        .sb-date-row { display:flex; align-items:center; gap:6px; padding:10px 6px 4px; }
        .sb-date-label { flex:1; }
        .sb-task-row {
          display:flex; align-items:center; gap:6px; margin-left:6px; padding:8px 8px;
          border-left:1px solid var(--color-border); color:var(--color-foreground);
          cursor:pointer; position:relative; transition:background 0.15s, border-color 0.15s;
          /* Match Vue: no custom line-height — inherit body (tight, single-line rows). */
          line-height: normal;
        }
        .sb-task-row.sb-task-enter { animation: sb-task-enter 0.2s ease; }
        .sb-task-row:hover { background:var(--color-muted); }
        .sb-task-row.active { background:var(--neutral-wash-soft); border-left-color:var(--color-accent-neutral); }
        .sb-task-row.archived { opacity:0.55; }
        .sb-task-row.running { border-left-color:color-mix(in srgb, var(--color-accent) 50%, var(--color-border)); }
        .date-list .sb-task-row { margin-left:0; border-left:none; padding-left:6px; }
        /* Conversation title — Vue .task-title */
        .sb-task-title {
          flex:1; min-width:0; overflow:hidden; text-overflow:ellipsis; white-space:nowrap;
          font-size:13px; line-height:1.25; color:var(--color-foreground);
        }
        .sb-task-pin { color:var(--color-accent-neutral); flex-shrink:0; }
        /* Relative time — Vue .task-time (10px mono) */
        .sb-task-time {
          flex-shrink:0; font-size:10px; line-height:1; font-family:var(--font-mono);
          color:var(--color-muted-foreground);
        }
        .sb-task-time.running { color:var(--color-accent); }
        .sb-task-project {
          max-width:90px; flex-shrink:0; overflow:hidden; text-overflow:ellipsis; white-space:nowrap;
          color:var(--color-muted-foreground); font-size:10px; line-height:1; opacity:0.75;
        }
        /* ⋯ button (Vue .task-menu-btn) */
        .sb-task-menu-btn {
          display: grid;
          place-items: center;
          width: 20px;
          height: 20px;
          flex-shrink: 0;
          border: none;
          background: transparent;
          border-radius: var(--radius-sm);
          color: var(--color-muted-foreground);
          cursor: pointer;
          opacity: 0;
          transition: opacity 0.15s, background 0.15s, color 0.15s;
        }
        .sb-task-row:hover .sb-task-menu-btn,
        .sb-task-row:focus-within .sb-task-menu-btn { opacity: 1; }
        .sb-task-menu-btn:hover {
          background: var(--color-secondary);
          color: var(--color-foreground);
        }
        .sb-task-menu-items {
          min-width: 160px;
          width: max-content;
          padding: 4px;
          background: var(--color-surface);
          border: 1px solid var(--color-border);
          border-radius: var(--radius-lg);
          box-shadow: var(--shadow-md);
          outline: none;
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
          .sb-task-row.sb-task-enter { animation: none; }
        }
      `}</style>
    </aside>
  )
}

// ─── Sub-components ───

function CtxItem({
  icon: Icon,
  label,
  onClick,
  danger,
  disabled,
  title,
}: {
  icon: React.ComponentType<{ className?: string }>
  label: string
  onClick: () => void
  danger?: boolean
  disabled?: boolean
  title?: string
}) {
  return (
    <button
      type="button"
      role="menuitem"
      disabled={disabled}
      title={title}
      aria-disabled={disabled || undefined}
      onClick={disabled ? undefined : onClick}
      className={`flex w-full items-center gap-2 rounded-[var(--radius-md)] px-2 py-1.5 text-left text-[12.5px] transition-colors ${
        disabled
          ? 'cursor-not-allowed opacity-45'
          : 'hover:bg-[var(--color-muted)]'
      } ${
        danger && !disabled
          ? 'text-[var(--color-destructive)]'
          : disabled
            ? 'text-[var(--color-muted-foreground)]'
            : 'text-[var(--color-foreground)]'
      }`}
    >
      <Icon className="h-3.5 w-3.5" />
      {label}
    </button>
  )
}

// ─── Helpers ───

function rowTitle(r: SessionRow, untitled: string): string {
  // Never fall back to a raw UUID fragment — it reads as garbled noise.
  return r.title?.trim() || untitled
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

function compareProjectGroups(a: SidebarGroup, b: SidebarGroup, activePath: string, projectTimes: Record<string, string>): number {
  // Fallback ONLY for an empty active project: a freshly-opened project with no
  // sessions has no activity timestamp, so pure lastTs ordering would sink it to
  // the bottom. Float just that case to the top so the project you're in stays in
  // view. A non-empty active project is ordered by its activity like any other —
  // selecting a project must not yank it around.
  const aEmptyActive = a.path === activePath && a.items.length === 0
  const bEmptyActive = b.path === activePath && b.items.length === 0
  if (aEmptyActive !== bEmptyActive) return aEmptyActive ? -1 : 1

  const A = aggregate(a.items)
  const B = aggregate(b.items)
  // Otherwise order projects purely by last activity — most recent first. A live
  // run floats its project up (and a run only starts from a sent prompt).
  // Prefer the persisted project-level timestamp: it survives conversation
  // deletions (the server never rolls it back on delete), so removing a session
  // can't reorder the list. Legacy indexes without project metadata fall back
  // to deriving recency from the surviving child sessions.
  if (A.running !== B.running) return A.running ? -1 : 1
  const aTs = (a.path && projectTimes[a.path]) || A.lastTs
  const bTs = (b.path && projectTimes[b.path]) || B.lastTs
  // Compare parsed instants, not strings: RFC3339 string order breaks across
  // UTC offsets (the index mixes server-local "+08:00" writes with UTC "Z").
  const byActivity = tsCmp(aTs, bTs)
  if (byActivity !== 0) return byActivity > 0 ? -1 : 1
  const byLabel = a.label.localeCompare(b.label)
  // Stable final tiebreaker (path) so equal-label groups don't reshuffle when
  // /api/tasks is re-fetched in a non-deterministic order.
  return byLabel !== 0 ? byLabel : (a.path || '').localeCompare(b.path || '')
}

/** Chronological compare for RFC3339 strings: negative if a is older, positive
 *  if newer. Valid instants beat unparseable/empty ones (treated as oldest);
 *  two invalid values compare equal so the label/path tiebreakers decide. */
function tsCmp(a: string, b: string): number {
  const at = a ? Date.parse(a) : Number.NaN
  const bt = b ? Date.parse(b) : Number.NaN
  const aOk = !Number.isNaN(at)
  const bOk = !Number.isNaN(bt)
  if (!aOk && !bOk) return 0
  if (!aOk) return -1
  if (!bOk) return 1
  return at - bt
}

function aggregate(items: SessionRow[]) {
  let running = false
  let lastTs = ''
  for (const item of items) {
    if (item.running) running = true
    const ts = item.updated_at || item.created_at || ''
    if (ts > lastTs) lastTs = ts
  }
  return { running, lastTs }
}
