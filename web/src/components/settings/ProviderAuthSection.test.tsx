import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { i18n } from '../../i18n'
import type { ProviderAuthStatus } from '../../lib/types'
import {
  ProviderAuthSection,
  isProviderAuthReady,
  resolveProviderAuthAccount,
} from './ProviderAuthSection'

const mocks = vi.hoisted(() => ({
  openUrl: vi.fn<() => Promise<void>>(),
  status: vi.fn(),
  start: vi.fn(),
  poll: vi.fn(),
  cancel: vi.fn(),
  setDefault: vi.fn(),
  removeAccount: vi.fn(),
  logout: vi.fn(),
}))

vi.mock('../../lib/useDesktop', () => ({ openUrl: mocks.openUrl }))

vi.mock('../../lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../../lib/api')>()
  return {
    ...actual,
    api: {
      ...actual.api,
      providerAuthStatus: mocks.status,
      startProviderAuth: mocks.start,
      pollProviderAuthFlow: mocks.poll,
      cancelProviderAuthFlow: mocks.cancel,
      setProviderAuthDefault: mocks.setDefault,
      removeProviderAuthAccount: mocks.removeAccount,
      logoutProviderAuth: mocks.logout,
    },
  }
})

const EMPTY_STATUS: ProviderAuthStatus = {
  method: 'codex_oauth',
  accounts: [],
}

const CONNECTED_STATUS: ProviderAuthStatus = {
  method: 'codex_oauth',
  default_account_id: 'account-1',
  accounts: [{
    id: 'account-1',
    login: 'jack@example.com',
    email: 'jack@example.com',
    authenticated_at: '2026-08-09T08:00:00Z',
    requires_reauth: false,
  }],
}

function deferred<T>() {
  let resolve!: (value: T | PromiseLike<T>) => void
  let reject!: (reason?: unknown) => void
  const promise = new Promise<T>((resolvePromise, rejectPromise) => {
    resolve = resolvePromise
    reject = rejectPromise
  })
  return { promise, resolve, reject }
}

beforeEach(async () => {
  cleanup()
  vi.clearAllMocks()
  vi.useRealTimers()
  await i18n.changeLanguage('en')
  mocks.openUrl.mockResolvedValue()
  mocks.status.mockResolvedValue(EMPTY_STATUS)
  mocks.start.mockResolvedValue({
    flow_id: 'flow-1',
    user_code: 'ABCD-EFGH',
    verification_uri: 'https://auth.example/device',
    verification_uri_complete: 'https://auth.example/device?user_code=ABCD-EFGH',
    expires_at: '2099-01-01T00:00:00Z',
    interval_seconds: 1,
  })
  mocks.cancel.mockResolvedValue({ status: 'cancelled' })
})

afterEach(() => {
  vi.useRealTimers()
})

function renderOAuth(overrides: Partial<React.ComponentProps<typeof ProviderAuthSection>> = {}) {
  const props: React.ComponentProps<typeof ProviderAuthSection> = {
    methods: ['api_key', 'codex_oauth'],
    value: 'codex_oauth',
    binding: { method: 'codex_oauth' },
    apiKeyField: <input aria-label="test api key" />,
    onMethodChange: vi.fn(),
    onBindingChange: vi.fn(),
    ...overrides,
  }
  return { ...render(<ProviderAuthSection {...props} />), props }
}

