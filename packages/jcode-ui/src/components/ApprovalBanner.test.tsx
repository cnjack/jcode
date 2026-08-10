import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it } from 'vitest'
import { RuntimeProvider, createMockRuntime } from 'jcode-ui-core/runtime'
import type { Approval } from 'jcode-ui-core'
import { ApprovalBanner } from './ApprovalBanner.js'

afterEach(cleanup)

function renderApproval(approval: Approval) {
  const runtime = createMockRuntime()
  render(
    <RuntimeProvider runtime={runtime}>
      <ApprovalBanner approval={approval} />
    </RuntimeProvider>,
  )
  return runtime
}

const BASE_APPROVAL: Approval = {
  id: 'approval-1',
  tool_name: 'generate_image',
  tool_args: JSON.stringify({ prompt: 'private prompt text', api_key: 'secret' }),
  is_external: true,
  approvalClass: 'billable_external',
  billableSummary: {
    capability: 'image.generate',
    provider: 'qianwen',
    model: 'wanx-v1',
    size: '1024x1024',
    count: 1,
    billable: true,
  },
}

describe('ApprovalBanner billable approvals', () => {
  it('fails closed when the host supplies no structured decision options', () => {
    const { container } = render(<RuntimeProvider runtime={createMockRuntime()}><ApprovalBanner approval={BASE_APPROVAL} /></RuntimeProvider>)

    expect(screen.getByRole('button', { name: 'Deny' })).toBeTruthy()
    expect(screen.queryByRole('button', { name: /allow/i })).toBeNull()
    expect(screen.queryByText(/private prompt text/i)).toBeNull()
    expect(screen.queryByText(/secret/i)).toBeNull()
    expect(container.querySelector('details')).toBeNull()
  })

  it('returns the opaque allow-once option id and never offers a session grant', () => {
    const runtime = renderApproval({
      ...BASE_APPROVAL,
      options: [
        { id: 'opaque-deny', label: 'Deny', kind: 'deny' },
        { id: 'opaque-allow', label: 'Allow once', kind: 'allow_once' },
        { id: 'opaque-always', label: 'Always allow', kind: 'allow_always' },
      ],
    })

    fireEvent.click(screen.getByRole('button', { name: 'Allow once' }))
    expect(runtime.calls).toContainEqual({
      action: 'resolveApprovalOption',
      args: ['approval-1', 'opaque-allow'],
    })
    expect(screen.queryByRole('button', { name: /always allow/i })).toBeNull()
    expect(screen.queryByRole('button', { name: /allow all/i })).toBeNull()
  })

  it('renders web search as search rather than image generation', () => {
    renderApproval({
      ...BASE_APPROVAL,
      tool_name: 'provider_web_search',
      billableSummary: {
        capability: 'web.search',
        provider: 'BigModel',
        model: 'web_search_prime',
        count: 1,
        billable: true,
      },
    })

    expect(screen.getByText('External web search')).toBeTruthy()
    expect(screen.getByText('Run 1 web search?')).toBeTruthy()
    expect(screen.queryByText(/image generation/i)).toBeNull()
    expect(screen.queryByRole('button', { name: /allow/i })).toBeNull()
  })

  it('shows native aspect ratio and resolution before a billable image decision', () => {
    renderApproval({
      ...BASE_APPROVAL,
      billableSummary: {
        capability: 'image.generate',
        provider: 'xai',
        model: 'grok-imagine-image-quality',
        aspect_ratio: '9:16',
        resolution: '2k',
        count: 1,
        billable: true,
      },
    })

    expect(screen.getByText(/xai · grok-imagine-image-quality · 9:16 · 2k · 1 image/)).toBeTruthy()
  })

  it('hides allow when a host cannot return opaque option ids', () => {
    const base = createMockRuntime()
    const { resolveApprovalOption: _resolveApprovalOption, ...actions } = base.actions
    const runtime = { ...base, actions }
    render(
      <RuntimeProvider runtime={runtime}>
        <ApprovalBanner approval={{
          ...BASE_APPROVAL,
          options: [
            { id: 'opaque-deny', label: 'Deny', kind: 'deny' },
            { id: 'opaque-allow', label: 'Allow once', kind: 'allow_once' },
          ],
        }} />
      </RuntimeProvider>,
    )

    expect(screen.getByRole('button', { name: 'Deny' })).toBeTruthy()
    expect(screen.queryByRole('button', { name: 'Allow once' })).toBeNull()
    fireEvent.click(screen.getByRole('button', { name: 'Deny' }))
    expect(base.calls).toContainEqual({
      action: 'resolveApproval',
      args: ['approval-1', false, false],
    })
  })
})
