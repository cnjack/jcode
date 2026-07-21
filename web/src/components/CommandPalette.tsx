import { useEffect, useMemo, useRef, useState } from 'react'
import {
  ChatBubbleLeftIcon,
  Cog6ToothIcon,
  FolderOpenIcon,
  MagnifyingGlassIcon,
  PlusIcon,
  ServerIcon,
} from '@heroicons/react/24/outline'
import { useTranslation } from 'react-i18next'
import { useAppDispatch, useAppSelector } from '../app/hooks'
import { chatActions, loadSession, loadTasks, loadWorkspaceState, sessionActions, uiActions } from '../app/store'
import { api } from '../lib/api'
import type { TaskItem } from '../lib/types'
import { isRemotePath, openRemoteConnect, parseRemoteLabel } from '../lib/remote'

interface PaletteItem {
  id: string
  group: string
  label: string
  hint?: string
  Icon: typeof ChatBubbleLeftIcon
  run: () => void | Promise<void>
}

export function CommandPalette() {
  const { t } = useTranslation()
  const dispatch = useAppDispatch()
  const tasks = useAppSelector((s) => s.session.tasks)
  const activePath = useAppSelector((s) => s.session.projectPath)
  const [query, setQuery] = useState('')
  const [selectedIdx, setSelectedIdx] = useState(0)
  const [opening, setOpening] = useState(false)
  const inputRef = useRef<HTMLInputElement | null>(null)
  const resultsRef = useRef<HTMLDivElement | null>(null)

  useEffect(() => {
    void dispatch(loadTasks())
    setTimeout(() => inputRef.current?.focus(), 0)
  }, [dispatch])

  async function newChat() {
    dispatch(uiActions.setView('chat'))
    dispatch(chatActions.clearChat())
    const resp = await api.newSession()
    // Stay off the sidebar until the first user message (empty UUID rows look broken).
    dispatch(sessionActions.setCurrentSession(resp.session_id))
  }

  async function openTask(task: TaskItem) {
    if (opening) return
    setOpening(true)
    try {
      if (task.unread) await api.updateTask(task.uuid, { unread: false }).catch(() => undefined)
      dispatch(uiActions.setView('chat'))
      if (task.project && task.project !== activePath) {
        if (isRemotePath(task.project)) {
          const meta = parseRemoteLabel(task.project)
          openRemoteConnect(meta ? { ...meta, loadTaskUuid: task.uuid } : undefined)
          close()
          return
        }
        const resp = await api.switchProject(task.project)
        dispatch(sessionActions.setProjectPath(resp.pwd || task.project))
        await dispatch(loadWorkspaceState())
      }
      await dispatch(loadSession(task.uuid))
      close()
    } finally {
      setOpening(false)
    }
  }

  function close() {
    dispatch(uiActions.setPaletteOpen(false))
  }

  const actions = useMemo<PaletteItem[]>(() => [
    { id: 'new', group: t('commandPalette.groups.actions'), label: t('commandPalette.newTask'), Icon: PlusIcon, run: newChat },
    { id: 'chat', group: t('commandPalette.groups.actions'), label: t('nav.chat'), Icon: ChatBubbleLeftIcon, run: () => dispatch(uiActions.setView('chat')) },
    { id: 'automations', group: t('commandPalette.groups.actions'), label: t('nav.automations'), Icon: ChatBubbleLeftIcon, run: () => dispatch(uiActions.setView('automations')) },
    { id: 'channels', group: t('commandPalette.groups.actions'), label: t('nav.channels'), Icon: ChatBubbleLeftIcon, run: () => dispatch(uiActions.setView('channels')) },
    { id: 'remote', group: t('commandPalette.groups.actions'), label: t('nav.remoteConnect'), Icon: ServerIcon, run: () => openRemoteConnect() },
    { id: 'settings', group: t('commandPalette.groups.actions'), label: t('nav.openSettings'), Icon: Cog6ToothIcon, run: () => dispatch(uiActions.setView('settings')) },
    { id: 'folder', group: t('commandPalette.groups.actions'), label: t('nav.openProject'), Icon: FolderOpenIcon, run: () => {
      dispatch(uiActions.setView('chat'))
      setTimeout(() => window.dispatchEvent(new Event('jcode:open-workspace-picker')), 0)
    } },
  ], [dispatch, t])

  const taskItems = useMemo<PaletteItem[]>(() =>
    tasks
      .filter((task) => !task.archived)
      .map((task) => ({
        id: `task-${task.uuid}`,
        group: t('commandPalette.groups.tasks'),
        label: task.title || `${task.uuid.slice(0, 8)}...`,
        hint: workspaceName(task.project),
        Icon: ChatBubbleLeftIcon,
        run: () => openTask(task),
      })),
  [tasks, t, activePath, opening])

  const results = useMemo(() => {
    const q = query.trim().toLowerCase()
    const all = [...actions, ...taskItems]
    if (!q) return [...actions, ...taskItems.slice(0, 8)]
    return all.filter((item) => item.label.toLowerCase().includes(q) || (item.hint || '').toLowerCase().includes(q))
  }, [actions, taskItems, query])

  useEffect(() => {
    setSelectedIdx(0)
  }, [query, results.length])

  function move(delta: number) {
    if (results.length === 0) return
    setSelectedIdx((idx) => {
      const next = (idx + delta + results.length) % results.length
      setTimeout(() => resultsRef.current?.querySelector(`[data-index="${next}"]`)?.scrollIntoView({ block: 'nearest' }), 0)
      return next
    })
  }

  async function runItem(item: PaletteItem) {
    await item.run()
    close()
  }

  function onKey(e: React.KeyboardEvent<HTMLInputElement>) {
    if (e.key === 'Escape') {
      e.preventDefault()
      close()
    } else if (e.key === 'ArrowDown') {
      e.preventDefault()
      move(1)
    } else if (e.key === 'ArrowUp') {
      e.preventDefault()
      move(-1)
    } else if (e.key === 'Enter') {
      e.preventDefault()
      const item = results[selectedIdx]
      if (item) void runItem(item)
    }
  }

  const groups: { name: string; items: PaletteItem[] }[] = []
  for (const item of results) {
    let group = groups.find((g) => g.name === item.group)
    if (!group) {
      group = { name: item.group, items: [] }
      groups.push(group)
    }
    group.items.push(item)
  }

  return (
    <div className="fixed inset-0 z-[var(--z-modal)] flex items-start justify-center bg-[var(--backdrop)] px-4 pt-[12vh] backdrop-blur-[6px]" onClick={close}>
      <div className="w-full max-w-xl overflow-hidden rounded-[var(--radius-xl)] border border-[var(--color-border)] bg-[var(--color-surface)] shadow-[var(--shadow-lg)]" onClick={(e) => e.stopPropagation()}>
        <div className="flex items-center gap-2 border-b border-[var(--color-border)] px-3 py-2">
          <MagnifyingGlassIcon className="h-4 w-4 text-[var(--color-muted-foreground)]" />
          <input
            ref={inputRef}
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            onKeyDown={onKey}
            placeholder={t('commandPalette.placeholder')}
            className="min-w-0 flex-1 border-none bg-transparent text-sm text-[var(--color-foreground)] outline-none"
          />
          <kbd className="rounded-[var(--radius-sm)] border border-[var(--color-border)] bg-[var(--color-muted)] px-1.5 py-0.5 font-mono text-[10px] text-[var(--color-muted-foreground)]">Esc</kbd>
        </div>
        <div ref={resultsRef} className="max-h-[420px] overflow-y-auto p-1.5">
          {results.length === 0 ? (
            <div className="px-3 py-8 text-center text-xs text-[var(--color-muted-foreground)]">{t('commandPalette.noResults')}</div>
          ) : (
            groups.map((group) => (
              <div key={group.name} className="pb-1">
                <div className="px-2 pb-1 pt-2 text-[10px] font-semibold uppercase tracking-[0.06em] text-[var(--color-muted-foreground)]">{group.name}</div>
                {group.items.map((item) => {
                  const idx = results.findIndex((x) => x.id === item.id)
                  const selected = idx === selectedIdx
                  return (
                    <button
                      key={item.id}
                      type="button"
                      data-index={idx}
                      onMouseEnter={() => setSelectedIdx(idx)}
                      onClick={() => void runItem(item)}
                      className={`flex w-full items-center gap-2.5 rounded-[var(--radius-md)] px-2.5 py-2 text-left transition-colors ${
                        selected ? 'bg-[var(--color-muted)]' : 'hover:bg-[var(--neutral-wash-soft)]'
                      }`}
                    >
                      <item.Icon className="h-4 w-4 shrink-0 text-[var(--color-muted-foreground)]" />
                      <span className="min-w-0 flex-1">
                        <span className="block truncate text-[13px] text-[var(--color-foreground)]">{item.label}</span>
                        {item.hint && <span className="block truncate font-mono text-[10.5px] text-[var(--color-muted-foreground)]">{item.hint}</span>}
                      </span>
                    </button>
                  )
                })}
              </div>
            ))
          )}
        </div>
      </div>
    </div>
  )
}

function workspaceName(path: string): string {
  if (!path) return ''
  const parts = path.split('/').filter(Boolean)
  return parts.at(-1) || path
}
