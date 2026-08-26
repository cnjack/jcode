import { afterEach, describe, expect, it, vi } from 'vitest'

import { api } from './api'

afterEach(() => {
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})

describe('task-scoped API contracts', () => {
  it('routes active-task reads through task_id query parameters', async () => {
    const fetchMock = vi.fn(async (_input: RequestInfo | URL, _init?: RequestInit) => new Response('{}', {
      status: 200,
      headers: { 'Content-Type': 'application/json' },
    }))
    vi.stubGlobal('fetch', fetchMock)

    await api.todos('task/one')
    await api.goal('task/one')
    await api.models('task/one')
    await api.agents('task/one')
    await api.approvalMode('task/one')
    await api.askPending('task/one')
    await api.approvalPending('task/one')
    await api.gitBranches('task/one')
    await api.slashCommands('task/one')

    expect(fetchMock.mock.calls.map(([input]) => String(input))).toEqual(expect.arrayContaining([
      expect.stringContaining('/api/todos?task_id=task%2Fone'),
      expect.stringContaining('/api/goal?task_id=task%2Fone'),
      expect.stringContaining('/api/models?task_id=task%2Fone'),
      expect.stringContaining('/api/agents?task_id=task%2Fone'),
      expect.stringContaining('/api/approval/mode?task_id=task%2Fone'),
      expect.stringContaining('/api/ask/pending?task_id=task%2Fone'),
      expect.stringContaining('/api/approval/pending?task_id=task%2Fone'),
      expect.stringContaining('/api/git/branches?task_id=task%2Fone'),
      expect.stringContaining('/api/slash-commands?task_id=task%2Fone'),
    ]))
  })

  it('includes task identity in active-task mutations and fresh-session guards', async () => {
    const fetchMock = vi.fn(async (_input: RequestInfo | URL, _init?: RequestInit) => new Response('{}', {
      status: 200,
      headers: { 'Content-Type': 'application/json' },
    }))
    vi.stubGlobal('fetch', fetchMock)

    await api.setGoal('ship it', true, 'task-a')
    await api.clearGoal('task-a')
    await api.switchModel('provider', 'model', 'task-a')
    await api.switchMode('plan', 'task-a')
    await api.switchAgent('reviewer', 'task-a')
    await api.setApprovalMode(true, 'task-a')
    await api.gitCheckout('feature/task-a', false, '', 'task-a')
    await api.newSession(
      undefined,
      undefined,
      'project',
      '/work/project',
      { expectedSessionId: 'task-a', requireIdle: true },
    )

    const calls = fetchMock.mock.calls.map(([input, init]) => ({
      url: String(input),
      method: init?.method || 'GET',
      body: init?.body
        ? JSON.parse(String(init.body)) as Record<string, unknown>
        : undefined,
    }))
    expect(calls.find((call) => call.url.endsWith('/api/goal') && call.method === 'POST')?.body)
      .toMatchObject({ objective: 'ship it', task_id: 'task-a' })
    expect(calls.find((call) => call.url.includes('/api/goal?task_id=') && call.method === 'DELETE')?.url)
      .toContain('task_id=task-a')
    expect(calls.find((call) => call.url.endsWith('/api/model'))?.body).toMatchObject({ task_id: 'task-a' })
    expect(calls.find((call) => call.url.endsWith('/api/mode'))?.body).toMatchObject({ task_id: 'task-a' })
    expect(calls.find((call) => call.url.endsWith('/api/agent'))?.body).toMatchObject({ task_id: 'task-a' })
    expect(calls.find((call) => call.url.endsWith('/api/approval/mode') && call.method === 'POST')?.body)
      .toMatchObject({ task_id: 'task-a' })
    expect(calls.find((call) => call.url.endsWith('/api/git/checkout'))?.body)
      .toMatchObject({ branch: 'feature/task-a', task_id: 'task-a' })
    expect(calls.find((call) => call.url.endsWith('/api/sessions'))?.body).toMatchObject({
      pwd: '/work/project',
      expected_session_id: 'task-a',
      require_idle: true,
    })
  })

  it('uploads browser files as multipart data to the encoded task route', async () => {
    const fetchMock = vi.fn(async (_input: RequestInfo | URL, _init?: RequestInit) => new Response(JSON.stringify({
      path: '/tmp/.jcode-uploads-task/notes-a1b2c3.pdf',
      name: 'notes-a1b2c3.pdf',
      size: 3,
    }), {
      status: 201,
      headers: { 'Content-Type': 'application/json' },
    }))
    vi.stubGlobal('fetch', fetchMock)
    const file = new File(['pdf'], 'notes.pdf', { type: 'application/pdf' })

    const result = await api.uploadTaskFile('task/one', file)

    expect(result.path).toContain('notes-a1b2c3.pdf')
    const [input, init] = fetchMock.mock.calls[0] ?? []
    expect(String(input)).toContain('/api/tasks/task%2Fone/uploads')
    expect(init?.method).toBe('POST')
    expect(init?.body).toBeInstanceOf(FormData)
    const uploaded = (init?.body as FormData).get('file') as File
    expect(uploaded.name).toBe('notes.pdf')
    expect(uploaded.size).toBe(file.size)
    expect(new Headers(init?.headers).has('Content-Type')).toBe(false)
  })
})
