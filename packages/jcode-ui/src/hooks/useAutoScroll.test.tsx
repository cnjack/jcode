import { renderHook } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { useAutoScroll } from 'jcode-ui-core/hooks'
import type { MutableRefObject } from 'react'

afterEach(() => {
  vi.restoreAllMocks()
})

/**
 * The Thread scroll container carries the `scroll-smooth` Tailwind class
 * (`scroll-behavior: smooth`). Under that CSS, `el.scrollTo({ behavior: 'auto' })`
 * *animates* instead of snapping, and mid-animation scroll events flip
 * `isAtBottomRef` to false before the target lands — which then suppresses the
 * re-measure follow-up and strands conversation switches partway up the thread.
 *
 * `scrollToBottom` must therefore snap via a direct `scrollTop` assignment
 * (unaffected by CSS `scroll-behavior`) for the default 'auto' behavior, and
 * only animate when explicitly asked for 'smooth'.
 */
describe('useAutoScroll.scrollToBottom', () => {
  function attachScrollContainer() {
    const el = document.createElement('div')
    let scrollTop = 0
    Object.defineProperty(el, 'scrollHeight', {
      configurable: true,
      get: () => 1000,
    })
    Object.defineProperty(el, 'scrollTop', {
      configurable: true,
      get: () => scrollTop,
      set: (v: number) => {
        scrollTop = v
      },
    })
    const scrollTo = vi.fn()
    Object.defineProperty(el, 'scrollTo', {
      configurable: true,
      value: scrollTo,
    })
    return { el, scrollTo, getScrollTop: () => scrollTop }
  }

  it('snaps via scrollTop assignment for the default behavior (ignores scroll-behavior CSS)', () => {
    const { el, scrollTo, getScrollTop } = attachScrollContainer()
    const { result } = renderHook(() => useAutoScroll<HTMLDivElement>())
    ;(result.current.ref as MutableRefObject<HTMLDivElement | null>).current = el

    result.current.scrollToBottom()

    expect(getScrollTop()).toBe(1000)
    expect(scrollTo).not.toHaveBeenCalled()
    expect(result.current.getIsAtBottom()).toBe(true)
  })

  it('animates only for an explicit smooth request', () => {
    const { el, scrollTo } = attachScrollContainer()
    const { result } = renderHook(() => useAutoScroll<HTMLDivElement>())
    ;(result.current.ref as MutableRefObject<HTMLDivElement | null>).current = el

    result.current.scrollToBottom('smooth')

    expect(scrollTo).toHaveBeenCalledWith({ top: 1000, behavior: 'smooth' })
  })
})
