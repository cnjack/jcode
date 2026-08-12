import { afterEach, describe, expect, it, vi } from 'vitest'
import { isAbsoluteLocalWorkspacePath } from './useDesktop'

const mocks = vi.hoisted(() => ({
  openUrl: vi.fn<() => Promise<void>>(),
}))

vi.mock('@tauri-apps/plugin-opener', () => ({ openUrl: mocks.openUrl }))

afterEach(() => {
  delete (window as unknown as { __TAURI_INTERNALS__?: unknown }).__TAURI_INTERNALS__
  document.body.replaceChildren()
  vi.clearAllMocks()
  vi.resetModules()
})

describe('isAbsoluteLocalWorkspacePath', () => {
  it('accepts absolute macOS paths and rejects relative or remote workspaces', () => {
    expect(isAbsoluteLocalWorkspacePath('/Users/test/work/jcode')).toBe(true)
    expect(isAbsoluteLocalWorkspacePath('/Volumes/source/jcode')).toBe(true)
    expect(isAbsoluteLocalWorkspacePath('C:\\Users\\test\\work\\jcode')).toBe(true)
    expect(isAbsoluteLocalWorkspacePath('D:/source/jcode')).toBe(true)
    expect(isAbsoluteLocalWorkspacePath('work/jcode')).toBe(false)
    expect(isAbsoluteLocalWorkspacePath('C:work\\jcode')).toBe(false)
    expect(isAbsoluteLocalWorkspacePath('\\\\server\\share\\jcode')).toBe(false)
    expect(isAbsoluteLocalWorkspacePath('ssh://host/work/jcode')).toBe(false)
    expect(isAbsoluteLocalWorkspacePath('docker://container/work/jcode')).toBe(false)
  })
})

describe('initExternalLinks', () => {
  it.each([
    ['ordinary click', {}],
    ['Command-click', { metaKey: true }],
    ['Ctrl-click', { ctrlKey: true }],
  ])('opens a localhost preview in the system browser on Desktop: %s', async (_label, modifiers) => {
    ;(window as unknown as { __TAURI_INTERNALS__?: unknown }).__TAURI_INTERNALS__ = {}
    const { initExternalLinks } = await import('./useDesktop')
    const cleanup = initExternalLinks()
    const anchor = document.createElement('a')
    anchor.href = 'http://localhost:3721/'
    document.body.append(anchor)

    const followedInWebview = anchor.dispatchEvent(new MouseEvent('click', {
      bubbles: true,
      cancelable: true,
      button: 0,
      ...modifiers,
    }))

    expect(followedInWebview).toBe(false)
    await vi.waitFor(() => expect(mocks.openUrl).toHaveBeenCalledWith('http://localhost:3721/'))
    cleanup()
  })

  it('keeps Browser Web Command-click behavior native', async () => {
    const { initExternalLinks } = await import('./useDesktop')
    const cleanup = initExternalLinks()
    const anchor = document.createElement('a')
    anchor.href = 'https://example.com/'
    document.body.append(anchor)
    let preventedByRouter = false
    anchor.addEventListener('click', (event) => {
      preventedByRouter = event.defaultPrevented
      // Stop jsdom from attempting an actual navigation after observing the
      // capture-phase router's decision.
      event.preventDefault()
    })

    anchor.dispatchEvent(new MouseEvent('click', {
      bubbles: true,
      cancelable: true,
      button: 0,
      metaKey: true,
    }))

    expect(preventedByRouter).toBe(false)
    expect(mocks.openUrl).not.toHaveBeenCalled()
    cleanup()
  })
})
