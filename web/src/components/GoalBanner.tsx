/**
 * GoalBanner — active goal display (set via /goal or the Goal toggle).
 * Ported from web/src/components/GoalBanner.vue: a rounded inset card with
 * target/status tint, status label, objective, and clear button — not a full-width
 * border-b strip.
 */

import { ViewfinderCircleIcon } from '@heroicons/react/24/outline'
import { useTranslation } from 'react-i18next'
import { useAppDispatch, useAppSelector } from '../app/hooks'
import { chatActions } from '../app/store'
import { api } from '../lib/api'

export function GoalBanner() {
  const { t } = useTranslation()
  const dispatch = useAppDispatch()
  const goal = useAppSelector((s) => s.chat.goal)
  if (!goal) return null

  const statusColor =
    goal.status === 'complete'
      ? 'var(--color-success-fg)'
      : goal.status === 'blocked'
        ? 'var(--color-destructive, var(--color-error-fg))'
        : 'var(--color-accent-neutral)'

  const statusLabel =
    goal.status === 'active'
      ? t('goal.status.active')
      : goal.status === 'complete'
        ? t('goal.status.completed')
        : goal.status === 'blocked'
          ? t('goal.status.blocked')
          : String(goal.status || '').replace(/_/g, ' ')

  const used = goal.tokens_used ?? 0
  const tokensLabel =
    used <= 0
      ? ''
      : used < 1000
        ? t('goal.tokens', { used })
        : t('goal.tokensK', { k: (used / 1000).toFixed(1) })

  async function clear() {
    try {
      await api.clearGoal()
    } catch {
      // still clear local so the banner dismisses
    }
    dispatch(chatActions.setGoal(null))
  }

  return (
    <div
      className="mt-2 flex items-start gap-2 rounded-md border px-3 py-2"
      style={{ borderColor: 'var(--color-border)', backgroundColor: 'var(--color-secondary)' }}
    >
      <ViewfinderCircleIcon className="mt-0.5 h-3.5 w-3.5 shrink-0" style={{ color: statusColor }} />
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-2">
          <span
            className="text-[10px] font-semibold uppercase tracking-wide"
            style={{ color: statusColor }}
          >
            {statusLabel}
          </span>
          {tokensLabel && (
            <span className="text-[10px]" style={{ color: 'var(--color-muted-foreground)' }}>
              {tokensLabel}
            </span>
          )}
        </div>
        <div className="mt-0.5 break-words text-xs" style={{ color: 'var(--color-foreground)' }}>
          {goal.objective}
        </div>
      </div>
      <button
        type="button"
        className="goal-clear shrink-0 cursor-pointer rounded px-2 py-1 text-[10px] transition-colors hover:bg-[var(--color-muted)] hover:text-[var(--color-foreground)]"
        style={{ backgroundColor: 'var(--color-background)', color: 'var(--color-muted-foreground)' }}
        title={t('goal.clearGoal')}
        onClick={() => void clear()}
      >
        {t('goal.clear')}
      </button>
    </div>
  )
}
