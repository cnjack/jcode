/**
 * ComputerShotPiP — Codex-style picture-in-picture for computer use.
 *
 * Floats top-right (under the TopBar) and always shows the LATEST screenshot
 * the agent captured in this session, so the user can watch what the agent is
 * seeing without scrolling the thread. Derives its data from the chat timeline:
 * any tool result whose output carries an `image_ref=/api/computer/shots/…`
 * marker (computer_screenshot) — no extra WS channel needed, so it also
 * works when replaying a historical session.
 *
 * The card collapses to a small pill (Codex's 显示/Show toggle). "Open full"
 * expands the image IN PLACE inside the card (wider card, taller frame) rather
 * than navigating to a new tab — the raw link used to be a no-op target in the
 * Tauri shell and a jarring context switch in the browser. Expired shots (the
 * backend shot store is TTL-bound) render a note instead of a broken img.
 */

import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import {
  ArrowsPointingInIcon,
  ArrowsPointingOutIcon,
  ChevronUpIcon,
  ComputerDesktopIcon,
} from '@heroicons/react/24/outline'
import { useAppSelector } from '../app/hooks'
import { apiBase } from '../lib/apiBase'

const SHOT_RE = /image_ref=(\/api\/computer\/shots\/[\w-]+\.png)/

export function ComputerShotPiP() {
  const { t } = useTranslation()
  const timeline = useAppSelector((s) => s.chat.timeline)
  const [collapsed, setCollapsed] = useState(false)
  /** In-card enlarged view of the latest shot (replaces external-tab open). */
  const [expanded, setExpanded] = useState(false)
  /** ref that failed to load — keyed so a NEW shot clears the error state. */
  const [brokenRef, setBrokenRef] = useState('')

  // Latest shot wins; count every shot so the header can show "n shots".
  const latest = useMemo(() => {
    let ref = ''
    let ts = 0
    let count = 0
    for (const item of timeline) {
      if (item.kind !== 'tool') continue
      const m = (item.data.output ?? '').match(SHOT_RE)
      if (m) {
        count++
        ref = m[1]
        ts = item.data.timestamp
      }
    }
    return ref ? { ref, ts, count } : null
  }, [timeline])

  if (!latest) return null

  const src = `${apiBase}${latest.ref}`
  const broken = brokenRef === latest.ref
  const title = t('pip.computerUse')

  if (collapsed) {
    return (
      <button
        type="button"
        onClick={() => setCollapsed(false)}
        title={t('pip.expand')}
        className="fixed right-[14px] top-[96px] z-[45] inline-flex items-center gap-1.5 rounded-[var(--radius-pill)] border border-[var(--color-border)] bg-[var(--color-surface)] px-2.5 py-1 text-[11px] font-medium text-[var(--color-foreground)] shadow-[var(--shadow-md)] transition-colors hover:bg-[var(--color-muted)]"
      >
        <ComputerDesktopIcon className="h-3.5 w-3.5 text-[var(--color-primary)]" />
        {title}
        <span className="font-mono text-[10px] text-[var(--color-muted-foreground)]">{latest.count}</span>
      </button>
    )
  }

  const ExpandIcon = expanded ? ArrowsPointingInIcon : ArrowsPointingOutIcon

  return (
    <div
      className="fixed right-[14px] top-[96px] z-[45] overflow-hidden rounded-[var(--radius-xl)] border border-[var(--color-border)] bg-[var(--color-surface)] shadow-[var(--shadow-lg)] transition-[width] duration-200"
      style={{ width: expanded ? 'min(520px, calc(100vw - 28px))' : 240 }}
    >
      {/* Header */}
      <div className="flex items-center gap-1.5 border-b border-[var(--color-border)] px-2.5 py-1.5">
        <ComputerDesktopIcon className="h-3.5 w-3.5 shrink-0 text-[var(--color-primary)]" />
        <span className="min-w-0 flex-1 truncate text-[11px] font-semibold text-[var(--color-foreground)]">
          {title}
        </span>
        <span className="shrink-0 rounded-[var(--radius-pill)] bg-[var(--neutral-wash)] px-1.5 py-px font-mono text-[10px] text-[var(--color-muted-foreground)]">
          {t('pip.shots', { n: latest.count })}
        </span>
        <button
          type="button"
          onClick={() => setExpanded((v) => !v)}
          title={expanded ? t('pip.shrink') : t('pip.openFull')}
          aria-label={expanded ? t('pip.shrink') : t('pip.openFull')}
          className="grid h-5 w-5 shrink-0 place-items-center rounded-[var(--radius-sm)] text-[var(--color-muted-foreground)] transition-colors hover:bg-[var(--color-muted)] hover:text-[var(--color-foreground)]"
        >
          <ExpandIcon className="h-3 w-3" />
        </button>
        <button
          type="button"
          onClick={() => setCollapsed(true)}
          title={t('pip.collapse')}
          className="grid h-5 w-5 shrink-0 place-items-center rounded-[var(--radius-sm)] text-[var(--color-muted-foreground)] transition-colors hover:bg-[var(--color-muted)] hover:text-[var(--color-foreground)]"
        >
          <ChevronUpIcon className="h-3 w-3" />
        </button>
      </div>

      {/* Body — latest frame; click to toggle the in-card enlarged view. */}
      {broken ? (
        <div className="grid h-[120px] place-items-center px-3 text-center text-[11px] text-[var(--color-muted-foreground)]">
          {t('pip.unavailable')}
        </div>
      ) : (
        <button
          type="button"
          onClick={() => setExpanded((v) => !v)}
          title={expanded ? t('pip.shrink') : t('pip.openFull')}
          className="block w-full cursor-zoom-in border-none bg-transparent p-0"
          style={{ cursor: expanded ? 'zoom-out' : 'zoom-in' }}
        >
          <img
            key={latest.ref}
            src={src}
            alt={title}
            onError={() => setBrokenRef(latest.ref)}
            className="w-full object-cover object-top"
            style={{ maxHeight: expanded ? 'min(70vh, 460px)' : 160 }}
          />
        </button>
      )}

      {/* Footer — capture time */}
      <div className="flex items-center justify-between px-2.5 py-1 text-[10px] text-[var(--color-muted-foreground)]">
        <span>{t('pip.latest')}</span>
        <span className="font-mono">
          {latest.ts > 0 ? new Date(latest.ts).toLocaleTimeString() : '—'}
        </span>
      </div>
    </div>
  )
}
