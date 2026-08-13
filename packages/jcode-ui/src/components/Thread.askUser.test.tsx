import { act, cleanup, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { RuntimeProvider, createMockRuntime } from 'jcode-ui-core/runtime'
import type { ThreadItem, ToolCall } from 'jcode-ui-core'
import { Thread } from './Thread.js'

afterEach(cleanup)

Object.defineProperty(HTMLElement.prototype, 'scrollTo', {
  configurable: true,
  value: vi.fn(),
})

function askTool(overrides: Partial<ToolCall> = {}): ToolCall {
  return {
    id: 'ask-tool',
    name: 'ask_user',
    args: JSON.stringify({ questions: [{ header: 'Place', question: 'Hidden pending question?' }] }),
    status: 'running',
    timestamp: 1,
    askUserId: 'ask-request',
    askUserQuestions: [{ header: 'Place', question: 'Hidden pending question?' }],
    ...overrides,
  }
}

describe('Thread docked Ask User behavior', () => {
  it('filters pending Ask User before grouping and restores its resolved receipt', () => {
    const pending = askTool()
    const items: ThreadItem[] = [
      { kind: 'message', seq: 1, data: { id: 'm1', role: 'assistant', content: 'I need one detail.', timestamp: 1 } },
      { kind: 'tool', seq: 2, data: pending },
    ]
    const runtime = createMockRuntime({ items, isRunning: true })
    render(
      <RuntimeProvider runtime={runtime}>
        <Thread virtualize={false} hidePendingAskUser renderPending={() => null} />
      </RuntimeProvider>,
    )

    expect(screen.getByText('I need one detail.')).toBeTruthy()
    expect(screen.queryByText('Hidden pending question?')).toBeNull()

    act(() => {
      runtime.setItems([
        items[0],
        {
          kind: 'tool',
          seq: 2,
          data: askTool({
            status: 'done',
            askUserId: undefined,
            output: "User's answer: Home",
          }),
        },
      ])
      runtime.setRunning(false)
    })

    const receipt = screen.getByText('Home').closest('.jcode-ask-user-resolved')
    expect(receipt).toBeTruthy()
    expect(receipt?.closest('.jcode-standalone-tool')?.classList.contains('jcode-gutter')).toBe(true)
  })

  it('hides an in-flight Ask User even before askUserId is attached', () => {
    const items: ThreadItem[] = [
      { kind: 'message', seq: 1, data: { id: 'm1', role: 'assistant', content: 'Need a choice.', timestamp: 1 } },
      { kind: 'tool', seq: 2, data: askTool({ askUserId: undefined, askUserQuestions: undefined }) },
    ]
    render(
      <RuntimeProvider runtime={createMockRuntime({ items, isRunning: true })}>
        <Thread virtualize={false} hidePendingAskUser renderPending={() => null} />
      </RuntimeProvider>,
    )

    expect(screen.getByText('Need a choice.')).toBeTruthy()
    expect(screen.queryByText('Hidden pending question?')).toBeNull()
    expect(document.querySelector('.jcode-ask-user')).toBeNull()
  })
})
