/**
 * Product composer (ChatInput) interaction tests.
 *
 * Covers the key interactions called out for the M14 extraction:
 *   - slash-menu trigger + keyboard apply
 *   - goal armed toggle (via the "+" menu and the armed chip)
 *   - model picker filtering (search + enabled gating)
 *   - image attachment limits (10MB cap, image/* only)
 */

import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { RuntimeProvider, createMockRuntime } from 'jcode-ui-core/runtime'
import type { ChatRuntime } from 'jcode-ui-core/runtime'
import { ChatInput } from './ChatInput.js'
import type { ProductComposerHost } from './host.js'
import type { ProviderInfo } from './types.js'

const PROVIDERS: ProviderInfo[] = [
  {
    id: 'openai',
    name: 'OpenAI',
    models: [
      { id: 'gpt-4o', name: 'GPT-4o', tool_call: true, image_support: true, context_limit: 128000 },
      { id: 'gpt-4o-mini', name: 'GPT-4o mini', tool_call: true, context_limit: 128000 },
    ],
  },
  {
    id: 'anthropic',
    name: 'Anthropic',
    models: [
      { id: 'claude-sonnet', name: 'Claude Sonnet', tool_call: true, reasoning: true },
      { id: 'claude-hidden', name: 'Claude Hidden', tool_call: true, enabled: false },
    ],
  },
]

function makeHost(overrides: Partial<ProductComposerHost> = {}): ProductComposerHost {
  return {
    providerName: 'openai',
    modelName: 'gpt-4o',
    mode: 'approval',
    providers: PROVIDERS,
    favoriteModels: [],
    recentModels: [],
    imageSupport: true,
    effortOverrides: {},
    slashCommands: [
      { slash: '/clear', description: 'Clear the conversation', type: 'builtin' },
      { slash: '/compact', description: 'Compact context', type: 'builtin' },
    ],
    hasMessages: false,
    goalArmed: false,
    sessionId: 's1',
    projectPath: '/tmp/project',
    tasks: [],
    selectModel: vi.fn(),
    selectMode: vi.fn(),
    setEffort: vi.fn(),
    toggleFavorite: vi.fn(),
    setModelEnabled: vi.fn(),
    refreshModels: vi.fn(),
    setGoalArmed: vi.fn(),
    fetchTaskStats: vi.fn(async () => null),
    validateWorkspacePaths: vi.fn(async () => []),
    browseFolders: vi.fn(async () => ({ current: '/', folders: [] })),
    switchWorkspace: vi.fn(async () => {}),
    fetchBranches: vi.fn(async () => ({ current: '', branches: [] })),
    checkoutBranch: vi.fn(async () => ({ branch: '' })),
    setGoal: vi.fn(async () => ({ objective: '', status: 'active' as const })),
    clearGoal: vi.fn(async () => {}),
    ...overrides,
  }
}

function renderComposer(host: ProductComposerHost, runtime?: ChatRuntime) {
  const rt = runtime ?? createMockRuntime()
  return {
    runtime: rt,
    ...render(
      <RuntimeProvider runtime={rt}>
        <ChatInput host={host} />
      </RuntimeProvider>,
    ),
  }
}

function textarea(container: HTMLElement): HTMLTextAreaElement {
  const el = container.querySelector('textarea')
  if (!el) throw new Error('composer textarea not found')
  return el
}

beforeEach(() => {
  localStorage.clear()
})

afterEach(() => {
  cleanup()
})

