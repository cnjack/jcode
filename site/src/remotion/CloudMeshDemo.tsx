import type { CSSProperties, ReactNode } from 'react'
import {
  AbsoluteFill,
  interpolate,
  spring,
  useCurrentFrame,
  useVideoConfig,
} from 'remotion'

export const CLOUD_MESH_DEMO = {
  width: 1240,
  height: 720,
  fps: 30,
  durationInFrames: 600,
}

const T = {
  desktopA: 10,
  desktopB: 55,
  key: 105,
  config: 165,
  provider: 285,
  proxy: 360,
  companion: 440,
  settle: 520,
}

const colors = {
  paper: '#fffaf1',
  ink: '#211b14',
  soft: '#766a59',
  line: '#d8cbb3',
  orange: '#ea721e',
  orangeSoft: '#fff0e2',
  blue: '#4169e1',
  blueSoft: '#edf1ff',
  green: '#258557',
  greenSoft: '#eaf7ef',
  navy: '#1d2836',
}

function enter(frame: number, at: number, fps: number) {
  return spring({ frame: frame - at, fps, config: { damping: 18, mass: 0.65 } })
}

function Card({ children, style }: { children: ReactNode; style?: CSSProperties }) {
  return (
    <div
      style={{
        background: colors.paper,
        border: `2px solid ${colors.line}`,
        borderRadius: 24,
        boxShadow: '0 24px 55px -34px rgba(50,35,20,.45)',
        ...style,
      }}
    >
      {children}
    </div>
  )
}

function Device({
  title,
  subtitle,
  at,
  right,
}: {
  title: string
  subtitle: string
  at: number
  right?: boolean
}) {
  const frame = useCurrentFrame()
  const { fps } = useVideoConfig()
  const p = enter(frame, at, fps)
  return (
    <Card
      style={{
        position: 'absolute',
        top: 112,
        left: right ? 870 : 62,
        width: 306,
        padding: 22,
        opacity: p,
        transform: `translateX(${(1 - p) * (right ? 30 : -30)}px)`,
      }}
    >
      <div style={{ display: 'flex', alignItems: 'center', gap: 14 }}>
        <div
          style={{
            width: 50,
            height: 50,
            display: 'grid',
            placeItems: 'center',
            borderRadius: 15,
            color: '#fff',
            background: colors.navy,
            fontSize: 25,
          }}
        >
          ◫
        </div>
        <div>
          <div style={{ fontSize: 21, fontWeight: 700 }}>{title}</div>
          <div style={{ color: colors.soft, fontSize: 13, marginTop: 2 }}>{subtitle}</div>
        </div>
      </div>
      <div
        style={{
          marginTop: 18,
          padding: '12px 14px',
          borderRadius: 12,
          background: '#f2eadc',
          fontFamily: 'IBM Plex Mono, monospace',
          fontSize: 13,
        }}
      >
        local provider · direct
      </div>
    </Card>
  )
}

function Flow({
  from,
  to,
  y,
  at,
  color,
  dashed = false,
}: {
  from: number
  to: number
  y: number
  at: number
  color: string
  dashed?: boolean
}) {
  const frame = useCurrentFrame()
  const progress = interpolate(frame, [at, at + 48], [0, 1], {
    extrapolateLeft: 'clamp',
    extrapolateRight: 'clamp',
  })
  const width = Math.abs(to - from)
  return (
    <svg
      width={width}
      height={24}
      viewBox={`0 0 ${width} 24`}
      style={{ position: 'absolute', left: Math.min(from, to), top: y - 12, overflow: 'visible' }}
    >
      <line
        x1={from < to ? 0 : width}
        x2={from < to ? width : 0}
        y1="12"
        y2="12"
        stroke={color}
        strokeWidth="3"
        strokeDasharray={dashed ? '10 8' : `${width} ${width}`}
        strokeDashoffset={width * (1 - progress)}
        opacity={0.9}
      />
      {progress > 0.88 && (
        <path
          d={
            from < to
              ? `M ${width - 12} 4 L ${width} 12 L ${width - 12} 20`
              : 'M 12 4 L 0 12 L 12 20'
          }
          fill="none"
          stroke={color}
          strokeWidth="3"
          strokeLinecap="round"
          strokeLinejoin="round"
        />
      )}
    </svg>
  )
}

