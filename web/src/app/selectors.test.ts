import { describe, expect, it } from 'vitest'
import type { RootState } from './store'
import { selectShowSessionChrome } from './selectors'

function state({
  activeView = 'chat',
  currentSessionId = '',
  sessionLoading = false,
  isRunning = false,
  timelineLength = 0,
  taskIds = [],
  sessionIds = [],
}: {
  activeView?: RootState['ui']['activeView']
  currentSessionId?: string
  sessionLoading?: boolean
  isRunning?: boolean
  timelineLength?: number
  taskIds?: string[]
  sessionIds?: string[]
} = {}): RootState {
  return {
    ui: { activeView },
    chat: {
      sessionLoading,
      isRunning,
      timeline: Array.from({ length: timelineLength }),
    },
    session: {
      currentSessionId,
      tasks: taskIds.map((uuid) => ({ uuid })),
      sessions: sessionIds.map((uuid) => ({ uuid })),
    },
  } as unknown as RootState
}

describe('selectShowSessionChrome', () => {
  it('hides task controls for a fresh blank session, before and after id allocation', () => {
    expect(selectShowSessionChrome(state())).toBe(false)
    expect(selectShowSessionChrome(state({ currentSessionId: 'fresh-session' }))).toBe(false)
  })

  it('shows task controls as soon as conversation work exists', () => {
    expect(selectShowSessionChrome(state({ timelineLength: 1 }))).toBe(true)
    expect(selectShowSessionChrome(state({ isRunning: true }))).toBe(true)
  })

  it('keeps task controls visible while an existing conversation loads', () => {
    expect(selectShowSessionChrome(state({ sessionLoading: true }))).toBe(true)
  })

  it('keeps task controls for persisted sessions even when their history is empty', () => {
    expect(selectShowSessionChrome(state({
      currentSessionId: 'task-session',
      taskIds: ['task-session'],
    }))).toBe(true)
    expect(selectShowSessionChrome(state({
      currentSessionId: 'indexed-session',
      sessionIds: ['indexed-session'],
    }))).toBe(true)
  })

  it('never shows chat task controls in another view', () => {
    expect(selectShowSessionChrome(state({
      activeView: 'settings',
      timelineLength: 1,
    }))).toBe(false)
  })
})
