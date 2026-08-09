/**
 * AskUserCard — styled interactive question surface (wraps AskUserBlock).
 *
 * Pending batches are presented one question at a time so the card can own the
 * product's bottom interaction dock without growing into the transcript. The
 * headless primitive still owns selections, keyboard shortcuts and submission.
 */

import { memo, useId } from 'react'
import {
  ArrowPathIcon,
  CheckIcon,
  ChevronLeftIcon,
  ChevronRightIcon,
  PencilSquareIcon,
  QuestionMarkCircleIcon,
} from '@heroicons/react/24/outline'
import type { AskUserQuestion, ToolCall, AskUserAnswer } from 'jcode-ui-core'
import type { AskUserControls } from 'jcode-ui-core/primitives'
import { AskUserBlock } from 'jcode-ui-core/primitives'
import { renderMarkdown } from '../lib/markdown.js'

export interface AskUserCardStrings {
  title: string
  helper: string
  previous: string
  next: string
  skip: string
  submit: string
  submitting: string
  customPlaceholder: string
  recommended: string
  multiSelect: string
  skipped: string
  noAnswer: string
  submitError: string
}

export interface AskUserCardProps {
  tool: ToolCall
  /** Docked cards replace the product composer while the agent waits. */
  placement?: 'timeline' | 'dock'
  /** Host-provided copy keeps the package backend and i18n agnostic. */
  strings?: Partial<AskUserCardStrings>
}

const DEFAULT_STRINGS: AskUserCardStrings = {
  title: 'Your input is needed',
  helper: 'Choose an option or add your own answer.',
  previous: 'Previous question',
  next: 'Next',
  skip: 'Skip',
  submit: 'Submit',
  submitting: 'Submitting…',
  customPlaceholder: 'Other answer or additional context…',
  recommended: 'Recommended',
  multiSelect: 'Select all that apply',
  skipped: 'Skipped',
  noAnswer: 'No recorded answer',
  submitError: "Couldn't submit your answer. Try again.",
}

export const AskUserCard = memo(function AskUserCard({
  tool,
  placement = 'timeline',
  strings: stringOverrides,
}: AskUserCardProps) {
  const strings = { ...DEFAULT_STRINGS, ...stringOverrides }
  return (
    <AskUserBlock
      tool={tool}
      data-jcode-ui=""
      className={`jcode-ask-user jcode-ask-user--${placement}`}
      renderPending={(questions, controls) => (
        <PendingCard questions={questions} controls={controls} strings={strings} />
      )}
      renderResolved={(t, answers) => <ResolvedCard tool={t} answers={answers} strings={strings} />}
    />
  )
})

