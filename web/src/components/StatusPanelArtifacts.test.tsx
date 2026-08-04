import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { Provider } from 'react-redux'
import { chatActions, sessionActions, store } from '../app/store'
import { i18n } from '../i18n'
import { StatusPanel } from './StatusPanel'

const mocks = vi.hoisted(() => ({
  artifacts: vi.fn(),
  markArtifactsViewed: vi.fn(),
  artifactDownload: vi.fn(),
  artifactContent: vi.fn(),
  cloudStatus: vi.fn(),
  artifactShares: vi.fn(),
}))

vi.mock('../lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../lib/api')>()
  return { ...actual, api: { ...actual.api, ...mocks } }
})

beforeEach(async () => {
  cleanup()
  vi.clearAllMocks()
  await i18n.changeLanguage('en')
  store.dispatch(chatActions.clearChat())
  store.dispatch(sessionActions.setCurrentSession('compact-artifact-task'))
  store.dispatch(sessionActions.setTasks([{
    uuid: 'compact-artifact-task',
    project: '/workspace/jcode',
    created_at: '2026-08-04T08:00:00Z',
    provider: 'openai',
    model: 'gpt-5',
    pinned: false,
    archived: false,
    unread: false,
    artifact_count: 1,
  }]))
  mocks.artifacts.mockResolvedValue([{
    id: 'artifact-1',
    session_id: 'compact-artifact-task',
    relative_path: 'reports/route-analysis.md',
    title: 'Route analysis',
    kind: 'markdown',
    media_type: 'text/markdown',
    size: 1843,
    revision: 2,
    updated_at: '2026-08-04T08:30:00Z',
    status: 'available',
  }])
  mocks.markArtifactsViewed.mockResolvedValue(undefined)
  mocks.artifactContent.mockResolvedValue(new Blob(['# Route analysis'], { type: 'text/markdown' }))
  mocks.cloudStatus.mockResolvedValue({ logged_in: true, state: 'offline' })
  mocks.artifactShares.mockResolvedValue([])
})

afterEach(() => {
  cleanup()
  store.dispatch(sessionActions.setCurrentSession(''))
  store.dispatch(sessionActions.setTasks([]))
  store.dispatch(chatActions.clearChat())
})

describe('StatusPanel compact artifacts', () => {
  it('opens the full artifact preview and keeps Cloud sharing available', async () => {
    render(
      <Provider store={store}>
        <StatusPanel open isRunning={false} onOpen={() => {}} onClose={() => {}} />
      </Provider>,
    )

    expect(await screen.findByText('Route analysis')).toBeTruthy()
    expect(screen.getByText('markdown · 1.8 KB')).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Download Route analysis' })).toBeTruthy()
    expect(screen.queryByRole('button', { name: 'Full screen' })).toBeNull()
    expect(screen.queryByRole('button', { name: 'Share' })).toBeNull()
    await waitFor(() => expect(mocks.markArtifactsViewed).toHaveBeenCalledWith('compact-artifact-task'))

    fireEvent.click(screen.getByRole('button', { name: 'Preview artifact Route analysis' }))
    expect(await screen.findByRole('dialog', { name: 'Preview artifact' })).toBeTruthy()
    expect(await screen.findByRole('button', { name: 'Full screen' })).toBeTruthy()
    const share = await screen.findByRole('button', { name: 'Share' })
    fireEvent.click(share)
    expect(await screen.findByRole('dialog', { name: 'Share encrypted artifact' })).toBeTruthy()
  })
})
