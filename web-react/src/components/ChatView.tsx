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
import { useTranslation } from 'react-i18next'
import { useAppSelector } from '../app/hooks'
import { GoalBanner } from './GoalBanner'
import { ChatInput } from './ChatInput'

export interface ChatViewProps {
  /** Read-only mode (automation-run replay): no composer, no follow. */
  readOnly?: boolean
}

export function ChatView({ readOnly }: ChatViewProps) {
  const { t } = useTranslation()
  const hasMessages = useAppSelector((s) => s.chat.timeline.length > 0)
  const projectPath = useAppSelector((s) => s.session.projectPath)
  const project = projectName(projectPath) || 'jcode'

  if (readOnly) {
    return (
      <div className="chat-panel flex min-h-0 flex-1 flex-col">
        <GoalBanner />
        <div className="min-h-0 flex-1">
          <Thread overscanBottom={8} />
        </div>
      </div>
    )
  }

  // Welcome screen: centered hero + composer (no messages yet).
  if (!hasMessages) {
    const subtitle = t('welcome.subtitle')
    const title = t('welcome.startIn').replace('{project}', project)
    const [subtitleBefore, subtitleAfter] = subtitle.split('{kbd}')

    return (
      <div className="chat-panel welcome flex flex-1 flex-col items-center overflow-y-auto px-6">
        <div className="welcome-aura" aria-hidden="true" />
        {/* Top half: hero floats above the centered composer. */}
        <div className="welcome-hero flex min-h-0 flex-1 flex-col items-center justify-end pb-10">
          <div className="welcome-logo select-none">
            <span className="wl-dim">[</span>
            <span className="wl-j">J</span>
            <span className="wl-fg">CODE</span>
            <span className="wl-dim">]</span>
          </div>
          <h2 className="welcome-title">{title}</h2>
          <p className="welcome-sub">
            {subtitleBefore}
            <kbd className="welcome-kbd">/</kbd>
            {subtitleAfter}
          </p>
        </div>
        {/* Centered composer */}
        <div className="welcome-composer w-full max-w-2xl">
          <ChatInput pickerPlacement="bottom" onSent={() => { /* timeline auto-follows */ }} />
        </div>
        {/* Bottom half balances the center */}
        <div className="min-h-0 flex-1" aria-hidden="true" />
      </div>
    )
  }

  // Active conversation: scrollable timeline + docked composer.
  return (
    <div className="chat-panel flex min-h-0 flex-1 flex-col">
      <GoalBanner />
      <div className="min-h-0 flex-1">
        <Thread overscanBottom={96} />
      </div>
      <div className="border-t border-[var(--color-border)] px-3 py-2.5">
        <ChatInput onSent={() => { /* timeline auto-follows via useStreamFollow */ }} />
      </div>
    </div>
  )
}

function projectName(path: string): string {
  if (!path) return ''
  if (path.startsWith('ssh://') || path.startsWith('docker://')) {
    const clean = path.replace(/^ssh:\/\//, '').replace(/^docker:\/\//, '')
    const parts = clean.split('/').filter(Boolean)
    return parts[parts.length - 1] || clean
  }
  const parts = path.split('/').filter(Boolean)
  return parts[parts.length - 1] || path
}
