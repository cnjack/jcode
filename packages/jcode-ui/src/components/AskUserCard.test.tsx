import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { RuntimeProvider, createMockRuntime } from 'jcode-ui-core/runtime'
import type { AskUserQuestion, ToolCall } from 'jcode-ui-core'
import { AskUserCard } from './AskUserCard.js'

afterEach(cleanup)

const QUESTIONS: AskUserQuestion[] = [
  {
    header: 'Place',
    question: 'Where should we work?',
    options: [
      { label: 'Home (Recommended)', description: 'Quiet and focused.' },
      { label: 'Studio', description: 'More room to collaborate.' },
    ],
  },
  {
    header: 'Time',
    question: 'When should we start?',
  },
  {
    header: 'Focus',
    question: 'What should we include?',
    multi_select: true,
    options: [
      { label: 'Tests', description: 'Cover the critical flow.' },
      { label: 'Docs', description: 'Record the final behavior.' },
      { label: 'Release', description: 'Prepare the rollout.' },
    ],
  },
]

function pendingTool(questions: AskUserQuestion[] = QUESTIONS): ToolCall {
  return {
    id: 'ask-tool',
    name: 'ask_user',
    args: JSON.stringify({ questions }),
    status: 'running',
    timestamp: 1,
    askUserId: 'ask-request',
    askUserQuestions: questions,
  }
}

function renderCard(tool = pendingTool(), runtime = createMockRuntime()) {
  const view = render(
    <RuntimeProvider runtime={runtime}>
      <AskUserCard tool={tool} placement="dock" />
    </RuntimeProvider>,
  )
  return { runtime, ...view }
}

function primary(container: HTMLElement): HTMLButtonElement {
  const button = container.querySelector<HTMLButtonElement>('.jcode-ask-user__primary')
  if (!button) throw new Error('primary Ask User button not found')
  return button
}

