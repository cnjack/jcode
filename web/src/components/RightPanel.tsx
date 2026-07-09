/**
 * RightPanel — right-side panel (width ~20rem, full height, border-left) with
 * three tabs (Plan / Files / Changes) and a close button.
 *
 * Ported from web/src/components/RightPanel.vue. The Vue version imports three
 * child components (FileTreePanel.vue / DiffViewer.vue / TaskList.vue) that
 * don't exist in React yet; per the migration brief they are implemented INLINE
 * here as simple functional sub-components so the panel is functional now.
 *
 * The Plan tab reads `todos` from the Redux chat slice. The Files and Changes
 * tabs fetch their own data on mount (and are keyed on the project path so a
 * project switch remounts + re-fetches them — see the key prop in render).
 */

import { useCallback, useEffect, useState } from 'react'
import {
  ArrowPathIcon,
  CheckCircleIcon,
  ChevronLeftIcon,
  ChevronRightIcon,
  DocumentIcon,
  EllipsisHorizontalCircleIcon,
  FolderIcon,
  MinusCircleIcon,
  NoSymbolIcon,
  XMarkIcon,
} from '@heroicons/react/24/outline'
import { useAppSelector } from '../app/hooks'
import { api } from '../lib/api'
import type { DiffResponse, FileItem, TodoItem } from '../lib/types'

type Tab = 'files' | 'changes' | 'plan'

interface Props {
  activeTab: Tab
  onClose: () => void
  onSwitchTab: (tab: Tab) => void
}

const TAB_LABELS: Record<Tab, string> = {
  plan: 'Plan',
  files: 'Files',
  changes: 'Changes',
} as const

export function RightPanel({ activeTab, onClose, onSwitchTab }: Props) {
  // Panel width with a min/max clamp; resized via the left drag handle.
  const [panelWidth, setPanelWidth] = useState(320)
  const projectPath = useAppSelector((s) => s.session.projectPath)

  function startResize(e: React.MouseEvent) {
    e.preventDefault()
    const startX = e.clientX
    const startWidth = panelWidth

    function onMove(ev: MouseEvent) {
      // Dragging left grows the panel (the handle is on the left edge).
      const dx = startX - ev.clientX
      setPanelWidth(Math.min(600, Math.max(220, startWidth + dx)))
    }
    function onUp() {
      window.removeEventListener('mousemove', onMove)
      window.removeEventListener('mouseup', onUp)
    }
    window.addEventListener('mousemove', onMove)
    window.addEventListener('mouseup', onUp)
  }

  return (
    <aside
      className="relative flex h-full min-w-[220px] max-w-[600px] flex-col overflow-hidden border-l border-[var(--color-border)] bg-[var(--color-background)]"
      style={{ width: `${panelWidth}px` }}
    >
      {/* Resize handle (left edge). */}
      <div
        onMouseDown={startResize}
        className="absolute bottom-0 left-0 top-0 z-10 w-1 cursor-col-resize transition-colors hover:bg-[color-mix(in_srgb,var(--color-accent-neutral)_40%,transparent)]"
      />

      {/* Header: tab strip + close. */}
      <div className="flex h-10 shrink-0 items-center justify-between border-b border-[var(--color-border)] pl-3 pr-2">
        <div className="flex items-center gap-0.5">
          {(['plan', 'files', 'changes'] as const).map((t) => (
            <button
              key={t}
              type="button"
              onClick={() => onSwitchTab(t)}
              className={`rounded-[var(--radius-md)] px-2.5 py-1 text-xs font-medium transition-colors ${
                activeTab === t
                  ? 'bg-[var(--color-muted)] text-[var(--color-foreground)]'
                  : 'text-[var(--color-muted-foreground)] hover:text-[var(--color-foreground)]'
              }`}
            >
              {TAB_LABELS[t]}
            </button>
          ))}
        </div>
        <button
          type="button"
          onClick={onClose}
          aria-label="Close panel"
          className="flex h-6 w-6 items-center justify-center rounded-[var(--radius-sm)] text-[var(--color-muted-foreground)] transition-colors hover:bg-[var(--color-muted)] hover:text-[var(--color-foreground)]"
        >
          <XMarkIcon className="h-3.5 w-3.5" />
        </button>
      </div>

      <div className="min-h-0 flex-1 overflow-hidden">
        {/* Keyed on the project path so switching projects remounts the children
            and re-fetches — both only load on mount. */}
        {activeTab === 'files' ? (
          <FileTreePanel key={`files:${projectPath}`} />
        ) : activeTab === 'changes' ? (
          <DiffViewer key={`changes:${projectPath}`} />
        ) : (
          <PlanPane />
        )}
      </div>
    </aside>
  )
}

// ═══════════════════════════════════════════════════════════════════════════
// Plan pane — todos from the Redux store + a progress bar.
// ═══════════════════════════════════════════════════════════════════════════

