/**
 * ThreadList — session/thread list sidebar (the sidebar analog of `Thread`).
 *
 * Reads a `ThreadStore` (via `ThreadStoreProvider` from jcode-ui-core) and
 * renders grouped rows: an Active group and a collapsible Archived group. Each
 * row shows the title, a relative timestamp, and a pulsing dot while running;
 * the active row gets an `--jcode-accent-wash` fill plus a 2px accent bar.
 *
 * Fail-visible: per-row controls (rename / archive / delete) and the New button
 * render only when the matching `store.actions.*` exists — mirroring how
 * `Message` shows its edit affordance only when `canEdit` is set. A host that
 * wires just `select` gets a clean read-only list with no dangling controls.
 *
 * All visuals live in `../styles/threadlist.css` (`.jcode-threadlist-*`), which
 * the host imports via `jcode-ui/styles.css`. Style is aligned with Sources /
 * ContextBar: `data-jcode-ui` root, token-driven colors, heroicons, memo.
 */

import { memo, useEffect, useMemo, useRef, useState } from 'react'
import type { KeyboardEvent as ReactKeyboardEvent } from 'react'
import {
  ArchiveBoxIcon,
  ChatBubbleLeftRightIcon,
  ChevronRightIcon,
  EllipsisHorizontalIcon,
  PencilSquareIcon,
  PlusIcon,
  TrashIcon,
} from '@heroicons/react/24/outline'
import type { ThreadSummary, ThreadStoreActions } from 'jcode-ui-core'
import { useThreadListState, useThreadStoreActions } from 'jcode-ui-core'

export interface ThreadListProps {
  /** Optional small header label above the list (e.g. "Sessions"). */
  title?: string
  /** Extra class on the root (composed after `jcode-threadlist`). */
  className?: string
}

export const ThreadList = memo(function ThreadList({ title, className }: ThreadListProps) {
  const { threads, activeId, loading } = useThreadListState()
  const actions = useThreadStoreActions()
  const [renamingId, setRenamingId] = useState<string | null>(null)
  const [archivedOpen, setArchivedOpen] = useState(false)

  const { active, archived } = useMemo(() => {
    const a: ThreadSummary[] = []
    const ar: ThreadSummary[] = []
    for (const t of threads) (t.archived ? ar : a).push(t)
    const byRecency = (x: ThreadSummary, y: ThreadSummary) => y.updatedAt - x.updatedAt
    return { active: a.sort(byRecency), archived: ar.sort(byRecency) }
  }, [threads])

  const canCreate = !!actions.create

  // Empty state — centered prompt + New button (fail-visible).
  if (threads.length === 0 && !loading) {
    return (
      <div data-jcode-ui="" className={cx('jcode-threadlist jcode-threadlist--empty', className)}>
        <div className="jcode-threadlist-empty">
          <ChatBubbleLeftRightIcon className="jcode-threadlist-empty-icon" />
          <p className="jcode-threadlist-empty-text">No threads yet</p>
          {canCreate && (
            <button type="button" className="jcode-threadlist-new" onClick={() => actions.create!()}>
              <PlusIcon className="jcode-threadlist-icon" />
              New thread
            </button>
          )}
        </div>
      </div>
    )
  }

  return (
    <div data-jcode-ui="" className={cx('jcode-threadlist', className)}>
      {(title || canCreate) && (
        <div className="jcode-threadlist-header">
          {title && <span className="jcode-threadlist-title">{title}</span>}
          {canCreate && (
            <button type="button" className="jcode-threadlist-new" onClick={() => actions.create!()}>
              <PlusIcon className="jcode-threadlist-icon" />
              New thread
            </button>
          )}
        </div>
      )}

      <div className="jcode-threadlist-scroll">
        {loading && threads.length === 0 && (
          <div className="jcode-threadlist-loading">Loading…</div>
        )}

        {active.length > 0 && (
          <div className="jcode-threadlist-group">
            {archived.length > 0 && <div className="jcode-threadlist-group-label">Active</div>}
            <ul className="jcode-threadlist-rows">
              {active.map((t) => (
                <ThreadRow
                  key={t.id}
                  thread={t}
                  isActive={t.id === activeId}
                  actions={actions}
                  renaming={renamingId === t.id}
                  onStartRename={() => setRenamingId(t.id)}
                  onStopRename={() => setRenamingId(null)}
                />
              ))}
            </ul>
          </div>
        )}

        {archived.length > 0 && (
          <div className="jcode-threadlist-group">
            <button
              type="button"
              className="jcode-threadlist-archived-toggle"
              aria-expanded={archivedOpen}
              onClick={() => setArchivedOpen((o) => !o)}
            >
              <ChevronRightIcon
                className={cx(
                  'jcode-threadlist-chevron',
                  archivedOpen && 'jcode-threadlist-chevron--open',
                )}
              />
              Archived
              <span className="jcode-threadlist-count">{archived.length}</span>
            </button>
            {archivedOpen && (
              <ul className="jcode-threadlist-rows">
                {archived.map((t) => (
                  <ThreadRow
                    key={t.id}
                    thread={t}
                    isActive={t.id === activeId}
                    actions={actions}
                    renaming={renamingId === t.id}
                    onStartRename={() => setRenamingId(t.id)}
                    onStopRename={() => setRenamingId(null)}
                  />
                ))}
              </ul>
            )}
          </div>
        )}
      </div>
    </div>
  )
})

