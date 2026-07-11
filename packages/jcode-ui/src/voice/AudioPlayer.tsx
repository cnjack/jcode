/**
 * AudioPlayer — minimal, fully self-drawn audio transport.
 *
 * Deliberately avoids `<audio controls>`: an offscreen `<audio>` element carries
 * playback while every control (play/pause, a draggable + keyboard-accessible
 * seek bar, elapsed/total time, 1×/1.5×/2× speed) is token-styled markup. Emits
 * `onTimeUpdate(ms)` so a sibling `<Transcription>` can highlight in lockstep.
 */

import { memo, useCallback, useEffect, useRef, useState } from 'react'
import { PlayIcon, PauseIcon } from '@heroicons/react/24/outline'

const RATES = [1, 1.5, 2] as const

export interface AudioPlayerProps {
  /** Audio source URL (or object URL / data URI). */
  src: string
  /** Fires on every playback tick and seek, in milliseconds. */
  onTimeUpdate?: (ms: number) => void
  /** Autoplay once metadata is ready (subject to browser policy). */
  autoPlay?: boolean
  className?: string
}

function formatClock(ms: number): string {
  if (!isFinite(ms) || ms < 0) ms = 0
  const total = Math.floor(ms / 1000)
  const m = Math.floor(total / 60)
  const s = total % 60
  return `${m}:${s.toString().padStart(2, '0')}`
}

function clamp01(n: number): number {
  return n < 0 ? 0 : n > 1 ? 1 : n
}