function PlanPane() {
  const todos = useAppSelector((s) => s.chat.todos)
  const total = todos.length
  const completed = todos.filter((t) => t.status === 'completed').length
  const progressPct = total ? Math.round((completed / total) * 100) : 0

  return (
    <div className="flex h-full flex-col">
      <div className="flex shrink-0 items-center gap-2.5 border-b border-[var(--color-border)] px-3 py-2.5">
        <span className="text-xs font-medium text-[var(--color-foreground)]">Plan</span>
        <div className="h-[5px] flex-1 overflow-hidden rounded-[var(--radius-pill)] bg-[var(--color-border)]">
          <div
            className="h-full rounded-[var(--radius-pill)] bg-[var(--color-accent-neutral)] transition-[width] duration-[var(--duration-slow)] ease-out"
            style={{ width: `${progressPct}%` }}
          />
        </div>
        <span className="whitespace-nowrap font-mono text-[11px] text-[var(--color-muted-foreground)]">
          {completed} / {total}
        </span>
      </div>
      <div className="min-h-0 flex-1 overflow-y-auto p-1.5">
        {total > 0 ? <TaskList todos={todos} /> : <div className="px-2 py-4 text-center text-[13px] text-[var(--color-muted-foreground)]">No tasks yet</div>}
      </div>
    </div>
  )
}

// ═══════════════════════════════════════════════════════════════════════════
// TaskList — inline port of web/src/components/TaskList.vue. Status icon +
// title per todo, with a left accent bar for the in-progress task.
// ═══════════════════════════════════════════════════════════════════════════

function TaskList({ todos }: { todos: TodoItem[] }) {
  // Respect prefers-reduced-motion: swap the spinning ArrowPathIcon for a static
  // EllipsisHorizontalCircleIcon so the spin actually stops.
  const [reduceMotion, setReduceMotion] = useState(false)
  useEffect(() => {
    if (typeof window === 'undefined' || !window.matchMedia) return
    const mql = window.matchMedia('(prefers-reduced-motion: reduce)')
    const sync = (e: MediaQueryListEvent) => setReduceMotion(e.matches)
    setReduceMotion(mql.matches)
    mql.addEventListener('change', sync)
    return () => mql.removeEventListener('change', sync)
  }, [])

  return (
    <div className="space-y-0.5">
      {todos.map((todo) => {
        const done = todo.status === 'completed' || todo.status === 'cancelled'
        const inProgress = todo.status === 'in_progress'
        return (
          <div
            key={todo.id}
            className={`relative flex items-center gap-2 py-1 pl-2 pr-1.5 ${inProgress ? 'rounded-[var(--radius-md)]' : ''}`}
            style={inProgress ? { backgroundColor: 'var(--color-warning-bg)' } : undefined}
          >
            {/* 2px left accent bar for the active task (inner span, not a
                single-sided border — matches the Vue layout). */}
            {inProgress && (
              <span
                aria-hidden="true"
                className="absolute left-0 top-0.5 bottom-0.5 w-0.5 rounded-[var(--radius-pill)]"
                style={{ backgroundColor: 'var(--color-accent-neutral)' }}
              />
            )}

            {/* Status icon */}
            {todo.status === 'completed' ? (
              <CheckCircleIcon className="h-3.5 w-3.5 shrink-0" style={{ color: 'var(--color-success-fg)' }} />
            ) : todo.status === 'cancelled' ? (
              <NoSymbolIcon className="h-3.5 w-3.5 shrink-0" style={{ color: 'var(--color-destructive)' }} />
            ) : todo.status === 'in_progress' ? (
              reduceMotion ? (
                <EllipsisHorizontalCircleIcon className="h-3.5 w-3.5 shrink-0" style={{ color: 'var(--color-accent-neutral)' }} />
              ) : (
                <ArrowPathIcon
                  className="h-3.5 w-3.5 shrink-0 animate-spin"
                  style={{ color: 'var(--color-accent-neutral)' }}
                />
              )
            ) : (
              <MinusCircleIcon className="h-3.5 w-3.5 shrink-0" style={{ color: 'var(--color-muted-foreground)' }} />
            )}

            <span
              className={`min-w-0 flex-1 truncate text-xs sm:text-sm ${inProgress ? 'font-medium' : ''}`}
              style={{
                color: done ? 'var(--color-muted-foreground)' : 'var(--color-foreground)',
                textDecoration: done ? 'line-through' : 'none',
              }}
            >
              {todo.title}
            </span>
          </div>
        )
      })}
    </div>
  )
}

// ═══════════════════════════════════════════════════════════════════════════
// FileTreePanel — inline port of web/src/components/FileTreePanel.vue (the
// navigable directory browser: breadcrumb + listing + file preview). Kept
// functional but simpler than the Vue original's highlight.js preview.
// ═══════════════════════════════════════════════════════════════════════════

function FileTreePanel() {
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
      setDirError("Couldn't load this directory.")
    } finally {
      setLoading(false)
    }
  }, [])

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
        setFileError(`Couldn't open ${item.name} (it may be a binary file, too large, or unreadable).`)
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
            <div className="px-3 py-6 text-center text-xs text-[var(--color-muted-foreground)]">Loading…</div>
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
                <div className="px-3 py-6 text-center text-xs text-[var(--color-muted-foreground)]">Empty directory</div>
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

const DIFF_MODES: { value: DiffMode; label: string }[] = [
  { value: 'session', label: 'Session' },
  { value: 'working', label: 'Working' },
  { value: 'staged', label: 'Staged' },
  { value: 'branch', label: 'Branch' },
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
      setError('Failed to load changes')
    } finally {
      setLoading(false)
    }
    // selectedFile is intentionally read from state inside the callback rather
    // than captured, so we don't re-run fetch when the selection changes.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

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
            Changes
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
                {m.label}
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
            ↻ Refresh
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
            <div className="py-6 text-center text-[11px] text-[var(--color-muted-foreground)]">No changes</div>
          )}
          {loading && (
            <div className="animate-pulse py-6 text-center text-[11px] text-[var(--color-muted-foreground)]">
              Loading...
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
