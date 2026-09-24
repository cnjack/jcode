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
  it.each([
    { question: 'Deploy?', options: [{ label: 'MLX', description: 'Local' }, { label: 'CUDA' }] },
    { questions: [{ header: 'Long header from the model', question: 'Deploy?', options: [{ label: 'MLX' }, { label: 'CUDA' }] }] },
  ])('matches backend-normalized questions to the original arguments: %j', (args) => {
    store.dispatch(chatActions.attachAskUser({
      toolName: 'ask_user', askUserId: 'ask-normalized',
      questions: [{ header: 'Long header', question: 'Deploy?', options: [{ label: 'MLX' }, { label: 'CUDA' }] }],
    }))
    store.dispatch(chatActions.resolveAskUserItem({ id: 'ask-normalized', answers: [] }))
    store.dispatch(chatActions.addToolCall({ name: 'ask_user', args: JSON.stringify(args), toolCallID: 'tc-normalized' }))
    store.dispatch(chatActions.resolveToolCall({ name: 'ask_user', toolCallID: 'tc-normalized', output: 'The user did not provide any answers.' }))
    const timeline = store.getState().chat.timeline
    expect(timeline).toHaveLength(1)
    expect(timeline[0].data).toMatchObject({ toolCallID: 'tc-normalized', status: 'done', output: 'The user did not provide any answers.' })
  })

  it('binds a late call to its answered placeholder instead of a different pending question', () => {
    const questions = [{ question: 'First?', header: 'First' }]
    store.dispatch(chatActions.attachAskUser({ toolName: 'ask_user', askUserId: 'ask-first', questions }))
    store.dispatch(chatActions.resolveAskUserItem({ id: 'ask-first', answers: [{ question_header: 'First', answer: 'Yes' }] }))
    store.dispatch(chatActions.attachAskUser({
      toolName: 'ask_user', askUserId: 'ask-second', questions: [{ question: 'Second?', header: 'Second' }],
    }))
    store.dispatch(chatActions.addToolCall({
      name: 'ask_user', args: JSON.stringify({ questions: [{ header: 'First', question: 'First?' }] }), toolCallID: 'tc-first',
    }))
    store.dispatch(chatActions.resolveToolCall({ name: 'ask_user', toolCallID: 'tc-first', output: "User's answer: Yes" }))
    const timeline = store.getState().chat.timeline
    expect(timeline).toHaveLength(2)
    expect(timeline[0].data).toMatchObject({ status: 'done', toolCallID: 'tc-first', output: "User's answer: Yes" })
    expect(timeline[1].data).toMatchObject({ status: 'running', askUserId: 'ask-second' })
  })

  it('keeps separate answers to repeated questions after the first result is final', () => {
    const questions = [{ question: 'Continue?' }]
    for (const id of ['first', 'second']) {
      store.dispatch(chatActions.attachAskUser({ toolName: 'ask_user', askUserId: id, questions }))
      store.dispatch(chatActions.resolveAskUserItem({ id, answers: [{ question_header: '', answer: 'Yes' }] }))
      store.dispatch(chatActions.addToolCall({ name: 'ask_user', args: JSON.stringify({ questions }), toolCallID: id }))
      store.dispatch(chatActions.resolveToolCall({ name: 'ask_user', toolCallID: id, output: "User's answer: Yes" }))
    }
    const timeline = store.getState().chat.timeline
    expect(timeline).toHaveLength(2)
    expect(timeline.map((item) => item.data)).toMatchObject([
      { toolCallID: 'first', output: "User's answer: Yes" },
      { toolCallID: 'second', output: "User's answer: Yes" },
    ])
  })

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
  it.each(['before-submit', 'after-submit'])(
    'keeps one receipt when tool_call arrives %s and tool_result follows the API response',
    async (callOrder) => {
      vi.spyOn(api, 'askUser').mockResolvedValue(undefined as never)
      const questions = [{ header: 'Deploy', question: 'How should we deploy?' }]
      store.dispatch(chatActions.attachAskUser({
        toolName: 'ask_user', askUserId: 'ask-late', questions,
      }))
      const original = store.getState().chat.timeline[0]
      const call = chatActions.addToolCall({
        name: 'ask_user', args: JSON.stringify({ questions }), toolCallID: 'tc-late',
      })
      if (callOrder === 'before-submit') store.dispatch(call)

      await store.dispatch(submitAskUser({
        id: 'ask-late', answers: [{ question_header: 'Deploy', answer: 'MLX 可以吗？' }],
      })).unwrap()
      if (callOrder === 'after-submit') store.dispatch(call)
      store.dispatch(chatActions.resolveToolCall({
        name: 'ask_user', toolCallID: 'tc-late', output: "User's answer: MLX 可以吗？",
        durationMs: 38724,
      }))

      const timeline = store.getState().chat.timeline
      expect(timeline).toHaveLength(1)
      expect(timeline[0].seq).toBe(original.seq)
      expect(timeline[0].data.id).toBe(original.data.id)
      expect(timeline[0].data).toMatchObject({
        toolCallID: 'tc-late', status: 'done', output: "User's answer: MLX 可以吗？",
        meta: { duration_ms: 38724 },
      })
      expect((timeline[0].data as { askUserId?: string }).askUserId).toBeUndefined()
    },
  )

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
