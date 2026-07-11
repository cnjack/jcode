/**
 * Thread — styled conversation container (wraps the headless Thread primitive).
 *
 * Wires up the per-kind renderers (Message / ToolCallCard / ApprovalBanner /
 * ExploringGroupCard), applying exploring-group coalescing via mapItems.
 */

import { useCallback, type ReactNode } from 'react'
import { Thread as ThreadPrimitive } from 'jcode-ui-core/primitives'
import { useRuntimeState } from 'jcode-ui-core/runtime'
import {
  isMessageItem,
  isToolItem,
  isApprovalItem,
  isExploringItem,
  groupExploringTimeline,
} from 'jcode-ui-core'
import type { ThreadItem } from 'jcode-ui-core'
import { Message } from './Message.js'
import { ToolCallCard } from './ToolCallCard.js'
import { ApprovalBanner } from './ApprovalBanner.js'
import { ExploringGroupCard } from './ExploringGroupCard.js'

export interface ThreadProps {
  /** Disable virtualization (short/replay timelines). Default true. */
  virtualize?: boolean
  /** Empty-state node (typically `<ThreadWelcome>`). */
  emptyState?: ReactNode
  /** Follow-up content under the last turn when idle (typically
   *  `<Suggestions scroll>`), aligned to the chat column. */
  suggestions?: ReactNode
  /** Override the pending ("Thinking…") indicator. */
  renderPending?: () => ReactNode
  /** className passthrough for the scroll container. */
  className?: string
  /** Extra bottom padding (px) to clear a sticky composer. */
  overscanBottom?: number
}

export function Thread({
  virtualize,
  emptyState,
  suggestions,
  renderPending,
  className,
  overscanBottom,
}: ThreadProps): ReactNode {
  const { isRunning } = useRuntimeState()
  const mapItems = useCallback((items: ThreadItem[]) => groupExploringTimeline(items), [])

  return (
    <ThreadPrimitive
      virtualize={virtualize}
      className={`jcode-thread messages-feather min-h-0 w-full flex-1 scroll-smooth ${className ?? ''}`}
      overscanBottom={overscanBottom ?? 24}
      mapItems={mapItems}
      renderItem={(item) => renderItem(item, isRunning)}
      renderPending={renderPending ?? DefaultPending}
      renderEmpty={emptyState ? () => emptyState : undefined}
      renderFooter={
        suggestions
          ? () => (
              <div className="jcode-thread-followups jcode-chat-col">
                <div className="jcode-gutter">{suggestions}</div>
              </div>
            )
          : undefined
      }
    />
  )
}

function renderItem(item: ThreadItem, isRunning: boolean): ReactNode {
  if (isMessageItem(item)) {
    return (
      <Message
        message={item.data}
        canEdit={item.data.role === 'user' && !isRunning}
      />
    )
  }
  if (isExploringItem(item)) {
    return (
      <div className="jcode-chat-col">
        <ExploringGroupCard group={item.data} className="jcode-gutter" />
      </div>
    )
  }
  if (isToolItem(item)) {
    return (
      <div className="jcode-chat-col">
        <ToolCallCard tool={item.data} className="jcode-gutter" />
      </div>
    )
  }
  if (isApprovalItem(item)) {
    return (
      <div className="jcode-chat-col">
        <div className="jcode-gutter jcode-approval-slot">
          <ApprovalBanner approval={item.data} />
        </div>
      </div>
    )
  }
  return null
}

function DefaultPending(): ReactNode {
  return (
    <div
      className="jcode-pending jcode-chat-col"
      role="status"
      aria-live="polite"
      aria-label="Thinking…"
    >
      <div className="jcode-pending__inner jcode-gutter">
        <span className="jcode-pending__dots" aria-hidden="true">
          <span className="jcode-pending-dot" />
          <span className="jcode-pending-dot" />
          <span className="jcode-pending-dot" />
        </span>
        <span className="jcode-pending__label">Thinking</span>
      </div>
    </div>
  )
}
