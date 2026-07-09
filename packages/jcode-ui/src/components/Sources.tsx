/**
 * Sources — citation list for a message.
 *
 * Mirrors assistant-ui's Sources component. Renders a row of clickable source
 * chips (title + optional link), optionally expanding a snippet on click.
 */

import { useState } from 'react'
import { LinkIcon } from '@heroicons/react/24/outline'
import type { MessageSource } from 'jcode-ui-core'

export interface SourcesProps {
  sources: MessageSource[]
}

export function Sources({ sources }: SourcesProps) {
  const [openId, setOpenId] = useState<string | null>(null)
  if (!sources || sources.length === 0) return null
  return (
    <div className="jcode-sources mt-2.5 flex flex-wrap items-center gap-1.5">
      <span className="text-[0.68rem] font-medium uppercase tracking-wider text-[var(--color-muted-foreground)]">
        Sources
      </span>
      {sources.map((s, i) => (
        <span key={s.id} className="relative">
          <button
            type="button"
            onClick={() => setOpenId((id) => (id === s.id ? null : s.id))}
            className="inline-flex items-center gap-1 rounded-[var(--radius-pill)] border border-[var(--color-border)] bg-[var(--color-surface)] px-2.5 py-0.5 text-[0.72rem] text-[var(--color-foreground)] shadow-[var(--shadow-sm)] transition-all hover:border-[var(--accent-border)] hover:bg-[var(--accent-wash-soft)]"
          >
            {s.url && <LinkIcon className="h-2.5 w-2.5 text-[var(--color-primary)]" />}
            <span className="max-w-[180px] truncate">
              {i + 1}. {s.title}
            </span>
          </button>
          {openId === s.id && s.snippet && (
            <div className="absolute left-0 top-full z-[var(--z-dropdown)] mt-1.5 w-72 max-w-[80vw] animate-fade-up rounded-[var(--radius-xl)] border border-[var(--color-border)] bg-[var(--color-surface)] p-2.5 text-[0.74rem] shadow-[var(--shadow-lg)]">
              {s.url && (
                <a
                  href={s.url}
                  target="_blank"
                  rel="noreferrer"
                  className="mb-1 block truncate font-medium text-[var(--color-primary)]"
                >
                  {s.title}
                </a>
              )}
              <p className="leading-relaxed text-[var(--color-muted-foreground)]">{s.snippet}</p>
            </div>
          )}
        </span>
      ))}
    </div>
  )
}
