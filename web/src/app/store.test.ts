/**
 * ui routing for the M18 settings view: settings is a first-class
 * `activeView`, and `settingsTab` carries deep links (e.g. CloudBadge →
 * the Cloud tab).
 */

import { describe, it, expect, afterEach } from 'vitest'
import { store, uiActions } from './store'

afterEach(() => {
  store.dispatch(uiActions.setView('chat'))
  store.dispatch(uiActions.setSettingsTab('general'))
})

describe('ui routing (M18 settings view)', () => {
  it('routes to the settings view and back to chat', () => {
    expect(store.getState().ui.activeView).toBe('chat')
    store.dispatch(uiActions.setView('settings'))
    expect(store.getState().ui.activeView).toBe('settings')
    store.dispatch(uiActions.setView('chat'))
    expect(store.getState().ui.activeView).toBe('chat')
  })

  it('carries a settings-tab deep link alongside the view', () => {
    store.dispatch(uiActions.setSettingsTab('cloud'))
    store.dispatch(uiActions.setView('settings'))
    expect(store.getState().ui.activeView).toBe('settings')
    expect(store.getState().ui.settingsTab).toBe('cloud')
  })

  it('defaults the settings tab to general', () => {
    expect(store.getState().ui.settingsTab).toBe('general')
  })
})
