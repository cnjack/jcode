/**
 * useDesktop — Tauri detection + bridge. Ported from web/src/composables/useDesktop.ts.
 * `isTauri` is computed once at module load (the global is injected by Tauri's
 * withGlobalTauri before any module runs). The Tauri APIs are dynamically imported
 * so the browser bundle stays free of Tauri runtime code.
 */

export const isTauri: boolean =
  typeof window !== 'undefined' && !!(window as unknown as { __TAURI_INTERNALS__?: unknown }).__TAURI_INTERNALS__

/**
 * Tag the root document for native desktop shell styling. The macOS Tauri
 * window uses an overlay title bar, so CSS needs an `is-tauri-macos` hook to
 * reveal draggable strips and inset content below the traffic-light controls.
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

/** Invoke a Tauri command (no-op in browser mode). */
export async function invoke<T>(cmd: string, args?: Record<string, unknown>): Promise<T> {
  if (!isTauri) throw new Error('invoke() called outside Tauri')
  const { invoke: tauriInvoke } = await import('@tauri-apps/api/core')
  return tauriInvoke<T>(cmd, args)
}

/** Open a URL in the system browser (Tauri) or a new tab (browser). */
export async function openUrl(url: string): Promise<void> {
  if (isTauri) {
    const { openUrl: tauriOpen } = await import('@tauri-apps/plugin-opener')
    await tauriOpen(url)
  } else {
    window.open(url, '_blank', 'noopener,noreferrer')
  }
}

/**
 * Route external link clicks out of the app webview.
 *
 * Chat markdown renders plain `<a href>` anchors (no target), so in the Tauri
 * desktop shell a click navigates the app window itself away from the UI, and
 * `target="_blank"` clicks are dead (new-window creation is denied). One
 * delegated capture-phase listener covers every current and future anchor:
 * external http(s) links are opened in the system browser (Tauri) or a new
 * tab (browser); same-origin and loopback (sidecar API, Bearer-auth'd) links
 * are left alone.
 */
export function initExternalLinks(): void {
  if (typeof document === 'undefined') return
  document.addEventListener(
    'click',
    (e) => {
      if (e.defaultPrevented || e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return
      const anchor = (e.target as Element | null)?.closest?.('a[href]')
      if (!anchor) return
      const href = anchor.getAttribute('href') ?? ''
      let url: URL
      try {
        url = new URL(href, window.location.href)
      } catch {
        return
      }
      if (url.protocol !== 'http:' && url.protocol !== 'https:') return
      if (url.origin === window.location.origin) return
      if (isLoopback(url.hostname)) return
      e.preventDefault()
      void openUrl(url.href)
    },
    true,
  )
}

function isLoopback(hostname: string): boolean {
  return hostname === 'localhost' || hostname === '127.0.0.1' || hostname === '[::1]' || hostname.endsWith('.localhost')
}

/** Show a native notification (Tauri) or fall back to the Web Notifications API. */
export async function notify(title: string, body?: string): Promise<void> {
  if (isTauri) {
    const { sendNotification } = await import('@tauri-apps/plugin-notification')
    sendNotification({ title, body })
  } else if ('Notification' in window) {
    if (Notification.permission === 'granted') {
      new Notification(title, { body })
    } else if (Notification.permission !== 'denied') {
      const perm = await Notification.requestPermission()
      if (perm === 'granted') new Notification(title, { body })
    }
  }
}

/**
 * Open the OS folder picker and return the chosen absolute path, or null if the
 * user cancelled. Throws when the native picker is unavailable so callers can
 * fall back to the in-app folder browser (null = cancel → do nothing).
 */
export async function pickFolder(defaultPath?: string): Promise<string | null> {
  if (!isTauri) throw new Error('native folder picker unavailable')
  const { open } = await import('@tauri-apps/plugin-dialog')
  const res = await open({ directory: true, multiple: false, defaultPath })
  return typeof res === 'string' ? res : null
}
