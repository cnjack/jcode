/**
 * VoiceVisualizer — live mic-volume bar waveform on a canvas.
 *
 * Feeds an `AnalyserNode` from `props.stream` and paints a bar spectrum in
 * `--jcode-color-primary`. With no stream (or `active={false}`) it renders a calm
 * low-amplitude breathing idle state. The `AudioContext` and its graph are torn
 * down on unmount / stream change so no audio session leaks. SSR-safe: all Web
 * Audio access lives inside client effects behind `typeof window` guards.
 */

import { memo, useEffect, useRef } from 'react'

type AudioContextCtor = typeof AudioContext

function getAudioContextCtor(): AudioContextCtor | null {
  if (typeof window === 'undefined') return null
  const w = window as unknown as {
    AudioContext?: AudioContextCtor
    webkitAudioContext?: AudioContextCtor
  }
  return w.AudioContext ?? w.webkitAudioContext ?? null
}

export interface VoiceVisualizerProps {
  /** Live input to analyze (e.g. from `getUserMedia`). Omit for the idle state. */
  stream?: MediaStream | null
  /** When false, renders the idle breathing state even if a stream is present. Default true. */
  active?: boolean
  /** Number of bars to render. Default 32. */
  bars?: number
  className?: string
}

export const VoiceVisualizer = memo(function VoiceVisualizer({
  stream = null,
  active = true,
  bars = 32,
  className,
}: VoiceVisualizerProps) {
  const wrapRef = useRef<HTMLDivElement | null>(null)
  const canvasRef = useRef<HTMLCanvasElement | null>(null)
  const rafRef = useRef<number | null>(null)

  const ctxRef = useRef<AudioContext | null>(null)
  const analyserRef = useRef<AnalyserNode | null>(null)
  const sourceRef = useRef<MediaStreamAudioSourceNode | null>(null)
  const freqRef = useRef<Uint8Array<ArrayBuffer> | null>(null)

  const cssSizeRef = useRef({ w: 0, h: 0, dpr: 1 })
  const colorRef = useRef('#FF8400')
  const activeRef = useRef(active)
  activeRef.current = active

  // Build / tear down the Web Audio graph when the stream changes.
  useEffect(() => {
    const teardown = () => {
      try {
        sourceRef.current?.disconnect()
      } catch {
        /* no-op */
      }
      try {
        analyserRef.current?.disconnect()
      } catch {
        /* no-op */
      }
      const ctx = ctxRef.current
      if (ctx && ctx.state !== 'closed') void ctx.close().catch(() => undefined)
      sourceRef.current = null
      analyserRef.current = null
      ctxRef.current = null
      freqRef.current = null
    }

    if (!stream) {
      teardown()
      return
    }
    const Ctor = getAudioContextCtor()
    if (!Ctor) return
    const ctx = new Ctor()
    const analyser = ctx.createAnalyser()
    analyser.fftSize = 256
    analyser.smoothingTimeConstant = 0.8
    const source = ctx.createMediaStreamSource(stream)
    source.connect(analyser)
    ctxRef.current = ctx
    analyserRef.current = analyser
    sourceRef.current = source
    freqRef.current = new Uint8Array(analyser.frequencyBinCount)
    // Autoplay policy can leave the context suspended; nudge it.
    if (ctx.state === 'suspended') void ctx.resume().catch(() => undefined)

    return teardown
  }, [stream])

  // Keep the canvas backing store sized to its box (device-pixel crisp).
  useEffect(() => {
    const wrap = wrapRef.current
    const canvas = canvasRef.current
    if (!wrap || !canvas || typeof window === 'undefined') return
    const applySize = () => {
      const dpr = window.devicePixelRatio || 1
      const w = wrap.clientWidth
      const h = wrap.clientHeight
      cssSizeRef.current = { w, h, dpr }
      canvas.width = Math.max(1, Math.round(w * dpr))
      canvas.height = Math.max(1, Math.round(h * dpr))
    }
    applySize()
    const ro = new ResizeObserver(applySize)
    ro.observe(wrap)
    return () => ro.disconnect()
  }, [])

  // Single rAF paint loop for both live and idle states.
  useEffect(() => {
    const canvas = canvasRef.current
    const wrap = wrapRef.current
    if (!canvas || !wrap) return
    const g = canvas.getContext('2d')
    if (!g) return

    let frame = 0
    const refreshColor = () => {
      const v = getComputedStyle(wrap).getPropertyValue('--jcode-color-primary').trim()
      if (v) colorRef.current = v
    }
    refreshColor()

    const draw = () => {
      rafRef.current = requestAnimationFrame(draw)
      frame++
      if (frame % 30 === 0) refreshColor()

      const { w, h, dpr } = cssSizeRef.current
      if (w <= 0 || h <= 0) return
      g.setTransform(dpr, 0, 0, dpr, 0, 0)
      g.clearRect(0, 0, w, h)

      const count = Math.max(1, bars)
      const gap = Math.max(1, w / count / 3)
      const barW = (w - gap * (count - 1)) / count
      const mid = h / 2
      const analyser = analyserRef.current
      const freq = freqRef.current
      const live = activeRef.current && !!analyser && !!freq
      if (live && analyser && freq) analyser.getByteFrequencyData(freq)

      g.fillStyle = colorRef.current || '#FF8400'
      const now = performance.now() / 1000
      const binStep = live && freq ? freq.length / count : 0

      for (let i = 0; i < count; i++) {
        let amp: number
        if (live && freq) {
          const v = freq[Math.floor(i * binStep)] ?? 0
          amp = v / 255
        } else {
          // Idle: gentle travelling breath so it never looks frozen.
          const phase = now * 1.6 + i * 0.5
          amp = 0.06 + 0.05 * (0.5 + 0.5 * Math.sin(phase))
        }
        const barH = Math.max(2, amp * (h - 2))
        const x = i * (barW + gap)
        const y = mid - barH / 2
        const r = Math.min(barW / 2, 2)
        // Rounded bar.
        g.beginPath()
        if (typeof g.roundRect === 'function') {
          g.roundRect(x, y, barW, barH, r)
        } else {
          g.rect(x, y, barW, barH)
        }
        g.fill()
      }
    }
    draw()

    return () => {
      if (rafRef.current != null) cancelAnimationFrame(rafRef.current)
      rafRef.current = null
    }
  }, [bars])

  return (
    <div
      data-jcode-ui=""
      ref={wrapRef}
      className={['jcode-voice-visualizer', className].filter(Boolean).join(' ')}
      data-active={active || undefined}
      aria-hidden="true"
    >
      <canvas ref={canvasRef} className="jcode-voice-visualizer__canvas" />
    </div>
  )
})
