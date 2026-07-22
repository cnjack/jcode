/**
 * WorkspacePicker — project/workspace switcher pill for the composer header.
 *
 * Ported from the jcode web product UI. All backend/store side effects go
 * through `ProductComposerHost` (validateWorkspacePaths / browseFolders /
 * switchWorkspace / pickFolder / openRemoteConnect); `isRunning` comes from
 * the ChatRuntime like every other composer surface.
 */

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
import { useRuntimeState } from 'jcode-ui-core/runtime'
import type { ProductComposerHost } from './host.js'
import { useComposerStrings } from './useComposerStrings.js'
import { isRemotePath, parseRemoteLabel, workspaceName } from './remote.js'

interface WorkspaceNode {
  path: string
  name: string
  remote: boolean
}

interface BrowseFolder {
  name: string
  path: string
}

export function WorkspacePicker({ host, placement = 'top' }: { host: ProductComposerHost; placement?: 'top' | 'bottom' }) {
  const strings = useComposerStrings(host)
  const activePath = host.projectPath
  const tasks = host.tasks
  const { isRunning } = useRuntimeState()
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

  const activeName = activePath ? workspaceName(activePath) : strings.workspaceNone
  const activeRemote = isRemotePath(activePath)

  const validateKnownPaths = useCallback(async () => {
    const localPaths = [...new Set(tasks.map((t) => t.project).filter((p) => p && !isRemotePath(p)))]
    if (activePath && !isRemotePath(activePath)) localPaths.push(activePath)
    if (localPaths.length === 0) return
    try {
      const missingPaths = await host.validateWorkspacePaths([...new Set(localPaths)])
      setMissing(new Set(missingPaths || []))
    } catch {
      // The picker remains usable; failed validation should not hide paths.
    }
  }, [activePath, tasks, host])

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
      const res = await host.browseFolders(path)
      setBrowsePath(res.current)
      setPathInput(res.current)
      setFolders(res.folders || [])
      setBrowserOpen(true)
    } catch (e) {
      setError(e instanceof Error ? e.message : strings.workspaceOpenError)
    } finally {
      setLoadingFolders(false)
    }
  }

  async function switchTo(path: string) {
    if (!path) return
    setSwitching(true)
    setError('')
    try {
      // Remote workspaces must reconnect through the SSH/Docker wizard.
      if (isRemotePath(path)) {
        const meta = parseRemoteLabel(path)
        reset()
        host.openRemoteConnect?.(meta ?? undefined)
        return
      }
      await host.switchWorkspace(path)
      reset()
    } catch (e) {
      setError(e instanceof Error ? e.message : strings.workspaceOpenError)
    } finally {
      setSwitching(false)
    }
  }

  /** "Open folder": native picker on desktop, in-app browser in web. */
  async function openFolderAction() {
    if (host.pickFolder) {
      try {
        const path = await host.pickFolder(activePath && !isRemotePath(activePath) ? activePath : undefined)
        if (path) await switchTo(path)
        return
      } catch {
        // Native picker unavailable → fall through to in-app browser.
      }
    }
    void loadFolders(activePath && !isRemotePath(activePath) ? activePath : undefined)
  }

  function openRemote() {
    reset()
    host.openRemoteConnect?.()
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
    <div ref={rootRef} className={`ws-bar${open ? ' is-open' : ''}`} style={{ position: 'relative' }}>
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
        className="ws-pill ws-pill-action"
      >
        {activeRemote
          ? <ServerIcon className="h-3.5 w-3.5 ws-pill-icon" />
          : <FolderOpenIcon className="h-3.5 w-3.5 ws-pill-icon" />}
        <span className="ws-name">{activeName}</span>
        <ChevronDownIcon className={`h-3 w-3 ws-caret${open ? ' open' : ''}`} />
      </button>

      {open && (
        <div className={`ws-panel ${placement === 'bottom' ? 'place-bottom' : 'place-top'}`}>
          {browserOpen ? (
            <div className="ws-browser">
              <div className="ws-browser-head">
                <button type="button" className="ws-back" onClick={() => setBrowserOpen(false)}>
                  <ArrowLeftIcon className="h-3.5 w-3.5" />
                </button>
                <input
                  value={pathInput}
                  onChange={(e) => setPathInput(e.target.value)}
                  onKeyDown={(e) => {
                    if (e.key === 'Enter') void loadFolders(pathInput)
                  }}
                  placeholder={strings.workspacePathPlaceholder}
                  className="ws-path-input"
                />
              </div>
              <div className="ws-list">
                {browsePath && browsePath !== '/' && (
                  <button type="button" className="ws-row ws-folder" onClick={goUp}>
                    <span className="ws-folder-icon">..</span>
                  </button>
                )}
                {loadingFolders ? (
                  <div className="ws-hint">{strings.workspaceLoading}</div>
                ) : folders.length === 0 ? (
                  <div className="ws-hint">{strings.workspaceNoFolders}</div>
                ) : (
                  folders.map((folder) => (
                    <button key={folder.path} type="button" className="ws-row ws-folder" onClick={() => void loadFolders(folder.path)}>
                      <FolderIcon className="h-3.5 w-3.5 ws-folder-icon" />
                      <span className="ws-row-name">{folder.name}</span>
                    </button>
                  ))
                )}
              </div>
              {error && <div className="ws-error">{error}</div>}
              <div className="ws-browser-foot">
                <span className="ws-cur-path">{browsePath || '~'}</span>
                <button type="button" className="ws-open-btn" disabled={!browsePath || switching} onClick={() => void switchTo(browsePath)}>
                  {strings.workspaceOpen}
                </button>
              </div>
            </div>
          ) : (
            <div className="ws-listview">
              <div className="ws-search">
                <MagnifyingGlassIcon className="h-3 w-3 ws-search-icon" />
                <input value={query} onChange={(e) => setQuery(e.target.value)} placeholder={strings.workspaceSearch} className="ws-search-input" />
              </div>
              <div className="ws-list">
                {workspaces.length === 0 ? (
                  <div className="ws-hint">{strings.workspaceNonePlural}</div>
                ) : (
                  workspaces.map((node) => {
                    const active = node.path === activePath
                    return (
                      <button
                        key={node.path}
                        type="button"
                        disabled={switching}
                        onClick={() => void switchTo(node.path)}
                        className={`ws-row${active ? ' active' : ''}`}
                      >
                        {node.remote
                          ? <ServerIcon className="h-3.5 w-3.5 ws-row-icon" />
                          : <FolderIcon className="h-3.5 w-3.5 ws-row-icon" />}
                        <span className="ws-row-name">{node.name}</span>
                        {active && <CheckIcon className="h-3.5 w-3.5 ws-check" />}
                      </button>
                    )
                  })
                )}
              </div>
              {error && <div className="ws-error">{error}</div>}
              <div className="ws-actions">
                <button type="button" className="ws-action" onClick={() => void openFolderAction()}>
                  <PlusIcon className="h-3.5 w-3.5" />
                  <span>{strings.workspaceOpenFolder}</span>
                </button>
                {host.openRemoteConnect && <button type="button" className="ws-action" onClick={openRemote}>
                  <ServerIcon className="h-3.5 w-3.5" />
                  <span>{strings.remoteConnect}</span>
                </button>}
              </div>
            </div>
          )}
        </div>
      )}
      <style>{WS_CSS}</style>
    </div>
  )
}

