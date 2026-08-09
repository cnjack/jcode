import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { Provider } from 'react-redux'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { chatActions, store } from '../app/store'
import { i18n } from '../i18n'
import { RightPanel } from './RightPanel'

afterEach(cleanup)

beforeEach(async () => {
  await i18n.changeLanguage('en')
  store.dispatch(chatActions.setTodos([]))
  Object.defineProperty(window, 'matchMedia', {
    configurable: true,
    value: vi.fn().mockReturnValue({
      matches: true,
      media: '(max-width: 899px)',
      onchange: null,
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
      addListener: vi.fn(),
      removeListener: vi.fn(),
      dispatchEvent: vi.fn(),
    }),
  })
})

describe('RightPanel compact accessibility', () => {
  it('acts as a focus-contained dialog, closes with Escape, and restores focus', async () => {
    const opener = document.createElement('button')
    opener.textContent = 'Open panel'
    document.body.appendChild(opener)
    opener.focus()
    const close = vi.fn()

    const view = render(
      <Provider store={store}>
        <RightPanel activeTab="plan" onClose={close} onSwitchTab={vi.fn()} />
      </Provider>,
    )

    const dialog = await screen.findByRole('dialog', { name: 'Plan' })
    await waitFor(() => expect(document.activeElement).toBe(dialog))

    fireEvent.keyDown(window, { key: 'Tab', shiftKey: true })
    expect(document.activeElement).toBe(screen.getByRole('button', { name: 'Close panel' }))

    fireEvent.keyDown(window, { key: 'Escape' })
    expect(close).toHaveBeenCalledOnce()

    view.unmount()
    await waitFor(() => expect(document.activeElement).toBe(opener))
    opener.remove()
  })
})
