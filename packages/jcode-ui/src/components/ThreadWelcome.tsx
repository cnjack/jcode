/**
 * ThreadWelcome — the empty-thread hero.
 *
 * Drops into `<Thread emptyState={…}>`: brand mark (slot), a one-line pitch,
 * and room for starter `<Suggestions>` below. Deliberately quiet — the
 * composer is the call to action, the welcome only orients.
 */

import { memo } from 'react'
import type { ReactNode } from 'react'

export interface ThreadWelcomeProps {
  /** Brand mark / logo slot. Default: a neutral chat glyph. */
  logo?: ReactNode
  /** Headline. */
  title?: string
  /** Supporting line under the headline. */
  subtitle?: string
  /** Extra content below (typically `<Suggestions>`). */
  children?: ReactNode
}

export const ThreadWelcome = memo(function ThreadWelcome({
  logo,
  title = 'Start a new conversation',
  subtitle,
  children,
}: ThreadWelcomeProps) {
  return (
    <div data-jcode-ui="" className="jcode-welcome jcode-chat-col" role="presentation">
      <div className="jcode-welcome__inner">
        <div className="jcode-welcome__logo" aria-hidden>
          {logo ?? <DefaultMark />}
        </div>
        <h2 className="jcode-welcome__title">{title}</h2>
        {subtitle && <p className="jcode-welcome__subtitle">{subtitle}</p>}
        {children && <div className="jcode-welcome__extra">{children}</div>}
      </div>
    </div>
  )
})

function DefaultMark() {
  return (
    <svg viewBox="0 0 32 32" width="40" height="40" aria-hidden>
      <rect
        x="3"
        y="3"
        width="26"
        height="26"
        rx="8"
        fill="var(--jcode-accent-wash)"
        stroke="var(--jcode-accent-border)"
      />
      <circle cx="11" cy="16" r="2" fill="var(--jcode-color-primary)" />
      <circle cx="16" cy="16" r="2" fill="var(--jcode-color-primary)" opacity="0.7" />
      <circle cx="21" cy="16" r="2" fill="var(--jcode-color-primary)" opacity="0.4" />
    </svg>
  )
}
