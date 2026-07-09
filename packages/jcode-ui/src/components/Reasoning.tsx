/**
 * Reasoning — collapsible model thinking / chain-of-thought block.
 *
 * Mirrors assistant-ui's Reasoning component. Renders an assistant message's
 * `reasoning` field (model thinking text) in a collapsed disclosure by default,
 * with markdown support. Toggle open to read the full chain-of-thought.
 */

import { useState } from 'react'
import { ChevronDownIcon } from '@heroicons/react/24/outline'
import { renderMarkdown } from '../lib/markdown.js'

export interface ReasoningProps {
  /** The reasoning text (markdown). */
  reasoning: string
  /** Default expanded. Default false. */
  defaultExpanded?: boolean
  /** Show a "Thought for Ns" label using the message duration. */
  durationMs?: number
}

export function Reasoning({ reasoning, defaultExpanded = false, durationMs }: ReasoningProps) {
  const [expanded, setExpanded] = useState(defaultExpanded)
  const label = durationMs != null ? `Thought for ${(durationMs / 1000).toFixed(1)}s` : 'Thought process'
  return (
    <div className="jcode-reasoning my-1.5">
      <button
        type="button"
        onClick={() => setExpanded((e) => !e)}
        className="jcode-reasoning__trigger flex items-center gap-1.5 rounded-[var(--radius-md)] px-2 py-1 text-[0.75rem] text-[var(--color-muted-foreground)] hover:bg-[var(--neutral-wash-soft)] hover:text-[var(--color-foreground)]"
        aria-expanded={expanded}
      >
        <ChevronDownIcon
          className={`h-3 w-3 transition-transform duration-[var(--duration-normal)] ${expanded ? 'rotate-180' : ''}`}
        />
        <span className="font-medium not-italic tracking-wide">{label}</span>
      </button>
      {expanded && (
        <div
          className="jcode-reasoning__body jcode-prose mt-1.5 border-l-2 border-[var(--color-border)] pl-3.5 text-[0.82rem] leading-relaxed text-[var(--color-muted-foreground)]"
          dangerouslySetInnerHTML={{ __html: renderMarkdown(reasoning) }}
        />
      )}
    </div>
  )
}
