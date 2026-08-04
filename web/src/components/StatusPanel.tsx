import { useCallback, useEffect, useState, type ReactNode } from 'react'
import { createPortal } from 'react-dom'
import { useTranslation } from 'react-i18next'
import {
  ArrowDownTrayIcon,
  ArrowPathIcon,
  CheckCircleIcon,
  ChevronDownIcon,
  ChevronUpIcon,
  ClipboardDocumentCheckIcon,
  ClockIcon,
  DocumentDuplicateIcon,
  EllipsisHorizontalCircleIcon,
  ExclamationTriangleIcon,
  MinusCircleIcon,
  NoSymbolIcon,
  XMarkIcon,
} from '@heroicons/react/24/outline'
import { useAppSelector } from '../app/hooks'
import { api } from '../lib/api'
import type { ArtifactRecord, PlanHistoryEntry, TodoItem } from '../lib/types'
import { ArtifactsPanel } from './ArtifactsPanel'

interface Props {
  open: boolean
  isRunning: boolean
  bottomOffset?: number
  onOpen: () => void
  onClose: () => void
}

export function StatusPanel({ open, isRunning, bottomOffset = 18, onOpen, onClose }: Props) {
  const { t } = useTranslation()
  const todos = useAppSelector((state) => state.chat.todos)
  const planHistory = useAppSelector((state) => state.chat.planHistory)
  const currentSessionId = useAppSelector((state) => state.session.currentSessionId)
  const artifactTask = useAppSelector((state) => state.session.tasks.find((task) => task.uuid === currentSessionId))
  const artifactCount = artifactTask?.artifact_count ?? 0
  const activeTodo = todos.find((todo) => todo.status === 'in_progress') ?? todos.find((todo) => todo.status === 'pending')
  const latestCompletedTodo = [...todos].reverse().find((todo) => todo.status === 'completed' || todo.status === 'cancelled')
  const summaryTodo = activeTodo ?? latestCompletedTodo
  const summaryDone = summaryTodo?.status === 'completed'
  const summaryTitle = summaryTodo?.title || t('statusPanel.title')
  const summaryIconClass = `grid h-5 w-5 shrink-0 place-items-center rounded-[var(--radius-pill)] ${summaryDone ? 'bg-[var(--color-success-bg)] text-[var(--color-success-fg)]' : isRunning || activeTodo ? 'bg-[var(--accent-wash)] text-[var(--color-primary)]' : 'bg-[var(--color-muted)] text-[var(--color-muted-foreground)]'}`
  const hasContent = todos.length > 0 || planHistory.length > 0 || artifactCount > 0

  return (
    <section
      role={open ? 'dialog' : undefined}
      aria-label={t('statusPanel.title')}
      className={`absolute right-8 top-14 z-[44] flex w-[min(248px,calc(100%_-_64px))] flex-col overflow-hidden border border-[var(--color-border)] bg-[var(--color-surface)] shadow-[var(--shadow-md)] ${open ? 'rounded-[var(--radius-lg)]' : 'rounded-[var(--radius-pill)]'}`}
      style={open ? { maxHeight: `calc(100% - 56px - ${bottomOffset}px)` } : undefined}
    >
      <button
        type="button"
        onClick={open ? onClose : onOpen}
        aria-label={open ? t('statusPanel.collapse') : t('statusPanel.open')}
        title={open ? t('statusPanel.collapse') : t('statusPanel.open')}
        className={`flex h-10 w-full shrink-0 items-center gap-2 px-3 text-left transition-colors hover:bg-[var(--color-muted)] active:bg-[var(--color-background)] ${open ? 'border-b border-[var(--color-border)]' : ''}`}
      >
        <span className={summaryIconClass}>
          {summaryDone ? <CheckCircleIcon className="h-3.5 w-3.5" /> : <ClipboardDocumentCheckIcon className="h-3.5 w-3.5" />}
        </span>
        <span className="min-w-0 flex-1 truncate text-[12px] font-medium text-[var(--color-foreground)]">{summaryTitle}</span>
        {open ? <ChevronUpIcon className="h-3.5 w-3.5 text-[var(--color-muted-foreground)]" /> : <ChevronDownIcon className="h-3.5 w-3.5 text-[var(--color-muted-foreground)]" />}
      </button>

      {open && (
        <div className="min-h-0 flex-1 overflow-y-auto">
          {!hasContent ? (
            <div className="px-3 py-3 text-[11px] text-[var(--color-muted-foreground)]">{t('statusPanel.empty')}</div>
          ) : (
            <>
              <StatusSection icon={<ClipboardDocumentCheckIcon className="h-3.5 w-3.5" />} title={t('statusPanel.todos')} count={todos.length}>
                {todos.length > 0 && <CurrentPlanPane todos={todos} />}
              </StatusSection>
              <StatusSection icon={<ClockIcon className="h-3.5 w-3.5" />} title={t('statusPanel.plans')} count={planHistory.length}>
                {planHistory.length > 0 && <PlanHistoryPane history={planHistory} />}
              </StatusSection>
              <StatusSection icon={<DocumentDuplicateIcon className="h-3.5 w-3.5" />} title={t('statusPanel.artifacts')} count={artifactCount} last>
                {artifactCount > 0 && <CompactArtifactsPane taskId={currentSessionId} />}
              </StatusSection>
            </>
          )}
        </div>
      )}
    </section>
  )
}

