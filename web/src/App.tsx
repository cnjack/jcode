/**
 * App shell — orchestrates boot, WS, view routing.
 *
 * Boot flow (mirrors the Vue App.vue):
 *   1. initApiBase() already resolved in main.tsx (dual-host contract).
 *   2. GET /api/health → seed model/mode/session, gate on auth/setup.
 *   3. Connect WS, load sessions/tasks/slash commands.
 *   4. Render the active view (chat / automations / channels) wrapped in the
 *      jcode-ui RuntimeProvider so Thread/Composer read from the RTK store.
 *
 * The WS bridge is a module-level singleton created here via useEffect (once).
 */

import { useCallback, useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import {
  ArrowLeftIcon,
  CheckCircleIcon,
  ExclamationCircleIcon,
  PlayIcon,
  StopIcon,
} from '@heroicons/react/24/outline'
import {
  RuntimeProvider,
  ToolRegistryProvider,
  ApiBaseProvider,
  createDefaultToolRegistry,
} from 'jcode-ui'
import { WSClient } from './lib/ws'
import { api } from './lib/api'
import { apiBase } from './lib/apiBase'
import { normalizeMode } from './lib/types'
import { useAppDispatch, useAppSelector } from './app/hooks'
import {
  modelActions,
  sessionActions,
  uiActions,
  chatActions,
  loadSession,
  loadWorkspaceState,
  replaySession,
  startNewChat,
} from './app/store'
import { bridgeWS } from './app/wsBridge'
import { useChatRuntime } from './app/runtime'
import { triggerKindLabel, type AutomationRun } from './lib/automation'
import { Sidebar } from './components/Sidebar'
import { ChatView } from './components/ChatView'
import { AutomationsView } from './components/AutomationsView'
import { ChannelsView } from './components/ChannelsView'
import { CommandPalette } from './components/CommandPalette'
import { AuthGate } from './components/AuthGate'
import { SetupView } from './components/SetupView'
import { SettingsDialog } from './components/SettingsDialog'
import { TopBar } from './components/TopBar'
import { CloudSyncToggle } from './components/CloudSyncToggle'
import { ComputerShotPiP } from './components/ComputerShotPiP'
import { RightPanel } from './components/RightPanel'
import { TerminalPanel } from './components/TerminalPanel'
import { RemoteConnectWizard } from './components/RemoteConnectWizard'
import type { RemotePrefill } from './lib/remote'

export default function App() {
  const dispatch = useAppDispatch()
  const activeView = useAppSelector((s) => s.ui.activeView)
  const needsAuth = useAppSelector((s) => s.ui.needsAuth)
  const needsSetup = useAppSelector((s) => s.ui.needsSetup)
  const connectionError = useAppSelector((s) => s.ui.connectionError)
  const wsRef = useRef<WSClient | null>(null)

  // Boot: health check + seed state. Runs once.
  useEffect(() => {
    let cancelled = false
    async function boot() {
      try {
        const h = await api.health()
        if (cancelled) return
        dispatch(modelActions.setProvider(h.provider))
        dispatch(modelActions.setModel(h.model))
        dispatch(modelActions.setMode(normalizeMode(h.mode)))
        dispatch(modelActions.setServerVersion(h.version))
        dispatch(modelActions.setImageSupport(!!h.image_support))
        dispatch(sessionActions.setProjectPath(h.pwd))
        dispatch(sessionActions.setCurrentSession(h.session_id || ''))
        dispatch(chatActions.setRunning(!!h.running))
        if (h.auth_required) dispatch(uiActions.setNeedsAuth(true))
        if (h.needs_setup) dispatch(uiActions.setNeedsSetup(true))
        // Load the workspace state, then restore the current session only if it
        // has persisted history. A fresh empty session should stay on welcome.
        if (!h.auth_required && !h.needs_setup) {
          await dispatch(loadWorkspaceState())
          if (h.session_id) await dispatch(loadSession(h.session_id))
        }
      } catch (err) {
        if (!cancelled) {
          dispatch(uiActions.setConnectionError(err instanceof Error ? err.message : String(err)))
        }
      }
    }
    void boot()
    return () => {
      cancelled = true
    }
  }, [dispatch])

  // WS bridge: connect once. The handlers read fresh state via getState, so a
  // single client + handler set stays correct across session/view changes.
  useEffect(() => {
    const client = new WSClient({
      activeTaskId: () => undefined, // replaced by bridgeWS with the store getter
    })
    wsRef.current = bridgeWS(
      client,
      () => store_getState(),
      dispatch as never,
    )
    return () => {
      client.disconnect()
      wsRef.current = null
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [dispatch])

  // Global keyboard shortcuts: ⌘K (command palette), ⌘N / ⇧⌘O (new chat), Esc
  // (close overlays). ⌘N is reserved by browsers (new window) so it only fires
  // in the desktop app; ⇧⌘O is interceptable everywhere and is the shortcut
  // shown in the UI.
  useEffect(() => {
    function onKey(e: KeyboardEvent) {
      const meta = e.metaKey || e.ctrlKey
      if (meta && e.key === 'k' && !e.shiftKey) {
        e.preventDefault()
        dispatch(uiActions.setPaletteOpen(true))
      } else if (meta && !e.shiftKey && e.key === 'n') {
        e.preventDefault()
        void dispatch(startNewChat())
      } else if (meta && e.shiftKey && e.key.toLowerCase() === 'o') {
        e.preventDefault()
        void dispatch(startNewChat())
      } else if (meta && e.key === ',') {
        e.preventDefault()
        dispatch(uiActions.setSettingsOpen(true))
      } else if (e.key === 'Escape') {
        dispatch(uiActions.setPaletteOpen(false))
        dispatch(uiActions.setSettingsOpen(false))
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [dispatch])

  // Gate screens take precedence.
  if (connectionError) {
    return <ErrorScreen message={connectionError} />
  }
  if (needsSetup) {
    return <SetupView />
  }
  if (needsAuth) {
    return <AuthGate />
  }

  return <Shell activeView={activeView} />
}

// store_getState is bound lazily to avoid a circular import at module eval.
// It reads from the singleton store exported by app/store.
import { store } from './app/store'
function store_getState() {
  return store.getState()
}

type PanelType = 'terminal' | 'files' | 'changes' | 'plan'

function Shell({ activeView }: { activeView: 'chat' | 'automations' | 'channels' | 'automation-run' }) {
  const dispatch = useAppDispatch()
  const runtime = useChatRuntime()
  const registry = useRef(createDefaultToolRegistry()).current
  const paletteOpen = useAppSelector((s) => s.ui.paletteOpen)
  const isRunning = useAppSelector((s) => s.chat.isRunning)
  const wsConnected = useAppSelector((s) => s.session.wsConnected)

  // Panel state — mirrors Vue App.vue: a right panel (files/changes/plan) and a
  // bottom panel (terminal) that can be open simultaneously.
  const [rightPanelOpen, setRightPanelOpen] = useState(false)
  const [rightPanelTab, setRightPanelTab] = useState<'files' | 'changes' | 'plan'>('files')
  const [bottomPanel, setBottomPanel] = useState<'none' | 'terminal'>('none')
  const [bottomPanelHeight, setBottomPanelHeight] = useState(260)
  const [activeRun, setActiveRun] = useState<AutomationRun | null>(null)
  const [remoteWizardOpen, setRemoteWizardOpen] = useState(false)
  const [remotePrefill, setRemotePrefill] = useState<RemotePrefill | null>(null)

  const openRun = useCallback((run: AutomationRun) => {
    setActiveRun(run)
    dispatch(uiActions.setView('automation-run'))
    void dispatch(replaySession(run.session_id))
  }, [dispatch])

  const closeRun = useCallback(() => {
    setActiveRun(null)
    dispatch(uiActions.setView('automations'))
  }, [dispatch])

  const togglePanel = useCallback((panel: PanelType) => {
    if (panel === 'terminal') {
      setBottomPanel((p) => (p === 'terminal' ? 'none' : 'terminal'))
      return
    }
    setRightPanelTab((current) => {
      if (rightPanelOpen && current === panel) {
        setRightPanelOpen(false)
        return current
      }
      setRightPanelOpen(true)
      return panel
    })
  }, [rightPanelOpen])

  // Panel keyboard shortcuts: ⇧⌘P (plan), ⇧⌘E (files), ⇧⌘G (changes), ⌘` / ⌘J (terminal).
  useEffect(() => {
    function onKey(e: KeyboardEvent) {
      const meta = e.metaKey || e.ctrlKey
      if (meta && e.shiftKey && e.key.toLowerCase() === 'p') {
        e.preventDefault(); togglePanel('plan')
      } else if (meta && e.shiftKey && e.key.toLowerCase() === 'e') {
        e.preventDefault(); togglePanel('files')
      } else if (meta && e.shiftKey && e.key.toLowerCase() === 'g') {
        e.preventDefault(); togglePanel('changes')
      } else if (meta && !e.shiftKey && (e.key === '`' || e.key.toLowerCase() === 'j')) {
        // ⌘` never reaches the page on macOS (OS window cycling), so ⌘J is the
        // alias shown in the UI. `!e.shiftKey` keeps ⇧⌘J (DevTools) intact.
        e.preventDefault(); togglePanel('terminal')
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [togglePanel])

  useEffect(() => {
    function onOpenRemote(e: Event) {
      const detail = (e as CustomEvent<RemotePrefill | null>).detail
      setRemotePrefill(detail ?? null)
      setRemoteWizardOpen(true)
    }
    window.addEventListener('jcode:open-remote-connect', onOpenRemote)
    return () => window.removeEventListener('jcode:open-remote-connect', onOpenRemote)
  }, [])

  // Bottom-panel resize handle.
  const startResize = useCallback((e: React.MouseEvent) => {
    e.preventDefault()
    const startY = e.clientY
    const startH = bottomPanelHeight
    function onMove(ev: MouseEvent) {
      const diff = startY - ev.clientY
      setBottomPanelHeight(Math.max(120, Math.min(600, startH + diff)))
    }
    function onUp() {
      document.removeEventListener('mousemove', onMove)
      document.removeEventListener('mouseup', onUp)
    }
    document.addEventListener('mousemove', onMove)
    document.addEventListener('mouseup', onUp)
  }, [bottomPanelHeight])

  return (
    <RuntimeProvider runtime={runtime}>
      {/* ApiBaseProvider lets tool renderers (browser/computer screenshots)
          resolve "/api/…" image refs against the real backend origin — required
          in the Tauri shell, where the page itself is served from tauri://localhost
          and a bare relative <img src> would 404. '' in browser mode (same-origin). */}
      <ApiBaseProvider apiBase={apiBase}>
        <ToolRegistryProvider registry={registry}>
        <div className="app-shell relative flex h-[100dvh] overflow-hidden bg-[var(--color-background)] text-[var(--color-foreground)]">
          {/* Native title-bar drag strip — hidden in browser, shown in Tauri (CSS). */}
          <div className="titlebar-drag" data-tauri-drag-region aria-hidden="true" />
          {/* TopBar — floated top-right (only on chat view, like Vue). */}
          {activeView === 'chat' && (
            <TopBar
              isRunning={isRunning}
              wsConnected={wsConnected}
              activePanel={rightPanelOpen ? rightPanelTab : 'none'}
              terminalOpen={bottomPanel === 'terminal'}
              onTogglePanel={togglePanel}
            />
          )}
          {/* Per-session cloud sync switch (M19) — floated left of the TopBar. */}
          {activeView === 'chat' && <CloudSyncToggle />}
          {/* Codex-style computer-use PiP — floats under the TopBar, shows the
              latest screenshot from the session's computer_screenshot calls. */}
          {activeView === 'chat' && <ComputerShotPiP />}

          {/* Sidebar floats on the shell background — no border, no shared frame
              with the main canvas (matches Vue App.vue). */}
          <Sidebar />

          {/* Main column: the chat/page surface is an inset rounded card with
              breathing room; its left corners are fully rounded because the
              sidebar is outside the card. */}
          <main className="workspace-main relative flex min-h-0 min-w-0 flex-1 flex-col">
            {activeView === 'chat' && <ChatView />}
            {activeView === 'automations' && <AutomationsView onOpenRun={openRun} />}
            {activeView === 'channels' && <ChannelsView />}
            {activeView === 'automation-run' && (
              <AutomationRunReplay run={activeRun} onBack={closeRun} />
            )}

            {/* Bottom panel (terminal) — lives under the inset surface. */}
            {bottomPanel === 'terminal' && (
              <div className="relative shrink-0 border-t border-[var(--color-border)]" style={{ height: bottomPanelHeight }}>
                <div
                  className="absolute -top-1 left-0 right-0 z-10 h-2 cursor-row-resize"
                  onMouseDown={startResize}
                >
                  <div className="absolute left-1/2 top-[3px] h-1 w-8 -translate-x-1/2 rounded-full bg-[var(--color-border)]" />
                </div>
                <TerminalPanel onClose={() => setBottomPanel('none')} />
              </div>
            )}
          </main>

          {/* Right panel (files/changes/plan) — sibling of main, like Vue. */}
          {rightPanelOpen && (
            <RightPanel
              activeTab={rightPanelTab}
              onClose={() => setRightPanelOpen(false)}
              onSwitchTab={setRightPanelTab}
            />
          )}

          {paletteOpen && <CommandPalette />}
          <SettingsDialog />
          <RemoteConnectWizard
            open={remoteWizardOpen}
            prefill={remotePrefill}
            onClose={() => {
              setRemoteWizardOpen(false)
              setRemotePrefill(null)
            }}
            onBound={() => {
              setRemoteWizardOpen(false)
              setRemotePrefill(null)
              dispatch(uiActions.setSettingsOpen(false))
            }}
          />
        </div>
        </ToolRegistryProvider>
      </ApiBaseProvider>
    </RuntimeProvider>
  )
}

function AutomationRunReplay({ run, onBack }: { run: AutomationRun | null; onBack: () => void }) {
  const { t } = useTranslation()
  const isRunning = run ? (run.terminal_status || run.status) === 'running' || (!run.terminal_status && run.status === 'running') : false
  const status = run ? statusKind(run) : 'running'
  const StatusIcon = status === 'success' ? CheckCircleIcon : status === 'error' ? ExclamationCircleIcon : PlayIcon
  const statusLabel =
    status === 'success'
      ? t('automations.replay.completed')
      : status === 'error'
        ? t('automations.replay.failed')
        : t('automations.replay.running')

  async function stopRun() {
    if (!run) return
    await api.stop(run.session_id).catch(() => {})
  }

  return (
    <div className="page-surface flex min-h-0 flex-1 flex-col">
      <header className="flex shrink-0 flex-col gap-2 border-b border-[var(--color-border)] bg-[var(--color-surface)] px-4 py-3">
        <button
          type="button"
          onClick={onBack}
          className="inline-flex w-fit items-center gap-1.5 rounded-[var(--radius-md)] px-1.5 py-1 text-[12px] text-[var(--color-muted-foreground)] transition-colors hover:bg-[var(--color-muted)] hover:text-[var(--color-foreground)]"
        >
          <ArrowLeftIcon className="h-3.5 w-3.5" />
          {t('nav.automations')}
        </button>
        <div className="flex min-w-0 items-center gap-2">
          <h1 className="min-w-0 flex-1 truncate text-base font-semibold text-[var(--color-foreground)]">
            {run?.title || t('automations.runFallback')}
          </h1>
          <span className={`inline-flex shrink-0 items-center gap-1 rounded-full px-2 py-1 text-[11px] font-semibold ${statusClass(status)}`}>
            <StatusIcon className="h-3.5 w-3.5" />
            {statusLabel}
          </span>
          {isRunning && (
            <button
              type="button"
              onClick={() => void stopRun()}
              className="inline-flex shrink-0 items-center gap-1 rounded-[var(--radius-md)] border border-[var(--color-border)] bg-[var(--color-background)] px-2 py-1 text-[11px] text-[var(--color-foreground)] hover:bg-[var(--color-muted)]"
            >
              <StopIcon className="h-3.5 w-3.5" />
              {t('chat.stop')}
            </button>
          )}
        </div>
        {run && (
          <div className="flex flex-wrap items-center gap-x-2 gap-y-1 text-[11.5px] text-[var(--color-muted-foreground)]">
            <span className="font-mono uppercase tracking-[0.05em]">{t('automations.replay.trigger')}</span>
            <span>{triggerKindLabel(run.trigger_kind, t)}</span>
            {run.project && (
              <>
                <span className="text-[var(--color-border)]">·</span>
                <span className="font-mono uppercase tracking-[0.05em]">{t('automations.replay.project')}</span>
                <span className="max-w-[360px] truncate">{run.project}</span>
              </>
            )}
            {run.start_time && (
              <>
                <span className="text-[var(--color-border)]">·</span>
                <span>{new Date(run.start_time).toLocaleString()}</span>
              </>
            )}
          </div>
        )}
        {run?.error_reason && status === 'error' && (
          <div className="rounded-[var(--radius-md)] border border-[var(--color-destructive)] bg-[var(--color-error-bg)] px-2.5 py-2 text-xs text-[var(--color-error-fg)]">
            {run.error_reason}
          </div>
        )}
      </header>
      <ChatView readOnly />
    </div>
  )
}

function statusKind(run: AutomationRun): 'success' | 'error' | 'running' {
  const s = run.terminal_status || run.status
  if (s === 'success') return 'success'
  if (s === 'error') return 'error'
  return 'running'
}

function statusClass(status: 'success' | 'error' | 'running'): string {
  if (status === 'success') return 'bg-[var(--color-success-bg)] text-[var(--color-success-fg)]'
  if (status === 'error') return 'bg-[var(--color-error-bg)] text-[var(--color-error-fg)]'
  return 'bg-[var(--accent-wash)] text-[var(--color-primary)]'
}

function ErrorScreen({ message }: { message: string }) {
  const { t } = useTranslation()
  return (
    <div className="flex h-screen flex-col items-center justify-center gap-2 bg-[var(--color-background)] text-[var(--color-foreground)]">
      <h1 className="text-xl font-semibold">{t('connection.errorTitle')}</h1>
      <p className="max-w-md text-center text-sm text-[var(--color-muted-foreground)]">{message}</p>
      <button
        type="button"
        onClick={() => location.reload()}
        className="mt-2 rounded-[var(--radius-md)] bg-[var(--color-primary)] px-3 py-1.5 text-sm text-[var(--color-on-primary)]"
      >
        {t('common.retry')}
      </button>
    </div>
  )
}
