/**
 * TopBar — the single top-right FLOATING control (absolute, top:6px right:14px,
 * z-46). Carries the panels menu: a button showing a RectangleStackIcon + a
 * ChevronDown + a live status dot, which opens a dropdown with Plan / Files /
 * Changes / Terminal. The Changes item shows a live diff stat (+N/-M).
 *
 * Ported from web/src/components/TopBar.vue. The React app has no headlessui, so
 * the dropdown is implemented manually: a button + an absolute-positioned menu
 * div + a document click listener that closes on outside clicks.
 *
 * The control is position:absolute and expects its parent to be position:relative
 * (the App shell). Terminal is a bottom panel and tracked separately from
 * activePanel (which reflects the right panel only).
 */

import { useCallback, useEffect, useRef, useState } from 'react'
import {
  ArrowsRightLeftIcon,
  ChevronDownIcon,
  ClipboardDocumentCheckIcon,
  CommandLineIcon,
  FolderOpenIcon,
  RectangleStackIcon,
} from '@heroicons/react/24/outline'
import { useTranslation } from 'react-i18next'
import { api } from '../lib/api'

type PanelType = 'plan' | 'files' | 'changes' | 'terminal'

interface Props {
  isRunning: boolean
  wsConnected: boolean
  activePanel: 'none' | 'plan' | 'files' | 'changes' | 'terminal'
  terminalOpen: boolean
  onTogglePanel: (panel: PanelType) => void
}

const PANEL_BUTTONS: { panel: PanelType; shortcut: string }[] = [
  { panel: 'plan', shortcut: '⇧⌘P' },
  { panel: 'files', shortcut: '⇧⌘E' },
  { panel: 'changes', shortcut: '⇧⌘G' },
  { panel: 'terminal', shortcut: '⌘`' },
]

const PANEL_ICONS: Record<PanelType, typeof RectangleStackIcon> = {
  plan: ClipboardDocumentCheckIcon,
  files: FolderOpenIcon,
  changes: ArrowsRightLeftIcon,
  terminal: CommandLineIcon,
}

