import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { Provider } from 'react-redux'
import { chatActions, store } from '../app/store'
import { i18n } from '../i18n'
import { StatusPanel } from './StatusPanel'

beforeEach(async () => {
  cleanup()
  await i18n.changeLanguage('en')
  store.dispatch(chatActions.clearChat())
  store.dispatch(chatActions.setTodos([
    { id: 1, title: 'Inspect layout', status: 'completed' },
    { id: 2, title: 'Verify floating panel', status: 'in_progress' },
  ]))
  store.dispatch(chatActions.setPlanHistory([
    {
      id: 'plan-1',
      title: 'Initial layout plan',
      status: 'completed',
      todos: [{ id: 1, title: 'Inspect layout', status: 'completed' }],
      timestamp: Date.parse('2026-08-04T08:00:00Z'),
    },
    {
      id: 'plan-2',
      title: 'Verify floating panel',
      status: 'in_progress',
      todos: [
        { id: 1, title: 'Inspect layout', status: 'completed' },
        { id: 2, title: 'Verify floating panel', status: 'in_progress' },
      ],
      timestamp: Date.parse('2026-08-04T09:00:00Z'),
    },
  ]))
})

afterEach(() => {
  cleanup()
  store.dispatch(chatActions.clearChat())
})

describe('StatusPanel', () => {
  it('shows plan history in the floating task-status region', () => {
    const onClose = vi.fn()
    render(
      <Provider store={store}>
        <StatusPanel open isRunning onOpen={() => {}} onClose={onClose} />
      </Provider>,
    )

    expect(screen.getByRole('dialog', { name: 'Task status' })).toBeTruthy()
    expect(screen.getAllByText('Verify floating panel').length).toBeGreaterThan(0)
    expect(screen.getByText('Initial layout plan')).toBeTruthy()
    expect(screen.getAllByText('Todo List').length).toBeGreaterThan(0)
    expect(screen.getAllByText('Plan history').length).toBeGreaterThan(0)
    expect(screen.getAllByText('Artifacts').length).toBeGreaterThan(0)
    expect(screen.queryByRole('button', { name: 'Open task status' })).toBeNull()

    const collapseHeader = screen.getByRole('button', { name: 'Collapse task status' })
    expect(collapseHeader.textContent).toContain('Verify floating panel')
    fireEvent.click(collapseHeader)
    expect(onClose).toHaveBeenCalledOnce()
  })

  it('uses the live status capsule as the only opener while collapsed', () => {
    const onOpen = vi.fn()
    render(
      <Provider store={store}>
        <StatusPanel open={false} isRunning onOpen={onOpen} onClose={() => {}} />
      </Provider>,
    )

    fireEvent.click(screen.getByRole('button', { name: 'Open task status' }))
    expect(onOpen).toHaveBeenCalledOnce()
    expect(screen.queryByRole('dialog', { name: 'Task status' })).toBeNull()
  })

  it('shows the latest completed todo in the collapsed status capsule', () => {
    store.dispatch(chatActions.setTodos([
      { id: 1, title: 'Run tests', status: 'completed' },
      { id: 2, title: 'commit + push master', status: 'completed' },
    ]))

    render(
      <Provider store={store}>
        <StatusPanel open={false} isRunning={false} onOpen={() => {}} onClose={() => {}} />
      </Provider>,
    )

    expect(screen.getByText('commit + push master')).toBeTruthy()
    expect(screen.queryByText('Run tests')).toBeNull()
  })

  it('summarizes plan snapshots without repeating their todo rows', () => {
    store.dispatch(chatActions.setTodos([{ id: 1, title: 'Current todo', status: 'in_progress' }]))
    store.dispatch(chatActions.setPlanHistory([{
      id: 'history-only',
      title: 'Historical plan',
      status: 'completed',
      todos: [{ id: 9, title: 'Duplicated historical todo', status: 'completed' }],
      timestamp: Date.parse('2026-08-04T09:00:00Z'),
    }]))

    render(
      <Provider store={store}>
        <StatusPanel open isRunning={false} onOpen={() => {}} onClose={() => {}} />
      </Provider>,
    )

    expect(screen.getByText('Historical plan')).toBeTruthy()
    expect(screen.getByText(/1 Todo/)).toBeTruthy()
    expect(screen.queryByText('Duplicated historical todo')).toBeNull()
  })

  it('keeps the status opener visible for sessions without status data', () => {
    store.dispatch(chatActions.clearChat())
    const onOpen = vi.fn()

    const view = render(
      <Provider store={store}>
        <StatusPanel open={false} isRunning={false} onOpen={onOpen} onClose={() => {}} />
      </Provider>,
    )

    const opener = screen.getByRole('button', { name: 'Open task status' })
    expect(opener.parentElement?.className).toContain('w-[min(248px,calc(100%_-_64px))]')
    expect(screen.getByText('Task status')).toBeTruthy()
    expect(screen.queryByText('Todo List')).toBeNull()
    expect(screen.queryByText('Plan history')).toBeNull()
    expect(screen.queryByText('Artifacts')).toBeNull()

    fireEvent.click(opener)
    expect(onOpen).toHaveBeenCalledOnce()

    view.rerender(
      <Provider store={store}>
        <StatusPanel open isRunning={false} onOpen={onOpen} onClose={() => {}} />
      </Provider>,
    )
    const panel = screen.getByRole('dialog', { name: 'Task status' })
    expect(panel.className).toContain('w-[min(248px,calc(100%_-_64px))]')
    expect(screen.getByRole('button', { name: 'Collapse task status' })).toBe(opener)
    expect(screen.getByText('No Todo, Plan, or artifacts yet')).toBeTruthy()
    expect(screen.queryByText('Todo List')).toBeNull()
  })
})
