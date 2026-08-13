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

import { Fragment, useCallback, useLayoutEffect, useMemo, useRef } from 'react'
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
  /** Optional footer after the last item when idle (e.g. follow-up
   *  suggestions). Not rendered while running or when the thread is empty. */
  renderFooter?: () => ReactNode
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
  /**
   * Optional pure transform applied to runtime items before render (e.g.
   * exploring-group coalescing). Must not mutate the input array.
   */
  mapItems?: (items: ThreadItem[]) => ThreadItem[]
}

export function Thread({
  renderItem,
  renderPending,
  renderEmpty,
  renderFooter,
  virtualize = true,
  estimateSize = 80,
  scrollThreshold = 80,
  overscanBottom = 0,
  className,
  role = 'log',
  containerRef,
  mapItems,
}: ThreadProps): ReactNode {
  const { items: rawItems, isRunning } = useRuntimeState()
  const items = useMemo(
    () => (mapItems ? mapItems(rawItems) : rawItems),
    [rawItems, mapItems],
  )
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
        data-jcode-ui=""
        role={role}
        onScroll={autoScroll.onScroll}
        style={{ overflowY: 'auto', minHeight: 0, height: '100%' } as React.CSSProperties}
      >
        {items.map((it) => (
          <Fragment key={it.seq}>{renderItem(it)}</Fragment>
        ))}
        {isRunning && renderPending?.()}
        {!isRunning && items.length > 0 && renderFooter?.()}
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
      renderFooter={renderFooter}
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
  renderFooter?: () => ReactNode
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
  renderFooter,
  estimateSize,
  overscanBottom,
  className,
  role,
  autoScroll,
  containerRef,
}: VirtualizedThreadProps): ReactNode {
  const { ref: parentRef, getIsAtBottom, onScroll, scrollToBottom } = autoScroll
  // Geometry alone cannot identify user intent while virtual rows are being
  // measured: as the estimated total grows, the browser emits scroll events
  // with a large temporary bottom gap. Keep the initial bottom lock until an
  // actual upward user gesture releases it. Reaching the bottom manually arms
  // the lock again for later streaming/measurement updates.
  const followMeasurementsRef = useRef(true)
  const userScrollDirectionRef = useRef<-1 | 0 | 1>(0)
  const handleScroll = useCallback(() => {
    onScroll()
    // Do not re-arm during the first few pixels of an upward gesture: those
    // events are still inside the bottom threshold. Only an explicit downward
    // gesture that actually reaches the bottom restores measurement following.
    if (userScrollDirectionRef.current > 0 && getIsAtBottom()) {
      followMeasurementsRef.current = true
    }
  }, [getIsAtBottom, onScroll])
  const releaseBottomLock = useCallback(() => {
    userScrollDirectionRef.current = -1
    followMeasurementsRef.current = false
  }, [])
  const rearmAtBottom = useCallback(() => {
    if (getIsAtBottom()) followMeasurementsRef.current = true
  }, [getIsAtBottom])
  // count includes the trailing pending row + overscan spacer when present.
  const trailingCount = (isRunning && renderPending ? 1 : 0) + (overscanBottom > 0 ? 1 : 0)
  const count = items.length + trailingCount
  const rowVirtualizer = useVirtualizer({
    count,
    getScrollElement: () => parentRef.current,
    estimateSize: () => estimateSize,
    overscan: 8,
  })

  // Pin to the true bottom once rows re-measure. On a freshly mounted thread
  // (e.g. switching conversations) getTotalSize() starts at the row ESTIMATE and
  // only converges after ResizeObserver measurements land — scrolling to the
  // estimate would strand the view partway up. A layout effect closes each
  // measurement gap before paint while the bottom lock remains armed.
  const totalSize = rowVirtualizer.getTotalSize()
  useLayoutEffect(() => {
    if (followMeasurementsRef.current) scrollToBottom('auto')
  }, [totalSize, scrollToBottom])

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
        data-jcode-ui=""
        role={role}
        onScroll={handleScroll}
        onWheel={(event) => {
          if (event.deltaY < 0) {
            releaseBottomLock()
          } else if (event.deltaY > 0) {
            userScrollDirectionRef.current = 1
          }
        }}
        onTouchMove={releaseBottomLock}
        onTouchEnd={rearmAtBottom}
        onKeyDown={(event) => {
          if (event.key === 'ArrowUp' || event.key === 'PageUp' || event.key === 'Home') {
            releaseBottomLock()
          } else if (
            event.key === 'ArrowDown' ||
            event.key === 'PageDown' ||
            event.key === 'End'
          ) {
            userScrollDirectionRef.current = 1
          }
        }}
        onPointerDown={(event) => {
          // A scrollbar-thumb drag does not emit wheel/touch events. Detect a
          // pointer in the classic scrollbar gutter without treating ordinary
          // clicks inside message content as scroll intent.
          const el = event.currentTarget
          const scrollbarWidth = el.offsetWidth - el.clientWidth
          if (scrollbarWidth <= 0) return
          const rect = el.getBoundingClientRect()
          if (event.clientX >= rect.right - scrollbarWidth) releaseBottomLock()
        }}
        onPointerUp={rearmAtBottom}
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
        {/* Footer flows after the virtual sizer — it is never virtualized so
            follow-up chips stay simple and measurable. */}
        {!isRunning && items.length > 0 && renderFooter?.()}
      </div>
    </div>
  )
}
