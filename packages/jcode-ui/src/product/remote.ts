/**
 * Remote workspace helpers — pure label parsing shared by the workspace picker.
 */

import type { RemoteMeta } from './types.js'

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

/** Display name for a workspace path (local basename or remote label). */
export function workspaceName(path: string): string {
  if (!path) return ''
  if (path.startsWith('docker://')) {
    const rest = path.slice('docker://'.length)
    return rest.split('/')[0] || path
  }
  if (path.startsWith('ssh://')) {
    const rest = path.slice('ssh://'.length)
    const slash = rest.indexOf('/')
    const host = slash >= 0 ? rest.slice(0, slash) : rest
    const tail = slash >= 0 ? rest.slice(slash).split('/').filter(Boolean).at(-1) : ''
    return tail ? `${tail} (${host})` : host
  }
  const parts = path.split('/').filter(Boolean)
  return parts.at(-1) || path
}
