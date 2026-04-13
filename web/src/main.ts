import { createApp } from 'vue'
import { createPinia } from 'pinia'

import App from './App.vue'
import './style.css'
import 'highlight.js/styles/github.css'

const app = createApp(App)
app.use(createPinia())
app.mount('#app')