interface ThreadRowProps {
  thread: ThreadSummary
  isActive: boolean
  actions: ThreadStoreActions
  renaming: boolean
  onStartRename: () => void
  onStopRename: () => void
}

const ThreadRow = memo(function ThreadRow({
  thread,
  isActive,
  actions,
  renaming,
  onStartRename,
  onStopRename,
}: ThreadRowProps) {
  const [draft, setDraft] = useState(thread.title)

  // Re-seed the draft each time this row enters rename mode.
  useEffect(() => {
    if (renaming) setDraft(thread.title)
  }, [renaming, thread.title])

  const commit = () => {
    const text = draft.trim()
    onStopRename()
    if (text && text !== thread.title) actions.rename?.(thread.id, text)
  }

  if (renaming) {
    return (
      <li className="jcode-threadlist-item">
        <input
          className="jcode-threadlist-rename-input"
          value={draft}
          aria-label="Rename thread"
          onChange={(e) => setDraft(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === 'Enter') {
              e.preventDefault()
              commit()
            } else if (e.key === 'Escape') {
              e.preventDefault()
              onStopRename()
            }
          }}
          // Clicking away discards the edit (Enter saves, Esc cancels).
          onBlur={onStopRename}
          // eslint-disable-next-line jsx-a11y/no-autofocus
          autoFocus
        />
      </li>
    )
  }

  const relative = formatRelative(thread.updatedAt)

  return (
    <li className="jcode-threadlist-item">
      <button
        type="button"
        className={cx('jcode-threadlist-row', isActive && 'jcode-threadlist-row--active')}
        aria-current={isActive ? 'page' : undefined}
        onClick={() => actions.select?.(thread.id)}
      >
        <span className="jcode-threadlist-row-main">
          <span className="jcode-threadlist-row-title">{thread.title || 'Untitled'}</span>
          <span className="jcode-threadlist-row-meta">
            {thread.status === 'running' && (
              <span className="jcode-threadlist-dot" title="Running" aria-hidden="true" />
            )}
            <span>{relative}</span>
          </span>
        </span>
      </button>
      <RowActions thread={thread} actions={actions} onRename={onStartRename} />
    </li>
  )
})

interface RowActionsProps {
  thread: ThreadSummary
  actions: ThreadStoreActions
  onRename: () => void
}

/** The hover ⋯ menu. Owns its own open state + focus management so click-outside
 *  auto-closes any other row's menu. Renders nothing if no menu action exists. */
