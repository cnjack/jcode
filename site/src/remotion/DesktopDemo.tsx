import type { ReactNode } from 'react'
import { AbsoluteFill, Sequence, interpolate, spring, useCurrentFrame, useVideoConfig } from 'remotion'
import {
  AgentText,
  Composer,
  DiffCard,
  DoneBanner,
  JcodeWordmark,
  Sidebar,
  ToolChip,
  UserBubble,
  WindowChrome,
} from '../components/desktop/parts'

export const DESKTOP_DEMO = {
  width: 1240,
  height: 800,
  fps: 30,
  durationInFrames: 620,
}

const PROMPT = 'fix the flaky TestSessionRefresh in internal/auth'

/* timeline (frames @30fps) */
const T = {
  windowIn: 0,
  typeStart: 40,
  typeEnd: 150,
  send: 165,
  sessionIn: 180,
  tool1: 200,
  tool2: 235,
  agent1: 275,
  diff: 330,
  tool3: 395,
  done: 470,
  settle: 560,
}

function Rise({ children, at }: { children: ReactNode; at: number }) {
  const frame = useCurrentFrame()
  const { fps } = useVideoConfig()
  const p = spring({ frame: frame - at, fps, config: { damping: 16, mass: 0.7 } })
  return (
    <div style={{ opacity: p, transform: `translateY(${(1 - p) * 14}px)` }}>{children}</div>
  )
}

export default function DesktopDemo() {
  const frame = useCurrentFrame()
  const { fps } = useVideoConfig()

  const winIn = spring({ frame, fps, config: { damping: 15 } })
  const typedChars = Math.round(
    interpolate(frame, [T.typeStart, T.typeEnd], [0, PROMPT.length], {
      extrapolateLeft: 'clamp',
      extrapolateRight: 'clamp',
    }),
  )
  const inSession = frame >= T.sessionIn
  const sendHot = frame >= T.typeEnd - 6 && frame < T.sessionIn

  // gentle push-in while the agent works, release at the end
  const zoom = interpolate(
    frame,
    [T.sessionIn, T.sessionIn + 40, T.settle, T.settle + 40],
    [1, 1.035, 1.035, 1],
    { extrapolateLeft: 'clamp', extrapolateRight: 'clamp' },
  )

  const caretOn = Math.floor(frame / 16) % 2 === 0

  return (
    <AbsoluteFill style={{ background: 'transparent', justifyContent: 'center', alignItems: 'center' }}>
      <div
        className="dk-demo-scale"
        style={{
          width: 1180,
          opacity: winIn,
          transform: `scale(${0.96 + winIn * 0.04}) scale(${zoom})`,
        }}
      >
        <WindowChrome>
          <Sidebar active="jtype" />
          <section className="dk-main">
            {!inSession ? (
              <div className="dk-empty">
                <div className="dk-halo" />
                <JcodeWordmark />
                <h3 className="dk-empty-title">Start a new task in jack</h3>
                <p className="dk-empty-sub">
                  Send a message to start. <span className="dk-kbd">/</span> for commands.
                </p>
                <Composer
                  workspace="jtype"
                  value={PROMPT.slice(0, typedChars) + (typedChars > 0 && typedChars < PROMPT.length && caretOn ? '▏' : '')}
                  sendActive={sendHot || (frame >= T.send && frame < T.sessionIn)}
                />
              </div>
            ) : (
              <div className="dk-session">
                <div className="dk-session-head">
                  <span className="dk-session-ws">
                    <span className="dk-folder">▤</span> jtype
                  </span>
                  <span className={`dk-session-state${frame < T.done ? ' running' : ''}`}>
                    {frame < T.done ? '◌ running' : '✓ finished'}
                  </span>
                </div>
                <div className="dk-stream">
                  <Rise at={T.sessionIn}>
                    <UserBubble>{PROMPT}</UserBubble>
                  </Rise>
                  <Sequence from={T.tool1} layout="none">
                    <Rise at={T.tool1}>
                      <ToolChip
                        icon="⌕"
                        label="grep"
                        detail='"TestSessionRefresh" — 2 hits'
                        status={frame > T.tool1 + 26 ? 'done' : 'running'}
                      />
                    </Rise>
                  </Sequence>
                  <Sequence from={T.tool2} layout="none">
                    <Rise at={T.tool2}>
                      <ToolChip
                        icon="☰"
                        label="read"
                        detail="internal/auth/session_test.go"
                        status={frame > T.tool2 + 28 ? 'done' : 'running'}
                      />
                    </Rise>
                  </Sequence>
                  <Sequence from={T.agent1} layout="none">
                    <Rise at={T.agent1}>
                      <AgentText>
                        The test races on <code>time.Now()</code> — expiry is compared against a
                        real clock. Injecting a fake clock.
                      </AgentText>
                    </Rise>
                  </Sequence>
                  <Sequence from={T.diff} layout="none">
                    <Rise at={T.diff}>
                      <DiffCard
                        file="internal/auth/session.go"
                        lines={[
                          '- if token.ExpiresAt.Before(time.Now()) {',
                          '+ if token.ExpiresAt.Before(s.clock.Now()) {',
                          '    return s.refresh(ctx, token)',
                        ]}
                      />
                    </Rise>
                  </Sequence>
                  <Sequence from={T.tool3} layout="none">
                    <Rise at={T.tool3}>
                      <ToolChip
                        icon="▶"
                        label="execute"
                        detail="go test ./internal/auth/... -count=20"
                        status={frame > T.tool3 + 55 ? 'done' : 'running'}
                      />
                    </Rise>
                  </Sequence>
                  <Sequence from={T.done} layout="none">
                    <Rise at={T.done}>
                      <DoneBanner summary="2 files changed · 20/20 runs green · 41s" />
                    </Rise>
                  </Sequence>
                </div>
              </div>
            )}
          </section>
        </WindowChrome>
      </div>
    </AbsoluteFill>
  )
}
