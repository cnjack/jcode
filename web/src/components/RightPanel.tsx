/**
 * RightPanel — temporary workspace overlay with three context views
 * (Plan / Files / Changes). It floats above the current task so opening it
 * never resizes the conversation or the bottom terminal.
 *
 * Ported from web/src/components/RightPanel.vue. The Vue version imports three
 * child components (FileTreePanel.vue / DiffViewer.vue) that
 * don't exist in React yet; per the migration brief they are implemented INLINE
 * here as simple functional sub-components so the panel is functional now.
 *
 * Files and Changes fetch their own data on mount (and are keyed on the project path so a
 * project switch remounts + re-fetches them — see the key prop in render).
 */

import { useCallback, useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import {
  ArrowsRightLeftIcon,
  ChevronLeftIcon,
  ChevronRightIcon,
  ClipboardDocumentCheckIcon,
  DocumentIcon,
  FolderIcon,
  FolderOpenIcon,
  XMarkIcon,
} from '@heroicons/react/24/outline'
import { useAppSelector } from '../app/hooks'
import { api } from '../lib/api'
import type { DiffResponse, FileItem } from '../lib/types'
import { CurrentPlanPane } from './StatusPanel'

type Tab = 'plan' | 'files' | 'changes'

interface Props {
  activeTab: Tab
  onClose: () => void
  onSwitchTab: (tab: Tab) => void
}

/** i18n keys for the tab strip (resolved with t() at render). */
const TAB_KEYS: Record<Tab, string> = {
  plan: 'rightPanel.plan',
  files: 'rightPanel.files',
  changes: 'rightPanel.changes',
} as const

const TAB_ICONS = {
  plan: ClipboardDocumentCheckIcon,
  files: FolderOpenIcon,
  changes: ArrowsRightLeftIcon,
} as const

export function RightPanel({ activeTab, onClose, onSwitchTab }: Props) {
  const { t } = useTranslation()
  const projectPath = useAppSelector((s) => s.session.projectPath)

  return (
    <>
      <button
        type="button"
        data-testid="workspace-backdrop"
        aria-label={t('rightPanel.closeWorkspace')}
        onClick={onClose}
        className="absolute inset-0 z-[44] cursor-default bg-[color-mix(in_srgb,var(--color-foreground)_16%,transparent)] backdrop-blur-[2px]"
      />
      <section
        role="dialog"
        aria-modal="true"
        aria-label={t('rightPanel.workspace')}
        data-testid="workspace-panel"
        className="absolute bottom-6 left-1/2 top-16 z-[45] grid w-[min(1080px,calc(100%_-_48px))] -translate-x-1/2 grid-cols-[168px_minmax(0,1fr)] grid-rows-[52px_minmax(0,1fr)] overflow-hidden rounded-[var(--radius-xl)] border border-[var(--color-border)] bg-[var(--color-surface)] shadow-[var(--shadow-xl)]"
      >
        <header className="col-span-2 flex min-w-0 items-center gap-2 border-b border-[var(--color-border)] px-4">
          <h2 className="shrink-0 text-[13px] font-semibold text-[var(--color-foreground)]">
            {t('rightPanel.workspace')}
          </h2>
          <span className="min-w-0 flex-1 truncate text-[11px] text-[var(--color-muted-foreground)]">
            {t('rightPanel.workspaceSummary')}
          </span>
          <button
            type="button"
            onClick={onClose}
            aria-label={t('rightPanel.closeWorkspace')}
            className="grid h-7 w-7 shrink-0 place-items-center rounded-[var(--radius-md)] text-[var(--color-muted-foreground)] transition-colors hover:bg-[var(--color-muted)] hover:text-[var(--color-foreground)]"
          >
            <XMarkIcon className="h-4 w-4" />
          </button>
        </header>

        <nav aria-label={t('rightPanel.workspaceNavigation')} className="min-h-0 border-r border-[var(--color-border)] p-3">
          <div className="space-y-1">
            {(['plan', 'files', 'changes'] as const).map((tab) => {
              const TabIcon = TAB_ICONS[tab]
              return (
                <button
                  key={tab}
                  type="button"
                  onClick={() => onSwitchTab(tab)}
                  aria-current={activeTab === tab ? 'page' : undefined}
                  className={`flex w-full items-center gap-2.5 rounded-[var(--radius-lg)] px-3 py-2.5 text-left text-[12px] font-medium transition-colors ${
                    activeTab === tab
                      ? 'bg-[var(--color-muted)] text-[var(--color-foreground)]'
                      : 'text-[var(--color-muted-foreground)] hover:bg-[var(--color-background)] hover:text-[var(--color-foreground)]'
                  }`}
                >
                  <TabIcon className="h-4 w-4 shrink-0" />
                  {t(TAB_KEYS[tab])}
                </button>
              )
            })}
          </div>
        </nav>

        <div className="min-h-0 min-w-0 overflow-hidden bg-[var(--color-background)]">
          {/* Keyed on the project path so switching projects remounts the children
              and re-fetches — both only load on mount. */}
          {activeTab === 'plan' ? (
            <div className="h-full overflow-y-auto px-3 pt-4"><CurrentPlanPane /></div>
          ) : activeTab === 'files' ? (
            <FileTreePanel key={`files:${projectPath}`} />
          ) : (
            <DiffViewer key={`changes:${projectPath}`} />
          )}
        </div>
      </section>
    </>
  )
}

// ═══════════════════════════════════════════════════════════════════════════
// FileTreePanel — inline port of web/src/components/FileTreePanel.vue (the
// navigable directory browser: breadcrumb + listing + file preview). Kept
// functional but simpler than the Vue original's highlight.js preview.
// ═══════════════════════════════════════════════════════════════════════════

function FileTreePanel() {
  const { t } = useTranslation()
  const [items, setItems] = useState<FileItem[]>([])
  const [currentPath, setCurrentPath] = useState('')
  const [loading, setLoading] = useState(false)
  const [dirError, setDirError] = useState('')
  const [fileError, setFileError] = useState('')
  const [previewFile, setPreviewFile] = useState<{ path: string; content: string } | null>(null)

  const fetchDir = useCallback(async (path: string) => {
    setLoading(true)
    setDirError('')
    setFileError('')
    try {
      const result = await api.files(path || undefined)
      setItems(result)
      setCurrentPath(path)
    } catch (err) {
      console.error('Failed to fetch files:', err)
      setItems([])
      setDirError(t('rightPanel.dirError'))
    } finally {
      setLoading(false)
    }
  }, [t])

  // Initial load of the workspace root (the parent keys this component on the
  // project path, so a project switch remounts + re-fetches).
  useEffect(() => {
    void fetchDir('')
  }, [fetchDir])

  const breadcrumbs = currentPath ? currentPath.split('/').filter(Boolean) : []

  async function openItem(item: FileItem) {
    if (item.is_dir) {
      const newPath = currentPath ? `${currentPath}/${item.name}` : item.name
      await fetchDir(newPath)
    } else {
      const filePath = currentPath ? `${currentPath}/${item.name}` : item.name
      setFileError('')
      try {
        const result = await api.fileContent(filePath)
        setPreviewFile(result)
      } catch (err) {
        console.error('Failed to fetch file content:', err)
        setFileError(t('rightPanel.fileError', { name: item.name }))
      }
    }
  }

  function navigateTo(index: number) {
    setPreviewFile(null)
    if (index < 0) {
      void fetchDir('')
    } else {
      void fetchDir(breadcrumbs.slice(0, index + 1).join('/'))
    }
  }

  function goBack() {
    if (previewFile) {
      setPreviewFile(null)
      return
    }
    if (breadcrumbs.length > 0) {
      void fetchDir(breadcrumbs.slice(0, -1).join('/'))
    }
  }

  function formatSize(bytes: number): string {
    if (bytes < 1024) return `${bytes} B`
    if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
    return `${(bytes / (1024 * 1024)).toFixed(1)} MB`
  }

  const canGoBack = breadcrumbs.length > 0 || !!previewFile

  return (
    <div className="flex h-full flex-col overflow-hidden">
      <div className="flex min-h-[36px] items-center gap-1.5 border-b border-[var(--color-border)] px-3 py-2">
        {canGoBack && (
          <button
            type="button"
            onClick={goBack}
            className="flex h-[22px] w-[22px] shrink-0 items-center justify-center rounded-[var(--radius-sm)] text-[var(--color-muted-foreground)] transition-colors hover:bg-[var(--color-muted)] hover:text-[var(--color-foreground)]"
          >
            <ChevronLeftIcon className="h-3.5 w-3.5" />
          </button>
        )}
        <div className="flex min-w-0 flex-1 items-center gap-0.5 overflow-hidden">
          <button
            type="button"
            onClick={() => navigateTo(-1)}
            className="whitespace-nowrap rounded-[var(--radius-xs)] bg-transparent px-1 py-0.5 text-[11px] text-[var(--color-muted-foreground)] hover:bg-[var(--color-muted)] hover:text-[var(--color-foreground)]"
          >
            root
          </button>
          {breadcrumbs.map((seg, i) => (
            <span key={i} className="flex items-center gap-0.5">
              <ChevronRightIcon className="h-2.5 w-2.5 shrink-0 opacity-50 text-[var(--color-muted-foreground)]" />
              <button
                type="button"
                onClick={() => navigateTo(i)}
                className="whitespace-nowrap rounded-[var(--radius-xs)] bg-transparent px-1 py-0.5 text-[11px] text-[var(--color-muted-foreground)] hover:bg-[var(--color-muted)] hover:text-[var(--color-foreground)]"
              >
                {seg}
              </button>
            </span>
          ))}
          {previewFile && (
            <span className="flex items-center gap-0.5">
              <ChevronRightIcon className="h-2.5 w-2.5 shrink-0 opacity-50 text-[var(--color-muted-foreground)]" />
              <span className="whitespace-nowrap px-1 py-0.5 text-[11px] text-[var(--color-foreground)]">
                {previewFile.path.split('/').pop()}
              </span>
            </span>
          )}
        </div>
      </div>

      {previewFile ? (
        <div className="min-h-0 flex-1 overflow-auto p-3">
          <pre
            className="m-0 whitespace-pre-wrap break-all font-mono text-[11px] leading-relaxed"
            style={{ fontFamily: 'var(--font-mono)' }}
          >
            {previewFile.content}
          </pre>
        </div>
      ) : (
        <div className="min-h-0 flex-1 overflow-y-auto py-1">
          {loading ? (
            <div className="px-3 py-6 text-center text-xs text-[var(--color-muted-foreground)]">{t('common.loading')}</div>
          ) : dirError ? (
            <div className="px-3 py-6 text-center text-xs text-[var(--color-error-fg)]">{dirError}</div>
          ) : (
            <>
              {fileError && (
                <div className="mx-2 my-1.5 rounded-[var(--radius-md)] border border-[var(--color-error-fg)] bg-[var(--color-error-bg)] px-2 py-1.5 text-[11px] leading-tight text-[var(--color-error-fg)]">
                  {fileError}
                </div>
              )}
              {items.map((item) => (
                <button
                  key={item.name}
                  type="button"
                  onClick={() => openItem(item)}
                  className="flex w-full items-center gap-2 px-3 py-1.5 text-left transition-colors hover:bg-[var(--color-muted)]"
                >
                  {item.is_dir ? (
                    <FolderIcon className="h-3.5 w-3.5 shrink-0 text-[var(--color-accent-neutral)]" />
                  ) : (
                    <DocumentIcon className="h-3.5 w-3.5 shrink-0 text-[var(--color-muted-foreground)]" />
                  )}
                  <span className="min-w-0 flex-1 truncate text-xs text-[var(--color-foreground)]">{item.name}</span>
                  {!item.is_dir && (
                    <span className="shrink-0 font-mono text-[10px] text-[var(--color-muted-foreground)]">
                      {formatSize(item.size)}
                    </span>
                  )}
                </button>
              ))}
              {items.length === 0 && (
                <div className="px-3 py-6 text-center text-xs text-[var(--color-muted-foreground)]">{t('rightPanel.emptyDir')}</div>
              )}
            </>
          )}
        </div>
      )}
    </div>
  )
}

// ═══════════════════════════════════════════════════════════════════════════
// DiffViewer — inline port of web/src/components/DiffViewer.vue (file list +
// per-file patch view). The default mode is 'working' per the migration brief.
// ═══════════════════════════════════════════════════════════════════════════

type DiffMode = 'working' | 'staged' | 'branch' | 'session'

const DIFF_MODES: { value: DiffMode; labelKey: string }[] = [
  { value: 'session', labelKey: 'diff.modes.session' },
  { value: 'working', labelKey: 'diff.modes.working' },
  { value: 'staged', labelKey: 'diff.modes.staged' },
  { value: 'branch', labelKey: 'diff.modes.branch' },
]

interface ParsedLine {
  text: string
  type: 'add' | 'del' | 'hunk' | 'ctx'
}

function parsePatchLines(patch: string): ParsedLine[] {
  return patch.split('\n').map((line) => {
    if (line.startsWith('+') && !line.startsWith('+++')) return { text: line, type: 'add' }
    if (line.startsWith('-') && !line.startsWith('---')) return { text: line, type: 'del' }
    if (line.startsWith('@@')) return { text: line, type: 'hunk' }
    return { text: line, type: 'ctx' }
  })
}

function statusBadge(status: string): { label: string; bg: string; fg: string } {
  switch (status) {
    case 'A':
      return { label: 'A', bg: 'var(--color-success-bg)', fg: 'var(--color-success-fg)' }
    case 'D':
      return { label: 'D', bg: 'var(--color-error-bg)', fg: 'var(--color-error-fg)' }
    default:
      return { label: 'M', bg: 'var(--color-warning-bg)', fg: 'var(--color-warning-fg)' }
  }
}

const LINE_STYLE: Record<ParsedLine['type'], string> = {
  add: 'bg-[var(--color-success-bg)] border-l-[var(--color-success-fg)] text-[var(--color-success-fg)]',
  del: 'bg-[var(--color-error-bg)] border-l-[var(--color-error-fg)] text-[var(--color-error-fg)]',
  hunk: 'bg-[var(--color-info-bg)] border-l-[var(--color-info-fg)] text-[var(--color-info-fg)]',
  ctx: 'border-transparent text-[var(--color-muted-foreground)]',
}

function DiffViewer() {
  const { t } = useTranslation()
  const [entries, setEntries] = useState<DiffResponse['entries']>([])
  const [mode, setMode] = useState<DiffMode>('working')
  const [loading, setLoading] = useState(false)
  const [selectedFile, setSelectedFile] = useState<string | null>(null)
  const [error, setError] = useState('')

  const fetchDiff = useCallback(async (m: DiffMode) => {
    setLoading(true)
    setError('')
    try {
      const result = await api.diff(m)
      setEntries(result.entries)
      // Keep the current selection only if it still exists in the new set;
      // otherwise fall back to the first entry.
      const stillThere = selectedFile && result.entries.some((e) => e.file === selectedFile)
      if (!stillThere) {
        setSelectedFile(result.entries.length > 0 ? result.entries[0]?.file ?? null : null)
      }
    } catch (err) {
      console.error('Failed to fetch diff:', err)
      setEntries([])
      setError(t('diff.loadError'))
    } finally {
      setLoading(false)
    }
    // selectedFile is intentionally read from state inside the callback rather
    // than captured, so we don't re-run fetch when the selection changes.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [t])

  useEffect(() => {
    void fetchDiff(mode)
  }, [mode, fetchDiff])

  const selectedEntry = entries.find((e) => e.file === selectedFile) ?? null
  const totalAdditions = entries.reduce((sum, e) => sum + e.additions, 0)
  const totalDeletions = entries.reduce((sum, e) => sum + e.deletions, 0)

  return (
    <div className="flex h-full flex-col bg-[var(--color-background)]">
      {/* Header */}
      <div className="flex shrink-0 items-center justify-between border-b border-[var(--color-border)] bg-[var(--color-sidebar-bg)] px-3 py-1.5">
        <div className="flex items-center gap-2">
          <span className="text-[11px] font-semibold uppercase tracking-wider text-[var(--color-muted-foreground)]">
            {t('diff.changes')}
          </span>
          <div className="flex gap-0.5">
            {DIFF_MODES.map((m) => (
              <button
                key={m.value}
                type="button"
                onClick={() => setMode(m.value)}
                className={`rounded px-1.5 py-0.5 text-[10px] font-medium transition-colors ${
                  mode === m.value
                    ? 'bg-[var(--neutral-wash)] text-[var(--color-accent-neutral)]'
                    : 'text-[var(--color-muted-foreground)] hover:bg-[var(--color-muted)] hover:text-[var(--color-foreground)]'
                }`}
              >
                {t(m.labelKey)}
              </button>
            ))}
          </div>
        </div>
        <div className="flex items-center gap-2">
          {(totalAdditions > 0 || totalDeletions > 0) && (
            <span className="font-mono text-[10px]">
              <span style={{ color: 'var(--color-success-fg)' }}>+{totalAdditions}</span>
              <span className="mx-0.5 text-[var(--color-border)]">/</span>
              <span style={{ color: 'var(--color-error-fg)' }}>-{totalDeletions}</span>
            </span>
          )}
          <button
            type="button"
            onClick={() => void fetchDiff(mode)}
            className="cursor-pointer text-[10px] font-medium text-[var(--color-muted-foreground)] transition-colors hover:text-[var(--color-foreground)]"
          >
            {t('diff.refresh')}
          </button>
        </div>
      </div>

      {error && (
        <div className="shrink-0 px-3 py-2 text-center text-[11px] text-[var(--color-error-fg)]">{error}</div>
      )}

      <div className="flex min-h-0 flex-1 flex-col">
        {/* File list */}
        <div className="max-h-[30%] shrink-0 overflow-y-auto border-b border-[var(--color-border)]">
          {entries.length === 0 && !loading && (
            <div className="py-6 text-center text-[11px] text-[var(--color-muted-foreground)]">{t('diff.noChanges')}</div>
          )}
          {loading && (
            <div className="animate-pulse py-6 text-center text-[11px] text-[var(--color-muted-foreground)]">
              {t('common.loading')}
            </div>
          )}
          {entries.map((entry) => {
            const badge = statusBadge(entry.status)
            return (
              <button
                key={entry.file}
                type="button"
                onClick={() => setSelectedFile(entry.file)}
                className={`flex w-full cursor-pointer items-center gap-1.5 px-2 py-1.5 text-left transition-colors ${
                  selectedFile === entry.file
                    ? 'bg-[var(--neutral-wash-soft)] text-[var(--color-foreground)]'
                    : 'text-[var(--color-muted-foreground)] hover:bg-[var(--color-muted)]'
                }`}
              >
                <span
                  className="shrink-0 rounded px-1 py-px text-[9px] font-bold"
                  style={{ backgroundColor: badge.bg, color: badge.fg }}
                >
                  {badge.label}
                </span>
                <span className="truncate font-mono text-[11px]">{entry.file.split('/').pop()}</span>
                <span className="ml-auto shrink-0 font-mono text-[9px]">
                  <span style={{ color: 'var(--color-success-fg)' }}>+{entry.additions}</span>
                  <span className="ml-0.5" style={{ color: 'var(--color-error-fg)' }}>
                    -{entry.deletions}
                  </span>
                </span>
              </button>
            )
          })}
        </div>

        {/* Diff content */}
        <div className="min-h-0 flex-1 overflow-auto">
          {!selectedEntry ? (
            <div className="py-8 text-center text-[11px] text-[var(--color-muted-foreground)]">
              Select a file to view changes
            </div>
          ) : (
            <div>
              <div className="border-b border-[var(--color-border)] bg-[var(--color-muted)] px-3 py-1.5">
                <span className="font-mono text-[11px] text-[var(--color-foreground)]">{selectedEntry.file}</span>
                <span className="ml-2 font-mono text-[10px]">
                  <span style={{ color: 'var(--color-success-fg)' }}>+{selectedEntry.additions}</span>
                  <span className="mx-0.5 text-[var(--color-border)]">/</span>
                  <span style={{ color: 'var(--color-error-fg)' }}>-{selectedEntry.deletions}</span>
                </span>
              </div>
              <div className="font-mono text-[11px] leading-5" style={{ fontFamily: 'var(--font-mono)' }}>
                {parsePatchLines(selectedEntry.patch).map((line, i) => (
                  <div key={i} className={`border-l-2 px-3 ${LINE_STYLE[line.type]}`}>
                    <pre className="whitespace-pre-wrap">{line.text}</pre>
                  </div>
                ))}
              </div>
            </div>
          )}
        </div>
      </div>
    </div>
  )
}
