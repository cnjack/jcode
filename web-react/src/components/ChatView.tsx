/**
 * ChatView — the main chat column. Composes jcode-ui's Thread + ChatInput with
 * a header (project path, connection status) and a GoalBanner. The data flows
 * from the RTK store via the RuntimeProvider wired in App.tsx.
 */

import { Thread, ChatInput } from 'jcode-ui'
import { useAppSelector } from '../app/hooks'
import { GoalBanner } from './GoalBanner'
import { ProjectHeader } from './ProjectHeader'

export interface ChatViewProps {
  /** Read-only mode (automation-run replay): no composer, no follow. */
  readOnly?: boolean
}

export function ChatView({ readOnly }: ChatViewProps) {
  const slashCommands = useAppSelector((s) => s.chat.slashCommands)
  const imageSupport = useAppSelector((s) => s.model.imageSupport)

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <ProjectHeader />
      <GoalBanner />
      <div className="min-h-0 flex-1">
        <Thread overscanBottom={readOnly ? 8 : 96} />
      </div>
      {!readOnly && (
        <div className="border-t border-[var(--color-border)] bg-[var(--color-surface)] px-3 py-2.5">
          <ChatInput
            slashCommands={slashCommands.map((c) => ({ slash: c.slash, description: c.description }))}
            allowImages={imageSupport}
            onSent={() => {
              /* timeline auto-follows via useStreamFollow; no-op here */
            }}
          />
        </div>
      )}
    </div>
  )
}
