import { act, cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { RuntimeProvider, createMockRuntime } from 'jcode-ui-core/runtime'
import { createToolRendererRegistry } from 'jcode-ui-core/adapters'
import type { Approval, ThreadItem, ToolCall } from 'jcode-ui-core'
import { Thread } from './Thread.js'
import { ToolCallCard } from './ToolCallCard.js'
import { ToolRegistryProvider } from './ToolRegistryContext.js'

afterEach(cleanup)

Object.defineProperty(HTMLElement.prototype, 'scrollTo', {
  configurable: true,
  value: vi.fn(),
})

function executeTool(overrides: Partial<ToolCall> = {}): ToolCall {
  return {
    id: 'tool-1',
    toolCallID: 'call-1',
    name: 'execute',
    args: JSON.stringify({ command: 'pnpm test' }),
    status: 'done',
    timestamp: 2_000,
    displayInfo: { title: 'Shell', subtitle: 'pnpm test', kind: 'shell' },
    ...overrides,
  }
}

function approval(overrides: Partial<Approval> = {}): Approval {
  return {
    id: 'approval-1',
    tool_name: 'execute',
    tool_args: JSON.stringify({ command: 'pnpm test' }),
    tool_call_id: 'call-1',
    is_external: false,
    ...overrides,
  }
}

describe('Thread completed turns', () => {
  it('collapses intermediate work after completion and keeps the final reply visible', () => {
    const items: ThreadItem[] = [
      { kind: 'message', seq: 1, data: { id: 'user', role: 'user', content: 'Please run the tests', timestamp: 1_000 } },
      { kind: 'message', seq: 2, data: { id: 'progress', role: 'assistant', content: 'I am checking the suite.', timestamp: 2_000 } },
      { kind: 'tool', seq: 3, data: executeTool() },
      { kind: 'message', seq: 4, data: { id: 'final', role: 'assistant', content: 'All tests passed.', timestamp: 13_000, durationMs: 12_000 } },
    ]
    render(
      <RuntimeProvider runtime={createMockRuntime({ items, isRunning: false })}>
        <Thread
          virtualize={false}
          renderPending={() => null}
          turnDurationLabel={(ms) => `本次耗时 ${Math.round(ms / 1000)}秒`}
          turnExpandLabel="展开本轮工作过程"
          turnCollapseLabel="折叠本轮工作过程"
        />
      </RuntimeProvider>,
    )

    expect(screen.getByText('Please run the tests')).toBeTruthy()
    expect(screen.getByText('All tests passed.')).toBeTruthy()
    expect(screen.getByText('本次耗时 12秒')).toBeTruthy()
    expect(screen.queryByText('I am checking the suite.')).toBeNull()
    expect(screen.queryByText('Shell')).toBeNull()

    const disclosure = screen.getByRole('button', { name: /本次耗时 12秒 · 展开本轮工作过程/ })
    expect(disclosure.getAttribute('aria-expanded')).toBe('false')
    fireEvent.click(disclosure)

    expect(disclosure.getAttribute('aria-expanded')).toBe('true')
    expect(screen.getByText('I am checking the suite.')).toBeTruthy()
    expect(screen.getByText('Shell')).toBeTruthy()
    expect(screen.getAllByText('All tests passed.')).toHaveLength(1)
  })

  it('automatically folds the final turn when the runtime changes from running to idle', () => {
    const items: ThreadItem[] = [
      { kind: 'message', seq: 1, data: { id: 'user', role: 'user', content: 'Do it', timestamp: 1_000 } },
      { kind: 'tool', seq: 2, data: executeTool() },
      { kind: 'message', seq: 3, data: { id: 'final', role: 'assistant', content: 'Done.', timestamp: 4_000 } },
    ]
    const runtime = createMockRuntime({ items, isRunning: true })
    render(
      <RuntimeProvider runtime={runtime}>
        <Thread virtualize={false} renderPending={() => null} />
      </RuntimeProvider>,
    )

    expect(screen.getByText('Shell')).toBeTruthy()
    expect(screen.queryByTestId('completed-turn')).toBeNull()

    act(() => runtime.setRunning(false))

    expect(screen.getByTestId('completed-turn')).toBeTruthy()
    expect(screen.queryByText('Shell')).toBeNull()
    expect(screen.getByText('Done.')).toBeTruthy()
  })
})

describe('Thread approval/tool integration', () => {
  it('renders a pending decision inside the exact tool card, not as a second timeline card', () => {
    const gate = approval()
    const items: ThreadItem[] = [
      { kind: 'message', seq: 1, data: { id: 'user', role: 'user', content: 'Run tests', timestamp: 1_000 } },
      { kind: 'tool', seq: 2, data: executeTool({ status: 'running' }) },
      { kind: 'approval', seq: 3, data: gate },
    ]
    const runtime = createMockRuntime({ items, isRunning: true })
    const { container } = render(
      <RuntimeProvider runtime={runtime}>
        <Thread virtualize={false} renderPending={() => null} />
      </RuntimeProvider>,
    )

    expect(screen.getByTestId('tool-approval')).toBeTruthy()
    expect(screen.getAllByText('Approval needed')).toHaveLength(1)
    expect(screen.getByText('Run command')).toBeTruthy()
    expect(container.querySelector('.jcode-approval-slot')).toBeNull()
    expect(container.querySelector('[data-tool-name="execute"]')).toBeNull()
    expect(screen.queryByText('Shell')).toBeNull()

    fireEvent.click(screen.getByRole('button', { name: 'Allow once' }))

    expect(runtime.calls).toContainEqual({
      action: 'resolveApproval',
      args: ['approval-1', true, false],
    })
    expect(screen.queryByTestId('tool-approval')).toBeNull()
    expect(screen.getByText('Allowed')).toBeTruthy()
    expect(container.querySelector('[data-tool-name="execute"]')).toBeTruthy()

    act(() => {
      runtime.setItems([
        items[0],
        { kind: 'tool', seq: 2, data: executeTool({ status: 'done' }) },
        { kind: 'approval', seq: 3, data: { ...gate, resolved: true, approved: true } },
      ])
    })
    expect(screen.getByText('Allowed')).toBeTruthy()
  })

  it('keeps an orphan approval standalone as a fail-safe', () => {
    const items: ThreadItem[] = [
      { kind: 'approval', seq: 1, data: approval({ tool_call_id: 'missing' }) },
    ]
    const { container } = render(
      <RuntimeProvider runtime={createMockRuntime({ items, isRunning: true })}>
        <Thread virtualize={false} renderPending={() => null} />
      </RuntimeProvider>,
    )
    expect(container.querySelector('.jcode-approval-slot')).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Allow once' })).toBeTruthy()
  })

  it('restores a replay-settled tool to pending when /approval/pending supplies its gate', () => {
    const items: ThreadItem[] = [
      { kind: 'message', seq: 1, data: { id: 'user', role: 'user', content: 'Run tests', timestamp: 1_000 } },
      { kind: 'tool', seq: 2, data: executeTool({ status: 'done', output: undefined }) },
      { kind: 'approval', seq: 3, data: approval() },
    ]
    const { container } = render(
      <RuntimeProvider runtime={createMockRuntime({ items, isRunning: true })}>
        <Thread virtualize={false} renderPending={() => null} />
      </RuntimeProvider>,
    )

    expect(screen.getByRole('button', { name: 'Allow once' })).toBeTruthy()
    expect(container.querySelector('[data-tool-name="execute"]')).toBeNull()
    expect(container.querySelector('.jcode-approval-slot')).toBeNull()
  })

  it('renders a denied receipt without ever mounting the tool before the decision', () => {
    const gate = approval()
    const items: ThreadItem[] = [
      { kind: 'message', seq: 1, data: { id: 'user', role: 'user', content: 'Run tests', timestamp: 1_000 } },
      { kind: 'tool', seq: 2, data: executeTool({ status: 'running' }) },
      { kind: 'approval', seq: 3, data: gate },
    ]
    const runtime = createMockRuntime({ items, isRunning: true })
    const { container } = render(
      <RuntimeProvider runtime={runtime}>
        <Thread virtualize={false} renderPending={() => null} />
      </RuntimeProvider>,
    )

    expect(container.querySelector('[data-tool-name="execute"]')).toBeNull()
    fireEvent.click(screen.getByRole('button', { name: 'Deny' }))

    expect(screen.getByText('Denied')).toBeTruthy()
    expect(container.querySelector('[data-tool-name="execute"][data-tool-denied="true"]')).toBeTruthy()
  })

  it('keeps a standalone image renderer unmounted before approval and after denial', () => {
    let rendererCalls = 0
    const registry = createToolRendererRegistry().register('generate_image', () => {
      rendererCalls += 1
      return <div>image renderer mounted</div>
    })
    const gate: Approval = {
      ...approval(),
      tool_name: 'generate_image',
      tool_call_id: 'image-call',
    }
    const items: ThreadItem[] = [
      {
        kind: 'tool',
        seq: 1,
        data: {
          id: 'image-tool',
          toolCallID: 'image-call',
          name: 'generate_image',
          args: JSON.stringify({ prompt: 'test' }),
          status: 'running',
          surface: 'standalone',
          timestamp: 1_000,
        },
      },
      { kind: 'approval', seq: 2, data: gate },
    ]
    const runtime = createMockRuntime({ items, isRunning: true })
    const { container } = render(
      <RuntimeProvider runtime={runtime}>
        <ToolRegistryProvider registry={registry}>
          <Thread virtualize={false} renderPending={() => null} />
        </ToolRegistryProvider>
      </RuntimeProvider>,
    )

    expect(rendererCalls).toBe(0)
    expect(screen.queryByText('image renderer mounted')).toBeNull()
    fireEvent.click(screen.getByRole('button', { name: 'Deny' }))

    expect(rendererCalls).toBe(0)
    expect(screen.getByText('Denied')).toBeTruthy()
    expect(container.querySelector('[data-tool-name="generate_image"][data-tool-denied="true"]')).toBeTruthy()
  })
})

describe('ToolCallCard approval gate', () => {
  it('does not mount its header or renderer until approval resolves', () => {
    const gate = approval()
    const runtime = createMockRuntime()
    const pendingTool = executeTool({
      status: 'running',
      awaitingApproval: true,
      approval: gate,
    })
    const { container, rerender } = render(
      <RuntimeProvider runtime={runtime}>
        <ToolCallCard tool={pendingTool} />
      </RuntimeProvider>,
    )

    expect(screen.getByText('Run command')).toBeTruthy()
    expect(screen.queryByText('Shell')).toBeNull()
    expect(container.querySelector('[data-tool-name="execute"]')).toBeNull()

    rerender(
      <RuntimeProvider runtime={runtime}>
        <ToolCallCard tool={{
          ...pendingTool,
          status: 'done',
          awaitingApproval: undefined,
          approval: { ...gate, resolved: true, approved: true },
        }} />
      </RuntimeProvider>,
    )

    expect(screen.getByText('Shell')).toBeTruthy()
    expect(screen.getByText('Allowed')).toBeTruthy()
    expect(container.querySelector('[data-tool-name="execute"]')).toBeTruthy()
  })
})

describe('ActivityGroup approval receipts', () => {
  it('keeps Allowed visible on settled grouped tools after expansion', () => {
    const approved = (id: string): ToolCall => executeTool({
      id: `tool-${id}`,
      toolCallID: `call-${id}`,
      status: 'done',
      approval: {
        ...approval({ id: `approval-${id}`, tool_call_id: `call-${id}` }),
        resolved: true,
        approved: true,
      },
    })
    const { container } = render(
      <RuntimeProvider runtime={createMockRuntime({
        items: [
          { kind: 'tool', seq: 1, data: approved('one') },
          { kind: 'tool', seq: 2, data: approved('two') },
        ],
        isRunning: false,
      })}>
        <Thread virtualize={false} renderPending={() => null} />
      </RuntimeProvider>,
    )

    expect(screen.queryByText('Allowed')).toBeNull()
    const header = container.querySelector('[data-testid="activity-group"] > button')
    expect(header).toBeTruthy()
    fireEvent.click(header!)
    expect(screen.getAllByText('Allowed')).toHaveLength(2)
  })
})