export default function CloudMeshDemo() {
  const frame = useCurrentFrame()
  const { fps } = useVideoConfig()
  const cloudIn = enter(frame, T.key - 22, fps)
  const keyIn = enter(frame, T.key, fps)
  const configIn = enter(frame, T.config + 25, fps)
  const providerIn = enter(frame, T.provider, fps)
  const proxyIn = enter(frame, T.proxy + 36, fps)
  const companionIn = enter(frame, T.companion, fps)

  return (
    <AbsoluteFill
      style={{
        background: '#f7f0e4',
        color: colors.ink,
        fontFamily: 'Instrument Sans, system-ui, sans-serif',
        overflow: 'hidden',
      }}
    >
      <div
        style={{
          position: 'absolute',
          inset: 0,
          backgroundImage:
            'linear-gradient(rgba(100,80,50,.055) 1px, transparent 1px), linear-gradient(90deg, rgba(100,80,50,.055) 1px, transparent 1px)',
          backgroundSize: '40px 40px',
        }}
      />
      <div style={{ position: 'absolute', left: 62, top: 42 }}>
        <div
          style={{
            fontFamily: 'IBM Plex Mono, monospace',
            color: colors.orange,
            letterSpacing: '.16em',
            fontSize: 13,
            fontWeight: 600,
          }}
        >
          [ DESKTOP ↔ CLOUD ]
        </div>
        <div style={{ fontSize: 27, fontWeight: 700, marginTop: 5 }}>
          One account. Two deliberately separate lanes.
        </div>
      </div>

      <Device title="Desktop A" subtitle="MacBook · trusted" at={T.desktopA} />
      <Device title="Desktop B" subtitle="Studio · newly approved" at={T.desktopB} right />

      <Card
        style={{
          position: 'absolute',
          top: 103,
          left: 462,
          width: 318,
          padding: 22,
          textAlign: 'center',
          opacity: cloudIn,
          transform: `translateY(${(1 - cloudIn) * -20}px)`,
          borderColor: '#b8c5f4',
          background: '#f8f9ff',
        }}
      >
        <div style={{ fontSize: 14, color: colors.blue, fontWeight: 700 }}>JCODE CLOUD</div>
        <div style={{ fontSize: 27, fontWeight: 700, marginTop: 6 }}>Account mesh</div>
        <div style={{ color: colors.soft, fontSize: 13, marginTop: 4 }}>
          metadata, encrypted envelopes, Cloud providers
        </div>
      </Card>

      <div
        style={{
          position: 'absolute',
          left: 395,
          top: 267,
          width: 450,
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'center',
          gap: 12,
          opacity: keyIn,
          transform: `scale(${0.92 + keyIn * 0.08})`,
        }}
      >
        <span
          style={{
            width: 42,
            height: 42,
            display: 'grid',
            placeItems: 'center',
            borderRadius: 99,
            background: colors.orange,
            color: '#fff',
            fontSize: 21,
          }}
        >
          ⌘
        </span>
        <div>
          <div style={{ fontSize: 16, fontWeight: 700 }}>ASK approved on a trusted Desktop</div>
          <div style={{ color: colors.soft, fontSize: 13 }}>Cloud never receives the plaintext key</div>
        </div>
      </div>

      <Flow from={366} to={870} y={335} at={T.config} color={colors.orange} dashed />
      <Flow from={870} to={366} y={356} at={T.config + 38} color={colors.orange} dashed />
      <div
        style={{
          position: 'absolute',
          left: 424,
          top: 367,
          width: 390,
          padding: '10px 16px',
          borderRadius: 999,
          textAlign: 'center',
          background: colors.orangeSoft,
          border: '1px solid #f3c59f',
          color: '#9b470f',
          fontFamily: 'IBM Plex Mono, monospace',
          fontSize: 13,
          opacity: configIn,
        }}
      >
        E2EE sync · keys + headers + model state
      </div>

      <Card
        style={{
          position: 'absolute',
          left: 62,
          top: 450,
          width: 306,
          height: 172,
          padding: 20,
          opacity: providerIn,
          transform: `translateY(${(1 - providerIn) * 24}px)`,
          borderColor: '#c8dbc9',
          background: '#fbfffb',
        }}
      >
        <div style={{ color: colors.green, fontSize: 13, fontWeight: 700 }}>LOCAL PROVIDER</div>
        <div style={{ fontSize: 22, fontWeight: 700, marginTop: 8 }}>Zhipu AI</div>
        <div style={{ color: colors.soft, fontSize: 13 }}>API key stays on this Desktop</div>
        <div
          style={{
            marginTop: 17,
            background: colors.greenSoft,
            color: colors.green,
            borderRadius: 10,
            padding: '10px 12px',
            fontFamily: 'IBM Plex Mono, monospace',
            fontSize: 12,
          }}
        >
          Desktop ──direct──▶ API
        </div>
      </Card>

      <Card
        style={{
          position: 'absolute',
          left: 462,
          top: 450,
          width: 318,
          height: 172,
          padding: 20,
          opacity: providerIn,
          transform: `translateY(${(1 - providerIn) * 24}px)`,
          borderColor: '#b8c5f4',
          background: '#f8f9ff',
        }}
      >
        <div style={{ color: colors.blue, fontSize: 13, fontWeight: 700 }}>CLOUD PROVIDER</div>
        <div style={{ fontSize: 22, fontWeight: 700, marginTop: 8 }}>Project model catalog</div>
        <div style={{ color: colors.soft, fontSize: 13 }}>provider kind supplies name + icon</div>
        <div
          style={{
            marginTop: 17,
            background: colors.blueSoft,
            color: colors.blue,
            borderRadius: 10,
            padding: '10px 12px',
            fontFamily: 'IBM Plex Mono, monospace',
            fontSize: 12,
            opacity: proxyIn,
          }}
        >
          Desktop ─cloud_proxy─▶ API
        </div>
      </Card>

      <Card
        style={{
          position: 'absolute',
          left: 870,
          top: 450,
          width: 306,
          height: 172,
          padding: 20,
          opacity: companionIn,
          transform: `translateY(${(1 - companionIn) * 24}px)`,
          background: colors.navy,
          borderColor: '#35475d',
          color: '#fff',
        }}
      >
        <div style={{ color: '#ffb276', fontSize: 13, fontWeight: 700 }}>ANY SCREEN</div>
        <div style={{ fontSize: 22, fontWeight: 700, marginTop: 8 }}>Browser + mobile</div>
        <div style={{ color: '#aebbc9', fontSize: 13 }}>resume tasks and approve actions</div>
        <div
          style={{
            marginTop: 17,
            borderRadius: 10,
            padding: '10px 12px',
            border: '1px solid #495d74',
            fontFamily: 'IBM Plex Mono, monospace',
            fontSize: 12,
            color: '#dce6f1',
          }}
        >
          device identity · encrypted commands
        </div>
      </Card>

      <div
        style={{
          position: 'absolute',
          right: 63,
          bottom: 30,
          fontFamily: 'IBM Plex Mono, monospace',
          fontSize: 11,
          color: colors.soft,
          opacity: interpolate(frame, [T.settle, T.settle + 30], [0, 1], {
            extrapolateLeft: 'clamp',
            extrapolateRight: 'clamp',
          }),
        }}
      >
        local-first · explicit trust · no secret downgrade
      </div>
    </AbsoluteFill>
  )
}
