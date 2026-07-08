/** AuthGate — login form shown when the server requires a token (non-loopback bind). */

import { useState } from 'react'
import { api } from '../lib/api'
import { setAuthToken, clearAuthToken } from '../lib/authToken'
import { useAppDispatch } from '../app/hooks'
import { uiActions } from '../app/store'

export function AuthGate() {
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
        dispatch(uiActions.setNeedsAuth(false))
      } else {
        setError('Invalid token')
      }
    } catch {
      setError('Invalid token')
    } finally {
      setChecking(false)
    }
  }

  return (
    <div className="flex h-screen items-center justify-center bg-[var(--color-background)]">
      <form
        onSubmit={submit}
        className="w-full max-w-sm rounded-[var(--radius-xl)] border border-[var(--color-border)] bg-[var(--color-surface)] p-6 shadow-[var(--shadow-md)]"
      >
        <h1 className="text-lg font-semibold">jcode</h1>
        <p className="mt-1 text-sm text-[var(--color-muted-foreground)]">
          This server requires an access token. Enter it below.
        </p>
        <input
          type="password"
          value={token}
          onChange={(e) => setToken(e.target.value)}
          placeholder="Access token"
          autoFocus
          className="mt-4 w-full rounded-[var(--radius-md)] border border-[var(--color-border)] bg-[var(--color-muted)] px-3 py-2 text-sm outline-none focus:border-[var(--color-primary)]"
        />
        {error && <div className="mt-2 text-xs text-[var(--color-error-fg)]">{error}</div>}
        <button
          type="submit"
          disabled={checking || !token}
          className="mt-4 w-full rounded-[var(--radius-md)] bg-[var(--color-primary)] px-3 py-2 text-sm font-medium text-[var(--color-on-primary)] disabled:opacity-50"
        >
          {checking ? 'Checking…' : 'Unlock'}
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
