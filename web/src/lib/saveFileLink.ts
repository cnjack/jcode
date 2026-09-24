import { apiBase } from './apiBase'
import { api } from './api'
import { invoke, isTauri } from './useDesktop'

type SaveFileHandle = {
  createWritable(): Promise<{
    write(data: Blob): Promise<void>
    close(): Promise<void>
    abort?: () => Promise<void>
  }>
}

type SaveFilePicker = (options: { suggestedName: string }) => Promise<SaveFileHandle>

const downloadCheckCache = new Map<string, { checkedAt: number; result: Promise<boolean | null> }>()
const downloadCheckTtlMs = 5_000

/** Verify that a service artifact URL or relative task-workspace file exists. */
export async function validateServiceFileLink(href: string, currentTaskId: string): Promise<boolean | null> {
  const resolved = resolveServiceDownloadLink(href, currentTaskId)
  if (resolved === 'external') return null
  if (!resolved) return false

  const cacheKey = `${resolved.kind}:${resolved.taskId}:${resolved.url.href}`
  const cached = downloadCheckCache.get(cacheKey)
  if (cached && Date.now() - cached.checkedAt < downloadCheckTtlMs) return cached.result

  const result = (resolved.kind === 'artifact'
    ? api.artifactDownloadAvailable(resolved.taskId, resolved.artifactId)
    : api.workspaceFileDownloadAvailable(resolved.taskId, resolved.path)
  ).catch(() => false)
  downloadCheckCache.set(cacheKey, { checkedAt: Date.now(), result })
  return result
}

/** Save a markdown-linked file through the native or browser Save As dialog. */
export async function saveFileLink(href: string, fileName: string, currentTaskId: string): Promise<void> {
  const resolved = resolveServiceDownloadLink(href, currentTaskId)
  const suggestedName = resolved && resolved !== 'external' && resolved.kind === 'workspace'
    ? resolved.path.split('/').pop() ?? fileName
    : fileName
  const safeName = sanitizeFileName(suggestedName)
  if (!isTauri) {
    const picker = (window as Window & { showSaveFilePicker?: SaveFilePicker }).showSaveFilePicker
    if (picker) {
      let handle: SaveFileHandle
      try {
        // Open the picker before the first network await so browser user
        // activation is still available.
        handle = await picker({ suggestedName: safeName })
      } catch (error) {
        if (error instanceof DOMException && error.name === 'AbortError') return
        throw error
      }

      const writable = await handle.createWritable()
      try {
        await writable.write(await fetchLinkedFile(href, currentTaskId))
        await writable.close()
      } catch (error) {
        await writable.abort?.()
        throw error
      }
      return
    }
  }

  const blob = await fetchLinkedFile(href, currentTaskId)
  if (isTauri) {
    const data = Array.from(new Uint8Array(await blob.arrayBuffer()))
    await invoke<boolean>('save_download_as', { fileName: safeName, data })
    return
  }

  const objectUrl = URL.createObjectURL(blob)
  const anchor = document.createElement('a')
  anchor.href = objectUrl
  anchor.download = safeName
  document.body.appendChild(anchor)
  anchor.click()
  anchor.remove()
  window.setTimeout(() => URL.revokeObjectURL(objectUrl), 0)
}

async function fetchLinkedFile(href: string, currentTaskId: string): Promise<Blob> {
  const resolved = resolveServiceDownloadLink(href, currentTaskId)
  if (!resolved || resolved === 'external') throw new Error('File link is not a service artifact')
  return resolved.kind === 'artifact'
    ? api.artifactDownload(resolved.taskId, resolved.artifactId)
    : api.workspaceFileDownload(resolved.taskId, resolved.path)
}

type ResolvedServiceDownload =
  | { kind: 'artifact'; url: URL; taskId: string; artifactId: string }
  | { kind: 'workspace'; url: URL; taskId: string; path: string }

function resolveServiceDownloadLink(href: string, currentTaskId: string): 'external' | ResolvedServiceDownload | null {
  const base = apiBase ? `${apiBase}/` : new URL('/', document.baseURI).href
  let url: URL
  try {
    url = new URL(href, base)
    if (url.protocol !== 'http:' && url.protocol !== 'https:') return null
    if (url.origin !== new URL(base).origin) return 'external'
  } catch {
    return null
  }

  const match = /^\/api\/tasks\/([^/]+)\/artifacts\/([^/]+)\/download$/.exec(url.pathname)
  if (match) {
    try {
      const taskId = decodeURIComponent(match[1])
      if (!currentTaskId || taskId !== currentTaskId) return null
      return {
        kind: 'artifact',
        url,
        taskId,
        artifactId: decodeURIComponent(match[2]),
      }
    } catch {
      return null
    }
  }

  const path = relativeWorkspacePath(href)
  if (!path || !currentTaskId || url.origin !== new URL(base).origin) return null
  return { kind: 'workspace', url, taskId: currentTaskId, path }
}

function relativeWorkspacePath(href: string): string | null {
  const encodedPath = href.trim().split(/[?#]/, 1)[0]
  if (
    !encodedPath ||
    encodedPath.startsWith('/') ||
    encodedPath.startsWith('\\') ||
    /^[a-z][a-z\d+.-]*:/i.test(encodedPath)
  ) {
    return null
  }

  let path: string
  try {
    path = decodeURIComponent(encodedPath)
  } catch {
    return null
  }
  if (path.includes('\\') || path.includes('\0')) return null
  const parts = path.split('/')
  if (parts.some((part) => part === '' || part === '..')) return null
  const normalized = parts.filter((part) => part !== '.').join('/')
  return normalized || null
}

function sanitizeFileName(fileName: string): string {
  const baseName = fileName.trim().split(/[\\/]/).pop() ?? ''
  const safeName = baseName.replace(/[\u0000-\u001f\u007f]/g, '')
  return safeName && safeName !== '.' && safeName !== '..' ? safeName : 'download'
}
