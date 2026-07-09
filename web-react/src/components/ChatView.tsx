/**
 * ChatView — the main chat column. The TopBar (floated top-right) carries the
 * panel menu + connection status, so there is NO separate header here (matches
 * the Vue app, where the chat canvas has no header bar). Just GoalBanner +
 * Thread + ChatInput.
 */

import { Thread } from 'jcode-ui'
import { GoalBanner } from './GoalBanner'
import { ChatInput } from './ChatInput'

export interface ChatViewProps {
  /** Read-only mode (automation-run replay): no composer, no follow. */
  readOnly?: boolean
}

export function ChatView({ readOnly }: ChatViewProps) {
  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <GoalBanner />
      <div className="min-h-0 flex-1">
        <Thread overscanBottom={readOnly ? 8 : 96} />
      </div>
      {!readOnly && (
        <div className="border-t border-[var(--color-border)] bg-[var(--color-surface)] px-3 py-2.5">
          <ChatInput onSent={() => { /* timeline auto-follows via useStreamFollow */ }} />
        </div>
      )}
    </div>
  )
}
