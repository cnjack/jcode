/**
 * TodoRenderer — renders `todowrite`/`todoread` tool calls.
 * Parses the todo list from args (todowrite) or output (todoread) and renders
 * status icons. Mirrors the Vue TaskList component.
 */

import { memo, useMemo } from 'react'
import {
  CheckCircleIcon,
  XCircleIcon,
  PlayCircleIcon,
  EllipsisHorizontalCircleIcon,
} from '@heroicons/react/24/outline'
import type { ToolRendererProps } from 'jcode-ui-core/adapters'
import type { TodoItem } from 'jcode-ui-core'

export const TodoRenderer = memo(function TodoRenderer({ args, output }: ToolRendererProps) {
  const todos = useMemo(() => extractTodos(args, output), [args, output])
  if (todos.length === 0) {
    return <div className="px-3 py-2 text-[var(--color-muted-foreground)]">No todos</div>
  }
  return (
    <ul className="jcode-todo jcode-selectable my-1 space-y-1 rounded-[var(--radius-md)] border border-[var(--color-border)] bg-[var(--code-bg)] px-3 py-2">
      {todos.map((t, i) => (
        <li key={t.id ?? i} className="flex items-start gap-2 text-[0.82rem]">
          <StatusIcon status={t.status} />
          <span className={t.status === 'completed' ? 'text-[var(--color-muted-foreground)] line-through' : 'text-[var(--color-foreground)]'}>
            {t.title}
          </span>
        </li>
      ))}
    </ul>
  )
})

function StatusIcon({ status }: { status: TodoItem['status'] }) {
  const cls = 'mt-0.5 h-4 w-4 shrink-0'
  switch (status) {
    case 'completed':
      return <CheckCircleIcon className={`${cls} text-[var(--color-success)]`} />
    case 'in_progress':
      return <PlayCircleIcon className={`${cls} text-[var(--color-primary)]`} />
    case 'cancelled':
      return <XCircleIcon className={`${cls} text-[var(--color-muted-foreground)]`} />
    default:
      return <EllipsisHorizontalCircleIcon className={`${cls} text-[var(--color-muted-foreground)]`} />
  }
}

function extractTodos(args: string, output?: string): TodoItem[] {
  // todowrite: todos in args
  try {
    const parsed = JSON.parse(args)
    if (Array.isArray(parsed.todos)) return normalize(parsed.todos)
    if (Array.isArray(parsed)) return normalize(parsed)
  } catch {
    // ignore
  }
  // todoread: todos in output (JSON or free text)
  if (output) {
    try {
      const parsed = JSON.parse(output)
      if (Array.isArray(parsed)) return normalize(parsed)
      if (Array.isArray(parsed.todos)) return normalize(parsed)
    } catch {
      // ignore
    }
  }
  return []
}

function normalize(arr: unknown[]): TodoItem[] {
  return arr
    .map((a, i): TodoItem | null => {
      if (typeof a !== 'object' || a === null) return null
      const o = a as Record<string, unknown>
      return {
        id: typeof o.id === 'string' ? o.id : `todo_${i}`,
        title: typeof o.title === 'string' ? o.title : typeof o.content === 'string' ? o.content : '',
        status: isValidStatus(o.status) ? o.status : 'pending',
      }
    })
    .filter((x): x is TodoItem => x !== null)
}

function isValidStatus(s: unknown): s is TodoItem['status'] {
  return s === 'pending' || s === 'in_progress' || s === 'completed' || s === 'cancelled'
}
