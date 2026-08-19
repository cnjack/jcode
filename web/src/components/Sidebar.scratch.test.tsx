import { beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, render, screen, waitFor } from '@testing-library/react'
import { Provider } from 'react-redux'
import { i18n } from '../i18n'
import { sessionActions, store } from '../app/store'
import { Sidebar } from './Sidebar'

vi.mock('../lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../lib/api')>()
  return {
    ...actual,
    api: {
      ...actual.api,
      cloudStatus: vi.fn().mockRejectedValue(new Error('not configured')),
      cloudPairings: vi.fn().mockResolvedValue({ pairings: [] }),
    },
  }
})

beforeEach(async () => {
  cleanup()
  await i18n.changeLanguage('en')
  store.dispatch(sessionActions.setCurrentSession('scratch-new'))
  store.dispatch(sessionActions.setProjectPath('/tmp/.jcode/workspace/2026-08-19-002'))
  store.dispatch(sessionActions.setWorkspaceKind('scratch'))
  store.dispatch(sessionActions.setTasks([
    {
      uuid: 'scratch-old',
      project: '/tmp/.jcode/workspace/2026-08-19-001',
      workspace_kind: 'scratch',
      created_at: '2026-08-19T09:00:00Z',
      updated_at: '2026-08-19T09:00:00Z',
      provider: 'openai', model: 'gpt-5', title: 'First scratch task',
      pinned: false, archived: false, unread: false,
    },
    {
      uuid: 'scratch-new',
      project: '/tmp/.jcode/workspace/2026-08-19-002',
      workspace_kind: 'scratch',
      created_at: '2026-08-19T11:00:00Z',
      updated_at: '2026-08-19T11:00:00Z',
      provider: 'openai', model: 'gpt-5', title: 'Second scratch task',
      pinned: false, archived: false, unread: false,
    },
    {
      uuid: 'project-task',
      project: '/work/jcode',
      workspace_kind: 'project',
      created_at: '2026-08-19T10:00:00Z',
      updated_at: '2026-08-19T10:00:00Z',
      provider: 'openai', model: 'gpt-5', title: 'Project task',
      pinned: false, archived: false, unread: false,
    },
  ]))
  store.dispatch(sessionActions.setProjectTimes([
    { path: '/tmp/.jcode/workspace/2026-08-19-001', updated_at: '2026-08-19T09:00:00Z', workspace_kind: 'scratch' },
    { path: '/tmp/.jcode/workspace/2026-08-19-002', updated_at: '2026-08-19T11:00:00Z', workspace_kind: 'scratch' },
    { path: '/work/jcode', updated_at: '2026-08-19T10:00:00Z', workspace_kind: 'project' },
  ]))
})

describe('Sidebar no-project grouping', () => {
  it('merges scratch paths into one recent no-project group', async () => {
    const { container } = render(
      <Provider store={store}>
        <Sidebar />
      </Provider>,
    )

    await waitFor(() => expect(screen.getByText('Second scratch task')).toBeTruthy())
    expect(screen.getAllByText('No project')).toHaveLength(1)
    expect(screen.queryByText('2026-08-19-001')).toBeNull()
    expect(screen.queryByText('2026-08-19-002')).toBeNull()
    const groupNames = [...container.querySelectorAll('.sb-project-name')].map((node) => node.textContent)
    expect(groupNames).toEqual(['No project', 'jcode'])
  })
})