describe('ProviderAuthSection', () => {
  it('opens the complete verification URL and cancels the active flow', async () => {
    renderOAuth()

    fireEvent.click(await screen.findByRole('button', { name: 'Sign in with ChatGPT' }))
    await screen.findByText('ABCD-EFGH')

    expect(mocks.start).toHaveBeenCalledWith('codex_oauth')
    expect(mocks.openUrl).toHaveBeenCalledWith('https://auth.example/device?user_code=ABCD-EFGH')
    expect(screen.getByTitle('https://auth.example/device?user_code=ABCD-EFGH')).toBeTruthy()
    const panel = screen.getByRole('region', { name: 'Enter this code in your browser' })
    expect(document.activeElement).toBe(panel)
    const describedBy = panel.getAttribute('aria-describedby')?.split(' ') ?? []
    expect(describedBy).toHaveLength(2)
    expect(document.getElementById(describedBy[0])?.textContent).toContain('authorization page')
    expect(document.getElementById(describedBy[1])?.textContent).toBe('ABCD-EFGH')

    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))
    await waitFor(() => expect(mocks.cancel).toHaveBeenCalledWith('codex_oauth', 'flow-1'))
  })

  it('cancels a pending flow when the form unmounts', async () => {
    const view = renderOAuth()
    fireEvent.click(await screen.findByRole('button', { name: 'Sign in with ChatGPT' }))
    await screen.findByText('ABCD-EFGH')

    view.unmount()
    await waitFor(() => expect(mocks.cancel).toHaveBeenCalledWith('codex_oauth', 'flow-1'))
  })

  it('polls at interval_seconds, binds the authorized account, and reports success', async () => {
    vi.useFakeTimers()
    mocks.status.mockResolvedValueOnce(EMPTY_STATUS).mockResolvedValueOnce(CONNECTED_STATUS)
    mocks.poll.mockResolvedValue({ state: 'authorized', account: CONNECTED_STATUS.accounts[0] })
    const onBindingChange = vi.fn()
    const onAuthenticated = vi.fn()
    renderOAuth({ onBindingChange, onAuthenticated })

    await act(async () => {})
    fireEvent.click(screen.getByRole('button', { name: 'Sign in with ChatGPT' }))
    await act(async () => {})
    expect(screen.getByText('ABCD-EFGH')).toBeTruthy()

    await act(async () => {
      await vi.advanceTimersByTimeAsync(1000)
    })

    expect(mocks.poll).toHaveBeenCalledWith('codex_oauth', 'flow-1')
    expect(onBindingChange).toHaveBeenCalledWith({ method: 'codex_oauth', account_id: 'account-1' })
    expect(onAuthenticated).toHaveBeenCalledWith(CONNECTED_STATUS)
    expect(screen.getByText('jack@example.com')).toBeTruthy()
  })

  it('keeps the flow visible until status, binding, and the refresh callback are committed', async () => {
    vi.useFakeTimers()
    const statusResponse = deferred<ProviderAuthStatus>()
    const refresh = deferred<void>()
    mocks.status.mockResolvedValueOnce(EMPTY_STATUS).mockReturnValueOnce(statusResponse.promise)
    mocks.poll.mockResolvedValue({ state: 'authorized', account: CONNECTED_STATUS.accounts[0] })
    const onBindingChange = vi.fn()
    const onStatusChange = vi.fn()
    const onAuthenticated = vi.fn(() => refresh.promise)
    renderOAuth({ onBindingChange, onStatusChange, onAuthenticated })

    await act(async () => {})
    onStatusChange.mockClear()
    fireEvent.click(screen.getByRole('button', { name: 'Sign in with ChatGPT' }))
    await act(async () => {})
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1000)
    })

    expect(screen.getByText('ABCD-EFGH')).toBeTruthy()
    expect(screen.getByText('Finishing sign-in…')).toBeTruthy()
    expect(onBindingChange).not.toHaveBeenCalled()
    expect(onAuthenticated).not.toHaveBeenCalled()

    await act(async () => {
      statusResponse.resolve(CONNECTED_STATUS)
      await Promise.resolve()
    })
    expect(onStatusChange).toHaveBeenCalledWith(CONNECTED_STATUS)
    expect(onBindingChange).toHaveBeenCalledWith({ method: 'codex_oauth', account_id: 'account-1' })
    expect(onAuthenticated).toHaveBeenCalledWith(CONNECTED_STATUS)
    expect(screen.getByText('ABCD-EFGH')).toBeTruthy()

    await act(async () => {
      refresh.resolve()
      await Promise.resolve()
    })
    expect(screen.queryByText('ABCD-EFGH')).toBeNull()
    expect(screen.getByText('jack@example.com')).toBeTruthy()
  })

  it('invalidates an older initial status request when login starts and succeeds', async () => {
    vi.useFakeTimers()
    const oldStatus = deferred<ProviderAuthStatus>()
    mocks.status.mockReturnValueOnce(oldStatus.promise).mockResolvedValueOnce(CONNECTED_STATUS)
    mocks.poll.mockResolvedValue({ state: 'authorized', account: CONNECTED_STATUS.accounts[0] })
    const onStatusChange = vi.fn()
    renderOAuth({ initialStatus: EMPTY_STATUS, onStatusChange })

    fireEvent.click(screen.getByRole('button', { name: 'Sign in with ChatGPT' }))
    await act(async () => {})
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1000)
    })
    expect(screen.getByText('jack@example.com')).toBeTruthy()
    expect(onStatusChange).toHaveBeenCalledTimes(1)

    await act(async () => {
      oldStatus.resolve({
        method: 'codex_oauth',
        default_account_id: 'stale-account',
        accounts: [{
          id: 'stale-account', login: 'stale@example.com', authenticated_at: '2026-08-09T07:00:00Z', requires_reauth: false,
        }],
      })
      await Promise.resolve()
    })
    expect(onStatusChange).toHaveBeenCalledTimes(1)
    expect(screen.queryByText('stale@example.com')).toBeNull()
  })

  it('uses the latest pending interval_seconds after an upstream slow_down', async () => {
    vi.useFakeTimers()
    mocks.poll
      .mockResolvedValueOnce({ state: 'pending', interval_seconds: 4 })
      .mockResolvedValueOnce({ state: 'pending', interval_seconds: 4 })
    renderOAuth()

    await act(async () => {})
    fireEvent.click(screen.getByRole('button', { name: 'Sign in with ChatGPT' }))
    await act(async () => {})
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1000)
    })
    expect(mocks.poll).toHaveBeenCalledTimes(1)

    await act(async () => {
      await vi.advanceTimersByTimeAsync(3999)
    })
    expect(mocks.poll).toHaveBeenCalledTimes(1)
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1)
    })
    expect(mocks.poll).toHaveBeenCalledTimes(2)
  })

  it('cancels a late start after a method switch without opening the browser and ignores double clicks', async () => {
    const startResponse = deferred<Awaited<ReturnType<typeof mocks.start>>>()
    mocks.start.mockReturnValue(startResponse.promise)
    const view = renderOAuth()
    const signIn = await screen.findByRole('button', { name: 'Sign in with ChatGPT' })

    fireEvent.click(signIn)
    fireEvent.click(signIn)
    expect(mocks.start).toHaveBeenCalledTimes(1)
    view.rerender(<ProviderAuthSection {...view.props} value="api_key" binding={null} />)

    await act(async () => {
      startResponse.resolve({
        flow_id: 'late-flow',
        user_code: 'LATE-CODE',
        verification_uri: 'https://auth.example/device',
        expires_at: '2099-01-01T00:00:00Z',
        interval_seconds: 1,
      })
      await Promise.resolve()
    })

    await waitFor(() => expect(mocks.cancel).toHaveBeenCalledWith('codex_oauth', 'late-flow'))
    expect(mocks.openUrl).not.toHaveBeenCalled()
    expect(screen.queryByText('LATE-CODE')).toBeNull()
  })

  it('cancels a start that resolves after unmount without opening the browser', async () => {
    const startResponse = deferred<Awaited<ReturnType<typeof mocks.start>>>()
    mocks.start.mockReturnValue(startResponse.promise)
    const view = renderOAuth()
    fireEvent.click(await screen.findByRole('button', { name: 'Sign in with ChatGPT' }))
    view.unmount()

    await act(async () => {
      startResponse.resolve({
        flow_id: 'unmounted-flow',
        user_code: 'GONE-CODE',
        verification_uri: 'https://auth.example/device',
        expires_at: '2099-01-01T00:00:00Z',
        interval_seconds: 1,
      })
      await Promise.resolve()
    })

    await waitFor(() => expect(mocks.cancel).toHaveBeenCalledWith('codex_oauth', 'unmounted-flow'))
    expect(mocks.openUrl).not.toHaveBeenCalled()
  })

  it('reports post-auth refresh failures without turning a successful login into an auth failure', async () => {
    vi.useFakeTimers()
    mocks.status.mockResolvedValueOnce(EMPTY_STATUS).mockResolvedValueOnce(CONNECTED_STATUS)
    mocks.poll.mockResolvedValue({ state: 'authorized', account: CONNECTED_STATUS.accounts[0] })
    renderOAuth({ onAuthenticated: vi.fn().mockRejectedValue(new Error('catalog offline')) })

    await act(async () => {})
    fireEvent.click(screen.getByRole('button', { name: 'Sign in with ChatGPT' }))
    await act(async () => {})
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1000)
    })

    expect(screen.getByText('jack@example.com')).toBeTruthy()
    expect(screen.getByRole('alert').textContent).toContain('The account is connected')
    expect(screen.getByRole('alert').textContent).toContain('catalog offline')
    expect(screen.queryByText('Authentication failed. Try again.')).toBeNull()
  })

  it('supports multiple accounts and changes the managed default explicitly', async () => {
    const status: ProviderAuthStatus = {
      method: 'codex_oauth',
      default_account_id: 'account-1',
      accounts: [
        CONNECTED_STATUS.accounts[0],
        {
          id: 'account-2',
          login: 'work@example.com',
          domain: 'github.example.com',
          authenticated_at: '2026-08-09T09:00:00Z',
          requires_reauth: false,
        },
      ],
    }
    const oldStatus = deferred<ProviderAuthStatus>()
    const nextStatus = { ...status, default_account_id: 'account-2' }
    mocks.status.mockReturnValue(oldStatus.promise)
    mocks.setDefault.mockResolvedValue(nextStatus)
    const onStatusChange = vi.fn()
    renderOAuth({ binding: { method: 'codex_oauth', account_id: 'account-2' }, initialStatus: status, onStatusChange })

    fireEvent.click(await screen.findByRole('button', { name: 'Manage accounts (2)' }))
    expect(screen.getByText('work@example.com')).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Set as default' }))

    await waitFor(() => expect(mocks.setDefault).toHaveBeenCalledWith('codex_oauth', 'account-2'))
    expect(onStatusChange).toHaveBeenLastCalledWith(nextStatus)
    await act(async () => {
      oldStatus.resolve(status)
      await Promise.resolve()
    })
    expect(onStatusChange).toHaveBeenCalledTimes(1)
  })

  it('keeps an explicitly bound removed account missing instead of silently following the default', async () => {
    const status: ProviderAuthStatus = {
      method: 'codex_oauth',
      default_account_id: 'account-1',
      accounts: [
        CONNECTED_STATUS.accounts[0],
        {
          id: 'account-2',
          login: 'work@example.com',
          authenticated_at: '2026-08-09T09:00:00Z',
          requires_reauth: false,
        },
      ],
    }
    mocks.status.mockResolvedValue(status)
    mocks.removeAccount.mockResolvedValue({
      ...status,
      default_account_id: 'account-2',
      accounts: [status.accounts[1]],
    })
    const onBindingChange = vi.fn()
    renderOAuth({
      binding: { method: 'codex_oauth', account_id: 'account-1' },
      initialStatus: status,
      onBindingChange,
    })

    fireEvent.click(await screen.findByRole('button', { name: 'Manage accounts (2)' }))
    fireEvent.click(screen.getAllByRole('button', { name: 'Remove account' })[0])
    const confirm = screen.getByRole('button', { name: 'Remove' })
    expect(document.activeElement).toBe(confirm)
    fireEvent.click(confirm)

    await waitFor(() => expect(mocks.removeAccount).toHaveBeenCalledWith('codex_oauth', 'account-1'))
    expect(onBindingChange).not.toHaveBeenCalled()
    expect(await screen.findByText('The account saved on this provider is no longer available. Choose another account or sign in again.')).toBeTruthy()
  })

  it('does not commit a late logout result after switching methods', async () => {
    const logoutResponse = deferred<ProviderAuthStatus>()
    mocks.status.mockResolvedValue(CONNECTED_STATUS)
    mocks.logout.mockReturnValue(logoutResponse.promise)
    const onStatusChange = vi.fn()
    const onAuthenticated = vi.fn()
    const view = renderOAuth({
      binding: { method: 'codex_oauth', account_id: 'account-1' },
      initialStatus: CONNECTED_STATUS,
      onStatusChange,
      onAuthenticated,
    })

    fireEvent.click(await screen.findByRole('button', { name: 'Manage accounts (1)' }))
    fireEvent.click(screen.getByRole('button', { name: 'Sign out all' }))
    const confirm = screen.getByRole('button', { name: 'Sign out all' })
    expect(document.activeElement).toBe(confirm)
    fireEvent.click(confirm)
    await waitFor(() => expect(mocks.logout).toHaveBeenCalledWith('codex_oauth'))
    onStatusChange.mockClear()
    onAuthenticated.mockClear()
    view.rerender(<ProviderAuthSection {...view.props} value="api_key" binding={null} />)

    await act(async () => {
      logoutResponse.resolve({ method: 'codex_oauth', accounts: [] })
      await Promise.resolve()
    })
    expect(onStatusChange).not.toHaveBeenCalled()
    expect(onAuthenticated).not.toHaveBeenCalled()
  })

  it('blocks a missing saved account and offers re-authentication', async () => {
    mocks.status.mockResolvedValue(CONNECTED_STATUS)
    renderOAuth({
      binding: { method: 'codex_oauth', account_id: 'missing-account' },
      initialStatus: CONNECTED_STATUS,
    })

    expect(await screen.findByText('The account saved on this provider is no longer available. Choose another account or sign in again.')).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Re-authenticate' })).toBeTruthy()
    expect(isProviderAuthReady(CONNECTED_STATUS, { method: 'codex_oauth', account_id: 'missing-account' })).toBe(false)
  })

  it('resolves default and explicit bindings while rejecting accounts that need re-authentication', () => {
    expect(resolveProviderAuthAccount(CONNECTED_STATUS, { method: 'codex_oauth' })?.id).toBe('account-1')
    expect(isProviderAuthReady(CONNECTED_STATUS, { method: 'codex_oauth' })).toBe(true)
    expect(isProviderAuthReady({
      ...CONNECTED_STATUS,
      accounts: [{ ...CONNECTED_STATUS.accounts[0], requires_reauth: true }],
    }, { method: 'codex_oauth' })).toBe(false)
  })

  it('exposes labelled authentication and account groups', async () => {
    mocks.status.mockResolvedValue(CONNECTED_STATUS)
    renderOAuth({ initialStatus: CONNECTED_STATUS })

    expect(screen.getByRole('group', { name: 'Authentication' })).toBeTruthy()
    expect(await screen.findByLabelText('Account used by this provider')).toBeTruthy()
  })

  it('freezes the whole authentication group while its parent form is saving', async () => {
    renderOAuth({ initialStatus: EMPTY_STATUS, disabled: true })

    const group = screen.getByRole('group', { name: 'Authentication' })
    expect(group.hasAttribute('disabled')).toBe(true)
    const signIn = await screen.findByRole('button', { name: 'Sign in with ChatGPT' })
    expect(signIn.hasAttribute('disabled')).toBe(true)
    fireEvent.click(signIn)
    expect(mocks.start).not.toHaveBeenCalled()
  })
})