function StatusSection({ icon, title, count, last = false, children }: { icon: ReactNode; title: string; count: number; last?: boolean; children: ReactNode }) {
  return (
    <section className={last ? '' : 'border-b border-[var(--color-border)]'}>
      <header className="flex h-9 items-center gap-2 px-3 text-[var(--color-muted-foreground)]">
        {icon}
        <h3 className="text-[12px] font-semibold text-[var(--color-foreground)]">{title}</h3>
        <span className="font-mono text-[10px]">{count}</span>
      </header>
      {children}
    </section>
  )
}

function CompactArtifactsPane({ taskId }: { taskId: string }) {
  const { t } = useTranslation()
  const [records, setRecords] = useState<ArtifactRecord[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [downloadingID, setDownloadingID] = useState('')
  const [previewID, setPreviewID] = useState('')

  const load = useCallback(async () => {
    if (!taskId) { setRecords([]); setLoading(false); return }
    setLoading(true)
    setError('')
    try {
      setRecords(await api.artifacts(taskId))
      await api.markArtifactsViewed(taskId).catch(() => undefined)
    } catch {
      setError(t('artifacts.loadError'))
    } finally {
      setLoading(false)
    }
  }, [taskId, t])

  useEffect(() => { void load() }, [load])
  useEffect(() => {
    const refresh = () => { void load() }
    window.addEventListener('jcode:artifact-upserted', refresh)
    return () => window.removeEventListener('jcode:artifact-upserted', refresh)
  }, [load])

  async function download(record: ArtifactRecord) {
    if (downloadingID) return
    setDownloadingID(record.id)
    setError('')
    try {
      const blob = await api.artifactDownload(taskId, record.id)
      const objectURL = URL.createObjectURL(blob)
      const anchor = document.createElement('a')
      anchor.href = objectURL
      anchor.download = record.relative_path.split('/').pop() || record.title
      document.body.appendChild(anchor)
      anchor.click()
      anchor.remove()
      window.setTimeout(() => URL.revokeObjectURL(objectURL), 0)
    } catch {
      setError(t('artifacts.downloadError'))
    } finally {
      setDownloadingID('')
    }
  }

  if (loading) return <CompactArtifactMessage>{t('common.loading')}</CompactArtifactMessage>
  if (error && records.length === 0) return <CompactArtifactMessage error>{error}</CompactArtifactMessage>
  if (records.length === 0) return <CompactArtifactMessage>{t('artifacts.empty')}</CompactArtifactMessage>

  return (
    <>
      <div className="border-t border-[var(--color-border)]">
        {records.map((record) => (
          <article key={record.id} className="flex min-w-0 items-center border-b border-[var(--color-border)] pr-2 last:border-b-0">
            <button
              type="button"
              disabled={record.status !== 'available'}
              onClick={() => setPreviewID(record.id)}
              aria-label={`${t('artifacts.preview')} ${record.title}`}
              className="flex min-w-0 flex-1 items-center gap-2 px-3 py-2 text-left transition-colors hover:bg-[var(--color-muted)] disabled:cursor-default"
            >
              <span className="grid h-7 w-7 shrink-0 place-items-center rounded-[var(--radius-md)] bg-[var(--color-muted)] text-[var(--color-muted-foreground)]">
                {record.status === 'available' ? <DocumentDuplicateIcon className="h-3.5 w-3.5" /> : <ExclamationTriangleIcon className="h-3.5 w-3.5 text-[var(--color-warning-fg)]" />}
              </span>
              <span className="min-w-0 flex-1">
                <span className="block truncate text-[11px] font-medium text-[var(--color-foreground)]">{record.title}</span>
                <span className="mt-0.5 block truncate font-mono text-[9px] text-[var(--color-muted-foreground)]">{record.kind} · {formatArtifactBytes(record.size)}</span>
              </span>
            </button>
            <button
              type="button"
              disabled={record.status !== 'available' || !!downloadingID}
              onClick={() => void download(record)}
              aria-label={`${t('artifacts.download')} ${record.title}`}
              title={t('artifacts.download')}
              className="grid h-7 w-7 shrink-0 place-items-center rounded-[var(--radius-md)] text-[var(--color-muted-foreground)] transition-colors hover:bg-[var(--color-muted)] hover:text-[var(--color-foreground)] disabled:opacity-40"
            >
              {downloadingID === record.id ? <ArrowPathIcon className="h-3.5 w-3.5 animate-spin" /> : <ArrowDownTrayIcon className="h-3.5 w-3.5" />}
            </button>
          </article>
        ))}
        {error && <div role="alert" className="px-3 py-2 text-[10px] text-[var(--color-error-fg)]">{error}</div>}
      </div>
      {previewID && createPortal(
        <div className="fixed inset-0 z-[var(--z-modal)] grid place-items-center p-6">
          <button type="button" aria-label={t('common.close')} onClick={() => setPreviewID('')} className="absolute inset-0 cursor-default bg-[var(--backdrop)] backdrop-blur-[6px]" />
          <section role="dialog" aria-modal="true" aria-label={t('artifacts.preview')} className="relative flex h-[min(760px,calc(100dvh_-_48px))] w-[min(1120px,calc(100vw_-_48px))] flex-col overflow-hidden rounded-[var(--radius-xl)] border border-[var(--color-border)] bg-[var(--color-surface)] shadow-[var(--shadow-xl)]">
            <header className="flex h-11 shrink-0 items-center gap-2 border-b border-[var(--color-border)] px-4">
              <DocumentDuplicateIcon className="h-4 w-4 text-[var(--color-accent-neutral)]" />
              <h2 className="min-w-0 flex-1 truncate text-[13px] font-semibold text-[var(--color-foreground)]">{t('statusPanel.artifacts')}</h2>
              <button type="button" aria-label={t('common.close')} onClick={() => setPreviewID('')} className="artifact-action"><XMarkIcon className="h-4 w-4" /></button>
            </header>
            <div className="min-h-0 flex-1"><ArtifactsPanel initialSelectedID={previewID} /></div>
          </section>
        </div>,
        document.body,
      )}
    </>
  )
}

function CompactArtifactMessage({ error = false, children }: { error?: boolean; children: ReactNode }) {
  return <div role={error ? 'alert' : undefined} className={`border-t border-[var(--color-border)] px-3 py-2 text-[10px] ${error ? 'text-[var(--color-error-fg)]' : 'text-[var(--color-muted-foreground)]'}`}>{children}</div>
}

function formatArtifactBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`
}

export function CurrentPlanPane({ todos }: { todos?: TodoItem[] }) {
  const { t } = useTranslation()
  const storeTodos = useAppSelector((state) => state.chat.todos)
  const items = todos ?? storeTodos
  const completed = items.filter((todo) => todo.status === 'completed' || todo.status === 'cancelled').length
  const progress = items.length > 0 ? Math.round((completed / items.length) * 100) : 0
  return (
    <div className="px-2 pb-3">
      <div className="flex items-center gap-3 px-2 pb-2">
        <span className="text-[11px] text-[var(--color-muted-foreground)]">{t('statusPanel.currentProgress')}</span>
        <div className="h-1.5 flex-1 overflow-hidden rounded-[var(--radius-pill)] bg-[var(--color-border)]">
          <div className="h-full rounded-[var(--radius-pill)] bg-[var(--color-accent-neutral)] transition-[width]" style={{ width: `${progress}%` }} />
        </div>
        <span className="font-mono text-[11px] text-[var(--color-success-fg)]">{completed} / {items.length}</span>
      </div>
      {items.length > 0 ? <TaskList todos={items} /> : <EmptyState>{t('rightPanel.noTasks')}</EmptyState>}
    </div>
  )
}

function PlanHistoryPane({ history }: { history: PlanHistoryEntry[] }) {
  const { t } = useTranslation()
  if (history.length === 0) return <EmptyState>{t('statusPanel.noPlans')}</EmptyState>
  return (
    <div className="space-y-1.5 px-3 pb-3">
      {[...history].reverse().map((entry, index) => <PlanHistoryRow key={entry.id} entry={entry} defaultOpen={index === 0} />)}
    </div>
  )
}

function PlanHistoryRow({ entry, defaultOpen }: { entry: PlanHistoryEntry; defaultOpen: boolean }) {
  const { t } = useTranslation()
  const hasDetails = !!entry.content || !!entry.feedback
  const summary = (
    <>
      <PlanStatusIcon status={entry.status} />
      <span className="min-w-0 flex-1">
        <span className="block truncate text-[11px] font-medium text-[var(--color-foreground)]">{entry.title || t('statusPanel.planFallback')}</span>
        <span className="mt-0.5 block truncate text-[9px] text-[var(--color-muted-foreground)]">
          {formatPlanTime(entry.timestamp)}{entry.todos.length > 0 ? ` · ${t('statusPanel.todoCount', { count: entry.todos.length })}` : ''}
        </span>
      </span>
    </>
  )

  if (!hasDetails) return <article className="flex items-center gap-2 rounded-[var(--radius-md)] bg-[var(--color-background)] px-2.5 py-2">{summary}</article>
  return (
    <details open={defaultOpen} className="group rounded-[var(--radius-md)] bg-[var(--color-background)]">
      <summary className="flex cursor-pointer list-none items-center gap-2 px-2.5 py-2">
        {summary}
        <ChevronDownIcon className="h-3 w-3 shrink-0 text-[var(--color-muted-foreground)] transition-transform group-open:rotate-180" />
      </summary>
      <div className="border-t border-[var(--color-border)] px-2.5 py-2">
        {entry.content && <pre className="m-0 whitespace-pre-wrap font-sans text-[11px] leading-relaxed text-[var(--color-foreground)]">{entry.content}</pre>}
        {entry.feedback && <p className="mt-2 text-[10px] text-[var(--color-error-fg)]">{entry.feedback}</p>}
      </div>
    </details>
  )
}

function TaskList({ todos, compact = false }: { todos: TodoItem[]; compact?: boolean }) {
  const [reduceMotion, setReduceMotion] = useState(false)
  useEffect(() => {
    if (typeof window === 'undefined' || !window.matchMedia) return
    const query = window.matchMedia('(prefers-reduced-motion: reduce)')
    const sync = (event: MediaQueryListEvent) => setReduceMotion(event.matches)
    setReduceMotion(query.matches)
    query.addEventListener('change', sync)
    return () => query.removeEventListener('change', sync)
  }, [])

  return (
    <div className="space-y-0.5">
      {todos.map((todo) => {
        const done = todo.status === 'completed' || todo.status === 'cancelled'
        const active = todo.status === 'in_progress'
        return (
          <div key={todo.id} className={`relative flex items-center gap-2 rounded-[var(--radius-md)] px-2 ${compact ? 'py-1' : 'py-2'} ${active ? 'bg-[var(--color-warning-bg)]' : ''}`}>
            {todo.status === 'completed' ? (
              <CheckCircleIcon className="h-4 w-4 shrink-0 text-[var(--color-success-fg)]" />
            ) : todo.status === 'cancelled' ? (
              <NoSymbolIcon className="h-4 w-4 shrink-0 text-[var(--color-destructive)]" />
            ) : active ? (
              reduceMotion ? <EllipsisHorizontalCircleIcon className="h-4 w-4 shrink-0 text-[var(--color-accent-neutral)]" /> : <ArrowPathIcon className="h-4 w-4 shrink-0 animate-spin text-[var(--color-accent-neutral)]" />
            ) : (
              <MinusCircleIcon className="h-4 w-4 shrink-0 text-[var(--color-muted-foreground)]" />
            )}
            <span className={`min-w-0 flex-1 text-[12px] ${active ? 'font-medium' : ''} ${done ? 'line-through text-[var(--color-muted-foreground)]' : 'text-[var(--color-foreground)]'}`}>
              {todo.title}
            </span>
          </div>
        )
      })}
    </div>
  )
}

function PlanStatusIcon({ status }: { status: string }) {
  if (status === 'completed' || status === 'approved') return <CheckCircleIcon className="h-4 w-4 shrink-0 text-[var(--color-success-fg)]" />
  if (status === 'rejected') return <NoSymbolIcon className="h-4 w-4 shrink-0 text-[var(--color-error-fg)]" />
  return <ClipboardDocumentCheckIcon className="h-4 w-4 shrink-0 text-[var(--color-accent-neutral)]" />
}

function EmptyState({ children }: { children: ReactNode }) {
  return <div className="grid min-h-20 place-items-center px-5 pb-4 text-center text-[12px] text-[var(--color-muted-foreground)]">{children}</div>
}

function formatPlanTime(timestamp: number): string {
  if (!timestamp || Number.isNaN(timestamp)) return '—'
  return new Date(timestamp).toLocaleString([], { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' })
}
