import { beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { Provider } from 'react-redux'
import { i18n } from '../i18n'
import { sessionActions, store } from '../app/store'
import { DesktopTitlebar } from './DesktopTitlebar'

const mocks = vi.hoisted(() => ({
  listWorkspaceApplications: vi.fn(),
  openWorkspaceInApplication: vi.fn<() => Promise<void>>(),
  updateTask: vi.fn(),
  workspace: vi.fn(),
  cloudSetSessionSync: vi.fn(),
}))

vi.mock('../lib/useDesktop', () => ({
  listWorkspaceApplications: mocks.listWorkspaceApplications,
  openWorkspaceInApplication: mocks.openWorkspaceInApplication,
}))

vi.mock('../lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../lib/api')>()
  return {
    ...actual,
    api: {
      ...actual.api,
      workspace: mocks.workspace,
      cloudStatus: vi.fn().mockResolvedValue({ logged_in: true }),
      cloudSync: vi.fn().mockResolvedValue({
        sync_default: false,
        sessions: { 'titlebar-test': true },
      }),
      cloudSetSessionSync: mocks.cloudSetSessionSync,
      diff: vi.fn().mockResolvedValue({ mode: 'working', entries: [] }),
      updateTask: mocks.updateTask,
    },
  }
})

function renderTitlebar() {
  return render(
    <Provider store={store}>
      <DesktopTitlebar
        isRunning={false}
        wsConnected
        activePanel="none"
        terminalOpen={false}
        onTogglePanel={() => {}}
      />
    </Provider>,
  )
}

beforeEach(async () => {
  cleanup()
  vi.clearAllMocks()
  mocks.listWorkspaceApplications.mockResolvedValue([
    { id: 'vscode', label: 'VS Code', group: 'editor', iconDataUrl: 'data:image/png;base64,icon' },
    { id: 'cursor', label: 'Cursor', group: 'editor' },
    { id: 'finder', label: 'Finder', group: 'system' },
  ])
  mocks.openWorkspaceInApplication.mockResolvedValue()
  mocks.updateTask.mockResolvedValue({})
  mocks.workspace.mockResolvedValue({ branch: 'codex/desktop-task-titlebar', dirty: true })
  mocks.cloudSetSessionSync.mockResolvedValue({ enabled: true })
  await i18n.changeLanguage('en')
  store.dispatch(sessionActions.setProjectPath('/Users/test/work/jcode'))
  store.dispatch(sessionActions.setCurrentSession('titlebar-test'))
  store.dispatch(sessionActions.upsertTask({
    uuid: 'titlebar-test',
    project: '/Users/test/work/jcode',
    created_at: '2026-07-30T00:00:00Z',
    updated_at: '2026-07-30T00:00:00Z',
    provider: 'openai',
    model: 'gpt-5',
    title: 'Design the Desktop titlebar',
    pinned: false,
    archived: false,
    unread: false,
  }))
})

