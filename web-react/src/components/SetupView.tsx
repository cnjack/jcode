/**
 * SetupView — first-run provider configuration.
 *
 * Functional skeleton: lists setup providers, lets the user enter an API key +
 * pick a model, and calls /api/setup/complete. The full Vue view (617 lines)
 * has provider detail panes, model catalogs, validation, and base_url/headers
 * for custom endpoints — those are a follow-up. The data layer is wired.
 */

import { useEffect, useState } from 'react'
import { api } from '../lib/api'
import type { SetupProvider, SetupModel } from '../lib/types'
import { useAppDispatch } from '../app/hooks'
import { uiActions } from '../app/store'

export function SetupView() {
  const dispatch = useAppDispatch()
  const [providers, setProviders] = useState<SetupProvider[]>([])
  const [selected, setSelected] = useState<SetupProvider | null>(null)
  const [models, setModels] = useState<SetupModel[]>([])
  const [apiKey, setApiKey] = useState('')
  const [model, setModel] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState('')

  useEffect(() => {
    api.setupProviders().then(setProviders).catch(() => {})
  }, [])

  useEffect(() => {
    if (!selected) return
    setModels([])
    setModel('')
    api.setupProviderModels(selected.id).then(setModels).catch(() => {})
  }, [selected])

  async function complete(e: React.FormEvent) {
    e.preventDefault()
    if (!selected) return
    setSubmitting(true)
    setError('')
    try {
      await api.setupComplete({ provider: selected.id, api_key: apiKey, model: model || undefined })
      dispatch(uiActions.setNeedsSetup(false))
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <div className="flex h-screen items-center justify-center bg-[var(--color-background)]">
      <form
        onSubmit={complete}
        className="w-full max-w-md rounded-[var(--radius-xl)] border border-[var(--color-border)] bg-[var(--color-surface)] p-6 shadow-[var(--shadow-md)]"
      >
        <h1 className="text-lg font-semibold">Welcome to jcode</h1>
        <p className="mt-1 text-sm text-[var(--color-muted-foreground)]">
          Pick a provider and enter an API key to get started.
        </p>

        <label className="mt-4 block text-xs font-medium text-[var(--color-muted-foreground)]">Provider</label>
        <select
          value={selected?.id ?? ''}
          onChange={(e) => setSelected(providers.find((p) => p.id === e.target.value) ?? null)}
          className="mt-1 w-full rounded-[var(--radius-md)] border border-[var(--color-border)] bg-[var(--color-muted)] px-3 py-2 text-sm"
        >
          <option value="">Select…</option>
          {providers.map((p) => (
            <option key={p.id} value={p.id}>
              {p.name}
            </option>
          ))}
        </select>

        <label className="mt-3 block text-xs font-medium text-[var(--color-muted-foreground)]">API key</label>
        <input
          type="password"
          value={apiKey}
          onChange={(e) => setApiKey(e.target.value)}
          className="mt-1 w-full rounded-[var(--radius-md)] border border-[var(--color-border)] bg-[var(--color-muted)] px-3 py-2 text-sm outline-none focus:border-[var(--color-primary)]"
        />

        {models.length > 0 && (
          <>
            <label className="mt-3 block text-xs font-medium text-[var(--color-muted-foreground)]">Model</label>
            <select
              value={model}
              onChange={(e) => setModel(e.target.value)}
              className="mt-1 w-full rounded-[var(--radius-md)] border border-[var(--color-border)] bg-[var(--color-muted)] px-3 py-2 text-sm"
            >
              <option value="">Default</option>
              {models.map((m) => (
                <option key={m.id} value={m.id}>
                  {m.name}
                </option>
              ))}
            </select>
          </>
        )}

        {error && <div className="mt-2 text-xs text-[var(--color-error-fg)]">{error}</div>}
        <button
          type="submit"
          disabled={submitting || !selected || !apiKey}
          className="mt-4 w-full rounded-[var(--radius-md)] bg-[var(--color-primary)] px-3 py-2 text-sm font-medium text-[var(--color-on-primary)] disabled:opacity-50"
        >
          {submitting ? 'Setting up…' : 'Complete setup'}
        </button>
      </form>
    </div>
  )
}
