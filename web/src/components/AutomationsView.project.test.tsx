import { beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { i18n } from '../i18n'
import { api } from '../lib/api'
import type { AutomationItem } from '../lib/automation'
import type { TaskItem } from '../lib/types'
import { AutomationsView } from './AutomationsView'

vi.mock('../lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../lib/api')>()
  return {
    ...actual,
    api: {
      ...actual.api,
      automations: vi.fn(),
      automationRuns: vi.fn(),
      automationTemplates: vi.fn(),
      automationUpdate: vi.fn(),
      projects: vi.fn(),
      tasks: vi.fn(),
    },
  }
})

function automation(overrides: Partial<AutomationItem> = {}): AutomationItem {
  return {
    id: 'auto-1',
    name: 'CPU check',
    prompt: 'Check CPU usage',
    trigger: { type: 'schedule', cadence: 'cron', expr: '*/30 * * * *' },
    project_path: '/work/jcode',
    context_policy: 'isolated',
    mode: 'full_access',
    run_in_cloud: false,
    enabled: true,
    source: 'manual',
    created_at: '2026-09-04T09:00:00Z',
    updated_at: '2026-09-04T09:00:00Z',
    human_schedule: 'Every 30 minutes',
    badge: 'Cron',
    state: {},
    workspace_kind: 'project',
    ...overrides,
  }
}

function conversation(overrides: Partial<TaskItem> = {}): TaskItem {
  return {
    uuid: 'owner-session',
    project: '/work/jcode',
    workspace_kind: 'project',
    created_at: '2026-09-04T08:00:00Z',
    updated_at: '2026-09-04T09:00:00Z',
    provider: 'openai',
    model: 'gpt-5',
    title: 'JCode automation work',
    pinned: false,
    archived: false,
    unread: false,
    ...overrides,
  }
}

beforeEach(async () => {
  cleanup()
  await i18n.changeLanguage('en')
  vi.mocked(api.automations).mockReset()
  vi.mocked(api.automationRuns).mockReset()
  vi.mocked(api.automationTemplates).mockReset()
  vi.mocked(api.automationUpdate).mockReset()
  vi.mocked(api.projects).mockReset()
  vi.mocked(api.tasks).mockReset()
  vi.mocked(api.automations).mockResolvedValue([])
  vi.mocked(api.automationRuns).mockResolvedValue([])
  vi.mocked(api.automationTemplates).mockResolvedValue([])
  vi.mocked(api.automationUpdate).mockResolvedValue(automation())
  vi.mocked(api.projects).mockResolvedValue([
    { path: '/work/jcode', workspace_kind: 'project' },
    { path: '/tmp/.jcode/workspace/2026-09-04-006', workspace_kind: 'scratch' },
  ])
  vi.mocked(api.tasks).mockResolvedValue([])
})

