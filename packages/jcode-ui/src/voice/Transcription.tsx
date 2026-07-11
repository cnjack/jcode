/**
 * Transcription — timestamped transcript with playback sync.
 *
 * Renders speaker-tagged segments; the segment covering `currentTimeMs` is
 * highlighted (`--jcode-accent-wash`) and, when following, scrolled into view.
 * Clicking a segment seeks via `onSeek`. Auto-scroll follows playback but pauses
 * the moment the user scrolls the active segment out of view — a self-contained
 * take on core's `useAutoScroll`, with a "Follow" chip to re-engage.
 */

import { memo, useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { ArrowDownIcon } from '@heroicons/react/24/outline'

export interface TranscriptSegment {
  id: string
  text: string
  /** Segment start offset in milliseconds. */
  startMs?: number
  /** Segment end offset in milliseconds. */
  endMs?: number
  /** Optional speaker label / diarization tag. */
  speaker?: string
}

export interface TranscriptionProps {
  segments: TranscriptSegment[]
  /** Current playback position (ms) — drives the active highlight. */
  currentTimeMs?: number
  /** Seek handler; a segment is clickable when both this and `startMs` exist. */
  onSeek?: (ms: number) => void
  className?: string
}

function formatClock(ms: number): string {
  const total = Math.max(0, Math.floor(ms / 1000))
  const m = Math.floor(total / 60)
  const s = total % 60
  return `${m}:${s.toString().padStart(2, '0')}`
}

/** Index of the last segment whose start is at/behind the playhead. */
function activeSegmentIndex(segments: TranscriptSegment[], t: number | undefined): number {
  if (t == null) return -1
  let idx = -1
  for (let i = 0; i < segments.length; i++) {
    const start = segments[i]?.startMs
    if (start != null && start <= t) idx = i
  }
  return idx
}

export const Transcription = memo(function Transcription({
  segments,
  currentTimeMs,
  onSeek,
  className,
}: TranscriptionProps) {
  const scrollRef = useRef<HTMLDivElement | null>(null)
  const activeRef = useRef<HTMLButtonElement | HTMLDivElement | null>(null)
  const programmaticRef = useRef(false)
  const [follow, setFollow] = useState(true)

  const activeIndex = useMemo(
    () => activeSegmentIndex(segments, currentTimeMs),
    [segments, currentTimeMs],
  )
  const activeId = activeIndex >= 0 ? segments[activeIndex]?.id : undefined

  // Follow the playhead: scroll the active segment into view while following.
  useEffect(() => {
    if (!follow || !activeId) return
    const el = activeRef.current
    if (!el) return
    programmaticRef.current = true
    el.scrollIntoView({ block: 'nearest', behavior: 'smooth' })
    const timer = window.setTimeout(() => {
      programmaticRef.current = false
    }, 400)
    return () => window.clearTimeout(timer)
  }, [activeId, follow])

  // A user scroll that hides the active segment pauses following.
  const onScroll = useCallback(() => {
    if (programmaticRef.current) return
    const container = scrollRef.current
    const el = activeRef.current
    if (!container) return
    if (!el) return
    const c = container.getBoundingClientRect()
    const e = el.getBoundingClientRect()
    const visible = e.top >= c.top && e.bottom <= c.bottom
    setFollow(visible)
  }, [])

  const resumeFollow = useCallback(() => {
    setFollow(true)
    const el = activeRef.current
    if (el) {
      programmaticRef.current = true
      el.scrollIntoView({ block: 'center', behavior: 'smooth' })
      window.setTimeout(() => {
        programmaticRef.current = false
      }, 400)
    }
  }, [])

  return (
    <div
      data-jcode-ui=""
      className={['jcode-voice-transcript', className].filter(Boolean).join(' ')}
    >
      <div className="jcode-voice-transcript__scroll" ref={scrollRef} onScroll={onScroll}>
        {segments.map((seg, i) => {
          const isActive = i === activeIndex
          const seekable = onSeek != null && seg.startMs != null
          const setRef = (node: HTMLButtonElement | HTMLDivElement | null) => {
            if (isActive) activeRef.current = node
          }
          const content = (
            <>
              {(seg.speaker || seg.startMs != null) && (
                <span className="jcode-voice-transcript__meta">
                  {seg.speaker && (
                    <span className="jcode-voice-transcript__speaker">{seg.speaker}</span>
                  )}
                  {seg.startMs != null && (
                    <span className="jcode-voice-transcript__time">{formatClock(seg.startMs)}</span>
                  )}
                </span>
              )}
              <span className="jcode-voice-transcript__text">{seg.text}</span>
            </>
          )
          const cls = [
            'jcode-voice-transcript__seg',
            isActive && 'jcode-voice-transcript__seg--active',
          ]
            .filter(Boolean)
            .join(' ')
          return seekable ? (
            <button
              key={seg.id}
              type="button"
              ref={setRef}
              className={cls}
              data-active={isActive || undefined}
              aria-current={isActive || undefined}
              onClick={() => onSeek?.(seg.startMs as number)}
            >
              {content}
            </button>
          ) : (
            <div
              key={seg.id}
              ref={setRef}
              className={cls}
              data-active={isActive || undefined}
              aria-current={isActive || undefined}
            >
              {content}
            </div>
          )
        })}
      </div>
      {!follow && activeId && (
        <button
          type="button"
          className="jcode-voice-transcript__follow"
          onClick={resumeFollow}
          aria-label="Resume following playback"
        >
          <ArrowDownIcon className="jcode-voice-transcript__follow-icon" aria-hidden="true" />
          <span>Follow</span>
        </button>
      )}
    </div>
  )
})
