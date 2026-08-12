import { describe, expect, it, vi } from 'vitest'
import type { AppDispatch, RootState } from './store'
import { createWSHandlers } from './wsBridge'

describe('user-message WebSocket bridge', () => {
  it('drops a local echo but preserves Cloud-originated user turns', () => {
    const dispatch = vi.fn()
    const handlers = createWSHandlers(
      () => ({ session: { currentSessionId: 'task-1' } }) as RootState,
      dispatch as unknown as AppDispatch,
    )

    handlers.onUserMessage?.({ content: 'local prompt', source: '', local_echo: true })
    expect(dispatch).not.toHaveBeenCalled()

    handlers.onUserMessage?.({ content: 'remote prompt', source: 'console' })
    expect(dispatch).toHaveBeenCalledTimes(2)
    expect(dispatch.mock.calls[0][0]).toMatchObject({
      type: 'chat/addMessage',
      payload: { role: 'user', content: 'remote prompt', source: 'console' },
    })
    expect(dispatch.mock.calls[1][0]).toMatchObject({ type: 'chat/setRunning', payload: true })
  })
})
