/**
 * Ask-user optimistic resolve: the docked card must leave pending the moment
 * /api/ask succeeds, without waiting for the later tool_result event.
 */
import { afterEach, describe, expect, it, vi } from 'vitest'
import {
  chatActions,
  formatAskUserOutput,
  store,
  submitAskUser,
} from './store'
import { api } from '../lib/api'

afterEach(() => {
  store.dispatch(chatActions.clearChat())
  vi.restoreAllMocks()
})

describe('formatAskUserOutput', () => {
  it('mirrors the backend single- and multi-question shapes', () => {
    expect(formatAskUserOutput([])).toBe('The user did not provide any answers.')
    expect(formatAskUserOutput([{ question_header: 'Place', answer: 'Home' }])).toBe(
      "User's answer: Home",
    )
    expect(
      formatAskUserOutput([
        { question_header: 'Place', answer: 'Home' },
        { question_header: 'Time', answer: 'Now' },
      ]),
    ).toBe(
      JSON.stringify({
        answers: [
          { question_header: 'Place', answer: 'Home' },
          { question_header: 'Time', answer: 'Now' },
        ],
      }),
    )
  })
})

describe('addToolCall ask_user merge', () => {
  it('folds a late tool_call into the pending ask_user_request row', () => {
    store.dispatch(
      chatActions.attachAskUser({
        toolName: 'ask_user',
        askUserId: 'ask-merge',
        questions: [{ header: 'Place', question: 'Where?' }],
      }),
    )
    store.dispatch(
      chatActions.addToolCall({
        name: 'ask_user',
        args: JSON.stringify({ questions: [{ header: 'Place', question: 'Where?' }] }),
        toolCallID: 'tc-merge',
      }),
    )

    const tools = store.getState().chat.timeline.filter((item) => item.kind === 'tool')
    expect(tools).toHaveLength(1)
    if (tools[0]?.kind !== 'tool') return
    expect(tools[0].data.askUserId).toBe('ask-merge')
    expect(tools[0].data.toolCallID).toBe('tc-merge')
    expect(tools[0].data.status).toBe('running')
  })
})

describe('resolveAskUserItem', () => {
  it('marks the matching ask_user tool done and clears pending markers', () => {
    store.dispatch(
      chatActions.attachAskUser({
        toolName: 'ask_user',
        askUserId: 'ask-1',
        questions: [{ header: 'Place', question: 'Where?' }],
        taskId: 'task-1',
      }),
    )

    store.dispatch(
      chatActions.resolveAskUserItem({
        id: 'ask-1',
        answers: [{ question_header: 'Place', answer: 'Home' }],
      }),
    )

    const tool = store.getState().chat.timeline.find((item) => item.kind === 'tool')
    expect(tool?.kind).toBe('tool')
    if (tool?.kind !== 'tool') return
    expect(tool.data.status).toBe('done')
    expect(tool.data.askUserId).toBeUndefined()
    expect(tool.data.askUserQuestions).toBeUndefined()
    expect(tool.data.output).toBe("User's answer: Home")
    expect((tool.data as { askUserTaskId?: string }).askUserTaskId).toBeUndefined()
  })
})

describe('submitAskUser', () => {
  it('optimistically resolves the card after a successful API submit', async () => {
    const askUser = vi.spyOn(api, 'askUser').mockResolvedValue(undefined as never)
    store.dispatch(
      chatActions.attachAskUser({
        toolName: 'ask_user',
        askUserId: 'ask-2',
        questions: [{ header: 'PR', question: 'Split?' }],
        taskId: 'task-2',
      }),
    )

    await store
      .dispatch(
        submitAskUser({
          id: 'ask-2',
          answers: [{ question_header: 'PR', answer: 'One PR' }],
        }),
      )
      .unwrap()

    expect(askUser).toHaveBeenCalledWith(
      'ask-2',
      [{ question_header: 'PR', answer: 'One PR' }],
      'task-2',
    )
    const tool = store.getState().chat.timeline.find((item) => item.kind === 'tool')
    expect(tool?.kind).toBe('tool')
    if (tool?.kind !== 'tool') return
    expect(tool.data.status).toBe('done')
    expect(tool.data.askUserId).toBeUndefined()
    expect(tool.data.output).toBe("User's answer: One PR")
  })

  it('keeps the pending card when the API submit fails', async () => {
    vi.spyOn(api, 'askUser').mockRejectedValue(new Error('offline'))
    store.dispatch(
      chatActions.attachAskUser({
        toolName: 'ask_user',
        askUserId: 'ask-3',
        questions: [{ header: 'PR', question: 'Split?' }],
      }),
    )

    await expect(
      store
        .dispatch(
          submitAskUser({
            id: 'ask-3',
            answers: [{ question_header: 'PR', answer: 'Two PRs' }],
          }),
        )
        .unwrap(),
    ).rejects.toThrow('offline')

    const tool = store.getState().chat.timeline.find((item) => item.kind === 'tool')
    expect(tool?.kind).toBe('tool')
    if (tool?.kind !== 'tool') return
    expect(tool.data.status).toBe('running')
    expect(tool.data.askUserId).toBe('ask-3')
    expect(tool.data.output).toBeUndefined()
  })
})
