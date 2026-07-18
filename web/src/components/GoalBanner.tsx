/**
 * GoalBanner — floating pill behind the composer.
 *
 *   ┌──────────────────────────────────────────────────────────────┐
 *   │ [icon] 进行中 · 目标文本截断…      3/5 ▰▰▰▱ 18h 34m 52s ✎ 🗑 › │  ← floating pill, z-index below composer
 *   └──────────────────────────────────────────────────────────────┘
 *   ╭──────────────────────────────────────────────────────────────╮
 *   │                                                              │
 *   │  Composer (on top of pill)                                   │
 *   │                                                              │
 *   ╰──────────────────────────────────────────────────────────────╯
 *
 * The pill overlaps the composer by ~14px; the composer sits on top
 * (higher z-index) so the pill looks like a tag peeking from behind.
 * Status icon is a simple line icon with a tiny active dot; the expand
 * arrow points right (ChevronRight) and rotates down when expanded.
 */

import { useEffect, useMemo, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { useTranslation } from 'react-i18next'
import {
  CheckCircleIcon,
  ChevronRightIcon,
  ExclamationTriangleIcon,
  PencilSquareIcon,
  TrashIcon,
  ViewfinderCircleIcon,
  XMarkIcon,
} from '@heroicons/react/24/outline'
import { useAppDispatch, useAppSelector } from '../app/hooks'
import { chatActions } from '../app/store'
import { api } from '../lib/api'
import type { GoalStatus } from '../lib/types'

// ─── Status presentation ────────────────────────────────────────────────────

const STATUS_STYLE: Record<GoalStatus, { labelKey: string; Icon: typeof ViewfinderCircleIcon }> = {
  active: {
    labelKey: 'goal.status.active',
    Icon: ViewfinderCircleIcon,
  },
  complete: {
    labelKey: 'goal.status.completed',
    Icon: CheckCircleIcon,
  },
  blocked: {
    labelKey: 'goal.status.blocked',
    Icon: ExclamationTriangleIcon,
  },
}

// ─── Component ──────────────────────────────────────────────────────────────

export function GoalBanner() {
  const { t } = useTranslation()
  const dispatch = useAppDispatch()
  const goal = useAppSelector((s) => s.chat.goal)
  const todos = useAppSelector((s) => s.chat.todos)
  const [expanded, setExpanded] = useState(false)
  const [editing, setEditing] = useState(false)

  // Live elapsed clock — 1s tick only while the goal is active.
  const isActive = goal?.status === 'active'
  const [nowMs, setNowMs] = useState(() => Date.now())
  useEffect(() => {
    if (!isActive) return
    const id = window.setInterval(() => setNowMs(Date.now()), 1000)
    return () => window.clearInterval(id)
  }, [isActive])

  const { doneCount, totalCount, progressPct } = useMemo(() => {
    const done = todos.filter((todo) => todo.status === 'completed' || todo.status === 'cancelled').length
    const total = todos.length
    return {
      doneCount: done,
      totalCount: total,
      progressPct: total > 0 ? Math.round((done / total) * 100) : 0,
    }
  }, [todos])

  if (!goal) return null

  const style = STATUS_STYLE[goal.status] ?? STATUS_STYLE.active
  const statusLabel = STATUS_STYLE[goal.status]
    ? t(style.labelKey)
    : String(goal.status || '').replace(/_/g, ' ')

  const startMs = (goal.created_at ?? 0) * 1000
  const hasTimes = startMs > 0
  const endMs = isActive ? nowMs : (goal.updated_at ?? 0) * 1000
  const elapsedSec = hasTimes ? Math.max(0, Math.floor((endMs - startMs) / 1000)) : 0
  const elapsedLabel = hasTimes ? formatElapsed(elapsedSec, t) : ''

  const used = goal.tokens_used ?? 0
  const tokensLabel =
    used <= 0 ? '' : used < 1000 ? t('goal.tokens', { used }) : t('goal.tokensK', { k: (used / 1000).toFixed(1) })

  async function clear() {
    try {
      await api.clearGoal()
    } catch {
      // still clear local so the banner dismisses
    }
    dispatch(chatActions.setGoal(null))
  }

  const showProgress = isActive && totalCount > 0
  const statusColor =
    goal.status === 'complete'
      ? 'var(--color-success-fg)'
      : goal.status === 'blocked'
        ? 'var(--color-error-fg)'
        : 'var(--color-foreground)'

  return (
    <div
      className={`goal-pill relative mx-auto -mb-3.5 w-[calc(100%-12px)] rounded-2xl border px-3.5 py-3 pb-3.5 transition-[box-shadow,transform,z-index] hover:-translate-y-px ${expanded ? 'z-[3]' : 'z-[1]'}`}
      style={{
        background: 'var(--color-surface)',
        borderColor: 'color-mix(in srgb, var(--color-border) 55%, transparent)',
        boxShadow: '0 4px 14px -4px rgba(24, 20, 16, 0.12)',
      }}
    >
      {/* Collapsed row */}
      <div
        className="flex cursor-pointer items-center gap-2"
        role="button"
        tabIndex={0}
        aria-expanded={expanded}
        onClick={() => setExpanded((v) => !v)}
        onKeyDown={(e) => {
          if (e.key === 'Enter' || e.key === ' ') {
            e.preventDefault()
            setExpanded((v) => !v)
          }
        }}
      >
        {/* Leading icon + active dot */}
        <span className="relative shrink-0 text-[var(--color-muted-foreground)]">
          <style.Icon className="h-[15px] w-[15px]" strokeWidth={1.8} />
          {isActive && (
            <span
              aria-hidden="true"
              className="absolute -right-1 -top-1 h-[6px] w-[6px] animate-pulse rounded-full motion-reduce:animate-none"
              style={{ background: 'var(--color-primary)', border: '1.5px solid var(--color-surface)' }}
            />
          )}
        </span>

        {/* Status label + objective (single truncated line) */}
        <span className="min-w-0 flex-1 truncate text-[12.5px] leading-none">
          <span className="mr-1.5 font-semibold" style={{ color: statusColor }}>
            {statusLabel}
          </span>
          <span
            className={
              goal.status === 'active'
                ? 'text-[var(--color-foreground)]'
                : 'text-[var(--color-muted-foreground)]'
            }
          >
            {goal.objective}
          </span>
        </span>

        {/* Inline todo progress */}
        {showProgress && (
          <span className="inline-flex shrink-0 items-center gap-1.5">
            <span className="font-mono text-[10px] tabular-nums text-[var(--color-muted-foreground)]">
              {doneCount}/{totalCount}
            </span>
            <span
              className="h-[3px] w-11 overflow-hidden rounded-full"
              style={{ background: 'color-mix(in srgb, var(--color-foreground) 10%, transparent)' }}
            >
              <span
                className="block h-full rounded-full transition-[width] duration-500 ease-out motion-reduce:transition-none"
                style={{ width: `${progressPct}%`, background: 'var(--color-primary)' }}
              />
            </span>
          </span>
        )}

        {/* Elapsed */}
        {elapsedLabel && (
          <span
            className="shrink-0 font-mono text-[10px] tabular-nums text-[var(--color-muted-foreground)]"
            title={t('goal.elapsed')}
          >
            {elapsedLabel}
          </span>
        )}

        {/* Row actions — stop propagation so they don't toggle the pane */}
        <span className="flex shrink-0 items-center gap-0.5">
          <button
            type="button"
            title={t('goal.edit')}
            aria-label={t('goal.edit')}
            onClick={(e) => {
              e.stopPropagation()
              setEditing(true)
            }}
            className="grid h-[22px] w-[22px] place-items-center rounded-md border-none bg-transparent text-[var(--color-muted-foreground)] transition-colors hover:bg-[var(--color-muted)] hover:text-[var(--color-foreground)]"
          >
            <PencilSquareIcon className="h-3.5 w-3.5" />
          </button>
          <button
            type="button"
            title={t('goal.clearGoal')}
            aria-label={t('goal.clearGoal')}
            onClick={(e) => {
              e.stopPropagation()
              void clear()
            }}
            className="grid h-[22px] w-[22px] place-items-center rounded-md border-none bg-transparent text-[var(--color-muted-foreground)] transition-colors hover:bg-[var(--color-muted)] hover:text-[var(--color-error-fg)]"
          >
            <TrashIcon className="h-3.5 w-3.5" />
          </button>
          <span
            className={`grid h-[22px] w-[22px] place-items-center text-[var(--color-muted-foreground)] transition-transform duration-200 ${expanded ? 'rotate-90' : ''}`}
          >
            <ChevronRightIcon className="h-3.5 w-3.5" />
          </span>
        </span>
      </div>

      {/* Expanded detail */}
      <div
        className={`grid transition-[grid-template-rows] duration-200 ease-out motion-reduce:transition-none ${
          expanded ? 'grid-rows-[1fr]' : 'grid-rows-[0fr]'
        }`}
      >
        <div className="overflow-hidden">
          <div
            className="border-t px-0.5 pb-1 pt-2.5"
            style={{ borderColor: 'color-mix(in srgb, var(--color-border) 60%, transparent)' }}
          >
            <div className="max-h-[180px] overflow-y-auto whitespace-pre-wrap break-words text-xs leading-relaxed text-[var(--color-foreground)]">
              {goal.objective}
            </div>

            {/* Meta line */}
            <div className="mt-2 flex flex-wrap items-center gap-x-3 gap-y-1 text-[10px] text-[var(--color-muted-foreground)]">
              {hasTimes && (
                <>
                  <span>
                    {t('goal.started')} {new Date(startMs).toLocaleString()}
                  </span>
                  <span>
                    {t('goal.elapsed')} {elapsedLabel}
                  </span>
                </>
              )}
              {tokensLabel && <span className="font-mono">{tokensLabel}</span>}
            </div>

            {/* Labeled actions */}
            <div className="mt-2.5 flex items-center gap-2">
              <button
                type="button"
                onClick={() => setEditing(true)}
                className="inline-flex h-7 items-center gap-1.5 rounded-md border border-[var(--color-border)] bg-[var(--color-surface)] px-2.5 text-[11px] font-medium text-[var(--color-foreground)] transition-colors hover:bg-[var(--color-muted)]"
              >
                <PencilSquareIcon className="h-3.5 w-3.5" />
                {t('goal.edit')}
              </button>
              <button
                type="button"
                onClick={() => void clear()}
                className="inline-flex h-7 items-center gap-1.5 rounded-md border-none bg-transparent px-2.5 text-[11px] font-medium text-[var(--color-muted-foreground)] transition-colors hover:bg-[var(--color-muted)] hover:text-[var(--color-error-fg)]"
              >
                <TrashIcon className="h-3.5 w-3.5" />
                {t('goal.clear')}
              </button>
              <button
                type="button"
                onClick={() => setExpanded(false)}
                className="ml-auto inline-flex h-7 items-center gap-1 rounded-md border-none bg-transparent px-2 text-[11px] text-[var(--color-muted-foreground)] transition-colors hover:bg-[var(--color-muted)] hover:text-[var(--color-foreground)]"
              >
                <ChevronRightIcon className="h-3.5 w-3.5 rotate-90" />
                {t('goal.collapse')}
              </button>
            </div>
          </div>
        </div>
      </div>

      {editing && <EditGoalDialog initial={goal.objective} onClose={() => setEditing(false)} />}
    </div>
  )
}

// ─── Edit dialog ────────────────────────────────────────────────────────────

function EditGoalDialog({ initial, onClose }: { initial: string; onClose: () => void }) {
  const { t } = useTranslation()
  const dispatch = useAppDispatch()
  const [text, setText] = useState(initial)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')
  const textareaRef = useRef<HTMLTextAreaElement>(null)

  useEffect(() => {
    textareaRef.current?.focus()
    textareaRef.current?.setSelectionRange(textareaRef.current.value.length, textareaRef.current.value.length)
  }, [])

  useEffect(() => {
    function onKey(e: KeyboardEvent) {
      if (e.key === 'Escape') onClose()
    }
    document.addEventListener('keydown', onKey)
    return () => document.removeEventListener('keydown', onKey)
  }, [onClose])

  const canSave = text.trim().length > 0 && !saving

  async function save() {
    if (!canSave) return
    setSaving(true)
    setError('')
    try {
      // start=false: editing the objective must not kick off another agent run.
      const goal = await api.setGoal(text.trim(), false)
      dispatch(chatActions.setGoal(goal))
      onClose()
    } catch (err) {
      setError(err instanceof Error ? err.message : t('goal.saveFailed'))
    } finally {
      setSaving(false)
    }
  }

  return createPortal(
    <div
      className="fixed inset-0 z-[var(--z-modal)] flex items-center justify-center bg-[var(--backdrop)]"
      style={{ backdropFilter: 'blur(6px)', WebkitBackdropFilter: 'blur(6px)' }}
      onClick={onClose}
    >
      <div
        role="dialog"
        aria-modal="true"
        aria-label={t('goal.editTitle')}
        onClick={(e) => e.stopPropagation()}
        className="m-4 flex min-w-0 flex-col overflow-hidden rounded-[var(--radius-xl)] border border-[var(--color-border)] bg-[var(--color-surface)] shadow-[var(--shadow-lg)]"
        style={{ width: 'min(520px, 94vw)' }}
      >
        {/* Header */}
        <div className="flex items-center gap-3 border-b border-[var(--color-border)] px-[18px] py-4">
          <div className="grid h-[30px] w-[30px] shrink-0 place-items-center rounded-[var(--radius-md)] bg-[var(--accent-wash)] text-[var(--color-primary)]">
            <ViewfinderCircleIcon className="h-4 w-4" />
          </div>
          <h3 className="m-0 flex-1 text-sm font-semibold tracking-[-0.01em] text-[var(--color-foreground)]">
            {t('goal.editTitle')}
          </h3>
          <button
            type="button"
            aria-label={t('common.close')}
            onClick={onClose}
            className="grid h-7 w-7 shrink-0 place-items-center rounded-[var(--radius-md)] border border-transparent bg-transparent text-[var(--color-muted-foreground)] transition-colors hover:border-[var(--color-border)] hover:bg-[var(--color-muted)] hover:text-[var(--color-foreground)]"
          >
            <XMarkIcon className="h-4 w-4" />
          </button>
        </div>

        {/* Body */}
        <div className="px-[18px] py-4">
          <textarea
            ref={textareaRef}
            value={text}
            onChange={(e) => setText(e.target.value)}
            rows={6}
            className="w-full resize-y rounded-[var(--radius-md)] border border-[var(--color-border)] bg-[var(--color-muted)] px-3 py-2.5 text-[13px] leading-relaxed text-[var(--color-foreground)] outline-none transition-colors focus:border-[var(--color-primary)]"
          />
          <p className="mt-2 text-[11px] leading-relaxed text-[var(--color-muted-foreground)]">
            {t('chat.goalHint.replace')}
          </p>
          {error && <p className="mt-2 text-[11px] text-[var(--color-error-fg)]">{error}</p>}
        </div>

        {/* Footer */}
        <div className="flex justify-end gap-2 border-t border-[var(--color-border)] bg-[var(--color-muted)] px-[18px] py-3">
          <button
            type="button"
            onClick={onClose}
            className="inline-flex h-[30px] items-center rounded-[var(--radius-md)] border border-[var(--color-border)] bg-[var(--color-surface)] px-3.5 text-xs font-medium text-[var(--color-foreground)] transition-colors hover:bg-[var(--color-muted)]"
          >
            {t('common.cancel')}
          </button>
          <button
            type="button"
            disabled={!canSave}
            onClick={() => void save()}
            className="inline-flex h-[30px] items-center gap-1.5 rounded-[var(--radius-md)] border border-transparent bg-[var(--color-primary)] px-3.5 text-xs font-medium text-[var(--color-on-primary)] transition-colors enabled:hover:opacity-90 disabled:cursor-not-allowed disabled:opacity-45"
          >
            {saving ? t('common.loading') : t('common.save')}
          </button>
        </div>
      </div>
    </div>,
    document.body,
  )
}

// ─── Helpers ────────────────────────────────────────────────────────────────

function formatElapsed(totalSec: number, t: (key: string, opts?: Record<string, unknown>) => string): string {
  const s = Math.max(0, totalSec)
  if (s < 60) return t('chat.durationSeconds', { n: s })
  const m = Math.floor(s / 60)
  if (m < 60) return t('chat.durationMinutes', { m, s: s % 60 })
  const h = Math.floor(m / 60)
  return t('goal.durationHours', { h, m: m % 60 })
}
