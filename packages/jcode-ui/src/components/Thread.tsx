/**
 * Thread — styled conversation container (wraps the headless Thread primitive).
 *
 * Wires up the per-kind renderers (Message / ToolCallCard / ApprovalBanner),
 * the "Thinking…" pending indicator, and the empty state. Consumers provide a
 * ChatRuntime via <RuntimeProvider> and drop this in. For custom item rendering,
 * use the headless primitive directly.
 */

import type { ReactNode } from 'react'
import { Thread as ThreadPrimitive } from 'jcode-ui-core/primitives'
import { useRuntimeState } from 'jcode-ui-core/runtime'
import { isMessageItem, isToolItem, isApprovalItem } from 'jcode-ui-core'
import type { ThreadItem } from 'jcode-ui-core'
import { Message } from './Message.js'
import { ToolCallCard } from './ToolCallCard.js'
import { ApprovalBanner } from './ApprovalBanner.js'

export interface ThreadProps {
  /** Disable virtualization (short/replay timelines). Default true. */
  virtualize?: boolean
  /** Empty-state node. */
  emptyState?: ReactNode
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
  renderPending,
  className,
  overscanBottom,
}: ThreadProps): ReactNode {
  const { isRunning } = useRuntimeState()
  // min-h-0 is required for flex children to shrink and scroll; h-full fills
  // definite parents. Avoid forcing height when the host uses content-sized embeds.
  return (
    <ThreadPrimitive
      virtualize={virtualize}
      className={`jcode-thread messages-feather min-h-0 w-full flex-1 scroll-smooth ${className ?? ''}`}
      overscanBottom={overscanBottom ?? 24}
      renderItem={(item) => renderItem(item, isRunning)}
      renderPending={renderPending ?? DefaultPending}
      renderEmpty={emptyState ? () => emptyState : undefined}
    />
  )
}

function renderItem(item: ThreadItem, isRunning: boolean): ReactNode {
  // Keys live on the Thread list containers (virtualizer / map) — do not set
  // keys here or React warns about duplicate sibling keys when seq collides.
  if (isMessageItem(item)) {
    return (
      <Message
        message={item.data}
        canEdit={item.data.role === 'user' && !isRunning}
      />
    )
  }
  if (isToolItem(item)) {
    // Indent under the avatar gutter so tools line up with message body.
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
  // Align with message body / tools (chat-col + gutter under the avatar).
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
