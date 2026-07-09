/**
 * Auth token storage — ported from web/src/composables/authToken.ts.
 * The token is stored in localStorage under 'jcode_web_token' (same key as the
 * Vue app, so tokens survive a Vue→React switch) and sent as a Bearer header on
 * REST + a second WS subprotocol (browsers can't set WS headers).
 *
 * Kept OUTSIDE the Redux store on purpose: api.ts and ws.ts must read the token
 * without importing the store (circular import risk). The token is plain
 * localStorage; the App registers a 401-expiry handler at mount.
 */

const KEY = 'jcode_web_token'

export function getAuthToken(): string {
  try {
    return localStorage.getItem(KEY) ?? ''
  } catch {
    return ''
  }
}

export function setAuthToken(token: string): void {
  try {
    if (token) localStorage.setItem(KEY, token)
    else localStorage.removeItem(KEY)
  } catch {
    // storage unavailable (private mode) — no-op
  }
}

export function clearAuthToken(): void {
  setAuthToken('')
}

// --- expiry notification ---------------------------------------------------
// api.ts can't import the App component, so on a 401 it calls notifyAuthExpired()
// and App registers a handler at mount (clears the token + shows the login gate).
type AuthExpiredHandler = () => void
let onExpired: AuthExpiredHandler | null = null

export function setAuthExpiredHandler(fn: AuthExpiredHandler | null): void {
  onExpired = fn
}

export function notifyAuthExpired(): void {
  onExpired?.()
}
