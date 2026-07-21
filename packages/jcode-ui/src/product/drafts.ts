/**
 * Composer drafts — remembers unsent input text per conversation.
 *
 * Keyed by session id in localStorage so drafts survive conversation switches,
 * composer remounts (welcome ↔ conversation layout), and full app restarts
 * (desktop). All access is try/catch-guarded: localStorage may be unavailable
 * in hardened webviews. The key prefix matches the historical jcode web app so
 * existing drafts carry over.
 */

const KEY_PREFIX = 'jcode-composer-draft:'

/** readDraft returns the saved draft for a session ('' when none). */
export function readDraft(sessionId: string): string {
  if (!sessionId) return ''
  try {
    return localStorage.getItem(KEY_PREFIX + sessionId) ?? ''
  } catch {
    return ''
  }
}

/** writeDraft saves a draft; empty text removes the entry. */
export function writeDraft(sessionId: string, text: string): void {
  if (!sessionId) return
  try {
    if (text) {
      localStorage.setItem(KEY_PREFIX + sessionId, text)
    } else {
      localStorage.removeItem(KEY_PREFIX + sessionId)
    }
  } catch {
    // storage unavailable — drafts are best-effort
  }
}
