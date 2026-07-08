/**
 * ProjectHeader — top bar showing the active project path, model, and mode.
 * Minimal product chrome; the full Vue header has more controls (model/mode
 * pickers) which the Composer's suffix slots cover for the chat view.
 */

import { useAppSelector } from '../app/hooks'

export function ProjectHeader() {
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
      <span
        className={`ml-auto h-2 w-2 rounded-full ${wsConnected ? 'bg-[var(--color-success)]' : 'bg-[var(--color-muted-foreground)]'}`}
        title={wsConnected ? 'connected' : 'disconnected'}
      />
    </header>
  )
}