function PendingCard({
  questions,
  controls,
  strings,
}: {
  questions: AskUserQuestion[]
  controls: AskUserControls
  strings: AskUserCardStrings
}) {
  const headingId = useId()
  const question = questions[controls.activeIndex]
  if (!question) return null

  const key = question.header ?? question.question
  const selected = controls.selected[key] ?? []
  const other = controls.other[key] ?? ''
  const answered = selected.length > 0 || other.trim().length > 0
  const isLast = controls.activeIndex === questions.length - 1

  const advance = () => {
    if (!answered || controls.isSubmitting) return
    if (isLast) void controls.submit()
    else controls.setActiveIndex(controls.activeIndex + 1)
  }

  return (
    <section className="jcode-interactive-card" aria-labelledby={headingId} aria-busy={controls.isSubmitting}>
      <header className="jcode-ask-user__header">
        <div className="jcode-ask-user__eyebrow">
          <QuestionMarkCircleIcon className="h-4 w-4" aria-hidden="true" />
          <span>{strings.title}</span>
          {question.multi_select && <span className="jcode-ask-user__multi">{strings.multiSelect}</span>}
        </div>
        <div className="jcode-ask-user__pager" aria-label={`${controls.activeIndex + 1} / ${questions.length}`}>
          <button
            type="button"
            className="jcode-ask-user__pager-button"
            aria-label={strings.previous}
            disabled={controls.activeIndex === 0 || controls.isSubmitting}
            onClick={() => controls.setActiveIndex(controls.activeIndex - 1)}
          >
            <ChevronLeftIcon className="h-3.5 w-3.5" aria-hidden="true" />
          </button>
          <span className="jcode-ask-user__progress">{controls.activeIndex + 1} / {questions.length}</span>
          <button
            type="button"
            className="jcode-ask-user__pager-button"
            aria-label={strings.next}
            disabled={isLast || !answered || controls.isSubmitting}
            onClick={advance}
          >
            <ChevronRightIcon className="h-3.5 w-3.5" aria-hidden="true" />
          </button>
        </div>
      </header>

      <div className="jcode-ask-user__body">
        {question.header && <div className="jcode-ask-user__question-label">{question.header}</div>}
        <div
          id={headingId}
          className="jcode-prose jcode-ask-user__question"
          dangerouslySetInnerHTML={{ __html: renderMarkdown(question.question) }}
        />
        <p className="jcode-ask-user__helper">{strings.helper}</p>

        {!!question.options?.length && (
          <div className="jcode-ask-user__options" role="group" aria-labelledby={headingId}>
            {question.options.map((option, optionIndex) => {
              const active = selected.includes(option.label)
              const display = optionDisplay(option.label)
              return (
                <button
                  key={option.label}
                  type="button"
                  aria-pressed={active}
                  disabled={controls.isSubmitting}
                  onClick={() => controls.toggleOption(question, option.label)}
                  className={`jcode-ask-user__option${active ? ' is-selected' : ''}`}
                >
                  <span className="jcode-ask-user__key" aria-hidden="true">{optionIndex + 1}</span>
                  <span className="jcode-ask-user__option-copy">
                    <span className="jcode-ask-user__option-heading">
                      <span className="jcode-ask-user__option-label">{display.label}</span>
                      {display.recommended && (
                        <span className="jcode-ask-user__recommended">{strings.recommended}</span>
                      )}
                    </span>
                    {option.description && (
                      <span className="jcode-ask-user__option-description">{option.description}</span>
                    )}
                  </span>
                  <span className="jcode-ask-user__check" aria-hidden="true">
                    {active && <CheckIcon className="h-3.5 w-3.5" />}
                  </span>
                </button>
              )
            })}
          </div>
        )}
      </div>

      <footer className="jcode-ask-user__footer">
        <label className={`jcode-ask-user__custom${other ? ' has-value' : ''}`}>
          <PencilSquareIcon className="h-4 w-4" aria-hidden="true" />
          <input
            type="text"
            placeholder={strings.customPlaceholder}
            aria-label={strings.customPlaceholder}
            value={other}
            disabled={controls.isSubmitting}
            onChange={(event) => controls.setOther(question, event.target.value)}
            onKeyDown={(event) => {
              if (event.key === 'Enter' && !event.nativeEvent.isComposing) {
                event.preventDefault()
                advance()
              }
            }}
          />
        </label>
        <button
          type="button"
          className="jcode-ask-user__skip"
          disabled={controls.isSubmitting}
          onClick={() => void controls.skip()}
        >
          {strings.skip}
        </button>
        <button
          type="button"
          className="jcode-btn jcode-btn-primary jcode-ask-user__primary"
          disabled={!answered || controls.isSubmitting}
          onClick={advance}
        >
          {controls.isSubmitting ? (
            <>
              <ArrowPathIcon className="h-3.5 w-3.5 animate-spin" aria-hidden="true" />
              {strings.submitting}
            </>
          ) : (
            <>
              {isLast ? strings.submit : strings.next}
              {!isLast && <ChevronRightIcon className="h-3.5 w-3.5" aria-hidden="true" />}
            </>
          )}
        </button>
      </footer>
      {controls.submitError && <p className="jcode-ask-user__error" role="alert">{strings.submitError}</p>}
    </section>
  )
}

function ResolvedCard({ tool, answers, strings }: { tool: ToolCall; answers: AskUserAnswer[]; strings: AskUserCardStrings }) {
  if (answers.length === 0) {
    return <div className="jcode-ask-user-resolved">· {tool.output ? strings.skipped : strings.noAnswer}</div>
  }
  return (
    <div className="jcode-ask-user-resolved">
      {answers.map((answer, index) => (
        <div key={index} className="jcode-ask-user-resolved__answer">
          {answer.question_header && <span className="jcode-ask-user-resolved__label">{answer.question_header}: </span>}
          <span>{answer.answer}</span>
        </div>
      ))}
    </div>
  )
}

function optionDisplay(label: string): { label: string; recommended: boolean } {
  const match = label.match(/\s*(?:\((?:recommended|推荐)\)|（推荐）)\s*$/i)
  if (!match) return { label, recommended: false }
  return { label: label.slice(0, match.index).trim(), recommended: true }
}
