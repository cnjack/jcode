import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { SessionActivationResponse, SessionEntry } from '../lib/types'
import { api } from '../lib/api'
import {
  cancelConversationLoad,
  continueConversationLoad,
  conversationLoadActions,
  conversationLoadTimeouts,
  chatActions,
  loadSession,
  openConversation,
  sessionActions,
  startNewChat,
  startScratchChat,
  store,
} from './store'
import { createWSHandlers } from './wsBridge'

beforeEach(async () => {
  await store.dispatch(cancelConversationLoad())
  store.dispatch(chatActions.clearChat())
  store.dispatch(chatActions.dropSessionQueue('session-agent-done-cancel'))
  store.dispatch(chatActions.dropSessionQueue('session-agent-done-commit'))
  store.dispatch(sessionActions.setCurrentSession(''))
  store.dispatch(sessionActions.setProjectPath(''))
  store.dispatch(sessionActions.setWorkspaceKind('project'))
})

afterEach(async () => {
  await store.dispatch(cancelConversationLoad())
  store.dispatch(conversationLoadActions.reset())
  vi.useRealTimers()
  vi.restoreAllMocks()
})

function activation(sessionID: string, project = '/workspace', focused = true): SessionActivationResponse {
  return {
    status: 'ready',
    session_id: sessionID,
    kind: project.startsWith('ssh://') ? 'ssh' : 'local',
    pwd: project.startsWith('ssh://') ? '/workspace' : project,
    project,
    workspace_key: project,
    provider: 'openai',
    model: 'test-model',
    agent: '',
    mode: 'approval',
    running: false,
    activated: true,
    focused,
  }
}

function history(content: string): SessionEntry[] {
  return [{ type: 'user', content, timestamp: '2026-08-12T00:00:00Z' }]
}

function mockFollowUpAPIs() {
  vi.spyOn(api, 'goal').mockResolvedValue(null)
  vi.spyOn(api, 'todos').mockResolvedValue([])
  vi.spyOn(api, 'askPending').mockResolvedValue([])
  vi.spyOn(api, 'approvalPending').mockResolvedValue([])
  vi.spyOn(api, 'sessions').mockResolvedValue([])
  vi.spyOn(api, 'tasks').mockResolvedValue([])
  vi.spyOn(api, 'projects').mockResolvedValue([])
}

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((done) => { resolve = done })
  return { promise, resolve }
}

