// Notifications for long-running agent events (task finished, approval needed).
// Fires only when the user isn't already looking at the app, so a run is never
// interrupted while it's being watched.
//
// In the Tauri desktop shell this routes through the native OS notification
// plugin; in a plain browser it falls back to the web Notification API. Both
// paths are feature-detected, so the same bundle works in either host.
import { ref } from 'vue'

import { isTauri, isAppFocused, ensureNativePermission, nativeNotify } from './useDesktop'

const permission = ref<NotificationPermission>(
  typeof Notification !== 'undefined' ? Notification.permission : 'denied',
)

export function useNotifications() {
  const supported = isTauri || typeof Notification !== 'undefined'

  async function ensurePermission() {
    if (isTauri) {
      await ensureNativePermission()
      return
    }
    if (typeof Notification === 'undefined' || permission.value !== 'default') return
    try {
      permission.value = await Notification.requestPermission()
    } catch {
      /* ignore */
    }
  }

  function notify(title: string, body?: string) {
    // Desktop: native OS notification, only when the window isn't focused.
    if (isTauri) {
      if (isAppFocused()) return
      void nativeNotify(title, body)
      return
    }
    // Web: only notify when the tab is in the background.
    if (typeof Notification === 'undefined' || permission.value !== 'granted') return
    if (typeof document !== 'undefined' && !document.hidden) return
    try {
      const n = new Notification(title, { body, icon: '/icon.svg', tag: 'jcode' })
      n.onclick = () => {
        window.focus()
        n.close()
      }
    } catch {
      /* ignore */
    }
  }

  return { supported, permission, ensurePermission, notify }
}
