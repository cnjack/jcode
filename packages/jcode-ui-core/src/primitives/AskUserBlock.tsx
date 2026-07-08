/**
 * AskUserBlock — the headless interactive question block.
 *
 * Owns: the pending/resolved split, per-question selection state (single +
 * multi-select), free-text "Other" input, digit-key shortcuts (1-9), and
 * dispatching via runtime actions. Does NOT own styling or the output-format
 * parsing for resolved display (those live in the styled wrapper).
 *
 * The `renderPending` slot receives a `controls` object exposing the live
 * `selected`/`other` maps plus mutators (`toggleOption`, `setOther`) and the
 * `submit`/`skip` actions — so a styled consumer needs no local state.
 */

import { useCallback, useEffect, useMemo, useState } from 'react'
import type { ReactNode } from 'react'
import type { AskUserQuestion, AskUserAnswer, ToolCall } from '../types/index.js'
import { useRuntimeActions } from '../runtime/context.js'

export interface AskUserState {
  /** Per-question-header selected labels (single-select: one entry; multi: N). */
  selected: Record<string, string[]>
  /** Per-question-header free-text "Other" value. */
  other: Record<string, string>
}

/** Controls handed to the pending render-prop. */
export interface AskUserControls {
  /** Current selection map (question key → labels). */
  selected: Record<string, string[]>
  /** Current "Other" text map (question key → free text). */
  other: Record<string, string>
  /** Toggle an option. Honors multi_select. */
  toggleOption: (question: AskUserQuestion, label: string) => void
  /** Set the free-text value for a question. */
  setOther: (question: AskUserQuestion, value: string) => void
  /** Submit the current selections (no-op if nothing chosen per question). */
  submit: () => void
  /** Submit empty answers (skip). */
  skip: () => void
}

export interface AskUserBlockRenderSlots {
  /** Render the pending interactive card. */
  renderPending?: (questions: AskUserQuestion[], controls: AskUserControls) => ReactNode
  /** Render the resolved (replay) view. */
  renderResolved?: (tool: ToolCall, answers: AskUserAnswer[]) => ReactNode
}

export interface AskUserBlockProps extends AskUserBlockRenderSlots {
  tool: ToolCall
  /** className passthrough. */
  className?: string
}

const EMPTY_STATE: AskUserState = { selected: {}, other: {} }

