/**
 * Remote workspace helpers — parse labels and open the connect wizard.
 * Mirrors web/src/stores/project.ts parseRemoteLabel / isRemotePath.
 */

import type { RemoteMeta } from './types'

export type RemotePrefill = RemoteMeta & { loadTaskUuid?: string }

export function isRemotePath(path: string): boolean {
  return path.startsWith('ssh://') || path.startsWith('docker://')
}

/** Decompose ssh:// / docker:// project labels for wizard reconnect. */
export function parseRemoteLabel(label: string): RemoteMeta | null {
  if (label.startsWith('docker://')) {
    const rest = label.slice('docker://'.length)
    const slash = rest.indexOf('/')
    const container = slash < 0 ? rest : rest.slice(0, slash)
    const remotePath = slash < 0 ? '/' : rest.slice(slash)
    return { kind: 'docker', host: '', user: '', port: 0, remotePath, container }
  }
  if (!label.startsWith('ssh://')) return null
  const rest = label.slice('ssh://'.length)
  const at = rest.indexOf('@')
  if (at < 0) return null
  const user = rest.slice(0, at)
  const afterUser = rest.slice(at + 1)
  const slash = afterUser.indexOf('/')
  const hostPort = slash < 0 ? afterUser : afterUser.slice(0, slash)
  const remotePath = slash < 0 ? '/' : afterUser.slice(slash)
  const colon = hostPort.lastIndexOf(':')
  const port = colon >= 0 ? parseInt(hostPort.slice(colon + 1), 10) || 22 : 22
  return { kind: 'ssh', host: hostPort, user, port, remotePath }
}

/** Open RemoteConnectWizard (optionally prefilled for reconnect). */
export function openRemoteConnect(prefill?: RemotePrefill | null): void {
  window.dispatchEvent(
    new CustomEvent<RemotePrefill | null>('jcode:open-remote-connect', {
      detail: prefill ?? null,
    }),
  )
}
