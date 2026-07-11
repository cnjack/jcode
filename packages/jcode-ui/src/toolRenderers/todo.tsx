/**
 * TodoRenderer — `todowrite` / `todoread`. Thin wrapper: parse todos from
 * args/output, then defer to the first-class <TaskList> component.
 */

import { memo, useMemo } from 'react'
import type { ToolRendererProps } from 'jcode-ui-core/adapters'
import type { TodoItem } from 'jcode-ui-core'
import { TaskList } from '../components/TaskList.js'

export const TodoRenderer = memo(function TodoRenderer({
  args,
  output,
  error,
  status,
}: ToolRendererProps) {
  const todos = useMemo(() => extractTodos(args, output), [args, output])

  if (todos.length === 0) {
    if (status === 'running') {
      return (
        <div className="animate-pulse px-3 py-2 text-xs" style={{ color: 'var(--jcode-color-muted-foreground)' }}>
          Loading…
        </div>
      )
    }
    return (
      <div className="px-3 py-2 text-xs italic" style={{ color: 'var(--jcode-color-muted-foreground)' }}>
        No todos
      </div>
    )
  }

  return (
    <div className="jcode-todo max-h-64 overflow-y-auto px-3 py-2" style={{ background: 'var(--jcode-color-surface)' }}>
      <TaskList items={todos} compact />
      {error && (
        <div
          className="mt-1.5 whitespace-pre-wrap font-mono text-xs"
          style={{ color: 'var(--jcode-color-destructive, var(--jcode-color-error-fg))' }}
        >
          {error}
        </div>
      )}
    </div>
  )
})

function extractTodos(args: string, output?: string): TodoItem[] {
  // Prefer output (final state), fall back to args (requested state) — Vue order.
  if (output) {
    const jsonMatch = output.match(/\[[\s\S]*\]/)
    if (jsonMatch) {
      try {
        return normalize(JSON.parse(jsonMatch[0]) as unknown[])
      } catch {
        // fall through
      }
    }
    try {
      const parsed = JSON.parse(output)
      if (Array.isArray(parsed)) return normalize(parsed)
      if (Array.isArray(parsed.todos)) return normalize(parsed.todos)
    } catch {
      // fall through
    }
  }
  try {
    const parsed = JSON.parse(args)
    if (Array.isArray(parsed.todos)) return normalize(parsed.todos)
    if (Array.isArray(parsed)) return normalize(parsed)
  } catch {
    // ignore
  }
  return []
}

function normalize(arr: unknown[]): TodoItem[] {
  return arr
    .map((a, i): TodoItem | null => {
      if (typeof a !== 'object' || a === null) return null
      const o = a as Record<string, unknown>
      return {
        id: typeof o.id === 'number' ? o.id : i,
        title:
          typeof o.title === 'string'
            ? o.title
            : typeof o.content === 'string'
              ? o.content
              : '',
        status: isValidStatus(o.status) ? o.status : 'pending',
      }
    })
    .filter((x): x is TodoItem => x !== null)
}

function isValidStatus(s: unknown): s is TodoItem['status'] {
  return s === 'pending' || s === 'in_progress' || s === 'completed' || s === 'cancelled'
}