export function AskUserBlock({ tool, className, renderPending, renderResolved }: AskUserBlockProps): ReactNode {
  const actions = useRuntimeActions()
  const questions = useMemo(() => extractQuestions(tool), [tool])
  const isPending = !!tool.askUserId && tool.status === 'running' && !tool.output

  const [state, setState] = useState<AskUserState>(EMPTY_STATE)

  const keyOf = useCallback((q: AskUserQuestion) => q.header ?? q.question, [])

  const toggleOption = useCallback(
    (q: AskUserQuestion, label: string) => {
      const key = keyOf(q)
      setState((s) => ({
        ...s,
        selected: q.multi_select
          ? { ...s.selected, [key]: toggle(s.selected[key], label) }
          : { ...s.selected, [key]: [label] },
      }))
    },
    [keyOf],
  )

  const setOther = useCallback(
    (q: AskUserQuestion, value: string) => {
      const key = keyOf(q)
      setState((s) => ({ ...s, other: { ...s.other, [key]: value } }))
    },
    [keyOf],
  )

  const submit = useCallback(() => {
    const answers: AskUserAnswer[] = questions.map((q) => {
      const key = keyOf(q)
      const sel = state.selected[key] ?? []
      const other = state.other[key] ?? ''
      return {
        question_header: key,
        answer: sel.length > 0 ? sel.join(', ') : other,
        selected: sel.length > 0 ? sel : undefined,
      }
    })
    if (tool.askUserId) actions.submitAskUser(tool.askUserId, answers)
  }, [actions, keyOf, questions, state, tool.askUserId])

  const skip = useCallback(() => {
    if (tool.askUserId) actions.submitAskUser(tool.askUserId, [])
  }, [actions, tool.askUserId])

  // Digit-key shortcuts (1-9) select an option for the first unanswered question.
  useEffect(() => {
    if (!isPending) return
    function onKey(e: KeyboardEvent) {
      if (e.target instanceof HTMLInputElement || e.target instanceof HTMLTextAreaElement) return
      const n = Number(e.key)
      if (!Number.isInteger(n) || n < 1 || n > 9) return
      const q = questions.find((qq) => (state.selected[keyOf(qq)]?.length ?? 0) === 0)
      if (!q?.options || n > q.options.length) return
      e.preventDefault()
      toggleOption(q, q.options[n - 1].label)
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [isPending, questions, state.selected, keyOf, toggleOption])

  if (!isPending) {
    const answers = parseResolvedAnswers(tool)
    return <div className={className}>{renderResolved?.(tool, answers) ?? <DefaultResolved tool={tool} answers={answers} />}</div>
  }

  const controls: AskUserControls = {
    selected: state.selected,
    other: state.other,
    toggleOption,
    setOther,
    submit,
    skip,
  }

  return (
    <div className={className} data-ask-user-id={tool.askUserId}>
      {renderPending?.(questions, controls) ?? <DefaultPending questions={questions} controls={controls} />}
    </div>
  )
}

/** Extract the questions list from tool fields (with fallbacks). */
function extractQuestions(tool: ToolCall): AskUserQuestion[] {
  if (tool.askUserQuestions && tool.askUserQuestions.length > 0) return tool.askUserQuestions
  try {
    const parsed = JSON.parse(tool.args)
    if (Array.isArray(parsed.questions)) return parsed.questions as AskUserQuestion[]
    if (parsed.question) {
      // legacy single-question shape
      return [{ question: parsed.question, options: parsed.options ?? [] }]
    }
  } catch {
    // ignore
  }
  return []
}

/** Best-effort parse of a resolved tool's output into answers (for replay). */
export function parseResolvedAnswers(tool: ToolCall): AskUserAnswer[] {
  if (!tool.output) return []
  try {
    const parsed = JSON.parse(tool.output)
    if (Array.isArray(parsed.answers)) return parsed.answers as AskUserAnswer[]
  } catch {
    // fall through to text parse
  }
  // "User's answer: X" form.
  const m = tool.output.match(/User'?s answer:\s*(.+)/i)
  if (m) return [{ question_header: '', answer: m[1].trim() }]
  return []
}

function toggle(arr: string[] | undefined, label: string): string[] {
  const set = new Set(arr ?? [])
  if (set.has(label)) set.delete(label)
  else set.add(label)
  return [...set]
}

function DefaultPending({ questions, controls }: { questions: AskUserQuestion[]; controls: AskUserControls }): ReactNode {
  return (
    <div style={{ border: '1px solid', padding: 8 }}>
      {questions.map((q, qi) => {
        const key = q.header ?? q.question
        const sel = controls.selected[key] ?? []
        return (
          <div key={qi} style={{ marginBottom: 8 }}>
            {q.header && <div style={{ fontWeight: 600 }}>{q.header}</div>}
            <div>{q.question}</div>
            {(q.options ?? []).map((opt, oi) => {
              const active = sel.includes(opt.label)
              return (
                <button
                  key={opt.label}
                  type="button"
                  onClick={() => controls.toggleOption(q, opt.label)}
                  style={{ display: 'block', width: '100%', textAlign: 'left', fontWeight: active ? 'bold' : 'normal' }}
                >
                  {oi < 9 ? `${oi + 1}. ` : ''}
                  {opt.label}
                  {opt.description ? ` — ${opt.description}` : ''}
                </button>
              )
            })}
            <input
              type="text"
              placeholder="Other…"
              value={controls.other[key] ?? ''}
              onChange={(e) => controls.setOther(q, e.target.value)}
            />
          </div>
        )
      })}
      <div style={{ display: 'flex', gap: 8 }}>
        <button type="button" onClick={controls.submit}>Submit</button>
        <button type="button" onClick={controls.skip}>Skip</button>
      </div>
    </div>
  )
}

function DefaultResolved({ answers }: { tool: ToolCall; answers: AskUserAnswer[] }): ReactNode {
  if (answers.length === 0) return <span>· no answer</span>
  return (
    <ul>
      {answers.map((a, i) => (
        <li key={i}>
          {a.question_header ? `${a.question_header}: ` : ''}
          {a.answer}
        </li>
      ))}
    </ul>
  )
}
