import { useCallback, useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import DOMPurify from 'dompurify'
import { marked } from 'marked'
import {
  ArrowDownTrayIcon,
  ArrowPathIcon,
  ArrowTopRightOnSquareIcon,
  ArrowsPointingOutIcon,
  ClipboardDocumentIcon,
  CloudArrowUpIcon,
  DocumentIcon,
  ExclamationTriangleIcon,
  FolderOpenIcon,
  LockClosedIcon,
  TrashIcon,
  XMarkIcon,
} from '@heroicons/react/24/outline'
import { useAppSelector } from '../app/hooks'
import { api } from '../lib/api'
import type { ArtifactRecord, ArtifactShareResult, ArtifactShareSummary } from '../lib/types'
import { isTauri } from '../lib/useDesktop'

const HTML_CSP = "default-src 'none'; img-src data: blob:; style-src 'unsafe-inline'; script-src 'unsafe-inline'; connect-src 'none'; frame-src 'none'; object-src 'none'; base-uri 'none'; form-action 'none'"

const BLOCKED_HOST_OPEN_EXTENSIONS = new Set([
  '.app', '.application', '.bat', '.cmd', '.com', '.command', '.cpl', '.desktop', '.exe', '.gadget',
  '.hta', '.htm', '.html', '.inf', '.ins', '.isp', '.jar', '.js', '.jse', '.lnk', '.msc', '.msi',
  '.msp', '.mst', '.pif', '.ps1', '.reg', '.scr', '.sh', '.svg', '.svgz', '.url', '.vb', '.vbe',
  '.vbs', '.workflow', '.ws', '.wsf', '.wsh', '.xhtml',
])

export function canOpenArtifactOnDesktop(record: ArtifactRecord): boolean {
  if (record.kind === 'html') return false
  const mediaType = record.media_type.split(';', 1)[0]?.trim().toLowerCase()
  if (mediaType === 'text/html' || mediaType === 'application/xhtml+xml' || mediaType === 'image/svg+xml') return false
  const base = record.relative_path.toLowerCase().split('/').pop() ?? ''
  const dot = base.lastIndexOf('.')
  return dot < 0 || !BLOCKED_HOST_OPEN_EXTENSIONS.has(base.slice(dot))
}

function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`
}

function escapeHTML(source: string): string {
  return source.replaceAll('&', '&amp;').replaceAll('<', '&lt;').replaceAll('>', '&gt;')
}

function readBlobText(blob: Blob): Promise<string> {
  if (typeof blob.text === 'function') return blob.text()
  return new Promise((resolve, reject) => {
    const reader = new FileReader()
    reader.onload = () => resolve(String(reader.result ?? ''))
    reader.onerror = () => reject(reader.error ?? new Error('blob read failed'))
    reader.readAsText(blob)
  })
}

function parseCSV(source: string, maxRows = 200, maxColumns = 50): string[][] {
  const rows: string[][] = []
  let row: string[] = []
  let field = ''
  let quoted = false
  for (let i = 0; i < source.length && rows.length < maxRows; i++) {
    const char = source[i]
    if (char === '"') {
      if (quoted && source[i + 1] === '"') { field += '"'; i++ } else quoted = !quoted
    } else if (char === ',' && !quoted) {
      if (row.length < maxColumns) row.push(field)
      field = ''
    } else if ((char === '\n' || char === '\r') && !quoted) {
      if (char === '\r' && source[i + 1] === '\n') i++
      if (row.length < maxColumns) row.push(field)
      rows.push(row)
      row = []
      field = ''
    } else {
      field += char
    }
  }
  if ((field || row.length) && rows.length < maxRows) {
    if (row.length < maxColumns) row.push(field)
    rows.push(row)
  }
  return rows
}

export function ArtifactsPanel({ initialSelectedID = '' }: { initialSelectedID?: string }) {
  const { t } = useTranslation()
  const taskId = useAppSelector((state) => state.session.currentSessionId)
  const [records, setRecords] = useState<ArtifactRecord[]>([])
  const [selectedID, setSelectedID] = useState(initialSelectedID)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [cloudLoggedIn, setCloudLoggedIn] = useState(false)
  const [fullscreen, setFullscreen] = useState(false)
  const [shareOpen, setShareOpen] = useState(false)
  const [downloading, setDownloading] = useState(false)
  const [actionError, setActionError] = useState('')

  const load = useCallback(async (preferredID?: string) => {
    if (!taskId) { setRecords([]); setLoading(false); return }
    setLoading(true)
    setError('')
    try {
      const next = await api.artifacts(taskId)
      setRecords(next)
      setSelectedID((current) => {
        if (preferredID && next.some((item) => item.id === preferredID)) return preferredID
        return next.some((item) => item.id === current) ? current : (next[0]?.id ?? '')
      })
      await api.markArtifactsViewed(taskId).catch(() => undefined)
    } catch {
      setError(t('artifacts.loadError'))
    } finally {
      setLoading(false)
    }
  }, [taskId, t])

  useEffect(() => { void load(initialSelectedID) }, [initialSelectedID, load])
  useEffect(() => {
    let active = true
    void api.cloudStatus().then((status) => { if (active) setCloudLoggedIn(status.logged_in) }).catch(() => undefined)
    return () => { active = false }
  }, [])
  useEffect(() => {
    const refresh = (event: Event) => {
      const detail = (event as CustomEvent<{ artifact_id?: string }>).detail
      void load(detail?.artifact_id)
    }
    window.addEventListener('jcode:artifact-upserted', refresh)
    return () => window.removeEventListener('jcode:artifact-upserted', refresh)
  }, [load])

  const selected = records.find((item) => item.id === selectedID) ?? null
  const viewer = selected ? <ArtifactViewer taskId={taskId} record={selected} /> : null

  const download = useCallback(async () => {
    if (!selected || downloading) return
    setDownloading(true)
    setActionError('')
    try {
      const blob = await api.artifactDownload(taskId, selected.id)
      const objectURL = URL.createObjectURL(blob)
      const anchor = document.createElement('a')
      anchor.href = objectURL
      anchor.download = selected.relative_path.split('/').pop() || selected.title
      document.body.appendChild(anchor)
      anchor.click()
      anchor.remove()
      window.setTimeout(() => URL.revokeObjectURL(objectURL), 0)
    } catch {
      setActionError(t('artifacts.downloadError'))
    } finally {
      setDownloading(false)
    }
  }, [downloading, selected, t, taskId])

  const desktopAction = useCallback(async (reveal: boolean) => {
    if (!selected) return
    setActionError('')
    try {
      if (reveal) await api.revealArtifact(taskId, selected.id)
      else await api.openArtifact(taskId, selected.id)
    } catch {
      setActionError(t('artifacts.desktopError'))
    }
  }, [selected, t, taskId])

  return (
    <div className="flex h-full min-h-0 flex-col">
      <div className="flex min-h-0 flex-1">
        <div className="w-[168px] shrink-0 overflow-y-auto border-r border-[var(--color-border)] py-1.5">
          {loading && <div role="status" className="px-3 py-6 text-center text-xs text-[var(--color-muted-foreground)]">{t('common.loading')}</div>}
          {error && <div role="alert" className="px-3 py-6 text-center text-xs text-[var(--color-error-fg)]">{error}</div>}
          {!loading && !error && records.length === 0 && <div className="px-3 py-6 text-center text-xs text-[var(--color-muted-foreground)]">{t('artifacts.empty')}</div>}
          {records.map((record) => (
            <button
              key={record.id}
              type="button"
              onClick={() => setSelectedID(record.id)}
              className={`mx-1 flex w-[calc(100%-0.5rem)] items-start gap-2 rounded-[var(--radius-md)] px-2 py-2 text-left transition-colors ${selectedID === record.id ? 'bg-[var(--color-muted)]' : 'hover:bg-[var(--color-muted)]'}`}
            >
              <DocumentIcon className="mt-0.5 h-4 w-4 shrink-0 text-[var(--color-accent-neutral)]" />
              <span className="min-w-0 flex-1">
                <span className="block truncate text-xs font-medium text-[var(--color-foreground)]">{record.title}</span>
                <span className="mt-0.5 block truncate font-mono text-[10px] text-[var(--color-muted-foreground)]">{record.kind} · r{record.revision}</span>
              </span>
              {record.status !== 'available' && <ExclamationTriangleIcon className="h-3.5 w-3.5 text-[var(--color-warning-fg)]" />}
            </button>
          ))}
        </div>

        <div className="min-w-0 flex-1 overflow-hidden">
          {selected ? (
            <div className="flex h-full min-h-0 flex-col">
              <header className="flex shrink-0 items-start gap-3 border-b border-[var(--color-border)] px-3 py-2.5">
                <div className="min-w-0 flex-1">
                  <h2 className="truncate text-sm font-semibold text-[var(--color-foreground)]">{selected.title}</h2>
                  <p className="mt-0.5 truncate font-mono text-[10px] text-[var(--color-muted-foreground)]">{selected.relative_path} · {formatBytes(selected.size)}</p>
                </div>
                <div className="flex shrink-0 items-center gap-1">
                  {cloudLoggedIn && <button type="button" aria-label={t('artifacts.share')} title={t('artifacts.share')} onClick={() => setShareOpen(true)} className="artifact-action"><CloudArrowUpIcon className="h-4 w-4" /></button>}
                  {isTauri && canOpenArtifactOnDesktop(selected) && <button type="button" aria-label={t('artifacts.open')} title={t('artifacts.open')} onClick={() => void desktopAction(false)} className="artifact-action"><ArrowTopRightOnSquareIcon className="h-4 w-4" /></button>}
                  {isTauri && <button type="button" aria-label={t('artifacts.reveal')} title={t('artifacts.reveal')} onClick={() => void desktopAction(true)} className="artifact-action"><FolderOpenIcon className="h-4 w-4" /></button>}
                  <button type="button" disabled={downloading} aria-label={t('artifacts.download')} title={t('artifacts.download')} onClick={() => void download()} className="artifact-action"><ArrowDownTrayIcon className="h-4 w-4" /></button>
                  <button type="button" aria-label={t('artifacts.fullscreen')} title={t('artifacts.fullscreen')} onClick={() => setFullscreen(true)} className="artifact-action"><ArrowsPointingOutIcon className="h-4 w-4" /></button>
                </div>
              </header>
              {actionError && <div role="alert" className="border-b border-[var(--color-border)] px-3 py-2 text-xs text-[var(--color-error-fg)]">{actionError}</div>}
              <div className="min-h-0 flex-1 overflow-auto p-3">{viewer}</div>
            </div>
          ) : !loading && <div className="grid h-full place-items-center text-xs text-[var(--color-muted-foreground)]">{t('artifacts.select')}</div>}
        </div>
      </div>

      {fullscreen && selected && (
        <div role="dialog" aria-modal="true" aria-label={selected.title} className="fixed inset-3 z-[var(--z-modal)] flex flex-col overflow-hidden rounded-[var(--radius-xl)] border border-[var(--color-border)] bg-[var(--color-background)] shadow-[var(--shadow-xl)]">
          <header className="flex h-12 shrink-0 items-center border-b border-[var(--color-border)] px-4">
            <h2 className="min-w-0 flex-1 truncate text-sm font-semibold">{selected.title}</h2>
            <button type="button" aria-label={t('common.close')} onClick={() => setFullscreen(false)} className="artifact-action"><XMarkIcon className="h-4 w-4" /></button>
          </header>
          <div className="min-h-0 flex-1 overflow-auto p-5"><ArtifactViewer taskId={taskId} record={selected} /></div>
        </div>
      )}
      {shareOpen && selected && (
        <ArtifactShareDialog key={`${selected.id}:${selected.revision}`} taskId={taskId} record={selected} onClose={() => setShareOpen(false)} />
      )}
    </div>
  )
}

function ArtifactShareDialog({ taskId, record, onClose }: { taskId: string; record: ArtifactRecord; onClose: () => void }) {
  const { t } = useTranslation()
  const [expiresIn, setExpiresIn] = useState(7 * 24 * 60 * 60)
  const [sharing, setSharing] = useState(false)
  const [result, setResult] = useState<ArtifactShareResult | null>(null)
  const [shares, setShares] = useState<ArtifactShareSummary[]>([])
  const [error, setError] = useState('')

  const loadShares = useCallback(async () => {
    try {
      setShares(await api.artifactShares(taskId, record.id))
    } catch {
      setShares([])
    }
  }, [record.id, taskId])

  useEffect(() => { void loadShares() }, [loadShares])

  async function createShare() {
    if (sharing) return
    setSharing(true)
    setError('')
    try {
      const next = await api.createArtifactShare(taskId, record.id, expiresIn)
      setResult(next)
      await loadShares()
    } catch {
      setError(t('artifacts.shareDialog.createError'))
    } finally {
      setSharing(false)
    }
  }

  async function revoke(shareID: string) {
    setError('')
    try {
      await api.revokeArtifactShare(taskId, record.id, shareID)
      setShares((current) => current.filter((share) => share.share_id !== shareID))
    } catch {
      setError(t('artifacts.shareDialog.revokeError'))
    }
  }

  async function copyLink() {
    if (!result) return
    try {
      await navigator.clipboard.writeText(result.url)
    } catch {
      const input = document.querySelector<HTMLInputElement>('[data-artifact-share-url]')
      input?.select()
      document.execCommand('copy')
    }
  }

  return (
    <div className="fixed inset-0 z-[var(--z-modal)] grid place-items-center bg-[var(--backdrop)] p-4 backdrop-blur-[6px]" role="dialog" aria-modal="true" aria-label={t('artifacts.shareDialog.title')}>
      <div className="flex max-h-[min(680px,90vh)] w-full max-w-lg flex-col overflow-hidden rounded-[var(--radius-xl)] border border-[var(--color-border)] bg-[var(--color-surface)] shadow-[var(--shadow-xl)]">
        <header className="flex h-12 shrink-0 items-center gap-3 border-b border-[var(--color-border)] px-4">
          <LockClosedIcon className="h-4 w-4 text-[var(--color-accent-neutral)]" />
          <h2 className="min-w-0 flex-1 truncate text-sm font-semibold">{t('artifacts.shareDialog.title')}</h2>
          <button type="button" aria-label={t('common.close')} onClick={onClose} className="artifact-action"><XMarkIcon className="h-4 w-4" /></button>
        </header>
        <div className="min-h-0 flex-1 space-y-4 overflow-auto p-4">
          <p className="text-xs leading-5 text-[var(--color-muted-foreground)]">{t('artifacts.shareDialog.privacy')}</p>
          {!result ? (
            <div className="space-y-3">
              <label className="block text-xs font-medium text-[var(--color-foreground)]">
                {t('artifacts.shareDialog.expiry')}
                <select value={expiresIn} onChange={(event) => setExpiresIn(Number(event.target.value))} className="mt-1.5 block h-9 w-full rounded-[var(--radius-md)] border border-[var(--color-border)] bg-[var(--color-background)] px-2.5 text-sm">
                  <option value={3600}>{t('artifacts.shareDialog.oneHour')}</option>
                  <option value={86400}>{t('artifacts.shareDialog.oneDay')}</option>
                  <option value={604800}>{t('artifacts.shareDialog.sevenDays')}</option>
                  <option value={2592000}>{t('artifacts.shareDialog.thirtyDays')}</option>
                </select>
              </label>
              <button type="button" disabled={sharing} onClick={() => void createShare()} className="inline-flex h-9 items-center gap-2 rounded-[var(--radius-md)] bg-[var(--color-accent-neutral)] px-3 text-sm font-medium text-[var(--color-background)] disabled:opacity-60">
                <CloudArrowUpIcon className="h-4 w-4" />{sharing ? t('artifacts.shareDialog.creating') : t('artifacts.shareDialog.create')}
              </button>
            </div>
          ) : (
            <div className="space-y-2 rounded-[var(--radius-lg)] border border-[var(--color-border)] p-3">
              <p className="text-xs font-medium text-[var(--color-foreground)]">{t('artifacts.shareDialog.ready')}</p>
              <input data-artifact-share-url readOnly aria-label={t('artifacts.shareDialog.link')} value={result.url} className="h-9 w-full rounded-[var(--radius-md)] border border-[var(--color-border)] bg-[var(--color-background)] px-2.5 font-mono text-xs" />
              <div className="flex gap-2">
                <button type="button" onClick={() => void copyLink()} className="inline-flex h-8 items-center gap-1.5 rounded-[var(--radius-md)] border border-[var(--color-border)] px-2.5 text-xs"><ClipboardDocumentIcon className="h-4 w-4" />{t('artifacts.shareDialog.copy')}</button>
                <a href={result.url} target="_blank" rel="noreferrer" className="inline-flex h-8 items-center gap-1.5 rounded-[var(--radius-md)] border border-[var(--color-border)] px-2.5 text-xs"><ArrowTopRightOnSquareIcon className="h-4 w-4" />{t('artifacts.shareDialog.open')}</a>
              </div>
              <p className="text-[11px] leading-4 text-[var(--color-muted-foreground)]">{t('artifacts.shareDialog.keyWarning')}</p>
            </div>
          )}
          {error && <p role="alert" className="text-xs text-[var(--color-error-fg)]">{error}</p>}
          {shares.filter((share) => !share.revoked_at && share.state !== 'revoked').length > 0 && (
            <section>
              <h3 className="mb-2 text-xs font-medium">{t('artifacts.shareDialog.active')}</h3>
              <div className="space-y-1.5">
                {shares.filter((share) => !share.revoked_at && share.state !== 'revoked').map((share) => (
                  <div key={share.share_id} className="flex items-center gap-2 rounded-[var(--radius-md)] border border-[var(--color-border)] px-2.5 py-2">
                    <span className="min-w-0 flex-1 truncate font-mono text-[11px] text-[var(--color-muted-foreground)]">r{share.revision} · {new Date(share.expires_at).toLocaleString()}</span>
                    <button type="button" aria-label={t('artifacts.shareDialog.revoke')} onClick={() => void revoke(share.share_id)} className="artifact-action"><TrashIcon className="h-4 w-4" /></button>
                  </div>
                ))}
              </div>
            </section>
          )}
        </div>
      </div>
    </div>
  )
}

function ArtifactViewer({ taskId, record }: { taskId: string; record: ArtifactRecord }) {
  const { t } = useTranslation()
  const [blob, setBlob] = useState<Blob | null>(null)
  const [error, setError] = useState('')
  useEffect(() => {
    let active = true
    setBlob(null)
    setError('')
    if (record.status !== 'available') return () => { active = false }
    void api.artifactContent(taskId, record.id).then((next) => { if (active) setBlob(next) }).catch(() => { if (active) setError(t('artifacts.contentError')) })
    return () => { active = false }
  }, [record.id, record.status, taskId, t])

  const [text, setText] = useState('')
  const [textLoaded, setTextLoaded] = useState(false)
  useEffect(() => {
    let active = true
    setText('')
    setTextLoaded(false)
    if (!blob || !['text', 'markdown', 'code', 'html', 'csv'].includes(record.kind)) return
    void readBlobText(blob).then((value) => {
      if (active) {
        setText(value)
        setTextLoaded(true)
      }
    }).catch(() => { if (active) setError(t('artifacts.contentError')) })
    return () => { active = false }
  }, [blob, record.kind, t])

  const objectURL = useMemo(() => blob && ['image', 'pdf', 'binary'].includes(record.kind) ? URL.createObjectURL(blob) : '', [blob, record.kind])
  useEffect(() => () => { if (objectURL) URL.revokeObjectURL(objectURL) }, [objectURL])

  if (record.status !== 'available') return <ViewerState title={t(`artifacts.status.${record.status}`)} />
  if (error) return <ViewerState title={error} retry={() => window.dispatchEvent(new Event('jcode:artifact-upserted'))} />
  if (!blob || (['text', 'markdown', 'code', 'html', 'csv'].includes(record.kind) && !textLoaded)) return <ViewerState title={t('artifacts.loading')} />
  if (record.kind === 'html') return <iframe title={record.title} sandbox="allow-scripts" srcDoc={`<meta http-equiv="Content-Security-Policy" content="${HTML_CSP}">${text}`} className="h-full min-h-[420px] w-full rounded-[var(--radius-lg)] border border-[var(--color-border)] bg-[var(--color-background)]" />
  if (record.kind === 'markdown') {
    const html = DOMPurify.sanitize(marked.parse(escapeHTML(text)) as string)
    return <article className="artifact-markdown" dangerouslySetInnerHTML={{ __html: html }} />
  }
  if (record.kind === 'csv') return <CSVTable source={text} />
  if (record.kind === 'text' || record.kind === 'code') return <pre className="m-0 whitespace-pre-wrap break-words font-mono text-xs leading-6 text-[var(--color-foreground)]">{text}</pre>
  if (record.kind === 'image' && objectURL) return <img src={objectURL} alt={record.title} className="mx-auto block max-h-[72vh] max-w-full object-contain" />
  if (record.kind === 'pdf' && objectURL) return <iframe title={record.title} src={objectURL} className="h-full min-h-[520px] w-full rounded-[var(--radius-lg)] border border-[var(--color-border)]" />
  return <ViewerState title={t('artifacts.noPreview')} />
}

function CSVTable({ source }: { source: string }) {
  const rows = useMemo(() => parseCSV(source), [source])
  return <div className="overflow-auto rounded-[var(--radius-lg)] border border-[var(--color-border)]"><table className="min-w-full border-collapse text-left text-xs"><tbody>{rows.map((row, rowIndex) => <tr key={rowIndex} className={rowIndex === 0 ? 'bg-[var(--color-muted)] font-medium' : ''}>{row.map((cell, columnIndex) => <td key={columnIndex} className="max-w-[280px] border-b border-r border-[var(--color-border)] px-2.5 py-2 align-top">{cell}</td>)}</tr>)}</tbody></table></div>
}

function ViewerState({ title, retry }: { title: string; retry?: () => void }) {
  const { t } = useTranslation()
  return <div className="grid min-h-[280px] place-items-center text-center"><div><p className="text-sm text-[var(--color-muted-foreground)]">{title}</p>{retry && <button type="button" onClick={retry} className="mt-3 inline-flex items-center gap-1.5 text-xs text-[var(--color-accent-neutral)]"><ArrowPathIcon className="h-4 w-4" />{t('common.retry')}</button>}</div></div>
}
