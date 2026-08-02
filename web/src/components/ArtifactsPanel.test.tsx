import { beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { Provider } from 'react-redux'
import { sessionActions, store } from '../app/store'
import { i18n } from '../i18n'
import { ArtifactsPanel, canOpenArtifactOnDesktop } from './ArtifactsPanel'

const mocks = vi.hoisted(() => ({
  artifacts: vi.fn(),
  artifactContent: vi.fn(),
  markArtifactsViewed: vi.fn(),
  cloudStatus: vi.fn(),
  createArtifactShare: vi.fn(),
  artifactShares: vi.fn(),
  revokeArtifactShare: vi.fn(),
}))

vi.mock('../lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../lib/api')>()
  return { ...actual, api: { ...actual.api, ...mocks } }
})

const htmlArtifact = {
  id: 'artifact-html',
  session_id: 'artifact-task',
  relative_path: 'reports/demo.html',
  title: 'Interactive demo',
  kind: 'html' as const,
  media_type: 'text/html',
  size: 31,
  revision: 1,
  updated_at: '2026-08-01T12:00:00Z',
  status: 'available' as const,
  focus: true,
}

beforeEach(async () => {
  cleanup()
  vi.clearAllMocks()
  await i18n.changeLanguage('en')
  store.dispatch(sessionActions.setCurrentSession('artifact-task'))
  mocks.artifacts.mockResolvedValue([htmlArtifact])
  mocks.artifactContent.mockResolvedValue(new Blob(['<script>document.body.textContent="isolated"</script>'], { type: 'text/html' }))
  mocks.markArtifactsViewed.mockResolvedValue(undefined)
  mocks.cloudStatus.mockResolvedValue({ logged_in: false })
  mocks.createArtifactShare.mockResolvedValue({
    share_id: 'share-1',
    url: 'https://share.example/s/share-1#k=v1.secret',
    expires_at: '2026-08-08T00:00:00Z',
  })
  mocks.artifactShares.mockResolvedValue([])
  mocks.revokeArtifactShare.mockResolvedValue(undefined)
})

function renderPanel() {
  return render(<Provider store={store}><ArtifactsPanel /></Provider>)
}

describe('ArtifactsPanel', () => {
	it('hides host-open for spoofed active content and executable extensions', () => {
		expect(canOpenArtifactOnDesktop({ ...htmlArtifact, kind: 'text' })).toBe(false)
		expect(canOpenArtifactOnDesktop({ ...htmlArtifact, relative_path: 'reports/demo.svg', kind: 'image', media_type: 'image/svg+xml' })).toBe(false)
		expect(canOpenArtifactOnDesktop({ ...htmlArtifact, relative_path: 'reports/demo.command', kind: 'text', media_type: 'text/plain' })).toBe(false)
		expect(canOpenArtifactOnDesktop({ ...htmlArtifact, relative_path: 'reports/demo.md', kind: 'markdown', media_type: 'text/markdown' })).toBe(true)
	})

  it('loads the active task artifact and isolates HTML in an opaque-origin sandbox', async () => {
    renderPanel()
    expect(await screen.findByRole('heading', { name: 'Interactive demo' })).toBeTruthy()
    const frame = await screen.findByTitle('Interactive demo')
    expect(frame.getAttribute('sandbox')).toBe('allow-scripts')
    expect(frame.getAttribute('sandbox')).not.toContain('allow-same-origin')
    expect(frame.getAttribute('srcdoc')).toContain("connect-src 'none'")
    expect(mocks.artifacts).toHaveBeenCalledWith('artifact-task')
    await waitFor(() => expect(mocks.markArtifactsViewed).toHaveBeenCalledWith('artifact-task'))
  })

  it('hides Cloud sharing completely when the user is not logged in', async () => {
    renderPanel()
    await screen.findByRole('heading', { name: 'Interactive demo' })
    expect(screen.queryByRole('button', { name: 'Share' })).toBeNull()
    expect(mocks.artifactShares).not.toHaveBeenCalled()
  })

  it('shows explicit sharing when Cloud is logged in without requiring relay online', async () => {
    mocks.cloudStatus.mockResolvedValue({ logged_in: true, state: 'offline', auto_connect: false })
    renderPanel()
    await screen.findByRole('heading', { name: 'Interactive demo' })
    expect(await screen.findByRole('button', { name: 'Share' })).toBeTruthy()
  })

  it('requires explicit confirmation before creating an encrypted fragment link', async () => {
    mocks.cloudStatus.mockResolvedValue({ logged_in: true, state: 'offline', auto_connect: false })
    renderPanel()
    fireEvent.click(await screen.findByRole('button', { name: 'Share' }))
    expect(mocks.createArtifactShare).not.toHaveBeenCalled()
    fireEvent.click(await screen.findByRole('button', { name: 'Create encrypted link' }))
    await waitFor(() => expect(mocks.createArtifactShare).toHaveBeenCalledWith('artifact-task', 'artifact-html', 604800))
    const link = await screen.findByDisplayValue('https://share.example/s/share-1#k=v1.secret')
    expect(link.getAttribute('readonly')).not.toBeNull()
  })
})