/** Ported from web/src/components/WorkspacePicker.vue scoped styles. */
const WS_CSS = `
.ws-bar {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  min-width: 0;
}
.ws-bar.is-open {
  /* Cloud elevates the later composer toolbar. Raise the whole open picker
     one semantic layer so toolbar controls cannot paint through its panel. */
  z-index: calc(var(--z-dropdown, 40) + 1);
}
.ws-pill {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  height: 28px;
  padding: 0 9px;
  border: 1px solid transparent;
  border-radius: var(--radius-lg);
  font-size: 12.5px;
  color: var(--color-foreground);
  min-width: 0;
}
.ws-pill-icon { flex-shrink: 0; }
.ws-pill-action {
  max-width: 230px;
  background: transparent;
  cursor: pointer;
  transition: background 0.15s, transform 0.06s ease;
}
.ws-pill-action .ws-pill-icon { color: var(--color-accent-neutral); }
.ws-pill-action:hover:not(:disabled) { background: var(--color-muted); }
.ws-pill-action:active:not(:disabled) { transform: translateY(0.5px); }
.ws-pill-action:disabled { opacity: 0.55; cursor: not-allowed; }
.ws-name {
  font-weight: 500;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.ws-caret {
  color: var(--color-muted-foreground);
  flex-shrink: 0;
  margin-left: 1px;
  transition: transform 0.15s;
}
.ws-caret.open { transform: rotate(180deg); }
.ws-panel {
  position: absolute;
  left: 0;
  z-index: var(--z-dropdown, 40);
  width: 320px;
  max-width: 84vw;
  max-height: min(54vh, 360px);
  display: flex;
  flex-direction: column;
  padding: 6px;
  background: var(--color-surface);
  border: 1px solid var(--color-border);
  border-radius: var(--radius-xl);
  box-shadow: var(--shadow-lg);
}
.ws-panel.place-top { bottom: calc(100% + 6px); }
.ws-panel.place-bottom { top: calc(100% + 6px); }
.ws-browser, .ws-listview {
  display: flex;
  flex-direction: column;
  min-height: 0;
  flex: 1 1 auto;
}
.ws-search, .ws-browser-head {
  display: flex;
  align-items: center;
  gap: 6px;
  flex-shrink: 0;
  padding: 5px 8px;
  margin-bottom: 4px;
  border: 1px solid var(--color-border);
  border-radius: var(--radius-lg);
  background: var(--color-background);
}
.ws-search-icon { color: var(--color-muted-foreground); flex-shrink: 0; }
.ws-search-input, .ws-path-input {
  flex: 1;
  min-width: 0;
  border: none;
  outline: none;
  background: transparent;
  font-size: 12.5px;
  color: var(--color-foreground);
}
.ws-path-input { font-family: var(--font-mono); font-size: 11.5px; }
.ws-search-input::placeholder, .ws-path-input::placeholder { color: var(--color-muted-foreground); }
.ws-back {
  display: grid;
  place-items: center;
  border: none;
  background: transparent;
  color: var(--color-muted-foreground);
  cursor: pointer;
  flex-shrink: 0;
}
.ws-back:hover { color: var(--color-foreground); }
.ws-list {
  flex: 1 1 auto;
  min-height: 0;
  overflow-y: auto;
  padding: 2px;
}
.ws-row {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 100%;
  padding: 7px 8px;
  border: none;
  background: transparent;
  border-radius: var(--radius-md);
  cursor: pointer;
  text-align: left;
  color: var(--color-foreground);
  font-size: 12.5px;
  transition: background 0.12s;
}
.ws-row:hover { background: var(--color-muted); }
.ws-row.active { background: var(--neutral-wash-soft, var(--color-muted)); }
.ws-row:disabled { opacity: 0.55; cursor: not-allowed; }
.ws-row-icon, .ws-folder-icon {
  color: var(--color-muted-foreground);
  flex-shrink: 0;
}
.ws-row.active .ws-row-icon { color: var(--color-accent-neutral); }
.ws-row-name {
  flex: 1;
  min-width: 0;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.ws-folder .ws-folder-icon {
  font-family: var(--font-mono);
  font-size: 12px;
}
.ws-check { color: var(--color-accent-neutral); flex-shrink: 0; }
.ws-hint {
  padding: 14px 8px;
  text-align: center;
  font-size: 11.5px;
  color: var(--color-muted-foreground);
}
.ws-error {
  padding: 4px 8px;
  font-size: 11px;
  color: var(--color-destructive, #b91c1c);
  flex-shrink: 0;
}
.ws-actions {
  margin-top: 4px;
  padding-top: 4px;
  border-top: 1px solid var(--color-border);
  display: flex;
  flex-direction: column;
  flex-shrink: 0;
  gap: 2px;
}
.ws-action {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 100%;
  padding: 8px;
  border: none;
  background: transparent;
  border-radius: var(--radius-md);
  cursor: pointer;
  font-size: 12.5px;
  color: var(--color-foreground);
  transition: background 0.12s;
}
.ws-action:hover { background: var(--color-muted); }
.ws-action svg { color: var(--color-muted-foreground); flex-shrink: 0; }
.ws-browser-foot {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-shrink: 0;
  margin-top: 4px;
  padding-top: 6px;
  border-top: 1px solid var(--color-border);
}
.ws-cur-path {
  flex: 1;
  min-width: 0;
  font-family: var(--font-mono);
  font-size: 11px;
  color: var(--color-muted-foreground);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.ws-open-btn {
  flex-shrink: 0;
  padding: 5px 12px;
  border: none;
  border-radius: var(--radius-md);
  background: var(--color-accent-neutral);
  color: var(--color-surface);
  font-size: 12px;
  font-weight: 500;
  cursor: pointer;
}
.ws-open-btn:disabled { opacity: 0.5; cursor: not-allowed; }
.ws-open-btn:hover:not(:disabled) { opacity: 0.9; }
`
