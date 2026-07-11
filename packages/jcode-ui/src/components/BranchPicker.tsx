/**
 * BranchPicker — `‹ 2/3 ›` version stepper for a branched message.
 *
 * Edit/regenerate branches live on `Message.versions`; `Message.content` always
 * mirrors the active version. This control steps `activeVersionId` across the
 * `versions` array via `actions.switchVersion`.
 *
 * Fail-visible: renders nothing unless there is more than one version AND the
 * host wired `switchVersion` — never a dead stepper.
 */

import { memo, useCallback } from 'react'
import { ChevronLeftIcon, ChevronRightIcon } from '@heroicons/react/24/outline'
import type { Message as MessageData } from 'jcode-ui-core'
import { useRuntimeActions } from 'jcode-ui-core/runtime'

export interface BranchPickerProps {
  message: MessageData
}

export const BranchPicker = memo(function BranchPicker({ message }: BranchPickerProps) {
  const actions = useRuntimeActions()
  const versions = message.versions
  const switchVersion = actions.switchVersion

  // Resolve the visible index. Fall back to the newest branch when the host
  // hasn't stamped activeVersionId (a fresh regenerate is the latest entry).
  const activeIndex = (() => {
    if (!versions || versions.length === 0) return -1
    const i = versions.findIndex((v) => v.id === message.activeVersionId)
    return i >= 0 ? i : versions.length - 1
  })()

  const go = useCallback(
    (delta: number) => {
      if (!versions || !switchVersion) return
      const next = activeIndex + delta
      if (next < 0 || next >= versions.length) return
      switchVersion(message.id, versions[next].id)
    },
    [activeIndex, message.id, switchVersion, versions],
  )

  const goPrev = useCallback(() => go(-1), [go])
  const goNext = useCallback(() => go(1), [go])

  if (!versions || versions.length <= 1 || !switchVersion) return null

  const human = activeIndex + 1
  const total = versions.length

  return (
    <div
      data-jcode-ui=""
      className="jcode-branch-picker"
      role="group"
      aria-label={`Version ${human} of ${total}`}
    >
      <button
        type="button"
        className="jcode-branch-btn"
        onClick={goPrev}
        disabled={activeIndex <= 0}
        title="Previous version"
        aria-label="Previous version"
      >
        <ChevronLeftIcon className="h-3.5 w-3.5" />
      </button>
      <span className="jcode-branch-label" aria-hidden>
        {human}/{total}
      </span>
      <button
        type="button"
        className="jcode-branch-btn"
        onClick={goNext}
        disabled={activeIndex >= total - 1}
        title="Next version"
        aria-label="Next version"
      >
        <ChevronRightIcon className="h-3.5 w-3.5" />
      </button>
    </div>
  )
})
