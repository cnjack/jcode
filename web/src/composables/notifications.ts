// Browser notifications for long-running agent events. Fires only when the tab
// is in the background (document.hidden) so we never interrupt the user while
// they are watching the run. No-op when the Notification API is unavailable or
// permission was denied.
import { ref } from 'vue'

const permission = ref<NotificationPermission>(
  typeof Notification !== 'undefined' ? Notification.permission : 'denied',
)

export function useNotifications() {
  const supported = typeof Notification !== 'undefined'

  async function ensurePermission() {
    if (!supported || permission.value !== 'default') return
    try {
      permission.value = await Notification.requestPermission()
    } catch {
      /* ignore */
    }
  }

  function notify(title: string, body?: string) {
    if (!supported || permission.value !== 'granted') return
    // Only notify when the user isn't already looking at the page.
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
