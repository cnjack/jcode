// Central bridge to native desktop (Tauri) capabilities.
//
// Every export here is feature-detected and degrades to a web fallback or a
// no-op, so the exact same web bundle runs unchanged in a plain browser
// (`jcode web`) and inside the Tauri desktop shell. Tauri plugin packages are
// imported dynamically and only ever reached when `isTauri` is true, so the
// browser build never executes their native-only code paths.

// Tauri 2 injects `__TAURI_INTERNALS__` into every webview it controls,
// including the remote loopback origin the desktop app navigates to.
export const isTauri =
  typeof window !== 'undefined' && '__TAURI_INTERNALS__' in window

// --- window focus tracking -------------------------------------------------
// On the desktop we only want to fire a notification when the app isn't the
// focused window (the web build keeps using document.hidden instead).
let _focused = typeof document === 'undefined' ? true : document.hasFocus()
if (typeof window !== 'undefined') {
  window.addEventListener('focus', () => (_focused = true))
  window.addEventListener('blur', () => (_focused = false))
}

/** True when the app window is currently focused / in the foreground. */
export function isAppFocused(): boolean {
  if (typeof document !== 'undefined' && document.hidden) return false
  return _focused
}

// --- native notifications --------------------------------------------------
type NotifModule = typeof import('@tauri-apps/plugin-notification')
let _notif: NotifModule | null = null
async function notifModule(): Promise<NotifModule | null> {
  if (!isTauri) return null
  if (!_notif) _notif = await import('@tauri-apps/plugin-notification')
  return _notif
}

/** Request OS notification permission (idempotent). Returns whether granted. */
export async function ensureNativePermission(): Promise<boolean> {
  const m = await notifModule()
  if (!m) return false
  try {
    let granted = await m.isPermissionGranted()
    if (!granted) granted = (await m.requestPermission()) === 'granted'
    return granted
  } catch {
    return false
  }
}

/** Fire a native OS notification. No-op (returns false) outside Tauri. */
export async function nativeNotify(title: string, body?: string): Promise<boolean> {
  const m = await notifModule()
  if (!m) return false
  try {
    // Request permission on the spot if it was never granted — otherwise the
    // first notification (e.g. a task that finishes before the user answers the
    // initial async OS prompt) would be silently dropped.
    if (!(await m.isPermissionGranted())) {
      if ((await m.requestPermission()) !== 'granted') return false
    }
    m.sendNotification({ title, body })
    return true
  } catch {
    return false
  }
}

// --- open external links in the system browser -----------------------------
/** Open a URL in the user's default browser (system browser on desktop). */
export async function openExternal(url: string): Promise<void> {
  if (isTauri) {
    try {
      const { openUrl } = await import('@tauri-apps/plugin-opener')
      await openUrl(url)
      return
    } catch {
      /* fall through to web */
    }
  }
  if (typeof window !== 'undefined') {
    window.open(url, '_blank', 'noopener,noreferrer')
  }
}

// --- native folder picker --------------------------------------------------
/**
 * Open the OS folder picker and return the chosen absolute path, or null if
 * cancelled / unavailable. Callers fall back to the in-app folder browser when
 * this returns null.
 */
export async function pickFolder(defaultPath?: string): Promise<string | null> {
  if (!isTauri) return null
  try {
    const { open } = await import('@tauri-apps/plugin-dialog')
    const res = await open({ directory: true, multiple: false, defaultPath })
    return typeof res === 'string' ? res : null
  } catch {
    return null
  }
}

// --- one-time desktop init -------------------------------------------------
/**
 * Tag the document so CSS can adapt to the native shell (e.g. inset the top bar
 * for the macOS traffic-light buttons under an overlay title bar). Safe to call
 * on every boot; a no-op in the browser.
 */
export function initDesktop(): void {
  if (!isTauri || typeof document === 'undefined') return
  const root = document.documentElement
  root.classList.add('is-tauri')
  const platform = navigator.platform || ''
  if (/Mac/i.test(platform)) root.classList.add('is-tauri-macos')
  else if (/Win/i.test(platform)) root.classList.add('is-tauri-windows')
  else root.classList.add('is-tauri-linux')
}
