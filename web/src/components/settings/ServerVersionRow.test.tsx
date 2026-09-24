import { beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { Provider } from 'react-redux'
import { i18n } from '../../i18n'
import { modelActions, store } from '../../app/store'
import type { VersionInfoResponse } from '../../lib/api'
import { formatVersion, ServerVersionRow } from './ServerVersionRow'

const mocks = vi.hoisted(() => ({
  openUrl: vi.fn<(url: string) => Promise<void>>(),
  version: vi.fn<(check?: boolean) => Promise<VersionInfoResponse>>(),
}))

vi.mock('../../lib/useDesktop', () => ({
  isTauri: false,
  openUrl: mocks.openUrl,
}))

vi.mock('../../lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../../lib/api')>()
  return { ...actual, api: { ...actual.api, version: mocks.version } }
})

const build: VersionInfoResponse = {
  version: 'v0.13.8',
  git_commit: 'abc1234',
  checked: false,
  update_available: false,
}

function renderRow() {
  return render(
    <Provider store={store}>
      <ServerVersionRow />
    </Provider>,
  )
}

function statusLine() {
  return screen.getByTestId('server-version-row').querySelector('[aria-live="polite"]')?.textContent ?? ''
}

beforeEach(async () => {
  cleanup()
  vi.clearAllMocks()
  mocks.openUrl.mockResolvedValue()
  store.dispatch(modelActions.setServerVersion(''))
  await i18n.changeLanguage('en')
})

describe('formatVersion', () => {
  it('adds a single v prefix to numeric versions only', () => {
    expect(formatVersion('0.13.8')).toBe('v0.13.8')
    expect(formatVersion('v0.13.8')).toBe('v0.13.8')
    expect(formatVersion(' dev ')).toBe('dev')
    expect(formatVersion(undefined)).toBe('')
  })
})

describe('ServerVersionRow', () => {
  it('shows the running build without checking GitHub until asked', async () => {
    mocks.version.mockResolvedValue(build)
    renderRow()

    await waitFor(() => expect(statusLine()).toBe('Current version v0.13.8 (abc1234)'))
    expect(mocks.version).toHaveBeenCalledTimes(1)
    expect(mocks.version).toHaveBeenCalledWith()
    expect(screen.queryByRole('button', { name: /Release notes/ })).toBeNull()
  })

  it('falls back to the health-seeded version when /api/version is unavailable', async () => {
    store.dispatch(modelActions.setServerVersion('0.13.7'))
    mocks.version.mockRejectedValue(new Error('offline'))
    renderRow()

    await waitFor(() => expect(mocks.version).toHaveBeenCalled())
    expect(statusLine()).toBe('Current version v0.13.7')
  })

  it('reports an available update with release notes and the CLI upgrade path', async () => {
    mocks.version.mockImplementation(async (check) => check
      ? {
          ...build,
          checked: true,
          latest: 'v0.14.0',
          release_url: 'https://github.com/cnjack/jcode/releases/tag/v0.14.0',
          update_available: true,
        }
      : build)
    renderRow()

    fireEvent.click(screen.getByRole('button', { name: 'Check for updates' }))

    await waitFor(() => expect(statusLine()).toContain('New version v0.14.0 available'))
    expect(mocks.version).toHaveBeenLastCalledWith(true)
    expect(screen.getByText('jcode update')).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: /Release notes/ }))
    expect(mocks.openUrl).toHaveBeenCalledWith('https://github.com/cnjack/jcode/releases/tag/v0.14.0')
  })

  it('reports up to date and check failures', async () => {
    mocks.version.mockResolvedValueOnce(build)
    mocks.version.mockResolvedValueOnce({ ...build, checked: true, latest: 'v0.13.8' })
    renderRow()

    fireEvent.click(screen.getByRole('button', { name: 'Check for updates' }))
    await waitFor(() => expect(statusLine()).toContain('Up to date'))
    expect(screen.queryByText('jcode update')).toBeNull()

    mocks.version.mockResolvedValueOnce({ ...build, checked: true, check_error: 'HTTP 403' })
    fireEvent.click(screen.getByRole('button', { name: 'Check for updates' }))
    await waitFor(() => expect(statusLine()).toContain('Check failed'))
  })
})
