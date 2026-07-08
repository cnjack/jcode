/**
 * Resolves the absolute base URL of the jcode backend API.
 * Ported from web/src/composables/apiBase.ts — the dual-host contract:
 *
 *   • Browser mode (`jcode web`): page served by the Go server; origin IS the
 *     API origin. Base = '' (relative URLs).
 *   • Desktop mode (Tauri): page served cross-origin by Tauri; base =
 *     'http://127.0.0.1:<dynamic port>' resolved via the get_sidecar_port IPC.
 *
 * initApiBase() is awaited in main.tsx before the app mounts.
 */

import { isTauri } from './useDesktop'

/** HTTP base, e.g. '' (browser) or 'http://127.0.0.1:53913' (desktop). No trailing slash. */
export let apiBase = ''

/** WebSocket base derived from apiBase, e.g. '' or 'ws://127.0.0.1:53913'. */
export function wsBase(): string {
  if (!apiBase) {
    const proto = location.protocol === 'https:' ? 'wss:' : 'ws:'
    return `${proto}//${location.host}`
  }
  const wsProto = apiBase.startsWith('https://') ? 'wss://' : 'ws://'
  return wsProto + apiBase.replace(/^https?:\/\//, '')
}

/**
 * Resolve and cache the API base, then wait until the sidecar is actually
 * serving. Browser mode is a no-op. Desktop mode polls get_sidecar_port then
 * /api/health. Must complete before the app mounts and issues its first request.
 */
export async function initApiBase(): Promise<void> {
  if (!isTauri) return
  const { invoke } = await import('@tauri-apps/api/core')
  const port = await waitForPort(invoke)
  apiBase = `http://127.0.0.1:${port}`
  await waitForHealth()
}

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
      // IPC not ready yet
    }
    await new Promise((r) => setTimeout(r, intervalMs))
  }
  throw new Error('Timed out waiting for the jcode backend port')
}

async function waitForHealth(attempts = 400, intervalMs = 150): Promise<void> {
  for (let i = 0; i < attempts; i++) {
    try {
      const resp = await fetch(`${apiBase}/api/health`)
      if (resp.ok) {
        const body = await resp.json().catch(() => null)
        if (body && body.status) return
      }
    } catch {
      // ECONNREFUSED while binding
    }
    await new Promise((r) => setTimeout(r, intervalMs))
  }
  throw new Error('Timed out waiting for the jcode backend to become healthy')
}
