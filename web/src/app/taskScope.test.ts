import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { api } from '../lib/api'
import type { Goal, TodoItem } from '../lib/types'
import type { Approval } from 'jcode-ui-core'
import {
  chatActions,
  loadAgents,
  loadApprovalMode,
  loadGoal,
  loadModels,
  loadSlashCommands,
  loadStatus,
  loadTodos,
  reconcilePendingInteractions,
  resolveApproval,
  resolveApprovalOption,
  sendMessage,
  sessionActions,
  store,
  submitAskUser,
} from './store'
import { createWSHandlers } from './wsBridge'

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((done) => { resolve = done })
  return { promise, resolve }
}

beforeEach(() => {
  store.dispatch(chatActions.clearChat())
  store.dispatch(sessionActions.setCurrentSession('task-a'))
  store.dispatch(sessionActions.setProjectPath('/task-a'))
})

afterEach(() => {
  vi.restoreAllMocks()
})

describe('foreground task API scoping', () => {
  it('passes the selected task to status, goal, todos, models, agents, and approval-mode loads', async () => {
    const status = vi.spyOn(api, 'status').mockResolvedValue({
      running: false,
      ws_clients: 1,
      pwd: '/task-a',
      project: '/task-a',
      provider: 'provider-a',
      model: 'model-a',
      mode: 'approval',
    })
    const goal = vi.spyOn(api, 'goal').mockResolvedValue(null)
    const todos = vi.spyOn(api, 'todos').mockResolvedValue([])
    const models = vi.spyOn(api, 'models').mockResolvedValue({
      current: { provider: 'provider-a', model: 'model-a' },
      current_image: { provider: '', model: '' },
      providers: [],
    })
    const agents = vi.spyOn(api, 'agents').mockResolvedValue({ agents: [], current: '' })
    const approvalMode = vi.spyOn(api, 'approvalMode').mockResolvedValue({ auto_approve: false })

    await Promise.all([
      store.dispatch(loadStatus()),
      store.dispatch(loadGoal()),
      store.dispatch(loadTodos()),
      store.dispatch(loadModels()),
      store.dispatch(loadAgents()),
      store.dispatch(loadApprovalMode()),
    ])

    for (const request of [status, goal, todos, models, agents, approvalMode]) {
      expect(request).toHaveBeenCalledWith('task-a')
    }
  })

  it('does not apply a task response after the user navigates away', async () => {
    const pendingGoal = deferred<Goal | null>()
    vi.spyOn(api, 'goal').mockReturnValue(pendingGoal.promise)
    const loading = store.dispatch(loadGoal())
    await vi.waitFor(() => expect(api.goal).toHaveBeenCalledWith('task-a'))

    store.dispatch(sessionActions.setCurrentSession('task-b'))
    pendingGoal.resolve({
      objective: 'task-a objective',
      status: 'active',
      tokens_used: 0,
      created_at: 0,
      updated_at: 0,
    })
    await loading

    expect(store.getState().chat.goal).toBeNull()
  })

  it('keeps stale slash-goal and todo refresh responses out of the new task', async () => {
    const pendingGoal = deferred<Goal | null>()
    vi.spyOn(api, 'goal').mockReturnValue(pendingGoal.promise)
    const sending = store.dispatch(sendMessage({ text: '/goal status' }))
    await vi.waitFor(() => expect(api.goal).toHaveBeenCalledWith('task-a'))
    store.dispatch(sessionActions.setCurrentSession('task-b'))
    store.dispatch(chatActions.clearChat())
    pendingGoal.resolve({
      objective: 'task-a objective',
      status: 'active',
      tokens_used: 0,
      created_at: 0,
      updated_at: 0,
    })
    await sending
    expect(store.getState().chat.goal).toBeNull()
    expect(store.getState().chat.timeline).toEqual([])

    store.dispatch(sessionActions.setCurrentSession('task-a'))
    const pendingTodos = deferred<TodoItem[]>()
    vi.spyOn(api, 'todos').mockReturnValue(pendingTodos.promise)
    const handlers = createWSHandlers(() => store.getState(), store.dispatch)
    handlers.onTodoUpdate?.()
    await vi.waitFor(() => expect(api.todos).toHaveBeenCalledWith('task-a'))
    store.dispatch(sessionActions.setCurrentSession('task-b'))
    pendingTodos.resolve([{ id: 1, title: 'task-a todo', status: 'pending' }])
    await Promise.resolve()
    await Promise.resolve()
    expect(store.getState().chat.todos).toEqual([])
  })

  it('scopes pending interactions and drops both responses after navigation', async () => {
    const pendingAsk = deferred<Awaited<ReturnType<typeof api.askPending>>>()
    const pendingApproval = deferred<Awaited<ReturnType<typeof api.approvalPending>>>()
    const ask = vi.spyOn(api, 'askPending').mockReturnValue(pendingAsk.promise)
    const approval = vi.spyOn(api, 'approvalPending').mockReturnValue(pendingApproval.promise)

    const loading = store.dispatch(reconcilePendingInteractions())
    await vi.waitFor(() => {
      expect(ask).toHaveBeenCalledWith('task-a')
      expect(approval).toHaveBeenCalledWith('task-a')
    })
    store.dispatch(sessionActions.setCurrentSession('task-b'))
    store.dispatch(chatActions.clearChat())
    pendingAsk.resolve([{
      id: 'ask-a',
      questions: [{ question: 'Proceed?', header: 'Decision' }],
      task_id: 'task-a',
    }])
    pendingApproval.resolve([{
      id: 'approval-a',
      tool_name: 'write',
      tool_args: '{}',
      is_external: false,
      task_id: 'task-a',
    }])
    await loading

    expect(store.getState().session.currentSessionId).toBe('task-b')
    expect(store.getState().chat.timeline).toEqual([])
  })

  it('drops a task-scoped slash catalog after navigation', async () => {
    store.dispatch(chatActions.setSlashCommands([]))
    const pending = deferred<Awaited<ReturnType<typeof api.slashCommands>>>()
    const request = vi.spyOn(api, 'slashCommands').mockReturnValue(pending.promise)

    const loading = store.dispatch(loadSlashCommands())
    await vi.waitFor(() => expect(request).toHaveBeenCalledWith('task-a'))
    store.dispatch(sessionActions.setCurrentSession('task-b'))
    pending.resolve([{ slash: '/task-a-only', description: 'A', type: 'flow' }])
    await loading

    expect(store.getState().chat.slashCommands).toEqual([])
  })

  it('does not settle reused approval or ask ids in a newly selected task', async () => {
    const classic = deferred<Awaited<ReturnType<typeof api.approval>>>()
    const option = deferred<Awaited<ReturnType<typeof api.approvalOption>>>()
    const ask = deferred<Awaited<ReturnType<typeof api.askUser>>>()
    const approveRequest = vi.spyOn(api, 'approval').mockReturnValue(classic.promise)
    const optionRequest = vi.spyOn(api, 'approvalOption').mockReturnValue(option.promise)
    const askRequest = vi.spyOn(api, 'askUser').mockReturnValue(ask.promise)
    const options: Approval['options'] = [
      { id: 'deny-option', label: 'Deny', kind: 'deny' },
    ]
    const approvalFor = (id: string, taskId: string, withOptions = false) => ({
      id,
      tool_name: 'write',
      tool_args: '{}',
      is_external: false,
      options: withOptions ? options : undefined,
      task_id: taskId,
    } as Approval & { task_id: string })
    const question = [{ question: 'Proceed?', header: 'Decision' }]

    store.dispatch(chatActions.addApprovalRequest(approvalFor('approval-1', 'task-a')))
    store.dispatch(chatActions.addApprovalRequest(approvalFor('approval-2', 'task-a', true)))
    store.dispatch(chatActions.attachAskUser({
      toolName: 'ask_user', askUserId: 'ask-1', questions: question, taskId: 'task-a',
    }))
    const resolvingClassic = store.dispatch(resolveApproval({ id: 'approval-1', approved: true }))
    const resolvingOption = store.dispatch(resolveApprovalOption({ id: 'approval-2', optionId: 'deny-option' }))
    const resolvingAsk = store.dispatch(submitAskUser({
      id: 'ask-1', answers: [{ question_header: 'Decision', answer: 'Yes' }],
    }))
    await vi.waitFor(() => {
      expect(approveRequest).toHaveBeenCalledWith('approval-1', true, false, 'task-a')
      expect(optionRequest).toHaveBeenCalledWith('approval-2', 'deny-option', 'task-a')
      expect(askRequest).toHaveBeenCalledWith('ask-1', expect.any(Array), 'task-a')
    })

    store.dispatch(sessionActions.setCurrentSession('task-b'))
    store.dispatch(chatActions.clearChat())
    store.dispatch(chatActions.addApprovalRequest(approvalFor('approval-1', 'task-b')))
    store.dispatch(chatActions.addApprovalRequest(approvalFor('approval-2', 'task-b', true)))
    store.dispatch(chatActions.attachAskUser({
      toolName: 'ask_user', askUserId: 'ask-1', questions: question, taskId: 'task-b',
    }))
    classic.resolve({ status: 'ok' })
    option.resolve({ status: 'ok', resolved_option_id: 'deny-option' })
    ask.resolve({ status: 'ok' })
    await Promise.all([resolvingClassic, resolvingOption, resolvingAsk])

    const approvals = store.getState().chat.timeline.filter((item) => item.kind === 'approval')
    expect(approvals).toHaveLength(2)
    for (const item of approvals) {
      if (item.kind === 'approval') {
        expect(item.data.resolved).not.toBe(true)
        expect(item.data.resolving).not.toBe(true)
      }
    }
    const askTool = store.getState().chat.timeline.find((item) => item.kind === 'tool')
    expect(askTool?.kind).toBe('tool')
    if (askTool?.kind === 'tool') {
      expect(askTool.data.status).toBe('running')
      expect(askTool.data.output).toBeUndefined()
    }
  })
})
