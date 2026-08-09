import type { RootState } from './store'

/**
 * The task chrome belongs to a real conversation, not the fresh-task welcome
 * screen. A newly allocated session id is not enough: the backend assigns one
 * before the first message, while the task remains intentionally unpersisted.
 */
export function selectShowSessionChrome(state: RootState): boolean {
  if (state.ui.activeView !== 'chat') return false

  const sessionId = state.session.currentSessionId
  const hasPersistedSession = !!sessionId && (
    state.session.tasks.some((task) => task.uuid === sessionId) ||
    state.session.sessions.some((session) => session.uuid === sessionId)
  )

  return (
    state.chat.sessionLoading ||
    state.chat.isRunning ||
    state.chat.timeline.length > 0 ||
    hasPersistedSession
  )
}
