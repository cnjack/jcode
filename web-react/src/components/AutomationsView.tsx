/**
 * AutomationsView — scheduled/manual agent runs.
 *
 * Functional skeleton: lists automations + run buttons. The full Vue view has
 * a CRUD editor, run history, and template picker — those are a follow-up port.
 * The data layer (api.automations*) is already wired in lib/api.ts.
 */

import { useEffect, useState } from 'react'
import { BoltIcon, PlayIcon } from '@heroicons/react/24/outline'
import { api } from '../lib/api'
import type { AutomationItem } from '../lib/automation'

export function AutomationsView() {
  const [items, setItems] = useState<AutomationItem[]>([])
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    let cancelled = false
    api
      .automations()
      .then((a) => !cancelled && setItems(a))
      .catch(() => {})
      .finally(() => !cancelled && setLoading(false))
    return () => {
      cancelled = true
    }
  }, [])

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <header className="flex h-[var(--header-height)] shrink-0 items-center gap-2 border-b border-[var(--color-border)] bg-[var(--color-surface)] px-4">
        <BoltIcon className="h-4 w-4 text-[var(--color-primary)]" />
        <h1 className="text-sm font-medium">Automations</h1>
      </header>
      <div className="min-h-0 flex-1 overflow-y-auto p-4">
        {loading ? (
          <div className="text-sm text-[var(--color-muted-foreground)]">Loading…</div>
        ) : items.length === 0 ? (
          <div className="text-sm text-[var(--color-muted-foreground)]">No automations configured.</div>
        ) : (
          <ul className="space-y-2">
            {items.map((a) => (
              <li
                key={a.id}
                className="flex items-center gap-3 rounded-[var(--radius-lg)] border border-[var(--color-border)] bg-[var(--color-surface)] px-3 py-2"
              >
                <div className="min-w-0 flex-1">
                  <div className="truncate text-sm font-medium">{a.name}</div>
                  <div className="truncate text-xs text-[var(--color-muted-foreground)]">
                    {a.human_schedule || a.trigger.type} · {a.enabled ? 'enabled' : 'disabled'}
                  </div>
                </div>
                <button
                  type="button"
                  onClick={() => api.automationRunNow(a.id).catch(() => {})}
                  className="flex items-center gap-1 rounded-[var(--radius-md)] bg-[var(--color-muted)] px-2 py-1 text-xs hover:bg-[var(--neutral-wash-soft)]"
                >
                  <PlayIcon className="h-3 w-3" />
                  Run
                </button>
              </li>
            ))}
          </ul>
        )}
      </div>
    </div>
  )
}
