/**
 * ChatInput — styled composer (wraps headless Composer primitive).
 *
 * Floating surface with focus ring, queue chips, slash palette, attachments,
 * and a circular send/stop control. App-specific pickers (model/mode/workspace)
 * stay in the host product — pass them via `leadingControls` / `trailingControls`.
 *
 * Composer 2 adds a pluggable `attachmentAdapter` (images inline OR host uploads
 * with a progress state machine), drag-and-drop + paste ingestion, a dictation
 * mic button, control slots, and a `ComposerHandle` ref (`insertText` / `focus`).
 * With no adapter the legacy image-first `ChatImage` path is unchanged.
 */

import { forwardRef, memo } from 'react'
import type { ReactNode } from 'react'
import {
  ArrowUpTrayIcon,
  MicrophoneIcon,
  PaperAirplaneIcon,
  PaperClipIcon,
  StopIcon,
} from '@heroicons/react/24/outline'
import { Composer } from 'jcode-ui-core/primitives'
import type { AttachmentAdapter, ComposerHandle, PendingAttachment, SlashCommand } from 'jcode-ui-core/primitives'
import { AttachmentList, PendingAttachmentList } from './Attachment.js'
import { ContextBar } from './ContextBar.js'

export interface ChatInputProps {
  /** Slash commands (host-fetched). */
  slashCommands?: SlashCommand[]
  /** Allow image attachments (gated by model vision support). Legacy path. */
  allowImages?: boolean
  /** `accept` for the file picker. Default `image/*`. */
  acceptImages?: string
  /**
   * Pluggable attachment pipeline. Supersedes `allowImages` when provided —
   * files route through the adapter with an upload progress state machine.
   */
  attachmentAdapter?: AttachmentAdapter
  /** Fired on send with the completed attachments (adapter path). */
  onSendAttachments?: (attachments: PendingAttachment[]) => void
  /** Content rendered after the add-attachment control (e.g. a ModelSelector). */
  leadingControls?: ReactNode
  /** Content rendered just before the send button (e.g. a mode picker). */
  trailingControls?: ReactNode
  /** Content rendered below the composer row. */
  footer?: ReactNode
  /** Enable the dictation mic button (rendered only when the browser supports it). */
  enableDictation?: boolean
  /** BCP-47 language tag for dictation. */
  dictationLang?: string
  /** Placeholder text. */
  placeholder?: string
  /** Show the context bar suffix. Default true. */
  showContextBar?: boolean
  /** Callback after a message is sent/queued (host snaps timeline to bottom). */
  onSent?: () => void
}

