/**
 * Thread — the headless conversation container.
 *
 * Owns: virtualized rendering of `items`, auto-scroll-follow semantics, and the
 * "Thinking…" trailing indicator. Does NOT own styling — it renders children-in-
 * position and exposes render-prop slots for each item kind. The styled
 * `jcode-ui` `Thread` wraps this with token-driven classes.
 *
 * Virtualization: backed by @tanstack/react-virtual. `virtualize` defaults true
 * (the Vue version was unvirtualized and this was a known weakness); pass
 * `virtualize={false}` for short/replay timelines where DOM simplicity matters.
 */

import { Fragment, useMemo } from 'react'
import type { ReactNode } from 'react'
import { useVirtualizer } from '@tanstack/react-virtual'
import { useRuntimeState } from '../runtime/context.js'
import { useAutoScroll, useStreamFollow } from '../hooks/index.js'
import type { ThreadItem } from '../types/index.js'

export interface ThreadRenderSlots {
  /** Render a single timeline item. Dispatch on `item.kind` to pick a sub-view. */
  renderItem: (item: ThreadItem) => ReactNode
  /** Optional trailing "agent is working" row shown when isRunning. */
  renderPending?: () => ReactNode
  /** Optional empty state (no items at all). */
  renderEmpty?: () => ReactNode
}

export interface ThreadProps extends ThreadRenderSlots {
  /** Enable windowed virtualization. Default true. Disable for short/replay
   *  timelines. */
  virtualize?: boolean
  /** Estimated row height in px (virtualizer uses this before measure). */
  estimateSize?: number
  /** Bottom-edge threshold in px within which we consider the user "at bottom". */
  scrollThreshold?: number
  /** Extra padding after the last item (e.g. to clear a sticky composer). */
  overscanBottom?: number
  /** className passthrough for the scroll container. */
  className?: string
  /** Accessibility role for the scroll container. */
  role?: string
  /** Ref callback for the scroll container (for parent scroll control). */
  containerRef?: (el: HTMLElement | null) => void
}

export function Thread({
  renderItem,
  renderPending,
  renderEmpty,
  virtualize = true,
  estimateSize = 80,
  scrollThreshold = 80,
  overscanBottom = 0,
  className,
  role = 'log',
  containerRef,
}: ThreadProps): ReactNode {
  const { items, isRunning } = useRuntimeState()
  const autoScroll = useAutoScroll<HTMLDivElement>(scrollThreshold)

  // Stream-follow: a dep that changes whenever there's new/changed content.
  // items.length covers new rows; the last item's content covers streaming text.
  const last = items[items.length - 1]
  const followDep = useMemo(() => {
    if (!last) return items.length
    if (last.kind === 'message') return `${items.length}:${last.data.content.length}`
    return items.length
  }, [items, last])
  useStreamFollow(autoScroll, followDep)

  // Empty state.
  if (items.length === 0 && !isRunning && renderEmpty) {
    return <>{renderEmpty()}</>
  }

  if (!virtualize) {
    // IMPORTANT: do NOT use `contain: strict` here. Size containment collapses
    // the box to 0 when height is percentage/auto without a definite parent
    // size — which is exactly how most demos and short embeds mount Thread.
    return (
      <div
        ref={(el) => {
          ;(autoScroll.ref as React.MutableRefObject<HTMLDivElement | null>).current = el
          containerRef?.(el)
        }}
        className={className}
        role={role}
        onScroll={autoScroll.onScroll}
        style={{ overflowY: 'auto', minHeight: 0, height: '100%' } as React.CSSProperties}
      >
        {items.map((it) => (
          <Fragment key={it.seq}>{renderItem(it)}</Fragment>
        ))}
        {isRunning && renderPending?.()}
        {overscanBottom > 0 && <div style={{ height: overscanBottom }} aria-hidden />}
      </div>
    )
  }

  return (
    <VirtualizedThread
      items={items}
      isRunning={isRunning}
      renderItem={renderItem}
      renderPending={renderPending}
      estimateSize={estimateSize}
      overscanBottom={overscanBottom}
      className={className}
      role={role}
      autoScroll={autoScroll}
      containerRef={containerRef}
    />
  )
}

interface VirtualizedThreadProps {
  items: ThreadItem[]
  isRunning: boolean
  renderItem: (item: ThreadItem) => ReactNode
  renderPending?: () => ReactNode
  estimateSize: number
  overscanBottom: number
  className?: string
  role: string
  autoScroll: ReturnType<typeof useAutoScroll<HTMLDivElement>>
  containerRef?: (el: HTMLElement | null) => void
}

function VirtualizedThread({
  items,
  isRunning,
  renderItem,
  renderPending,
  estimateSize,
  overscanBottom,
  className,
  role,
  autoScroll,
  containerRef,
}: VirtualizedThreadProps): ReactNode {
  const parentRef = autoScroll.ref
  // count includes the trailing pending row + overscan spacer when present.
  const trailingCount = (isRunning && renderPending ? 1 : 0) + (overscanBottom > 0 ? 1 : 0)
  const count = items.length + trailingCount
  const rowVirtualizer = useVirtualizer({
    count,
    getScrollElement: () => parentRef.current,
    estimateSize: () => estimateSize,
    overscan: 8,
  })

  return (
    // Wrapper: the scroll element MUST resolve to a concrete pixel height for the
    // virtualizer to measure rows. flex:1 + min-height:0 fills flex parents;
    // height:100% covers block parents that already have a definite height.
    // Use layout/paint containment only — never size containment (collapses).
    <div
      style={{
        display: 'flex',
        flexDirection: 'column',
        width: '100%',
        height: '100%',
        minHeight: 0,
        flex: 1,
      }}
    >
      <div
        ref={(el) => {
          ;(parentRef as React.MutableRefObject<HTMLDivElement | null>).current = el
          containerRef?.(el)
        }}
        className={className}
        role={role}
        onScroll={autoScroll.onScroll}
        style={
          {
            overflowY: 'auto',
            contain: 'layout paint',
            flex: 1,
            minHeight: 0,
            height: '100%',
            position: 'relative',
          } as React.CSSProperties
        }
      >
        <div
          style={{
            height: Math.max(rowVirtualizer.getTotalSize(), 1),
            position: 'relative',
            width: '100%',
          }}
        >
          {rowVirtualizer.getVirtualItems().map((vi) => {
            // Trailing rows. Keys must be unique across the whole virtual list.
            if (vi.index === items.length && isRunning && renderPending) {
              return (
                <div
                  key={`__pending__${vi.index}`}
                  data-index={vi.index}
                  ref={rowVirtualizer.measureElement}
                  style={{
                    position: 'absolute',
                    top: 0,
                    left: 0,
                    width: '100%',
                    transform: `translateY(${vi.start}px)`,
                  }}
                >
                  {renderPending()}
                </div>
              )
            }
            if (vi.index >= items.length) {
              // overscan spacer
              return (
                <div
                  key={`__overscan__${vi.index}`}
                  style={{
                    position: 'absolute',
                    top: 0,
                    left: 0,
                    width: '100%',
                    height: overscanBottom,
                    transform: `translateY(${vi.start}px)`,
                  }}
                />
              )
            }
            const item = items[vi.index]
            return (
              <div
                key={`item-${item.seq}-${item.kind}-${vi.index}`}
                data-index={vi.index}
                ref={rowVirtualizer.measureElement}
                style={{
                  position: 'absolute',
                  top: 0,
                  left: 0,
                  width: '100%',
                  transform: `translateY(${vi.start}px)`,
                }}
              >
                {renderItem(item)}
              </div>
            )
          })}
        </div>
      </div>
    </div>
  )
}
