/**
 * ChatInput — styled composer (wraps headless Composer primitive).
 *
 * Floating surface with focus ring, queue chips, slash palette, attachments,
 * and a circular send/stop control. App-specific pickers (model/mode/workspace)
 * stay in the host product.
 *
 * Attachments follow assistant-ui's ComposerAttachments / ComposerAddAttachment
 * pattern, image-first via `ChatImage` + `AttachmentList`.
 */

import { memo } from 'react'
import {
  PaperAirplaneIcon,
  PaperClipIcon,
  StopIcon,
} from '@heroicons/react/24/outline'
import { Composer } from 'jcode-ui-core/primitives'
import type { SlashCommand } from 'jcode-ui-core/primitives'
import { AttachmentList } from './Attachment.js'
import { ContextBar } from './ContextBar.js'

export interface ChatInputProps {
  /** Slash commands (host-fetched). */
  slashCommands?: SlashCommand[]
  /** Allow image attachments (gated by model vision support). */
  allowImages?: boolean
  /** `accept` for the file picker. Default `image/*`. */
  acceptImages?: string
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
  acceptImages = 'image/*',
  placeholder = 'Send a message…',
  showContextBar = true,
  onSent,
}: ChatInputProps) {
  return (
    <Composer
      slashCommands={slashCommands}
      allowImages={allowImages}
      acceptImages={acceptImages}
      placeholder={placeholder}
      onSent={onSent}
      className="jcode-chat-input"
      renderQueue={(queued) =>
        queued.length > 0 ? (
          <div className="jcode-chat-input__queue mb-2 flex flex-wrap gap-1.5">
            {queued.map((q) => (
              <span
                key={q.id}
                className="inline-flex items-center gap-1 rounded-[var(--radius-pill)] border border-[var(--accent-border)] bg-[var(--accent-wash)] px-2.5 py-0.5 text-[0.72rem] text-[var(--color-foreground)] shadow-[var(--shadow-sm)]"
              >
                <span className="h-1.5 w-1.5 shrink-0 rounded-full bg-[var(--color-primary)]" aria-hidden />
                <span className="max-w-[200px] truncate">{q.text}</span>
              </span>
            ))}
          </div>
        ) : null
      }
      renderSlashMenu={(state) =>
        state.open ? (
          <div className="jcode-chat-input__slash mb-2 max-h-64 overflow-auto rounded-[var(--radius-xl)] border border-[var(--color-border)] bg-[var(--color-surface)] py-1.5 shadow-[var(--shadow-lg)]">
            {state.commands.map((cmd, i) => (
              <button
                key={cmd.slash}
                type="button"
                onClick={() => state.apply(cmd)}
                className={`flex w-full items-center gap-2.5 px-3.5 py-2 text-left text-[0.8rem] transition-colors ${
                  i === state.activeIndex
                    ? 'bg-[var(--accent-wash)] text-[var(--color-foreground)]'
                    : 'text-[var(--color-foreground)] hover:bg-[var(--neutral-wash-soft)]'
                }`}
              >
                <code className="rounded-[var(--radius-sm)] bg-[var(--color-muted)] px-1.5 py-0.5 font-mono text-[0.75rem] font-medium text-[var(--color-primary)]">
                  {cmd.slash}
                </code>
                {cmd.description && (
                  <span className="truncate text-[var(--color-muted-foreground)]">{cmd.description}</span>
                )}
              </button>
            ))}
          </div>
        ) : null
      }
      renderAttachments={(imgs, remove) => (
        <div className="jcode-chat-input__attachments">
          <AttachmentList images={imgs} onRemove={remove} size={56} />
        </div>
      )}
      renderAddAttachment={(openPicker) => (
        <button
          type="button"
          onClick={openPicker}
          className="jcode-chat-input__add-att"
          aria-label="Add attachment"
          title="Add image"
        >
          <PaperClipIcon className="h-4 w-4" />
        </button>
      )}
      renderSubmitButton={(mode, disabled, onActivate) =>
        mode === 'send' ? (
          <button
            type="button"
            onClick={onActivate}
            disabled={disabled}
            className="jcode-chat-input__send flex h-9 w-9 shrink-0 items-center justify-center rounded-full bg-[var(--color-primary)] text-[var(--color-on-primary)] shadow-[var(--shadow-sm)] transition-all duration-[var(--duration-fast)] hover:brightness-110 hover:shadow-[var(--shadow-md)] active:scale-95 disabled:opacity-35 disabled:shadow-none disabled:hover:brightness-100"
            aria-label="Send message"
          >
            <PaperAirplaneIcon className="h-4 w-4 -translate-x-px translate-y-px" />
          </button>
        ) : (
          <button
            type="button"
            onClick={onActivate}
            className="jcode-chat-input__stop flex h-9 w-9 shrink-0 items-center justify-center rounded-full bg-[var(--color-foreground)] text-[var(--color-surface)] shadow-[var(--shadow-sm)] transition-all duration-[var(--duration-fast)] hover:opacity-90 active:scale-95"
            aria-label="Stop"
          >
            <StopIcon className="h-3.5 w-3.5" />
          </button>
        )
      }
      renderSuffix={showContextBar ? () => <ContextBar size={20} /> : undefined}
    />
  )
})
