/**
 * TaskList — first-class, reusable task/todo list (extracted from the todo
 * renderer so hosts can drop it anywhere: cloud run pages, automations, goals…).
 *
 * Compound API:
 *   <TaskList items={TodoItem[]} title? compact? />
 *   <TaskList.Item item={TodoItem} compact? />
 *
 * Top progress bar tracks completed/total (filled with --jcode-accent-fill).
 * Status icons: pending = hollow ring · in_progress = half ring (spins) ·
 * completed = check (success) · cancelled = strikethrough (muted).
 *
 * `RuntimeTaskList` wires the same UI to the runtime `todos` selector.
 */

import { memo, useEffect, useMemo, useState } from 'react'
import type { TodoItem } from 'jcode-ui-core'
import { useRuntimeSelector } from 'jcode-ui-core/runtime'

export interface TaskListProps {
  /** Ordered task items. */
  items: TodoItem[]
  /** Optional heading shown above the progress bar. */
  title?: string
  /** Denser rows + smaller type for embedding in tool cards. */
  compact?: boolean
  /** Hide the top progress bar. Default false. */
  hideProgress?: boolean
  /** Extra classes on the root. */
  className?: string
}

export interface TaskListItemProps {
  item: TodoItem
  compact?: boolean
}

function TaskListRoot({ items, title, compact, hideProgress, className }: TaskListProps) {
  const total = items.length
  const completed = useMemo(
    () => items.filter((t) => t.status === 'completed').length,
    [items],
  )
  const pct = total > 0 ? Math.round((completed / total) * 100) : 0

  return (
    <div
      data-jcode-ui=""
      className={`jcode-tasklist${compact ? ' jcode-tasklist--compact' : ''}${className ? ` ${className}` : ''}`}
    >
      {(title || !hideProgress) && (
        <div className="jcode-tasklist__head">
          {title && <span className="jcode-tasklist__title">{title}</span>}
          {total > 0 && (
            <span className="jcode-tasklist__count" data-testid="tasklist-count">
              {completed}/{total}
            </span>
          )}
        </div>
      )}
      {!hideProgress && total > 0 && (
        <div
          className="jcode-tasklist__progress"
          role="progressbar"
          aria-valuemin={0}
          aria-valuemax={100}
          aria-valuenow={pct}
        >
          <div className="jcode-tasklist__progress-fill" style={{ width: `${pct}%` }} />
        </div>
      )}
      <div className="jcode-tasklist__items">
        {items.map((item, i) => (
          <TaskListItem key={item.id ?? i} item={item} compact={compact} />
        ))}
      </div>
    </div>
  )
}

const TaskListItem = memo(function TaskListItem({ item, compact }: TaskListItemProps) {
  const reduceMotion = usePrefersReducedMotion()
  const done = item.status === 'completed' || item.status === 'cancelled'
  const active = item.status === 'in_progress'
  return (
    <div
      className={`jcode-tasklist__item jcode-tasklist__item--${item.status}${compact ? ' jcode-tasklist__item--compact' : ''}`}
      data-status={item.status}
    >
      {active && <span className="jcode-tasklist__bar" aria-hidden />}
      <TaskStatusIcon status={item.status} reduceMotion={reduceMotion} />
      <span className={`jcode-tasklist__label${done ? ' jcode-tasklist__label--done' : ''}${active ? ' jcode-tasklist__label--active' : ''}`}>
        {item.title}
      </span>
    </div>
  )
})

function TaskStatusIcon({
  status,
  reduceMotion,
}: {
  status: TodoItem['status']
  reduceMotion: boolean
}) {
  const cls = `jcode-tasklist__icon jcode-tasklist__icon--${status}`
  switch (status) {
    case 'completed':
      return (
        <svg className={cls} viewBox="0 0 16 16" aria-hidden focusable="false">
          <circle cx="8" cy="8" r="7" fill="currentColor" />
          <path
            d="M4.5 8.2l2.2 2.2 4.6-4.8"
            fill="none"
            stroke="var(--jcode-color-surface)"
            strokeWidth="1.6"
            strokeLinecap="round"
            strokeLinejoin="round"
          />
        </svg>
      )
    case 'cancelled':
      return (
        <svg className={cls} viewBox="0 0 16 16" aria-hidden focusable="false">
          <circle cx="8" cy="8" r="6.4" fill="none" stroke="currentColor" strokeWidth="1.4" />
          <line x1="5" y1="8" x2="11" y2="8" stroke="currentColor" strokeWidth="1.4" strokeLinecap="round" />
        </svg>
      )
    case 'in_progress':
      return (
        <svg
          className={`${cls}${reduceMotion ? '' : ' jcode-spin'}`}
          viewBox="0 0 16 16"
          aria-hidden
          focusable="false"
        >
          <circle cx="8" cy="8" r="6.2" fill="none" stroke="currentColor" strokeOpacity="0.25" strokeWidth="1.6" />
          <path
            d="M8 1.8a6.2 6.2 0 0 1 6.2 6.2"
            fill="none"
            stroke="currentColor"
            strokeWidth="1.6"
            strokeLinecap="round"
          />
        </svg>
      )
    default:
      return (
        <svg className={cls} viewBox="0 0 16 16" aria-hidden focusable="false">
          <circle cx="8" cy="8" r="6.4" fill="none" stroke="currentColor" strokeWidth="1.4" />
        </svg>
      )
  }
}

function usePrefersReducedMotion(): boolean {
  const [v, setV] = useState(false)
  useEffect(() => {
    if (typeof window === 'undefined' || !window.matchMedia) return
    const mql = window.matchMedia('(prefers-reduced-motion: reduce)')
    setV(mql.matches)
    const onChange = (e: MediaQueryListEvent) => setV(e.matches)
    mql.addEventListener('change', onChange)
    return () => mql.removeEventListener('change', onChange)
  }, [])
  return v
}

/** Compound component: `TaskList` + `TaskList.Item`. */
export const TaskList = Object.assign(memo(TaskListRoot), { Item: TaskListItem })

export interface RuntimeTaskListProps {
  title?: string
  compact?: boolean
  hideProgress?: boolean
  className?: string
}

/** TaskList bound to the runtime `todos` selector. */
export const RuntimeTaskList = memo(function RuntimeTaskList(props: RuntimeTaskListProps) {
  const todos = useRuntimeSelector((s) => s.todos)
  if (!todos || todos.length === 0) return null
  return <TaskList items={todos} {...props} />
})
