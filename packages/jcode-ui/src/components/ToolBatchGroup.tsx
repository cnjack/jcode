/**
 * ToolBatchGroupCard — concurrent tool calls from one assistant message
 * (same `batchId`), rendered Claude-Code-style.
 *
 * - Explorative batch (all read/search/list): upgraded Exploring card with a
 *   category-count summary (delegates to ExploringGroupCard).
 * - Mixed batch: a flat row stack with NO group header. Each row = status
 *   icon + title + mono subtitle + right-side elapsed/exit badge + chevron,
 *   and expands independently to the tool's registry-rendered body via the
 *   shared ToolRow.
 *
 * @deprecated Superseded by ActivityGroupCard (`'activity'` items) — Thread no
 * longer produces `'batch'` items. Kept for external consumers that still feed
 * them directly.
 */

import { memo } from 'react'
import type { ToolBatchGroup } from 'jcode-ui-core'
import { ExploringGroupCard } from './ExploringGroupCard.js'
import { ToolRow } from './ToolRow.js'

export interface ToolBatchGroupCardProps {
  group: ToolBatchGroup
  className?: string
}

/** @deprecated See module note — use ActivityGroupCard for new timelines. */
export const ToolBatchGroupCard = memo(function ToolBatchGroupCard({
  group,
  className,
}: ToolBatchGroupCardProps) {
  if (group.explorative) {
    // All members are read/search/list — render as the upgraded Exploring card.
    return <ExploringGroupCard group={group} className={className} />
  }
  return (
    <div
      data-jcode-ui=""
      className={`jcode-toolbatch my-1 ${className ?? ''}`}
      data-tool-name="batch"
      data-tool-status={group.status}
    >
      {group.tools.map((t) => (
        <ToolRow key={t.id} tool={t} />
      ))}
    </div>
  )
})
