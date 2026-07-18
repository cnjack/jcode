/** AuthGate — login form shown when the server requires a token (non-loopback bind). */

import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api } from '../lib/api'
import { normalizeMode } from '../lib/types'
import { setAuthToken, clearAuthToken } from '../lib/authToken'
import { useAppDispatch } from '../app/hooks'
import { chatActions, loadWorkspaceState, modelActions, sessionActions, uiActions } from '../app/store'

export function AuthGate() {
  const { t } = useTranslation()
  const dispatch = useAppDispatch()
  const [token, setToken] = useState('')
  const [error, setError] = useState('')
  const [checking, setChecking] = useState(false)

  async function submit(e: React.FormEvent) {
    e.preventDefault()
    setChecking(true)
    setError('')
    try {
      const resp = await api.authVerify(token)
      if (resp.ok) {
        setAuthToken(token)
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
        dispatch(uiActions.setNeedsAuth(false))
      } else {
        setError(t('auth.invalid'))
      }
    } catch {
      setError(t('auth.invalid'))
    } finally {
      setChecking(false)
    }
  }

  return (
    <div className="setup-frame relative flex h-screen items-center justify-center bg-[var(--color-background)]">
      <div className="titlebar-drag" data-tauri-drag-region aria-hidden="true" />
      <form
        onSubmit={submit}
        className="w-full max-w-sm rounded-[var(--radius-xl)] border border-[var(--color-border)] bg-[var(--color-surface)] p-6 shadow-[var(--shadow-md)]"
      >
        <h1 className="text-lg font-semibold">jcode</h1>
        <p className="mt-1 text-sm text-[var(--color-muted-foreground)]">
          {t('auth.body')}
        </p>
        <input
          type="password"
          value={token}
          onChange={(e) => setToken(e.target.value)}
          placeholder={t('auth.placeholder')}
          autoFocus
          className="mt-4 w-full rounded-[var(--radius-md)] border border-[var(--color-border)] bg-[var(--color-muted)] px-3 py-2 text-sm outline-none focus:border-[var(--color-primary)]"
        />
        {error && <div className="mt-2 text-xs text-[var(--color-error-fg)]">{error}</div>}
        <button
          type="submit"
          disabled={checking || !token}
          className="mt-4 w-full rounded-[var(--radius-md)] bg-[var(--color-primary)] px-3 py-2 text-sm font-medium text-[var(--color-on-primary)] disabled:opacity-50"
        >
          {checking ? t('auth.verifying') : t('auth.submit')}
        </button>
      </form>
    </div>
  )
}

/** Allow clearing the token + re-gating from elsewhere (settings). */
export function signOut(dispatch: ReturnType<typeof useAppDispatch>) {
  clearAuthToken()
  dispatch(uiActions.setNeedsAuth(true))
}
