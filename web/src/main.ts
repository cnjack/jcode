import { createApp } from 'vue'
import { createPinia } from 'pinia'

import App from './App.vue'
import './style.css'
import { initDesktop } from './composables/useDesktop'

// Tag <html> for the native desktop shell (no-op in a plain browser).
initDesktop()

const app = createApp(App)
app.use(createPinia())
app.mount('#app')