export function TopBar({ isRunning, wsConnected, activePanel, terminalOpen, onTogglePanel }: Props) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  // Working-tree diff stat shown inline on the Changes item. null on failure /
  // clean tree (matches the Vue behaviour — never fabricated).
  const [diffStat, setDiffStat] = useState<{ additions: number; deletions: number } | null>(null)

  const controlRef = useRef<HTMLDivElement | null>(null)

  const loadDiffStat = useCallback(async () => {
    try {
      const result = await api.diff('working')
      const additions = result.entries.reduce((sum, e) => sum + e.additions, 0)
      const deletions = result.entries.reduce((sum, e) => sum + e.deletions, 0)
      setDiffStat(result.entries.length > 0 ? { additions, deletions } : null)
    } catch (err) {
      console.error('Failed to fetch diff stat:', err)
      setDiffStat(null)
    }
  }, [])

  // Initial load of the diff stat (mirrors the Vue onMounted).
  useEffect(() => {
    void loadDiffStat()
  }, [loadDiffStat])

  // Reload the stat when a run finishes (mirrors the Vue watch on isRunning):
  // a fresh tree of changes is most useful right after the agent stops.
  const wasRunningRef = useRef(isRunning)
  useEffect(() => {
    if (wasRunningRef.current && !isRunning) {
      void loadDiffStat()
    }
    wasRunningRef.current = isRunning
  }, [isRunning, loadDiffStat])

  // Click-outside handling: close the menu when a click lands outside the
  // control. A document listener is the simplest manual equivalent of headlessui.
  useEffect(() => {
    if (!open) return
    function onDocClick(e: MouseEvent) {
      if (controlRef.current && !controlRef.current.contains(e.target as Node)) {
        setOpen(false)
      }
    }
    // Defer to the next tick so the opening click itself doesn't immediately
    // re-close the menu (it's what toggled open).
    const id = window.setTimeout(() => {
      document.addEventListener('mousedown', onDocClick)
    }, 0)
    return () => {
      window.clearTimeout(id)
      document.removeEventListener('mousedown', onDocClick)
    }
  }, [open])

  // Close on Escape for keyboard parity with headlessui.
  useEffect(() => {
    if (!open) return
    function onKey(e: KeyboardEvent) {
      if (e.key === 'Escape') setOpen(false)
    }
    document.addEventListener('keydown', onKey)
    return () => document.removeEventListener('keydown', onKey)
  }, [open])

  // Dot colour and label share the same priority order (running > connected >
  // disconnected) so they never disagree.
  const statusColor = isRunning
    ? 'var(--color-accent-neutral)'
    : wsConnected
      ? 'var(--color-success)'
      : 'var(--color-muted-foreground)'
  const statusLabel = isRunning
    ? t('topbar.status.running')
    : wsConnected
      ? t('topbar.status.connected')
      : t('topbar.status.disconnected')

  // Terminal is a bottom panel tracked separately; the other three reflect
  // activePanel (the right panel).
  function isCurrent(panel: PanelType): boolean {
    if (panel === 'terminal') return terminalOpen
    return activePanel === panel
  }

  const panelsHint = t('topbar.panelsHint', { status: statusLabel })
  const panelsMenuLabel = t('topbar.panelsMenu', { status: statusLabel })

  return (
    <div
      ref={controlRef}
      className="absolute right-[14px] top-[6px] z-[46]"
      style={{ fontFamily: 'var(--font-sans)' }}
    >
      {/* position:relative wrapper so the absolute menu anchors to it, not to the
          floated outer div (matches the Vue inline style). */}
      <div className="relative inline-flex">
        <button
          type="button"
          aria-label={panelsMenuLabel}
          aria-expanded={open}
          title={panelsHint}
          onClick={() => {
            // Re-fetch on open so the diff stat is fresh when the user looks.
            void loadDiffStat()
            setOpen((v) => !v)
          }}
          className={`inline-flex h-[34px] items-center gap-[3px] rounded-[var(--radius-lg)] border px-[9px] transition-[background,color,border-color] duration-150 ${
            open
              ? 'bg-[var(--color-muted)] text-[var(--color-foreground)] border-[var(--color-foreground)]'
              : 'bg-[var(--color-background)] text-[var(--color-muted-foreground)] border-[var(--color-border)] hover:text-[var(--color-foreground)] hover:border-[var(--color-foreground)]'
          }`}
        >
          <RectangleStackIcon className="h-4 w-4" />
          <ChevronDownIcon className="h-3 w-3 opacity-60" />
          {/* Live status dot, pinned to the button corner with a shell-tone
              border so it reads as a separate indicator. */}
          <span
            aria-hidden="true"
            className="absolute right-1 top-1 h-[7px] w-[7px] rounded-[var(--radius-pill)]"
            style={{ backgroundColor: statusColor, border: '1.5px solid var(--color-background)' }}
          />
        </button>

        {open && (
          <div
            role="menu"
            className="absolute right-0 top-[calc(100%+6px)] z-[var(--z-dropdown)] min-w-[224px] rounded-[var(--radius-lg)] border border-[var(--color-border)] bg-[var(--color-surface)] p-1 shadow-[var(--shadow-md)] outline-none"
          >
            {PANEL_BUTTONS.map((btn) => {
              const Icon = PANEL_ICONS[btn.panel]
              const current = isCurrent(btn.panel)
              return (
                <button
                  key={btn.panel}
                  type="button"
                  role="menuitem"
                  aria-current={current ? 'true' : undefined}
                  onClick={() => {
                    setOpen(false)
                    onTogglePanel(btn.panel)
                  }}
                  className={`flex w-full items-center gap-2.5 rounded-[var(--radius-md)] px-2 py-[7px] text-left text-[13px] text-[var(--color-foreground)] transition-colors hover:bg-[var(--color-muted)] ${
                    current ? 'bg-[var(--color-muted)]' : ''
                  }`}
                >
                  <Icon
                    className={`h-4 w-4 shrink-0 ${
                      current ? 'text-[var(--color-accent-neutral)]' : 'text-[var(--color-muted-foreground)]'
                    }`}
                  />
                  <span className="flex-1 whitespace-nowrap">{t(`topbar.${btn.panel}`)}</span>
                  {btn.panel === 'changes' && diffStat && (
                    <span
                      className="inline-flex shrink-0 items-center gap-1.5 font-mono text-[11px]"
                      style={{ fontFamily: 'var(--font-mono)' }}
                    >
                      <span style={{ color: 'var(--color-success-fg)' }}>+{diffStat.additions}</span>
                      <span style={{ color: 'var(--color-error-fg)' }}>-{diffStat.deletions}</span>
                    </span>
                  )}
                  <span className="shrink-0 text-[11px] tracking-[0.04em] text-[var(--color-muted-foreground)]">
                    {btn.shortcut}
                  </span>
                </button>
              )
            })}
          </div>
        )}
      </div>
    </div>
  )
}