describe('conversation loading state', () => {
  it('ignores history and errors from an older navigation request', () => {
    store.dispatch(conversationLoadActions.begin({
      requestId: 'old',
      target: { uuid: 'session-old', project: '/old' },
    }))
    store.dispatch(conversationLoadActions.begin({
      requestId: 'new',
      target: { uuid: 'session-new', project: '/new' },
    }))

    store.dispatch(conversationLoadActions.historyReady({ requestId: 'old', timeline: [] }))
    store.dispatch(conversationLoadActions.failed({ requestId: 'old', error: 'stale failure' }))

    expect(store.getState().conversationLoad).toMatchObject({
      requestId: 'new',
      phase: 'loading',
      historyStatus: 'loading',
      error: '',
    })
  })

  it('uses atomic activation and never resumes through the legacy new-session endpoint', async () => {
    mockFollowUpAPIs()
    vi.spyOn(api, 'session').mockResolvedValue(history('atomic history'))
    const activate = vi.spyOn(api, 'activateSession').mockImplementation((request) =>
      Promise.resolve(activation('session-atomic', '/workspace', !!request.focus)))
    const legacy = vi.spyOn(api, 'newSession')
    const connect = vi.spyOn(api, 'remoteConnect')

    await store.dispatch(openConversation({ uuid: 'session-atomic', project: '/workspace' }))

    expect(activate).toHaveBeenNthCalledWith(1, {
      session_id: 'session-atomic', project_path: '/workspace', focus: false,
    }, expect.any(AbortSignal))
    expect(activate).toHaveBeenNthCalledWith(2, {
      session_id: 'session-atomic', project_path: '/workspace', focus: true,
    }, expect.any(AbortSignal))
    expect(legacy).not.toHaveBeenCalled()
    expect(connect).not.toHaveBeenCalled()
    expect(store.getState().session.currentSessionId).toBe('session-atomic')
    expect(store.getState().chat.timeline[0]).toMatchObject({
      kind: 'message', data: { role: 'user', content: 'atomic history' },
    })
  })

  it('binds an authenticated SSH connection directly to the requested session', async () => {
    mockFollowUpAPIs()
    const project = 'ssh://root@example.com/workspace'
    vi.spyOn(api, 'session').mockResolvedValue(history('remote history'))
    const activate = vi.spyOn(api, 'activateSession')
      .mockRejectedValueOnce(Object.assign(new Error('authentication required'), {
        status: 409,
        code: 'ssh_auth_required',
        body: { code: 'ssh_auth_required', error: 'authentication required', retryable: true, kind: 'ssh' },
      }))
      .mockResolvedValueOnce(activation('session-ssh', project, true))
    vi.spyOn(api, 'remoteConnect').mockResolvedValue({
      connection_id: 'connection-1', remote_pwd: '/root', platform: 'linux',
    })
    const bind = vi.spyOn(api, 'remoteBind').mockResolvedValue({
      ...activation('session-ssh', project, false),
      status: 'ready',
      kind: 'ssh',
      label: project,
      name: 'workspace',
      host: 'example.com',
      user: 'root',
      port: 22,
      remote_path: '/workspace',
    })

    await store.dispatch(openConversation({ uuid: 'session-ssh', project }))
    expect(store.getState().conversationLoad.phase).toBe('awaiting_auth')

    await store.dispatch(continueConversationLoad({
      requestId: store.getState().conversationLoad.requestId,
      credentials: { authMethod: 'password', password: 'secret' },
    }))

    expect(bind).toHaveBeenCalledWith(
      'connection-1',
      '/workspace',
      { session_id: 'session-ssh', focus: false },
      expect.any(AbortSignal),
    )
    expect(activate).toHaveBeenLastCalledWith({
      session_id: 'session-ssh', project_path: project, focus: true,
    }, expect.any(AbortSignal))
    expect(store.getState().session.currentSessionId).toBe('session-ssh')
    expect(store.getState().session.projectPath).toBe(project)
  })

  it('pins the displayed host-key fingerprint into the trust retry', async () => {
    mockFollowUpAPIs()
    const project = 'ssh://root@example.com/workspace'
    vi.spyOn(api, 'session').mockResolvedValue(history('trusted history'))
    vi.spyOn(api, 'activateSession')
      .mockRejectedValueOnce(Object.assign(new Error('unknown host key'), {
        status: 409,
        code: 'ssh_host_key_unknown',
        body: {
          code: 'ssh_host_key_unknown',
          error: 'unknown host key',
          host: 'example.com',
          fingerprint: 'SHA256:shown-to-user',
          key_type: 'ssh-ed25519',
        },
      }))
      .mockResolvedValueOnce(activation('session-trusted', project, true))
    const connect = vi.spyOn(api, 'remoteConnect').mockResolvedValue({
      connection_id: 'connection-trusted', remote_pwd: '/root', platform: 'linux',
    })
    vi.spyOn(api, 'remoteBind').mockResolvedValue({
      ...activation('session-trusted', project, false),
      status: 'ready',
      kind: 'ssh',
      label: project,
      name: 'workspace',
      host: 'example.com',
      user: 'root',
      port: 22,
      remote_path: '/workspace',
    })

    await store.dispatch(openConversation({ uuid: 'session-trusted', project }))
    expect(store.getState().conversationLoad.hostKey?.fingerprint).toBe('SHA256:shown-to-user')
    await store.dispatch(continueConversationLoad({
      requestId: store.getState().conversationLoad.requestId,
      acceptHostKey: true,
    }))

    expect(connect).toHaveBeenCalledWith(expect.objectContaining({
      accept_host_key: true,
      host_key_fingerprint: 'SHA256:shown-to-user',
      auth_method: undefined,
      key_path: undefined,
    }), expect.any(AbortSignal))
  })

  it('retains explicitly submitted credentials across a host-key confirmation retry', async () => {
    mockFollowUpAPIs()
    const project = 'ssh://dev@example.com/workspace'
    vi.spyOn(api, 'session').mockResolvedValue(history('credentialed history'))
    vi.spyOn(api, 'activateSession')
      .mockRejectedValueOnce(Object.assign(new Error('authentication required'), {
        status: 409,
        code: 'ssh_auth_required',
        body: { code: 'ssh_auth_required', error: 'authentication required', retryable: true, kind: 'ssh' },
      }))
      .mockResolvedValueOnce(activation('session-credentialed', project, true))
    const connect = vi.spyOn(api, 'remoteConnect')
      .mockRejectedValueOnce(Object.assign(new Error('unknown host key'), {
        status: 409,
        code: 'ssh_host_key_unknown',
        body: {
          code: 'ssh_host_key_unknown',
          error: 'unknown host key',
          host: 'example.com',
          fingerprint: 'SHA256:credentialed-host',
          key_type: 'ssh-ed25519',
        },
      }))
      .mockResolvedValueOnce({
        connection_id: 'connection-credentialed', remote_pwd: '/root', platform: 'linux',
      })
    vi.spyOn(api, 'remoteBind').mockResolvedValue({
      ...activation('session-credentialed', project, false),
      status: 'ready',
      kind: 'ssh',
      label: project,
      name: 'workspace',
      host: 'example.com',
      user: 'dev',
      port: 22,
      remote_path: '/workspace',
    })

    await store.dispatch(openConversation({ uuid: 'session-credentialed', project }))
    const requestId = store.getState().conversationLoad.requestId
    await store.dispatch(continueConversationLoad({
      requestId,
      credentials: { authMethod: 'key', keyPath: '~/.ssh/id_ed25519', passphrase: 'secret phrase' },
    }))
    expect(store.getState().conversationLoad.phase).toBe('awaiting_host_key')

    await store.dispatch(continueConversationLoad({ requestId, acceptHostKey: true }))

    expect(connect).toHaveBeenNthCalledWith(2, expect.objectContaining({
      auth_method: 'key',
      key_path: '~/.ssh/id_ed25519',
      passphrase: 'secret phrase',
      accept_host_key: true,
      host_key_fingerprint: 'SHA256:credentialed-host',
    }), expect.any(AbortSignal))
    expect(store.getState().session.currentSessionId).toBe('session-credentialed')
  })

  it('never lets a late older request replace the newest conversation', async () => {
    mockFollowUpAPIs()
    const historyA = deferred<SessionEntry[]>()
    const activationA = deferred<SessionActivationResponse>()
    vi.spyOn(api, 'session').mockImplementation((id) => id === 'session-a' ? historyA.promise : Promise.resolve(history('B')))
    vi.spyOn(api, 'activateSession').mockImplementation((request) => request.session_id === 'session-a'
      ? activationA.promise
      : Promise.resolve(activation('session-b', '/b')))

    const older = store.dispatch(openConversation({ uuid: 'session-a', project: '/a' }))
    await store.dispatch(openConversation({ uuid: 'session-b', project: '/b' }))
    expect(store.getState().session.currentSessionId).toBe('session-b')

    activationA.resolve(activation('session-a', '/a'))
    historyA.resolve(history('A arrived late'))
    await older

    expect(store.getState().session.currentSessionId).toBe('session-b')
    expect(store.getState().chat.timeline[0]).toMatchObject({ data: { content: 'B' } })
  })

  it('turns bounded request deadlines into a retryable page instead of endless loading', async () => {
    vi.useFakeTimers()
    const aborting = <T>(signal?: AbortSignal) => new Promise<T>((_resolve, reject) => {
      signal?.addEventListener('abort', () => reject(signal.reason || new DOMException('Aborted', 'AbortError')), { once: true })
    })
    vi.spyOn(api, 'session').mockImplementation((_id, signal) => aborting<SessionEntry[]>(signal))
    vi.spyOn(api, 'activateSession').mockImplementation((_request, signal) => aborting<SessionActivationResponse>(signal))

    const opening = store.dispatch(openConversation({ uuid: 'session-timeout', project: '/slow' }))
    await vi.advanceTimersByTimeAsync(conversationLoadTimeouts.historyMs)
    expect(store.getState().conversationLoad.historyStatus).toBe('error')

    await vi.advanceTimersByTimeAsync(conversationLoadTimeouts.activationMs - conversationLoadTimeouts.historyMs)
    await opening
    expect(store.getState().conversationLoad).toMatchObject({
      phase: 'error',
      environmentStatus: 'error',
    })
  })

  it('does not focus the prepared environment when history cannot be loaded', async () => {
    vi.spyOn(api, 'session').mockRejectedValue(new Error('history unavailable'))
    const activate = vi.spyOn(api, 'activateSession').mockResolvedValue(
      activation('session-history-error', '/workspace', false),
    )

    await store.dispatch(openConversation({ uuid: 'session-history-error', project: '/workspace' }))

    expect(activate).toHaveBeenCalledTimes(1)
    expect(activate).toHaveBeenCalledWith({
      session_id: 'session-history-error', project_path: '/workspace', focus: false,
    }, expect.any(AbortSignal))
    expect(store.getState().conversationLoad).toMatchObject({
      phase: 'error',
      issue: 'history',
      historyStatus: 'error',
      environmentStatus: 'ready',
      retryable: true,
    })
  })

  it('preserves backend non-retryable activation errors for the loading page', async () => {
    vi.spyOn(api, 'session').mockResolvedValue(history('stale conversation'))
    vi.spyOn(api, 'activateSession').mockRejectedValue(Object.assign(new Error('conversation not found'), {
      status: 404,
      code: 'conversation_not_found',
      body: { code: 'conversation_not_found', error: 'conversation not found', retryable: false },
    }))

    await store.dispatch(openConversation({ uuid: 'missing-session', project: '/workspace' }))

    expect(store.getState().conversationLoad).toMatchObject({
      phase: 'error',
      errorCode: 'conversation_not_found',
      retryable: false,
      environmentStatus: 'error',
    })
  })

  it('cancels an in-flight conversation navigation before creating a new chat', async () => {
    const prepared = deferred<SessionActivationResponse>()
    vi.spyOn(api, 'session').mockResolvedValue(history('old target'))
    vi.spyOn(api, 'activateSession').mockReturnValue(prepared.promise)
    vi.spyOn(api, 'newSession').mockResolvedValue({ status: 'ok', session_id: 'brand-new' })

    const opening = store.dispatch(openConversation({ uuid: 'old-target', project: '/old' }))
    await vi.waitFor(() => expect(store.getState().conversationLoad.historyStatus).toBe('ready'))
    await store.dispatch(startNewChat())
    prepared.resolve(activation('old-target', '/old', false))
    await opening

    expect(store.getState().conversationLoad.phase).toBe('idle')
    expect(store.getState().session.currentSessionId).toBe('brand-new')
    expect(store.getState().chat.timeline).toEqual([])
  })

  it('allocates a fresh scratch workspace for no-project new tasks', async () => {
    const scratchPath = '/Users/test/.jcode/workspace/2026-08-19-001'
    const create = vi.spyOn(api, 'newSession').mockResolvedValue({
      status: 'ok',
      session_id: 'scratch-session',
      pwd: scratchPath,
      project: scratchPath,
      workspace_kind: 'scratch',
    })

    await store.dispatch(startScratchChat())

    expect(create).toHaveBeenCalledWith(undefined, undefined, 'scratch')
    expect(store.getState().session).toMatchObject({
      currentSessionId: 'scratch-session',
      projectPath: scratchPath,
      workspaceKind: 'scratch',
    })
  })

  it('guards a background history repair from overwriting a newer navigation', async () => {
    store.dispatch(sessionActions.setCurrentSession('session-old'))
    store.dispatch(chatActions.addMessage({ role: 'user', content: 'keep me' }))
    const freshHistory = deferred<SessionEntry[]>()
    vi.spyOn(api, 'session').mockReturnValue(freshHistory.promise)
    const legacyResume = vi.spyOn(api, 'newSession')

    const refresh = store.dispatch(loadSession({ uuid: 'session-old', background: true }))
    await vi.waitFor(() => expect(api.session).toHaveBeenCalledWith('session-old'))
    store.dispatch(conversationLoadActions.begin({
      requestId: 'new-navigation',
      target: { uuid: 'session-new', project: '/new' },
    }))
    freshHistory.resolve(history('late stale replay'))
    await refresh

    expect(legacyResume).not.toHaveBeenCalled()
    expect(store.getState().chat.timeline[0]).toMatchObject({ data: { content: 'keep me' } })
  })

  it('flushes target WS events received after the history barrier in order', async () => {
    mockFollowUpAPIs()
    const prepared = deferred<SessionActivationResponse>()
    vi.spyOn(api, 'session').mockResolvedValue(history('snapshot'))
    vi.spyOn(api, 'activateSession').mockImplementation((request) => request.focus
      ? Promise.resolve(activation('session-live', '/live', true))
      : prepared.promise)

    const opening = store.dispatch(openConversation({ uuid: 'session-live', project: '/live' }))
    await vi.waitFor(() => expect(store.getState().conversationLoad.historyStatus).toBe('ready'))
    const handlers = createWSHandlers(() => store.getState(), store.dispatch)
    expect(handlers.pendingTaskId?.()).toBe('session-live')
    handlers.onPendingTaskEvent?.({
      type: 'agent_text', taskId: 'session-live', data: { text: 'live delta' },
    })
    prepared.resolve(activation('session-live', '/live', false))
    await opening

    expect(store.getState().chat.timeline).toEqual(expect.arrayContaining([
      expect.objectContaining({ kind: 'message', data: expect.objectContaining({ content: 'snapshot' }) }),
      expect.objectContaining({ kind: 'message', data: expect.objectContaining({ content: 'live delta' }) }),
    ]))
  })

  it('drains a pending target queue once even when navigation is cancelled', async () => {
    mockFollowUpAPIs()
    const targetId = 'session-agent-done-cancel'
    const prepared = deferred<SessionActivationResponse>()
    vi.spyOn(api, 'session').mockResolvedValue(history('snapshot'))
    vi.spyOn(api, 'activateSession').mockReturnValue(prepared.promise)
    const chat = vi.spyOn(api, 'chat').mockResolvedValue({ status: 'ok', session_id: targetId })

    const opening = store.dispatch(openConversation({ uuid: targetId, project: '/pending' }))
    await vi.waitFor(() => expect(store.getState().conversationLoad.historyStatus).toBe('ready'))
    store.dispatch(chatActions.enqueueMessage({
      sessionId: targetId,
      message: { id: 'queued-cancel', text: 'send despite cancel' },
    }))
    const handlers = createWSHandlers(() => store.getState(), store.dispatch)
    handlers.onPendingTaskEvent?.({
      type: 'agent_done', taskId: targetId, data: { task_id: targetId },
    })

    await vi.waitFor(() => expect(chat).toHaveBeenCalledTimes(1))
    expect(store.getState().chat.queuedBySession[targetId]).toBeUndefined()
    await store.dispatch(cancelConversationLoad())
    prepared.resolve(activation(targetId, '/pending', false))
    await opening

    expect(chat).toHaveBeenCalledTimes(1)
  })

  it('does not drain a second queued turn when buffered agent_done commits', async () => {
    mockFollowUpAPIs()
    const targetId = 'session-agent-done-commit'
    const prepared = deferred<SessionActivationResponse>()
    vi.spyOn(api, 'session').mockResolvedValue(history('snapshot'))
    vi.spyOn(api, 'activateSession').mockImplementation((request) => request.focus
      ? Promise.resolve(activation(targetId, '/pending', true))
      : prepared.promise)
    const chat = vi.spyOn(api, 'chat').mockResolvedValue({ status: 'ok', session_id: targetId })

    const opening = store.dispatch(openConversation({ uuid: targetId, project: '/pending' }))
    await vi.waitFor(() => expect(store.getState().conversationLoad.historyStatus).toBe('ready'))
    store.dispatch(chatActions.enqueueMessage({
      sessionId: targetId,
      message: { id: 'queued-first', text: 'first queued turn' },
    }))
    store.dispatch(chatActions.enqueueMessage({
      sessionId: targetId,
      message: { id: 'queued-second', text: 'second queued turn' },
    }))
    const handlers = createWSHandlers(() => store.getState(), store.dispatch)
    handlers.onPendingTaskEvent?.({
      type: 'agent_done', taskId: targetId, data: { task_id: targetId },
    })

    await vi.waitFor(() => expect(chat).toHaveBeenCalledTimes(1))
    prepared.resolve(activation(targetId, '/pending', false))
    await opening

    expect(chat).toHaveBeenCalledTimes(1)
    expect(store.getState().chat.queuedBySession[targetId]).toEqual([
      { id: 'queued-second', text: 'second queued turn' },
    ])
  })
})
