import { cleanup, render, waitFor } from '@testing-library/react'
import { Provider } from 'react-redux'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { store } from '../app/store'
import { api } from '../lib/api'
import { RemoteConnectWizard } from './RemoteConnectWizard'

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
})

describe('RemoteConnectWizard backend contract', () => {
  it('focuses a newly bound workspace explicitly', async () => {
    vi.spyOn(api, 'sshList').mockResolvedValue({ current: '', aliases: [] })
    vi.spyOn(api, 'remoteConnect').mockResolvedValue({
      connection_id: 'connection-new',
      remote_pwd: '/workspace',
      platform: 'linux',
    })
    const bind = vi.spyOn(api, 'remoteBind').mockImplementation(() => new Promise(() => {}))

    render(
      <Provider store={store}>
        <RemoteConnectWizard
          open
          prefill={{
            kind: 'ssh',
            host: 'example.com',
            port: 22,
            user: 'dev',
            remotePath: '/workspace',
          }}
          onClose={() => {}}
        />
      </Provider>,
    )

    await waitFor(() => expect(bind).toHaveBeenCalledWith(
      'connection-new',
      '/workspace',
      { focus: true },
    ))
  })
})