function RowActions({ thread, actions, onRename }: RowActionsProps) {
  const [open, setOpen] = useState(false)
  const triggerRef = useRef<HTMLButtonElement>(null)
  const menuRef = useRef<HTMLDivElement>(null)

  const canRename = !!actions.rename
  const canArchive = !!actions.archive && !thread.archived
  const canRemove = !!actions.remove

  const close = (returnFocus?: boolean) => {
    setOpen(false)
    if (returnFocus) triggerRef.current?.focus()
  }

  useEffect(() => {
    if (!open) return
    // Focus the first item on open.
    menuRef.current?.querySelector<HTMLButtonElement>('[role="menuitem"]')?.focus()
    // Dismiss on outside pointer-down.
    const onDoc = (e: MouseEvent) => {
      const target = e.target as Node
      if (!menuRef.current?.contains(target) && !triggerRef.current?.contains(target)) {
        setOpen(false)
      }
    }
    document.addEventListener('mousedown', onDoc)
    return () => document.removeEventListener('mousedown', onDoc)
  }, [open])

  if (!canRename && !canArchive && !canRemove) return null

  const onMenuKeyDown = (e: ReactKeyboardEvent) => {
    if (e.key === 'Escape') {
      e.preventDefault()
      close(true)
      return
    }
    if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
      e.preventDefault()
      const items = Array.from(
        menuRef.current?.querySelectorAll<HTMLButtonElement>('[role="menuitem"]') ?? [],
      )
      if (items.length === 0) return
      const idx = items.indexOf(document.activeElement as HTMLButtonElement)
      const delta = e.key === 'ArrowDown' ? 1 : -1
      const next = (idx + delta + items.length) % items.length
      items[next]?.focus()
    }
  }

  return (
    <div className="jcode-threadlist-actions">
      <button
        ref={triggerRef}
        type="button"
        className="jcode-threadlist-menu-btn"
        aria-haspopup="menu"
        aria-expanded={open}
        aria-label="Thread options"
        onClick={() => setOpen((o) => !o)}
      >
        <EllipsisHorizontalIcon className="jcode-threadlist-icon" />
      </button>
      {open && (
        <div ref={menuRef} role="menu" className="jcode-threadlist-menu" onKeyDown={onMenuKeyDown}>
          {canRename && (
            <button
              type="button"
              role="menuitem"
              className="jcode-threadlist-menu-item"
              onClick={() => {
                close()
                onRename()
              }}
            >
              <PencilSquareIcon className="jcode-threadlist-icon" />
              Rename
            </button>
          )}
          {canArchive && (
            <button
              type="button"
              role="menuitem"
              className="jcode-threadlist-menu-item"
              onClick={() => {
                close()
                actions.archive?.(thread.id)
              }}
            >
              <ArchiveBoxIcon className="jcode-threadlist-icon" />
              Archive
            </button>
          )}
          {canRemove && (
            <button
              type="button"
              role="menuitem"
              className="jcode-threadlist-menu-item jcode-threadlist-menu-item--danger"
              onClick={() => {
                close()
                actions.remove?.(thread.id)
              }}
            >
              <TrashIcon className="jcode-threadlist-icon" />
              Delete
            </button>
          )}
        </div>
      )}
    </div>
  )
}

/** Join truthy class fragments (tiny local helper — no clsx dependency). */
function cx(...parts: (string | false | null | undefined)[]): string {
  return parts.filter(Boolean).join(' ')
}

/**
 * Compact relative-time formatter (no date-fns / dayjs). Buckets: just now,
 * Nm, Nh, Nd, Nw, Nmo, Ny. `now` is injectable for deterministic tests.
 */
export function formatRelative(ts: number, now: number = Date.now()): string {
  const diff = now - ts
  if (diff < 0) return 'just now'
  const sec = Math.floor(diff / 1000)
  if (sec < 60) return 'just now'
  const min = Math.floor(sec / 60)
  if (min < 60) return `${min}m ago`
  const hr = Math.floor(min / 60)
  if (hr < 24) return `${hr}h ago`
  const day = Math.floor(hr / 24)
  if (day < 7) return `${day}d ago`
  const wk = Math.floor(day / 7)
  if (wk < 5) return `${wk}w ago`
  const mo = Math.floor(day / 30)
  if (mo < 12) return `${mo}mo ago`
  const yr = Math.floor(day / 365)
  return `${yr}y ago`
}