describe('slash menu', () => {
  it('opens on "/" and applies a command with Enter', async () => {
    const host = makeHost()
    const { container } = renderComposer(host)
    const ta = textarea(container)

    // Type "/" — the slash menu opens with the local /goal pseudo-command first.
    fireEvent.change(ta, { target: { value: '/' } })
    await waitFor(() => expect(screen.getByText('/goal')).toBeTruthy())
    expect(screen.getByText('/clear')).toBeTruthy()

    // Filter to /clear: ArrowDown past /goal and /clear... navigate then apply.
    fireEvent.change(ta, { target: { value: '/cle' } })
    await waitFor(() => expect(screen.queryByText('/compact')).toBeNull())
    fireEvent.keyDown(ta, { key: 'ArrowDown' }) // /goal → /clear
    fireEvent.keyDown(ta, { key: 'Enter' })
    expect(ta.value).toBe('/clear ')
  })

  it('does not open for path-like text', () => {
    const host = makeHost()
    const { container } = renderComposer(host)
    const ta = textarea(container)
    fireEvent.change(ta, { target: { value: '/usr/bin' } })
    expect(screen.queryByText('/clear')).toBeNull()
  })
})

describe('goal armed', () => {
  it('arms via the "+" menu and disarms via the chip', async () => {
    const setGoalArmed = vi.fn()
    const host = makeHost({ setGoalArmed })
    const view = renderComposer(host)

    // Open the "+" menu and pick "Goal".
    fireEvent.click(screen.getByTitle('Add'))
    fireEvent.click(screen.getByText('Goal'))
    expect(setGoalArmed).toHaveBeenCalledWith(true)

    // Armed state: placeholder switches and the chip renders; its X disarms.
    const armedHost = makeHost({ setGoalArmed, goalArmed: true })
    view.rerender(
      <RuntimeProvider runtime={view.runtime}>
        <ChatInput host={armedHost} />
      </RuntimeProvider>,
    )
    const ta = textarea(view.container)
    expect(ta.placeholder).toContain('goal')
    fireEvent.click(screen.getByTitle('Remove goal'))
    expect(setGoalArmed).toHaveBeenCalledWith(false)
  })

  it('"/goal" slash entry arms goal mode instead of inserting text', async () => {
    const setGoalArmed = vi.fn()
    const host = makeHost({ setGoalArmed })
    const { container } = renderComposer(host)
    const ta = textarea(container)

    fireEvent.change(ta, { target: { value: '/goal' } })
    await waitFor(() => expect(screen.getByText('/goal', { selector: 'span' })).toBeTruthy())
    fireEvent.keyDown(ta, { key: 'Enter' }) // first entry is the local /goal
    expect(setGoalArmed).toHaveBeenCalledWith(true)
    expect(ta.value).toBe('') // token stripped, nothing inserted
  })
})

describe('workspace picker', () => {
  it('offers the in-app folder browser when no native picker is available', async () => {
    const browseFolders = vi.fn(async () => ({
      current: '/tmp/project',
      folders: [{ name: 'src', path: '/tmp/project/src' }],
    }))
    renderComposer(makeHost({ browseFolders, pickFolder: undefined }))

    fireEvent.click(screen.getByText('project'))
    fireEvent.click(screen.getByText('Open folder'))

    await waitFor(() => expect(browseFolders).toHaveBeenCalledWith('/tmp/project'))
    expect(screen.getByText('src')).toBeTruthy()
  })
})

describe('model picker', () => {
  it('filters by search text and hides disabled models', async () => {
    const host = makeHost()
    renderComposer(host)

    // Open the model picker (button shows the current display name).
    fireEvent.click(screen.getByText('GPT-4o'))
    await waitFor(() => expect(screen.getByPlaceholderText('Filter models…')).toBeTruthy())

    // Enabled gating: claude-hidden never shows in the picker.
    expect(screen.queryByText('Claude Hidden')).toBeNull()
    expect(screen.getByText('Claude Sonnet')).toBeTruthy()
    expect(screen.getByText('GPT-4o mini')).toBeTruthy()

    // Search filter narrows the list.
    fireEvent.change(screen.getByPlaceholderText('Filter models…'), { target: { value: 'mini' } })
    await waitFor(() => expect(screen.getByText('GPT-4o mini')).toBeTruthy())
    expect(screen.queryByText('Claude Sonnet')).toBeNull()
  })

  it('selecting a model calls host.selectModel', async () => {
    const selectModel = vi.fn()
    const host = makeHost({ selectModel })
    renderComposer(host)
    fireEvent.click(screen.getByText('GPT-4o'))
    await waitFor(() => expect(screen.getByText('Claude Sonnet')).toBeTruthy())
    fireEvent.click(screen.getByText('Claude Sonnet'))
    expect(selectModel).toHaveBeenCalledWith('anthropic', 'claude-sonnet')
  })
})

