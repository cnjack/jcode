/**
 * Suggestions — clickable prompt pills.
 *
 * Two homes: inside `<ThreadWelcome>` as starters, or under the last turn as
 * follow-ups (`<Thread suggestions={…}>`). Picking a pill sends its prompt
 * through the runtime by default; hosts can intercept with `onPick`.
 */

import { memo } from 'react'
import { useRuntimeActions } from 'jcode-ui-core/runtime'

export interface SuggestionItem {
  /** Stable key; defaults to the label. */
  id?: string
  /** Pill text. */
  label: string
  /** Message to send; defaults to the label. */
  prompt?: string
}

export interface SuggestionsProps {
  items: SuggestionItem[]
  /** Intercept picks (e.g. to prefill the composer instead of sending). */
  onPick?: (item: SuggestionItem) => void
  /** Compact single-line variant with horizontal scroll. */
  scroll?: boolean
  /** Disable all pills (e.g. while running). */
  disabled?: boolean
}

export const Suggestions = memo(function Suggestions({ items, onPick, scroll, disabled }: SuggestionsProps) {
  const actions = useRuntimeActions()
  if (!items.length) return null
  const pick = (item: SuggestionItem) => {
    if (onPick) onPick(item)
    else actions.sendMessage(item.prompt ?? item.label)
  }
  return (
    <div
      data-jcode-ui=""
      className={`jcode-suggestions${scroll ? ' jcode-suggestions--scroll' : ''}`}
      role="list"
      aria-label="Suggestions"
    >
      {items.map((s) => (
        <button
          key={s.id ?? s.label}
          type="button"
          role="listitem"
          className="jcode-suggestion"
          disabled={disabled}
          onClick={() => pick(s)}
        >
          {s.label}
        </button>
      ))}
    </div>
  )
})
