import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import {
  ArrowLeftIcon,
  CheckIcon,
  ChevronDownIcon,
  FolderIcon,
  FolderOpenIcon,
  MagnifyingGlassIcon,
  PlusIcon,
  ServerIcon,
} from '@heroicons/react/24/outline'
import { useTranslation } from 'react-i18next'
import { useAppDispatch, useAppSelector } from '../app/hooks'
import { chatActions, loadWorkspaceState, sessionActions } from '../app/store'
import { api } from '../lib/api'

interface WorkspaceNode {
  path: string
  name: string
  remote: boolean
}

interface BrowseFolder {
  name: string
  path: string
}

export function WorkspacePicker({ placement = 'top' }: { placement?: 'top' | 'bottom' }) {
  const { t } = useTranslation()
  const dispatch = useAppDispatch()
  const activePath = useAppSelector((s) => s.session.projectPath)
  const tasks = useAppSelector((s) => s.session.tasks)
  const isRunning = useAppSelector((s) => s.chat.isRunning)
  const [open, setOpen] = useState(false)
  const [query, setQuery] = useState('')
  const [missing, setMissing] = useState<Set<string>>(new Set())
  const [browserOpen, setBrowserOpen] = useState(false)
  const [browsePath, setBrowsePath] = useState('')
  const [pathInput, setPathInput] = useState('')
  const [folders, setFolders] = useState<BrowseFolder[]>([])
  const [loadingFolders, setLoadingFolders] = useState(false)
  const [switching, setSwitching] = useState(false)
  const [error, setError] = useState('')
  const rootRef = useRef<HTMLDivElement | null>(null)

  const workspaces = useMemo<WorkspaceNode[]>(() => {
    const map = new Map<string, WorkspaceNode>()
    if (activePath) map.set(activePath, { path: activePath, name: workspaceName(activePath), remote: isRemotePath(activePath) })
    for (const task of tasks) {
      if (!task.project || map.has(task.project)) continue
      map.set(task.project, { path: task.project, name: workspaceName(task.project), remote: isRemotePath(task.project) })
    }
    const q = query.trim().toLowerCase()
    return [...map.values()]
      .filter((node) => node.remote || !missing.has(node.path))
      .filter((node) => !q || node.name.toLowerCase().includes(q) || node.path.toLowerCase().includes(q))
      .sort((a, b) => {
        if (a.path === activePath) return -1
        if (b.path === activePath) return 1
        return a.name.localeCompare(b.name)
      })
  }, [activePath, tasks, missing, query])

  const activeName = activePath ? workspaceName(activePath) : t('workspace.none')
  const activeRemote = isRemotePath(activePath)

  const validateKnownPaths = useCallback(async () => {
    const localPaths = [...new Set(tasks.map((t) => t.project).filter((p) => p && !isRemotePath(p)))]
    if (activePath && !isRemotePath(activePath)) localPaths.push(activePath)
    if (localPaths.length === 0) return
    try {
      const res = await api.validatePaths([...new Set(localPaths)])
      setMissing(new Set(res.missing || []))
    } catch {
      // The picker remains usable; failed validation should not hide paths.
    }
  }, [activePath, tasks])

  useEffect(() => {
    void validateKnownPaths()
  }, [validateKnownPaths])

  useEffect(() => {
    function onOpenPicker() {
      setOpen(true)
      void validateKnownPaths()
    }
    window.addEventListener('jcode:open-workspace-picker', onOpenPicker)
    return () => window.removeEventListener('jcode:open-workspace-picker', onOpenPicker)
  }, [validateKnownPaths])

  useEffect(() => {
    if (!open) return
    function onDown(e: MouseEvent) {
      if (rootRef.current && !rootRef.current.contains(e.target as Node)) reset()
    }
    function onKey(e: KeyboardEvent) {
      if (e.key === 'Escape') reset()
    }
    document.addEventListener('mousedown', onDown)
    document.addEventListener('keydown', onKey)
    return () => {
      document.removeEventListener('mousedown', onDown)
      document.removeEventListener('keydown', onKey)
    }
  }, [open])

  async function loadFolders(path?: string) {
    setLoadingFolders(true)
    setError('')
    try {
      const res = await api.browse(path)
      setBrowsePath(res.current)
      setPathInput(res.current)
      setFolders(res.folders || [])
      setBrowserOpen(true)
    } catch (e) {
      setError(e instanceof Error ? e.message : t('workspace.openError'))
    } finally {
      setLoadingFolders(false)
    }
  }

  async function switchTo(path: string) {
    if (!path) return
    setSwitching(true)
    setError('')
    try {
      if (isRemotePath(path)) {
        setError(t('errors.remoteReconnect'))
        return
      }
      if (path === activePath) {
        const resp = await api.newSession()
        dispatch(chatActions.clearChat())
        dispatch(sessionActions.setCurrentSession(resp.session_id))
        await dispatch(loadWorkspaceState())
        reset()
        return
      }
      const resp = await api.switchProject(path)
      dispatch(chatActions.clearChat())
      dispatch(sessionActions.setCurrentSession(''))
      dispatch(sessionActions.setProjectPath(resp.pwd || path))
      await dispatch(loadWorkspaceState())
      reset()
    } catch (e) {
      setError(e instanceof Error ? e.message : t('workspace.openError'))
    } finally {
      setSwitching(false)
    }
  }

  function reset() {
    setOpen(false)
    setQuery('')
    setBrowserOpen(false)
    setBrowsePath('')
    setPathInput('')
    setFolders([])
    setError('')
  }

  function goUp() {
    if (!browsePath || browsePath === '/') return
    const parent = browsePath.replace(/\/+$/, '').split('/').slice(0, -1).join('/') || '/'
    void loadFolders(parent)
  }

  return (
    <div ref={rootRef} className="relative inline-flex min-w-0">
      <button
        type="button"
        disabled={isRunning || switching}
        title={activePath}
        onClick={(e) => {
          e.stopPropagation()
          setOpen((v) => {
            if (!v) void validateKnownPaths()
            return !v
          })
        }}
        className="inline-flex h-7 max-w-[200px] items-center gap-1.5 rounded-[var(--radius-lg)] px-2 text-xs text-[var(--color-muted-foreground)] transition-colors hover:bg-[var(--color-muted)] hover:text-[var(--color-foreground)] disabled:opacity-55"
      >
        {activeRemote ? <ServerIcon className="h-3.5 w-3.5 shrink-0" /> : <FolderOpenIcon className="h-3.5 w-3.5 shrink-0" />}
        <span className="truncate">{activeName}</span>
        <ChevronDownIcon className={`h-3 w-3 shrink-0 opacity-70 transition-transform ${open ? 'rotate-180' : ''}`} />
      </button>

      {open && (
        <div className={`absolute left-0 z-[var(--z-dropdown)] w-[320px] max-w-[86vw] rounded-[var(--radius-xl)] border border-[var(--color-border)] bg-[var(--color-surface)] p-1.5 shadow-[var(--shadow-lg)] ${
          placement === 'bottom' ? 'top-full mt-1' : 'bottom-full mb-1'
        }`}>
          {browserOpen ? (
            <div className="flex flex-col gap-1.5">
              <div className="flex items-center gap-1.5">
                <button type="button" onClick={() => setBrowserOpen(false)} className="grid h-7 w-7 place-items-center rounded-[var(--radius-md)] text-[var(--color-muted-foreground)] hover:bg-[var(--color-muted)]">
                  <ArrowLeftIcon className="h-3.5 w-3.5" />
                </button>
                <input
                  value={pathInput}
                  onChange={(e) => setPathInput(e.target.value)}
                  onKeyDown={(e) => {
                    if (e.key === 'Enter') void loadFolders(pathInput)
                  }}
                  placeholder={t('projectSwitcher.pathPlaceholder')}
                  className="min-w-0 flex-1 rounded-[var(--radius-md)] border border-[var(--color-border)] bg-[var(--color-background)] px-2 py-1.5 font-mono text-xs text-[var(--color-foreground)] outline-none"
                />
              </div>
              <div className="max-h-60 overflow-y-auto">
                {browsePath && browsePath !== '/' && (
                  <button type="button" onClick={goUp} className="flex w-full items-center gap-2 rounded-[var(--radius-md)] px-2 py-1.5 text-left text-xs text-[var(--color-foreground)] hover:bg-[var(--color-muted)]">
                    <span className="w-4 text-center font-mono">..</span>
                  </button>
                )}
                {loadingFolders ? (
                  <div className="px-2 py-4 text-xs text-[var(--color-muted-foreground)]">{t('workspace.loading')}</div>
                ) : folders.length === 0 ? (
                  <div className="px-2 py-4 text-xs text-[var(--color-muted-foreground)]">{t('workspace.noFolders')}</div>
                ) : (
                  folders.map((folder) => (
                    <button key={folder.path} type="button" onClick={() => void loadFolders(folder.path)} className="flex w-full items-center gap-2 rounded-[var(--radius-md)] px-2 py-1.5 text-left text-xs text-[var(--color-foreground)] hover:bg-[var(--color-muted)]">
                      <FolderIcon className="h-3.5 w-3.5 shrink-0 text-[var(--color-muted-foreground)]" />
                      <span className="min-w-0 flex-1 truncate">{folder.name}</span>
                    </button>
                  ))
                )}
              </div>
              {error && <div className="px-2 text-[11px] text-[var(--color-destructive)]">{error}</div>}
              <div className="flex items-center gap-2 border-t border-[var(--color-border)] pt-1.5">
                <span className="min-w-0 flex-1 truncate font-mono text-[10.5px] text-[var(--color-muted-foreground)]">{browsePath || '~'}</span>
                <button type="button" disabled={!browsePath || switching} onClick={() => void switchTo(browsePath)} className="rounded-[var(--radius-md)] bg-[var(--color-primary)] px-2 py-1.5 text-xs font-medium text-[var(--color-on-primary)] disabled:opacity-50">
                  {t('workspace.open')}
                </button>
              </div>
            </div>
          ) : (
            <>
              <div className="flex items-center gap-2 rounded-[var(--radius-md)] border border-[var(--color-border)] bg-[var(--color-background)] px-2 py-1.5">
                <MagnifyingGlassIcon className="h-3.5 w-3.5 text-[var(--color-muted-foreground)]" />
                <input value={query} onChange={(e) => setQuery(e.target.value)} placeholder={t('workspace.search')} className="min-w-0 flex-1 border-none bg-transparent text-[12.5px] text-[var(--color-foreground)] outline-none" />
              </div>
              <div className="max-h-64 overflow-y-auto py-1">
                {workspaces.length === 0 ? (
                  <div className="px-2 py-4 text-xs text-[var(--color-muted-foreground)]">{t('workspace.nonePlural')}</div>
                ) : (
                  workspaces.map((node) => {
                    const active = node.path === activePath
                    return (
                      <button key={node.path} type="button" disabled={switching} onClick={() => void switchTo(node.path)} className={`flex w-full items-center gap-2 rounded-[var(--radius-md)] px-2 py-1.5 text-left text-xs transition-colors hover:bg-[var(--color-muted)] disabled:opacity-55 ${active ? 'font-semibold text-[var(--color-primary)]' : 'text-[var(--color-foreground)]'}`}>
                        {node.remote ? <ServerIcon className="h-3.5 w-3.5 shrink-0" /> : <FolderIcon className="h-3.5 w-3.5 shrink-0" />}
                        <span className="min-w-0 flex-1">
                          <span className="block truncate">{node.name}</span>
                          <span className="block truncate font-mono text-[10px] font-normal text-[var(--color-muted-foreground)]">{node.path}</span>
                        </span>
                        {active && <CheckIcon className="h-3.5 w-3.5 shrink-0" />}
                      </button>
                    )
                  })
                )}
              </div>
              {error && <div className="px-2 py-1 text-[11px] text-[var(--color-destructive)]">{error}</div>}
              <div className="flex gap-1.5 border-t border-[var(--color-border)] pt-1.5">
                <button type="button" onClick={() => void loadFolders(activePath || undefined)} className="flex flex-1 items-center gap-1.5 rounded-[var(--radius-md)] px-2 py-1.5 text-xs text-[var(--color-foreground)] hover:bg-[var(--color-muted)]">
                  <PlusIcon className="h-3.5 w-3.5" />
                  {t('workspace.openFolder')}
                </button>
                <button type="button" onClick={() => window.dispatchEvent(new Event('jcode:open-remote-connect'))} className="flex items-center gap-1.5 rounded-[var(--radius-md)] px-2 py-1.5 text-xs text-[var(--color-foreground)] hover:bg-[var(--color-muted)]">
                  <ServerIcon className="h-3.5 w-3.5" />
                  {t('nav.remoteConnect')}
                </button>
              </div>
            </>
          )}
        </div>
      )}
    </div>
  )
}

function isRemotePath(path: string): boolean {
  return path.startsWith('ssh://') || path.startsWith('docker://')
}

function workspaceName(path: string): string {
  if (!path) return ''
  if (path.startsWith('docker://')) {
    const rest = path.slice('docker://'.length)
    return rest.split('/')[0] || path
  }
  if (path.startsWith('ssh://')) {
    const rest = path.slice('ssh://'.length)
    const slash = rest.indexOf('/')
    const host = slash >= 0 ? rest.slice(0, slash) : rest
    const tail = slash >= 0 ? rest.slice(slash).split('/').filter(Boolean).at(-1) : ''
    return tail ? `${tail} (${host})` : host
  }
  const parts = path.split('/').filter(Boolean)
  return parts.at(-1) || path
}
