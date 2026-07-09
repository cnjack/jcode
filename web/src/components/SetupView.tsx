/**
 * SetupView — first-run provider configuration.
 *
 * Lists registry providers, supports OpenAI-compatible custom endpoints, lets
 * users test the connection, then calls /api/setup/complete.
 */

import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api } from '../lib/api'
import { normalizeMode, type SetupProvider, type SetupModel } from '../lib/types'
import { useAppDispatch } from '../app/hooks'
import { chatActions, loadWorkspaceState, modelActions, sessionActions, uiActions } from '../app/store'

export function SetupView() {
  const { t } = useTranslation()
  const dispatch = useAppDispatch()
  const [providers, setProviders] = useState<SetupProvider[]>([])
  const [selected, setSelected] = useState<SetupProvider | null>(null)
  const [custom, setCustom] = useState(false)
  const [customId, setCustomId] = useState('')
  const [customName, setCustomName] = useState('')
  const [baseUrl, setBaseUrl] = useState('')
  const [customModel, setCustomModel] = useState('')
  const [customReasoning, setCustomReasoning] = useState(false)
  const [headersText, setHeadersText] = useState('')
  const [models, setModels] = useState<SetupModel[]>([])
  const [apiKey, setApiKey] = useState('')
  const [model, setModel] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [validating, setValidating] = useState(false)
  const [validation, setValidation] = useState<{ valid: boolean; error?: string } | null>(null)
  const [error, setError] = useState('')

  useEffect(() => {
    api.setupProviders().then(setProviders).catch(() => {})
  }, [])

  useEffect(() => {
    if (!selected || custom) return
    setModels([])
    setModel('')
    api.setupProviderModels(selected.id).then(setModels).catch(() => {})
  }, [selected, custom])

  useEffect(() => {
    setValidation(null)
  }, [apiKey, selected, custom, baseUrl, customId, headersText])

  function parseHeaders(): Record<string, string> | undefined {
    const raw = headersText.trim()
    if (!raw) return undefined
    const parsed = JSON.parse(raw) as unknown
    if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) throw new Error('Headers must be a JSON object.')
    const out: Record<string, string> = {}
    for (const [key, value] of Object.entries(parsed)) out[key] = String(value)
    return out
  }

  function providerId(): string {
    return custom ? customId.trim() : selected?.id || ''
  }

  function validateInputs(): boolean {
    if (!apiKey.trim()) {
      setError(t('setup.apiKeyRequired'))
      return false
    }
    if (custom) {
      if (!customId.trim()) {
        setError(t('setup.customIdRequired'))
        return false
      }
      if (!baseUrl.trim()) {
        setError(t('setup.customUrlRequired'))
        return false
      }
      if (!customModel.trim()) {
        setError(t('setup.customModelRequired'))
        return false
      }
      try {
        parseHeaders()
      } catch (e) {
        setError(e instanceof Error ? e.message : String(e))
        return false
      }
    } else if (!selected) {
      setError(t('setup.chooseProvider'))
      return false
    }
    return true
  }

  async function testConnection() {
    setError('')
    setValidation(null)
    if (!validateInputs()) return
    setValidating(true)
    try {
      const result = await api.setupValidate({
        provider: custom ? 'openai-compatible' : providerId(),
        api_key: apiKey.trim(),
        base_url: baseUrl.trim() || undefined,
        headers: parseHeaders(),
      })
      setValidation(result)
    } catch (err) {
      setValidation({ valid: false, error: err instanceof Error ? err.message : String(err) })
    } finally {
      setValidating(false)
    }
  }

  async function complete(e: React.FormEvent) {
    e.preventDefault()
    setError('')
    if (!validateInputs()) return
    setSubmitting(true)
    try {
      await api.setupComplete({
        provider: providerId(),
        api_key: apiKey.trim(),
        model: custom ? customModel.trim() : model || undefined,
        model_reasoning: custom ? customReasoning : undefined,
        base_url: baseUrl.trim() || undefined,
        name: custom ? customName.trim() || customId.trim() : undefined,
        headers: parseHeaders(),
      })
      const h = await api.health()
      dispatch(modelActions.setProvider(h.provider))
      dispatch(modelActions.setModel(h.model))
      dispatch(modelActions.setMode(normalizeMode(h.mode)))
      dispatch(modelActions.setServerVersion(h.version))
      dispatch(modelActions.setImageSupport(!!h.image_support))
      dispatch(sessionActions.setProjectPath(h.pwd))
      dispatch(sessionActions.setCurrentSession(h.session_id || ''))
      dispatch(chatActions.setRunning(!!h.running))
      await dispatch(loadWorkspaceState())
      dispatch(uiActions.setNeedsSetup(false))
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <div className="setup-frame relative flex h-screen items-center justify-center bg-[var(--color-background)]">
      <div className="titlebar-drag" data-tauri-drag-region aria-hidden="true" />
      <form
        onSubmit={complete}
        className="w-full max-w-md rounded-[var(--radius-xl)] border border-[var(--color-border)] bg-[var(--color-surface)] p-6 shadow-[var(--shadow-md)]"
      >
        <h1 className="text-lg font-semibold">Welcome to jcode</h1>
        <p className="mt-1 text-sm text-[var(--color-muted-foreground)]">
          {t('setup.selectProviderDesc')}
        </p>

        <label className="mt-4 block text-xs font-medium text-[var(--color-muted-foreground)]">{t('setup.chooseProvider')}</label>
        <select
          value={custom ? '__custom__' : selected?.id ?? ''}
          onChange={(e) => {
            const value = e.target.value
            setCustom(value === '__custom__')
            setSelected(providers.find((p) => p.id === value) ?? null)
            setModels([])
            setModel('')
          }}
          className="mt-1 w-full rounded-[var(--radius-md)] border border-[var(--color-border)] bg-[var(--color-muted)] px-3 py-2 text-sm"
        >
          <option value="">Select…</option>
          {providers.map((p) => (
            <option key={p.id} value={p.id}>
              {p.name}
            </option>
          ))}
          <option value="__custom__">{t('setup.customProvider')}</option>
        </select>

        {custom && (
          <div className="mt-3 grid grid-cols-2 gap-2">
            <label className="block text-xs font-medium text-[var(--color-muted-foreground)]">
              {t('setup.customId')}
              <input value={customId} onChange={(e) => setCustomId(e.target.value)} placeholder={t('setup.customIdPlaceholder')} className="mt-1 w-full rounded-[var(--radius-md)] border border-[var(--color-border)] bg-[var(--color-muted)] px-3 py-2 text-sm text-[var(--color-foreground)] outline-none focus:border-[var(--color-primary)]" />
            </label>
            <label className="block text-xs font-medium text-[var(--color-muted-foreground)]">
              {t('setup.customName')}
              <input value={customName} onChange={(e) => setCustomName(e.target.value)} placeholder={t('setup.customNamePlaceholder')} className="mt-1 w-full rounded-[var(--radius-md)] border border-[var(--color-border)] bg-[var(--color-muted)] px-3 py-2 text-sm text-[var(--color-foreground)] outline-none focus:border-[var(--color-primary)]" />
            </label>
          </div>
        )}

        <label className="mt-3 block text-xs font-medium text-[var(--color-muted-foreground)]">{t('setup.apiKey')}</label>
        <input
          type="password"
          value={apiKey}
          onChange={(e) => setApiKey(e.target.value)}
          placeholder={t('setup.apiKeyPlaceholder')}
          className="mt-1 w-full rounded-[var(--radius-md)] border border-[var(--color-border)] bg-[var(--color-muted)] px-3 py-2 text-sm outline-none focus:border-[var(--color-primary)]"
        />

        {custom && (
          <>
            <label className="mt-3 block text-xs font-medium text-[var(--color-muted-foreground)]">{t('setup.baseUrl')}</label>
            <input value={baseUrl} onChange={(e) => setBaseUrl(e.target.value)} placeholder="https://api.example.com/v1" className="mt-1 w-full rounded-[var(--radius-md)] border border-[var(--color-border)] bg-[var(--color-muted)] px-3 py-2 text-sm text-[var(--color-foreground)] outline-none focus:border-[var(--color-primary)]" />
            <div className="mt-3 grid grid-cols-[1fr_auto] items-end gap-2">
              <label className="block text-xs font-medium text-[var(--color-muted-foreground)]">
                {t('setup.customModelId')}
                <input value={customModel} onChange={(e) => setCustomModel(e.target.value)} placeholder={t('setup.customModelPlaceholder')} className="mt-1 w-full rounded-[var(--radius-md)] border border-[var(--color-border)] bg-[var(--color-muted)] px-3 py-2 text-sm text-[var(--color-foreground)] outline-none focus:border-[var(--color-primary)]" />
              </label>
              <label className="inline-flex h-9 items-center gap-2 rounded-[var(--radius-md)] border border-[var(--color-border)] px-2 text-xs text-[var(--color-foreground)]">
                <input type="checkbox" checked={customReasoning} onChange={(e) => setCustomReasoning(e.target.checked)} />
                {t('setup.customReasoning')}
              </label>
            </div>
            <label className="mt-3 block text-xs font-medium text-[var(--color-muted-foreground)]">
              Headers JSON
              <textarea value={headersText} onChange={(e) => setHeadersText(e.target.value)} rows={2} placeholder='{"X-Header":"value"}' className="mt-1 w-full resize-y rounded-[var(--radius-md)] border border-[var(--color-border)] bg-[var(--color-muted)] px-3 py-2 font-mono text-xs text-[var(--color-foreground)] outline-none focus:border-[var(--color-primary)]" />
            </label>
          </>
        )}

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

        {validation && (
          <div className={`mt-3 rounded-[var(--radius-md)] px-3 py-2 text-xs ${validation.valid ? 'bg-[var(--color-success-bg)] text-[var(--color-success-fg)]' : 'bg-[var(--color-error-bg)] text-[var(--color-error-fg)]'}`}>
            {validation.valid ? t('setup.connected') : validation.error || 'Validation failed'}
          </div>
        )}
        {error && <div className="mt-2 text-xs text-[var(--color-error-fg)]">{error}</div>}
        <div className="mt-4 grid grid-cols-[auto_1fr] gap-2">
          <button
            type="button"
            disabled={validating || !apiKey}
            onClick={() => void testConnection()}
            className="rounded-[var(--radius-md)] border border-[var(--color-border)] px-3 py-2 text-sm font-medium text-[var(--color-foreground)] disabled:opacity-50"
          >
            {validating ? t('setup.checking') : t('setup.testConnection')}
          </button>
          <button
            type="submit"
            disabled={submitting || !apiKey || (!custom && !selected)}
            className="rounded-[var(--radius-md)] bg-[var(--color-primary)] px-3 py-2 text-sm font-medium text-[var(--color-on-primary)] disabled:opacity-50"
          >
            {submitting ? t('setup.settingUp') : t('setup.completeSetup')}
          </button>
        </div>
      </form>
    </div>
  )
}