describe('DesktopTitlebar', () => {
  it('shows task, branch, cloud, and actions without an ellipsis menu', async () => {
    renderTitlebar()
    expect(screen.getByTestId('desktop-titlebar').style.left).toBe(
      'calc(var(--sidebar-width, 20rem) + 10px)',
    )

    const details = screen.getByRole('button', { name: 'Task details' })
    fireEvent.click(details)

    const dialog = screen.getByRole('dialog', { name: 'Task details' })
    expect(dialog).toBeTruthy()
    await waitFor(() => expect(screen.getByText('codex/desktop-task-titlebar')).toBeTruthy())
    expect(mocks.workspace).toHaveBeenCalledWith('titlebar-test')
    expect(screen.getByRole('switch', { name: 'Cloud sync for this session' }).getAttribute('aria-checked')).toBe('true')
    expect(screen.getByRole('button', { name: 'Pin' })).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Rename' })).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Archive' })).toBeTruthy()
    expect(screen.queryByRole('button', { name: 'Task actions' })).toBeNull()
  })

  it('renames through the task API and updates the active title', async () => {
    renderTitlebar()
    fireEvent.click(screen.getByRole('button', { name: 'Task details' }))
    fireEvent.click(screen.getByRole('button', { name: 'Rename' }))

    const input = screen.getByRole('textbox', { name: 'Rename' })
    fireEvent.change(input, { target: { value: 'Desktop context controls' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))

    await waitFor(() => {
      expect(mocks.updateTask).toHaveBeenCalledWith('titlebar-test', { title: 'Desktop context controls' })
    })
    expect(store.getState().session.tasks.find((task) => task.uuid === 'titlebar-test')?.title).toBe('Desktop context controls')
  })

  it('keeps the rename editor open when the task update fails', async () => {
    mocks.updateTask.mockRejectedValueOnce(new Error('network down'))
    renderTitlebar()
    fireEvent.click(screen.getByRole('button', { name: 'Task details' }))
    fireEvent.click(screen.getByRole('button', { name: 'Rename' }))

    const input = screen.getByRole('textbox', { name: 'Rename' })
    fireEvent.change(input, { target: { value: 'Keep this draft' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))

    await waitFor(() => expect(screen.getByRole('alert').textContent).toBe('Could not update this task.'))
    expect(screen.getByRole('textbox', { name: 'Rename' }).getAttribute('value')).toBe('Keep this draft')
  })

  it('opens the local workspace in the selected application', async () => {
    renderTitlebar()
    fireEvent.click(screen.getByRole('button', { name: 'Open workspace' }))
    fireEvent.click(await screen.findByRole('menuitem', { name: 'VS Code' }))

    await waitFor(() => {
      expect(mocks.openWorkspaceInApplication).toHaveBeenCalledWith('/Users/test/work/jcode', 'vscode')
    })
  })

  it('uses discovered native icons and hides applications the system did not return', async () => {
    renderTitlebar()
    fireEvent.click(screen.getByRole('button', { name: 'Open workspace' }))

    const vscode = await screen.findByRole('menuitem', { name: 'VS Code' })
    expect(vscode.querySelector('img')?.getAttribute('src')).toBe('data:image/png;base64,icon')
    expect(screen.queryByRole('menuitem', { name: 'GoLand' })).toBeNull()
  })

  it('prevents duplicate cloud updates while a toggle is in flight', async () => {
    let finishToggle: (() => void) | undefined
    mocks.cloudSetSessionSync.mockImplementationOnce(() => new Promise<void>((resolve) => {
      finishToggle = resolve
    }))
    renderTitlebar()
    fireEvent.click(screen.getByRole('button', { name: 'Task details' }))
    const cloudSwitch = await screen.findByRole('switch', { name: 'Cloud sync for this session' })
    await waitFor(() => expect(cloudSwitch.getAttribute('aria-checked')).toBe('true'))

    fireEvent.click(cloudSwitch)
    fireEvent.click(cloudSwitch)
    expect(mocks.cloudSetSessionSync).toHaveBeenCalledTimes(1)
    expect(mocks.cloudSetSessionSync).toHaveBeenCalledWith('titlebar-test', false)
    finishToggle?.()
  })

  it('supports keyboard navigation and returns focus from the app menu', async () => {
    renderTitlebar()
    const trigger = screen.getByRole('button', { name: 'Open workspace' })
    fireEvent.click(trigger)
    const vscode = await screen.findByRole('menuitem', { name: 'VS Code' })
    await waitFor(() => expect(document.activeElement).toBe(vscode))

    fireEvent.keyDown(vscode, { key: 'ArrowDown' })
    expect(document.activeElement).toBe(screen.getByRole('menuitem', { name: 'Cursor' }))
    fireEvent.keyDown(document.activeElement!, { key: 'Escape' })
    expect(document.activeElement).toBe(trigger)
  })
})
