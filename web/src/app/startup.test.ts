import { describe, expect, it } from 'vitest'
import {
  coldDesktopFreshTarget,
  initializePageBootState,
  markPageBootComplete,
  pageBootCompleted,
  shouldBlockAppShortcut,
  shouldStartFreshOnWindowReopen,
  shouldShowBootScreen,
  startupLanding,
} from './startup'

function memoryStorage() {
  const values = new Map<string, string>()
  return {
    getItem: (key: string) => values.get(key) ?? null,
    setItem: (key: string, value: string) => { values.set(key, value) },
    removeItem: (key: string) => { values.delete(key) },
  }
}

describe('startup conversation policy', () => {
  it('distinguishes a cold open from a same-tab reload', () => {
    const storage = memoryStorage()
    expect(pageBootCompleted(storage, 'reload')).toBe(false)
    markPageBootComplete(storage)
    expect(pageBootCompleted(storage, 'reload')).toBe(true)
  })

  it('treats duplicated-tab storage as cold unless Navigation Timing says reload', () => {
    const copiedStorage = memoryStorage()
    markPageBootComplete(copiedStorage)

    expect(pageBootCompleted(copiedStorage, 'navigate')).toBe(false)
    expect(pageBootCompleted(copiedStorage, 'back_forward')).toBe(false)
    expect(pageBootCompleted(copiedStorage, undefined)).toBe(false)
  })

  it('clears a copied cold marker so Retry reload cannot restore the old task', () => {
    const copiedStorage = memoryStorage()
    markPageBootComplete(copiedStorage)

    expect(initializePageBootState(copiedStorage, 'navigate')).toBe(false)
    expect(initializePageBootState(copiedStorage, 'reload')).toBe(false)
  })

  it('preserves a successful marker across a genuine reload', () => {
    const storage = memoryStorage()
    markPageBootComplete(storage)

    expect(initializePageBootState(storage, 'reload')).toBe(true)
    expect(pageBootCompleted(storage, 'reload')).toBe(true)
  })

  it('provisions a new task instead of restoring an indexed session on a cold open', () => {
    expect(startupLanding(true, 'old-session', true, false, false)).toBe('provision')
  })

  it('restores on warm reload only when the session still exists in an index', () => {
    expect(startupLanding(false, 'active-session', true, false, false)).toBe('restore')
    expect(startupLanding(false, '', true, false, false)).toBe('provision')
  })

  it('reuses an unrecorded bootstrap task but never mistakes unindexed running work for one', () => {
    expect(startupLanding(true, 'bootstrap-session', false, false, true)).toBe('reuse_bootstrap')
    expect(startupLanding(false, 'bootstrap-session', false, false, true)).toBe('reuse_bootstrap')
    expect(startupLanding(false, 'running-session', false, true, true)).toBe('provision')
  })

  it('uses the explicit health contract when indexes fail and fails closed on old servers', () => {
    expect(startupLanding(false, 'durable-session', false, false, false)).toBe('restore')
    expect(startupLanding(false, 'unknown-session', false, false, undefined)).toBe('provision')
  })

  it('keeps running or approval-blocked work visible when Desktop reopens', () => {
    expect(shouldStartFreshOnWindowReopen(true)).toBe(false)
    expect(shouldStartFreshOnWindowReopen(false)).toBe(true)
  })

  it('blocks the product shell throughout initial boot and fresh-task handoff', () => {
    expect(shouldShowBootScreen(true, false)).toBe(true)
    expect(shouldShowBootScreen(false, true)).toBe(true)
    expect(shouldShowBootScreen(true, true)).toBe(true)
    expect(shouldShowBootScreen(false, false)).toBe(false)
  })

  it('suppresses app shortcuts while the shell is blocked', () => {
    expect(shouldBlockAppShortcut(true, true, 'n', false)).toBe(true)
    expect(shouldBlockAppShortcut(true, true, 'O', true)).toBe(true)
    expect(shouldBlockAppShortcut(true, true, ',', false)).toBe(true)
    expect(shouldBlockAppShortcut(false, true, 'n', false)).toBe(false)
    expect(shouldBlockAppShortcut(true, false, 'n', false)).toBe(false)
    expect(shouldBlockAppShortcut(true, true, 'x', false)).toBe(false)
  })

  it('preserves the last Desktop workspace without reopening its conversation', () => {
    expect(coldDesktopFreshTarget(true, true, '/work/project', 'project')).toEqual({
      projectPath: '/work/project',
      workspaceKind: 'project',
    })
    expect(coldDesktopFreshTarget(true, true, '/Users/test/.jcode/workspace/2026-08-26-001', 'scratch')).toEqual({
      workspaceKind: 'scratch',
    })
    expect(coldDesktopFreshTarget(false, true, '/work/project', 'project')).toBeUndefined()
    expect(coldDesktopFreshTarget(true, false, '/work/project', 'project')).toBeUndefined()
  })
})
