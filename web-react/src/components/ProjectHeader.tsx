/**
 * ProjectHeader — top bar: project path, model, mode, connection status,
 * theme toggle, and settings button.
 */

import { Cog6ToothIcon } from '@heroicons/react/24/outline'
import { useAppDispatch, useAppSelector } from '../app/hooks'
import { uiActions } from '../app/store'
import { ThemeToggle } from './ThemeToggle'

export function ProjectHeader() {
  const dispatch = useAppDispatch()
  const projectPath = useAppSelector((s) => s.session.projectPath)
  const provider = useAppSelector((s) => s.model.providerName)
  const model = useAppSelector((s) => s.model.modelName)
  const mode = useAppSelector((s) => s.model.mode)
  const wsConnected = useAppSelector((s) => s.session.wsConnected)

  return (
    <header className="flex h-[var(--header-height)] shrink-0 items-center gap-3 border-b border-[var(--color-border)] bg-[var(--color-surface)] px-4 text-sm">
      <span className="truncate font-mono text-xs text-[var(--color-muted-foreground)]" title={projectPath}>
        {projectPath || '—'}
      </span>
      {provider && model && (
        <span className="rounded-[var(--radius-pill)] bg-[var(--color-muted)] px-2 py-0.5 text-xs">
          {provider} · {model}
        </span>
      )}
      {mode && (
        <span className="rounded-[var(--radius-pill)] bg-[var(--accent-wash)] px-2 py-0.5 text-xs text-[var(--color-foreground)]">
          {mode}
        </span>
      )}
      <div className="ml-auto flex items-center gap-1">
        <ThemeToggle />
        <button
          type="button"
          onClick={() => dispatch(uiActions.setSettingsOpen(true))}
          className="rounded-[var(--radius-md)] p-1.5 text-[var(--color-muted-foreground)] transition-colors hover:bg-[var(--neutral-wash-soft)] hover:text-[var(--color-foreground)]"
          aria-label="Settings"
          title="Settings (⌘,)"
        >
          <Cog6ToothIcon className="h-4 w-4" />
        </button>
        <span
          className={`h-2 w-2 rounded-full ${wsConnected ? 'bg-[var(--color-success)]' : 'bg-[var(--color-muted-foreground)]'}`}
          title={wsConnected ? 'connected' : 'disconnected'}
        />
      </div>
    </header>
  )
}