describe('Automation project field', () => {
  it('shows a locked no-project value without exposing its scratch path', async () => {
    const scratchPath = '/tmp/.jcode/workspace/2026-09-04-006'
    vi.mocked(api.automations).mockResolvedValue([
      automation({
        project_path: scratchPath,
        context_policy: 'conversation',
        owner_session_id: 'owner-session',
        workspace_kind: 'scratch',
      }),
    ])
    render(<AutomationsView />)

    await screen.findByText('CPU check')
    fireEvent.click(screen.getByTitle('Edit'))
    const dialog = screen.getByRole('dialog', { name: 'Edit automation' })

    expect(within(dialog).getByRole('textbox', { name: 'Project' }).textContent).toBe('Chat')
    expect(within(dialog).queryByRole('combobox', { name: 'Project' })).toBeNull()
    expect(dialog.textContent).not.toContain(scratchPath)
    expect(dialog.textContent).not.toContain('cannot move')
    expect(dialog.textContent).not.toContain('Autopilot')
  })

  it('locks a project that follows a conversation', async () => {
    vi.mocked(api.automations).mockResolvedValue([
      automation({ context_policy: 'conversation', owner_session_id: 'owner-session' }),
    ])
    render(<AutomationsView />)

    await screen.findByText('CPU check')
    fireEvent.click(screen.getByTitle('Edit'))
    const dialog = screen.getByRole('dialog', { name: 'Edit automation' })

    expect(within(dialog).getByRole('textbox', { name: 'Project' }).textContent).toBe('jcode')
    expect(within(dialog).queryByRole('combobox', { name: 'Project' })).toBeNull()
  })

  it('shows, opens, and switches the linked conversation while deriving its project', async () => {
    const scratchPath = '/tmp/.jcode/workspace/2026-09-04-006'
    const first = conversation()
    const second = conversation({
      uuid: 'scratch-owner',
      project: scratchPath,
      workspace_kind: 'scratch',
      title: 'System resource checks',
      updated_at: '2026-09-04T10:00:00Z',
    })
    vi.mocked(api.automations).mockResolvedValue([
      automation({ context_policy: 'conversation', owner_session_id: first.uuid }),
    ])
    vi.mocked(api.tasks).mockResolvedValue([first, second])
    const onOpenConversation = vi.fn()
    render(<AutomationsView onOpenConversation={onOpenConversation} />)

    await screen.findByText('CPU check')
    expect(screen.getByRole('button', { name: 'JCode automation work' })).toBeTruthy()
    fireEvent.click(screen.getByTitle('Edit'))
    const dialog = screen.getByRole('dialog', { name: 'Edit automation' })
    const ownerSelect = within(dialog).getByRole('button', { name: 'Runs in' })
    expect(ownerSelect.textContent).toContain('JCode automation work')

    fireEvent.click(within(dialog).getByRole('button', { name: 'Open conversation' }))
    expect(onOpenConversation).toHaveBeenCalledWith(first)

    fireEvent.click(ownerSelect)
    const ownerList = within(dialog).getByRole('listbox', { name: 'Runs in' })
    const scratchOwner = within(ownerList).getByRole('option', { name: /System resource checks/ })
    expect(scratchOwner.textContent).toContain('Chat')
    fireEvent.click(scratchOwner)
    expect(ownerSelect.textContent).toContain('System resource checks')
    expect(within(dialog).getByRole('textbox', { name: 'Project' }).textContent).toBe('Chat')

    fireEvent.click(within(dialog).getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(api.automationUpdate).toHaveBeenCalled())
    const patch = vi.mocked(api.automationUpdate).mock.calls[0][1]
    expect(patch.owner_session_id).toBe('scratch-owner')
    expect(patch).not.toHaveProperty('project_path')
  })

  it('uses a project dropdown for an isolated automation and omits scratch workspaces', async () => {
    render(<AutomationsView />)
    await waitFor(() => expect(api.projects).toHaveBeenCalled())
    fireEvent.click(screen.getAllByRole('button', { name: 'New automation' })[0])
    const projectSelect = screen.getByRole('button', { name: 'Project' })

    expect(projectSelect.textContent).toContain('Select a project')
    expect(screen.queryByRole('combobox', { name: 'Project' })).toBeNull()
    fireEvent.click(projectSelect)
    const projectList = screen.getByRole('listbox', { name: 'Project' })
    expect(within(projectList).getByRole('option', { name: 'jcode' })).toBeTruthy()
    expect(within(projectList).queryByRole('option', { name: '2026-09-04-006' })).toBeNull()
    fireEvent.click(within(projectList).getByRole('option', { name: 'jcode' }))
    expect(projectSelect.textContent).toContain('jcode')
  })

  it('uses a themed listbox instead of a native select for the trigger', async () => {
    render(<AutomationsView />)
    await waitFor(() => expect(api.projects).toHaveBeenCalled())
    fireEvent.click(screen.getAllByRole('button', { name: 'New automation' })[0])
    const dialog = screen.getByRole('dialog', { name: 'New automation' })
    const trigger = within(dialog).getByRole('button', { name: 'Trigger' })

    expect(within(dialog).queryByRole('combobox', { name: 'Trigger' })).toBeNull()
    expect(dialog.querySelector('select')).toBeNull()
    fireEvent.click(trigger)
    const triggerList = within(dialog).getByRole('listbox', { name: 'Trigger' })
    expect(within(triggerList).getAllByRole('option')).toHaveLength(6)
    fireEvent.click(within(triggerList).getByRole('option', { name: 'Cron' }))
    expect(trigger.textContent).toContain('Cron')
  })

  it('still renders automations when loading project options fails', async () => {
    vi.mocked(api.automations).mockResolvedValue([automation()])
    vi.mocked(api.projects).mockRejectedValue(new Error('projects unavailable'))

    render(<AutomationsView />)

    expect(await screen.findByText('CPU check')).toBeTruthy()
  })
})
