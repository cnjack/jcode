import { useState } from 'react'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { i18n } from '../i18n'
import { TerminalPanel } from './TerminalPanel'

const mocks = vi.hoisted(() => ({
  ptyCreate: vi.fn(),
  ptyKill: vi.fn(),
  terminalDispose: vi.fn(),
  terminalFocus: vi.fn(),
  fit: vi.fn(),
}))

vi.mock('../lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../lib/api')>()
  return { ...actual, api: { ...actual.api, ptyCreate: mocks.ptyCreate, ptyKill: mocks.ptyKill } }
})

vi.mock('@xterm/xterm', () => ({
  Terminal: class {
    cols = 80
    rows = 24
    options: Record<string, unknown> = {}
    loadAddon() {}
    open() {}
    onData() { return { dispose() {} } }
    write() {}
    writeln() {}
    focus() { mocks.terminalFocus() }
    dispose() { mocks.terminalDispose() }
  },
}))

vi.mock('@xterm/addon-fit', () => ({
  FitAddon: class { fit() { mocks.fit() } },
}))

vi.mock('@xterm/addon-web-links', () => ({ WebLinksAddon: class {} }))

class FakeResizeObserver {
  observe() {}
  disconnect() {}
}

class FakeWebSocket {
  static OPEN = 1
  readyState = FakeWebSocket.OPEN
  binaryType = ''
  onopen: (() => void) | null = null
  onclose: (() => void) | null = null
  onerror: (() => void) | null = null
  onmessage: ((event: MessageEvent) => void) | null = null
  constructor(_url: string, _protocols?: string[]) {}
  send() {}
  close() {}
}

beforeEach(async () => {
  cleanup()
  vi.clearAllMocks()
  await i18n.changeLanguage('en')
  let id = 0
  mocks.ptyCreate.mockImplementation(async () => ({ id: `pty-${++id}` }))
  mocks.ptyKill.mockResolvedValue({ status: 'ok' })
  vi.stubGlobal('ResizeObserver', FakeResizeObserver)
  vi.stubGlobal('WebSocket', FakeWebSocket)
  vi.stubGlobal('requestAnimationFrame', (callback: FrameRequestCallback) => window.setTimeout(() => callback(0), 0))
  vi.stubGlobal('cancelAnimationFrame', (handle: number) => window.clearTimeout(handle))
})

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

describe('TerminalPanel tabs', () => {
  it('creates independent PTYs and closes individual tabs', async () => {
    const onClose = vi.fn()

    function Harness() {
      const [open, setOpen] = useState(true)
      return open ? <TerminalPanel onClose={() => { onClose(); setOpen(false) }} /> : null
    }

    render(<Harness />)
    expect(screen.getByRole('tab', { name: 'Shell 1' }).getAttribute('aria-selected')).toBe('true')
    await waitFor(() => expect(mocks.ptyCreate).toHaveBeenCalledTimes(1))

    fireEvent.click(screen.getByRole('button', { name: 'New terminal' }))
    expect(screen.getByRole('tab', { name: 'Shell 2' }).getAttribute('aria-selected')).toBe('true')
    await waitFor(() => expect(mocks.ptyCreate).toHaveBeenCalledTimes(2))

    fireEvent.click(screen.getByRole('button', { name: 'Close Shell 2' }))
    expect(screen.queryByRole('tab', { name: 'Shell 2' })).toBeNull()
    expect(screen.getByRole('tab', { name: 'Shell 1' }).getAttribute('aria-selected')).toBe('true')
    await waitFor(() => expect(mocks.ptyKill).toHaveBeenCalledWith('pty-2'))

    fireEvent.click(screen.getByRole('button', { name: 'Close Shell 1' }))
    expect(onClose).toHaveBeenCalledOnce()
    await waitFor(() => expect(mocks.ptyKill).toHaveBeenCalledWith('pty-1'))
  })
})
