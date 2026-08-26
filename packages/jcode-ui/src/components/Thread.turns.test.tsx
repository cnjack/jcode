import { act, cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { RuntimeProvider, createMockRuntime } from 'jcode-ui-core/runtime'
import { createToolRendererRegistry } from 'jcode-ui-core/adapters'
import { ToolCallProvider, ToolCallView } from 'jcode-ui-core/primitives'
import type { Approval, ThreadItem, ToolCall } from 'jcode-ui-core'
import { Thread } from './Thread.js'
import { ToolCallCard } from './ToolCallCard.js'
import { ToolRowHeader } from './ToolRow.js'
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

  it('keeps visible receipts before the final summary without reordering expanded work', () => {
    const items: ThreadItem[] = [
      { kind: 'message', seq: 1, data: { id: 'user', role: 'user', content: 'Make it', timestamp: 1_000 } },
      { kind: 'message', seq: 2, data: { id: 'before', role: 'assistant', content: 'Hidden work before', timestamp: 2_000 } },
      {
        kind: 'tool',
        seq: 3,
        data: executeTool({
          id: 'visible-receipt',
          name: 'artifact_receipt',
          args: '{}',
          surface: 'standalone',
          displayInfo: { title: 'Visible receipt', subtitle: 'artifact', kind: 'other' },
        }),
      },
      { kind: 'message', seq: 4, data: { id: 'after', role: 'assistant', content: 'Hidden work after', timestamp: 3_000 } },
      { kind: 'message', seq: 5, data: { id: 'final', role: 'assistant', content: 'Final summary', timestamp: 4_000 } },
    ]
    const { container } = render(
      <RuntimeProvider runtime={createMockRuntime({ items, isRunning: false })}>
        <Thread virtualize={false} renderPending={() => null} />
      </RuntimeProvider>,
    )

    expect(screen.queryByText('Hidden work before')).toBeNull()
    expect(screen.queryByText('Hidden work after')).toBeNull()
    expect(screen.getByText('Visible receipt')).toBeTruthy()
    expect(screen.getByText('Final summary')).toBeTruthy()
    let text = container.textContent ?? ''
    expect(text.indexOf('Visible receipt')).toBeLessThan(text.indexOf('Final summary'))

    fireEvent.click(screen.getByRole('button', { name: /Show work/ }))

    text = container.textContent ?? ''
    expect(text.indexOf('Hidden work before')).toBeLessThan(text.indexOf('Visible receipt'))
    expect(text.indexOf('Visible receipt')).toBeLessThan(text.indexOf('Hidden work after'))
    expect(text.indexOf('Hidden work after')).toBeLessThan(text.indexOf('Final summary'))
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

  it('blocks a standalone renderer for an option-based denial without approved', () => {
    let rendererCalls = 0
    const registry = createToolRendererRegistry().register('generate_image', () => {
      rendererCalls += 1
      return <div>image renderer mounted</div>
    })
    const deniedApproval = approval({
      tool_name: 'generate_image',
      tool_call_id: 'image-call',
      resolved: true,
      options: [{ id: 'deny-option', label: 'Deny', kind: 'deny' }],
      resolvedOptionId: 'deny-option',
    })
    const items: ThreadItem[] = [{
      kind: 'tool',
      seq: 1,
      data: {
        id: 'image-tool',
        toolCallID: 'image-call',
        name: 'generate_image',
        args: '{}',
        status: 'done',
        surface: 'standalone',
        timestamp: 1_000,
        approval: deniedApproval,
      },
    }]
    const { container } = render(
      <RuntimeProvider runtime={createMockRuntime({ items, isRunning: false })}>
        <ToolRegistryProvider registry={registry}>
          <Thread virtualize={false} renderPending={() => null} />
        </ToolRegistryProvider>
      </RuntimeProvider>,
    )

    expect(rendererCalls).toBe(0)
    expect(screen.queryByText('image renderer mounted')).toBeNull()
    expect(screen.getByText('Denied')).toBeTruthy()
    expect(container.querySelector('[data-tool-name="generate_image"][data-tool-denied="true"]')).toBeTruthy()

    const disclosure = container.querySelector('[data-tool-name="generate_image"] button')
    expect(disclosure).toBeTruthy()
    fireEvent.click(disclosure!)
    expect(rendererCalls).toBe(0)
    expect(screen.queryByText('image renderer mounted')).toBeNull()
  })

  it('mounts a standalone renderer for an option-based allowance without approved', () => {
    let rendererCalls = 0
    const registry = createToolRendererRegistry().register('generate_image', () => {
      rendererCalls += 1
      return <div>image renderer mounted</div>
    })
    const allowedApproval = approval({
      tool_name: 'generate_image',
      tool_call_id: 'image-call',
      resolved: true,
      options: [{ id: 'allow-option', label: 'Allow once', kind: 'allow_once' }],
      resolvedOptionId: 'allow-option',
    })
    const items: ThreadItem[] = [{
      kind: 'tool',
      seq: 1,
      data: {
        id: 'image-tool',
        toolCallID: 'image-call',
        name: 'generate_image',
        args: '{}',
        status: 'done',
        surface: 'standalone',
        timestamp: 1_000,
        approval: allowedApproval,
      },
    }]
    render(
      <RuntimeProvider runtime={createMockRuntime({ items, isRunning: false })}>
        <ToolRegistryProvider registry={registry}>
          <Thread virtualize={false} renderPending={() => null} />
        </ToolRegistryProvider>
      </RuntimeProvider>,
    )

    expect(rendererCalls).toBe(1)
    expect(screen.getByText('image renderer mounted')).toBeTruthy()
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

  it('derives allowed and denied receipts from resolved options', () => {
    const runtime = createMockRuntime()
    const options: Approval['options'] = [
      { id: 'allow', label: 'Allow once', kind: 'allow_once' },
      { id: 'deny', label: 'Deny', kind: 'deny' },
    ]
    const { container, rerender } = render(
      <RuntimeProvider runtime={runtime}>
        <ToolCallCard tool={executeTool({
          approval: approval({ resolved: true, options, resolvedOptionId: 'allow' }),
        })} />
      </RuntimeProvider>,
    )

    expect(screen.getByText('Allowed')).toBeTruthy()
    expect(container.querySelector('[data-tool-denied="true"]')).toBeNull()

    rerender(
      <RuntimeProvider runtime={runtime}>
        <ToolCallCard tool={executeTool({
          approval: approval({ resolved: true, options, resolvedOptionId: 'deny' }),
        })} />
      </RuntimeProvider>,
    )
    expect(screen.getByText('Denied')).toBeTruthy()
    expect(container.querySelector('[data-tool-denied="true"]')).toBeTruthy()
  })

  it('never mounts a custom renderer when a directly denied card is expanded', () => {
    let rendererCalls = 0
    const registry = createToolRendererRegistry().register('execute', () => {
      rendererCalls += 1
      return <div>execute renderer mounted</div>
    })
    const { container } = render(
      <RuntimeProvider runtime={createMockRuntime()}>
        <ToolCallCard tool={executeTool({ denied: true })} registry={registry} />
      </RuntimeProvider>,
    )

    expect(rendererCalls).toBe(0)
    const disclosure = container.querySelector('[data-tool-name="execute"] button')
    expect(disclosure).toBeTruthy()
    fireEvent.click(disclosure!)
    expect(container.querySelector('[data-tool-name="execute"]')?.getAttribute('data-expanded')).toBe('true')
    expect(rendererCalls).toBe(0)
    expect(screen.queryByText('execute renderer mounted')).toBeNull()
  })
})

describe('Headless ToolCallView approval gate', () => {
  it('normalizes a raw option denial before header, attributes, and renderer', () => {
    let rendererCalls = 0
    const registry = createToolRendererRegistry().register('execute', () => {
      rendererCalls += 1
      return <div>headless renderer mounted</div>
    })
    const options: Approval['options'] = [
      { id: 'deny', label: 'Deny', kind: 'deny' },
    ]
    const rawTool = executeTool({
      denied: undefined,
      approval: approval({ resolved: true, options, resolvedOptionId: 'deny' }),
    })
    const { container } = render(
      <ToolCallProvider value={{ registry }}>
        <ToolCallView
          tool={rawTool}
          renderHeader={(renderedTool, expanded, toggle) => (
            <button type="button" onClick={toggle}>
              {renderedTool.denied ? 'Denied receipt' : 'Allowed receipt'}
              {expanded ? ' expanded' : ' collapsed'}
            </button>
          )}
        />
      </ToolCallProvider>,
    )

    expect(screen.getByRole('button', { name: 'Denied receipt collapsed' })).toBeTruthy()
    expect(container.querySelector('[data-tool-name="execute"]')?.getAttribute('data-tool-denied')).toBe('true')
    expect(rendererCalls).toBe(0)

    fireEvent.click(screen.getByRole('button', { name: 'Denied receipt collapsed' }))
    expect(screen.getByRole('button', { name: 'Denied receipt expanded' })).toBeTruthy()
    expect(rendererCalls).toBe(0)
    expect(screen.queryByText('headless renderer mounted')).toBeNull()
  })

  it('does not invoke the ask-user slot for an option-based denial', () => {
    let askUserCalls = 0
    const registry = createToolRendererRegistry()
    const deniedAskUser = executeTool({
      name: 'ask_user',
      denied: undefined,
      approval: approval({
        tool_name: 'ask_user',
        resolved: true,
        options: [{ id: 'deny', label: 'Deny', kind: 'deny' }],
        resolvedOptionId: 'deny',
      }),
    })
    const { container } = render(
      <ToolCallProvider value={{
        registry,
        renderAskUser: () => {
          askUserCalls += 1
          return <div>ask-user slot mounted</div>
        },
      }}>
        <ToolCallView tool={deniedAskUser} />
      </ToolCallProvider>,
    )

    expect(askUserCalls).toBe(0)
    expect(screen.queryByText('ask-user slot mounted')).toBeNull()
    expect(container.querySelector('[data-tool-name="ask_user"]')?.getAttribute('data-tool-denied')).toBe('true')
  })

  it('keeps a denied subagent collapsed and never invokes result slots', () => {
    let outputCalls = 0
    let childrenCalls = 0
    const registry = createToolRendererRegistry()
    const deniedSubagent = executeTool({
      name: 'subagent',
      denied: undefined,
      output: 'private result',
      children: [executeTool({ id: 'child-tool' })],
      approval: approval({
        tool_name: 'subagent',
        resolved: true,
        options: [{ id: 'deny', label: 'Deny', kind: 'deny' }],
        resolvedOptionId: 'deny',
      }),
    })
    const { container } = render(
      <ToolCallProvider value={{ registry }}>
        <ToolCallView
          tool={deniedSubagent}
          renderHeader={(renderedTool, expanded, toggle) => (
            <button type="button" onClick={toggle}>
              {renderedTool.denied ? 'Denied subagent' : 'Allowed subagent'}
              {expanded ? ' expanded' : ' collapsed'}
            </button>
          )}
          renderSubagentOutput={() => {
            outputCalls += 1
            return <div>subagent output mounted</div>
          }}
          renderSubagentChildren={() => {
            childrenCalls += 1
            return <div>subagent children mounted</div>
          }}
        />
      </ToolCallProvider>,
    )

    expect(screen.getByRole('button', { name: 'Denied subagent collapsed' })).toBeTruthy()
    expect(container.querySelector('[data-tool-name="subagent"]')?.getAttribute('data-expanded')).toBe('false')
    expect(outputCalls).toBe(0)
    expect(childrenCalls).toBe(0)

    fireEvent.click(screen.getByRole('button', { name: 'Denied subagent collapsed' }))
    expect(screen.getByRole('button', { name: 'Denied subagent expanded' })).toBeTruthy()
    expect(outputCalls).toBe(0)
    expect(childrenCalls).toBe(0)
    expect(screen.queryByText('private result')).toBeNull()
    expect(screen.queryByText('subagent output mounted')).toBeNull()
    expect(screen.queryByText('subagent children mounted')).toBeNull()
  })
})

describe('ToolRowHeader approval receipts', () => {
  it('derives option-based receipts without relying on a parent card normalization', () => {
    const options: Approval['options'] = [
      { id: 'allow', label: 'Allow once', kind: 'allow_once' },
      { id: 'deny', label: 'Deny', kind: 'deny' },
    ]
    const { rerender } = render(<ToolRowHeader tool={executeTool({
      approval: approval({ resolved: true, options, resolvedOptionId: 'allow' }),
    })} />)
    expect(screen.getByText('Allowed')).toBeTruthy()

    rerender(<ToolRowHeader tool={executeTool({
      approval: approval({ resolved: true, options, resolvedOptionId: 'deny' }),
    })} />)
    expect(screen.getByText('Denied')).toBeTruthy()
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