describe('compact picker anchors', () => {
  it('keeps mode, model, and effort panels inside their own trigger anchors', async () => {
    renderComposer(makeHost({
      providerName: 'anthropic',
      modelName: 'claude-sonnet',
    }))

    const modeTrigger = screen.getByRole('button', { name: 'Ask for approval' })
    const modelTrigger = screen.getByRole('button', { name: 'Claude Sonnet' })
    const effortTrigger = screen.getByRole('button', { name: 'Effort: Default' })

    expect(modeTrigger.querySelector('.jcode-product-composer-picker-label')?.textContent).toBe('Ask for approval')
    expect(modelTrigger.querySelector('.jcode-product-composer-picker-label')?.textContent).toBe('Claude Sonnet')
    expect(effortTrigger.querySelector('.jcode-product-composer-picker-label')?.textContent).toBe('Effort')

    fireEvent.click(modeTrigger)
    expect(modeTrigger.closest('.jcode-product-composer-mode-picker')?.querySelector('.jcode-product-composer-mode-menu')).toBeTruthy()

    fireEvent.click(modelTrigger)
    await waitFor(() => expect(screen.getByPlaceholderText('Filter models…')).toBeTruthy())
    expect(modelTrigger.closest('.jcode-product-composer-model-picker')?.querySelector('.jcode-product-composer-model-menu')).toBeTruthy()

    fireEvent.click(effortTrigger)
    expect(effortTrigger.closest('.jcode-product-composer-effort-picker')?.querySelector('.jcode-product-composer-effort-menu')).toBeTruthy()
  })
})

describe('image attachments', () => {
  function fileInput(container: HTMLElement): HTMLInputElement {
    const el = container.querySelector('input[type="file"]')
    if (!el) throw new Error('file input not found')
    return el as HTMLInputElement
  }

  it('accepts images under 10MB and skips larger/non-image files', async () => {
    const host = makeHost()
    const { container } = renderComposer(host)
    const input = fileInput(container)

    const small = new File([new Uint8Array(1024)], 'small.png', { type: 'image/png' })
    const big = new File([new Uint8Array(11 * 1024 * 1024)], 'big.png', { type: 'image/png' })
    const doc = new File([new Uint8Array(128)], 'notes.txt', { type: 'text/plain' })
    fireEvent.change(input, { target: { files: [small, big, doc] } })

    // Only the small image makes it into the pending list.
    await waitFor(() => {
      const list = container.querySelector('.jcode-attachment-list')
      expect(list?.getAttribute('data-count')).toBe('1')
    })
  })

  it('sends images through the runtime sendMessage action', async () => {
    const host = makeHost()
    const runtime = createMockRuntime()
    const { container } = render(
      <RuntimeProvider runtime={runtime}>
        <ChatInput host={host} />
      </RuntimeProvider>,
    )
    fireEvent.change(fileInput(container), {
      target: { files: [new File([new Uint8Array(8)], 'a.png', { type: 'image/png' })] },
    })
    await waitFor(() => expect(container.querySelector('.jcode-attachment-list')).toBeTruthy())

    fireEvent.click(screen.getByLabelText('Send'))
    await waitFor(() => {
      const send = (runtime as ReturnType<typeof createMockRuntime>).calls.find((c) => c.action === 'sendMessage')
      expect(send).toBeTruthy()
      // Image-only body falls back to the attachedImages label, with the image payload.
      expect(send!.args[0]).toBe('(see attached images)')
      const images = send!.args[1] as { data: string; media_type: string; name?: string }[]
      expect(images).toHaveLength(1)
      expect(images[0].media_type).toBe('image/png')
      expect(images[0].name).toBe('a.png')
    })
  })
})
