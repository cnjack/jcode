/**
 * Thread — styled conversation container (wraps the headless Thread primitive).
 *
 * Wires up the per-kind renderers (Message / ToolCallCard / ApprovalBanner /
 * ActivityGroupCard / TurnChangesCard), coalescing ALL adjacent tools into
 * activity groups via mapItems (groupActivityTimeline). The legacy
 * exploring/batch branches remain for external hosts that feed those kinds
 * directly — Thread itself no longer produces them.
 */

import { useCallback, type ReactNode } from 'react'
import { Thread as ThreadPrimitive } from 'jcode-ui-core/primitives'
import { useRuntimeState } from 'jcode-ui-core/runtime'
import {
  isMessageItem,
  isToolItem,
  isApprovalItem,
  isActivityItem,
  isExploringItem,
  isBatchItem,
  isTurnChangesItem,
  groupActivityTimeline,
  appendTurnChangeSummaries,
} from 'jcode-ui-core'
import type { ThreadItem } from 'jcode-ui-core'
import { Message } from './Message.js'
import { ToolCallCard } from './ToolCallCard.js'
import { ApprovalBanner } from './ApprovalBanner.js'
import { ActivityGroupCard } from './ActivityGroupCard.js'
import { ExploringGroupCard } from './ExploringGroupCard.js'
import { ToolBatchGroupCard } from './ToolBatchGroup.js'
import { TurnChangesCard } from './TurnChangesCard.js'

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
  /** Label for the default pending indicator ("Thinking"). Ignored when
   *  renderPending is set. The library has no i18n; hosts pass a translated
   *  string here. */
  pendingLabel?: string
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
  pendingLabel,
  className,
  overscanBottom,
}: ThreadProps): ReactNode {
  const { isRunning } = useRuntimeState()
  // Activity coalescing (batches absorbed, ALL adjacent tools grouped), then
  // per-turn "Changed N files" summaries (the last turn stays summary-free
  // while the agent is working).
  const mapItems = useCallback(
    (items: ThreadItem[]) => appendTurnChangeSummaries(groupActivityTimeline(items), { isRunning }),
    [isRunning],
  )

  return (
    <ThreadPrimitive
      virtualize={virtualize}
      className={`jcode-thread messages-feather min-h-0 w-full flex-1 scroll-smooth ${className ?? ''}`}
      overscanBottom={overscanBottom ?? 24}
      mapItems={mapItems}
      renderItem={(item) => renderItem(item, isRunning)}
      renderPending={renderPending ?? (() => <DefaultPending label={pendingLabel} />)}
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
  if (isActivityItem(item)) {
    return (
      <div className="jcode-chat-col">
        <ActivityGroupCard group={item.data} className="jcode-gutter" />
      </div>
    )
  }
  // Legacy kinds — Thread no longer produces them; kept for hosts that do.
  if (isExploringItem(item)) {
    return (
      <div className="jcode-chat-col">
        <ExploringGroupCard group={item.data} className="jcode-gutter" />
      </div>
    )
  }
  if (isBatchItem(item)) {
    return (
      <div className="jcode-chat-col">
        <ToolBatchGroupCard group={item.data} className="jcode-gutter" />
      </div>
    )
  }
  if (isTurnChangesItem(item)) {
    return (
      <div className="jcode-chat-col">
        <TurnChangesCard summary={item.data} className="jcode-gutter" />
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

function DefaultPending({ label }: { label?: string }): ReactNode {
  const text = label ?? 'Thinking'
  return (
    <div
      className="jcode-pending jcode-chat-col"
      role="status"
      aria-live="polite"
      aria-label={text}
    >
      <div className="jcode-pending__inner jcode-gutter">
        <span className="jcode-pending__ring" aria-hidden="true">
          <span className="jcode-pending-ring" />
        </span>
        <span className="jcode-pending__label">{text}</span>
      </div>
    </div>
  )
}
