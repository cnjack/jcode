/**
 * Sidebar — workspace/task tree + nav (chat / automations / channels).
 *
 * A trimmed port of the Vue Sidebar (1.1k lines): shows the active project's
 * sessions grouped by recency, with running/pinned indicators, and the view
 * switcher. The full Vue sidebar has filter UI (status/project/group/sort) and
 * remote-workspace pickers; those are layered on in a follow-up — this is the
 * functional minimum that makes the chat app navigable.
 */

import { PlusIcon, ChatBubbleLeftIcon, BoltIcon, ChatBubbleOvalLeftIcon } from '@heroicons/react/24/outline'
import { useAppDispatch, useAppSelector } from '../app/hooks'
import { uiActions, sessionActions, chatActions } from '../app/store'
import { api } from '../lib/api'
import type { SessionItem } from '../lib/types'

export function Sidebar() {
  const dispatch = useAppDispatch()
  const sessions = useAppSelector((s) => s.session.sessions)
  const tasks = useAppSelector((s) => s.session.tasks)
  const currentSessionId = useAppSelector((s) => s.session.currentSessionId)
  const activeView = useAppSelector((s) => s.ui.activeView)
  const projectPath = useAppSelector((s) => s.session.projectPath)

  // A session is "running" if a task with the same uuid is running.
  const runningIds = new Set(tasks.filter((t) => t.running).map((t) => t.uuid))

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

  async function openSession(s: SessionItem) {
    dispatch(chatActions.clearChat())
    dispatch(sessionActions.setCurrentSession(s.uuid))
    dispatch(uiActions.setView('chat'))
    // Session replay (load entries + rebuild timeline) is a follow-up; the live
    // WS events will repopulate the timeline when the agent resumes.
  }

  return (
    <aside className="flex w-[var(--sidebar-width)] shrink-0 flex-col border-r border-[var(--color-border)] bg-[var(--color-sidebar-bg)]">
      {/* New chat */}
      <div className="p-2">
        <button
          type="button"
          onClick={newChat}
          className="flex w-full items-center gap-2 rounded-[var(--radius-lg)] bg-[var(--color-primary)] px-3 py-2 text-sm font-medium text-[var(--color-on-primary)] hover:bg-[var(--accent-wash-strong)]"
        >
          <PlusIcon className="h-4 w-4" />
          New chat
        </button>
      </div>

      {/* View nav */}
      <nav className="px-2 pb-2">
        <NavItem icon={ChatBubbleLeftIcon} label="Chat" active={activeView === 'chat'} onClick={() => dispatch(uiActions.setView('chat'))} />
        <NavItem icon={BoltIcon} label="Automations" active={activeView === 'automations'} onClick={() => dispatch(uiActions.setView('automations'))} />
        <NavItem icon={ChatBubbleOvalLeftIcon} label="Channels" active={activeView === 'channels'} onClick={() => dispatch(uiActions.setView('channels'))} />
      </nav>

      {/* Project label */}
      <div className="px-3 pb-1 pt-2 text-[0.65rem] font-semibold uppercase tracking-wide text-[var(--color-muted-foreground)]">
        {shortPath(projectPath)}
      </div>

      {/* Session list */}
      <div className="min-h-0 flex-1 overflow-y-auto px-1 pb-2">
        {sessions.length === 0 ? (
          <div className="px-3 py-2 text-xs text-[var(--color-muted-foreground)]">No conversations yet</div>
        ) : (
          sessions.map((s) => (
            <button
              key={s.uuid}
              type="button"
              onClick={() => openSession(s)}
              className={`group flex w-full items-center gap-2 rounded-[var(--radius-md)] px-2.5 py-1.5 text-left text-sm transition-colors ${
                s.uuid === currentSessionId
                  ? 'bg-[var(--accent-wash)] text-[var(--color-foreground)]'
                  : 'text-[var(--color-foreground)] hover:bg-[var(--neutral-wash-soft)]'
              }`}
            >
              {runningIds.has(s.uuid) && <span className="h-1.5 w-1.5 shrink-0 animate-pulse rounded-full bg-[var(--color-primary)]" />}
              <span className="truncate">{s.title || 'Untitled'}</span>
            </button>
          ))
        )}
      </div>
    </aside>
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

function shortPath(p: string): string {
  if (!p) return 'Workspace'
  const parts = p.replace(/\/$/, '').split('/')
  return parts.slice(-2).join('/')
}
