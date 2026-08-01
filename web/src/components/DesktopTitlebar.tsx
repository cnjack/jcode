/**
 * DesktopTitlebar — native-desktop task controls for the active task.
 *
 * macOS uses its otherwise-empty overlay titlebar band; Windows and Linux render
 * the same row immediately below their system titlebar. The component carries
 * task identity, branch/workspace context, per-session cloud sync, task actions,
 * installed app launchers, and the existing panel picker. Worktree/branch
 * mutation deliberately stays out of scope: the branch is informational only.
 */

import { useCallback, useEffect, useId, useMemo, useRef, useState } from 'react'
import {
  ArchiveBoxIcon,
  BookmarkIcon,
  ChevronDownIcon,
  CloudIcon,
  CodeBracketSquareIcon,
  CommandLineIcon,
  FolderOpenIcon,
  PencilSquareIcon,
} from '@heroicons/react/24/outline'
import type { ComponentType, RefObject, SVGProps } from 'react'
import { useTranslation } from 'react-i18next'
import { useAppDispatch, useAppSelector } from '../app/hooks'
import { sessionActions } from '../app/store'
import { api } from '../lib/api'
import { isRemotePath } from '../lib/remote'
import {
  listWorkspaceApplications,
  openWorkspaceInApplication,
  type WorkspaceApplication,
} from '../lib/useDesktop'
import { useCloudSessionSync } from './CloudSyncToggle'
import { TopBar } from './TopBar'

type PanelType = 'plan' | 'files' | 'changes' | 'artifacts' | 'terminal'
type OutlineIcon = ComponentType<SVGProps<SVGSVGElement>>

interface Props {
  isRunning: boolean
  wsConnected: boolean
  activePanel: 'none' | 'plan' | 'files' | 'changes' | 'artifacts' | 'terminal'
  terminalOpen: boolean
  onTogglePanel: (panel: PanelType) => void
}

export function DesktopTitlebar(props: Props) {
  const { t } = useTranslation()
  const dispatch = useAppDispatch()
  const currentSessionId = useAppSelector((s) => s.session.currentSessionId)
  const tasks = useAppSelector((s) => s.session.tasks)
  const sessions = useAppSelector((s) => s.session.sessions)
  const projectPath = useAppSelector((s) => s.session.projectPath)
  const currentProvider = useAppSelector((s) => s.model.providerName)
  const currentModel = useAppSelector((s) => s.model.modelName)

  const activeTask = useMemo(
    () => tasks.find((task) => task.uuid === currentSessionId),
    [currentSessionId, tasks],
  )
  const activeSession = useMemo(
    () => sessions.find((session) => session.uuid === currentSessionId),
    [currentSessionId, sessions],
  )
  const taskTitle = activeTask?.title?.trim() || activeSession?.title?.trim() || t('sidebar.untitled')
  const activeProject = activeTask?.project || projectPath
  const projectLabel = workspaceName(activeProject)
  const modelLabel = [activeTask?.provider || currentProvider, activeTask?.model || currentModel]
    .filter(Boolean)
    .join(' / ')

  const [branch, setBranch] = useState('')
  const branchRequest = useRef(0)
  const refreshBranch = useCallback(async () => {
    const request = ++branchRequest.current
    if (!activeProject) {
      setBranch('')
      return
    }
    try {
      const workspace = await api.workspace(currentSessionId)
      if (request === branchRequest.current) setBranch(workspace.branch)
    } catch {
      if (request === branchRequest.current) setBranch('')
    }
  }, [activeProject, currentSessionId])

  useEffect(() => {
    void refreshBranch()
    return () => {
      branchRequest.current++
    }
  }, [refreshBranch, currentSessionId])

  const wasRunning = useRef(props.isRunning)
  useEffect(() => {
    if (wasRunning.current && !props.isRunning) void refreshBranch()
    wasRunning.current = props.isRunning
  }, [props.isRunning, refreshBranch])

  return (
    <div
      data-testid="desktop-titlebar"
      style={{ left: 'calc(var(--sidebar-width, 20rem) + 10px)' }}
      className="pointer-events-none absolute right-[14px] top-[6px] z-[46] flex h-[34px] min-w-0 items-center gap-2"
    >
      <TaskDetails
        sessionId={currentSessionId}
        title={taskTitle}
        project={activeProject}
        projectLabel={projectLabel}
        branch={branch}
        model={modelLabel}
        pinned={!!activeTask?.pinned}
        archived={!!activeTask?.archived}
        running={props.isRunning}
        canEdit={!!activeTask}
        onPatch={async (patch) => {
          if (!currentSessionId) return
          await api.updateTask(currentSessionId, patch)
          dispatch(sessionActions.patchTask({ uuid: currentSessionId, ...patch }))
        }}
      />

      <div className="pointer-events-auto ml-auto flex shrink-0 items-center gap-1">
        <WorkspaceOpenMenu projectPath={activeProject} />
        <TopBar embedded {...props} />
      </div>
    </div>
  )
}

