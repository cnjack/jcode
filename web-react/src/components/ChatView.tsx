/**
 * ChatView — the main chat column. Composes jcode-ui's Thread + ChatInput with
 * a header (project path, connection status) and a GoalBanner. The data flows
 * from the RTK store via the RuntimeProvider wired in App.tsx.
 */

import { Thread } from 'jcode-ui'
import { GoalBanner } from './GoalBanner'
import { ProjectHeader } from './ProjectHeader'
import { ChatInput } from './ChatInput'

export interface ChatViewProps {
  /** Read-only mode (automation-run replay): no composer, no follow. */
  readOnly?: boolean
}

export function ChatView({ readOnly }: ChatViewProps) {
  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <ProjectHeader />
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
