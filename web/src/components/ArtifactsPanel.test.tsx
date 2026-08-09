import { beforeEach, describe, expect, it, vi } from 'vitest'
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { Provider } from 'react-redux'
import { sessionActions, store } from '../app/store'
import { i18n } from '../i18n'
import { ArtifactsPanel, canOpenArtifactOnDesktop } from './ArtifactsPanel'

const mocks = vi.hoisted(() => ({
  artifacts: vi.fn(),
  artifactContent: vi.fn(),
  markArtifactViewed: vi.fn(),
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
  shareable: true,
}

beforeEach(async () => {
  cleanup()
  vi.clearAllMocks()
  Object.defineProperty(URL, 'createObjectURL', { configurable: true, value: vi.fn(() => 'blob:artifact-test') })
  Object.defineProperty(URL, 'revokeObjectURL', { configurable: true, value: vi.fn() })
  await i18n.changeLanguage('en')
  store.dispatch(sessionActions.setCurrentSession('artifact-task'))
  mocks.artifacts.mockResolvedValue([htmlArtifact])
  mocks.artifactContent.mockResolvedValue(new Blob(['<script>document.body.textContent="isolated"</script>'], { type: 'text/html' }))
  mocks.markArtifactViewed.mockResolvedValue(undefined)
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
    fireEvent.load(frame)
    await waitFor(() => expect(mocks.markArtifactViewed).toHaveBeenCalledWith('artifact-task', 'artifact-html', 1))
  })

  it('marks only the artifact revision whose content loaded successfully', async () => {
    const first = { ...htmlArtifact, id: 'first', title: 'First', kind: 'text' as const, media_type: 'text/plain' }
    const second = { ...htmlArtifact, id: 'second', title: 'Second', kind: 'text' as const, media_type: 'text/plain', revision: 2 }
    mocks.artifacts.mockResolvedValue([first, second])
    mocks.artifactContent.mockImplementation(async (_taskID: string, artifactID: string) => new Blob([artifactID], { type: 'text/plain' }))
    renderPanel()
    await screen.findByText('first')
    await waitFor(() => expect(mocks.markArtifactViewed).toHaveBeenCalledWith('artifact-task', 'first', 1))
    expect(mocks.markArtifactViewed).not.toHaveBeenCalledWith('artifact-task', 'second', 2)
    fireEvent.click(screen.getByRole('button', { name: /Second/ }))
    await screen.findByText('second')
    await waitFor(() => expect(mocks.markArtifactViewed).toHaveBeenCalledWith('artifact-task', 'second', 2))
  })

  it('reloads the same artifact id before marking a newer revision viewed', async () => {
    const revisionOne = { ...htmlArtifact, id: 'same', title: 'Same', kind: 'text' as const, media_type: 'text/plain' }
    const revisionTwo = { ...revisionOne, revision: 2, updated_at: '2026-08-01T12:01:00Z' }
    mocks.artifacts.mockResolvedValueOnce([revisionOne]).mockResolvedValueOnce([revisionTwo])
    let resolveRevisionTwo: (blob: Blob) => void = () => undefined
    const revisionTwoContent = new Promise<Blob>((resolve) => { resolveRevisionTwo = resolve })
    mocks.artifactContent
      .mockResolvedValueOnce(new Blob(['revision one'], { type: 'text/plain' }))
      .mockReturnValueOnce(revisionTwoContent)

    renderPanel()
    await screen.findByText('revision one')
    await waitFor(() => expect(mocks.markArtifactViewed).toHaveBeenCalledWith('artifact-task', 'same', 1))

    window.dispatchEvent(new CustomEvent('jcode:artifact-upserted', { detail: { artifact_id: 'same' } }))
    await waitFor(() => expect(mocks.artifactContent).toHaveBeenCalledTimes(2))
    expect(screen.queryByText('revision one')).toBeNull()
    expect(mocks.markArtifactViewed).not.toHaveBeenCalledWith('artifact-task', 'same', 2)

    await act(async () => {
      resolveRevisionTwo(new Blob(['revision two'], { type: 'text/plain' }))
      await revisionTwoContent
    })
    await screen.findByText('revision two')
    await waitFor(() => expect(mocks.markArtifactViewed).toHaveBeenCalledWith('artifact-task', 'same', 2))
  })

  it('does not mark a revision when artifact content fails to load', async () => {
    mocks.artifactContent.mockRejectedValueOnce(new Error('unavailable'))
    renderPanel()
    await screen.findByText("Couldn't open this artifact.")
    expect(mocks.markArtifactViewed).not.toHaveBeenCalled()
  })

  it('waits for image decode before marking the revision viewed', async () => {
    mocks.artifacts.mockResolvedValue([{
      ...htmlArtifact, id: 'generated-image', title: 'Generated image',
      kind: 'image' as const, media_type: 'image/png', revision: 3,
    }])
    mocks.artifactContent.mockResolvedValue(new Blob(['png'], { type: 'image/png' }))
    renderPanel()
    const image = await screen.findByAltText('Generated image')
    expect(mocks.markArtifactViewed).not.toHaveBeenCalled()
    fireEvent.load(image)
    await waitFor(() => expect(mocks.markArtifactViewed).toHaveBeenCalledWith('artifact-task', 'generated-image', 3))
  })

  it('hides Cloud sharing completely when the user is not logged in', async () => {
    renderPanel()
    await screen.findByRole('heading', { name: 'Interactive demo' })
    expect(screen.queryByRole('button', { name: 'Share' })).toBeNull()
    expect(mocks.artifactShares).not.toHaveBeenCalled()
  })

  it('hides sharing for managed records that are not explicitly shareable', async () => {
    mocks.cloudStatus.mockResolvedValue({ logged_in: true, state: 'online' })
    mocks.artifacts.mockResolvedValue([{ ...htmlArtifact, storage_kind: 'managed', shareable: false }])
    renderPanel()
    await screen.findByRole('heading', { name: 'Interactive demo' })
    expect(screen.queryByRole('button', { name: 'Share' })).toBeNull()
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
