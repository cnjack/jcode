/**
 * ComputerActRenderer — `computer_act`.
 *
 * computer_act is the one tool here that genuinely needs a custom renderer. It
 * takes a *batch*: a dozen UI actions in one call. As raw JSON that is an
 * unreadable wall, and as raw text the one line that matters — the refusal —
 * is buried at the bottom.
 *
 * So: an ordered step list, each step showing what happened and where, plus the
 * refusal rendered as its own block. A refused batch may have partially applied
 * (steps 1..n-1 landed, step n was stopped), and that distinction is the whole
 * point of showing it — "3 of 5 done, then stopped" is a very different state
 * to explain than "nothing happened".
 *
 * The output shape comes from internal/computer/session.go Session.Act:
 *
 *   1. click [e3] in "Notes"
 *   2. type in "Notes"
 *   (2/2 actions completed)
 *
 * or, when the gate refused a step:
 *
 *   1. type in "Notes"
 *   Refused: "iTerm" (com.googlecode.iterm2) is at the "click" tier, …
 */

import { memo, useMemo } from 'react'
import type { ToolRendererProps } from 'jcode-ui-core/adapters'
import { GenericRenderer } from './generic.js'

type Step = { n: string; action: string; target: string; app: string }

const STEP_RE = /^(\d+)\.\s+(\w+)(\s+\[[^\]]+\]|\s+\([^)]*\))?\s+in\s+"([^"]*)"\s*$/
const DONE_RE = /^\((\d+)\/(\d+) actions completed\)\s*$/

/** Icon per action kind. Grouped by what the action does, not by input device. */
function iconFor(action: string): string {
  switch (action.toLowerCase()) {
    case 'click':
    case 'dblclick':
    case 'rclick':
      return '⊙'
    case 'type':
    case 'set_value':
      return '⌨'
    case 'press':
      return '⌘'
    case 'scroll':
      return '↕'
    case 'drag':
      return '⇄'
    case 'hover':
      return '⌖'
    case 'menu':
      return '☰'
    case 'select_text':
      return '⌗'
    default:
      return '•'
  }
}

export const ComputerActRenderer = memo(function ComputerActRenderer(props: ToolRendererProps) {
  const parsed = useMemo(() => {
    const lines = (props.output ?? '').split('\n')
    const steps: Step[] = []
    let done: { ok: number; total: number } | null = null
    let refusal = ''
    const rest: string[] = []

    for (const raw of lines) {
      const line = raw.trimEnd()
      if (!line) continue
      const m = STEP_RE.exec(line)
      if (m) {
        steps.push({ n: m[1], action: m[2], target: (m[3] ?? '').trim(), app: m[4] })
        continue
      }
      const d = DONE_RE.exec(line)
      if (d) {
        done = { ok: Number(d[1]), total: Number(d[2]) }
        continue
      }
      if (/^(Refused:|Computer control was interrupted|The screen is locked|step \d+ of \d+)/.test(line)) {
        refusal = refusal ? `${refusal} ${line}` : line
        continue
      }
      rest.push(line)
    }
    return { steps, done, refusal, rest }
  }, [props.output])

  // Nothing recognizable — a plain error, or a shape we don't know. Don't guess.
  if (!parsed.steps.length && !parsed.refusal) return <GenericRenderer {...props} />

  const stopped = Boolean(parsed.refusal)

  return (
    <div className="jcode-computer-act px-3 py-2" style={{ background: 'var(--jcode-color-surface)' }}>
      {parsed.steps.length > 0 && (
        <ol className="m-0 list-none space-y-1 p-0">
          {parsed.steps.map((s) => (
            <li key={s.n} className="flex items-baseline gap-2 text-sm">
              <span
                className="w-4 shrink-0 text-right tabular-nums"
                style={{ color: 'var(--jcode-color-text-muted)' }}
              >
                {s.n}
              </span>
              <span aria-hidden="true" style={{ color: 'var(--jcode-color-text-muted)' }}>
                {iconFor(s.action)}
              </span>
              <span className="font-medium">{s.action}</span>
              {s.target && (
                <code
                  className="rounded px-1 text-xs"
                  style={{
                    background: 'var(--jcode-color-surface-raised)',
                    color: 'var(--jcode-color-text-muted)',
                  }}
                >
                  {s.target.replace(/^\[|\]$/g, '')}
                </code>
              )}
              <span style={{ color: 'var(--jcode-color-text-muted)' }}>in {s.app}</span>
            </li>
          ))}
        </ol>
      )}

      {stopped && (
        <div
          className="mt-2 rounded-md px-2 py-1.5 text-sm"
          style={{
            background: 'var(--jcode-color-surface-raised)',
            borderLeft: '3px solid var(--jcode-color-warning, var(--jcode-color-border))',
          }}
        >
          {/* Say plainly how far it got: a partially applied batch is a state the
              user has to reason about, not an error to wave away. */}
          {parsed.steps.length > 0 && (
            <div className="mb-1 font-medium">
              Stopped after {parsed.steps.length} {parsed.steps.length === 1 ? 'action' : 'actions'}
            </div>
          )}
          <div style={{ color: 'var(--jcode-color-text-muted)' }}>{parsed.refusal}</div>
        </div>
      )}

      {!stopped && parsed.done && (
        <div className="mt-1.5 text-xs" style={{ color: 'var(--jcode-color-text-muted)' }}>
          {parsed.done.ok}/{parsed.done.total} actions completed
        </div>
      )}

      {parsed.rest.length > 0 && (
        <pre
          className="mt-1.5 overflow-x-auto text-xs"
          style={{ color: 'var(--jcode-color-text-muted)' }}
        >
          {parsed.rest.join('\n')}
        </pre>
      )}
    </div>
  )
})
