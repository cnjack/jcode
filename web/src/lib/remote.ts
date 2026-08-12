/**
 * Remote workspace helpers — parse labels and open the connect wizard.
 * Mirrors web/src/stores/project.ts parseRemoteLabel / isRemotePath.
 */

import type { RemoteConnectRequest, RemoteMeta } from './types'

export type RemotePrefill = RemoteMeta

/** Build the credential-free reconnect request used for an existing SSH
 * workspace. Omitting auth fields preserves the backend's agent/default-key
 * fallback, including when retrying a host-key confirmation. */
export function sshReconnectRequest(
  prefill: RemotePrefill,
  confirmedFingerprint?: string,
): RemoteConnectRequest {
  return {
    type: 'ssh',
    host: prefill.host.trim(),
    port: prefill.port || 22,
    user: prefill.user.trim() || 'root',
    accept_host_key: confirmedFingerprint ? true : undefined,
    host_key_fingerprint: confirmedFingerprint,
  }
}

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
  let host = hostPort
  let port = 22
  if (hostPort.startsWith('[')) {
    const bracket = hostPort.indexOf(']')
    if (bracket > 0) {
      host = hostPort.slice(1, bracket)
      if (hostPort[bracket + 1] === ':') port = parseInt(hostPort.slice(bracket + 2), 10) || 22
    }
  } else {
    const colon = hostPort.lastIndexOf(':')
    // A single colon with a numeric suffix is host:port. Multiple colons are
    // an unbracketed IPv6 literal and must remain intact.
    if (colon > 0 && hostPort.indexOf(':') === colon && /^\d+$/.test(hostPort.slice(colon + 1))) {
      host = hostPort.slice(0, colon)
      port = parseInt(hostPort.slice(colon + 1), 10) || 22
    }
  }
  return { kind: 'ssh', host, user, port, remotePath }
}

/** Open RemoteConnectWizard (optionally prefilled for reconnect). */
export function openRemoteConnect(prefill?: RemotePrefill | null): void {
  window.dispatchEvent(
    new CustomEvent<RemotePrefill | null>('jcode:open-remote-connect', {
      detail: prefill ?? null,
    }),
  )
}
