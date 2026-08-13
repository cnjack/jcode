/**
 * Behavioral hooks for chat UI primitives. These contain the interaction logic
 * the Vue version baked into App.vue (scroll tracking, type-ahead draining,
 * etc.) but framework-correct and reusable.
 */

import { useCallback, useEffect, useRef, useState } from 'react'
import { useRuntimeState } from '../runtime/context.js'

/**
 * Auto-scroll-to-bottom tracking: reports whether the user is "at the bottom"
 * of a scroll container (within `threshold` px of the bottom edge). When at the
 * bottom, streaming content auto-follows; when scrolled up, it does NOT yank the
 * user back down (the core streaming-UX contract from the Vue version).
 *
 * Returns the container ref to attach, the live `isAtBottom` flag, and a
 * `scrollToBottom` imperative. The caller decides when to call the latter
 * (typically on send, and on new content if `isAtBottom`).
 */
export function useAutoScroll<T extends HTMLElement>(threshold = 80) {
  const ref = useRef<T>(null)
  const isAtBottomRef = useRef(true)

  /** Imperatively scroll to the bottom edge. `behavior` defaults to 'auto'
   *  (instant) since this is called mid-stream. */
  const scrollToBottom = useCallback((behavior: ScrollBehavior = 'auto') => {
    const el = ref.current
    if (!el) return
    // A scroll container carrying CSS `scroll-behavior: smooth` (the Thread's
    // `scroll-smooth` class) turns `behavior: 'auto'` into an *animated* scroll.
    // Mid-animation scroll events flip `isAtBottomRef` false before the target
    // lands, which then suppresses the re-measure follow-up — conversation
    // switches strand partway up. Assigning `scrollTop` directly is always
    // instant and unaffected by the CSS, which is what 'auto' is documented to
    // mean here. Only an explicit 'smooth' animates.
    if (behavior === 'smooth') {
      el.scrollTo({ top: el.scrollHeight, behavior: 'smooth' })
    } else {
      el.scrollTop = el.scrollHeight
    }
    isAtBottomRef.current = true
  }, [])

  /** Attach to the container's onScroll (or wire a listener). Updates the flag. */
  const onScroll = useCallback(() => {
    const el = ref.current
    if (!el) return
    const distance = el.scrollHeight - el.scrollTop - el.clientHeight
    isAtBottomRef.current = distance <= threshold
  }, [threshold])

  /** Read the current flag. Use this in effects; for render, prefer the
   *  `useIsAtBottom` hook below which re-renders on change. */
  const getIsAtBottom = useCallback(() => isAtBottomRef.current, [])

  return { ref, onScroll, scrollToBottom, getIsAtBottom, isAtBottomRef }
}

/**
 * Re-render-friendly version of the at-bottom flag: re-renders the component
 * when the flag flips. Use sparingly (the scroll handler runs a lot); for most
 * cases the imperative `getIsAtBottom` + an effect is enough.
 *
 * NOTE: this intentionally tracks a coarse boolean — it only re-renders on
 * crossing the threshold, not on every scroll event.
 */
export function useIsAtBottom<T extends HTMLElement>(threshold = 80) {
  const { ref, onScroll, scrollToBottom } = useAutoScroll<T>(threshold)
  return { ref, onScroll, scrollToBottom }
}

/**
 * Stream-follow effect: when the runtime emits new/changed items, scroll to
 * bottom ONLY if the user was already at the bottom. This is the declarative
 * form of the Vue watch on `timeline.length + lastMessage.content.length`.
 *
 * `dep` should be a value that changes whenever there's new content to follow
 * (e.g. items length, or last-item content length).
 */
export function useStreamFollow<T extends HTMLElement>(
  autoScroll: ReturnType<typeof useAutoScroll<T>>,
  dep: unknown,
) {
  const { getIsAtBottom, scrollToBottom } = autoScroll
  useEffect(() => {
    if (getIsAtBottom()) scrollToBottom('auto')
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [dep])
}

/**
 * Auto-focus a ref on mount and when `isRunning` flips false (the Vue version
 * refocuses the composer when a turn ends).
 */
export function useFocusOnIdle<T extends HTMLElement>(isRunning: boolean) {
  const ref = useRef<T>(null)
  useEffect(() => {
    if (!isRunning) ref.current?.focus()
  }, [isRunning])
  return ref
}

/**
 * Track + drain the type-ahead queue: returns the current queued messages.
 * Draining is the runtime's job (it sends the next queued message on each turn
 * end); this hook just surfaces the queue for rendering.
 */
export function useQueuedMessages() {
  const { queued } = useRuntimeState()
  return queued
}

/**
 * Live elapsed milliseconds since `startedAt` (unix ms), ticking once per
 * second while `active`. When inactive (or `startedAt` is missing) the
 * interval is not scheduled — mount this only where a live badge is shown
 * (e.g. a running batch row) to keep timers scarce.
 */
export function useElapsed(startedAt: number | undefined, active = true): number {
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    if (!active || !startedAt) return
    setNow(Date.now())
    const id = setInterval(() => setNow(Date.now()), 1000)
    return () => clearInterval(id)
  }, [active, startedAt])
  if (!startedAt) return 0
  return Math.max(0, now - startedAt)
}

/** Format elapsed/duration ms as a compact badge: `2s`, `1m 05s`. */
export function formatElapsed(ms: number): string {
  const totalSec = Math.floor(ms / 1000)
  if (totalSec < 60) return `${totalSec}s`
  const min = Math.floor(totalSec / 60)
  const sec = totalSec % 60
  return `${min}m ${String(sec).padStart(2, '0')}s`
}