interface TaskDetailsProps {
  sessionId: string
  title: string
  project: string
  projectLabel: string
  branch: string
  model: string
  pinned: boolean
  archived: boolean
  running: boolean
  canEdit: boolean
  onPatch: (patch: { title?: string; pinned?: boolean; archived?: boolean }) => Promise<void>
}

function TaskDetails({
  sessionId,
  title,
  project,
  projectLabel,
  branch,
  model,
  pinned,
  archived,
  running,
  canEdit,
  onPatch,
}: TaskDetailsProps) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const [editing, setEditing] = useState(false)
  const [draftTitle, setDraftTitle] = useState(title)
  const [error, setError] = useState('')
  const rootRef = useRef<HTMLDivElement | null>(null)
  const triggerRef = useRef<HTMLButtonElement | null>(null)
  const popoverId = useId()
  const cloudLabelId = useId()
  const cloudDescriptionId = useId()
  const cloud = useCloudSessionSync(sessionId)

  useEffect(() => setDraftTitle(title), [title])
  useEffect(() => {
    if (open) return
    setEditing(false)
    setDraftTitle(title)
    setError('')
  }, [open, title])
  useDismissableMenu(open, setOpen, rootRef, triggerRef)

  async function patch(patchValue: Parameters<TaskDetailsProps['onPatch']>[0]): Promise<boolean> {
    setError('')
    try {
      await onPatch(patchValue)
      return true
    } catch {
      setError(t('desktopTitlebar.updateFailed'))
      return false
    }
  }

  async function saveTitle() {
    const next = draftTitle.trim()
    if (!next || next === title) {
      setDraftTitle(title)
      setEditing(false)
      return
    }
    if (await patch({ title: next })) setEditing(false)
  }

  const branchLabel = branch || t('desktopTitlebar.noBranch')
  const cloudDescription = !cloud.loggedIn
    ? t('cloud.syncNeedLogin')
    : cloud.synced
      ? t('cloud.syncOn')
      : t('cloud.syncOff')

  return (
    <div ref={rootRef} className="pointer-events-auto relative min-w-0 max-w-[48vw]">
      <button
        ref={triggerRef}
        type="button"
        aria-expanded={open}
        aria-haspopup="dialog"
        aria-controls={open ? popoverId : undefined}
        aria-label={t('desktopTitlebar.taskDetails')}
        onClick={() => {
          setError('')
          setOpen((value) => !value)
        }}
        className={`flex h-[34px] min-w-0 max-w-full items-center gap-2 rounded-[var(--radius-lg)] px-2.5 text-left transition-colors ${
          open
            ? 'bg-[var(--color-muted)] text-[var(--color-foreground)]'
            : 'text-[var(--color-foreground)] hover:bg-[var(--color-muted)]'
        }`}
      >
        <span className="min-w-0 truncate text-[13px] font-semibold">{title}</span>
        <span className="shrink-0 text-[var(--color-muted-foreground)]">·</span>
        <span className="min-w-0 truncate font-mono text-[11px] text-[var(--color-muted-foreground)]">
          {projectLabel} / {branchLabel}
        </span>
        <ChevronDownIcon className={`h-3 w-3 shrink-0 text-[var(--color-muted-foreground)] transition-transform ${open ? 'rotate-180' : ''}`} />
      </button>

      {open && (
        <div
          id={popoverId}
          role="dialog"
          aria-label={t('desktopTitlebar.taskDetails')}
          className="absolute left-0 top-[calc(100%+6px)] z-[var(--z-dropdown)] w-[420px] overflow-hidden rounded-[var(--radius-xl)] border border-[var(--color-border)] bg-[var(--color-surface)] shadow-[var(--shadow-lg)]"
        >
          <div className="border-b border-[var(--color-border)] px-4 py-3.5">
            {editing ? (
              <form
                onSubmit={(event) => {
                  event.preventDefault()
                  void saveTitle()
                }}
                className="flex items-center gap-2"
              >
                <input
                  autoFocus
                  value={draftTitle}
                  onChange={(event) => setDraftTitle(event.target.value)}
                  onKeyDown={(event) => {
                    if (event.key === 'Escape') {
                      event.stopPropagation()
                      setDraftTitle(title)
                      setEditing(false)
                    }
                  }}
                  aria-label={t('sidebar.actions.rename')}
                  className="h-8 min-w-0 flex-1 rounded-[var(--radius-md)] border border-[var(--color-border)] bg-[var(--color-background)] px-2.5 text-[13px] text-[var(--color-foreground)]"
                />
                <button
                  type="submit"
                  className="h-8 rounded-[var(--radius-md)] bg-[var(--color-primary)] px-3 text-[12px] font-medium text-[var(--color-on-primary)]"
                >
                  {t('common.save')}
                </button>
              </form>
            ) : (
              <div className="flex min-w-0 items-center gap-2">
                <h2 className="min-w-0 flex-1 truncate text-[14px] font-semibold text-[var(--color-foreground)]">{title}</h2>
                <span className={`inline-flex shrink-0 items-center gap-1.5 rounded-[var(--radius-pill)] px-2 py-1 text-[10px] font-medium ${
                  running
                    ? 'bg-[var(--neutral-wash)] text-[var(--color-accent-neutral)]'
                    : 'bg-[var(--color-muted)] text-[var(--color-muted-foreground)]'
                }`}>
                  <span className={`h-1.5 w-1.5 rounded-full ${running ? 'bg-[var(--color-accent-neutral)]' : 'bg-[var(--color-muted-foreground)]'}`} />
                  {running ? t('topbar.status.running') : t('desktopTitlebar.ready')}
                </span>
              </div>
            )}
          </div>

          <dl className="grid grid-cols-[92px_minmax(0,1fr)] gap-x-3 gap-y-2.5 border-b border-[var(--color-border)] px-4 py-3.5 text-[12px]">
            <dt className="text-[var(--color-muted-foreground)]">{t('desktopTitlebar.workspace')}</dt>
            <dd className="truncate font-medium text-[var(--color-foreground)]">{projectLabel}</dd>
            <dt className="text-[var(--color-muted-foreground)]">{t('desktopTitlebar.branch')}</dt>
            <dd className="truncate font-mono text-[var(--color-foreground)]">{branchLabel}</dd>
            <dt className="text-[var(--color-muted-foreground)]">{t('desktopTitlebar.path')}</dt>
            <dd className="truncate font-mono text-[11px] text-[var(--color-foreground)]" title={project}>{project || '—'}</dd>
            {model && (
              <>
                <dt className="text-[var(--color-muted-foreground)]">{t('desktopTitlebar.model')}</dt>
                <dd className="truncate font-mono text-[11px] text-[var(--color-foreground)]">{model}</dd>
              </>
            )}
          </dl>

          <div className="border-b border-[var(--color-border)] p-2">
            <button
              type="button"
              role="switch"
              aria-checked={cloud.synced}
              aria-labelledby={cloudLabelId}
              aria-describedby={cloudDescriptionId}
              disabled={!sessionId || !cloud.loggedIn || cloud.busy}
              onClick={() => void cloud.toggle()}
              className="flex w-full items-center gap-3 rounded-[var(--radius-lg)] px-2.5 py-2 text-left transition-colors hover:bg-[var(--color-muted)] disabled:cursor-not-allowed disabled:opacity-55"
            >
              <CloudIcon className={`h-4 w-4 shrink-0 ${cloud.synced ? 'text-[var(--color-accent-neutral)]' : 'text-[var(--color-muted-foreground)]'}`} />
              <span className="min-w-0 flex-1">
                <span id={cloudLabelId} className="block text-[12.5px] font-medium text-[var(--color-foreground)]">{t('cloud.syncSession')}</span>
                <span id={cloudDescriptionId} className="block truncate text-[10.5px] text-[var(--color-muted-foreground)]">{cloudDescription}</span>
              </span>
              <span
                aria-hidden="true"
                className={`relative h-5 w-9 shrink-0 rounded-[var(--radius-pill)] transition-colors ${
                  cloud.synced ? 'bg-[var(--color-accent-neutral)]' : 'bg-[var(--color-border)]'
                }`}
              >
                <span className={`absolute top-0.5 h-4 w-4 rounded-full bg-[var(--color-background)] shadow-[var(--shadow-sm)] transition-transform ${
                  cloud.synced ? 'translate-x-[18px]' : 'translate-x-0.5'
                }`} />
              </span>
            </button>
          </div>

          {canEdit && (
            <div className="p-2">
              <TaskAction
                icon={BookmarkIcon}
                label={pinned ? t('sidebar.actions.unpin') : t('sidebar.actions.pin')}
                onClick={() => void patch({ pinned: !pinned })}
              />
              <TaskAction
                icon={PencilSquareIcon}
                label={t('sidebar.actions.rename')}
                onClick={() => setEditing(true)}
              />
              <TaskAction
                icon={ArchiveBoxIcon}
                label={archived ? t('sidebar.actions.unarchive') : t('sidebar.actions.archive')}
                onClick={() => void patch({ archived: !archived })}
              />
            </div>
          )}

          {error && (
            <p role="alert" className="border-t border-[var(--color-border)] px-4 py-2 text-[11px] text-[var(--color-error-fg)]">
              {error}
            </p>
          )}
        </div>
      )}
    </div>
  )
}

