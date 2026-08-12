import { cleanup, render, screen } from '@testing-library/react'
import { Provider } from 'react-redux'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { conversationLoadActions, store } from '../app/store'
import { i18n } from '../i18n'
import { ConversationLoadingView } from './ConversationLoadingView'

beforeEach(async () => {
  Object.defineProperty(HTMLElement.prototype, 'scrollTo', {
    configurable: true,
    value: () => {},
  })
  await i18n.changeLanguage('en')
  store.dispatch(conversationLoadActions.reset())
})

afterEach(() => {
  cleanup()
  store.dispatch(conversationLoadActions.reset())
})

function begin() {
  store.dispatch(conversationLoadActions.begin({
    requestId: 'loading-request',
    target: { uuid: 'session-1', project: 'ssh://dev@example.com/workspace' },
  }))
}

function renderView() {
  return render(<Provider store={store}><ConversationLoadingView /></Provider>)
}

describe('ConversationLoadingView actions', () => {
  it('hides Retry for a backend non-retryable failure', () => {
    begin()
    store.dispatch(conversationLoadActions.failed({
      requestId: 'loading-request',
      error: 'Conversation no longer exists',
      code: 'conversation_not_found',
      retryable: false,
    }))

    renderView()

    expect(screen.queryByRole('button', { name: 'Retry' })).toBeNull()
    expect(screen.getByRole('button', { name: 'Cancel' })).toBeTruthy()
  })

  it('shows both expected and presented fingerprints after a confirmation mismatch', () => {
    begin()
    store.dispatch(conversationLoadActions.requireHostKey({
      requestId: 'loading-request',
      prompt: {
        code: 'ssh_host_key_confirmation_mismatch',
        error: 'key changed during confirmation',
        host: 'example.com',
        key_type: 'ssh-ed25519',
        fingerprint: 'SHA256:presented',
        expected_fingerprint: 'SHA256:expected',
      },
    }))

    renderView()

    expect(screen.getByText('SHA256:expected')).toBeTruthy()
    expect(screen.getByText('SHA256:presented')).toBeTruthy()
  })

  it('marks history preview inert instead of exposing dead interaction controls', () => {
    begin()
    store.dispatch(conversationLoadActions.historyReady({
      requestId: 'loading-request',
      timeline: [{
        kind: 'message',
        seq: 1,
        data: { id: 'message-1', role: 'user', content: 'saved turn', timestamp: 1 },
      }],
    }))

    renderView()

    expect(screen.getByLabelText('Read-only conversation history').hasAttribute('inert')).toBe(true)
  })
})
