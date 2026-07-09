/**
 * AskUserCard — styled interactive question block (wraps headless AskUserBlock).
 *
 * Uses the primitive's `controls` API (toggleOption/setOther/submit/skip) so no
 * local state is needed here. Token-driven styling with markdown question bodies.
 */

import { memo } from 'react'
import type { AskUserQuestion, ToolCall, AskUserAnswer } from 'jcode-ui-core'
import type { AskUserControls } from 'jcode-ui-core/primitives'
import { AskUserBlock } from 'jcode-ui-core/primitives'
import { renderMarkdown } from '../lib/markdown.js'

export interface AskUserCardProps {
  tool: ToolCall
}

export const AskUserCard = memo(function AskUserCard({ tool }: AskUserCardProps) {
  return (
    <AskUserBlock
      tool={tool}
      className="jcode-ask-user px-4 py-1"
      renderPending={(questions, controls) => <PendingCard questions={questions} controls={controls} />}
      renderResolved={(t, answers) => <ResolvedCard tool={t} answers={answers} />}
    />
  )
})

function PendingCard({ questions, controls }: { questions: AskUserQuestion[]; controls: AskUserControls }) {
  return (
    <div className="jcode-interactive-card my-1.5 border border-[var(--color-border)] bg-[var(--color-surface)] px-3.5 py-3">
      {questions.map((q, qi) => {
        const key = q.header ?? q.question
        const sel = controls.selected[key] ?? []
        return (
          <div key={qi} className={qi > 0 ? 'mt-3.5 border-t border-[var(--color-border)] pt-3.5' : ''}>
            {q.header && (
              <div className="mb-1 text-[0.68rem] font-semibold uppercase tracking-wider text-[var(--color-muted-foreground)]">
                {q.header}
              </div>
            )}
            <div
              className="jcode-prose mb-2.5 text-[0.9rem] text-[var(--color-foreground)]"
              dangerouslySetInnerHTML={{ __html: renderMarkdown(q.question) }}
            />
            <div className="space-y-1.5">
              {(q.options ?? []).map((opt, oi) => {
                const active = sel.includes(opt.label)
                return (
                  <button
                    key={opt.label}
                    type="button"
                    onClick={() => controls.toggleOption(q, opt.label)}
                    className={`flex w-full items-center gap-2.5 rounded-[var(--radius-lg)] border px-3 py-2 text-left text-[0.84rem] transition-all ${
                      active
                        ? 'border-[var(--color-primary)] bg-[var(--accent-wash)] text-[var(--color-foreground)] shadow-[var(--shadow-sm)]'
                        : 'border-[var(--color-border)] bg-[var(--color-muted)] text-[var(--color-foreground)] hover:bg-[var(--neutral-wash-soft)]'
                    }`}
                  >
                    {oi < 9 && (
                      <span
                        className={`flex h-5 w-5 shrink-0 items-center justify-center rounded-[var(--radius-sm)] font-mono text-[0.68rem] ${
                          active
                            ? 'bg-[var(--color-primary)] text-[var(--color-on-primary)]'
                            : 'bg-[var(--color-surface)] text-[var(--color-muted-foreground)]'
                        }`}
                      >
                        {oi + 1}
                      </span>
                    )}
                    <span className="min-w-0 flex-1">
                      <span className="block truncate font-medium">{opt.label}</span>
                      {opt.description && (
                        <span className="mt-0.5 block truncate text-[0.74rem] text-[var(--color-muted-foreground)]">
                          {opt.description}
                        </span>
                      )}
                    </span>
                  </button>
                )
              })}
            </div>
            <input
              type="text"
              placeholder="Other…"
              value={controls.other[key] ?? ''}
              onChange={(e) => controls.setOther(q, e.target.value)}
              className="mt-2 w-full rounded-[var(--radius-lg)] border border-[var(--color-border)] bg-[var(--color-muted)] px-3 py-1.5 text-[0.82rem] text-[var(--color-foreground)] outline-none transition-[border-color,box-shadow] focus:border-[var(--color-primary)] focus:shadow-[0_0_0_3px_var(--accent-wash)]"
            />
          </div>
        )
      })}
      <div className="mt-3.5 flex gap-2">
        <button type="button" onClick={controls.submit} className="jcode-btn jcode-btn-primary">
          Submit
        </button>
        <button type="button" onClick={controls.skip} className="jcode-btn jcode-btn-secondary">
          Skip
        </button>
      </div>
    </div>
  )
}

function ResolvedCard({ answers }: { tool: ToolCall; answers: AskUserAnswer[] }) {
  if (answers.length === 0) {
    return <div className="px-4 py-1 text-[0.82rem] text-[var(--color-muted-foreground)]">· skipped</div>
  }
  return (
    <div className="my-1 space-y-1 rounded-[var(--radius-md)] border border-[var(--color-border)] bg-[var(--color-muted)] px-3 py-2 text-[0.8rem]">
      {answers.map((a, i) => (
        <div key={i}>
          {a.question_header && <span className="text-[var(--color-muted-foreground)]">{a.question_header}: </span>}
          <span className="text-[var(--color-foreground)]">{a.answer}</span>
        </div>
      ))}
    </div>
  )
}
