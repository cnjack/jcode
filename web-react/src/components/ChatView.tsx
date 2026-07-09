/**
 * ChatView — the main chat column.
 *
 * Two states (matches Vue App.vue):
 *   - Welcome: no messages → centered hero ([ J CODE ]) + centered composer.
 *   - Conversation: messages exist → scrollable timeline + docked composer.
 *
 * The TopBar (floated top-right) carries the panel menu + connection status,
 * so there is NO separate header here.
 */

import { Thread } from 'jcode-ui'
import { useAppSelector } from '../app/hooks'
import { GoalBanner } from './GoalBanner'
import { ChatInput } from './ChatInput'

export interface ChatViewProps {
  /** Read-only mode (automation-run replay): no composer, no follow. */
  readOnly?: boolean
}

export function ChatView({ readOnly }: ChatViewProps) {
  const hasMessages = useAppSelector((s) => s.chat.timeline.length > 0)

  if (readOnly) {
    return (
      <div className="flex min-h-0 flex-1 flex-col">
        <GoalBanner />
        <div className="min-h-0 flex-1">
          <Thread overscanBottom={8} />
        </div>
      </div>
    )
  }

  // Welcome screen: centered hero + composer (no messages yet).
  if (!hasMessages) {
    return (
      <div className="flex flex-1 flex-col items-center overflow-y-auto px-6">
        {/* Top half: hero floats above the centered composer. */}
        <div className="flex min-h-0 flex-1 flex-col items-center justify-end pb-10">
          <div className="select-none pb-4 text-center">
            <span className="font-mono text-3xl font-bold tracking-tight" style={{ color: 'var(--color-muted-foreground)' }}>
              <span style={{ opacity: 0.4 }}>[</span>
              <span style={{ color: 'var(--color-primary)' }}>J</span>
              <span style={{ color: 'var(--color-foreground)' }}>CODE</span>
              <span style={{ opacity: 0.4 }}>)</span>
            </span>
          </div>
          <h2 className="text-xl font-semibold text-[var(--color-foreground)]">Start a conversation</h2>
          <p className="mt-2 text-sm text-[var(--color-muted-foreground)]">
            Type <kbd className="rounded border border-[var(--color-border)] px-1.5 py-0.5 font-mono text-xs">/</kbd> for commands, or just start typing.
          </p>
        </div>
        {/* Centered composer */}
        <div className="w-full max-w-2xl pb-10">
          <ChatInput onSent={() => { /* timeline auto-follows */ }} />
        </div>
        {/* Bottom half balances the center */}
        <div className="min-h-0 flex-1" aria-hidden="true" />
      </div>
    )
  }

  // Active conversation: scrollable timeline + docked composer.
  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <GoalBanner />
      <div className="min-h-0 flex-1">
        <Thread overscanBottom={96} />
      </div>
      <div className="border-t border-[var(--color-border)] bg-[var(--color-surface)] px-3 py-2.5">
        <ChatInput onSent={() => { /* timeline auto-follows via useStreamFollow */ }} />
      </div>
    </div>
  )
}
