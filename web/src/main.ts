import { createApp } from 'vue'
import { createPinia } from 'pinia'

import App from './App.vue'
import './style.css'
import { initDesktop } from './composables/useDesktop'
import { initApiBase } from './composables/apiBase'
import { i18n } from './i18n'

// Tag <html> for the native desktop shell (no-op in a plain browser).
initDesktop()

// Resolve the backend API base before mounting. In browser mode (`jcode web`)
// this is a no-op (relative URLs). In the Tauri desktop shell the page is
// cross-origin to the Go server, so this awaits the sidecar port via the
// `get_sidecar_port` IPC command; every request/WS then uses an absolute URL.
// Awaited up front so no component issues a request against the wrong origin.
// The Tauri-built page (splash/app shell) renders immediately while this
// resolves, so there is no blank-screen penalty.
try {
  await initApiBase()
} catch (err) {
  console.error('[jcode] failed to resolve backend port:', err)
}

const app = createApp(App)
app.use(createPinia())
app.use(i18n)
app.mount('#app')
