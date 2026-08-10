import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { Provider } from 'react-redux'
import { store } from '../app/store'
import { i18n } from '../i18n'
import { api } from '../lib/api'
import { SetupView } from './SetupView'

beforeEach(async () => {
  cleanup()
  vi.restoreAllMocks()
  await i18n.changeLanguage('en')
})

function deferred<T>() {
  let resolve!: (value: T | PromiseLike<T>) => void
  const promise = new Promise<T>((resolvePromise) => {
    resolve = resolvePromise
  })
  return { promise, resolve }
}

describe('SetupView managed provider authentication', () => {
  it('completes first-run setup with an OAuth binding and no API key', async () => {
    vi.spyOn(api, 'setupProviders').mockResolvedValue([{
      id: 'openai',
      name: 'OpenAI',
      configured: false,
      auth_methods: ['codex_oauth'],
    }])
    vi.spyOn(api, 'setupProviderModels').mockResolvedValue([])
    vi.spyOn(api, 'providerAuthStatus').mockResolvedValue({
      method: 'codex_oauth',
      default_account_id: 'account-1',
      accounts: [{
        id: 'account-1',
        login: 'jack@example.com',
        authenticated_at: '2026-08-09T08:00:00Z',
        requires_reauth: false,
      }],
    })
    const complete = vi.spyOn(api, 'setupComplete').mockImplementation(() => new Promise(() => {}))

    render(
      <Provider store={store}>
        <SetupView />
      </Provider>,
    )

    const provider = await screen.findByRole('combobox')
    fireEvent.change(provider, { target: { value: 'openai' } })
    await screen.findByText('jack@example.com')

    await waitFor(() => expect(api.setupProviderModels).toHaveBeenCalledWith('openai', {
      method: 'codex_oauth',
      account_id: 'account-1',
    }))

    expect(screen.queryByLabelText('API Key')).toBeNull()
    const submit = screen.getByRole('button', { name: 'Complete Setup' })
    await waitFor(() => expect(submit.hasAttribute('disabled')).toBe(false))
    fireEvent.click(submit)

    await waitFor(() => expect(complete).toHaveBeenCalledTimes(1))
    expect(complete.mock.calls[0][0]).toMatchObject({
      provider: 'openai',
      auth_binding: { method: 'codex_oauth' },
    })
    expect(complete.mock.calls[0][0].api_key).toBeUndefined()
    expect(complete.mock.calls[0][0]).not.toHaveProperty('base_url')
    expect(complete.mock.calls[0][0]).not.toHaveProperty('headers')
  })

  it('ignores a late model response from a previously selected provider', async () => {
    const alphaModels = deferred<Array<{ id: string; name: string; tool_call: boolean; context_limit?: number }>>()
    const betaModels = deferred<Array<{ id: string; name: string; tool_call: boolean; context_limit?: number }>>()
    vi.spyOn(api, 'setupProviders').mockResolvedValue([
      { id: 'alpha', name: 'Alpha', configured: false, auth_methods: ['api_key'] },
      { id: 'beta', name: 'Beta', configured: false, auth_methods: ['api_key'] },
    ])
    vi.spyOn(api, 'setupProviderModels').mockImplementation((providerID) => (
      providerID === 'alpha' ? alphaModels.promise : betaModels.promise
    ))

    render(
      <Provider store={store}>
        <SetupView />
      </Provider>,
    )

    const provider = await screen.findByRole('combobox')
    fireEvent.change(provider, { target: { value: 'alpha' } })
    await waitFor(() => expect(api.setupProviderModels).toHaveBeenCalledWith('alpha'))
    fireEvent.change(provider, { target: { value: 'beta' } })
    await waitFor(() => expect(api.setupProviderModels).toHaveBeenCalledWith('beta'))

    await act(async () => {
      betaModels.resolve([{ id: 'beta-model', name: 'Beta Model', tool_call: true }])
      await Promise.resolve()
    })
    expect(await screen.findByRole('option', { name: 'Beta Model' })).toBeTruthy()

    await act(async () => {
      alphaModels.resolve([{ id: 'alpha-model', name: 'Alpha Model', tool_call: true }])
      await Promise.resolve()
    })
    expect(screen.queryByRole('option', { name: 'Alpha Model' })).toBeNull()
    expect(screen.getByRole('option', { name: 'Beta Model' })).toBeTruthy()
  })
})
