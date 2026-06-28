// Resolves the absolute base URL of the jcode backend API, and exposes it to
// api.ts / ws.ts / TerminalInstance.vue.
//
// Two hosting modes share one frontend bundle:
//
//   • Browser mode (`jcode web`): the page is served by the Go server itself,
//     so the page's origin IS the API origin. All request/WebSocket URLs stay
//     relative (base = '').
//
//   • Desktop mode (Tauri shell): the page is served by Tauri's built-in
//     frontend (origin `tauri://localhost` / `http://tauri.localhost`), which is
//     cross-origin to the Go server on `127.0.0.1:<dynamic port>`. The port is
//     published to the frontend via the `get_sidecar_port` Tauri IPC command,
//     and every request / WS must use an absolute `http(s)://127.0.0.1:<port>`
//     (or `ws(s)://…`) URL.
//
// `initApiBase()` is awaited in main.ts before the app mounts, so by the time
// any component issues a request the base is known. In desktop mode it polls the
// port until the sidecar has published it (the window can render before the Go
// server has bound its port).

import { isTauri } from './useDesktop'

/** HTTP base, e.g. '' (browser) or 'http://127.0.0.1:53913' (desktop). No trailing slash. */
export let apiBase = ''

/** WebSocket base derived from apiBase, e.g. '' or 'ws://127.0.0.1:53913'. */
export function wsBase(): string {
  if (!apiBase) {
    // Browser mode: keep using the page's own protocol/host (same-origin).
    const proto = location.protocol === 'https:' ? 'wss:' : 'ws:'
    return `${proto}//${location.host}`
  }
  // Desktop mode: swap the http(s):// scheme to ws(s):// on the resolved host.
  const wsProto = apiBase.startsWith('https://') ? 'wss://' : 'ws://'
  return wsProto + apiBase.replace(/^https?:\/\//, '')
}

/**
 * Resolve and cache the API base, then wait until the sidecar is actually
 * serving. In browser mode this is a no-op (the server that served the page is
 * already up). In desktop mode it invokes `get_sidecar_port`, polls the port
 * until the Rust side has published it, then polls `/api/health` until the Go
 * server is accepting connections. This must complete before the app mounts and
 * issues its first request — otherwise boot() races a not-yet-listening server
 * and shows the "can't connect" overlay even though the sidecar comes up a
 * moment later. Safe to call once at boot; later calls are no-ops once a
 * non-null port + healthy server have been resolved.
 */
export async function initApiBase(): Promise<void> {
  if (!isTauri) return // browser mode: relative URLs, server already up
  // Dynamic import: the @tauri-apps/api/core package is only ever pulled in on
  // the desktop, keeping the browser bundle free of Tauri runtime code.
  const { invoke } = await import('@tauri-apps/api/core')
  const port = await waitForPort(invoke)
  apiBase = `http://127.0.0.1:${port}`
  await waitForHealth()
}

// Poll the IPC command until it returns a port (or time out). The window can
// mount and reach this code before sidecar::start has stored the port, so a
// single invoke returning null must retry rather than fail.
async function waitForPort(
  invoke: <T>(cmd: string) => Promise<T>,
  attempts = 200,
  intervalMs = 100,
): Promise<number> {
  for (let i = 0; i < attempts; i++) {
    try {
      const port = await invoke<number | null>('get_sidecar_port')
      if (port) return port
    } catch {
      // IPC not ready yet (very early boot) — keep polling.
    }
    await new Promise((r) => setTimeout(r, intervalMs))
  }
  throw new Error('Timed out waiting for the jcode backend port')
}

// Poll /api/health until the Go server is accepting connections. The port is
// known before the server binds (pick_free_port races the sidecar startup), so
// resolving the port alone is not enough — without this, boot()'s first
// fetchHealth hits a not-yet-listening socket and the "can't connect" overlay
// flashes on every desktop launch. Matches the Rust health_ok gate.
async function waitForHealth(attempts = 400, intervalMs = 150): Promise<void> {
  for (let i = 0; i < attempts; i++) {
    try {
      const resp = await fetch(`${apiBase}/api/health`)
      if (resp.ok) {
        const body = await resp.json().catch(() => null)
        if (body && body.status) return
      }
    } catch {
      // ECONNREFUSED while the server is still binding — keep polling.
    }
    await new Promise((r) => setTimeout(r, intervalMs))
  }
  throw new Error('Timed out waiting for the jcode backend to become healthy')
}
