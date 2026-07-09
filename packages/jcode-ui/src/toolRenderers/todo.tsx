/**
 * TodoRenderer — `todowrite` / `todoread` (matches Vue TaskList + tool block).
 * Icons: CheckCircle / ArrowPath(spin) / NoSymbol / MinusCircle.
 * in_progress gets a soft highlight + left accent bar.
 */

import { memo, useEffect, useMemo, useState } from 'react'
import {
  ArrowPathIcon,
  CheckCircleIcon,
  EllipsisHorizontalCircleIcon,
  MinusCircleIcon,
  NoSymbolIcon,
} from '@heroicons/react/24/outline'
import type { ToolRendererProps } from 'jcode-ui-core/adapters'
import type { TodoItem } from 'jcode-ui-core'

export const TodoRenderer = memo(function TodoRenderer({
  args,
  output,
  error,
  status,
}: ToolRendererProps) {
  const todos = useMemo(() => extractTodos(args, output), [args, output])
  const reduceMotion = usePrefersReducedMotion()

  if (todos.length === 0) {
    if (status === 'running') {
      return (
        <div className="animate-pulse px-3 py-2 text-xs" style={{ color: 'var(--color-muted-foreground)' }}>
          Loading…
        </div>
      )
    }
    return (
      <div className="px-3 py-2 text-xs italic" style={{ color: 'var(--color-muted-foreground)' }}>
        No todos
      </div>
    )
  }

  return (
    <div className="jcode-todo max-h-64 overflow-y-auto px-3 py-2" style={{ background: 'var(--color-surface)' }}>
      <div className="space-y-0.5">
        {todos.map((todo, i) => {
          const done = todo.status === 'completed' || todo.status === 'cancelled'
          const active = todo.status === 'in_progress'
          return (
            <div
              key={todo.id ?? i}
              className="relative flex items-center gap-2 py-1 pl-2 pr-1.5"
              style={
                active
                  ? {
                      backgroundColor: 'var(--color-warning-bg)',
                      borderRadius: 'var(--radius-md)',
                    }
                  : undefined
              }
            >
              {active && (
                <span
                  className="absolute bottom-0.5 left-0 top-0.5 w-0.5 rounded-[var(--radius-pill)]"
                  style={{ backgroundColor: 'var(--color-accent-neutral)' }}
                  aria-hidden
                />
              )}
              <StatusIcon status={todo.status} reduceMotion={reduceMotion} />
              <span
                className={`min-w-0 flex-1 truncate text-xs sm:text-sm ${active ? 'font-medium' : ''}`}
                style={{
                  color: done ? 'var(--color-muted-foreground)' : 'var(--color-foreground)',
                  textDecoration: done ? 'line-through' : 'none',
                }}
              >
                {todo.title}
              </span>
            </div>
          )
        })}
      </div>
      {error && (
        <div
          className="mt-1.5 whitespace-pre-wrap font-mono text-xs"
          style={{ color: 'var(--color-destructive, var(--color-error-fg))' }}
        >
          {error}
        </div>
      )}
    </div>
  )
})

function StatusIcon({
  status,
  reduceMotion,
}: {
  status: TodoItem['status']
  reduceMotion: boolean
}) {
  const cls = 'h-3.5 w-3.5 shrink-0'
  switch (status) {
    case 'completed':
      return <CheckCircleIcon className={cls} style={{ color: 'var(--color-success-fg)' }} />
    case 'cancelled':
      return (
        <NoSymbolIcon
          className={cls}
          style={{ color: 'var(--color-destructive, var(--color-error-fg))' }}
        />
      )
    case 'in_progress':
      return reduceMotion ? (
        <EllipsisHorizontalCircleIcon className={cls} style={{ color: 'var(--color-accent-neutral)' }} />
      ) : (
        <ArrowPathIcon
          className={`${cls} animate-spin`}
          style={{ color: 'var(--color-accent-neutral)' }}
        />
      )
    default:
      return <MinusCircleIcon className={cls} style={{ color: 'var(--color-muted-foreground)' }} />
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
