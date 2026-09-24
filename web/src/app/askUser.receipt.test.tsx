import { act, cleanup, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { RuntimeProvider } from 'jcode-ui-core/runtime'
import { Thread } from 'jcode-ui'
import { api } from '../lib/api'
import { useChatRuntime } from './runtime'
import { chatActions, replayTimeline, store, submitAskUser } from './store'
import { createWSHandlers } from './wsBridge'

Object.defineProperty(HTMLElement.prototype, 'scrollTo', { configurable: true, value: vi.fn() })

afterEach(() => {
  cleanup()
  store.dispatch(chatActions.clearChat())
  vi.restoreAllMocks()
})

function ReceiptThread() {
  return (
    <RuntimeProvider runtime={useChatRuntime()}>
      <Thread virtualize={false} hidePendingAskUser />
    </RuntimeProvider>
  )
}

describe('Ask User answer receipts', () => {
  it.each(['api-first', 'events-first'])(
    'renders the answer once through %s submission, late events, and history replay',
    async (order) => {
      const questions = [{ header: 'Deploy', question: 'How should we deploy?' }]
      const args = JSON.stringify({ questions })
      const output = "User's answer: MLX 可以吗？"
      const handlers = createWSHandlers(store.getState, store.dispatch)
      const deliverResult = () => {
        handlers.onToolCall?.({ name: 'ask_user', args, tool_call_id: 'call-1' })
        handlers.onToolResult?.({ name: 'ask_user', output, tool_call_id: 'call-1' })
      }
      vi.spyOn(api, 'askUser').mockImplementation(async () => {
        if (order === 'events-first') deliverResult()
        return { status: 'ok' }
      })
      handlers.onAskUserRequest?.({ id: 'ask-1', questions })
      render(<ReceiptThread />)
      expect(screen.queryByText('MLX 可以吗？')).toBeNull()

      await act(async () => {
        await store.dispatch(submitAskUser({
          id: 'ask-1', answers: [{ question_header: 'Deploy', answer: 'MLX 可以吗？' }],
        })).unwrap()
      })
      expect(screen.getAllByText('MLX 可以吗？')).toHaveLength(1)
      if (order === 'api-first') act(deliverResult)
      expect(screen.getAllByText('MLX 可以吗？')).toHaveLength(1)

      act(() => {
        store.dispatch(chatActions.setTimeline(replayTimeline([
          { type: 'tool_call', name: 'ask_user', args, tool_call_id: 'call-1', timestamp: '2026-09-10T17:50:13+08:00' },
          { type: 'tool_result', name: 'ask_user', output, tool_call_id: 'call-1', timestamp: '2026-09-10T17:50:52+08:00' },
        ], true)))
      })
      expect(screen.getAllByText('MLX 可以吗？')).toHaveLength(1)
    },
  )
})
