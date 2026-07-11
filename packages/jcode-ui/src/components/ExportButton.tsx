/**
 * ExportButton — download the current conversation as markdown.
 *
 * Reads the timeline from the runtime and serializes via
 * `exportThreadMarkdown` (core, pure). Renders nothing while the thread is
 * empty — an export of nothing is noise.
 */

import { memo, useCallback } from 'react'
import { ArrowDownTrayIcon } from '@heroicons/react/24/outline'
import { exportThreadMarkdown } from 'jcode-ui-core'
import { useRuntimeState } from 'jcode-ui-core/runtime'

export interface ExportButtonProps {
  /** Download filename. Default `conversation.md`. */
  filename?: string
  /** Document title inside the markdown. */
  title?: string
  className?: string
}

export const ExportButton = memo(function ExportButton({ filename, title, className }: ExportButtonProps) {
  const { items } = useRuntimeState()

  const download = useCallback(() => {
    const md = exportThreadMarkdown(items, { title, now: new Date() })
    const blob = new Blob([md], { type: 'text/markdown;charset=utf-8' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = filename ?? 'conversation.md'
    a.click()
    URL.revokeObjectURL(url)
  }, [items, filename, title])

  if (!items.length) return null
  return (
    <button
      data-jcode-ui=""
      type="button"
      onClick={download}
      title="Export conversation as Markdown"
      aria-label="Export conversation as Markdown"
      className={`jcode-export-btn ${className ?? ''}`}
    >
      <ArrowDownTrayIcon className="h-3.5 w-3.5" />
      <span>Export</span>
    </button>
  )
})
