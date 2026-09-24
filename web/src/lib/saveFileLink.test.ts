import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

const mocks = vi.hoisted(() => ({
  isTauri: false,
  invoke: vi.fn(),
}))

vi.mock('./useDesktop', () => ({
  get isTauri() {
    return mocks.isTauri
  },
  invoke: mocks.invoke,
}))
vi.mock('./apiBase', () => ({ apiBase: '' }))

import { saveFileLink } from './saveFileLink'
import { validateServiceFileLink } from './saveFileLink'

const fileBlob = new Blob(['xlsx-bytes'], { type: 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet' })
Object.defineProperty(fileBlob, 'arrayBuffer', {
  value: async () => new TextEncoder().encode('xlsx-bytes').buffer,
})

beforeEach(() => {
  mocks.isTauri = false
  mocks.invoke.mockReset()
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
    ok: true,
    blob: async () => fileBlob,
  }))
})

afterEach(() => {
  delete (window as Window & { showSaveFilePicker?: unknown }).showSaveFilePicker
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})

describe('saveFileLink', () => {
  it('validates only same-service artifact download URLs', async () => {
    await expect(
      validateServiceFileLink('/api/tasks/task-7/artifacts/artifact-1/download', 'task-7'),
    ).resolves.toBe(true)
    expect(fetch).toHaveBeenCalledWith(
      '/api/tasks/task-7/artifacts/artifact-1/download',
      expect.objectContaining({ method: 'HEAD', cache: 'no-store' }),
    )
    await expect(validateServiceFileLink('示例销售数据.xlsx', 'task-7')).resolves.toBe(true)
    expect(fetch).toHaveBeenCalledWith(
      '/api/tasks/task-7/files/download?path=%E7%A4%BA%E4%BE%8B%E9%94%80%E5%94%AE%E6%95%B0%E6%8D%AE.xlsx',
      expect.objectContaining({ method: 'HEAD', cache: 'no-store' }),
    )
    await expect(validateServiceFileLink('../private.xlsx', 'task-7')).resolves.toBe(false)
    await expect(validateServiceFileLink('/api/files/private.xlsx', 'task-7')).resolves.toBe(false)
    await expect(validateServiceFileLink('file:///tmp/sales.xlsx', 'task-7')).resolves.toBe(false)
    await expect(validateServiceFileLink('https://files.example.com/sales.xlsx', 'task-7')).resolves.toBeNull()
  })

  it('uses the browser Save As picker before fetching and writes the response', async () => {
    const order: string[] = []
    const picker = vi.fn(async () => {
      order.push('picker')
      return {
        createWritable: async () => ({
          write: async (blob: Blob) => {
            order.push('write')
            expect(blob).toBe(fileBlob)
          },
          close: async () => { order.push('close') },
        }),
      }
    })
    Object.defineProperty(window, 'showSaveFilePicker', { configurable: true, value: picker })
    vi.mocked(fetch).mockImplementation(async () => {
      order.push('fetch')
      return { ok: true, blob: async () => fileBlob } as Response
    })

    await saveFileLink('/api/tasks/task-8/artifacts/artifact-2/download', 'sales.xlsx', 'task-8')

    expect(order).toEqual(['picker', 'fetch', 'write', 'close'])
    expect(picker).toHaveBeenCalledWith({ suggestedName: 'sales.xlsx' })
    expect(fetch).toHaveBeenCalledWith('/api/tasks/task-8/artifacts/artifact-2/download', expect.any(Object))
  })

  it('uses the workspace filename instead of the descriptive link label', async () => {
    const picker = vi.fn(async () => ({
      createWritable: async () => ({
        write: async () => {},
        close: async () => {},
      }),
    }))
    Object.defineProperty(window, 'showSaveFilePicker', { configurable: true, value: picker })

    await saveFileLink('示例销售数据.xlsx', '下载示例销售数据.xlsx', 'task-8')

    expect(picker).toHaveBeenCalledWith({ suggestedName: '示例销售数据.xlsx' })
    expect(fetch).toHaveBeenCalledWith(
      '/api/tasks/task-8/files/download?path=%E7%A4%BA%E4%BE%8B%E9%94%80%E5%94%AE%E6%95%B0%E6%8D%AE.xlsx',
      expect.any(Object),
    )
  })

  it('treats cancelling the browser Save As picker as a no-op', async () => {
    const picker = vi.fn().mockRejectedValue(new DOMException('Cancelled', 'AbortError'))
    Object.defineProperty(window, 'showSaveFilePicker', { configurable: true, value: picker })

    await expect(
      saveFileLink('/api/tasks/task-8/artifacts/artifact-3/download', 'sales.xlsx', 'task-8'),
    ).resolves.toBeUndefined()
    expect(fetch).not.toHaveBeenCalled()
  })

  it('asks the native shell to save the fetched bytes in desktop mode', async () => {
    mocks.isTauri = true
    mocks.invoke.mockResolvedValue(true)

    await saveFileLink('/api/tasks/task-8/artifacts/artifact-4/download', '../sales.xlsx', 'task-8')

    expect(mocks.invoke).toHaveBeenCalledWith('save_download_as', {
      fileName: 'sales.xlsx',
      data: Array.from(new TextEncoder().encode('xlsx-bytes')),
    })
  })
})
