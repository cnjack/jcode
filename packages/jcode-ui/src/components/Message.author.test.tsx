import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { RuntimeProvider, createMockRuntime } from 'jcode-ui-core/runtime'
import { Message } from './Message.js'

describe('Message attributed author contract', () => {
  it('renders a host-provided author through the shared Message chrome', () => {
    const runtime = createMockRuntime()
    render(
      <RuntimeProvider runtime={runtime}>
        <Message
          message={{
            id: 'cloud-user-1',
            role: 'user',
            content: 'Please continue',
            timestamp: 1,
            author: 'Ada Lovelace',
          }}
        />
      </RuntimeProvider>,
    )

    expect(screen.getByTestId('thread-message-user')).toBeTruthy()
    expect(screen.getByText('Ada Lovelace')).toBeTruthy()
    expect(screen.queryByText('You')).toBeNull()
  })
})