function TaskAction({ icon: Icon, label, onClick }: { icon: OutlineIcon; label: string; onClick: () => void }) {
  return (
    <button
      type="button"
      onClick={onClick}
      className="flex w-full items-center gap-2.5 rounded-[var(--radius-md)] px-2.5 py-2 text-left text-[12.5px] text-[var(--color-foreground)] transition-colors hover:bg-[var(--color-muted)]"
    >
      <Icon className="h-4 w-4 shrink-0 text-[var(--color-muted-foreground)]" />
      {label}
    </button>
  )
}

function WorkspaceOpenMenu({ projectPath }: { projectPath: string }) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const [error, setError] = useState('')
  const [applications, setApplications] = useState<WorkspaceApplication[]>([])
  const [loading, setLoading] = useState(true)
  const [discoveryFailed, setDiscoveryFailed] = useState(false)
  const rootRef = useRef<HTMLDivElement | null>(null)
  const triggerRef = useRef<HTMLButtonElement | null>(null)
  const itemRefs = useRef<Array<HTMLButtonElement | null>>([])
  const menuId = useId()
  const disabled = !projectPath || isRemotePath(projectPath)

  useEffect(() => {
    let active = true
    setLoading(true)
    setDiscoveryFailed(false)
    void listWorkspaceApplications()
      .then((items) => {
        if (active) setApplications(items)
      })
      .catch(() => {
        if (active) {
          setApplications([])
          setDiscoveryFailed(true)
        }
      })
      .finally(() => {
        if (active) setLoading(false)
      })
    return () => {
      active = false
    }
  }, [])

  useDismissableMenu(open, setOpen, rootRef, triggerRef)
  useEffect(() => {
    if (!open) return
    const frame = window.requestAnimationFrame(() => itemRefs.current[0]?.focus())
    return () => window.cancelAnimationFrame(frame)
  }, [applications.length, open])

  async function openIn(application: WorkspaceApplication) {
    if (disabled) return
    setError('')
    try {
      await openWorkspaceInApplication(projectPath, application.id)
      setOpen(false)
    } catch {
      setError(t('desktopTitlebar.openFailed', { app: application.label }))
    }
  }

  function onMenuKeyDown(event: React.KeyboardEvent<HTMLDivElement>) {
    const items = itemRefs.current.filter((item): item is HTMLButtonElement => item !== null)
    if (items.length === 0) return
    const currentIndex = items.indexOf(document.activeElement as HTMLButtonElement)
    let nextIndex: number | null = null
    if (event.key === 'ArrowDown') nextIndex = currentIndex < 0 ? 0 : (currentIndex + 1) % items.length
    else if (event.key === 'ArrowUp') nextIndex = currentIndex < 0 ? items.length - 1 : (currentIndex - 1 + items.length) % items.length
    else if (event.key === 'Home') nextIndex = 0
    else if (event.key === 'End') nextIndex = items.length - 1
    if (nextIndex === null) return
    event.preventDefault()
    items[nextIndex].focus()
  }

  return (
    <div ref={rootRef} className="relative">
      <button
        ref={triggerRef}
        type="button"
        disabled={disabled}
        aria-expanded={open}
        aria-haspopup="menu"
        aria-controls={open ? menuId : undefined}
        aria-label={t('desktopTitlebar.openWorkspace')}
        title={disabled ? t('desktopTitlebar.localOnly') : t('desktopTitlebar.openWorkspace')}
        onClick={() => {
          setError('')
          setOpen((value) => !value)
        }}
        className={`inline-flex h-[34px] items-center gap-1.5 rounded-[var(--radius-lg)] px-2.5 text-[12px] transition-colors disabled:cursor-not-allowed disabled:opacity-45 ${
          open
            ? 'bg-[var(--color-muted)] text-[var(--color-foreground)]'
            : 'text-[var(--color-muted-foreground)] hover:bg-[var(--color-muted)] hover:text-[var(--color-foreground)]'
        }`}
      >
        <CodeBracketSquareIcon className="h-4 w-4" />
        <span>{t('desktopTitlebar.open')}</span>
        <ChevronDownIcon className="h-3 w-3 opacity-60" />
      </button>

      {open && (
        <div
          id={menuId}
          role="menu"
          aria-label={t('desktopTitlebar.openWorkspace')}
          onKeyDown={onMenuKeyDown}
          className="absolute right-0 top-[calc(100%+6px)] z-[var(--z-dropdown)] max-h-[calc(100vh-58px)] w-[232px] overflow-y-auto rounded-[var(--radius-xl)] border border-[var(--color-border)] bg-[var(--color-surface)] p-1.5 shadow-[var(--shadow-lg)]"
        >
          {loading && (
            <div className="px-2.5 py-2 text-[11px] text-[var(--color-muted-foreground)]">
              {t('common.loading')}
            </div>
          )}
          {!loading && discoveryFailed && (
            <p role="alert" className="px-2.5 py-2 text-[11px] leading-relaxed text-[var(--color-error-fg)]">
              {t('desktopTitlebar.discoveryFailed')}
            </p>
          )}
          {!loading && !discoveryFailed && applications.length === 0 && (
            <div className="px-2.5 py-2 text-[11px] text-[var(--color-muted-foreground)]">
              {t('desktopTitlebar.noApplications')}
            </div>
          )}
          {applications.map((application, index) => {
            const startsGroup = index > 0 && applications[index - 1].group !== application.group
            return (
              <div key={application.id}>
                {startsGroup && <div className="my-1 h-px bg-[var(--color-border)]" />}
                <button
                  ref={(element) => {
                    itemRefs.current[index] = element
                  }}
                  type="button"
                  role="menuitem"
                  onClick={() => void openIn(application)}
                  className="flex w-full items-center gap-2.5 rounded-[var(--radius-md)] px-2.5 py-1.5 text-left text-[13px] text-[var(--color-foreground)] transition-colors hover:bg-[var(--color-muted)] focus:bg-[var(--color-muted)] focus:outline-none"
                >
                  <WorkspaceApplicationIcon application={application} />
                  {application.label}
                </button>
              </div>
            )
          })}
          {error && (
            <p role="alert" className="mt-1 border-t border-[var(--color-border)] px-2.5 py-2 text-[10.5px] text-[var(--color-error-fg)]">
              {error}
            </p>
          )}
        </div>
      )}
    </div>
  )
}

