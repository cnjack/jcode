/** CommandPalette — ⌘K palette (skeleton). Actions: new chat, switch view, open settings. */

import { useEffect } from 'react'
import { useAppDispatch, useAppSelector } from '../app/hooks'
import { uiActions, sessionActions, chatActions } from '../app/store'

export function CommandPalette() {
  const dispatch = useAppDispatch()
  const sessions = useAppSelector((s) => s.session.sessions)

  useEffect(() => {
    function onKey(e: KeyboardEvent) {
      if (e.key === 'Escape') dispatch(uiActions.setPaletteOpen(false))
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [dispatch])

  const actions: { label: string; run: () => void }[] = [
    { label: 'New chat', run: () => goNewChat() },
    { label: 'Go to Chat', run: () => dispatch(uiActions.setView('chat')) },
    { label: 'Go to Automations', run: () => dispatch(uiActions.setView('automations')) },
    { label: 'Go to Channels', run: () => dispatch(uiActions.setView('channels')) },
    { label: 'Open Settings', run: () => dispatch(uiActions.setSettingsOpen(true)) },
    ...sessions.slice(0, 8).map((s) => ({
      label: `Open: ${s.title || 'Untitled'}`,
      run: () => {
        dispatch(chatActions.clearChat())
        dispatch(sessionActions.setCurrentSession(s.uuid))
        dispatch(uiActions.setView('chat'))
      },
    })),
  ]

  function goNewChat() {
    dispatch(chatActions.clearChat())
    dispatch(sessionActions.setCurrentSession(''))
    dispatch(uiActions.setView('chat'))
  }

  return (
    <div
      className="fixed inset-0 z-[var(--z-modal)] flex items-start justify-center bg-[var(--backdrop)] pt-[15vh]"
      onClick={() => dispatch(uiActions.setPaletteOpen(false))}
    >
      <div
        className="w-full max-w-lg overflow-hidden rounded-[var(--radius-xl)] border border-[var(--color-border)] bg-[var(--color-surface)] shadow-[var(--shadow-lg)]"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="border-b border-[var(--color-border)] px-3 py-2 text-xs text-[var(--color-muted-foreground)]">
          Command palette
        </div>
        <ul className="max-h-80 overflow-y-auto py-1">
          {actions.map((a, i) => (
            <li key={i}>
              <button
                type="button"
                onClick={() => {
                  a.run()
                  dispatch(uiActions.setPaletteOpen(false))
                }}
                className="block w-full px-3 py-1.5 text-left text-sm hover:bg-[var(--neutral-wash-soft)]"
              >
                {a.label}
              </button>
            </li>
          ))}
        </ul>
      </div>
    </div>
  )
}
