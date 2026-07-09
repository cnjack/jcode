/**
 * ChatInput — styled composer (wraps headless Composer primitive).
 *
 * Renders the autosizing textarea, the send/stop button (swaps on isRunning),
 * the type-ahead queue chips, slash-command dropdown, and image attachments.
 * App-specific pickers (model/mode/workspace) are NOT included — the product
 * app layers those on top. This keeps the component reusable across agents.
 */

import { memo } from 'react'
import {
  PaperAirplaneIcon,
  StopCircleIcon,
  XMarkIcon,
} from '@heroicons/react/24/outline'
import { Composer } from 'jcode-ui-core/primitives'
import type { SlashCommand } from 'jcode-ui-core/primitives'
import { ContextBar } from './ContextBar.js'

export interface ChatInputProps {
  /** Slash commands (host-fetched). */
  slashCommands?: SlashCommand[]
  /** Allow image attachments (gated by model vision support). */
  allowImages?: boolean
  /** Placeholder text. */
  placeholder?: string
  /** Show the context bar suffix. Default true. */
  showContextBar?: boolean
  /** Callback after a message is sent/queued (host snaps timeline to bottom). */
  onSent?: () => void
}

export const ChatInput = memo(function ChatInput({
  slashCommands,
  allowImages = false,
  placeholder = 'Send a message…',
  showContextBar = true,
  onSent,
}: ChatInputProps) {
  return (
    <Composer
      slashCommands={slashCommands}
      allowImages={allowImages}
      placeholder={placeholder}
      onSent={onSent}
      className="jcode-chat-input"
      renderQueue={(queued) =>
        queued.length > 0 ? (
          <div className="mb-1 flex flex-wrap gap-1">
            {queued.map((q) => (
              <span
                key={q.id}
                className="inline-flex items-center gap-1 rounded-[var(--radius-pill)] bg-[var(--accent-wash)] px-2 py-0.5 text-[0.72rem] text-[var(--color-foreground)]"
              >
                <span className="max-w-[200px] truncate">{q.text}</span>
              </span>
            ))}
          </div>
        ) : null
      }
      renderSlashMenu={(state) =>
        state.open ? (
          <div className="mb-1 max-h-64 overflow-auto rounded-[var(--radius-lg)] border border-[var(--color-border)] bg-[var(--color-surface)] py-1 shadow-[var(--shadow-md)]">
            {state.commands.map((cmd, i) => (
              <button
                key={cmd.slash}
                type="button"
                onMouseEnter={() => {
                  // selection highlight is driven by keyboard; clicking applies
                }}
                onClick={() => state.apply(cmd)}
                className={`flex w-full items-center gap-2 px-3 py-1.5 text-left text-[0.8rem] ${
                  i === state.activeIndex
                    ? 'bg-[var(--accent-wash)] text-[var(--color-foreground)]'
                    : 'text-[var(--color-foreground)] hover:bg-[var(--neutral-wash-soft)]'
                }`}
              >
                <code className="font-mono text-[var(--color-primary)]">{cmd.slash}</code>
                {cmd.description && <span className="truncate text-[var(--color-muted-foreground)]">{cmd.description}</span>}
              </button>
            ))}
          </div>
        ) : null
      }
      renderAttachments={(imgs, remove) => (
        <div className="mt-1 flex flex-wrap gap-1">
          {imgs.map((img, i) => (
            <div key={i} className="relative">
              <img
                src={`data:${img.media_type};base64,${img.data}`}
                alt={`attachment ${i + 1}`}
                className="h-16 w-16 rounded-[var(--radius-md)] border border-[var(--color-border)] object-cover"
              />
              <button
                type="button"
                onClick={() => remove(i)}
                className="absolute -right-1 -top-1 rounded-full bg-[var(--color-surface)] p-0.5 text-[var(--color-muted-foreground)] shadow-[var(--shadow-sm)] hover:text-[var(--color-foreground)]"
                aria-label="Remove attachment"
              >
                <XMarkIcon className="h-3 w-3" />
              </button>
            </div>
          ))}
        </div>
      )}
      renderSubmitButton={(mode, disabled) =>
        mode === 'send' ? (
          <button
            type="button"
            disabled={disabled}
            className="flex h-8 w-8 shrink-0 items-center justify-center rounded-[var(--radius-md)] bg-[var(--color-primary)] text-[var(--color-on-primary)] transition-colors hover:bg-[var(--accent-wash-strong)] disabled:opacity-40"
            aria-label="Send message"
          >
            <PaperAirplaneIcon className="h-4 w-4" />
          </button>
        ) : (
          <button
            type="button"
            className="flex h-8 w-8 shrink-0 items-center justify-center rounded-[var(--radius-md)] bg-[var(--color-muted)] text-[var(--color-foreground)] transition-colors hover:bg-[var(--neutral-wash-soft)]"
            aria-label="Stop"
          >
            <StopCircleIcon className="h-4 w-4" />
          </button>
        )
      }
      renderSuffix={showContextBar ? () => <ContextBar size={20} /> : undefined}
    />
  )
})
