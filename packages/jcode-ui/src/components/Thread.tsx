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
  return (
    <ThreadPrimitive
      virtualize={virtualize}
      className={`jcode-thread messages-feather h-full scroll-smooth rounded-t-[13px] ${className ?? ''}`}
      overscanBottom={overscanBottom ?? 24}
      renderItem={(item) => renderItem(item, isRunning)}
      renderPending={renderPending ?? DefaultPending}
      renderEmpty={emptyState ? () => emptyState : undefined}
    />
  )
}

function renderItem(item: ThreadItem, isRunning: boolean): ReactNode {
  if (isMessageItem(item)) {
    return (
      <Message
        key={item.seq}
        message={item.data}
        canEdit={item.data.role === 'user' && !isRunning}
      />
    )
  }
  if (isToolItem(item)) {
    // pl-9 matches Vue App.vue: tools indent under the message content column.
    return (
      <div key={item.seq} className="chat-col">
        <ToolCallCard tool={item.data} className="pl-9" />
      </div>
    )
  }
  if (isApprovalItem(item)) {
    return (
      <div key={item.seq} className="chat-col">
        <div className="pl-9">
          <ApprovalBanner approval={item.data} />
        </div>
      </div>
    )
  }
  return null
}

function DefaultPending(): ReactNode {
  // Matches Vue's thinking footer: three pulsing dots + label, indented pl-9.
  return (
    <div
      className="jcode-pending chat-col flex select-none items-center gap-2.5 py-3 pl-[3.25rem]"
      role="status"
      aria-live="polite"
      aria-label="Thinking…"
    >
      <span className="flex gap-1" aria-hidden="true">
        <span
          className="h-1.5 w-1.5 animate-pulse rounded-full"
          style={{ background: 'var(--color-accent-neutral)', animationDelay: '0ms' }}
        />
        <span
          className="h-1.5 w-1.5 animate-pulse rounded-full"
          style={{ background: 'var(--color-accent-neutral)', animationDelay: '160ms' }}
        />
        <span
          className="h-1.5 w-1.5 animate-pulse rounded-full"
          style={{ background: 'var(--color-accent-neutral)', animationDelay: '320ms' }}
        />
      </span>
      <span className="text-[13px]" style={{ fontFamily: 'var(--font-sans)', color: 'var(--color-muted-foreground)' }}>
        Thinking…
      </span>
    </div>
  )
}
