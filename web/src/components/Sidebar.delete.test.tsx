import { beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { Provider } from 'react-redux'
import { i18n } from '../i18n'
import { sessionActions, store } from '../app/store'
import { api } from '../lib/api'
import { Sidebar } from './Sidebar'

vi.mock('../lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../lib/api')>()
  return {
    ...actual,
    api: {
      ...actual.api,
      sessionDeleteImpact: vi.fn(),
      deleteSession: vi.fn(),
      cloudStatus: vi.fn().mockRejectedValue(new Error('not configured')),
      cloudPairings: vi.fn().mockResolvedValue({ pairings: [] }),
    },
  }
})

beforeEach(async () => {
  cleanup()
  await i18n.changeLanguage('en')
  vi.mocked(api.sessionDeleteImpact).mockReset()
  vi.mocked(api.deleteSession).mockReset()
  vi.mocked(api.deleteSession).mockResolvedValue({ status: 'ok' })
  store.dispatch(sessionActions.setCurrentSession('another-session'))
  store.dispatch(sessionActions.setProjectPath('/work/project'))
  store.dispatch(sessionActions.setWorkspaceKind('project'))
  store.dispatch(sessionActions.setTasks([{
    uuid: 'owner-session',
    project: '/work/project',
    workspace_kind: 'project',
    created_at: '2026-09-04T09:00:00Z',
    updated_at: '2026-09-04T09:00:00Z',
    provider: 'openai', model: 'gpt-5', title: 'Automation owner',
    pinned: false, archived: false, unread: false,
  }]))
})

async function openDeleteDialog() {
  render(
    <Provider store={store}>
      <Sidebar />
    </Provider>,
  )
  fireEvent.contextMenu(screen.getByText('Automation owner'))
  fireEvent.click(await screen.findByRole('menuitem', { name: 'Delete' }))
  await screen.findByRole('dialog', { name: 'Delete “Automation owner”?' })
}

describe('Sidebar conversation deletion with automations', () => {
  it('keeps related automations by detaching them', async () => {
    vi.mocked(api.sessionDeleteImpact).mockResolvedValue({
      automations: [{ id: 'auto-1', name: 'CPU check', human_schedule: 'Every 30 minutes', enabled: true }],
    })
    await openDeleteDialog()

    expect(screen.getByText('CPU check')).toBeTruthy()
    expect(screen.getByText('Every 30 minutes')).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: /Keep automations/ }))
    await waitFor(() => expect(api.deleteSession).toHaveBeenCalledWith('owner-session', 'detach'))
  })

  it('can cascade-delete the related automations', async () => {
    vi.mocked(api.sessionDeleteImpact).mockResolvedValue({
      automations: [{ id: 'auto-1', name: 'CPU check', human_schedule: 'Once', enabled: true }],
    })
    await openDeleteDialog()

    fireEvent.click(screen.getByRole('button', { name: /Delete conversation \+ 1 automation/ }))
    await waitFor(() => expect(api.deleteSession).toHaveBeenCalledWith('owner-session', 'delete'))
  })
})
