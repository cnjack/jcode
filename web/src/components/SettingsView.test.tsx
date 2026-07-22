/**
 * SettingsView render tests (M18): the section rail, default General section,
 * tab switching to the Cloud section, and store-driven deep links.
 *
 * The view is rendered against the real singleton store + real i18n resources
 * (English). Backend calls fail in jsdom (no server), which is exactly the
 * degradation path the components already tolerate: Cloud shows the logged-out
 * login entry, General still renders its local preferences.
 */

import { describe, it, expect, beforeEach } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { Provider } from 'react-redux'
import { i18n } from '../i18n'
import { store, uiActions } from '../app/store'
import { SettingsView } from './SettingsView'

function renderView() {
  return render(
    <Provider store={store}>
      <SettingsView />
    </Provider>,
  )
}

beforeEach(async () => {
  cleanup()
  await i18n.changeLanguage('en')
  store.dispatch(uiActions.setView('settings'))
  store.dispatch(uiActions.setSettingsTab('general'))
})

describe('SettingsView', () => {
  it('renders the section rail with every settings section', () => {
    renderView()
    // The rail is labelled navigation.
    expect(screen.getByRole('navigation', { name: 'Settings' })).toBeTruthy()
    for (const label of [
      'General',
      'Cloud',
      'Appearance',
      'Providers',
      'MCP Servers',
      'Skills',
      'Memory',
      'Browser',
      'Computer',
      'SSH',
      'Channels',
      'Shortcuts',
      'Usage',
      'Developer',
    ]) {
      expect(screen.getByRole('button', { name: label })).toBeTruthy()
    }
    // Way back to the workspace.
    expect(screen.getByRole('button', { name: /Back to workspace/ })).toBeTruthy()
  })

  it('shows the General section by default', () => {
    renderView()
    // Rail marks General active.
    expect(screen.getByRole('button', { name: 'General' }).getAttribute('aria-current')).toBe('page')
    // Existing general settings items (moved here, not re-invented).
    expect(screen.getByText('Default auto-approve')).toBeTruthy()
    expect(screen.queryByText('Sync new sessions to the cloud')).toBeNull()
  })

  it('switches to the Cloud section when the rail tab is clicked', async () => {
    renderView()
    fireEvent.click(screen.getByRole('button', { name: 'Cloud' }))
    expect(store.getState().ui.settingsTab).toBe('cloud')
    expect(screen.getByRole('button', { name: 'Cloud' }).getAttribute('aria-current')).toBe('page')
    // Logged out (no backend in tests) → the device-code login entry point.
    await waitFor(() => {
      expect(screen.getByText('Log in to use this device remotely from the cloud console and mobile app.')).toBeTruthy()
      expect(screen.getByRole('button', { name: 'Log in to cloud' })).toBeTruthy()
    })
  })

  it('honours a store deep link that pre-selects the Cloud tab', async () => {
    store.dispatch(uiActions.setSettingsTab('cloud'))
    renderView()
    expect(screen.getByRole('button', { name: 'Cloud' }).getAttribute('aria-current')).toBe('page')
    await waitFor(() => expect(screen.getByRole('button', { name: 'Log in to cloud' })).toBeTruthy())
  })

  it('routes back to the chat view from the rail back action', () => {
    renderView()
    fireEvent.click(screen.getByRole('button', { name: /Back to workspace/ }))
    expect(store.getState().ui.activeView).toBe('chat')
  })
})
