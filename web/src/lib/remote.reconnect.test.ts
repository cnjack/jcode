import { describe, expect, it } from 'vitest'
import { sshReconnectRequest } from './remote'

describe('SSH workspace reconnect request', () => {
  it('keeps agent/default-key authentication when confirming a host key', () => {
    const request = sshReconnectRequest({
      kind: 'ssh',
      host: 'example.com',
      port: 2222,
      user: 'dev',
      remotePath: '/workspace',
    }, 'SHA256:trusted')

    expect(request).toEqual({
      type: 'ssh',
      host: 'example.com',
      port: 2222,
      user: 'dev',
      accept_host_key: true,
      host_key_fingerprint: 'SHA256:trusted',
    })
    expect(request).not.toHaveProperty('auth_method')
    expect(request).not.toHaveProperty('key_path')
  })
})