export const AudioPlayer = memo(function AudioPlayer({
  src,
  onTimeUpdate,
  autoPlay = false,
  className,
}: AudioPlayerProps) {
  const audioRef = useRef<HTMLAudioElement | null>(null)
  const trackRef = useRef<HTMLDivElement | null>(null)
  const draggingRef = useRef(false)
  const onTimeUpdateRef = useRef(onTimeUpdate)
  onTimeUpdateRef.current = onTimeUpdate

  const [playing, setPlaying] = useState(false)
  const [currentMs, setCurrentMs] = useState(0)
  const [durationMs, setDurationMs] = useState(0)
  const [rate, setRate] = useState<(typeof RATES)[number]>(1)

  const fraction = durationMs > 0 ? clamp01(currentMs / durationMs) : 0

  // Wire native media events to local state.
  useEffect(() => {
    const audio = audioRef.current
    if (!audio) return
    const onTime = () => {
      const ms = audio.currentTime * 1000
      setCurrentMs(ms)
      onTimeUpdateRef.current?.(ms)
    }
    const onMeta = () => setDurationMs(isFinite(audio.duration) ? audio.duration * 1000 : 0)
    const onPlay = () => setPlaying(true)
    const onPause = () => setPlaying(false)
    const onEnded = () => setPlaying(false)
    audio.addEventListener('timeupdate', onTime)
    audio.addEventListener('loadedmetadata', onMeta)
    audio.addEventListener('durationchange', onMeta)
    audio.addEventListener('play', onPlay)
    audio.addEventListener('pause', onPause)
    audio.addEventListener('ended', onEnded)
    return () => {
      audio.removeEventListener('timeupdate', onTime)
      audio.removeEventListener('loadedmetadata', onMeta)
      audio.removeEventListener('durationchange', onMeta)
      audio.removeEventListener('play', onPlay)
      audio.removeEventListener('pause', onPause)
      audio.removeEventListener('ended', onEnded)
    }
  }, [])

  // Keep element playbackRate in sync with the rate control.
  useEffect(() => {
    if (audioRef.current) audioRef.current.playbackRate = rate
  }, [rate])

  const togglePlay = useCallback(() => {
    const audio = audioRef.current
    if (!audio) return
    if (audio.paused) void audio.play().catch(() => setPlaying(false))
    else audio.pause()
  }, [])

  const seekToFraction = useCallback((f: number) => {
    const audio = audioRef.current
    if (!audio || !isFinite(audio.duration) || audio.duration <= 0) return
    const t = clamp01(f) * audio.duration
    audio.currentTime = t
    const ms = t * 1000
    setCurrentMs(ms)
    onTimeUpdateRef.current?.(ms)
  }, [])

  const fractionFromPointer = useCallback((clientX: number): number => {
    const track = trackRef.current
    if (!track) return 0
    const rect = track.getBoundingClientRect()
    if (rect.width <= 0) return 0
    return clamp01((clientX - rect.left) / rect.width)
  }, [])

  const nudge = useCallback((seconds: number) => {
    const audio = audioRef.current
    if (!audio || !isFinite(audio.duration) || audio.duration <= 0) return
    const t = Math.min(Math.max(0, audio.currentTime + seconds), audio.duration)
    audio.currentTime = t
    const ms = t * 1000
    setCurrentMs(ms)
    onTimeUpdateRef.current?.(ms)
  }, [])

  const cycleRate = useCallback(() => {
    setRate((r) => {
      const idx = RATES.indexOf(r)
      return RATES[(idx + 1) % RATES.length]
    })
  }, [])

  const durationSec = durationMs / 1000

  return (
    <div
      data-jcode-ui=""
      className={['jcode-voice-player', className].filter(Boolean).join(' ')}
    >
      {/* Offscreen media element — no native chrome. */}
      <audio ref={audioRef} src={src} autoPlay={autoPlay} preload="metadata" />

      <button
        type="button"
        className="jcode-voice-player__btn"
        onClick={togglePlay}
        aria-label={playing ? 'Pause' : 'Play'}
        aria-pressed={playing}
      >
        {playing ? (
          <PauseIcon className="jcode-voice-player__icon" aria-hidden="true" />
        ) : (
          <PlayIcon className="jcode-voice-player__icon" aria-hidden="true" />
        )}
      </button>

      <time className="jcode-voice-player__time" aria-label="Elapsed time">
        {formatClock(currentMs)}
      </time>

      <div
        ref={trackRef}
        className="jcode-voice-player__seek"
        role="slider"
        tabIndex={0}
        aria-label="Seek"
        aria-valuemin={0}
        aria-valuemax={Math.max(0, Math.round(durationSec))}
        aria-valuenow={Math.round(currentMs / 1000)}
        aria-valuetext={`${formatClock(currentMs)} of ${formatClock(durationMs)}`}
        onPointerDown={(e) => {
          e.currentTarget.setPointerCapture(e.pointerId)
          draggingRef.current = true
          seekToFraction(fractionFromPointer(e.clientX))
        }}
        onPointerMove={(e) => {
          if (draggingRef.current) seekToFraction(fractionFromPointer(e.clientX))
        }}
        onPointerUp={(e) => {
          draggingRef.current = false
          try {
            e.currentTarget.releasePointerCapture(e.pointerId)
          } catch {
            /* no-op */
          }
        }}
        onKeyDown={(e) => {
          if (e.key === 'ArrowRight' || e.key === 'ArrowUp') {
            e.preventDefault()
            nudge(5)
          } else if (e.key === 'ArrowLeft' || e.key === 'ArrowDown') {
            e.preventDefault()
            nudge(-5)
          } else if (e.key === 'Home') {
            e.preventDefault()
            seekToFraction(0)
          } else if (e.key === 'End') {
            e.preventDefault()
            seekToFraction(1)
          }
        }}
      >
        <div className="jcode-voice-player__seek-track">
          <div
            className="jcode-voice-player__seek-fill"
            style={{ width: `${fraction * 100}%` }}
          />
          <div
            className="jcode-voice-player__seek-thumb"
            style={{ left: `${fraction * 100}%` }}
          />
        </div>
      </div>

      <time className="jcode-voice-player__time jcode-voice-player__time--total" aria-label="Total time">
        {formatClock(durationMs)}
      </time>

      <button
        type="button"
        className="jcode-voice-player__rate"
        onClick={cycleRate}
        aria-label={`Playback speed ${rate}×`}
        title="Playback speed"
      >
        {rate}×
      </button>
    </div>
  )
})
