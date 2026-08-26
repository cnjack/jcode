/**
 * Product composer (ChatInput) interaction tests.
 *
 * Covers the key interactions called out for the M14 extraction:
 *   - slash-menu trigger + keyboard apply
 *   - goal armed toggle (via the "+" menu and the armed chip)
 *   - model picker filtering (search + enabled gating)
 *   - image attachment limits (10MB cap, image/* only)
 */

import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { RuntimeProvider, createMockRuntime } from 'jcode-ui-core/runtime'
import type { ChatRuntime } from 'jcode-ui-core/runtime'
import { ChatInput } from './ChatInput.js'
import type { ProductComposerHost } from './host.js'
import type { FileDropEvent } from './host.js'
import type { ProviderInfo } from './types.js'

const PROVIDERS: ProviderInfo[] = [
  {
    id: 'openai',
    name: 'OpenAI',
    models: [
      { id: 'gpt-4o', name: 'GPT-4o', tool_call: true, enabled: true, image_support: true, context_limit: 128000 },
      { id: 'gpt-4o-mini', name: 'GPT-4o mini', tool_call: true, enabled: true, context_limit: 128000 },
    ],
  },
  {
    id: 'anthropic',
    name: 'Anthropic',
    models: [
      { id: 'claude-sonnet', name: 'Claude Sonnet', tool_call: true, enabled: true, reasoning: true },
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
    agents: [],
    agentName: '',
    slashCommands: [
      { slash: '/clear', description: 'Clear the conversation', type: 'builtin' },
      { slash: '/compact', description: 'Compact context', type: 'builtin' },
    ],
    hasMessages: false,
    goalArmed: false,
    sessionId: 's1',
    projectPath: '/tmp/project',
    workspaceKind: 'project',
    tasks: [],
    selectModel: vi.fn(),
    selectMode: vi.fn(),
    selectAgent: vi.fn(),
    setEffort: vi.fn(),
    toggleFavorite: vi.fn(),
    setModelEnabled: vi.fn(),
    refreshModels: vi.fn(),
    setGoalArmed: vi.fn(),
    fetchTaskStats: vi.fn(async () => null),
    validateWorkspacePaths: vi.fn(async () => []),
    browseFolders: vi.fn(async () => ({ current: '/', folders: [] })),
    switchWorkspace: vi.fn(async () => {}),
    startScratchWorkspace: vi.fn(async () => {}),
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
  vi.restoreAllMocks()
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

describe('composer toolbar', () => {
  it('keeps the add control without exposing a task-scoped Tools entry', () => {
    renderComposer(makeHost())

    expect(screen.getByRole('button', { name: 'Add' })).toBeTruthy()
    expect(screen.queryByRole('button', { name: 'Tools' })).toBeNull()
  })
})

describe('custom agent picker', () => {
  it('stays hidden when no custom agents are available', () => {
    renderComposer(makeHost())
    expect(screen.queryByLabelText('Agent: Default agent')).toBeNull()
  })

  it('switches between Default and a Markdown-defined custom agent', async () => {
    const selectAgent = vi.fn()
    const host = makeHost({
      agents: [
        {
          name: 'bug-fix-teammate',
          description: 'Investigates regressions and implements focused fixes.',
          model: 'anthropic/claude-sonnet',
        },
      ],
      selectAgent,
    })
    const view = renderComposer(host)

    fireEvent.click(screen.getByLabelText('Agent: Default agent'))
    expect(screen.getByText('bug-fix-teammate')).toBeTruthy()
    expect(screen.getByText('Investigates regressions and implements focused fixes.')).toBeTruthy()
    expect(screen.getByText('anthropic/claude-sonnet')).toBeTruthy()
    fireEvent.click(screen.getByText('bug-fix-teammate'))
    await waitFor(() => expect(selectAgent).toHaveBeenCalledWith('bug-fix-teammate'))

    view.rerender(
      <RuntimeProvider runtime={view.runtime}>
        <ChatInput host={{ ...host, agentName: 'bug-fix-teammate' }} />
      </RuntimeProvider>,
    )
    fireEvent.click(screen.getByLabelText('Agent: bug-fix-teammate'))
    fireEvent.click(screen.getByText('Default agent'))
    await waitFor(() => expect(selectAgent).toHaveBeenCalledWith(''))
  })
})

describe('workspace picker', () => {
  it('raises the open workspace panel above the composer toolbar', () => {
    const { container } = renderComposer(makeHost())

    fireEvent.click(screen.getByText('project'))

    const picker = container.querySelector('.ws-bar')
    expect(picker?.classList.contains('is-open')).toBe(true)
    expect(picker?.querySelector('.ws-panel')).toBeTruthy()
  })

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

  it('creates a managed no-project workspace without listing scratch paths as projects', async () => {
    const startScratchWorkspace = vi.fn(async () => {})
    renderComposer(makeHost({
      startScratchWorkspace,
      tasks: [
        { uuid: 'project-task', project: '/tmp/project', workspace_kind: 'project', updated_at: '2026-08-19T10:00:00Z' },
        { uuid: 'scratch-task', project: '/tmp/.jcode/workspace/2026-08-19-001', workspace_kind: 'scratch', updated_at: '2026-08-19T11:00:00Z' },
      ],
    }))

    fireEvent.click(screen.getByText('project'))
    expect(screen.queryByText('2026-08-19-001')).toBeNull()
    fireEvent.click(screen.getByText('Work without a project'))
    await waitFor(() => expect(startScratchWorkspace).toHaveBeenCalledTimes(1))
  })

  it('orders merged workspaces by timestamp instant across UTC offsets', () => {
    const { container } = renderComposer(makeHost({
      projectPath: '',
      tasks: [
        // 02:00 UTC — the older activity for this workspace.
        { uuid: 'older-mixed', project: '/tmp/mixed', workspace_kind: 'project', updated_at: '2026-08-19T10:00:00+08:00' },
        // 05:00 UTC — newer despite its smaller local clock value.
        { uuid: 'newer-mixed', project: '/tmp/mixed', workspace_kind: 'project', updated_at: '2026-08-19T05:00:00Z' },
        // 04:00 UTC — should follow the merged /tmp/mixed workspace.
        { uuid: 'intermediate', project: '/tmp/intermediate', workspace_kind: 'project', updated_at: '2026-08-19T12:00:00+08:00' },
      ],
    }))

    const pickerButton = container.querySelector('.ws-pill-action')
    if (!pickerButton) throw new Error('workspace picker button not found')
    fireEvent.click(pickerButton)

    const names = [...container.querySelectorAll('.ws-row-name')].map((node) => node.textContent)
    expect(names).toEqual(['mixed', 'intermediate'])
  })

  it('shows the no-project state as selected without allocating again', () => {
    const startScratchWorkspace = vi.fn(async () => {})
    renderComposer(makeHost({
      projectPath: '/tmp/.jcode/workspace/2026-08-19-001',
      workspaceKind: 'scratch',
      startScratchWorkspace,
    }))

    fireEvent.click(screen.getByText('Work without a project'))
    const labels = screen.getAllByText('Work without a project')
    fireEvent.click(labels[labels.length - 1])
    expect(startScratchWorkspace).not.toHaveBeenCalled()
  })
})

describe('model picker', () => {
  it('uses a bounded scrolling catalog on desktop', async () => {
    const { container } = renderComposer(makeHost())

    fireEvent.click(screen.getByText('GPT-4o'))
    await waitFor(() => expect(screen.getByPlaceholderText('Filter models…')).toBeTruthy())

    const menu = container.querySelector('.jcode-product-composer-model-menu')
    const list = menu?.querySelector('.jcode-product-composer-model-list')
    expect(menu).toBeTruthy()
    expect(list).toBeTruthy()
  })

  it('uses host copy for the image-output model badge', async () => {
    const providers: ProviderInfo[] = PROVIDERS.map((provider) => provider.id !== 'openai' ? provider : {
      ...provider,
      models: provider.models.map((model) => model.id !== 'gpt-4o' ? model : {
        ...model,
        output_modalities: ['text', 'image'],
      }),
    })
    renderComposer(makeHost({
      providers,
      strings: { modelImageOutput: 'Localized image output' },
    }))

    fireEvent.click(screen.getByText('GPT-4o'))
    await waitFor(() => expect(screen.getByPlaceholderText('Filter models…')).toBeTruthy())
    expect(screen.getAllByTitle('Localized image output').length).toBeGreaterThan(0)
    fireEvent.click(screen.getByText('Manage models…'))
    expect(screen.getByText('Localized image output')).toBeTruthy()
  })

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

  it('keeps image-only and non-tool models out of the chat picker', async () => {
    const providers: ProviderInfo[] = [{
      id: 'media',
      name: 'Media Provider',
      models: [
        { id: 'image-gen', name: 'Image Generator', tool_call: false, enabled: true, output_modalities: ['image'], capability_availability: 'supported' },
        { id: 'text-no-tools', name: 'Text Without Tools', tool_call: false, enabled: true, output_modalities: ['text'] },
        { id: 'chat', name: 'Tool Chat', tool_call: true, enabled: true, output_modalities: ['text'] },
      ],
    }]
    renderComposer(makeHost({ providers: [...PROVIDERS, ...providers] }))

    fireEvent.click(screen.getByText('GPT-4o'))
    await waitFor(() => expect(screen.getByPlaceholderText('Filter models…')).toBeTruthy())

    expect(screen.queryByText('Image Generator')).toBeNull()
    expect(screen.queryByText('Text Without Tools')).toBeNull()
    expect(screen.getByText('Tool Chat')).toBeTruthy()
  })

  it('fails closed when a model has no enabled flag', async () => {
    const providers = [
      ...PROVIDERS,
      {
        id: 'legacy',
        name: 'Legacy Provider',
        models: [
          {
            id: 'builtin-without-state',
            name: 'Built-in Without State',
            tool_call: true,
          },
        ],
      },
    ]
    renderComposer(makeHost({ providers }))

    fireEvent.click(screen.getByText('GPT-4o'))
    await waitFor(() => expect(screen.getByPlaceholderText('Filter models…')).toBeTruthy())

    expect(screen.queryByText('Built-in Without State')).toBeNull()
  })

  it('does not surface a disabled favorite', async () => {
    renderComposer(makeHost({
      favoriteModels: ['anthropic/claude-hidden'],
      recentModels: [{ provider: 'anthropic', model: 'claude-hidden' }],
    }))

    fireEvent.click(screen.getByText('GPT-4o'))
    await waitFor(() => expect(screen.getByPlaceholderText('Filter models…')).toBeTruthy())

    expect(screen.queryByText('Claude Hidden')).toBeNull()
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
    const { container } = renderComposer(makeHost({
      providerName: 'anthropic',
      modelName: 'claude-sonnet',
    }))

    expect(container.querySelector('.jcode-product-composer')).toBeTruthy()
    const modeTrigger = screen.getByRole('button', { name: 'Ask for approval' })
    const modelTrigger = screen.getByRole('button', { name: 'Claude Sonnet' })
    const effortTrigger = screen.getByRole('button', { name: 'Effort: Default' })

    expect(modeTrigger.title).toBe('Ask for approval')
    expect(modelTrigger.title).toBe('Claude Sonnet')
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

  it('nudges an edge-colliding model panel back inside the viewport', async () => {
    vi.spyOn(window, 'innerWidth', 'get').mockReturnValue(360)
    vi.spyOn(window, 'innerHeight', 'get').mockReturnValue(760)
    vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockImplementation(function (this: HTMLElement) {
      if (this.classList.contains('jcode-product-composer-model-menu')) {
        return {
          x: -64,
          y: 100,
          left: -64,
          right: 226,
          top: 100,
          bottom: 460,
          width: 290,
          height: 360,
          toJSON: () => ({}),
        }
      }
      return {
        x: 0,
        y: 0,
        left: 0,
        right: 0,
        top: 0,
        bottom: 0,
        width: 0,
        height: 0,
        toJSON: () => ({}),
      }
    })

    const { container } = renderComposer(makeHost())
    fireEvent.click(screen.getByRole('button', { name: 'GPT-4o' }))

    const panel = container.querySelector<HTMLElement>('.jcode-product-composer-model-menu')
    await waitFor(() => expect(panel?.style.transform).toBe('translate(76px, 0px)'))
  })
})

describe('file attachments', () => {
  function fileInput(container: HTMLElement): HTMLInputElement {
    const el = container.querySelector('input[type="file"]')
    if (!el) throw new Error('file input not found')
    return el as HTMLInputElement
  }

  it('keeps small images inline and stages other selected files for upload', async () => {
    const host = makeHost()
    const { container } = renderComposer(host)
    const input = fileInput(container)

    const small = new File([new Uint8Array(1024)], 'small.png', { type: 'image/png' })
    const big = new File([new Uint8Array(11 * 1024 * 1024)], 'big.png', { type: 'image/png' })
    const doc = new File([new Uint8Array(128)], 'notes.txt', { type: 'text/plain' })
    fireEvent.change(input, { target: { files: [small, big, doc] } })

    await waitFor(() => {
      expect(container.querySelector('.jcode-attachment-list')?.getAttribute('data-count')).toBe('1')
      expect(container.querySelector('.jcode-pending-attachments')?.getAttribute('data-count')).toBe('2')
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

  it('accepts browser file drops and turns unsupported files into prompt context', async () => {
    const runtime = createMockRuntime()
    const uploadDroppedFile = vi.fn(async () => ({
      path: '/Users/jack/.jcode/uploads/s1/notes-a1b2c3.pdf',
      name: 'notes-a1b2c3.pdf',
      size: 128,
    }))
    const { container } = renderComposer(makeHost({
      strings: { attachFiles: '添加附件' },
      uploadDroppedFile,
    }), runtime)
    const composer = container.querySelector('.jcode-product-composer') as HTMLDivElement
    const doc = new File([new Uint8Array(128)], 'notes.pdf', { type: 'application/pdf' })
    const dataTransfer = { types: ['Files'], files: [doc], dropEffect: 'none' }

    fireEvent.dragEnter(composer, { dataTransfer })
    expect(composer.textContent).toContain('添加附件')
    fireEvent.drop(composer, { dataTransfer })

    const ta = textarea(container)
    expect(ta.value).toBe('')
    expect(container.querySelector('.jcode-pending-attachments')?.getAttribute('data-count')).toBe('1')
    fireEvent.click(screen.getByLabelText('Send'))
    await waitFor(() => {
      const send = (runtime as ReturnType<typeof createMockRuntime>).calls.find((call) => call.action === 'sendMessage')
      expect(send?.args[0]).toContain('[File Drop]')
      expect(send?.args[0]).toContain('/Users/jack/.jcode/uploads/s1/notes-a1b2c3.pdf')
    })
    expect(uploadDroppedFile).toHaveBeenCalledWith('s1', doc)
  })

  it('keeps a browser file pending when upload fails', async () => {
    const runtime = createMockRuntime()
    const uploadDroppedFile = vi.fn(async () => { throw new Error('offline') })
    const { container } = renderComposer(makeHost({ uploadDroppedFile }), runtime)
    const composer = container.querySelector('.jcode-product-composer') as HTMLDivElement
    fireEvent.drop(composer, {
      dataTransfer: {
        types: ['Files'],
        files: [new File([new Uint8Array(8)], 'notes.pdf', { type: 'application/pdf' })],
        dropEffect: 'none',
      },
    })

    fireEvent.click(screen.getByLabelText('Send'))
    await waitFor(() => expect(container.textContent).toContain('File upload failed'))
    expect((runtime as ReturnType<typeof createMockRuntime>).calls.some((call) => call.action === 'sendMessage')).toBe(false)
    expect(container.querySelector('.jcode-pending-attachments')?.getAttribute('data-count')).toBe('1')
  })

  it('keeps supported browser-dropped images on the image attachment path', async () => {
    const { container } = renderComposer(makeHost())
    const composer = container.querySelector('.jcode-product-composer') as HTMLDivElement
    const image = new File([new Uint8Array(16)], 'drop.png', { type: 'image/png' })

    fireEvent.drop(composer, {
      dataTransfer: { types: ['Files'], files: [image], dropEffect: 'none' },
    })

    await waitFor(() => {
      expect(container.querySelector('.jcode-attachment-list')?.getAttribute('data-count')).toBe('1')
    })
    expect(textarea(container).value).toBe('')
  })

  it('uses native absolute paths and only loads supported dropped images', async () => {
    let onFileDrop: ((event: FileDropEvent) => void) | undefined
    const unlisten = vi.fn()
    const readDroppedImage = vi.fn(async (path: string) => path.endsWith('.png') ? {
      data: 'cG5n',
      media_type: 'image/png',
      name: 'screen.png',
    } : null)
    const host = makeHost({
      listenForFileDrops: vi.fn(async (listener) => {
        onFileDrop = listener
        return unlisten
      }),
      readDroppedImage,
    })
    const { container } = renderComposer(host)
    const composer = container.querySelector('.jcode-product-composer') as HTMLDivElement
    vi.spyOn(composer, 'getBoundingClientRect').mockReturnValue({
      left: 10,
      top: 20,
      right: 510,
      bottom: 220,
      width: 500,
      height: 200,
      x: 10,
      y: 20,
      toJSON: () => ({}),
    })
    await waitFor(() => expect(onFileDrop).toBeTypeOf('function'))

    act(() => onFileDrop?.({ type: 'drop', paths: [
      '/Users/jack/Documents/report.pdf',
      '/Users/jack/Documents/screen.png',
    ], x: 100, y: 100 }))

    await waitFor(() => {
      expect(textarea(container).value).toContain('/Users/jack/Documents/report.pdf')
      expect(container.querySelector('.jcode-attachment-list')?.getAttribute('data-count')).toBe('1')
    })
    expect(readDroppedImage).toHaveBeenCalledTimes(2)
    expect(textarea(container).value).toContain('dragged a file into the input')
  })
})
