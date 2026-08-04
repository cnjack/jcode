/**
 * ui routing for the M18 settings view: settings is a first-class
 * `activeView`, and `settingsTab` carries deep links (e.g. CloudBadge →
 * the Cloud tab).
 */

import { describe, it, expect, afterEach } from 'vitest'
import { buildPlanHistory, chatActions, store, uiActions } from './store'

afterEach(() => {
  store.dispatch(uiActions.setView('chat'))
  store.dispatch(uiActions.setSettingsTab('general'))
  store.dispatch(chatActions.clearChat())
})

describe('plan history', () => {
  it('reconstructs durable todo snapshots and legacy plans without duplicate revisions', () => {
    const history = buildPlanHistory([
      {
        type: 'todo_snapshot',
        timestamp: '2026-08-04T08:00:00Z',
        todos: [{ id: 1, title: 'Inspect layout', status: 'in_progress' }],
      },
      {
        type: 'todo_snapshot',
        timestamp: '2026-08-04T08:01:00Z',
        todos: [{ id: 1, title: 'Inspect layout', status: 'in_progress' }],
      },
      {
        type: 'plan_update',
        timestamp: '2026-08-04T08:02:00Z',
        plan_status: 'approved',
        plan_title: 'Desktop redesign',
        plan_content: '1. Inspect\n2. Implement',
      },
      { type: 'todo_snapshot', timestamp: '2026-08-04T08:03:00Z', todos: [] },
    ])

    expect(history).toHaveLength(2)
    expect(history[0].title).toBe('Inspect layout')
    expect(history[1]).toMatchObject({ title: 'Desktop redesign', status: 'approved' })
  })

  it('merges status revisions of the same todo plan into its latest snapshot', () => {
    const history = buildPlanHistory([
      {
        type: 'todo_snapshot',
        timestamp: '2026-08-04T08:00:00Z',
        todos: [
          { id: 1, title: 'Create tests', status: 'in_progress' },
          { id: 2, title: 'Verify state', status: 'pending' },
        ],
      },
      {
        type: 'todo_snapshot',
        timestamp: '2026-08-04T08:05:00Z',
        todos: [
          { id: 2, title: 'Verify state', status: 'completed' },
          { id: 1, title: 'Create tests', status: 'completed' },
        ],
      },
    ])

    expect(history).toHaveLength(1)
    expect(history[0]).toMatchObject({ status: 'completed', timestamp: Date.parse('2026-08-04T08:05:00Z') })
    expect(history[0].todos.every((todo) => todo.status === 'completed')).toBe(true)
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
