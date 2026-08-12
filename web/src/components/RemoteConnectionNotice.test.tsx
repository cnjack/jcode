import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { Provider } from 'react-redux'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { api } from '../lib/api'
import { i18n } from '../i18n'
import {
  conversationLoadActions,
  cancelConversationLoad,
  remoteConnectionActions,
  sessionActions,
  store,
} from '../app/store'
import { RemoteConnectionNotice } from './RemoteConnectionNotice'

beforeEach(async () => {
  await i18n.changeLanguage('en')
  store.dispatch(conversationLoadActions.reset())
  store.dispatch(remoteConnectionActions.reset())
  store.dispatch(sessionActions.setCurrentSession('task-active'))
  store.dispatch(sessionActions.setProjectPath('ssh://dev@example.com/workspace'))
})

afterEach(() => {
  cleanup()
  vi.useRealTimers()
  vi.restoreAllMocks()
  store.dispatch(remoteConnectionActions.reset())
})

function renderNotice() {
  return render(<Provider store={store}><RemoteConnectionNotice /></Provider>)
}

describe('RemoteConnectionNotice', () => {
  it('announces a bounded backoff with attempt and delay without replacing the conversation', () => {
    store.dispatch(remoteConnectionActions.statusReceived({
      task_id: 'task-active',
      kind: 'ssh',
      status: 'waiting',
      attempt: 2,
      max_attempts: 8,
      retry_in_ms: 2_400,
      host: 'dev@example.com',
    }))

    renderNotice()

    expect(screen.getByRole('status', { name: 'Remote connection status' })).toBeTruthy()
    expect(screen.getByText('Attempt 2/8')).toBeTruthy()
    expect(screen.getByText('Retrying dev@example.com in about 3s.')).toBeTruthy()
  })

  it('does not expose a background task status in the foreground conversation', () => {
    store.dispatch(remoteConnectionActions.statusReceived({
      task_id: 'task-background',
      kind: 'ssh',
      status: 'reconnecting',
      attempt: 1,
      max_attempts: 8,
    }))

    renderNotice()

    expect(screen.queryByRole('status')).toBeNull()
  })

  it('does not let an old recovered timer clear a newer status revision', () => {
    vi.useFakeTimers()
    store.dispatch(remoteConnectionActions.statusReceived({
      task_id: 'task-active', kind: 'ssh', status: 'ready', attempt: 2, max_attempts: 8,
    }))
    renderNotice()

    act(() => vi.advanceTimersByTime(2_000))
    act(() => {
      store.dispatch(remoteConnectionActions.statusReceived({
        task_id: 'task-active', kind: 'ssh', status: 'ready', attempt: 3, max_attempts: 8,
      }))
    })
    act(() => vi.advanceTimersByTime(2_100))
    expect(screen.getByRole('status')).toBeTruthy()

    act(() => vi.advanceTimersByTime(2_000))
    expect(screen.queryByRole('status')).toBeNull()
  })

  it('retries an exhausted connection in place and shows recovery', async () => {
    const activate = vi.spyOn(api, 'activateSession').mockResolvedValue({
      status: 'ready',
      session_id: 'task-active',
      kind: 'ssh',
      pwd: '/workspace',
      project: 'ssh://dev@example.com/workspace',
      workspace_key: 'ssh://dev@example.com/workspace',
      mode: 'approval',
      running: false,
      activated: true,
      focused: true,
    })
    store.dispatch(remoteConnectionActions.statusReceived({
      task_id: 'task-active',
      kind: 'ssh',
      status: 'failed',
      attempt: 8,
      max_attempts: 8,
      host: 'dev@example.com',
      error: 'connection reset',
      retryable: true,
    }))
    renderNotice()

    fireEvent.click(screen.getByRole('button', { name: 'Retry now' }))

    await waitFor(() => expect(activate).toHaveBeenCalledTimes(1))
    expect(activate.mock.calls[0][0]).toMatchObject({ session_id: 'task-active', focus: false })
    await waitFor(() => expect(screen.getByText('SSH connection restored')).toBeTruthy())
  })

  it('clears a manual retry notice when the user switches tasks before focus', async () => {
    let release: ((value: Awaited<ReturnType<typeof api.activateSession>>) => void) | undefined
    vi.spyOn(api, 'activateSession').mockImplementation(() => new Promise((resolve) => { release = resolve }))
    store.dispatch(remoteConnectionActions.statusReceived({
      task_id: 'task-active', kind: 'ssh', status: 'failed', attempt: 8, max_attempts: 8, retryable: true,
    }))
    renderNotice()

    fireEvent.click(screen.getByRole('button', { name: 'Retry now' }))
    await waitFor(() => expect(release).toBeTruthy())
    act(() => store.dispatch(sessionActions.setCurrentSession('task-other')))
    await act(async () => {
      release?.({
        status: 'ready', session_id: 'task-active', kind: 'ssh', pwd: '/workspace',
        project: 'ssh://dev@example.com/workspace', workspace_key: 'ssh://dev@example.com/workspace',
        mode: 'approval', running: false, activated: true, focused: false,
      })
    })

    await waitFor(() => expect(store.getState().remoteConnection.byTaskId['task-active']).toBeUndefined())
  })

  it('keeps backend detail collapsed and offers an executable review action', () => {
    store.dispatch(remoteConnectionActions.statusReceived({
      task_id: 'task-active',
      kind: 'ssh',
      status: 'action_required',
      attempt: 2,
      max_attempts: 8,
      code: 'ssh_auth_required',
      error: 'private diagnostic detail',
      retryable: false,
    }))
    renderNotice()

    const details = screen.getByText('Technical details').closest('details')
    expect(details?.hasAttribute('open')).toBe(false)
    expect(screen.getByRole('button', { name: 'Review connection' })).toBeTruthy()
  })

  it('routes credential attention into the existing inline conversation load flow', async () => {
    vi.spyOn(api, 'session').mockResolvedValue([])
    vi.spyOn(api, 'activateSession').mockRejectedValue(Object.assign(new Error('SSH authentication required'), {
      status: 409,
      code: 'ssh_auth_required',
      body: { error: 'SSH authentication required', code: 'ssh_auth_required', retryable: true },
    }))
    store.dispatch(remoteConnectionActions.statusReceived({
      task_id: 'task-active', kind: 'ssh', status: 'action_required', attempt: 2, max_attempts: 8,
      code: 'ssh_auth_required', retryable: false,
    }))
    renderNotice()

    fireEvent.click(screen.getByRole('button', { name: 'Review connection' }))

    await waitFor(() => expect(store.getState().conversationLoad.phase).toBe('awaiting_auth'))
    expect(store.getState().conversationLoad.target?.uuid).toBe('task-active')
    await store.dispatch(cancelConversationLoad())
  })

  it('asks the user to verify an unknown command outcome instead of reconnecting again', () => {
    const activate = vi.spyOn(api, 'activateSession')
    store.dispatch(remoteConnectionActions.statusReceived({
      task_id: 'task-active',
      kind: 'ssh',
      status: 'action_required',
      attempt: 2,
      max_attempts: 8,
      code: 'remote_outcome_unknown',
      error: 'raw remote detail',
      retryable: false,
    }))
    renderNotice()

    expect(screen.getByText('Connection restored · check the last command')).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'I understand' }))

    expect(activate).not.toHaveBeenCalled()
    expect(screen.queryByRole('status')).toBeNull()
  })
})
