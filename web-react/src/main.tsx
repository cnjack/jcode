/**
 * App bootstrap.
 *
 * Mirrors web/src/main.ts: resolve the API base (awaited before mount so the
 * dual-host contract holds — browser mode is instant, desktop mode polls the
 * sidecar port + health), then render. The Provider wires Redux; the App wires
 * the WS bridge + jcode-ui RuntimeProvider.
 */

import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { Provider } from 'react-redux'
import { store } from './app/store'
import { initApiBase } from './lib/apiBase'
import { setAuthExpiredHandler } from './lib/authToken'
import { uiActions } from './app/store'
import App from './App'
import './styles.css'

async function bootstrap() {
  // Dual-host: resolve the API base + wait for the sidecar to be healthy before
  // mounting. In browser mode this is a no-op.
  try {
    await initApiBase()
  } catch (err) {
    store.dispatch(uiActions.setConnectionError(err instanceof Error ? err.message : String(err)))
  }

  // Register the 401-expiry handler: a failed auth clears the token + shows the
  // login gate. (Matches the Vue App.vue wiring.)
  setAuthExpiredHandler(() => {
    store.dispatch(uiActions.setNeedsAuth(true))
  })

  createRoot(document.getElementById('root')!).render(
    <StrictMode>
      <Provider store={store}>
        <App />
      </Provider>
    </StrictMode>,
  )
}

void bootstrap()
