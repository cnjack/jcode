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
  /** className passthrough. */
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
  return (
    <ThreadPrimitive
      virtualize={virtualize}
      className={`jcode-thread ${className ?? ''}`}
      overscanBottom={overscanBottom}
      renderItem={renderItem}
      renderPending={renderPending ?? DefaultPending}
      renderEmpty={emptyState ? () => emptyState : undefined}
    />
  )
}

function renderItem(item: ThreadItem): ReactNode {
  if (isMessageItem(item)) {
    return <Message key={item.seq} message={item.data} canEdit={item.data.role === 'user'} />
  }
  if (isToolItem(item)) {
    return <ToolCallCard key={item.seq} tool={item.data} />
  }
  if (isApprovalItem(item)) {
    return <ApprovalBanner key={item.seq} approval={item.data} />
  }
  return null
}

function DefaultPending(): ReactNode {
  return (
    <div className="jcode-pending flex items-center gap-2 px-4 py-2 text-[var(--color-muted-foreground)]">
      <span className="inline-block h-2 w-2 animate-pulse rounded-full bg-[var(--color-primary)]" />
      <span>Thinking…</span>
    </div>
  )
}
