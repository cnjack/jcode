/**
 * useDesktop — Tauri detection + bridge. Ported from web/src/composables/useDesktop.ts.
 * `isTauri` is computed once at module load (the global is injected by Tauri's
 * withGlobalTauri before any module runs). The Tauri APIs are dynamically imported
 * so the browser bundle stays free of Tauri runtime code.
 */

export const isTauri: boolean =
  typeof window !== 'undefined' && !!(window as unknown as { __TAURI_INTERNALS__?: unknown }).__TAURI_INTERNALS__

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