export const ChatInput = memo(
  forwardRef<ComposerHandle, ChatInputProps>(function ChatInput(
    {
      slashCommands,
      allowImages = false,
      acceptImages = 'image/*',
      attachmentAdapter,
      onSendAttachments,
      leadingControls,
      trailingControls,
      footer,
      enableDictation = false,
      dictationLang,
      placeholder = 'Send a message…',
      showContextBar = true,
      onSent,
    },
    ref,
  ) {
    return (
      <Composer
        ref={ref}
        slashCommands={slashCommands}
        allowImages={allowImages}
        acceptImages={acceptImages}
        attachmentAdapter={attachmentAdapter}
        onSendAttachments={onSendAttachments}
        leadingControls={leadingControls}
        trailingControls={trailingControls}
        footer={footer}
        enableDictation={enableDictation}
        dictationLang={dictationLang}
        placeholder={placeholder}
        onSent={onSent}
        className="jcode-chat-input"
        renderQueue={(queued) =>
          queued.length > 0 ? (
            <div className="jcode-chat-input__queue mb-2 flex flex-wrap gap-1.5">
              {queued.map((q) => (
                <span
                  key={q.id}
                  className="inline-flex items-center gap-1 rounded-[var(--jcode-radius-pill)] border border-[var(--jcode-accent-border)] bg-[var(--jcode-accent-wash)] px-2.5 py-0.5 text-[0.72rem] text-[var(--jcode-color-foreground)] shadow-[var(--jcode-shadow-sm)]"
                >
                  <span className="h-1.5 w-1.5 shrink-0 rounded-full bg-[var(--jcode-color-primary)]" aria-hidden />
                  <span className="max-w-[200px] truncate">{q.text}</span>
                </span>
              ))}
            </div>
          ) : null
        }
        renderSlashMenu={(state) =>
          state.open ? (
            <div className="jcode-chat-input__slash mb-2 max-h-64 overflow-auto rounded-[var(--jcode-radius-xl)] border border-[var(--jcode-color-border)] bg-[var(--jcode-color-surface)] py-1.5 shadow-[var(--jcode-shadow-lg)]">
              {state.commands.map((cmd, i) => (
                <button
                  key={cmd.slash}
                  type="button"
                  onClick={() => state.apply(cmd)}
                  className={`flex w-full items-center gap-2.5 px-3.5 py-2 text-left text-[0.8rem] transition-colors ${
                    i === state.activeIndex
                      ? 'bg-[var(--jcode-accent-wash)] text-[var(--jcode-color-foreground)]'
                      : 'text-[var(--jcode-color-foreground)] hover:bg-[var(--jcode-neutral-wash-soft)]'
                  }`}
                >
                  <code className="rounded-[var(--jcode-radius-sm)] bg-[var(--jcode-color-muted)] px-1.5 py-0.5 font-mono text-[0.75rem] font-medium text-[var(--jcode-color-primary)]">
                    {cmd.slash}
                  </code>
                  {cmd.description && (
                    <span className="truncate text-[var(--jcode-color-muted-foreground)]">{cmd.description}</span>
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
        renderPendingAttachments={(items) => (
          <div className="jcode-chat-input__attachments">
            <PendingAttachmentList items={items} size={56} />
          </div>
        )}
        renderAddAttachment={(openPicker) => (
          <button
            type="button"
            onClick={openPicker}
            className="jcode-chat-input__add-att"
            aria-label="Add attachment"
            title={attachmentAdapter ? 'Attach files' : 'Add image'}
          >
            <PaperClipIcon className="h-4 w-4" />
          </button>
        )}
        renderDropOverlay={() => (
          <div className="jcode-composer-drop" aria-hidden>
            <div className="jcode-composer-drop__inner">
              <ArrowUpTrayIcon className="h-5 w-5" />
              <span>Drop files to attach</span>
            </div>
          </div>
        )}
        renderDictationButton={(state, toggle) => (
          <button
            type="button"
            onClick={toggle}
            className={`jcode-dictation-btn${state.listening ? ' is-listening' : ''}`}
            aria-label={state.listening ? 'Stop dictation' : 'Start dictation'}
            aria-pressed={state.listening}
            title={state.listening ? 'Stop dictation' : 'Dictate'}
          >
            <MicrophoneIcon className="h-4 w-4" />
          </button>
        )}
        renderSubmitButton={(mode, disabled, onActivate) =>
          mode === 'send' ? (
            <button
              type="button"
              onClick={onActivate}
              disabled={disabled}
              className="jcode-chat-input__send flex h-9 w-9 shrink-0 items-center justify-center rounded-full bg-[var(--jcode-color-primary)] text-[var(--jcode-color-on-primary)] shadow-[var(--jcode-shadow-sm)] transition-all duration-[var(--jcode-duration-fast)] hover:brightness-110 hover:shadow-[var(--jcode-shadow-md)] active:scale-95 disabled:opacity-35 disabled:shadow-none disabled:hover:brightness-100"
              aria-label="Send message"
            >
              <PaperAirplaneIcon className="h-4 w-4 -translate-x-px translate-y-px" />
            </button>
          ) : (
            <button
              type="button"
              onClick={onActivate}
              className="jcode-chat-input__stop flex h-9 w-9 shrink-0 items-center justify-center rounded-full bg-[var(--jcode-color-foreground)] text-[var(--jcode-color-surface)] shadow-[var(--jcode-shadow-sm)] transition-all duration-[var(--jcode-duration-fast)] hover:opacity-90 active:scale-95"
              aria-label="Stop"
            >
              <StopIcon className="h-3.5 w-3.5" />
            </button>
          )
        }
        renderSuffix={showContextBar ? () => <ContextBar size={20} /> : undefined}
      />
    )
  }),
)
