/** GoalBanner — renders the active goal (set via /goal). Reads from the chat slice. */

import { CheckCircleIcon, ExclamationTriangleIcon } from '@heroicons/react/24/outline'
import { useAppSelector } from '../app/hooks'

export function GoalBanner() {
  const goal = useAppSelector((s) => s.chat.goal)
  if (!goal) return null
  const Icon = goal.status === 'complete' ? CheckCircleIcon : goal.status === 'blocked' ? ExclamationTriangleIcon : null
  const tint =
    goal.status === 'complete'
      ? 'border-[var(--color-success)] bg-[var(--color-success-bg)] text-[var(--color-success-fg)]'
      : goal.status === 'blocked'
        ? 'border-[var(--color-error-fg)] bg-[var(--color-error-bg)] text-[var(--color-error-fg)]'
        : 'border-[var(--color-primary)] bg-[var(--accent-wash)] text-[var(--color-foreground)]'
  return (
    <div className={`flex items-center gap-2 border-b px-4 py-1.5 text-xs ${tint}`}>
      {Icon && <Icon className="h-3.5 w-3.5 shrink-0" />}
      <span className="truncate">{goal.objective}</span>
      {goal.tokens_used != null && (
        <span className="ml-auto shrink-0 font-mono opacity-70">{goal.tokens_used} tok</span>
      )}
    </div>
  )
}
