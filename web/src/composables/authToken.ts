// Web access token storage + accessors.
//
// Kept OUTSIDE the Pinia store on purpose: api.ts and ws.ts must read the token
// without importing a store, which would create a circular import (api ↔ store)
// and may run before createPinia(). The token is a plain module-level reactive
// ref, persisted to localStorage so it survives reloads.
//
// Only relevant when the server is bound to a non-loopback host (it reports
// `auth_required` from /api/health). On loopback / desktop the token stays empty
// and nothing here has any effect.
import { ref } from 'vue'

const STORAGE_KEY = 'jcode_web_token'

const token = ref<string>(localStorage.getItem(STORAGE_KEY) || '')

/** Current token (empty string when none). Read fresh on every request/WS connect. */
export function getAuthToken(): string {
  return token.value
}

/** Persist (or clear, when empty) the token. */
export function setAuthToken(t: string): void {
  token.value = t
  if (t) localStorage.setItem(STORAGE_KEY, t)
  else localStorage.removeItem(STORAGE_KEY)
}

export function clearAuthToken(): void {
  setAuthToken('')
}

/** Reactive ref for components (login gate) that need to watch the token. */
export function useAuthToken() {
  return token
}

// --- expiry notification ---------------------------------------------------
// api.ts cannot import the App component, so on a 401 it calls notifyAuthExpired()
// and App registers a handler at mount (clears the token + shows the login gate).
type AuthExpiredHandler = () => void
let onExpired: AuthExpiredHandler | null = null

export function setAuthExpiredHandler(fn: AuthExpiredHandler | null): void {
  onExpired = fn
}

export function notifyAuthExpired(): void {
  onExpired?.()
}

// NOTE: desktop (Tauri) token injection hook — when the desktop sidecar later
// runs on a non-loopback bind, initApiBase() can `invoke('get_sidecar_token')`
// and call setAuthToken() here; api.ts/ws.ts need no further change.
