import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { Provider } from 'react-redux'
import { chatActions, store } from '../app/store'
import { i18n } from '../i18n'
import { RightPanel } from './RightPanel'

beforeEach(async () => {
  cleanup()
  await i18n.changeLanguage('en')
  store.dispatch(chatActions.clearChat())
  store.dispatch(chatActions.setTodos([
    { id: 1, title: 'Keep the conversation width', status: 'in_progress' },
  ]))
})

afterEach(() => {
  cleanup()
  store.dispatch(chatActions.clearChat())
})

describe('RightPanel workspace overlay', () => {
  it('floats above the task and exposes only plan, files, and changes', () => {
    const onClose = vi.fn()
    const onSwitchTab = vi.fn()

    render(
      <Provider store={store}>
        <RightPanel activeTab="plan" onClose={onClose} onSwitchTab={onSwitchTab} />
      </Provider>,
    )

    const panel = screen.getByTestId('workspace-panel')
    expect(screen.getByRole('dialog', { name: 'Work panel' })).toBe(panel)
    expect(panel.className).toContain('absolute')
    expect(screen.getByRole('navigation', { name: 'Work panel sections' })).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Plan' }).getAttribute('aria-current')).toBe('page')
    expect(screen.getByRole('button', { name: 'Files' })).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Changes' })).toBeTruthy()
    expect(screen.queryByRole('button', { name: 'Artifacts' })).toBeNull()

    fireEvent.click(screen.getByRole('button', { name: 'Files' }))
    expect(onSwitchTab).toHaveBeenCalledWith('files')

    fireEvent.click(screen.getByTestId('workspace-backdrop'))
    expect(onClose).toHaveBeenCalledOnce()
  })
})
