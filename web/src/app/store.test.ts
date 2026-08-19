/**
 * ui routing for the M18 settings view: settings is a first-class
 * `activeView`, and `settingsTab` carries deep links (e.g. CloudBadge →
 * the Cloud tab).
 */

import { describe, it, expect, afterEach } from 'vitest'
import { sessionActions, store, uiActions } from './store'

afterEach(() => {
  store.dispatch(uiActions.setView('chat'))
  store.dispatch(uiActions.setSettingsTab('general'))
  store.dispatch(sessionActions.setTasks([]))
})

describe('workspace activity classification', () => {
  it('uses authoritative task metadata for a background scratch path', () => {
    const scratchPath = '/tmp/.jcode/workspace/2026-08-19-009'
    store.dispatch(sessionActions.setProjectPath('/work/current'))
    store.dispatch(sessionActions.setWorkspaceKind('project'))
    store.dispatch(sessionActions.setTasks([{
      uuid: 'scratch-background',
      project: scratchPath,
      workspace_kind: 'scratch',
      created_at: '2026-08-19T10:00:00Z',
      provider: 'openai', model: 'gpt-5', title: 'Background scratch',
      pinned: false, archived: false, unread: false,
    }]))

    store.dispatch(sessionActions.touchProjectTime({ path: scratchPath, ts: '2026-08-19T10:01:00Z' }))

    expect(store.getState().session.projectKinds[scratchPath]).toBe('scratch')
  })
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
