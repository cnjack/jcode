import { useEffect, useRef, useState } from 'react'
import { Player, type PlayerRef } from '@remotion/player'
import type { ComponentType } from 'react'

/** Remotion player wrapper: plays while on screen, pauses off screen. */
export default function DemoPlayer({
  component,
  durationInFrames,
  fps,
  width,
  height,
}: {
  component: ComponentType
  durationInFrames: number
  fps: number
  width: number
  height: number
}) {
  const playerRef = useRef<PlayerRef>(null)
  const hostRef = useRef<HTMLDivElement>(null)
  const [visible, setVisible] = useState(false)

  useEffect(() => {
    const el = hostRef.current
    if (!el) return
    const io = new IntersectionObserver(
      ([e]) => setVisible(e.isIntersecting),
      { threshold: 0.25 },
    )
    io.observe(el)
    return () => io.disconnect()
  }, [])

  useEffect(() => {
    const p = playerRef.current
    if (!p) return
    if (visible) p.play()
    else p.pause()
  }, [visible])

  return (
    <div ref={hostRef} className="demo-player">
      <Player
        ref={playerRef}
        component={component}
        durationInFrames={durationInFrames}
        fps={fps}
        compositionWidth={width}
        compositionHeight={height}
        loop
        controls={false}
        clickToPlay={false}
        style={{ width: '100%' }}
      />
    </div>
  )
}