function WorkspaceApplicationIcon({ application }: { application: WorkspaceApplication }) {
  if (application.iconDataUrl) {
    return (
      <img
        src={application.iconDataUrl}
        alt=""
        aria-hidden="true"
        className="h-[22px] w-[22px] shrink-0 object-contain"
      />
    )
  }
  const Icon = application.kind === 'fileManager'
    ? FolderOpenIcon
    : application.kind === 'editor'
      ? CodeBracketSquareIcon
      : CommandLineIcon
  return (
    <span className="grid h-[22px] w-[22px] shrink-0 place-items-center rounded-[var(--radius-sm)] bg-[var(--color-muted)]">
      <Icon className="h-3.5 w-3.5 text-[var(--color-muted-foreground)]" />
    </span>
  )
}

function useDismissableMenu(
  open: boolean,
  setOpen: (open: boolean) => void,
  rootRef: RefObject<HTMLElement | null>,
  triggerRef?: RefObject<HTMLElement | null>,
) {
  useEffect(() => {
    if (!open) return
    function onMouseDown(event: MouseEvent) {
      if (rootRef.current && !rootRef.current.contains(event.target as Node)) setOpen(false)
    }
    function onKeyDown(event: KeyboardEvent) {
      if (event.key === 'Escape') {
        setOpen(false)
        triggerRef?.current?.focus()
      }
    }
    document.addEventListener('mousedown', onMouseDown)
    document.addEventListener('keydown', onKeyDown)
    return () => {
      document.removeEventListener('mousedown', onMouseDown)
      document.removeEventListener('keydown', onKeyDown)
    }
  }, [open, rootRef, setOpen, triggerRef])
}

function workspaceName(path: string): string {
  if (!path) return '—'
  if (isRemotePath(path)) {
    const parts = path.replace(/\/+$/, '').split('/')
    return parts[parts.length - 1] || path
  }
  const normalized = path.replace(/\\/g, '/').replace(/\/+$/, '')
  return normalized.split('/').pop() || path
}
