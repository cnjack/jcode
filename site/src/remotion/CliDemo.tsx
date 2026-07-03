import type { ReactNode } from 'react'
import { AbsoluteFill, Sequence, interpolate, spring, useCurrentFrame, useVideoConfig } from 'remotion'

export const CLI_DEMO = {
  width: 1240,
  height: 720,
  fps: 30,
  durationInFrames: 640,
}

const CMD = 'refactor the config loader to support env overrides'

const T = {
  welcome: 8,
  typeStart: 45,
  typeEnd: 160,
  submit: 175,
  think: 190,
  tool1: 225,
  tool2: 260,
  plan: 300,
  diff: 380,
  test: 450,
  ok: 520,
  summary: 545,
}

function Appear({ at, children }: { at: number; children: ReactNode }) {
  const frame = useCurrentFrame()
  const { fps } = useVideoConfig()
  const p = spring({ frame: frame - at, fps, config: { damping: 18, mass: 0.6 } })
  return <div style={{ opacity: p, transform: `translateY(${(1 - p) * 8}px)` }}>{children}</div>
}

export default function CliDemo() {
  const frame = useCurrentFrame()

  const typed = Math.round(
    interpolate(frame, [T.typeStart, T.typeEnd], [0, CMD.length], {
      extrapolateLeft: 'clamp',
      extrapolateRight: 'clamp',
    }),
  )
  const submitted = frame >= T.submit
  const caretOn = Math.floor(frame / 15) % 2 === 0
  const spinner = ['◐', '◓', '◑', '◒'][Math.floor(frame / 8) % 4]
  const running = frame >= T.think && frame < T.ok

  return (
    <AbsoluteFill style={{ justifyContent: 'center', alignItems: 'center', background: 'transparent' }}>
      <div className="cli-shot">
        <div className="cli-head">
          <span className="cli-dot r" />
          <span className="cli-dot y" />
          <span className="cli-dot g" />
          <span className="cli-title">jcode — ~/projects/api</span>
        </div>
        <div className="cli-cols">
          <div className="cli-maincol">
            <Appear at={T.welcome}>
              <div className="cli-dim">Welcome to JCODE. How can I help you today?</div>
            </Appear>

            {submitted && (
              <Appear at={T.submit}>
                <div className="cli-user">
                  <span className="cli-gt">&gt;</span> {CMD}
                </div>
              </Appear>
            )}

            <Sequence from={T.think} layout="none">
              <Appear at={T.think}>
                <div className="cli-agent">
                  ◆ Reading the current loader before touching anything.
                </div>
              </Appear>
            </Sequence>

            <Sequence from={T.tool1} layout="none">
              <Appear at={T.tool1}>
                <div className="cli-tool">
                  ⚙ read <span className="cli-dim">internal/config/loader.go</span>{' '}
                  {frame > T.tool1 + 25 ? <span className="cli-ok">✓</span> : spinner}
                </div>
              </Appear>
            </Sequence>
            <Sequence from={T.tool2} layout="none">
              <Appear at={T.tool2}>
                <div className="cli-tool">
                  ⚙ grep <span className="cli-dim">"os.Getenv" — 3 call sites</span>{' '}
                  {frame > T.tool2 + 25 ? <span className="cli-ok">✓</span> : spinner}
                </div>
              </Appear>
            </Sequence>

            <Sequence from={T.plan} layout="none">
              <Appear at={T.plan}>
                <div className="cli-agent">
                  ◆ Plan: introduce <code>EnvOverride</code> layering — file → env → flags.
                  Applying the edit.
                </div>
              </Appear>
            </Sequence>

            <Sequence from={T.diff} layout="none">
              <Appear at={T.diff}>
                <div className="cli-diffbox">
                  <div className="cli-difffile">internal/config/loader.go</div>
                  <div className="del">- cfg, err := parseFile(path)</div>
                  <div className="add">+ cfg, err := parseFile(path)</div>
                  <div className="add">+ cfg = applyEnvOverrides(cfg, os.Environ())</div>
                  <div> return cfg, err</div>
                </div>
              </Appear>
            </Sequence>

            <Sequence from={T.test} layout="none">
              <Appear at={T.test}>
                <div className="cli-tool">
                  ⚙ execute <span className="cli-dim">go test ./internal/config/...</span>{' '}
                  {frame > T.ok ? <span className="cli-ok">✓</span> : spinner}
                </div>
              </Appear>
            </Sequence>

            <Sequence from={T.summary} layout="none">
              <Appear at={T.summary}>
                <div className="cli-done">✓ Done — env overrides live, tests green, 2 files changed.</div>
              </Appear>
            </Sequence>

            <div className="cli-spring" />

            <div className="cli-statusline">
              <span className="cli-mode">Agent</span>
              <span className="cli-dim">·</span>
              <span className="cli-auto">Auto</span>
              <span className="cli-rule" />
            </div>
            <div className="cli-input">
              <span className="cli-gt">&gt;</span>{' '}
              {!submitted ? (
                <>
                  {CMD.slice(0, typed)}
                  {caretOn && <span className="cli-caret" />}
                  {typed === 0 && <span className="cli-dim"> Type your prompt here...</span>}
                </>
              ) : (
                <>
                  {running ? <span className="cli-dim">{spinner} working…</span> : <span className="cli-dim">Type your prompt here...</span>}
                  {!running && caretOn && <span className="cli-caret" />}
                </>
              )}
            </div>
          </div>
          <div className="cli-sidecol">
            <div className="cli-brand">
              [<span className="j">J</span>CODE]
            </div>
            <div className="cli-side-sec">
              <div className="cli-side-label">Model</div>
              <div>zhipuai-coding-plan / glm-5.1</div>
            </div>
            <div className="cli-side-sec">
              <div className="cli-side-label">Env</div>
              <div>🖥 Local</div>
            </div>
            <div className="cli-side-sec">
              <div className="cli-side-label">MCP</div>
              <div>
                <span className="cli-ok">●</span> web-search (1 tool)
              </div>
              <div>
                <span className="cli-ok">●</span> zread (3 tools)
              </div>
            </div>
          </div>
        </div>
      </div>
    </AbsoluteFill>
  )
}