describe('AskUserCard', () => {
  it('pages one question at a time, preserves answers, and submits the raw batch values', () => {
    const { container, runtime } = renderCard()

    expect(screen.getByText('Where should we work?')).toBeTruthy()
    expect(screen.queryByText('When should we start?')).toBeNull()
    expect(screen.getByText('1 / 3')).toBeTruthy()
    expect(screen.getByText('Recommended')).toBeTruthy()
    expect(screen.queryByText('(Recommended)')).toBeNull()

    fireEvent.click(screen.getByText('Home').closest('button')!)
    expect(screen.getByText('When should we start?')).toBeTruthy()

    const custom = screen.getByRole('textbox') as HTMLInputElement
    fireEvent.change(custom, { target: { value: 'Tomorrow morning' } })
    fireEvent.click(screen.getByRole('button', { name: 'Previous question' }))
    expect(screen.getByText('Where should we work?')).toBeTruthy()
    expect(screen.getByText('Home').closest('button')?.getAttribute('aria-pressed')).toBe('true')

    fireEvent.click(primary(container))
    expect((screen.getByRole('textbox') as HTMLInputElement).value).toBe('Tomorrow morning')
    fireEvent.click(primary(container))

    expect(screen.getByText('What should we include?')).toBeTruthy()
    expect(screen.getByText('Select all that apply')).toBeTruthy()
    fireEvent.click(screen.getByText('Tests').closest('button')!)
    fireEvent.click(screen.getByText('Docs').closest('button')!)
    fireEvent.click(primary(container))

    expect(runtime.calls).toContainEqual({
      action: 'submitAskUser',
      args: [
        'ask-request',
        [
          { question_header: 'Place', answer: 'Home (Recommended)', selected: ['Home (Recommended)'] },
          { question_header: 'Time', answer: 'Tomorrow morning', selected: undefined },
          { question_header: 'Focus', answer: 'Tests, Docs', selected: ['Tests', 'Docs'] },
        ],
      ],
    })
  })

  it('keeps option and custom answers mutually exclusive', () => {
    const { container } = renderCard(pendingTool([QUESTIONS[0]]))
    const option = screen.getByText('Studio').closest('button')!
    const custom = screen.getByRole('textbox') as HTMLInputElement

    fireEvent.click(option)
    expect(option.getAttribute('aria-pressed')).toBe('true')
    fireEvent.change(custom, { target: { value: 'Library' } })
    expect(option.getAttribute('aria-pressed')).toBe('false')
    fireEvent.click(option)
    expect(custom.value).toBe('')
    expect(primary(container).disabled).toBe(false)
  })

  it('scopes digit shortcuts to the visible question and ignores focused inputs', () => {
    renderCard(pendingTool(QUESTIONS.slice(0, 2)))
    fireEvent.keyDown(window, { key: '2' })
    expect(screen.getByText('When should we start?')).toBeTruthy()

    const custom = screen.getByRole('textbox')
    fireEvent.change(custom, { target: { value: 'Afternoon' } })
    fireEvent.keyDown(custom, { key: '1' })
    expect((custom as HTMLInputElement).value).toBe('Afternoon')

    fireEvent.click(screen.getByRole('button', { name: 'Previous question' }))
    expect(screen.getByText('Studio').closest('button')?.getAttribute('aria-pressed')).toBe('true')
  })

  it('auto-advances after a single-select choice but stays on the last question', () => {
    const { container, runtime } = renderCard(pendingTool(QUESTIONS.slice(0, 2)))

    fireEvent.click(screen.getByText('Studio').closest('button')!)
    expect(screen.getByText('When should we start?')).toBeTruthy()
    expect(runtime.calls).toEqual([])

    fireEvent.change(screen.getByRole('textbox'), { target: { value: 'Tonight' } })
    expect(screen.getByText('When should we start?')).toBeTruthy()
    expect(primary(container).textContent).toContain('Submit')
    expect(runtime.calls).toEqual([])
  })

  it('does not auto-advance multi-select questions', () => {
    const { container } = renderCard()
    fireEvent.click(screen.getByText('Home').closest('button')!)
    fireEvent.change(screen.getByRole('textbox'), { target: { value: 'Tonight' } })
    fireEvent.click(primary(container))

    expect(screen.getByText('What should we include?')).toBeTruthy()
    fireEvent.click(screen.getByText('Tests').closest('button')!)
    expect(screen.getByText('What should we include?')).toBeTruthy()
    expect(screen.getByText('Tests').closest('button')?.getAttribute('aria-pressed')).toBe('true')
  })

  it('locks duplicate actions while submitting', () => {
    let resolveSubmission: (() => void) | undefined
    const submitAskUser = vi.fn(() => new Promise<void>((resolve) => { resolveSubmission = resolve }))
    const runtime = createMockRuntime({ actions: { submitAskUser } })
    const { container } = renderCard(pendingTool([QUESTIONS[0]]), runtime)

    fireEvent.click(screen.getByText('Home').closest('button')!)
    const submit = primary(container)
    fireEvent.click(submit)
    fireEvent.click(submit)
    fireEvent.click(screen.getByRole('button', { name: 'Skip' }))

    expect(submitAskUser).toHaveBeenCalledTimes(1)
    expect(submit.disabled).toBe(true)
    expect(screen.getByText('Submitting…')).toBeTruthy()
    resolveSubmission?.()
  })

  it('shows an inline error and permits retry after submission fails', async () => {
    const submitAskUser = vi
      .fn()
      .mockRejectedValueOnce(new Error('offline'))
      .mockResolvedValueOnce(undefined)
    const runtime = createMockRuntime({ actions: { submitAskUser } })
    const { container } = renderCard(pendingTool([QUESTIONS[0]]), runtime)

    fireEvent.click(screen.getByText('Studio').closest('button')!)
    fireEvent.click(primary(container))
    await waitFor(() => expect(screen.getByRole('alert')).toBeTruthy())
    expect(primary(container).disabled).toBe(false)

    fireEvent.click(primary(container))
    expect(submitAskUser).toHaveBeenCalledTimes(2)
  })

  it('distinguishes a skipped receipt from a replay with no recorded output', () => {
    const skipped: ToolCall = {
      ...pendingTool([QUESTIONS[0]]),
      status: 'done',
      askUserId: undefined,
      output: 'The user did not provide any answers.',
    }
    const view = renderCard(skipped)
    expect(screen.getByText('· Skipped')).toBeTruthy()

    view.rerender(
      <RuntimeProvider runtime={view.runtime}>
        <AskUserCard tool={{ ...skipped, output: undefined }} />
      </RuntimeProvider>,
    )
    expect(screen.getByText('· No recorded answer')).toBeTruthy()
  })
})
