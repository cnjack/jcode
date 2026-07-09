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
import {
  RuntimeProvider,
  ToolRegistryProvider,
  createDefaultToolRegistry,
} from 'jcode-ui'
import { WSClient } from './lib/ws'
import { api } from './lib/api'
import { normalizeMode } from './lib/types'
import { useAppDispatch, useAppSelector } from './app/hooks'
import {
  modelActions,
  sessionActions,
  uiActions,
  loadSessions,
  loadTasks,
  loadSlashCommands,
  loadSession,
} from './app/store'
import { bridgeWS } from './app/wsBridge'
import { useChatRuntime } from './app/runtime'
import { Sidebar } from './components/Sidebar'
import { ChatView } from './components/ChatView'
import { AutomationsView } from './components/AutomationsView'
import { ChannelsView } from './components/ChannelsView'
import { CommandPalette } from './components/CommandPalette'
import { AuthGate } from './components/AuthGate'
import { SetupView } from './components/SetupView'
import { SettingsDialog } from './components/SettingsDialog'
import { TopBar } from './components/TopBar'
import { RightPanel } from './components/RightPanel'
import { TerminalPanel } from './components/TerminalPanel'

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
        if (h.auth_required) dispatch(uiActions.setNeedsAuth(true))
        if (h.needs_setup) dispatch(uiActions.setNeedsSetup(true))
        // Load the current session's history into the timeline (replay). The
        // boot session_id may be a fresh empty session (no JSONL yet, 404) — in
        // that case fall back to the most recent listed session. loadSession
        // swallows the 404 internally and returns without setting a timeline,
        // so we detect an empty timeline and retry with the most recent session.
        if (!h.needs_setup) {
          await dispatch(loadSession(h.session_id))
          const state = store_getState()
          if (state.chat.timeline.length === 0) {
            const sessions = await api.sessions()
            if (sessions.length > 0) await dispatch(loadSession(sessions[0].uuid))
          }
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

  // Load sidebar data + slash commands after boot.
  useEffect(() => {
    void dispatch(loadSessions())
    void dispatch(loadTasks())
    void dispatch(loadSlashCommands())
  }, [dispatch])

  // Global keyboard shortcuts: ⌘K (command palette), ⌘N (new chat), Esc (close
  // overlays). Mirrors the Vue App.vue shortcut wiring.
  useEffect(() => {
    function onKey(e: KeyboardEvent) {
      const meta = e.metaKey || e.ctrlKey
      if (meta && e.key === 'k') {
        e.preventDefault()
        dispatch(uiActions.setPaletteOpen(true))
      } else if (meta && e.key === 'n') {
        e.preventDefault()
        // New chat: clear + reset session + switch to chat view.
        dispatch(loadSession('')) // empty → new session flow handled in Sidebar
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

  // Panel keyboard shortcuts: ⇧⌘P (plan), ⇧⌘E (files), ⇧⌘G (changes), ⌘` (terminal).
  useEffect(() => {
    function onKey(e: KeyboardEvent) {
      const meta = e.metaKey || e.ctrlKey
      if (meta && e.shiftKey && e.key.toLowerCase() === 'p') {
        e.preventDefault(); togglePanel('plan')
      } else if (meta && e.shiftKey && e.key.toLowerCase() === 'e') {
        e.preventDefault(); togglePanel('files')
      } else if (meta && e.shiftKey && e.key.toLowerCase() === 'g') {
        e.preventDefault(); togglePanel('changes')
      } else if (meta && e.key === '`') {
        e.preventDefault(); togglePanel('terminal')
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [togglePanel])

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
      <ToolRegistryProvider registry={registry}>
        <div className="relative flex h-[100dvh] overflow-hidden bg-[var(--color-background)] text-[var(--color-foreground)]">
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

          <Sidebar />

          <main className="relative flex min-w-0 flex-1 flex-col">
            {activeView === 'chat' && <ChatView />}
            {activeView === 'automations' && <AutomationsView />}
            {activeView === 'channels' && <ChannelsView />}
            {activeView === 'automation-run' && <ChatView readOnly />}

            {/* Bottom panel (terminal) */}
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

          {/* Right panel (files/changes/plan) */}
          {rightPanelOpen && (
            <RightPanel
              activeTab={rightPanelTab}
              onClose={() => setRightPanelOpen(false)}
              onSwitchTab={setRightPanelTab}
            />
          )}

          {paletteOpen && <CommandPalette />}
          <SettingsDialog />
        </div>
      </ToolRegistryProvider>
    </RuntimeProvider>
  )
}

function ErrorScreen({ message }: { message: string }) {
  return (
    <div className="flex h-screen flex-col items-center justify-center gap-2 bg-[var(--color-background)] text-[var(--color-foreground)]">
      <h1 className="text-xl font-semibold">Can't reach the jcode backend</h1>
      <p className="max-w-md text-center text-sm text-[var(--color-muted-foreground)]">{message}</p>
      <button
        type="button"
        onClick={() => location.reload()}
        className="mt-2 rounded-[var(--radius-md)] bg-[var(--color-primary)] px-3 py-1.5 text-sm text-[var(--color-on-primary)]"
      >
        Retry
      </button>
    </div>
  )
}
