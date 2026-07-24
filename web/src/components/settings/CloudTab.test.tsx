import { beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { i18n } from '../../i18n'
import { CloudTab } from './CloudTab'

const mocks = vi.hoisted(() => ({
  openUrl: vi.fn<() => Promise<void>>(),
  cloudLogin: vi.fn(),
}))

vi.mock('../../lib/useDesktop', () => ({
  openUrl: mocks.openUrl,
}))

vi.mock('../../lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../../lib/api')>()
  return {
    ...actual,
    api: {
      ...actual.api,
      cloudStatus: vi.fn().mockResolvedValue({ logged_in: false }),
      cloudSync: vi.fn().mockResolvedValue({ sync_default: false }),
      cloudLoginStatus: vi.fn().mockResolvedValue({ state: 'idle' }),
      cloudLogin: mocks.cloudLogin,
    },
  }
})

beforeEach(async () => {
  cleanup()
  vi.clearAllMocks()
  mocks.openUrl.mockResolvedValue()
  mocks.cloudLogin.mockResolvedValue({
    user_code: 'ABCD-EFGH',
    verification_uri: 'https://cloud.example.com/device',
    expires_at: '2099-01-01T00:00:00Z',
  })
  await i18n.changeLanguage('en')
})

describe('CloudTab login', () => {
  it('opens device authorization in the system browser and keeps a safe fallback button', async () => {
    render(<CloudTab />)

    const login = await screen.findByRole('button', { name: 'Log in to cloud' })
    fireEvent.click(login)

    await waitFor(() => {
      expect(mocks.openUrl).toHaveBeenCalledWith('https://cloud.example.com/device')
    })

    const fallback = screen.getByRole('button', { name: 'https://cloud.example.com/device' })
    fireEvent.click(fallback)
    await waitFor(() => expect(mocks.openUrl).toHaveBeenCalledTimes(2))
  })
})
