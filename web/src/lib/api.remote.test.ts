import { afterEach, describe, expect, it, vi } from 'vitest'
import { api, isAPIError } from './api'

afterEach(() => {
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})

describe('remote API errors', () => {
  it('preserves structured SSH host-key evidence on a 409', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify({
      error: 'host key is unknown',
      code: 'ssh_host_key_unknown',
      host: 'example.com',
      fingerprint: 'SHA256:abc',
      key_type: 'ssh-ed25519',
    }), {
      status: 409,
      headers: { 'Content-Type': 'application/json' },
    })))

    const error = await api.remoteConnect({
      type: 'ssh', host: 'example.com', user: 'root', auth_method: 'key',
    }).catch((value: unknown) => value)

    expect(isAPIError(error)).toBe(true)
    expect(error).toMatchObject({
      status: 409,
      code: 'ssh_host_key_unknown',
      body: { fingerprint: 'SHA256:abc', key_type: 'ssh-ed25519' },
    })
  })

  it('scopes status reconciliation to the requested task', async () => {
    const fetchMock = vi.fn(async (_input: RequestInfo | URL) => new Response(JSON.stringify({
      running: true,
      ws_clients: 1,
      pwd: '/work',
      project: '/work',
      provider: 'openai',
      model: 'test',
      mode: 'approval',
    }), {
      status: 200,
      headers: { 'Content-Type': 'application/json' },
    }))
    vi.stubGlobal('fetch', fetchMock)

    await api.status('task/with space')

    expect(String(fetchMock.mock.calls[0]?.[0])).toContain('/api/status?task_id=task%2Fwith%20space')
  })
})
